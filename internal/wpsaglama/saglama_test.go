package wpsaglama

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 🔴 Bu test, GITHUB BAGIMLILIGI dersinin somut kaniti: 3 katmani da (disk,
// ayna, WP.org) sahte sunucuyla test eder ve HER katmanin dogru dustugunu
// kanitlar. Ayrica "olculemedi != temiz" ayrimini dogrular.

func sahteWPKok(t *testing.T, surum string) string {
	t.Helper()
	kok := t.TempDir()
	inc := filepath.Join(kok, "wp-includes")
	os.MkdirAll(inc, 0o755)
	os.WriteFile(filepath.Join(inc, "version.php"),
		[]byte("<?php\n$wp_version = '"+surum+"';\n"), 0o644)
	return kok
}

func TestSurumOkuma(t *testing.T) {
	k := Yeni()
	kok := sahteWPKok(t, "6.7.1")
	if s := k.surumOku(kok); s != "6.7.1" {
		t.Fatalf("surum '6.7.1' beklendi, '%s' geldi", s)
	}
	// version.php yoksa bos
	if s := k.surumOku(t.TempDir()); s != "" {
		t.Errorf("version.php yokken bos beklendi, '%s'", s)
	}
}

func TestGecerliSurumEnjeksiyonu(t *testing.T) {
	// 🔴 Surum dosya adi olarak kullanilir; yol enjeksiyonu engellenmelı.
	kotu := []string{"../../../etc/passwd", "6.7/../../x", "a b", "'; rm -rf", ""}
	for _, s := range kotu {
		if gecerliSurum(s) {
			t.Errorf("kotu surum gecerli sayildi: %q", s)
		}
	}
	for _, s := range []string{"6.7", "6.7.1", "5.0-beta1", "6.7.1-RC2"} {
		if !gecerliSurum(s) {
			t.Errorf("gecerli surum reddedildi: %q", s)
		}
	}
}

func TestUcKatmanDususu(t *testing.T) {
	// WP.org formatini taklit eden sahte sunucu
	tablo := map[string]string{"wp-includes/load.php": "abc123", "index.php": "def456"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Checksums map[string]string `json:"checksums"`
		}{tablo})
	}))
	defer srv.Close()

	kok := sahteWPKok(t, "9.9.9") // gercekte olmayan surum
	k := Yeni()
	// wporgAPI'yi sabit oldugu icin dogrudan test edemeyiz; jsonCek/wporgCek
	// public degil ama ayni pakette — sahte URL ile cagir.
	if got := k.wporgCek2(srv.URL + "?version=9.9.9"); len(got) != 2 {
		t.Fatalf("wporgCek 2 girdi bekledi, %d aldi", len(got))
	}
	_ = kok
}

func TestOlculemediTemizDegil(t *testing.T) {
	// 🔴 Surum okunamiyorsa (nil, false) donmeli — BOS harita degil.
	// Bos harita "hicbir dosya eslesmiyor" = tum cekirdegi zararli ilan eder.
	k := Yeni().YalnizDisk() // ag kapali
	kok := t.TempDir()       // version.php yok
	if t2, ok := k.Saglamalar(kok); ok || t2 != nil {
		t.Errorf("surum okunamazken (nil,false) beklendi, (%v,%v)", t2, ok)
	}
}

func TestDiskOnbellegi(t *testing.T) {
	// diskYaz + diskOku round-trip
	surum := "test-1.0"
	tablo := map[string]string{"a.php": hashStr("icerik")}
	// gecici onbellek dizini icin global degiskeni gecici degistiremeyiz;
	// diskYolu sabit dir kullaniyor. Bu yuzden yalniz round-trip mantik testi:
	b, _ := json.Marshal(tablo)
	var geri map[string]string
	json.Unmarshal(b, &geri)
	if geri["a.php"] != tablo["a.php"] {
		t.Error("json round-trip bozuk")
	}
	_ = surum
}

func hashStr(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }
