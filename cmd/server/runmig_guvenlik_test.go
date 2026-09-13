package main

import (
	"os"
	"path/filepath"
	"testing"
)

// runMigrations goc-guvenlik kapisi (Corgea SQLi FP -> defense-in-depth):
// goc SQL'i yalniz root'a ait + grup/diger-yazilamaz dizin/dosyalardan calisir.
// Bu testler chown/symlink kullandigi icin root gerektirir; degilse atlanir.

func rootGerek(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root gerektirir (chown + izin testleri)")
	}
}

func TestGocDiziniGuvenli(t *testing.T) {
	rootGerek(t)
	base := t.TempDir()

	iyi := filepath.Join(base, "iyi")
	if err := os.Mkdir(iyi, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(iyi, 0o755)
	if !gocDiziniGuvenli(iyi) {
		t.Errorf("root:root 0755 dizini GECMELI")
	}

	gevsek := filepath.Join(base, "gevsek")
	_ = os.Mkdir(gevsek, 0o755)
	_ = os.Chmod(gevsek, 0o777)
	if gocDiziniGuvenli(gevsek) {
		t.Errorf("0777 (diger-yazilabilir) dizin REDDEDILMELI")
	}

	yabanci := filepath.Join(base, "yabanci")
	_ = os.Mkdir(yabanci, 0o755)
	_ = os.Chmod(yabanci, 0o755)
	if err := os.Chown(yabanci, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if gocDiziniGuvenli(yabanci) {
		t.Errorf("root-disi sahipli dizin REDDEDILMELI")
	}

	hedef := filepath.Join(base, "hedef")
	_ = os.Mkdir(hedef, 0o755)
	sym := filepath.Join(base, "sym")
	if err := os.Symlink(hedef, sym); err != nil {
		t.Fatal(err)
	}
	if gocDiziniGuvenli(sym) {
		t.Errorf("symlink dizin REDDEDILMELI")
	}
}

func TestGocDosyasiGuvenli(t *testing.T) {
	rootGerek(t)
	dir := t.TempDir()

	yaz := func(ad string, mode os.FileMode) string {
		p := filepath.Join(dir, ad)
		if err := os.WriteFile(p, []byte("SELECT 1;"), mode); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(p, mode)
		return p
	}
	yaz("0001_iyi.sql", 0o644)
	yaz("0002_gevsek.sql", 0o666)
	yab := yaz("0003_yabanci.sql", 0o644)
	if err := os.Chown(yab, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "0001_iyi.sql"), filepath.Join(dir, "0004_sym.sql")); err != nil {
		t.Fatal(err)
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range ents {
		got[e.Name()] = gocDosyasiGuvenli(e)
	}

	if !got["0001_iyi.sql"] {
		t.Errorf("root 0644 duz dosya GECMELI")
	}
	if got["0002_gevsek.sql"] {
		t.Errorf("0666 (diger-yazilabilir) dosya REDDEDILMELI")
	}
	if got["0003_yabanci.sql"] {
		t.Errorf("root-disi sahipli dosya REDDEDILMELI")
	}
	if got["0004_sym.sql"] {
		t.Errorf("symlink dosya REDDEDILMELI")
	}
}
