package tasima

// kaynak_cpanel.go — cPanel kaynak bağdaştırıcısı (canlı SSH, root).
//
// Keşif: alan adı → belge kökü eşlemesi /var/cpanel/userdata/<u>/main'den kurulur
// (main_domain, addon_domains, sub_domains, parked_domains). Eski sürüm
// /var/cpanel/users/<u> DNS satırlarını okuyup userdata/<u>/<alan> dosyasını
// arıyordu: ek (addon) alan adının userdata dosyası ALT ALAN biçimindeki adıyla
// (addon.ana.com) durduğundan belge kökü bulunamıyor, ek alan ana sitenin
// public_html'ine düşüyordu.
//
// 🔴 Uzak çıktı DÜŞMANDIR: userdata YAML'ı kaynakta okunup ham satırlar olarak
// gelir; burada DAR bir alt küme ayrıştırıcıyla (yalnız düz anahtar, düz liste ve
// tek düzey eşleme) okunur. YAML çapası/takma adı/birleştirme (&, *, <<) içeren
// hesap bütünüyle atlanır; satır/eleman sınırları vardır. Alan adı, yol ve sayı
// her zamanki allowlist'lerden (reAlanAdi, gecerliUzakYol, reHesap) geçer.
//
// Doğrulanmalı (gerçek cPanel sunucusunda denenmedi — olgunluk "deneysel"):
//   - DB öneki: hesap adı ya da ilk 8 karakteri (eski cPanel) + "_".
//   - Posta: ~/etc/<alan>/passwd ilk alanı kutu adı; maildir ~/mail/<alan>/<kutu>/.
//   - SSL: /var/cpanel/ssl/apache_tls/<alan>/combined (anahtar + sertifika zinciri).

import (
	"context"
	"fmt"
	"strings"
)

// komutCpanel — hesap başına: ###USER, ###DB, userdata/main satırları (###Y:)
// ve her userdata dosyasının belge kökü (###CPUD:<ad>|<kök>|<php>|<boyut MB>).
const komutCpanel = `for f in /var/cpanel/users/*; do
  [ -f "$f" ] || continue
  u=$(basename "$f")
  case "$u" in system|root|*.cache|*.lock) continue ;; esac
  ud="/var/cpanel/userdata/$u"
  [ -f "$ud/main" ] || continue
  u8=$(printf '%s' "$u" | cut -c1-8)
  echo "###USER:$u"
  echo "###DB:$(mysql -N -B -e "SHOW DATABASES" 2>/dev/null | grep -E "^(${u}|${u8})_" | tr '\n' ',')"
  head -n 400 "$ud/main" 2>/dev/null | sed 's/^/###Y:/'
  for g in "$ud"/*; do
    [ -f "$g" ] || continue
    n=$(basename "$g")
    case "$n" in main|cache|*.cache|*.json|*.yaml|*.stor|*_SSL|*.db|*.lock) continue ;; esac
    dr=$(grep -E '^documentroot:' "$g" 2>/dev/null | head -1 | sed 's/^documentroot: *//')
    [ -n "$dr" ] || continue
    pv=$(grep -E '^phpversion:' "$g" 2>/dev/null | head -1 | sed 's/^phpversion: *//')
    sz=$(du -sm "$dr" 2>/dev/null | cut -f1)
    echo "###CPUD:$n|$dr|$pv|$sz"
  done
done`

func cpanelKesfet(ctx context.Context, k *Kaynak) ([]Hesap, error) {
	cikti, err := k.Calistir(ctx, komutCpanel)
	if err != nil {
		return nil, err
	}
	return cpanelAyristir(cikti), nil
}

// cpanelMain — userdata/<u>/main'in ihtiyaç duyulan alt kümesi.
type cpanelMain struct {
	Ana    string
	Ekler  map[string]string // ek alan adı → userdata adı (alt alan biçimi)
	Altlar []string
	Parkl  []string
}

const (
	cpanelYAMLSatirSiniri  = 400
	cpanelYAMLElemanSiniri = 500
)

