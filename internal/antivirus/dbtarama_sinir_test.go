package antivirus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wpConfigOku root olarak kiracı dosyasını okur: symlink izlememeli, boyut tavanı olmalı
// (wp-config.php -> /dev/zero ya da dev dosya = panel OOM).
func TestWpConfigOku_SymlinkVeBoyutTavani(t *testing.T) {
	d := t.TempDir()
	govde := "define('DB_NAME','a_db');\ndefine('DB_USER','a_u');\ndefine('DB_PASSWORD','p');\n"
	gercek := filepath.Join(d, "gercek.php")
	if err := os.WriteFile(gercek, []byte(govde), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := wpConfigOku(gercek); !ok {
		t.Fatal("normal wp-config okunmalı")
	}
	ln := filepath.Join(d, "wp-config.php")
	if err := os.Symlink(gercek, ln); err != nil {
		t.Fatal(err)
	}
	if _, ok := wpConfigOku(ln); ok {
		t.Fatal("symlink'li wp-config root olarak izlenmemeli")
	}
	buyuk := filepath.Join(d, "buyuk.php")
	if err := os.WriteFile(buyuk, []byte(govde+"//"+strings.Repeat("x", 2<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := wpConfigOku(buyuk); ok {
		t.Fatal("1 MiB üstü wp-config okunmamalı")
	}
	if _, ok := wpConfigOku("/dev/zero"); ok {
		t.Fatal("aygıt dosyası okunmamalı")
	}
}
