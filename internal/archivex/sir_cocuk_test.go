package archivex

// Arşiv çıkarıcılar (tar/unzip/bsdtar...) kiracı kimliğinde koşar; kiracının
// arşivi/araç davranışı üzerinden /proc/<pid>/environ kiracı uid'sine açıktır →
// panel sırları çocuğa geçmemeli (sessiz ve ilerlemeli iki yol).

import (
	"os"
	"path/filepath"
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

func TestKiraciCikar_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	dir := t.TempDir()
	tgz := filepath.Join(dir, "a.tar.gz")
	zip := filepath.Join(dir, "a.zip")
	for _, p := range []string{tgz, zip} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(dir, "hedef")
	for _, c := range []struct {
		arsiv  string
		tur    Tur
		ilerle func(int)
	}{
		{tgz, TurTarGz, nil},
		{tgz, TurTarGz, func(int) {}},
		{zip, TurZip, nil},
		{zip, TurZip, func(int) {}},
	} {
		if _, err := kiraciCikar(c.arsiv, dest, dilimtest.SK, c.tur, c.ilerle); err != nil {
			t.Fatalf("%s: %v", c.arsiv, err)
		}
	}
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
	if n := len(k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--")); n != 4 {
		t.Fatalf("4 çıkarma çağrısı bekleniyordu, %d", n)
	}
}
