//go:build windows

// havuz_windows_test.go — B-15: havuzAdiRe/havuzAdiDogrula birim testi. Havuz adi
// appcmd'ye gecer; yalniz panelin urettigi gp_ havuzlari kabul edilmeli, sistem
// havuzlari (DefaultAppPool) ve enjeksiyon karakterleri REDDEDILMELI.
package platform

import (
	"errors"
	"strings"
	"testing"
)

func TestHavuzAdiDogrula(t *testing.T) {
	gecerli := []string{"gp_a12345678", "gp_x1", "gp_musteri0abc1234", "gp_b08testg20791e00"}
	gecersiz := []string{
		"", "DefaultAppPool", "gp_", "GP_x1", "gp_x-1", "gp_x 1",
		"gp_x;stop", "gp_\"x", "havuz_gp_x", "gp_" + strings.Repeat("a", 21),
	}
	for _, s := range gecerli {
		if _, err := havuzAdiDogrula(s); err != nil {
			t.Errorf("gecerli havuz reddedildi: %q (%v)", s, err)
		}
	}
	for _, s := range gecersiz {
		_, err := havuzAdiDogrula(s)
		if err == nil {
			t.Errorf("gecersiz havuz KABUL edildi (guvenlik): %q", s)
			continue
		}
		if !errors.Is(err, ErrGecersizIstek) {
			t.Errorf("%q icin ErrGecersizIstek bekleniyordu: %v", s, err)
		}
	}
	// kirpma: bosluklu gecerli ad kirpilip kabul edilmeli.
	if ad, err := havuzAdiDogrula("  gp_x1  "); err != nil || ad != "gp_x1" {
		t.Errorf("kirpma yanlis: ad=%q err=%v", ad, err)
	}
}
