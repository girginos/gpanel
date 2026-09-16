package marka

// Webmail (Roundcube) markalama.
//
// NE YAPAR: Elastic skin'inden türeyen bir ALT SKIN üretir (`skins/gosp`) ve
// Roundcube'un yapılandırmasına marka satırlarını ekler. Panelin markası
// değiştiğinde yeniden üretilir.
//
// ── Neden alt skin, neden Elastic'i değiştirmiyoruz ────────────────────────
// /usr/share/roundcubemail RPM'e aittir (roundcubemail-1.6.x). Elastic'in
// dosyalarını düzenlemek, ilk `dnf update roundcubemail`'de sessizce geri
// alınırdı. RPM yalnız KENDİ dosyalarını siler; eklediğimiz `skins/gosp`
// dizini güncellemeden sağ çıkar.
//
// ── Renk dönüşümü neden "hue değiştir, açıklığı koru" ─────────────────────
// Elastic'in renkleri LESS değişkenlerinden derlenmiştir; CSS özel değişkeni
// YOKTUR, yani "bir değişkeni ez" mümkün değil. Derlenmiş CSS ölçüldüğünde:
//
//	81 farklı hex / 531 geçiş. Hue'ya göre "mavi" görünen 417 geçişin BÜYÜK
//	ÇOĞUNLUĞU S=%12-15 doygunlukta, mavi tonlu GRİLERDİR (Elastic'in nötr
//	paleti @color-black: #161b1d'den türer). Gerçek vurgu ailesi yalnız
//	S>=%70 olan 17 hex / 94 geçiş + rgba(55,190,255) biçiminde 22 geçiştir.
//
// 🔴 Hue'ya göre seçseydik arayüzün TÜM GRİLERİ boyanırdı. Bu yüzden ölçüt
// "doygunluk >= %70 VE hue 175-230". Vurgu ailesinin 17 tonunun hepsi S=%100
// ve hue 198-200'dür; yalnız açıklıkları değişir (%26-%96). Dönüşüm bu yüzden
// birebir: AÇIKLIK KORUNUR, hue+doygunluk markanınkiyle değiştirilir. Nötr
// griler ve anlam renkleri (hata/uyarı/başarı) HİÇ ETKİLENMEZ.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	RCKok     = "/usr/share/roundcubemail"
	rcUstSkin = "elastic"
	rcAltSkin = "gosp"

	// rcMarkaConf — config.inc.php'nin SONUNDAN @include ile çağrılır.
	//
	// 🔴 MARKA config.inc.php'YE YAZILMAZ. O dosyayı webmail kurucusu her
	// çalıştığında SIFIRDAN üretiyor (yalnız des_key korunuyor); marka oraya
	// yazılsaydı ilk `repair`/yeniden kurulumda sessizce kaybolurdu. Ayrı
	// dosya + kurucu şablonundaki tek `@include` satırı, iki tarafın da
	// birbirini ezmemesini sağlar.
	rcMarkaConf = "/etc/roundcubemail/gosp-marka.inc.php"
	rcAnaConf   = "/etc/roundcubemail/config.inc.php"

	// rcIncludeSatir — ana yapılandırmanın sonuna eklenen TEK satır.
	// Marker olarak dosya yolunun kendisi kullanılır; ikinci kez eklenmez.
	rcIncludeSatir = "@include('" + rcMarkaConf + "');"

	// rcDamga — üretim imzası. İçerik değişmediyse yeniden üretilmez.
	//
	// 🔴 SKIN DİZİNİNDE DEĞİL: orası webmail docroot'u altında ve dosya
	// herkese açık servis ediliyordu (marka değerleri + kaynak CSS'in
	// boyut/mtime'ı). Marka dizinine alındı — orası HTTP'den erişilemez.
	rcDamga = "webmail-damga.json"
)

func rcSkinDiz() string { return filepath.Join(RCKok, "skins", rcAltSkin) }
func rcUstDiz() string  { return filepath.Join(RCKok, "skins", rcUstSkin) }

