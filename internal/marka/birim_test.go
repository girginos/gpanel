package marka

// birim_test.go — DB'siz birim testleri: doğrulayıcılar, görsel güvenliği,
// genel yanıt izin listesi, nginx saf üreticileri, emekli yol kapısı.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestEpostaTelefonGecerli(t *testing.T) {
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"", true}, {"destek@ornek.com", true}, {"a.b+c@alt.ornek.com.tr", true},
		{"a@b", false}, {"a@@b.com", false}, {"a b@c.com", false},
		{"a@b.com?cc=x@y.com", false}, {"a@b.com&body=x", false}, {"a@b.com\r\nBcc:x", false},
		{"@b.com", false}, {"a@.b.com", false}, {"a@b..com", false}, {"ç@b.com", false},
	} {
		if g := EpostaGecerli(c.s); g != c.ok {
			t.Errorf("EpostaGecerli(%q)=%v, beklenen %v", c.s, g, c.ok)
		}
	}
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"", true}, {"+90 (212) 555-1234", true}, {"05551234567", true},
		{"123", false}, {"+90 555 abc", false}, {"90+555123", false}, {"1234567890123456789012", false},
	} {
		if g := TelefonGecerli(c.s); g != c.ok {
			t.Errorf("TelefonGecerli(%q)=%v, beklenen %v", c.s, g, c.ok)
		}
	}
}

func TestNormalize_YeniAlanlarVeBIDI(t *testing.T) {
	m := Normalize(Marka{PanelAdi: "Nova‮Lenap", VurguRenk: "red;x", DestekEposta: "x@y.com?cc=z",
		DestekTelefon: "+90 555 123 45 67", DestekURL: "javascript:alert(1)"})
	if strings.ContainsRune(m.PanelAdi, '‮') {
		t.Errorf("BIDI karakteri atılmadı: %q", m.PanelAdi)
	}
	if m.VurguRenk != Varsayilan().VurguRenk {
		t.Errorf("geçersiz renk varsayılana düşmeli: %q", m.VurguRenk)
	}
	if m.DestekEposta != "" || m.DestekURL != "" {
		t.Errorf("geçersiz e-posta/URL OKURKEN boşaltılmalı: %q %q", m.DestekEposta, m.DestekURL)
	}
	if m.DestekTelefon != "+90 555 123 45 67" {
		t.Errorf("geçerli telefon korunmalı: %q", m.DestekTelefon)
	}
}

func pngYap(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestGorselDogrula(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, _, err := GorselDogrula(svg); err != ErrGorselTip {
		t.Errorf("SVG reddedilmeli: %v", err)
	}
	if _, _, err := GorselDogrula([]byte("<html><script>alert(1)</script>")); err != ErrGorselTip {
		t.Errorf("HTML reddedilmeli: %v", err)
	}
	if _, _, err := GorselDogrula(nil); err != ErrGorselBos {
		t.Errorf("boş dosya: %v", err)
	}
	if _, _, err := GorselDogrula(make([]byte, LogoMaks+1)); err != ErrGorselBuyuk {
		t.Errorf("512 KB + 1: %v", err)
	}
	// PNG sihri + bozuk gövde → reddedilir.
	if _, _, err := GorselDogrula(append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("<script>")...)); err != ErrGorselBozuk {
		t.Errorf("sahte PNG: %v", err)
	}
	// Geçerli PNG + HTML kuyruğu (poliglot) → kabul, ama YENİDEN KODLANMIŞ: kuyruk yok.
	p := pngYap(t, 8, 8)
	poli := append(append([]byte{}, p...), []byte("<html><script>alert(1)</script></html>")...)
	out, tip, err := GorselDogrula(poli)
	if err != nil || tip != "image/png" {
		t.Fatalf("geçerli PNG reddedildi: %v %q", err, tip)
	}
	if bytes.Contains(out, []byte("<script>")) {
		t.Errorf("poliglot kuyruğu yeniden kodlamada atılmadı")
	}
	// Boyut sınırı: 5000×1 → red.
	if _, _, err := GorselDogrula(pngYap(t, 5000, 1)); err != ErrGorselBozuk {
		t.Errorf("4096 px üstü reddedilmeli: %v", err)
	}
	// ICO: girdi sayısı/ofset tutarlılığı.
	ico := make([]byte, 6+16+4)
	binary.LittleEndian.PutUint16(ico[2:], 1)
	binary.LittleEndian.PutUint16(ico[4:], 1)
	binary.LittleEndian.PutUint32(ico[6+8:], 4)
	binary.LittleEndian.PutUint32(ico[6+12:], 22)
	if _, tip, err := GorselDogrula(ico); err != nil || tip != "image/x-icon" {
		t.Errorf("geçerli ICO başlığı: %v %q", err, tip)
	}
	binary.LittleEndian.PutUint32(ico[6+12:], 9999)
	if _, _, err := GorselDogrula(ico); err != ErrGorselBozuk {
		t.Errorf("taşan ICO ofseti reddedilmeli: %v", err)
	}
}

