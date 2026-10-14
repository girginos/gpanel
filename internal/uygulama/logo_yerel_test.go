package uygulama

import (
	"os"
	"regexp"
	"testing"
)

// Katalog logoları panelle gelir (dış CDN yok, CSP img-src 'self'): her LogoURL
// "/simge/<ad>.svg" biçiminde olmalı ve dosya frontend/public/simge'de bulunmalı.
func TestKatalogLogolariPanelde(t *testing.T) {
	bicim := regexp.MustCompile(`^/simge/[a-z0-9-]+\.svg$`)
	n := 0
	for _, k := range Katalog {
		if k.LogoURL == "" {
			continue
		}
		n++
		if !bicim.MatchString(k.LogoURL) {
			t.Errorf("%s: LogoURL %q panel dışı ya da biçimsiz (beklenen /simge/<ad>.svg)", k.Kod, k.LogoURL)
			continue
		}
		if _, err := os.Stat("../../frontend/public" + k.LogoURL); err != nil {
			t.Errorf("%s: logo dosyası yok: %v", k.Kod, err)
		}
	}
	if n == 0 {
		t.Fatal("hiç logo yok — test boş geçmesin")
	}
}
