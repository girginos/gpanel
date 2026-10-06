package arsiv

// manifest.go — hesap yedeği (cPanel cpmove / DirectAdmin) MANİFESTİ.
//
// Oku, arşivi Ac ile TEK geçişte envanterler (hiçbir şey diske yazılmaz), yedeğin
// türünü içerikten tanır ve yalnız KÜÇÜK üst veri dosyalarını (hesap kaydı,
// userdata, parola dosyalarının adları, DNS bölgeleri, cron) okuyup ayrıştırır;
// veritabanı dökümlerini akışla SQLTara'dan geçirir. Hesap OLUŞTURMAZ, DB'ye/panele
// bağlanmaz.
//
// 🔴 Arşivdeki her şey iddiadır, yetki değildir:
//   - sahip/bayi/plan/IP gibi iddialar (OWNER=, RESELLER=, PLAN= …) okunmaz; kimin
//     adına hesap açılacağı yalnız çağıranın oturumundan (Kapsam) gelir (faz 2).
//   - alan adı yalnız söz dizimi olarak doğrulanır (AlanAdiGecerli). Çakışma/sahiplik
//     denetimi (domains.AdAlaniDenetle) DB ister: faz 2'de, HESAP OLUŞTURMADAN ÖNCE.
//   - posta ve DNS yalnız hesabın kendi alan adı listesindeki alanlar için kabul edilir
//     (başkasının alan adı için bölge/kutu iddiası atlanır).
//   - e-posta kutusu adı ".." ya da dizin ayracı içeremez; parola özetleri manifeste
//     HİÇ girmez (hash, kimlik bilgisi taşınmaz).

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Tur — yedeğin türü.
type Tur string

const (
	TurCPanel      Tur = "cpanel"
	TurDirectAdmin Tur = "directadmin"
)

var (
	ErrTur   = errors.New("arşiv: cPanel ya da DirectAdmin hesap yedeği olarak tanınmadı")
	ErrHesap = errors.New("arşiv: hesap yapısı güvenle okunamadı")
)

// Alan — hesabın alan adı kaydı.
type Alan struct {
	Ad        string `json:"ad"`
	Tur       string `json:"tur"`                  // ana | ek | alt | park
	BelgeKoku string `json:"belge_koku,omitempty"` // ev dizinine göre (public_html/...)
}

// Veritabani — yedekteki bir veritabanı ve döküm tarama sonucu.
type Veritabani struct {
	Ad           string    `json:"ad"`
	DumpYol      string    `json:"dump_yol"` // arşiv içi üye adı (DumpAc'e verilir)
	Boyut        int64     `json:"boyut"`
	Rapor        *SQLRapor `json:"rapor,omitempty"`
	Reddedildi   string    `json:"reddedildi,omitempty"` // SQLTara kural kodu; boş = temiz
	Kullanicilar []string  `json:"kullanicilar,omitempty"`
}

// PostaHesabi — yedekteki bir e-posta kutusu (parola özeti YOK).
type PostaHesabi struct {
	Alan       string `json:"alan"`
	Yerel      string `json:"yerel"`
	MaildirVar bool   `json:"maildir_var"`
	Bayt       int64  `json:"bayt"`
}

// DNSBolge — yedekteki bir DNS bölgesi (yalnız özet; kayıtlar faz 2'de ayrıştırılır).
type DNSBolge struct {
	Alan  string `json:"alan"`
	Satir int    `json:"satir"`
}

