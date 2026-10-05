package main

// rota_ssl_hedef_test.go — hesap SSL hedef listesi ucu: MusteriScope arkasında (hesap
// sahipliği), AdminOnly/rolsüz kayıt yok, API anahtarı izin tablosunda YOK (varsayılan
// reddet), mevcut üç SSL ucu (ana/alt/ek) yerinde.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

func TestSSLHedefRotasi_MusteriScopeVeAnahtaraKapali(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	var n int
	re := regexp.MustCompile(`r\.With\(middleware\.MusteriScope\)\.Get\("/domains/\{id\}/ssl/hedefler", subH\.SSLHedefler\)`)
	for _, s := range strings.Split(src, "\n") {
		if strings.Contains(s, "/ssl/hedefler") {
			n++
			if !re.MatchString(strings.TrimSpace(s)) {
				t.Errorf("hedefler rotası MusteriScope+GET değil: %s", strings.TrimSpace(s))
			}
		}
	}
	if n != 1 {
		t.Fatalf("tek hedefler rotası bekleniyordu: %d", n)
	}
	for k := range apianahtar.Rotalar {
		if strings.Contains(k, "ssl/hedef") {
			t.Errorf("hedef listesi API anahtarına AÇILMAMALI: %s", k)
		}
	}
	// geriye uyum: mevcut üç SSL yolu yerinde
	for _, s := range []string{
		`"/domains/{id}/ssl/issue", domainsH.SSLIssue`,
		`"/domains/{id}/ssl", domainsH.SSLDisable`,
		`"/domains/{id}/subdomain/{sid}/ssl", subH.SSLKur`,
		`"/domains/{id}/subdomain/{sid}/ssl", subH.SSLKaldir`,
		`"/domains/{id}/ek-alan-adlari/{eid}/ssl", subH.EkSSLKur`,
		`"/domains/{id}/ek-alan-adlari/{eid}/ssl", subH.EkSSLKaldir`,
	} {
		if !strings.Contains(src, s) {
			t.Errorf("mevcut SSL ucu kayboldu: %s", s)
		}
	}
}
