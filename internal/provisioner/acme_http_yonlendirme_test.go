package provisioner

import (
	"bytes"
	"strings"
	"testing"
)

// :80 bloğundaki sunucu-seviyesi `if (...) { return 301 }` rewrite aşamasında, location
// seçiminden ÖNCE çalışır → ACME challenge 443'e atılır, uygulama/docroot kendi sayfasını
// döner, Let's Encrypt "reader size limit exceeded" ile reddeder, yenileme sessizce kırılır.
// (test2.girginos.app, 2026-10-05.) Challenge yolu yönlendirmeden muaf olmalı.
func TestVhostSSL_ACMEChallengeHTTPSyeYonlendirilmez(t *testing.T) {
	var buf bytes.Buffer
	opts := VhostOpts{AlanAdi: "acme-yonlendirme.test", WebRoot: "/home/c_x/public_html", SSLKaynak: "letsencrypt",
		CertPath: "/etc/pki/x.crt", KeyPath: "/etc/pki/x.key"}
	if err := vhostTmpl.Execute(&buf, opts); err != nil {
		t.Fatalf("vhost üretilemedi: %v", err)
	}
	v := buf.String()
	i := strings.Index(v, "return 301 https://$host$request_uri;")
	if i < 0 {
		t.Fatalf("http→https yönlendirmesi yok:\n%s", v)
	}
	oncesi := v[:i]
	if !strings.Contains(oncesi, `if ($uri ~ ^/\.well-known/acme-challenge/) {`) ||
		!strings.Contains(oncesi, "set $gosp_https_yonlendir 0;") {
		t.Fatalf("yönlendirmeden önce ACME muafiyeti yok:\n%s", oncesi)
	}
	if strings.Contains(oncesi, "if ($gosp_force_https) {") {
		t.Fatalf("koşulsuz $gosp_force_https yönlendirmesi geri gelmiş")
	}
	if !strings.Contains(v, "if ($gosp_https_yonlendir) {") {
		t.Fatalf("yönlendirme $gosp_https_yonlendir'e bağlı değil")
	}
}

// Heal'in yeni bloğu ile şablonun ürettiği blok AYNI olmalı; yoksa heal'li ve taze vhost'lar ayrışır.
func TestACMEHTTPYonlendirme_HealBlokuSablonlaAyni(t *testing.T) {
	var buf bytes.Buffer
	opts := VhostOpts{AlanAdi: "acme-yonlendirme.test", WebRoot: "/home/c_x/public_html", SSLKaynak: "letsencrypt",
		CertPath: "/etc/pki/x.crt", KeyPath: "/etc/pki/x.key"}
	if err := vhostTmpl.Execute(&buf, opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), acmeHTTPYeniBlok) {
		t.Fatalf("şablon heal'in yeni bloğunu içermiyor:\n%s", buf.String())
	}
	if strings.Contains(acmeHTTPYeniBlok, acmeHTTPEskiBlok) {
		t.Fatal("yeni blok eski kalıbı içeriyor → heal idempotent olmaz")
	}
}
