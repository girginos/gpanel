// veritabani_ortak_test.go — OS-NOTR: SQL enjeksiyon beyaz listesi + kacis
// testleri. Build etiketi YOK → hem Windows VM'inde hem LINUX CI'da
// (`go test ./internal/platform`) kosar (B-13).
package platform

import (
	"strings"
	"testing"
)

// TestVtAdRe — veritabani/kullanici adi beyaz listesi: SQL'e string birlestirmeyle
// girdigi icin TEK savunma budur. Enjeksiyon denemeleri REDDEDILMELI.
func TestVtAdRe(t *testing.T) {
	gecerli := []string{"db1", "musteri_db", "_gizli", "A", strings.Repeat("a", 64)}
	gecersiz := []string{
		"", "1db", "db-1", "db 1", `db;DROP DATABASE x`, `db'--`,
		`db]`, "türkçe", strings.Repeat("a", 65), // 64 ustu
	}
	for _, s := range gecerli {
		if !vtAdRe.MatchString(s) {
			t.Errorf("gecerli ad reddedildi: %q", s)
		}
	}
	for _, s := range gecersiz {
		if vtAdRe.MatchString(s) {
			t.Errorf("gecersiz ad KABUL edildi (enjeksiyon riski): %q", s)
		}
	}
}

// TestVtKacis — koseli-parantez ve tirnak kacisi (ikinci kat savunma). Bir gun
// vtAdRe gevserse bu kacislar hala ']' ve ”' saldirilarini notrler.
func TestVtKacis(t *testing.T) {
	if g := vtKoseKacis("a]b]c"); g != "a]]b]]c" {
		t.Errorf("vtKoseKacis yanlis: %q", g)
	}
	if g := vtTirnakKacis("O'Brien's"); g != "O''Brien''s" {
		t.Errorf("vtTirnakKacis yanlis: %q", g)
	}
	// Parola icinde tek tirnak N'...' literalinden KACAMAMALI.
	if g := vtTirnakKacis("p'; DROP LOGIN sa;--"); strings.Count(g, "'")%2 != 0 {
		t.Errorf("kacis sonrasi tek tirnak sayisi tek kaldi (literal kirilir): %q", g)
	}
}