// Manifest — yedeğin güvenli özeti.
type Manifest struct {
	Tur             Tur           `json:"tur"`
	Kullanici       string        `json:"kullanici"` // yedekteki hesap adı (bilgi; hedef ad panelden gelir)
	AnaAlan         string        `json:"ana_alan"`
	Alanlar         []Alan        `json:"alanlar"`
	Veritabanlari   []Veritabani  `json:"veritabanlari"`
	DBKullanicilari []string      `json:"db_kullanicilari"` // yalnız adlar; parola/özet taşınmaz
	Posta           []PostaHesabi `json:"posta"`
	DNS             []DNSBolge    `json:"dns"`
	Cron            []string      `json:"cron,omitempty"` // elle inceleme için; OTOMATİK kurulmaz

	UyeSayisi   int   `json:"uye_sayisi"`
	DosyaSayisi int   `json:"dosya_sayisi"` // site dosyası (ev/belge ağacı) düzenli dosya sayısı
	DizinSayisi int   `json:"dizin_sayisi"`
	ToplamBayt  int64 `json:"toplam_bayt"`
	Baglar      int   `json:"baglar"`       // symlink üyeleri
	SabitBaglar int   `json:"sabit_baglar"` // hardlink üyeleri
	Aygitlar    int   `json:"aygitlar"`     // aygıt/FIFO (çıkarmada atlanır)

	Uyarilar     []string `json:"uyarilar"`
	Tasinmayacak []string `json:"tasinmayacak"`
}

// Secenek — Oku ayarları.
type Secenek struct {
	Sinirlar  Sinirlar
	DumpSinir int64 // tek veritabanı dökümü bildirilen boyut üst sınırı (varsayılan 32 GiB)
	SQLAtla   bool  // true: dökümler taranmaz (yalnız ad/boyut). Güvenlik taraması VARSAYILAN açık.
}

const (
	maxUyari = 200
	maxAlan  = 5000
	maxPosta = 20000
	maxCron  = 200
)

// uyari — arşivden gelen metin %q ile ve kısaltılarak girer.
func (m *Manifest) uyari(bicim string, a ...any) {
	if len(m.Uyarilar) == maxUyari {
		m.Uyarilar = append(m.Uyarilar, "… daha fazla uyarı kırpıldı")
		return
	}
	if len(m.Uyarilar) > maxUyari {
		return
	}
	m.Uyarilar = append(m.Uyarilar, fmt.Sprintf(bicim, a...))
}

var (
	reKullanici = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	reDBAdi     = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)
	reDBKul     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
	reMailYerel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	reEtiket    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// sistemDBler — taşınamayacak sistem veritabanları (küçük harf).
var sistemDBler = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
	"test": true, "cphulkd": true, "roundcube": true, "horde": true, "eximstats": true,
	"leechprotect": true, "modsec": true, "cpanel_ssl_ingest": true,
}

// AlanAdiGecerli — alan adı SÖZ DİZİMİ (küçük harf, ≥2 etiket, etiket ≤63, toplam ≤253,
// son etiket yalnız rakamlardan oluşmaz → IP değil, joker yok). Sahiplik/çakışma
// denetimi DEĞİLDİR (faz 2: domains.AdAlaniDenetle).
func AlanAdiGecerli(s string) bool {
	if len(s) < 4 || len(s) > 253 || s != strings.ToLower(s) {
		return false
	}
	et := strings.Split(s, ".")
	if len(et) < 2 {
		return false
	}
	for _, e := range et {
		if !reEtiket.MatchString(e) {
			return false
		}
	}
	son := et[len(et)-1]
	return strings.Trim(son, "0123456789") != ""
}

// PostaYerelGecerli — e-posta kutusu yerel adı: dizin ayracı/".." yok (maildir yolunda
// kullanılacağı için /var/vmail kaçışı engellenir).
func PostaYerelGecerli(s string) bool {
	return reMailYerel.MatchString(s) && !strings.Contains(s, "..") && !strings.HasSuffix(s, ".")
}

// temizMetin — arşivden gelen yazdırılabilir tek satır metin mi (cron/uyarı için).
func temizMetin(s string) bool {
	if !utf8.ValidString(s) || len(s) > 512 {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return false
		}
	}
	return true
}

func bol(ad string) []string { return strings.Split(ad, "/") }

