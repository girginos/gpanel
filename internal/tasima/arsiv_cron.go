package tasima

// Arşivden (cPanel/DirectAdmin) gelen cron satırlarını hedef hesabın panel cron
// deposuna taşıyan SAF çevirici. Ağ/DB/root gerektirmez; depoya yazan tek adım
// cron.Ekle'dir (cronKur). Ana akış sırası:
//
//  1. hesap açılmadan: cronAta(manifest.Cron, bağlam)  → alan adı → satırlar
//  2. kalem için sk bilinince: cronKur(sk, satırlar, eskiKullanıcı)
//     (= cronCevir + cron.Ekle) ve cronRaporMetni ile kalem uyarısı.
//
// Karar: çevrilemeyen satır SESSİZCE atılmaz; cronRed olarak (neden ile) döner.
//
// 🔴 DÜRÜST SINIR: aşağıdaki "tehlikeli içerik" reddi YALNIZ İKİNCİ KATMANDIR —
// kabuk komutunu bir desen listesiyle tam denetlemek mümkün değildir (değişken
// birleştirme, ../ ile yol kaçışı, yorumlayıcı içinden çağrı, kodlama vb. ile
// atlatılabilir). Asıl yalıtım: görev kiracının kendi uid'iyle, kaynak diliminde
// (cgroup) koşar ve panel sırları çocuk sürecin ortamına verilmez
// (bkz. internal/cron/zamanlayici.go, sir_cocuk_test.go). Bu liste yalnız bariz
// kötüye kullanımı taşıma sırasında erkenden yakalar.

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"

	"girginospanel/internal/cron"
	"girginospanel/internal/tasima/arsiv"
)

const (
	cronMaxSatir   = 4096 // girdi satırı üst sınırı (bayt)
	cronMaxKomut   = 1024 // cron.maxKomut aynası (cron.Ekle de ayrıca doğrular)
	cronMaxGorev   = 100  // alan başına görev sınırı (cron.maxGorev aynası)
	cronKisaUzun   = 120  // rapordaki satır kısaltma sınırı (rune)
	cronMaxRedGos  = 10   // kalem uyarısında gösterilen en çok red
	cronNedenSinir = "görev sınırı (100)"
)

// cronBaglam — alan ataması için arşiv bilgisi.
type cronBaglam struct {
	EskiKullanici string
	AnaAlan       string
	Alanlar       []arsiv.Alan // BelgeKoku ev'e göre göreli
}

// cronRed — taşınamayan satır (kısaltılmış, kontrol karakterleri temizlenmiş).
type cronRed struct {
	Satir string
	Neden string
}

// cronTemizle — rapora konacak metin: kontrol karakteri → boşluk, geçersiz
// UTF-8 → '?', en çok cronKisaUzun rune.
func cronTemizle(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == unicode.ReplacementChar {
			r = '?'
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		if n >= cronKisaUzun {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// cronKontrolVar — NUL/kontrol karakteri (TAB serbest; satır sonu zaten yok).
func cronKontrolVar(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return true
		}
	}
	return false
}

var reCronOrtam = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*(.*)$`)

// cronYolKar — yol parçası olabilecek karakter (önceki/sonraki sınır denetimi).
func cronYolKar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '-'
}

// cronEvOnce — s[i] öncesi, bir yolun BAŞI sayılabilecek sınır mı ('/' ya da
// '~' öncesi değil: ".../home/x" iç içe yol başka şeydir).
func cronEvOnce(s string, i int) bool {
	if i == 0 {
		return true
	}
	c := s[i-1]
	return !(cronYolKar(c) || c == '/' || c == '~')
}

type cronEv struct{ adBas, adSon int }

// cronEvBul — s içindeki "/home/<ad>" geçişleri (ad = en uzun [A-Za-z0-9_.-]).
// "/home/abc" ile "/home/abcd" ayrı adlardır: ad maksimal alınır.
func cronEvBul(s string) []cronEv {
	var out []cronEv
	i := 0
	for {
		j := strings.Index(s[i:], "/home/")
		if j < 0 {
			return out
		}
		j += i
		i = j + 6
		if !cronEvOnce(s, j) {
			continue
		}
		k := j + 6
		for k < len(s) && cronYolKar(s[k]) {
			k++
		}
		out = append(out, cronEv{j + 6, k})
	}
}

var reCronDegisken = regexp.MustCompile(`^(\$(USER|LOGNAME)([^A-Za-z0-9_{}]|$)|\$\{(USER|LOGNAME)\})`)

// cronYolYaz — "/home/<eski>" → "/home/<sk>". Başka ad (ya da çözülemeyen
// "/home/*") → neden döner. $USER/$LOGNAME kiracının kendisine çözülür: serbest.
func cronYolYaz(s, eski, sk string) (string, string) {
	var b strings.Builder
	son := 0
	for _, e := range cronEvBul(s) {
		ad := s[e.adBas:e.adSon]
		switch {
		case ad != "" && ad == sk:
		case ad != "" && ad == eski:
			b.WriteString(s[son:e.adBas])
			b.WriteString(sk)
			son = e.adSon
		case ad == "" && reCronDegisken.MatchString(s[e.adSon:]):
		default:
			return "", "başka hesabın ev dizinine başvuruyor"
		}
	}
	b.WriteString(s[son:])
	return b.String(), ""
}

// ---- alan ataması ----

const cronYolDur = "[^ \t'\"`;|&<>()$*?\\[\\]{}\\\\,]*"

