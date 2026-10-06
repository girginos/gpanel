package arsiv

// cpanel.go — cPanel "cpmove-<kullanıcı>.tar.gz" / Full Backup ayrıştırıcısı.
//
// Yerleşim (kök = tek üst dizin, ör. cpmove-u/):
//   cp/<kullanıcı>           KEY=VALUE hesap kaydı (USER=, DNS=, DNS1=…); OWNER/PLAN/IP iddiaları OKUNMAZ
//   userdata/main            dar YAML: main_domain, addon_domains, sub_domains, parked_domains
//   userdata/<alan>          documentroot: /home/<u>/public_html/...
//   mysql/<db>.sql           veritabanı dökümü (mysqldump)   mysql.sql = GRANT dökümü (YALNIZ ad okunur)
//   dnszones/<alan>.db       BIND bölge dosyası
//   homedir/                 site dosyaları; homedir/mail/<alan>/<kutu>/ maildir; homedir/etc/<alan>/passwd
//   cron/<kullanıcı>         crontab
// Gerçek bir cPanel yedeğiyle doğrulanmadı (olgunluk: deneysel).

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var cpanelTasinmayacak = []string{
	"hesap/e-posta/veritabanı kullanıcı PAROLALARI ve özetleri (yeniden belirlenir)",
	"mysql.sql (GRANT/CREATE USER dökümü: hiçbir koşulda çalıştırılmaz)",
	"mysql/*.create ve DEFINER/GRANT içeren ifadeler",
	"e-posta yönlendiricileri, otomatik yanıtlayıcılar, posta listeleri, spam ayarları",
	"alt alan adı yönlendirmeleri, .htaccess dışı sunucu ayarları (Apache/PHP sürümü, ModSecurity)",
	"SSL sertifikaları (yeniden düzenlenir), uzak MySQL erişim konakları",
	"cron satırlarından çevrilemeyenler (@reboot, güvensiz içerik vb.) taşıma raporunda nedenleriyle listelenir",
	"kota/bant genişliği/plan, günlükler, ana hesabın sistem posta kutusu",
	"sembolik bağlar kök dışına çıkıyorsa, aygıt/FIFO düğümleri, setuid/setgid bitleri",
}

