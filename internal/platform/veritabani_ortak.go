// veritabani_ortak.go — OS-NOTR veritabani adi dogrulama + SQL kacis yardimcilari.
//
// 🔴 B-13: bu fonksiyonlar SAF (yan etkisiz) ve OS'tan bagimsiz; windows-etiketli
// dosyadan buraya (etiketsiz) tasindi ki birim testleri Windows VM'inin yani sira
// LINUX CI'da da (`go test ./internal/platform`) kossun. Windows tarafi (veritabani
// _windows.go) bunlari ayni pakette kullanmaya devam eder. Ayristirma/exec iceren
// yardimcilar (vtSatirlariCoz vb.) bilerek OS-etiketli kalir.
package platform

import (
	"regexp"
	"strings"
)

// vtAdRe — veritabani/kullanici adi beyaz listesi. 🔴 SQL ENJEKSIYONU: adlar
// string birlestirmeyle SQL'e girdigi icin TEK savunma bu desendir; harf ya da
// alt cizgi ile baslar, yalniz harf/rakam/alt cizgi, 1-64 karakter.
var vtAdRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// vtKoseKacis — koseli parantez identifier kacisi ([ad] baglaminda ']' -> ']]').
func vtKoseKacis(s string) string { return strings.ReplaceAll(s, "]", "]]") }

// vtTirnakKacis — SQL string literali kacisi (N'...' baglaminda ' -> ”).
func vtTirnakKacis(s string) string { return strings.ReplaceAll(s, "'", "''") }
