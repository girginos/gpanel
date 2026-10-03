package marka

import (
	"strings"
	"testing"
)

// Enjeksiyon kilidi: nginx sözdizimini bozabilecek HİÇBİR girdi üreticilerden
// geçemez (ad DB'den gelse bile — DB'ye elle yazılmış kötü satır dahil).
var kotuAdlar = []string{
	`a.com"`, "a.com;", "a.com }", "$host.com", "a .com", "a.com\nlisten 1", "a.com\r", "a.com#x",
	"../etc.com", "a/b.com", "*.a.com", "~^a\\.com$", "a.com{", "1.2.3.4", "_.a.com", "a..com", "-a.com", "A.COM", "",
	"a.com\x00", "a.com'", "a.com`",
}

func TestNginx_EnjeksiyonImkansiz(t *testing.T) {
	for _, kotu := range kotuAdlar {
		if _, err := SNIHaritasi([]string{"panel.iyi.com", kotu}); err == nil {
			t.Errorf("SNI kötü adı kabul etti: %q", kotu)
		}
		if _, err := AdlarInclude([]string{kotu}); err == nil {
			t.Errorf("include kötü adı kabul etti: %q", kotu)
		}
		if _, err := AcmeVhost80([]string{kotu}, 8443); err == nil {
			t.Errorf(":80 vhost kötü adı kabul etti: %q", kotu)
		}
		if BayiCrt(kotu) != "" || BayiKey(kotu) != "" {
			t.Errorf("sertifika yolu kötü addan üretildi: %q", kotu)
		}
	}
}

func TestNginx_AcmeVhost80(t *testing.T) {
	s, err := AcmeVhost80([]string{"panel.b.com", "panel.a.com"}, 23003)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s, "server {") != 2 {
		t.Fatalf("ad başına bir blok olmalı:\n%s", s)
	}
	if strings.Index(s, "server_name panel.a.com;") > strings.Index(s, "server_name panel.b.com;") {
		t.Error("sıra kararlı (alfabetik) değil")
	}
	if strings.Count(s, "return 301 https://$server_name:23003$request_uri;") != 2 {
		t.Errorf("yönlendirme kapı desenine uymuyor:\n%s", s)
	}
	if !strings.Contains(s, "root "+PanelAcmeWebroot+";") || strings.Contains(s, "$host") {
		t.Errorf("webroot/open-redirect:\n%s", s)
	}
	if b, err := AcmeVhost80(nil, 8443); err != nil || b != "" {
		t.Errorf("boş küme boş çıktı vermeli: %q %v", b, err)
	}
	for _, p := range []int{0, -1, 70000} {
		if _, err := AcmeVhost80([]string{"panel.a.com"}, p); err == nil {
			t.Errorf("geçersiz port kabul edildi: %d", p)
		}
	}
}

func TestNginx_PanelConfGeriAl(t *testing.T) {
	conf := "server {\n    listen 8443 ssl;\n    ssl_certificate /etc/ssl/girginospanel/panel.crt;\n" +
		"    ssl_certificate_key /etc/ssl/girginospanel/panel.key;\n    server_name panel.kok.com;\n" +
		"    location / { proxy_pass http://127.0.0.1:8080; }\n}\n"
	if PanelConfDegiskenli(conf) {
		t.Fatal("statik conf değişkenli sayıldı")
	}
	y, _, err := PanelConfUyarla(conf)
	if err != nil || !PanelConfDegiskenli(y) {
		t.Fatalf("uyarlama: %v", err)
	}
	g, degisti := PanelConfGeriAl(y)
	if !degisti || g != conf {
		t.Fatalf("geri alma özgün conf'u BAYT BAYT vermeli:\n%q\n%q", g, conf)
	}
	if g2, d2 := PanelConfGeriAl(conf); d2 || g2 != conf {
		t.Error("statik conf'ta geri alma no-op olmalı")
	}
}
