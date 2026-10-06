package git

// kiraciCalistirGercek: kiraci.Komut yolu (kiracı diliminde runuser) panel
// ortamını kiracı komutuna geçirmemeli; ortam AÇIKÇA asgari verilir.

import (
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

func TestKiraciCalistirGercek_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t, "runuser", "systemd-run")
	if _, e, err := kiraciCalistirGercek(testSK, t.TempDir(), []byte("x"), "true"); err != nil {
		t.Fatalf("beklenmeyen hata: %v (%s)", err, e)
	}
	k.SizintiYok(t, "runuser", "-u", testSK)
	dilimtest.DilimKilidi(t, k, testSK)
}
