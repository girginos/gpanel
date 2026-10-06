package provisioner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readUserIniAutoPrepend root olarak kiracının .user.ini'sini okur: symlink izlememeli,
// boyut tavanı olmalı.
func TestReadUserIniAutoPrepend_SymlinkVeTavan(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, ".user.ini"), []byte("auto_prepend_file = /x/a.php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readUserIniAutoPrepend(d); got != "/x/a.php" {
		t.Fatalf("normal okuma: %q", got)
	}
	// symlink
	d2 := t.TempDir()
	hedef := filepath.Join(d2, "baska")
	_ = os.WriteFile(hedef, []byte("auto_prepend_file = /x/a.php\n"), 0o644)
	if err := os.Symlink(hedef, filepath.Join(d2, ".user.ini")); err != nil {
		t.Fatal(err)
	}
	if got := readUserIniAutoPrepend(d2); got != "" {
		t.Fatalf("symlink izlenmemeli: %q", got)
	}
	// büyük
	d3 := t.TempDir()
	_ = os.WriteFile(filepath.Join(d3, ".user.ini"), []byte("auto_prepend_file = /x/a.php\n;"+strings.Repeat("x", 128<<10)), 0o644)
	if got := readUserIniAutoPrepend(d3); got != "" {
		t.Fatalf("tavan üstü okunmamalı: %q", got)
	}
}