// cpanelMainAyristir — main'in DAR alt kümesini okur. Desteklenmeyen YAML yapısı
// (çapa, takma ad, birleştirme, akış stili) → ok=false (hesap atlanır).
func cpanelMainAyristir(satirlar []string) (cpanelMain, bool) {
	m := cpanelMain{Ekler: map[string]string{}}
	if len(satirlar) > cpanelYAMLSatirSiniri {
		return m, false
	}
	bolum := ""
	eleman := 0
	for _, ham := range satirlar {
		satir := strings.TrimRight(ham, " \t\r")
		if strings.TrimSpace(satir) == "" || strings.HasPrefix(strings.TrimSpace(satir), "#") || satir == "---" {
			continue
		}
		// Çapa/takma ad/birleştirme/akış stili: dar ayrıştırıcının bilmediği yapı.
		if strings.ContainsAny(satir, "&*{}[]") || strings.Contains(satir, "<<") {
			return m, false
		}
		girintili := strings.HasPrefix(satir, " ") || strings.HasPrefix(satir, "\t")
		govde := strings.TrimSpace(satir)
		if !girintili {
			bolum = ""
			anahtar, deger, ok := strings.Cut(govde, ":")
			if !ok {
				return m, false
			}
			anahtar = strings.TrimSpace(anahtar)
			deger = yamlDuz(deger)
			switch anahtar {
			case "main_domain":
				m.Ana = deger
			case "addon_domains", "sub_domains", "parked_domains":
				if deger != "" && deger != "{}" && deger != "[]" {
					return m, false
				}
				bolum = anahtar
			}
			continue
		}
		if bolum == "" {
			continue // ilgilenmediğimiz bir anahtarın alt satırı
		}
		eleman++
		if eleman > cpanelYAMLElemanSiniri {
			return m, false
		}
		switch bolum {
		case "addon_domains":
			anahtar, deger, ok := strings.Cut(govde, ":")
			if !ok {
				return m, false
			}
			m.Ekler[yamlDuz(anahtar)] = yamlDuz(deger)
		case "sub_domains", "parked_domains":
			if !strings.HasPrefix(govde, "- ") && govde != "-" {
				return m, false
			}
			v := yamlDuz(strings.TrimPrefix(govde, "-"))
			if bolum == "sub_domains" {
				m.Altlar = append(m.Altlar, v)
			} else {
				m.Parkl = append(m.Parkl, v)
			}
		}
	}
	return m, m.Ana != ""
}

// yamlDuz — düz skaler: boşluk ve tek/çift tırnak soyulur, küçük harfe indirilir.
func yamlDuz(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' && s[len(s)-1] == '\'' || s[0] == '"' && s[len(s)-1] == '"') {
		s = s[1 : len(s)-1]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

type cpanelUD struct {
	Kok   string
	PHP   string
	Boyut int64
}

// cpanelAyristir — komutCpanel çıktısı → Hesap listesi.
func cpanelAyristir(cikti string) []Hesap {
	var sonuc []Hesap
	var hesap, dbler string
	var yaml []string
	ud := map[string]cpanelUD{}
	gecerli := false

	bitir := func() {
		if !gecerli {
			return
		}
		sonuc = append(sonuc, cpanelHesapKur(hesap, dbler, yaml, ud)...)
	}
	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimRight(satir, "\r")
		switch {
		case strings.HasPrefix(satir, "###USER:"):
			bitir()
			u := strings.TrimSpace(strings.TrimPrefix(satir, "###USER:"))
			hesap, dbler, yaml, ud = u, "", nil, map[string]cpanelUD{}
			gecerli = reHesap.MatchString(u)
		case strings.HasPrefix(satir, "###DB:"):
			dbler = strings.TrimPrefix(satir, "###DB:")
		case strings.HasPrefix(satir, "###Y:"):
			if len(yaml) <= cpanelYAMLSatirSiniri {
				yaml = append(yaml, strings.TrimPrefix(satir, "###Y:"))
			}
		case strings.HasPrefix(satir, "###CPUD:"):
			p := strings.Split(strings.TrimPrefix(satir, "###CPUD:"), "|")
			if len(p) < 4 || len(ud) >= cpanelYAMLElemanSiniri {
				continue
			}
			ad := strings.ToLower(strings.TrimSpace(p[0]))
			kok := strings.TrimSpace(p[1])
			if !reAlanAdi.MatchString(ad) || !gecerliUzakYol(kok) {
				continue
			}
			var boyut int64
			// Sscanf hatası bilinçli yok sayılır: boyut yalnız görüntü değeri.
			_, _ = fmt.Sscanf(strings.TrimSpace(p[3]), "%d", &boyut)
			ud[ad] = cpanelUD{Kok: kok, PHP: normalizePHP(p[2]), Boyut: boyut}
		}
	}
	bitir()
	return sonuc
}

