package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/backups/uzak"
)

// yedek-coz CLI'ı: .age dosyası + anahtar dosyasıyla çözer; panel/DB gerekmez; özel anahtar
// çıktıya yazılmaz; yanlış anahtarda ya da var olan çıktıda dosya ezilmez/bırakılmaz.
func TestYedekCozCLI(t *testing.T) {
	dir := t.TempDir()
	u, err := uzak.AnahtarCifti(false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := uzak.AliciCoz(u.Alici)
	duz := bytes.Repeat([]byte("gpanel yedek "), 5000)
	var sif bytes.Buffer
	if _, err := uzak.Sifrele(&sif, bytes.NewReader(duz), r); err != nil {
		t.Fatal(err)
	}
	dosya := filepath.Join(dir, "c_ornek-20260825-020000.tar.gz.age")
	_ = os.WriteFile(dosya, sif.Bytes(), 0o600)
	_, metin := uzak.KurtarmaDosyasi(u, time.Now())
	anahtar := filepath.Join(dir, "anahtar.txt")
	_ = os.WriteFile(anahtar, []byte(metin), 0o600)

	var out, errw bytes.Buffer
	if kod := yedekCozCLI([]string{dosya, "--anahtar", anahtar}, &out, &errw); kod != 0 {
		t.Fatalf("çıkış %d: %s", kod, errw.String())
	}
	cikti := strings.TrimSuffix(dosya, ".age")
	if g, _ := os.ReadFile(cikti); !bytes.Equal(g, duz) {
		t.Error("çözülen içerik özgün değil")
	}
	// Başarı satırı (envanterde belgeli): "decrypted: <çıktı yolu>".
	if !strings.Contains(out.String(), "decrypted: "+cikti) {
		t.Errorf("başarı satırı beklenen biçimde değil: %q", out.String())
	}
	// var olan çıktı ezilmez
	out.Reset()
	errw.Reset()
	if kod := yedekCozCLI([]string{dosya, "--anahtar", anahtar}, &out, &errw); kod != 1 || !strings.Contains(errw.String(), "already exists") {
		t.Errorf("var olan çıktı: %d %s", kod, errw.String())
	}
	// yanlış anahtar: çıkış 1, çıktı dosyası yok
	y, _ := uzak.AnahtarCifti(false)
	_, ym := uzak.KurtarmaDosyasi(y, time.Now())
	yk := filepath.Join(dir, "yanlis.txt")
	_ = os.WriteFile(yk, []byte(ym), 0o600)
	c2 := filepath.Join(dir, "yanlis-cikti")
	errw.Reset()
	if kod := yedekCozCLI([]string{dosya, "--anahtar", yk, "--cikti", c2}, &out, &errw); kod != 1 || !strings.Contains(errw.String(), "wrong key") {
		t.Errorf("yanlış anahtar: %d %s", kod, errw.String())
	}
	if _, err := os.Stat(c2); err == nil {
		t.Error("yanlış anahtarda çıktı dosyası kalmamalı")
	}
	// özel anahtar hiçbir çıktıya yazılmaz
	ozel := ""
	for _, l := range strings.Split(metin, "\n") {
		if strings.HasPrefix(l, "AGE-SECRET-KEY") {
			ozel = l
		}
	}
	if strings.Contains(out.String()+errw.String(), ozel) {
		t.Error("özel anahtar çıktıya sızdı")
	}
	// kullanım hataları
	for _, a := range [][]string{{}, {dosya}, {"--anahtar", anahtar}, {dosya, "--anahtar", anahtar, "--indir", "3"}} {
		if kod := yedekCozCLI(a, &out, &errw); kod != 2 {
			t.Errorf("%v: kullanım hatası 2 beklenirdi: %d", a, kod)
		}
	}
}
