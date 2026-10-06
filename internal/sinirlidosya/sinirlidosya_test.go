package sinirlidosya

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOku(t *testing.T) {
	d := t.TempDir()
	ok := filepath.Join(d, "ok")
	_ = os.WriteFile(ok, []byte("abc"), 0o600)
	if b, err := Oku(ok, 3); err != nil || string(b) != "abc" {
		t.Fatalf("tam sınırda okunmalı: %q %v", b, err)
	}
	if _, err := Oku(ok, 2); !errors.Is(err, ErrBuyuk) {
		t.Fatalf("tavan aşımı reddedilmeli: %v", err)
	}
	ln := filepath.Join(d, "ln")
	_ = os.Symlink(ok, ln)
	if _, err := Oku(ln, 100); err == nil {
		t.Fatal("symlink izlenmemeli")
	}
	if _, err := Oku("/dev/zero", 1<<20); err == nil {
		t.Fatal("/dev/zero (aygıt) reddedilmeli")
	}
	ff := filepath.Join(d, "fifo")
	if err := syscall.Mkfifo(ff, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Oku(ff, 100); err == nil {
		t.Fatal("FIFO reddedilmeli (bloklamamalı)")
	}
}