// ustVeriMi — belleğe alınacak küçük üst veri dosyaları (iki yerleşim birlikte).
func ustVeriMi(ad string) bool {
	p := bol(ad)
	switch {
	// cPanel: <kök>/cp|userdata|dnszones|cron/<ad>, <kök>/mysql.sql, <kök>/homedir/etc/<alan>/passwd
	case len(p) == 3 && (p[1] == "cp" || p[1] == "userdata" || p[1] == "dnszones" || p[1] == "cron"):
		return true
	case len(p) == 2 && p[1] == "mysql.sql":
		return true
	case len(p) == 5 && p[1] == "homedir" && p[2] == "etc" && p[4] == "passwd":
		return true
	// DirectAdmin: backup/*.conf, backup/<alan>/email/passwd
	case len(p) == 2 && p[0] == "backup" && strings.HasSuffix(p[1], ".conf"):
		return true
	case len(p) == 4 && p[0] == "backup" && p[2] == "email" && p[3] == "passwd":
		return true
	}
	return false
}

// Oku — yedeği güvenle okuyup manifestini döndürür.
func Oku(ctx context.Context, yol string, o Secenek) (*Manifest, error) {
	a, err := Ac(ctx, yol, o.Sinirlar, ustVeriMi)
	if err != nil {
		return nil, err
	}
	if o.DumpSinir <= 0 {
		o.DumpSinir = 32 << 30
	}
	m := &Manifest{UyeSayisi: len(a.Uyeler)}
	cpKok, daVar, err := turBul(a)
	if err != nil {
		return nil, err
	}
	switch {
	case cpKok != "":
		m.Tur = TurCPanel
		m.Tasinmayacak = cpanelTasinmayacak
		err = cpanelOku(ctx, a, cpKok, o, m)
	case daVar:
		m.Tur = TurDirectAdmin
		m.Tasinmayacak = daTasinmayacak
		err = directadminOku(ctx, a, o, m)
	}
	if err != nil {
		return nil, err
	}
	if m.Aygitlar > 0 {
		m.uyari("%d aygıt/FIFO düğümü çıkarmada atlanacak", m.Aygitlar)
	}
	if m.SabitBaglar > 0 {
		m.uyari("%d sabit bağ yalnız arşivde daha önce yazılmış dosyaya işaret ediyorsa çıkarılır", m.SabitBaglar)
	}
	m.sirala()
	return m, nil
}

// turBul — cPanel: tam bir "<kök>/userdata/main" üyesi; DA: "backup/user.conf".
func turBul(a *Arsiv) (cpKok string, daVar bool, err error) {
	var kokler []string
	for _, u := range a.Uyeler {
		p := bol(u.Ad)
		if len(p) == 3 && p[1] == "userdata" && p[2] == "main" && u.Tur == '0' {
			kokler = append(kokler, p[0])
		}
		if u.Ad == "backup/user.conf" && u.Tur == '0' {
			daVar = true
		}
	}
	switch {
	case len(kokler) > 1:
		return "", false, fmt.Errorf("%w: birden çok hesap kökü", ErrHesap)
	case len(kokler) == 1 && daVar:
		return "", false, fmt.Errorf("%w: hem cPanel hem DirectAdmin işaretleri var", ErrTur)
	case len(kokler) == 1:
		return kokler[0], false, nil
	case daVar:
		return "", true, nil
	}
	return "", false, ErrTur
}

func (m *Manifest) sirala() {
	sort.Slice(m.Alanlar, func(i, j int) bool { return m.Alanlar[i].Ad < m.Alanlar[j].Ad })
	sort.Slice(m.Veritabanlari, func(i, j int) bool { return m.Veritabanlari[i].Ad < m.Veritabanlari[j].Ad })
	sort.Strings(m.DBKullanicilari)
	sort.Slice(m.Posta, func(i, j int) bool {
		if m.Posta[i].Alan != m.Posta[j].Alan {
			return m.Posta[i].Alan < m.Posta[j].Alan
		}
		return m.Posta[i].Yerel < m.Posta[j].Yerel
	})
	sort.Slice(m.DNS, func(i, j int) bool { return m.DNS[i].Alan < m.DNS[j].Alan })
}