func cronBelgeYollari(satir, eski string) []string {
	oneki := "(?:\\$HOME|\\$\\{HOME\\}|~"
	if eski != "" {
		oneki += "|/home/" + regexp.QuoteMeta(eski)
	}
	oneki += ")"
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9_.\-/~])` + oneki + `(/` + cronYolDur + `)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(satir, -1) {
		p := path.Clean(m[1])
		p = strings.Trim(p, "/")
		if p == "" || p == ".." || strings.HasPrefix(p, "../") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// cronAlanBul — rel yoluna en uzun belge-kökü önekiyle uyan alan ("" = yok).
func cronAlanBul(rel string, alanlar []arsiv.Alan) string {
	en, ad, ana := -1, "", false
	for _, a := range alanlar {
		k := strings.Trim(path.Clean("/"+a.BelgeKoku), "/")
		if a.BelgeKoku == "" || k == "" {
			continue
		}
		if rel != k && !strings.HasPrefix(rel, k+"/") {
			continue
		}
		if len(k) > en || (len(k) == en && !ana && a.Tur == "ana") {
			en, ad, ana = len(k), a.Ad, a.Tur == "ana"
		}
	}
	return ad
}

// cronAta — ham crontab satırlarını alan adlarına dağıtır. Sonuçtaki her alanın
// listesi KENDİNE YETER: o alana ait görev satırları, önlerinde o ana kadar
// görülmüş ortam (KEY=VALUE) satırlarıyla (sırayla, her ortam satırı alana bir
// kez) araya serpiştirilmiş gelir; doğrudan cronCevir'e verilebilir. Ortam
// satırlarının geçerliliğini cronCevir denetler/raporlar (aynı kötü ortam satırı
// birden çok alanın listesinde geçerse her birinde raporlanır).
// Yorum ve boş satırlar sessizce atlanır (manifest zaten içermez).
func cronAta(satirlar []string, b cronBaglam) (map[string][]string, []cronRed) {
	out := map[string][]string{}
	var redler []cronRed
	var ortam []string
	verildi := map[string]int{}
	red := func(s, n string) { redler = append(redler, cronRed{cronTemizle(s), n}) }
	for _, ham := range satirlar {
		s := strings.TrimSpace(ham)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if len(ham) > cronMaxSatir {
			red(s, "satır çok uzun")
			continue
		}
		if reCronOrtam.MatchString(s) {
			ortam = append(ortam, s)
			continue
		}
		alan := ""
		coklu := false
		for _, rel := range cronBelgeYollari(s, b.EskiKullanici) {
			a := cronAlanBul(rel, b.Alanlar)
			if a == "" {
				continue
			}
			if alan != "" && a != alan {
				coklu = true
				break
			}
			alan = a
		}
		if coklu {
			red(s, "taşınamadı: birden çok alan adının dizinine başvuruyor")
			continue
		}
		if alan == "" {
			alan = b.AnaAlan
		}
		if alan == "" {
			red(s, "alan adı belirlenemedi")
			continue
		}
		out[alan] = append(out[alan], ortam[verildi[alan]:]...)
		verildi[alan] = len(ortam)
		out[alan] = append(out[alan], s)
	}
	return out, redler
}

// ---- tehlikeli içerik (İKİNCİ KATMAN; bkz. dosya başı) ----

const (
	cronKomutOnce  = `(?:^|[^A-Za-z0-9_.\-/])(?:/(?:usr/(?:local/)?)?s?bin/)?`
	cronKomutSonra = `(?:[^A-Za-z0-9_.\-/]|$)`
	cronKabuk      = `(?:ba|z|da|k)?sh`
)

type cronYasak struct {
	re    *regexp.Regexp
	neden string
}

var cronYasaklar = func() []cronYasak {
	var l []cronYasak
	yol := func(p, sonra string) {
		l = append(l, cronYasak{regexp.MustCompile(`(?:^|[^A-Za-z0-9_.\-/~])` + regexp.QuoteMeta(p) + sonra), "yasak yola başvuruyor: " + p})
	}
	for _, p := range []string{"/opt/girginospanel", "/etc/girginospanel", "/var/lib/girginospanel",
		"/var/backups/girginospanel", "/var/vmail", "/etc/cron", "/var/spool/cron",
		"/etc/passwd", "/etc/shadow", "/etc/sudoers", "/proc/", "/dev/tcp", "/dev/udp"} {
		yol(p, "")
	}
	yol("/root", `(?:[^A-Za-z0-9_.\-]|$)`)
	kelime := func(w string) {
		l = append(l, cronYasak{regexp.MustCompile(cronKomutOnce + w + cronKomutSonra), "yasak komut: " + w})
	}
	for _, w := range []string{"sudo", "su", "systemctl", "nsenter", "docker", "mkfifo", "crontab", "ncat"} {
		kelime(w)
	}
	ek := func(re, neden string) { l = append(l, cronYasak{regexp.MustCompile(re), neden}) }
	ek(cronKomutOnce+`(?:nc|netcat)\s+(?:[^;&|]*\s)?-[A-Za-z]*[ec][A-Za-z]*(?:\s|$)`, "yasak komut: nc -e (uzak kabuk)")
	ek(cronKomutOnce+`socat\s[^;&|]*(?i:exec|system):`, "yasak komut: socat exec")
	ek(cronKomutOnce+cronKabuk+`\s+(?:--?[A-Za-z-]+\s+)*-[A-Za-z]*i[A-Za-z]*(?:\s|$)`, "yasak komut: etkileşimli kabuk (sh -i)")
	ek(cronKomutOnce+`(?:curl|wget)\s.*\|\s*(?:sudo\s+)?(?:/(?:usr/)?s?bin/)?`+cronKabuk+cronKomutSonra, "indirip kabuğa borulama (curl|wget | sh)")
	ek(cronKomutOnce+cronKabuk+`\s+-c\s+["']?(?:\$\(|\x60)\s*(?:curl|wget)`, "indirip kabukta çalıştırma (sh -c $(curl))")
	ek(cronKomutOnce+`base64\s+(?:-d|-D|--decode)(?:\s|$).*\|\s*(?:sudo\s+)?(?:/(?:usr/)?s?bin/)?`+cronKabuk+cronKomutSonra, "base64 çözüp kabuğa borulama")
	return l
}()

// cronTehlikeli — içerik reddi nedeni ("" = temiz).
func cronTehlikeli(s string) string {
	if cronKontrolVar(s) {
		return "kontrol karakteri içeriyor"
	}
	for _, y := range cronYasaklar {
		if y.re.MatchString(s) {
			return "güvenlik: " + y.neden
		}
	}
	return ""
}

// ---- çeviri ----

var cronKisayol = map[string][5]string{
	"@hourly":   {"0", "*", "*", "*", "*"},
	"@daily":    {"0", "0", "*", "*", "*"},
	"@midnight": {"0", "0", "*", "*", "*"},
	"@weekly":   {"0", "0", "*", "*", "0"},
	"@monthly":  {"0", "0", "1", "*", "*"},
	"@yearly":   {"0", "0", "1", "1", "*"},
	"@annually": {"0", "0", "1", "1", "*"},
}

// cronYasakOrtam — kiracı ortamını/kabuğu bozabilecek değişkenler.
var cronYasakOrtam = map[string]bool{
	"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "LD_AUDIT": true, "BASH_ENV": true,
	"ENV": true, "IFS": true, "SHELLOPTS": true, "BASHOPTS": true, "PS4": true,
	"PROMPT_COMMAND": true, "HOME": true, "USER": true, "LOGNAME": true,
	"CDPATH": true, "GLOBIGNORE": true,
}

var reCronOrtamDeger = regexp.MustCompile(`^[A-Za-z0-9_@+=:,./-]*$`)

// cronAlanAl — s'den n boşlukla ayrılmış alan + kalan metin.
func cronAlanAl(s string, n int) ([]string, string, bool) {
	f := make([]string, 0, n)
	rest := s
	for len(f) < n {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return nil, "", false
		}
		i := strings.IndexAny(rest, " \t")
		if i < 0 {
			f, rest = append(f, rest), ""
			continue
		}
		f, rest = append(f, rest[:i]), rest[i:]
	}
	return f, strings.TrimSpace(rest), true
}