// Kimliksiz yanıtın anahtar kümesi: izin listesi DIŞINDA alan yok.
func TestGenelGorunum_IzinListesi(t *testing.T) {
	m := Varsayilan()
	m.WebmailMarka = true
	ham, _ := json.Marshal(genelYap(m, "abcdefghijklmnopqrst"))
	var harita map[string]any
	_ = json.Unmarshal(ham, &harita)
	for _, yasak := range []string{"reseller_id", "rid", "id", "izin", "durum", "webmail_marka", "kok_kapatti", "alanadi_izni", "surum", "kayitli"} {
		if _, var_ := harita[yasak]; var_ {
			t.Errorf("kimliksiz yanıtta yasak alan %q var: %s", yasak, ham)
		}
	}
	for _, gerek := range []string{"panel_adi", "anahtar", "destek_eposta", "destek_telefon", "logo", "vurgu_renk"} {
		if _, var_ := harita[gerek]; !var_ {
			t.Errorf("kimliksiz yanıtta %q eksik: %s", gerek, ham)
		}
	}
}

func TestAnahtarVeYol(t *testing.T) {
	a, err := YeniAnahtar()
	if err != nil || !AnahtarDeseni.MatchString(a) {
		t.Fatalf("YeniAnahtar deseni tutmuyor: %q %v", a, err)
	}
	for _, kotu := range []string{"../../etc/passwd", "kok", "ABCDEFGHIJKLMNOPQRST", "abcdefghijklmnopqrs", "abcdefghijklmnopqrs1", "abcdefghij/lmnopqrst"} {
		if BayiDizin(kotu) != "" {
			t.Errorf("BayiDizin(%q) boş olmalı", kotu)
		}
	}
	if _, ok := YuvaBul("../logo"); ok {
		t.Error("yuva adında yol kaçışı kabul edildi")
	}
}

func TestHostNormalize(t *testing.T) {
	for g, b := range map[string]string{
		"Panel.Bayi.COM:2083": "panel.bayi.com", "panel.bayi.com.": "panel.bayi.com",
		"1.2.3.4": "", "1.2.3.4:443": "", "localhost": "", "a b.com": "", "x.com;y": "", "": "",
	} {
		if s := HostNormalize(g); s != b {
			t.Errorf("HostNormalize(%q)=%q, beklenen %q", g, s, b)
		}
	}
}

