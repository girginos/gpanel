package provisioner

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Panel dışarıdan HİÇBİR kaynak yüklemez (kullanıcı kararı 2026-10-13): yazı tipleri
// (@fontsource), marka logoları (frontend/public/simge) ve betik/stiller panelle gelir.
// Bu dosya üç bekçidir: CSP değeri, kurulum nginx şablonu ve kaynak taraması.

func TestPanelCSPDisAdresYok(t *testing.T) {
	if strings.Contains(panelCSP, "http") || strings.Contains(panelCSP, "https:") {
		t.Fatalf("panelCSP dış adres içeriyor (kaynağı panele gömün): %s", panelCSP)
	}
	sablon, err := os.ReadFile("../../assets/nginx/_panel.conf")
	if err != nil {
		t.Fatal(err)
	}
	satirlar := regexp.MustCompile(`add_header Content-Security-Policy "([^"]*)"`).FindAllSubmatch(sablon, -1)
	if len(satirlar) == 0 {
		t.Fatal("kurulum şablonunda CSP satırı yok")
	}
	for _, s := range satirlar {
		if string(s[1]) != panelCSP {
			t.Errorf("_panel.conf CSP'si panelCSP'den farklı (kurulumda dış adres kalır):\n  şablon: %s\n  kod:    %s", s[1], panelCSP)
		}
	}
}

// Kaynak YÜKLEYEN dış adres desenleri. Düz bağlantılar (<a href>, kopyalanacak komut metni)
// yükleme değildir, kapsam dışı.
var disKaynakDeseni = regexp.MustCompile(
	`fonts\.googleapis\.com|fonts\.gstatic\.com|cdn\.simpleicons\.org|cdn\.jsdelivr\.net|unpkg\.com|cdnjs\.cloudflare\.com|` +
		`<script[^>]+src=\\?["']https?://|<link[^>]+href=\\?["']https?://|<img[^>]+src=\\?["']https?://|` +
		`url\(\\?["']?https?://|@import[^;\n]*https?://`)

func TestPanelKaynaktaDisYuklemeYok(t *testing.T) {
	kokler := []string{"../../frontend/index.html", "../../frontend/src", "../../internal", "../../cmd", "../../assets/nginx"}
	uzanti := map[string]bool{".go": true, ".ts": true, ".tsx": true, ".css": true, ".html": true, ".conf": true, ".tmpl": true}
	taranan := 0
	for _, kok := range kokler {
		err := filepath.WalkDir(kok, func(yol string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !uzanti[filepath.Ext(yol)] || strings.HasSuffix(yol, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(yol)
			if err != nil {
				return err
			}
			taranan++
			for i, satir := range strings.Split(string(b), "\n") {
				if m := disKaynakDeseni.FindString(satir); m != "" {
					t.Errorf("%s:%d dış kaynak yükleniyor (%q) — kaynağı panele gömün", yol, i+1, m)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", kok, err)
		}
	}
	if taranan < 100 {
		t.Fatalf("yalnız %d dosya tarandı — kökler yanlış (test sessizce boş geçmesin)", taranan)
	}
}