// sayim — onek ("" değil) altındaki üyeleri sayar: düzenli dosya/dizin/bağ/aygıt.
func (m *Manifest) sayim(a *Arsiv, onek string) {
	for _, u := range a.Uyeler {
		if _, ok := altinda(u.Ad, onek); !ok || u.Ad == onek {
			continue
		}
		switch u.Tur {
		case '0':
			m.DosyaSayisi++
			m.ToplamBayt += u.Boyut
		case '5':
			m.DizinSayisi++
		case '2':
			m.Baglar++
		case '1':
			m.SabitBaglar++
		case '3', '4', '6':
			m.Aygitlar++
		}
	}
}

// postaDosyasiOku — "<kullanıcı>:<özet>…" satırlarının YALNIZ ilk alanları (özet atılır).
func postaDosyasiOku(b []byte) (adlar []string, bozuk int) {
	for i, sat := range strings.Split(string(b), "\n") {
		if i > maxPosta {
			break
		}
		sat = strings.TrimRight(sat, "\r")
		if strings.TrimSpace(sat) == "" || strings.HasPrefix(sat, "#") {
			continue
		}
		ad, _, _ := strings.Cut(sat, ":")
		if !PostaYerelGecerli(ad) {
			bozuk++
			continue
		}
		adlar = append(adlar, ad)
	}
	return adlar, bozuk
}

// altBayt — "<onek>/<a>/<b>" biçiminde ilk iki bileşene göre toplam bayt + dizin var mı.
type maildirOzet struct {
	bayt int64
	var_ bool
}

func maildirOzetle(a *Arsiv, onek string) map[string]*maildirOzet {
	out := map[string]*maildirOzet{}
	for _, u := range a.Uyeler {
		rel, ok := altinda(u.Ad, onek)
		if !ok || rel == "" {
			continue
		}
		p := strings.SplitN(rel, "/", 3)
		if len(p) < 2 {
			continue
		}
		k := p[0] + "/" + p[1]
		o := out[k]
		if o == nil {
			o = &maildirOzet{}
			out[k] = o
		}
		o.var_ = true
		if u.Tur == '0' {
			o.bayt += u.Boyut
		}
	}
	return out
}

// dbSistemMi — taşınmaz veritabanı adı mı.
func dbSistemMi(ad string) bool { return sistemDBler[strings.ToLower(ad)] }

// dumpTara — bir döküm üyesini akışla tarar ve Veritabani kaydını doldurur.
func dumpTara(ctx context.Context, a *Arsiv, o Secenek, ad, yol string, boyut int64, m *Manifest) Veritabani {
	v := Veritabani{Ad: ad, DumpYol: yol, Boyut: boyut}
	if o.SQLAtla {
		return v
	}
	rc, err := a.DumpAc(ctx, yol, o.DumpSinir)
	if err != nil {
		v.Reddedildi = KuralBoyut
		m.uyari("veritabanı %q dökümü okunamadı: %v", kisalt(ad), err)
		return v
	}
	defer rc.Close()
	rap, err := SQLTara(ctx, rc, SQLSinir{ToplamBayt: o.DumpSinir})
	v.Rapor = &rap
	var he *SQLHata
	switch {
	case err == nil:
	case errors.As(err, &he):
		v.Reddedildi = he.Kural
		m.uyari("veritabanı %q dökümü güvenlik kuralıyla reddedildi: %s", kisalt(ad), he.Kural)
	default:
		v.Reddedildi = KuralBozuk
		m.uyari("veritabanı %q dökümü okunamadı: %v", kisalt(ad), err)
	}
	if err == nil && !rap.Tamam {
		m.uyari("veritabanı %q dökümünde bitiş damgası yok (kesik olabilir)", kisalt(ad))
	}
	if rap.Definer > 0 {
		m.uyari("veritabanı %q: %d ifadede DEFINER var (içe aktarmada süzülür)", kisalt(ad), rap.Definer)
	}
	return v
}
