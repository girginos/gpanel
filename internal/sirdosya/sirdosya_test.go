package sirdosya

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// os.WriteFile(…, 0600) önceden 0644 kalmış dosyanın iznini DEĞİŞTİRMEZ; Yaz değiştirir.
func TestYaz_GenisIzinliDosyaDaraltilir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sir")
	if err := os.WriteFile(p, []byte("eski"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Yaz(p, []byte("yeni"), 0o600, -1, -1); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mod %o", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "yeni" {
		t.Fatal("içerik yazılmadı")
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Errorf("geçici dosya kaldı: %v", ents)
	}
}

func TestYaz_SymlinkTakipEtmez(t *testing.T) {
	dir := t.TempDir()
	hedef := filepath.Join(dir, "hedef")
	if err := os.WriteFile(hedef, []byte("dokunma"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "sir")
	if err := os.Symlink(hedef, link); err != nil {
		t.Fatal(err)
	}
	if err := Yaz(link, []byte("x"), 0o600, -1, -1); !errors.Is(err, ErrSymlink) {
		t.Fatalf("symlink reddedilmedi: %v", err)
	}
	if b, _ := os.ReadFile(hedef); string(b) != "dokunma" {
		t.Fatal("symlink hedefi ezildi")
	}
	if _, _, err := Oku(link, 100); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Oku symlink'i izledi: %v", err)
	}
}

func TestYaz_UmasktanBagimsizGrupIzni(t *testing.T) {
	p := filepath.Join(t.TempDir(), "jeton")
	if err := Yaz(p, []byte("x"), 0o640, -1, -1); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mod %o", st.Mode().Perm())
	}
}

func TestOku_BoyutSiniri(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b")
	_ = os.WriteFile(p, make([]byte, 50), 0o600)
	if _, _, err := Oku(p, 10); err == nil {
		t.Fatal("sınır aşımı kabul edildi")
	}
	if b, _, err := Oku(p, 50); err != nil || len(b) != 50 {
		t.Fatal(err)
	}
}
