package avmotor

// duzdosya_test.go — denetim-b: motor kök olarak kiracı dosyası okur. Yürüyüşte düz dosya
// görünen yol, okuma anında FIFO/symlink'e takas edilebilir (TOCTOU): FIFO'da os.Open
// yazarsız SONSUZA DEK bloklar (tarama işçisi kalıcı takılır); symlink'te kök, kiracının
// işaret ettiği kök dosyayı okur. Okuma yolu O_NOFOLLOW|O_NONBLOCK + düzenli-dosya denetimi
// yapmalı.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func sureliCalistir(t *testing.T, f func()) bool {
	t.Helper()
	bitti := make(chan struct{})
	go func() { f(); close(bitti) }()
	select {
	case <-bitti:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func TestOkuma_FIFOdaBloklanmaz(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "x.php")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("mkfifo yok:", err)
	}
	if !sureliCalistir(t, func() { _, _ = okuSinirli(fifo, 1024) }) {
		t.Fatal("okuSinirli FIFO'da bloklandı")
	}
	if !sureliCalistir(t, func() { _, _ = okuKuyruk(fifo, 1024) }) {
		t.Fatal("okuKuyruk FIFO'da bloklandı")
	}
	if _, err := okuSinirli(fifo, 1024); err == nil {
		t.Fatal("FIFO reddedilmeli")
	}
}

func TestOkuma_SymlinkIzlenmez(t *testing.T) {
	d := t.TempDir()
	hedef := filepath.Join(d, "gizli.txt")
	if err := os.WriteFile(hedef, []byte("KOK-SIRRI"), 0o600); err != nil {
		t.Fatal(err)
	}
	bag := filepath.Join(d, "b.php")
	if err := os.Symlink(hedef, bag); err != nil {
		t.Fatal(err)
	}
	if b, err := okuSinirli(bag, 1024); err == nil {
		t.Fatalf("symlink izlendi: %q", b)
	}
	if b, err := okuKuyruk(bag, 1024); err == nil {
		t.Fatalf("symlink izlendi (kuyruk): %q", b)
	}
	// meşru düz dosya hâlâ okunur
	if b, err := okuSinirli(hedef, 1024); err != nil || string(b) != "KOK-SIRRI" {
		t.Fatalf("düz dosya okunamadı: %v %q", err, b)
	}
}