func TestNginx_SNIveInclude(t *testing.T) {
	s, err := SNIHaritasi([]string{"panel.b.com", "panel.a.com", "panel.a.com"})
	if err != nil {
		t.Fatal(err)
	}
	beklenen := "# GirginOSPanel — bayi özel panel adları (OTOMATİK ÜRETİLDİ, elle düzenlemeyin)\n" +
		"map $ssl_server_name $gp_panel_crt {\n    default /etc/ssl/girginospanel/panel.crt;\n" +
		"    panel.a.com /etc/ssl/girginospanel/bayi/panel.a.com.crt;\n    panel.b.com /etc/ssl/girginospanel/bayi/panel.b.com.crt;\n}\n" +
		"map $ssl_server_name $gp_panel_key {\n    default /etc/ssl/girginospanel/panel.key;\n" +
		"    panel.a.com /etc/ssl/girginospanel/bayi/panel.a.com.key;\n    panel.b.com /etc/ssl/girginospanel/bayi/panel.b.com.key;\n}\n"
	if s != beklenen {
		t.Errorf("SNI haritası altın çıktıdan farklı:\n%s", s)
	}
	inc, _ := AdlarInclude([]string{"panel.b.com", "panel.a.com"})
	if inc != "server_name panel.a.com panel.b.com;\n" {
		t.Errorf("include: %q", inc)
	}
	if bos, _ := AdlarInclude(nil); strings.Contains(bos, "server_name") {
		t.Errorf("boş include server_name içermemeli: %q", bos)
	}
	for _, kotu := range []string{`a.com"`, "a.com;", "a.com }", "$host.com", "a .com"} {
		if _, err := SNIHaritasi([]string{kotu}); err == nil {
			t.Errorf("kötü ad kabul edildi: %q", kotu)
		}
		if _, err := AdlarInclude([]string{kotu}); err == nil {
			t.Errorf("kötü ad include'a girdi: %q", kotu)
		}
	}
}

func TestNginx_PanelConfUyarla_Idempotent(t *testing.T) {
	conf := "server {\n    listen 8443 ssl;\n    server_name panel.kok.com 1.2.3.4;\n" +
		"    ssl_certificate /etc/ssl/girginospanel/panel.crt;\n    ssl_certificate_key /etc/ssl/girginospanel/panel.key;\n" +
		"    location / { proxy_set_header Host $host; }\n}\n"
	y1, d1, err := PanelConfUyarla(conf)
	if err != nil || !d1 {
		t.Fatalf("ilk uyarlama değişmeli: %v %v", d1, err)
	}
	if !strings.Contains(y1, "    server_name panel.kok.com 1.2.3.4;\n    include /etc/nginx/gosp/marka-adlari.inc;\n") {
		t.Errorf("include ilk server_name altına girmedi:\n%s", y1)
	}
	if !strings.Contains(y1, "ssl_certificate $gp_panel_crt;") || !strings.Contains(y1, "ssl_certificate_key $gp_panel_key;") {
		t.Errorf("sertifika satırları değişkene çevrilmedi:\n%s", y1)
	}
	if !strings.Contains(y1, "proxy_set_header Host $host;") {
		t.Error("öteki satırlar bozuldu")
	}
	y2, d2, err := PanelConfUyarla(y1)
	if err != nil || d2 || y2 != y1 {
		t.Errorf("ikinci uyarlama aynı çıktıyı vermeli (idempotent): değişti=%v", d2)
	}
	if strings.Count(y2, "marka-adlari.inc") != 1 {
		t.Error("include iki kez yazıldı")
	}
}

func TestEmekliYolMu(t *testing.T) {
	for p, b := range map[string]bool{
		"/api/v1/eklenti/whitelabel/ayarlar":        true,
		"/api/v1/eklenti/whitelabel/lisans":         true,
		"/api/v1/eklenti/whitelabel":                true,
		"/api/v1/eklenti-bundle/whitelabel/app.js":  true,
		"/api/v1/eklenti/mail/ayarlar":              false,
		"/api/v1/eklenti/whitelabelx/a":             false,
		"/api/v1/eklenti-bundle/calistirici/app.js": false,
	} {
		if g := emekliYolMu(p); g != b {
			t.Errorf("emekliYolMu(%q)=%v, beklenen %v", p, g, b)
		}
	}
}