func cpanelOku(ctx context.Context, a *Arsiv, kok string, o Secenek, m *Manifest) error {
	// Hesap adı: cp/<kullanıcı> (tek dosya).
	var cpAd string
	for _, u := range a.Uyeler {
		if rel, ok := altinda(u.Ad, kok+"/cp"); ok && rel != "" && !strings.Contains(rel, "/") && u.Tur == '0' {
			if cpAd != "" {
				return fmt.Errorf("%w: birden çok cp/ kaydı", ErrHesap)
			}
			cpAd = rel
		}
	}
	if !reKullanici.MatchString(cpAd) {
		return fmt.Errorf("%w: hesap adı geçersiz", ErrHesap)
	}
	m.Kullanici = cpAd

	cp := anahtarDeger(a.UstVeri[kok+"/cp/"+cpAd])
	if u := cp["USER"]; u != "" && u != cpAd {
		m.uyari("cp kaydındaki USER (%q) dosya adıyla uyuşmuyor; dosya adı esas alındı", kisalt(u))
	}
	for _, iddia := range []string{"OWNER", "RESELLER", "PLAN", "IP"} {
		if cp[iddia] != "" {
			m.uyari("yedekteki %s iddiası yok sayıldı (sahip/plan hedef panelde belirlenir)", iddia)
		}
	}

	main, err := cpanelMainOku(a.UstVeri[kok+"/userdata/main"])
	if err != nil {
		return err
	}
	if !AlanAdiGecerli(main.ana) {
		return fmt.Errorf("%w: ana alan adı geçersiz", ErrHesap)
	}
	if d := strings.ToLower(cp["DNS"]); d != "" && d != main.ana {
		m.uyari("cp kaydındaki DNS (%q) userdata/main ana alanıyla uyuşmuyor; userdata esas alındı", kisalt(d))
	}
	m.AnaAlan = main.ana

	home := kok + "/homedir"
	alanSet := map[string]bool{}
	ekle := func(ad, tur, ud string) {
		ad = strings.ToLower(ad)
		if !AlanAdiGecerli(ad) {
			m.uyari("geçersiz alan adı atlandı: %q", kisalt(ad))
			return
		}
		if alanSet[ad] {
			return
		}
		if len(m.Alanlar) >= maxAlan {
			m.uyari("alan adı sayısı sınırı aşıldı (%d); kalanlar atlandı", maxAlan)
			return
		}
		alanSet[ad] = true
		al := Alan{Ad: ad, Tur: tur}
		if ud == "" {
			ud = ad
		}
		if b, ok := a.UstVeri[kok+"/userdata/"+ud]; ok {
			al.BelgeKoku = cpanelBelgeKoku(b, cpAd, m)
		}
		if al.BelgeKoku != "" && !uyeVar(a, home+"/"+al.BelgeKoku) {
			m.uyari("alan %q belge kökü yedekte yok: %q", kisalt(ad), kisalt(al.BelgeKoku))
			al.BelgeKoku = ""
		}
		m.Alanlar = append(m.Alanlar, al)
	}
	ekle(main.ana, "ana", main.ana)
	keys := make([]string, 0, len(main.ekler))
	for k := range main.ekler {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ekle(k, "ek", main.ekler[k])
	}
	for _, s := range main.altlar {
		ekle(s, "alt", s)
	}
	for _, p := range main.parklar {
		ekle(p, "park", main.ana)
	}

	m.sayim(a, home)

	// Veritabanları: mysql/<db>.sql
	dbSet := map[string]int{}
	for _, u := range a.Uyeler {
		rel, ok := altinda(u.Ad, kok+"/mysql")
		if !ok || rel == "" || strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".sql") {
			continue
		}
		ad := strings.TrimSuffix(rel, ".sql")
		switch {
		case u.Tur != '0':
			m.uyari("veritabanı üyesi düzenli dosya değil, atlandı: %q", kisalt(rel))
		case !reDBAdi.MatchString(ad):
			m.uyari("geçersiz veritabanı adı atlandı: %q", kisalt(ad))
		case dbSistemMi(ad):
			m.uyari("sistem veritabanı atlandı: %q", kisalt(ad))
		default:
			dbSet[ad] = len(m.Veritabanlari)
			m.Veritabanlari = append(m.Veritabanlari, dumpTara(ctx, a, o, ad, u.Ad, u.Boyut, m))
		}
	}
	if len(m.Veritabanlari) > 0 || a.UstVeri[kok+"/mysql.sql"] != nil {
		cpanelDBKullanicilari(a.UstVeri[kok+"/mysql.sql"], dbSet, m)
	}

	// Posta: homedir/etc/<alan>/passwd → kutu adları; alan hesabın kendi listesinde olmalı.
	mail := maildirOzetle(a, home+"/mail")
	for ad, b := range a.UstVeri {
		p := bol(ad)
		if len(p) != 5 || p[0] != kok || p[1] != "homedir" || p[2] != "etc" || p[4] != "passwd" {
			continue
		}
		alan := p[3]
		m.postaEkle(alan, b, alanSet, mail)
	}

	// DNS: dnszones/<alan>.db
	for ad, b := range a.UstVeri {
		p := bol(ad)
		if len(p) != 3 || p[0] != kok || p[1] != "dnszones" || !strings.HasSuffix(p[2], ".db") {
			continue
		}
		alan := strings.TrimSuffix(p[2], ".db")
		if !AlanAdiGecerli(alan) || !alanSet[alan] {
			m.uyari("hesaba ait olmayan/geçersiz DNS bölgesi atlandı: %q", kisalt(alan))
			continue
		}
		n := 0
		for _, s := range strings.Split(string(b), "\n") {
			if t := strings.TrimSpace(s); t != "" && !strings.HasPrefix(t, ";") {
				n++
			}
		}
		m.DNS = append(m.DNS, DNSBolge{Alan: alan, Satir: n})
	}

	m.cronOku(a.UstVeri[kok+"/cron/"+cpAd])
	return nil
}