func cronTirnakAc(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// cronCevir — bir alanın (cronAta çıktısı) satırlarını hedef hesap için
// cron.Gorev'lere çevirir. Ortam satırları sonraki görevlere crontab
// semantiğiyle uygulanır (aynı ad tekrar gelirse son değer). Dönüş: görevler,
// taşınamayanlar (neden ile), kullanıcıya gösterilecek notlar (red değil).
func cronCevir(satirlar []string, eskiKullanici, sk string) ([]cron.Gorev, []cronRed, []string) {
	var gorevler []cron.Gorev
	var redler []cronRed
	var notlar []string
	notVar := map[string]bool{}
	not := func(s string) {
		if !notVar[s] {
			notVar[s] = true
			notlar = append(notlar, s)
		}
	}
	type ort struct{ ad, deger string }
	var ortam []ort
	bildirim := "bilgi"
	red := func(s, n string) { redler = append(redler, cronRed{cronTemizle(s), n}) }

	for _, ham := range satirlar {
		s := strings.TrimSpace(ham)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if len(ham) > cronMaxSatir {
			red(s, "satır çok uzun")
			continue
		}
		if cronKontrolVar(s) {
			red(s, "kontrol karakteri içeriyor")
			continue
		}
		if m := reCronOrtam.FindStringSubmatch(s); m != nil {
			ad, deger := m[1], cronTirnakAc(m[2])
			switch {
			case ad == "MAILTO":
				if deger == "" {
					bildirim = "yok"
				} else {
					bildirim = "bilgi"
					not("Cron: MAILTO=" + cronTemizle(deger) + " panelde karşılığı yok (bildirim yalnız etiket)")
				}
			case ad == "SHELL":
				switch deger {
				case "/bin/sh":
				case "/bin/bash":
					not("Cron: SHELL=/bin/bash — komutlar /bin/sh ile çalışır")
				default:
					not("Cron: SHELL=" + cronTemizle(deger) + " desteklenmiyor — komutlar /bin/sh ile çalışır")
				}
			case cronYasakOrtam[ad]:
				red(s, "güvensiz ortam değişkeni")
			default:
				yeni, neden := cronYolYaz(deger, eskiKullanici, sk)
				if neden == "" && !reCronOrtamDeger.MatchString(yeni) {
					neden = "ortam değeri güvenli değil"
				}
				if neden == "" {
					neden = cronTehlikeli(yeni)
				}
				if neden != "" {
					red(s, neden)
					continue
				}
				ayarlandi := false
				for i := range ortam {
					if ortam[i].ad == ad {
						ortam[i].deger, ayarlandi = yeni, true
					}
				}
				if !ayarlandi {
					ortam = append(ortam, ort{ad, yeni})
				}
			}
			continue
		}

		// görev satırı
		var z [5]string
		var kalan string
		if strings.HasPrefix(s, "@") {
			tok := s
			if i := strings.IndexAny(s, " \t"); i >= 0 {
				tok, kalan = s[:i], strings.TrimSpace(s[i:])
			}
			if tok == "@reboot" {
				red(s, "@reboot panel zamanlayıcısında desteklenmez")
				continue
			}
			k, ok := cronKisayol[tok]
			if !ok {
				red(s, "desteklenmeyen zamanlama")
				continue
			}
			z = k
		} else {
			f, k, ok := cronAlanAl(s, 5)
			if !ok {
				red(s, "geçersiz zaman ifadesi")
				continue
			}
			copy(z[:], f)
			kalan = k
			gecerli := true
			for _, a := range z {
				gecerli = gecerli && cron.ZamanGecerli(a)
			}
			if !gecerli {
				red(s, "geçersiz zaman ifadesi")
				continue
			}
		}
		if kalan == "" {
			red(s, "komut boş")
			continue
		}
		komut, neden := cronYolYaz(kalan, eskiKullanici, sk)
		if neden == "" {
			neden = cronTehlikeli(komut)
		}
		if neden != "" {
			red(s, neden)
			continue
		}
		var pre strings.Builder
		for _, o := range ortam {
			fmt.Fprintf(&pre, "export %s='%s'; ", o.ad, o.deger)
		}
		komut = pre.String() + komut
		if len(komut) > cronMaxKomut {
			red(s, "komut çok uzun")
			continue
		}
		if len(gorevler) >= cronMaxGorev {
			red(s, cronNedenSinir)
			continue
		}
		gorevler = append(gorevler, cron.Gorev{
			Dakika: z[0], Saat: z[1], Gun: z[2], Ay: z[3], Hafta: z[4],
			Komut: komut, Etkin: true, Tip: "komut", Bildirim: bildirim,
		})
	}
	return gorevler, redler, notlar
}

// cronEkle — test dikişi (üretimde cron.Ekle).
var cronEkle = cron.Ekle

// cronKur — cronCevir + cron.Ekle; Ekle'nin redlerini de satır metniyle
// cronRed'e çevirir. Hesap askıdaysa (cron.ErrAskida) hata döner, redler
// hesaplanmış çeviri redleriyle birlikte yine de verilir.
func cronKur(sk string, satirlar []string, eskiKullanici string) (eklenen, yinelenen int, redler []cronRed, notlar []string, err error) {
	gorevler, redler, notlar := cronCevir(satirlar, eskiKullanici, sk)
	if len(gorevler) == 0 {
		return 0, 0, redler, notlar, nil
	}
	s, err := cronEkle(sk, gorevler)
	for _, r := range s.Reddedilen {
		satir := ""
		if r.Idx >= 0 && r.Idx < len(gorevler) {
			g := gorevler[r.Idx]
			satir = g.Dakika + " " + g.Saat + " " + g.Gun + " " + g.Ay + " " + g.Hafta + " " + g.Komut
		}
		redler = append(redler, cronRed{cronTemizle(satir), r.Neden})
	}
	return s.Eklenen, s.Yinelenen, redler, notlar, err
}

// cronRaporMetni — kalem uyarısına konacak kısa Türkçe satırlar.
func cronRaporMetni(eklenen, yinelenen int, redler []cronRed) []string {
	if eklenen == 0 && yinelenen == 0 && len(redler) == 0 {
		return nil
	}
	out := []string{fmt.Sprintf("Cron: %d satır taşındı, %d zaten vardı", eklenen, yinelenen)}
	for i, r := range redler {
		if i >= cronMaxRedGos {
			out = append(out, fmt.Sprintf("Cron: … %d satır daha taşınamadı", len(redler)-cronMaxRedGos))
			break
		}
		out = append(out, fmt.Sprintf("Cron taşınamadı: \"%s\" — %s", cronTemizle(r.Satir), r.Neden))
	}
	return out
}
