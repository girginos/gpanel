package dns

import (
	"strings"
	"testing"
	"time"
)

// Zone aracı takılırsa zaman aşımıyla hata döner (kilitler süresiz tutulmaz).
func TestKomutCalistir_ZamanAsimi(t *testing.T) {
	eski := komutZamanAsimi
	t.Cleanup(func() { komutZamanAsimi = eski })
	komutZamanAsimi = 200 * time.Millisecond
	bas := time.Now()
	_, err := komutCalistir("sleep", "5")
	if err == nil || !strings.Contains(err.Error(), "sürede bitmedi") {
		t.Fatalf("zaman aşımı hatası beklenirdi: %v", err)
	}
	if d := time.Since(bas); d > 3*time.Second {
		t.Errorf("komut zaman aşımında kesilmedi: %v", d)
	}
	if out, err := komutCalistir("true"); err != nil {
		t.Errorf("hızlı komut başarılı olmalı: %v %s", err, out)
	}
}