func cpanelHesapKur(hesap, dbler string, yaml []string, ud map[string]cpanelUD) []Hesap {
	m, ok := cpanelMainAyristir(yaml)
	if !ok {
		return nil
	}
	alanGecerli := func(d string) bool { return reAlanAdi.MatchString(d) && strings.Contains(d, ".") }
	if !alanGecerli(m.Ana) {
		return nil
	}
	var out []Hesap
	gorulen := map[string]bool{}

	// Ana alan adı: belge kökü userdata/<ana>; yoksa cPanel varsayılanı.
	ana := Hesap{KaynakHesap: hesap, AlanAdi: m.Ana}
	if u, var_ := ud[m.Ana]; var_ {
		ana.WebRoot, ana.PHPSurum, ana.BoyutMB = u.Kok, u.PHP, u.Boyut
	} else {
		ana.WebRoot = "/home/" + hesap + "/public_html"
	}
	for _, db := range strings.Split(dbler, ",") {
		db = strings.TrimSpace(db)
		if db != "" && reDBAd.MatchString(db) && !sistemDB(db) {
			ana.DBler = append(ana.DBler, db)
		}
	}
	var parklar []string
	for _, p := range m.Parkl {
		if alanGecerli(p) {
			parklar = append(parklar, p)
		}
	}
	if len(parklar) > 0 {
		ana.Not = "park alan adları taşınmaz: " + kisaltAlan(strings.Join(parklar, ", "), 200)
	}
	out = append(out, ana)
	gorulen[m.Ana] = true

	// Ek alan adları: belge kökü, eşlendiği ALT ALAN biçimli userdata dosyasından.
	altEslesen := map[string]bool{}
	ekler := make([]string, 0, len(m.Ekler))
	for e := range m.Ekler {
		ekler = append(ekler, e)
	}
	sortStrings(ekler)
	for _, e := range ekler {
		alt := m.Ekler[e]
		altEslesen[alt] = true
		u, var_ := ud[alt]
		if !alanGecerli(e) || gorulen[e] || !var_ {
			continue
		}
		gorulen[e] = true
		out = append(out, Hesap{KaynakHesap: hesap, AlanAdi: e, WebRoot: u.Kok, PHPSurum: u.PHP,
			BoyutMB: u.Boyut, Not: "ek (addon) alan adı — veritabanı ana alan adıyla taşınır"})
	}
	// Alt alan adları (ek alanın arka plandaki alt alanı HARİÇ): ayrı site olarak.
	for _, s := range m.Altlar {
		u, var_ := ud[s]
		if !alanGecerli(s) || gorulen[s] || altEslesen[s] || !var_ {
			continue
		}
		gorulen[s] = true
		out = append(out, Hesap{KaynakHesap: hesap, AlanAdi: s, WebRoot: u.Kok, PHPSurum: u.PHP,
			BoyutMB: u.Boyut, Not: "alt alan adı — ayrı site olarak açılır"})
	}
	return out
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// uzakEvKomutu — hesabın ev dizinini (getent; yoksa /home/<u>) ###HOME: ile basar.
func uzakEvKomutu(hesap string) string {
	return "u=" + shQuote(hesap) + "\n" +
		`h=$(getent passwd "$u" 2>/dev/null | cut -d: -f6)
[ -n "$h" ] || h="/home/$u"
echo "###HOME:$h"
`
}

// uzakEvAyristir — ###HOME: satırı + kalan satırlar (kutu adları).
func uzakEvAyristir(cikti string) (ev string, adlar []string) {
	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimSpace(satir)
		if satir == "" {
			continue
		}
		if strings.HasPrefix(satir, "###HOME:") {
			ev = strings.TrimSpace(strings.TrimPrefix(satir, "###HOME:"))
			continue
		}
		adlar = append(adlar, strings.ToLower(satir))
	}
	if !gecerliUzakYol(ev) || ev == "/" {
		ev = ""
	}
	return ev, adlar
}