// postaEkle — passwd dosyasındaki kutuları manifeste ekler (cPanel + DA ortak).
func (m *Manifest) postaEkle(alan string, passwd []byte, alanSet map[string]bool, ozet map[string]*maildirOzet) {
	if !AlanAdiGecerli(alan) || !alanSet[alan] {
		m.uyari("hesaba ait olmayan/geçersiz alan için posta kutuları atlandı: %q", kisalt(alan))
		return
	}
	adlar, bozuk := postaDosyasiOku(passwd)
	if bozuk > 0 {
		m.uyari("alan %q: %d geçersiz kutu adı atlandı (\"..\", dizin ayracı vb.)", alan, bozuk)
	}
	gor := map[string]bool{}
	for _, y := range adlar {
		kucuk := strings.ToLower(y)
		if gor[kucuk] {
			m.uyari("alan %q: yinelenen kutu adı atlandı: %q", alan, kisalt(y))
			continue
		}
		gor[kucuk] = true
		if len(m.Posta) >= maxPosta {
			m.uyari("e-posta kutusu sayısı sınırı aşıldı (%d)", maxPosta)
			return
		}
		pk := PostaHesabi{Alan: alan, Yerel: y}
		if oz := ozet[alan+"/"+y]; oz != nil {
			pk.MaildirVar, pk.Bayt = oz.var_, oz.bayt
		} else {
			m.uyari("kutu %s@%s için maildir yedekte yok (yalnız hesap oluşturulur)", kisalt(y), alan)
		}
		m.Posta = append(m.Posta, pk)
	}
}