// WebmailKurulu — Roundcube var mı. Yoksa tüm işlemler sessizce atlanır:
// mail eklentisi kurulu olmayan bir panelde marka kaydetmek hata VERMEMELİ.
func WebmailKurulu() bool {
	fi, err := os.Stat(filepath.Join(rcUstDiz(), "styles", "styles.min.css"))
	return err == nil && !fi.IsDir()
}

// ── imza ────────────────────────────────────────────────────────────────────

// webmailImza — üretilmiş temanın hangi girdilerden doğduğu.
//
// 🔴 KAYNAK CSS'İN İMZASI DA DAHİL: `dnf update roundcubemail` Elastic'i
// değiştirdiğinde bizim kopyamız BAYAT kalır. Boyut+mtime imzaya girdiği için
// güncelleme sonrası ilk kontrolde tema kendiliğinden yeniden üretilir.
func webmailImza(m Marka) string {
	p := []string{"v1", m.VurguRenk, m.PanelAdi, m.DestekURL, strconv.FormatBool(m.Logo), strconv.FormatInt(m.LogoSurum, 10)}
	for _, ad := range []string{"styles.min.css", "embed.min.css"} {
		if fi, err := os.Stat(filepath.Join(rcUstDiz(), "styles", ad)); err == nil {
			p = append(p, fmt.Sprintf("%s:%d:%d", ad, fi.Size(), fi.ModTime().UnixNano()))
		} else {
			p = append(p, ad+":yok")
		}
	}
	return strings.Join(p, "|")
}

func damgaOku() string {
	b, err := os.ReadFile(filepath.Join(Kok, rcDamga))
	if err != nil {
		return ""
	}
	var d struct {
		Imza string `json:"imza"`
	}
	if json.Unmarshal(b, &d) != nil {
		return ""
	}
	return d.Imza
}

// WebmailGuncelMi — yeniden üretmeye gerek var mı.
//
// 🔴 Periyodik döngü bunu çağırır; imza aynıysa HİÇBİR ŞEY yapılmaz. Her
// turda yeniden üretmek 120 KB'lık dosyaları boşuna yazar ve log'u şişirirdi.
func WebmailGuncelMi(m Marka) bool {
	if !m.WebmailMarka {
		// Kapalıyken "güncel" demek, kaldırma işinin yapılmış olması demektir.
		_, err := os.Stat(rcMarkaConf)
		return os.IsNotExist(err)
	}
	return damgaOku() == webmailImza(m)
}

// ── renk dönüşümü ───────────────────────────────────────────────────────────

var (
	// 8 haneli biçim (#rrggbbaa) de yakalanır: Elastic 1.6.17'de yok ama
	// Roundcube paleti modernleşirse yarım boyanmış vurgular kalmasın.
	reHex = regexp.MustCompile(`#[0-9a-fA-F]{8}\b|#[0-9a-fA-F]{6}\b`)
	reRGB = regexp.MustCompile(`rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})`)
)

// vurguMu — bu renk Elastic'in VURGU ailesinden mi.
//
// Ölçüt ölçümle belirlendi (bkz. dosya başı): doygunluk >= %70 ve hue
// 175-230. Elastic'in mavi tonlu grileri S=%12-15'tir, bu eşiğin çok
// altında kalır ve dokunulmaz.
func vurguMu(r, g, b int) bool {
	h, s, _ := rgbHSLf(r, g, b)
	return s >= 0.70 && h >= 175 && h <= 230
}

// vurguCevir — tonu markanın hue/doygunluğuna taşır ve açıklığı `kaydir`
// kadar öteler. Uçlar kırpılır ki çok açık/koyu bir marka renginde bile
// en açık ton en açık, en koyu ton en koyu kalsın.
func vurguCevir(r, g, b int, markaH, markaS, kaydir float64) (int, int, int) {
	_, _, l := rgbHSLf(r, g, b)
	l += kaydir
	if l < 0.04 {
		l = 0.04
	}
	if l > 0.97 {
		l = 0.97
	}
	return hslRGBf(markaH, markaS, l)
}

