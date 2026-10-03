package sitekopya

// Kopyalama zinciri (test/du/mkdir/rsync/restorecon) kiracı kimliğinde koşar:
// panel sırları hiçbir adıma geçmemeli.

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestKopyaOlustur_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	eskiKok, eskiKul := evKoku, osKullanici
	evKoku = t.TempDir()
	osKullanici = func(string) string { return "nobody" }
	t.Cleanup(func() { evKoku, osKullanici = eskiKok, eskiKul })

	if _, err := kopyaOlustur(context.Background(), testSK, testZaman); err != nil {
		t.Fatal(err)
	}
	cs := k.SizintiYok(t, "runuser", "-u", "nobody", "--")
	gorulen := map[string]bool{}
	for _, c := range cs {
		if len(c.Argv) > 3 {
			gorulen[c.Argv[3]] = true
		}
	}
	for _, a := range []string{"test", "du", "mkdir", "rsync", "restorecon"} {
		if !gorulen[a] {
			t.Errorf("%s kiracı çağrısı görülmedi (görülen: %v)", a, gorulen)
		}
	}
}
