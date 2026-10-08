package httpx

// govde_siniri_test.go — kimlik doğrulamalı JSON uçlarında sınırsız gövde okuma yok.
// http.Server.ReadTimeout 30 dk ve genel bir gövde sınırı yok: sınırsız
// json.NewDecoder(r.Body)/io.ReadAll(r.Body) tek bir müşteri isteğiyle paneli
// (tüm kiracıları) belleksiz bırakabilir. Denetlenen paketlerde gövde, okunmadan
// önce http.MaxBytesReader ile sarılmalıdır.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestJSONUclariGovdeyiSinirlar(t *testing.T) {
	paketler := []string{"hostuyg", "uygulama", "laravel", "wordpress", "subdomain", "dns",
		"domainyasak", "oturumbosta", "panelhost"}
	ham := regexp.MustCompile(`NewDecoder\(r\.Body\)|ReadAll\(r\.Body\)`)
	for _, p := range paketler {
		dosyalar, _ := filepath.Glob(filepath.Join("..", p, "*.go"))
		if len(dosyalar) == 0 {
			t.Fatalf("paket bulunamadı: %s", p)
		}
		for _, f := range dosyalar {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			for i, satir := range strings.Split(src, "\n") {
				if !ham.MatchString(satir) || strings.HasPrefix(strings.TrimSpace(satir), "//") {
					continue
				}
				// Aynı dosyada r.Body = http.MaxBytesReader(...) ile önceden sarılmışsa serbest.
				if strings.Contains(src, "r.Body = http.MaxBytesReader(") {
					continue
				}
				t.Errorf("%s:%d: sınırsız gövde okuma: %s", f, i+1, strings.TrimSpace(satir))
			}
		}
	}
}