func rgbHSLf(r, g, b int) (h, s, l float64) {
	R, G, B := float64(r)/255, float64(g)/255, float64(b)/255
	maks, min := R, R
	for _, v := range []float64{G, B} {
		if v > maks {
			maks = v
		}
		if v < min {
			min = v
		}
	}
	l = (maks + min) / 2
	if maks == min {
		return 0, 0, l
	}
	d := maks - min
	if l > 0.5 {
		s = d / (2 - maks - min)
	} else {
		s = d / (maks + min)
	}
	switch maks {
	case R:
		h = (G - B) / d
		if G < B {
			h += 6
		}
	case G:
		h = (B-R)/d + 2
	default:
		h = (R-G)/d + 4
	}
	return h * 60, s, l
}

func hslRGBf(h, s, l float64) (int, int, int) {
	h = h - 360*float64(int(h/360))
	if h < 0 {
		h += 360
	}
	if s == 0 {
		v := int(l*255 + 0.5)
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	kanal := func(t float64) int {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 1.0/2:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return int(v*255 + 0.5)
	}
	H := h / 360
	return kanal(H + 1.0/3), kanal(H), kanal(H - 1.0/3)
}

// capaBul — kaynak CSS'in ANA vurgu tonunu bulur (en çok geçen vurgu rengi).
//
// 🔴 NEDEN GEREKLİ: yalnız "hue'yu değiştir, açıklığı koru" deseydik, Elastic'in
// ana vurgusu (#37beff, L=%61) markanın rengiyle AYNI OLMAZDI (#7c3aed, L=%58
// → sonuç #8548ee). Yani panelin birincil butonu ile webmail'in birincil butonu
// farklı renkte çıkardı — whitelabel'da bu görünür bir tutarsızlıktır (ölçüldü).
//
// Çapa bulunup tüm ölçek onun açıklık FARKI kadar kaydırılınca ana vurgu marka
// rengine BİREBİR oturur, diğer tonlar da aralarındaki mesafeyi korur.
// Çapa sabit yazılmaz, veriden bulunur: Roundcube paletini değiştirirse
// mekanizma kendiliğinden doğru tonu seçer.
func capaBul(css string) (float64, bool) {
	say := map[string]int{}
	for _, m := range reHex.FindAllString(css, -1) {
		say[strings.ToLower(m)]++
	}
	for _, m := range reRGB.FindAllStringSubmatch(css, -1) {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		say[fmt.Sprintf("#%02x%02x%02x", r, g, b)]++
	}
	enCok, enL, bulundu := 0, 0.0, false
	enHex := ""
	for h, n := range say {
		rgb := hexRGB(h)
		if rgb == nil || !vurguMu(rgb[0], rgb[1], rgb[2]) {
			continue
		}
		// Eşitlikte hex dizisi küçük olan kazanır: map yinelemesi rastgele
		// olduğu için tie-break olmadan aynı girdi farklı tema üretebilirdi.
		if n > enCok || (n == enCok && bulundu && h < enHex) {
			_, _, l := rgbHSLf(rgb[0], rgb[1], rgb[2])
			enCok, enL, enHex, bulundu = n, l, h, true
		}
	}
	return enL, bulundu
}

// cssBoyat — bir CSS metnindeki vurgu renklerini markaya çevirir ve göreli
// varlık yollarını düzeltir. `kaydir`, çapa tonunu marka rengine oturtan
// açıklık farkıdır (bkz. capaBul).
// bagilLum — WCAG bağıl parlaklık. Kontrast hesabının temeli.
func bagilLum(r, g, b int) float64 {
	k := func(v int) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*k(r) + 0.7152*k(g) + 0.0722*k(b)
}

// lumKoru — kaynağın BAĞIL PARLAKLIĞINI koruyarak markanın tonuna taşır.
//
// 🔴 KOYU MOD İÇİN ZORUNLU. Diğer dalda (açıklık koruma) vurgu bir ZEMİN
// olduğu için doğru çalışır. Ama Elastic koyu modda aynı rengi METİN olarak
// kullanıyor: `html.dark-mode .listing li.selected{color:…}` gibi 11 kural.
// HSL açıklığı aynı olsa bile algısal parlaklık tondan tona çok değişir —
// #37beff (açık camgöbeği) ile #7c3aed (koyu mor) aynı L'de, ama koyu zemine
// göre kontrast 7,03'ten 2,60'a düşüyordu (ölçüldü): seçili e-posta konusu
// okunmaz hâle geliyordu. Parlaklık korunursa kontrast KAYNAKLA AYNI kalır.
//
// L üzerinde ikili arama yapılır; parlaklık L'de monoton arttığı için hedefe
// yakınsar. Koyu bir markada sonuç pastel bir tona çıkar — koyu mod vurguları
// zaten böyledir.
func lumKoru(r, g, b int, markaH, markaS float64) (int, int, int) {
	hedef := bagilLum(r, g, b)
	alt, ust := 0.0, 1.0
	for i := 0; i < 24; i++ {
		orta := (alt + ust) / 2
		rr, gg, bb := hslRGBf(markaH, markaS, orta)
		if bagilLum(rr, gg, bb) < hedef {
			alt = orta
		} else {
			ust = orta
		}
	}
	return hslRGBf(markaH, markaS, (alt+ust)/2)
}

// koyuMu — bu kural koyu mod bloğuna mı ait.
func koyuMu(secici string) bool {
	return strings.Contains(strings.ToLower(secici), "dark-mode")
}

// kurallariBoya — CSS'i kural kural gezer ve her bildirim bloğunu, ait olduğu
// SEÇİCİYE göre boyar. Koyu mod kuralları parlaklık koruyan dalı kullanır.
//
// Basit bir tarayıcı yeterli: küçültülmüş CSS'te bildirim blokları iç içe
// süslü parantez içermez; iç içe olabilen @-kuralları (media/supports/keyframes)
// ayrıca ele alınır ve içlerindeki kurallar normal akışta işlenir.
func kurallariBoya(css string, markaH, markaS, kaydir float64) string {
	var out strings.Builder
	out.Grow(len(css) + 256)
	i, n := 0, len(css)
	for i < n {
		j := strings.IndexByte(css[i:], '{')
		if j < 0 {
			out.WriteString(css[i:])
			break
		}
		onek := css[i : i+j]
		out.WriteString(onek)
		out.WriteByte('{')
		i += j + 1

		t := strings.TrimSpace(onek)
		// "}" ile biten bir önekte önek, önceki kuralın kapanışını da taşır;
		// seçici metni yine içinde olduğu için koyuMu doğru çalışır.
		if k := strings.LastIndexByte(t, '}'); k >= 0 {
			t = strings.TrimSpace(t[k+1:])
		}
		if ickiAtKural(t) {
			continue // içindeki kurallar bir sonraki turda işlenir
		}
		k := strings.IndexByte(css[i:], '}')
		if k < 0 {
			out.WriteString(renkleriCevir(css[i:], markaH, markaS, kaydir, koyuMu(onek)))
			break
		}
		out.WriteString(renkleriCevir(css[i:i+k], markaH, markaS, kaydir, koyuMu(onek)))
		out.WriteByte('}')
		i += k + 1
	}
	return out.String()
}

func ickiAtKural(t string) bool {
	tl := strings.ToLower(t)
	for _, on := range []string{"@media", "@supports", "@document", "@layer", "@keyframes", "@-webkit-keyframes", "@-moz-keyframes"} {
		if strings.HasPrefix(tl, on) {
			return true
		}
	}
	return false
}

// renkleriCevir — tek bir bildirim bloğundaki vurgu renklerini çevirir.
func renkleriCevir(govde string, markaH, markaS, kaydir float64, koyu bool) string {
	cevir := func(r, g, b int) (int, int, int) {
		if koyu {
			return lumKoru(r, g, b, markaH, markaS)
		}
		return vurguCevir(r, g, b, markaH, markaS, kaydir)
	}
	govde = reHex.ReplaceAllStringFunc(govde, func(m string) string {
		r, _ := strconv.ParseInt(m[1:3], 16, 32)
		g, _ := strconv.ParseInt(m[3:5], 16, 32)
		b, _ := strconv.ParseInt(m[5:7], 16, 32)
		if !vurguMu(int(r), int(g), int(b)) {
			return m
		}
		nr, ng, nb := cevir(int(r), int(g), int(b))
		if len(m) == 9 { // #rrggbbaa — alfa korunur
			return fmt.Sprintf("#%02x%02x%02x%s", nr, ng, nb, m[7:])
		}
		return fmt.Sprintf("#%02x%02x%02x", nr, ng, nb)
	})
	// 🔴 rgba() DA ÇEVRİLMELİ: yalnız hex aransaydı `rgba(55,190,255,.2)`
	// biçimindeki 22 yarı-saydam vurgu ESKİ RENKTE kalır ve arayüzde iki
	// farklı marka rengi yan yana görünürdü.
	govde = reRGB.ReplaceAllStringFunc(govde, func(m string) string {
		p := reRGB.FindStringSubmatch(m)
		r, _ := strconv.Atoi(p[1])
		g, _ := strconv.Atoi(p[2])
		b, _ := strconv.Atoi(p[3])
		if !vurguMu(r, g, b) {
			return m
		}
		nr, ng, nb := cevir(r, g, b)
		on := "rgb("
		if strings.HasPrefix(m, "rgba") {
			on = "rgba("
		}
		return fmt.Sprintf("%s%d,%d,%d", on, nr, ng, nb)
	})
	return govde
}

func cssBoyat(css string, markaH, markaS, kaydir float64) string {
	css = kurallariBoya(css, markaH, markaS, kaydir)

	// 🔴 GÖRELİ VARLIK YOLLARI MUTLAKA ÇEVRİLMELİ. Kaynak CSS 19 yerde
	// `url("../fonts/...")` diyor; bu, `skins/gosp/styles/` içinden
	// `skins/gosp/fonts/`'a bakar ve o dizin YOKTUR → tüm ikonlar (FontAwesome)
	// ve yazı tipleri kırılırdı. Webmail docroot'u /usr/share/roundcubemail
	// olduğu için mutlak yol geçerlidir.
	for _, alt := range []string{"fonts", "images", "deps"} {
		css = strings.ReplaceAll(css, `url("../`+alt+`/`, `url("/skins/`+rcUstSkin+`/`+alt+`/`)
		css = strings.ReplaceAll(css, `url('../`+alt+`/`, `url('/skins/`+rcUstSkin+`/`+alt+`/`)
		css = strings.ReplaceAll(css, `url(../`+alt+`/`, `url(/skins/`+rcUstSkin+`/`+alt+`/`)
	}
	return css
}

// ── uygulama ────────────────────────────────────────────────────────────────

// WebmailUygula — alt skin + marka yapılandırmasını üretir.
//
// Roundcube kurulu değilse SESSİZCE atlanır (hata değil): mail eklentisi
// olmayan bir panelde marka kaydetmek başarısız OLMAMALI.
func WebmailUygula(m Marka) error {
	if !WebmailKurulu() {
		return nil
	}
	if !m.WebmailMarka {
		return WebmailKaldir()
	}

	rgb := hexRGB(m.VurguRenk)
	if rgb == nil {
		rgb = hexRGB(Varsayilan().VurguRenk)
	}
	markaH, markaS, markaL := rgbHSLf(rgb[0], rgb[1], rgb[2])

	// 🔴 ÇAPA TEK BİR KEZ, ANA DOSYADAN hesaplanır ve HER İKİ CSS'te aynı
	// kaydırma kullanılır. Her dosya kendi çapasını bulsaydı, gövde ile
	// gömülü editör farklı tonlara kayar ve yan yana iki marka rengi görünürdü.
	kaydir := 0.0
	if ana, err := os.ReadFile(filepath.Join(rcUstDiz(), "styles", "styles.min.css")); err == nil {
		if capaL, ok := capaBul(string(ana)); ok {
			kaydir = markaL - capaL
		}
	}

	diz := rcSkinDiz()
	for _, d := range []string{diz, filepath.Join(diz, "styles"), filepath.Join(diz, "images")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	// 1) Stil dosyaları — YALNIZ değiştirdiklerimiz kopyalanır. print.min.css
	// dokunulmadığı için üst skin'den gelir ve oradaki göreli yolları çalışır.
	for _, ad := range []string{"styles.min.css", "embed.min.css"} {
		ham, err := os.ReadFile(filepath.Join(rcUstDiz(), "styles", ad))
		if err != nil {
			return err
		}
		boyali := cssBoyat(string(ham), markaH, markaS, kaydir)
		if err := atomikYaz(filepath.Join(diz, "styles", ad), []byte(boyali), 0o644); err != nil {
			return err
		}
		// .min olmayan adla da yaz: Roundcube devel_mode'da sıkıştırılmamış
		// adı arar; ikisini de vermek çözümleme sırasına bağımlılığı kaldırır.
		sade := strings.Replace(ad, ".min.css", ".css", 1)
		if err := atomikYaz(filepath.Join(diz, "styles", sade), []byte(boyali), 0o644); err != nil {
			return err
		}
	}

	// 2) Logo
	logoVar := false
	if m.Logo {
		if ham, err := os.ReadFile(filepath.Join(Kok, LogoDosya)); err == nil {
			if err := atomikYaz(filepath.Join(diz, "images", "logo.png"), ham, 0o644); err == nil {
				logoVar = true
			}
		}
	}
	if !logoVar {
		_ = os.Remove(filepath.Join(diz, "images", "logo.png"))
	}

	// 3) meta.json — kalıtım
	meta := map[string]any{
		"name":    m.PanelAdi,
		"extends": rcUstSkin,
		"config": map[string]any{
			"supported_layouts":          []string{"widescreen"},
			"jquery_ui_colors_theme":     "bootstrap",
			"embed_css_location":         "/styles/embed.css",
			"editor_css_location":        "/styles/embed.css",
			"dark_mode_support":          true,
			"media_browser_css_location": "none",
			"additional_logo_types":      []string{"dark", "small", "small-dark"},
		},
		"meta": map[string]any{
			"viewport":                      "width=device-width, initial-scale=1.0, shrink-to-fit=no, maximum-scale=1.0",
			"theme-color":                   m.VurguRenk,
			"msapplication-navbutton-color": m.VurguRenk,
		},
	}
	mj, err := json.MarshalIndent(meta, "", "\t")
	if err != nil {
		return err
	}
	if err := atomikYaz(filepath.Join(diz, "meta.json"), mj, 0o644); err != nil {
		return err
	}

	// 4) Roundcube yapılandırması
	if err := markaConfYaz(m, logoVar); err != nil {
		return err
	}
	if err := configIncludeGarantile(); err != nil {
		return err
	}

	// Eski surumlerin skin dizinine biraktigi damgayi temizle (artik Kok'ta).
	_ = os.Remove(filepath.Join(diz, "gosp-damga.json"))
	_ = os.Remove(filepath.Join(diz, rcDamga))

	// 5) Damga
	d, _ := json.MarshalIndent(map[string]string{
		"imza": webmailImza(m),
		"not":  "GirginOSPanel tarafından üretildi. Elle düzenlemeyin; marka değişince yeniden yazılır.",
	}, "", "  ")
	// 🔴 DAMGA `Kok` ALTINA yazilir, skin dizinine DEGIL — okuma da oradan
	// yapiliyor. Ikisi ayrisirsa imza hicbir zaman eslesmez ve tema her
	// turda (30 sn) bosuna yeniden uretilir.
	return atomikYaz(filepath.Join(Kok, rcDamga), d, 0o600)
}

func markaConfYaz(m Marka, logoVar bool) error {
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("// GirginOSPanel Marka (whitelabel) — OTOMATİK ÜRETİLDİ.\n")
	b.WriteString("// config.inc.php'nin sonundaki @include ile yüklenir; webmail kurulumu\n")
	b.WriteString("// tekrarlandığında bu dosya KORUNUR.\n")
	b.WriteString("$config['skin'] = '" + rcAltSkin + "';\n")
	// 🔴 TEMA SEÇİCİ KİLİTLENİR. Aksi halde kullanıcı Ayarlar > Görünüm'den
	// "Elastic"e dönebiliyor; o tema kendi yazar adını ve CC-BY-SA lisans
	// bağlantısını basıyor — whitelabel'ın anlamını ortadan kaldıran bir
	// sızıntı (ölçüldü: her iki tema da listeleniyordu).
	b.WriteString("$config['skins_allowed'] = ['" + rcAltSkin + "'];\n")
	b.WriteString("$config['product_name'] = '" + phpTirnak(m.PanelAdi) + "';\n")
	b.WriteString("$config['support_url'] = '" + phpTirnak(m.DestekURL) + "';\n")
	if logoVar {
		// 🔴 `*[dark]` / `*[small]` / `*[small-dark]` VARYANTLARI YAZILMAZ.
		// Roundcube bu tipleri `abs_url($logo)` ile çözerken skin araması
		// YAPMAZ; şablonun geldiği yolu (skins/elastic) düz önekler ve
		// `skins/elastic/images/logo.png` üretir — o dosya YOKTUR. Sonuç:
		// koyu modda ve telefon düzeninde logo KAYBOLUYORDU (ölçüldü).
		// Stok Elastic'te bu hata yok; varyantları biz ekleyince ortaya
		// çıkıyordu. Yalnız `*` ve `[favicon]` verilir — Roundcube tip
		// istendiğinde `*` girdisini elemeyip null döner ve ana logo kalır.
		//
		// Ayrı koyu/küçük logo desteği istenirse ayrı bir yükleme alanı
		// gerekir; tek logoyu üç yere kopyalamak kullanıcının vermediği bir
		// tasarım kararını uydurmak olurdu.
		b.WriteString("$config['skin_logo'] = [\n")
		b.WriteString("    '*' => '/images/logo.png',\n")
		b.WriteString("    '[favicon]' => '/images/logo.png',\n")
		if m.DestekURL != "" {
			b.WriteString("    '[link]' => '" + phpTirnak(m.DestekURL) + "',\n")
		}
		b.WriteString("];\n")
	} else {
		b.WriteString("$config['skin_logo'] = null;\n")
	}
	// Aynı gerekçe (bkz. atomikYazSahipli): apache okuyamazsa `@include`
	// hatayı YUTAR ve marka sessizce hiç uygulanmaz.
	if err := atomikYazSahipli(rcMarkaConf, []byte(b.String()), 0o640); err != nil {
		return err
	}
	// 🔴 SAHIPLIK ZORUNLU: PHP-FPM `apache` olarak kosar. Dosya root:root
	// 0640 kalirsa apache OKUYAMAZ ve config.inc.php'deki `@include` hatayi
	// YUTAR — tema uretilir, config satiri yerindedir, ama marka HICBIR
	// SEKILDE uygulanmaz ve ortada tek bir hata mesaji bile olmaz.
	// (Olculdu: sayfa sessizce skins/elastic yuklemeye devam ediyordu.)
	//
	// 0640 KORUNUR, 0644 YAPILMAZ: kardes dosya config.inc.php ile ayni
	// duzeyde tutulur; marka dosyasi gizli tasimasa da bu dizinin duzeni
	// "yalniz root ve apache" olmalidir.
	return nil
}

// phpTirnak — tek tırnaklı PHP dizesi için kaçış.
//
// 🔴 Değerler yönetici girdisidir ve ÜRETİLEN PHP KODUNA gömülür. Kaçış
// olmadan tek tırnak içeren bir panel adı ("Ahmet'in Paneli") dosyayı
// bozardı; ters bölü ile birlikte kaçırmak ise kod enjeksiyonuna açık
// kapı olurdu. Alan zaten Normalize'dan geçiyor (kontrol karakteri yok).
func phpTirnak(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// configIncludeGarantile — ana config.inc.php'nin marka dosyasını yüklemesini sağlar.
//
// 🔴 KURUCU ŞABLONUNU DÜZELTMEK YETMEZ. Şablon yalnız BUNDAN SONRAKİ
// kurulumlara işler; ZATEN KURULU sunucularda config.inc.php eski hâliyle
// durur ve marka dosyası hiç yüklenmez — tema üretilir ama webmail onu
// görmez (ölçüldü: sayfa hâlâ skins/elastic yüklüyordu). Bu yüzden satır,
// var olan dosyaya da idempotent olarak eklenir.
//
// Ekleme GÜVENLİDİR: `@include` yok olan dosyada sessizce geçer, yani marka
// kapatılıp gosp-marka.inc.php silinse bile satır zararsız kalır.
func configIncludeGarantile() error {
	ham, err := os.ReadFile(rcAnaConf)
	if err != nil {
		// Webmail kurulu ama config yoksa yapacak bir şey yok; kurulum yazacak.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.Contains(string(ham), rcMarkaConf) {
		return nil
	}
	fi, err := os.Stat(rcAnaConf)
	if err != nil {
		return err
	}
	yeni := string(ham)
	if !strings.HasSuffix(yeni, "\n") {
		yeni += "\n"
	}
	yeni += "\n// GirginOSPanel Marka (whitelabel) — otomatik eklendi.\n" + rcIncludeSatir + "\n"

	// 🔴 İZİN VE SAHİPLİK KORUNMALI: dosya 0640 root:apache'dir ve DB parolası
	// ile des_key'i taşır. Yeniden yazarken izni gevşetmek, kiracı PHP
	// süreçlerine webmail veritabanı parolasını açardı.
	// 🔴 SAHİPLİK RENAME'DEN ÖNCE VERİLİR. Önceki sürüm önce rename edip
	// sonra chown ediyordu: rename yeni inode'u bırakınca dosya bir an
	// root:root oluyor ve chown başarısız olursa (apache grubu yok, süreç
	// arada ölür, SELinux) ORADA KALIYORDU. O hâlde PHP-FPM, DB parolasını
	// taşıyan ana yapılandırmayı okuyamaz ve WEBMAIL KOMPLE ÇÖKER — üstelik
	// döngü 30 saniyede bir aynı hatayı tekrarlar. Geçici dosya doğru
	// sahiplikle hazırlanırsa, chown hatası mevcut dosyayı hiç bozmadan geri
	// döner.
	return atomikYazSahipli(rcAnaConf, []byte(yeni), fi.Mode().Perm())
}

// atomikYazSahipli — geçici dosyayı root:apache yapıp SONRA rename eder.
func atomikYazSahipli(yol string, veri []byte, izin os.FileMode) error {
	gid, err := apacheGID()
	if err != nil {
		return err
	}
	tf, err := os.CreateTemp(filepath.Dir(yol), "."+filepath.Base(yol)+".*.tmp")
	if err != nil {
		return err
	}
	gec := tf.Name()
	temizle := func(e error) error {
		tf.Close()
		os.Remove(gec)
		return e
	}
	if _, err := tf.Write(veri); err != nil {
		return temizle(err)
	}
	if err := tf.Sync(); err != nil {
		return temizle(err)
	}
	if err := tf.Chmod(izin); err != nil {
		return temizle(err)
	}
	if err := tf.Chown(0, gid); err != nil {
		return temizle(err)
	}
	if err := tf.Close(); err != nil {
		os.Remove(gec)
		return err
	}
	if err := os.Rename(gec, yol); err != nil {
		os.Remove(gec)
		return err
	}
	return nil
}

func apacheGID() (int, error) {
	g, err := user.LookupGroup("apache")
	if err != nil {
		return 0, fmt.Errorf("apache grubu bulunamadı (webmail PHP dosyayı okuyamaz): %w", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("apache gid çözümlenemedi: %w", err)
	}
	return gid, nil
}

// WebmailKaldir — markalamayı geri alır: Roundcube kendi Elastic temasına döner.
func WebmailKaldir() error {
	if err := os.Remove(rcMarkaConf); err != nil && !os.IsNotExist(err) {
		return err
	}
	// 🔴 DAMGA DA SILINIR. Skin dizini silinip damga kalsaydi, marka yeniden
	// acildiginda imza eslesir ve tema URETILMEZDI: config `gosp` temasini
	// isaret ederken o tema diskte olmazdi.
	if err := os.Remove(filepath.Join(Kok, rcDamga)); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Alt skin dizini silinir: config artık ona işaret etmiyor, bırakmak
	// "hangi tema aktif" sorusunu belirsizleştirirdi.
	if err := os.RemoveAll(rcSkinDiz()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// atomikYaz — geçici dosya + rename (yarım dosya asla görünmez).
func atomikYaz(yol string, veri []byte, izin os.FileMode) error {
	gec := yol + ".yeni"
	if err := os.WriteFile(gec, veri, izin); err != nil {
		return err
	}
	if err := os.Chmod(gec, izin); err != nil {
		os.Remove(gec)
		return err
	}
	if err := os.Rename(gec, yol); err != nil {
		os.Remove(gec)
		return err
	}
	return nil
}