// cronOku — crontab satırlarını HAM olarak manifeste koyar (ortam satırları dahil, sırayla).
// Çevirme/kurma faz 2'dedir (tasima/arsiv_cron.go); kabuk komutudur, burada yorumlanmaz.
// Geçersiz karakterli ya da sınırı aşan satır SESSİZCE düşmez: uyarı sayısıyla raporlanır.
func (m *Manifest) cronOku(b []byte) {
	if len(b) == 0 {
		return
	}
	atlanan, asan := 0, 0
	for _, s := range strings.Split(string(b), "\n") {
		s = strings.TrimSpace(strings.TrimRight(s, "\r"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if !temizMetin(s) {
			atlanan++
			continue
		}
		if len(m.Cron) >= maxCron {
			asan++
			continue
		}
		m.Cron = append(m.Cron, s)
	}
	if atlanan > 0 {
		m.uyari("%d cron satırı geçersiz karakter/uzunluk nedeniyle okunamadı (taşınamadı)", atlanan)
	}
	if asan > 0 {
		m.uyari("cron satırı sınırı (%d) aşıldı: %d satır taşınamadı", maxCron, asan)
	}
}

// uyeVar — ad'ın kendisi ya da altında bir üye var mı.
func uyeVar(a *Arsiv, ad string) bool {
	if _, ok := a.indeks[ad]; ok {
		return true
	}
	for _, u := range a.Uyeler {
		if strings.HasPrefix(u.Ad, ad+"/") {
			return true
		}
	}
	return false
}

// anahtarDeger — KEY=VALUE satırları (ilk '='; yineleneni ilk kazanır, ≤2000 satır).
func anahtarDeger(b []byte) map[string]string {
	out := map[string]string{}
	for i, s := range strings.Split(string(b), "\n") {
		if i > 2000 {
			break
		}
		k, v, ok := strings.Cut(strings.TrimRight(s, "\r"), "=")
		if !ok || k == "" || len(k) > 64 || strings.ContainsAny(k, " \t") {
			continue
		}
		if _, var_ := out[k]; !var_ {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// cpanelBelgeKoku — "documentroot: /home/<u>/public_html/x" → "public_html/x" (ev'e göre).
func cpanelBelgeKoku(b []byte, kul string, m *Manifest) string {
	for _, s := range strings.Split(string(b), "\n") {
		s = strings.TrimRight(s, "\r")
		if !strings.HasPrefix(s, "documentroot:") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(s, "documentroot:")), `"'`)
		re := regexp.MustCompile(`^/home[0-9]*/` + regexp.QuoteMeta(kul) + `/(.+)$`)
		sm := re.FindStringSubmatch(v)
		if sm == nil {
			m.uyari("belge kökü hesabın ev dizini dışında, atlandı: %q", kisalt(v))
			return ""
		}
		rel, err := adTemizle(sm[1])
		if err != nil || rel == "" {
			m.uyari("geçersiz belge kökü atlandı: %q", kisalt(v))
			return ""
		}
		return rel
	}
	return ""
}

type cpMain struct {
	ana     string
	ekler   map[string]string // ek alan → userdata adı
	altlar  []string
	parklar []string
}

const cpYAMLSatir = 5000

// cpanelMainOku — userdata/main'in DAR alt kümesi. Çapa/takma ad/akış stili/etiket
// (&, *, {}, [], !, |, >) ilgilenilen bölümlerde → hesap reddedilir.
func cpanelMainOku(b []byte) (cpMain, error) {
	r := cpMain{ekler: map[string]string{}}
	if len(b) == 0 {
		return r, fmt.Errorf("%w: userdata/main yok", ErrHesap)
	}
	satirlar := strings.Split(string(b), "\n")
	if len(satirlar) > cpYAMLSatir {
		return r, fmt.Errorf("%w: userdata/main çok uzun", ErrHesap)
	}
	bolum := ""
	duz := func(s string) string { return strings.ToLower(strings.Trim(strings.TrimSpace(s), `"'`)) }
	kotu := func(s string) bool { return strings.ContainsAny(s, "&*{}[]!|>") || strings.Contains(s, "<<") }
	for _, ham := range satirlar {
		s := strings.TrimRight(ham, " \t\r")
		t := strings.TrimSpace(s)
		if t == "" || strings.HasPrefix(t, "#") || t == "---" {
			continue
		}
		girinti := s[0] == ' ' || s[0] == '\t'
		liste := strings.HasPrefix(t, "- ") || t == "-"
		if !girinti && !liste {
			k, v, ok := strings.Cut(t, ":")
			if !ok {
				continue // ilgilenmediğimiz çok satırlı değerin devamı
			}
			bolum = ""
			switch k {
			case "main_domain":
				// Çapa/takma ad içeren değer alan adı doğrulamasından geçemez (AlanAdiGecerli).
				r.ana = duz(v)
			case "addon_domains", "sub_domains", "parked_domains":
				v = strings.TrimSpace(v)
				if v != "" && v != "{}" && v != "[]" {
					return r, fmt.Errorf("%w: desteklenmeyen YAML (%s)", ErrHesap, k)
				}
				if v == "" {
					bolum = k
				}
			}
			continue
		}
		if bolum == "" {
			continue
		}
		if kotu(t) {
			return r, fmt.Errorf("%w: desteklenmeyen YAML (%s)", ErrHesap, bolum)
		}
		if len(r.ekler)+len(r.altlar)+len(r.parklar) >= maxAlan {
			return r, fmt.Errorf("%w: alan adı sayısı sınırı", ErrHesap)
		}
		switch {
		case liste:
			v := duz(strings.TrimPrefix(t, "-"))
			switch bolum {
			case "sub_domains":
				r.altlar = append(r.altlar, v)
			case "parked_domains":
				r.parklar = append(r.parklar, v)
			}
		case bolum == "addon_domains":
			k, v, ok := strings.Cut(t, ":")
			if ok {
				r.ekler[duz(k)] = duz(v)
			}
		}
	}
	return r, nil
}

var reGrant = regexp.MustCompile("^GRANT .+ ON `?([^`]{1,200}?)`?\\.\\* TO '([^']{1,64})'@")

// cpanelDBKullanicilari — mysql.sql'den YALNIZ kullanıcı adları ve hangi veritabanına
// yetkili oldukları (satır satır; hiçbir şey çalıştırılmaz, özet/parola saklanmaz).
func cpanelDBKullanicilari(b []byte, dbSet map[string]int, m *Manifest) {
	gor := map[string]bool{}
	for i, s := range strings.Split(string(b), "\n") {
		if i > 100000 {
			break
		}
		sm := reGrant.FindStringSubmatch(strings.TrimSpace(s))
		if sm == nil {
			continue
		}
		db := strings.ReplaceAll(sm[1], `\_`, "_")
		kul := sm[2]
		if !reDBKul.MatchString(kul) {
			m.uyari("geçersiz veritabanı kullanıcı adı atlandı: %q", kisalt(kul))
			continue
		}
		if !gor[kul] {
			gor[kul] = true
			m.DBKullanicilari = append(m.DBKullanicilari, kul)
		}
		if j, ok := dbSet[db]; ok {
			v := &m.Veritabanlari[j]
			dup := false
			for _, k := range v.Kullanicilar {
				dup = dup || k == kul
			}
			if !dup {
				v.Kullanicilar = append(v.Kullanicilar, kul)
			}
		}
	}
	if len(m.DBKullanicilari) > 0 {
		m.uyari("%d veritabanı kullanıcısı bulundu: parolalar taşınmaz, yeniden oluşturulur", len(m.DBKullanicilari))
	}
}
