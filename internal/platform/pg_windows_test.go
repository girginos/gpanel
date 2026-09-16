//go:build windows

// pg_windows_test.go — B-11: pg superuser parolasi optionfile'a yazilir (cmdline'da
// DEGIL). pgOptionDosyasi icerigi + benzersizligi.
package platform

import (
	"os"
	"testing"
)

func TestPgOptionDosyasi(t *testing.T) {
	dizin := t.TempDir()
	parola := "Gizli!Parola-9f3a2b1c"
	yol, err := pgOptionDosyasi(dizin, parola)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	beklenen := "superpassword=" + parola + "\n"
	if string(b) != beklenen {
		t.Errorf("option dosyasi icerigi yanlis: %q (beklenen %q)", string(b), beklenen)
	}
	// iki cagri BENZERSIZ yol vermeli (rastgele sonek).
	yol2, err := pgOptionDosyasi(dizin, "x")
	if err != nil {
		t.Fatal(err)
	}
	if yol == yol2 {
		t.Error("option dosyasi yollari benzersiz degil")
	}
}