// cpanelMailKesfet — ~/etc/<alan>/passwd'deki kutular. Parolalar kaynakta yalnız
// crypt özeti olarak durur (taşınamaz) → kutulara yeni parola üretilir.
func cpanelMailKesfet(ctx context.Context, k *Kaynak, hs Hesap) ([]mailHesap, error) {
	if !reAlanAdi.MatchString(hs.AlanAdi) || !reHesap.MatchString(hs.KaynakHesap) {
		return nil, fmt.Errorf("gecersiz hesap/alan adi")
	}
	uzak := uzakEvKomutu(hs.KaynakHesap) + "d=" + shQuote(hs.AlanAdi) + "\n" +
		`[ -f "$h/etc/$d/passwd" ] && cut -d: -f1 "$h/etc/$d/passwd" 2>/dev/null | head -n 2000`
	cikti, err := k.Calistir(ctx, uzak)
	if err != nil {
		return nil, err
	}
	ev, adlar := uzakEvAyristir(cikti)
	if ev == "" {
		return nil, fmt.Errorf("kaynak hesabin ev dizini okunamadi")
	}
	alan := hs.AlanAdi
	var out []mailHesap
	gorulen := map[string]bool{}
	for _, y := range adlar {
		if gorulen[y] {
			continue
		}
		gorulen[y] = true
		out = append(out, mailHesap{Yerel: y, uzakYol: func(yerel string) string {
			return ev + "/mail/" + alan + "/" + yerel + "/"
		}})
	}
	return out, nil
}

// cpanelCertOku — /var/cpanel/ssl/apache_tls/<alan>/combined (özel anahtar +
// sertifika zinciri tek dosyada). Bulunamazsa boş döner (ortak tarama denenir).
func cpanelCertOku(ctx context.Context, k *Kaynak, hs Hesap) (string, string, error) {
	if !reAlanAdi.MatchString(hs.AlanAdi) {
		return "", "", fmt.Errorf("gecersiz alan adi")
	}
	uzak := "d=" + shQuote(hs.AlanAdi) + "\n" +
		`f="/var/cpanel/ssl/apache_tls/$d/combined"
[ -f "$f" ] && head -c 262144 "$f"`
	cikti, err := k.Calistir(ctx, uzak)
	if err != nil {
		return "", "", err
	}
	cert, key := pemAyir(cikti)
	return cert, key, nil
}

// pemAyir — birleşik PEM metnini sertifika(lar) ve özel anahtar olarak ayırır.
// Yalnız tanınan blok başlıkları alınır; aradaki her şey atılır.
func pemAyir(s string) (cert, key string) {
	var cb, kb strings.Builder
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for {
		i := strings.Index(s, "-----BEGIN ")
		if i < 0 {
			break
		}
		s = s[i:]
		satirSonu := strings.Index(s, "-----\n")
		if satirSonu < 0 {
			break
		}
		baslik := s[len("-----BEGIN "):satirSonu]
		son := "-----END " + baslik + "-----"
		j := strings.Index(s, son)
		if j < 0 {
			break
		}
		blok := s[:j+len(son)] + "\n"
		s = s[j+len(son):]
		switch {
		case baslik == "CERTIFICATE":
			cb.WriteString(blok)
		case strings.HasSuffix(baslik, "PRIVATE KEY") && kb.Len() == 0:
			kb.WriteString(blok)
		}
	}
	return strings.TrimSpace(cb.String()), strings.TrimSpace(kb.String())
}
