package backups

// secili_kiraci_test.go — denetim-b: mod=dosya geri yükleme (kiracı çağırır) kopyalarını
// KÖK değil KİRACI kimliğinde yapmalı; aksi halde canlı ağaçta yedekten sonra symlink'e
// çevrilmiş bir dizin (x -> /etc) kök yazmasını jail dışına yönlendirir.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSecilenDosyalariGeriYukle_KiraciKimligindeYazar(t *testing.T) {
	tmp := t.TempDir()
	sk := "c_denetimtest"
	src := filepath.Join(tmp, sk, "x")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "dosya.txt"), []byte("icerik"), 0o644); err != nil {
		t.Fatal(err)
	}
	hedefKok := t.TempDir()

	var komutlar [][]string
	eskiK, eskiS := restoreKiraciKomut, sahnelemeyiKiraciyaDevret
	restoreKiraciKomut = func(kul string, argv ...string) ([]byte, error) {
		komutlar = append(komutlar, append([]string{kul}, argv...))
		// /home/<sk> yerine geçici dizine yönlendir
		yeni := make([]string, len(argv))
		for i, a := range argv {
			if len(a) >= len("/home/"+sk) && a[:len("/home/"+sk)] == "/home/"+sk {
				a = hedefKok + a[len("/home/"+sk):]
			}
			yeni[i] = a
		}
		return exec.Command(yeni[0], yeni[1:]...).CombinedOutput()
	}
	sahnelemeyiKiraciyaDevret = func(string, string) error { return nil }
	defer func() { restoreKiraciKomut, sahnelemeyiKiraciyaDevret = eskiK, eskiS }()

	n, _, err := secilenDosyalariGeriYukle(tmp, sk, []string{"x/dosya.txt"}, "yerinde")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(komutlar) == 0 {
		t.Fatal("hiç kiracı komutu çalışmadı (kök doğrudan yazmış olmalı)")
	}
	cp := false
	for _, k := range komutlar {
		if k[0] != sk {
			t.Fatalf("yanlış kimlik: %v", k)
		}
		cp = cp || k[1] == "cp"
	}
	if !cp {
		t.Fatalf("cp kiracı kimliğinde çalışmadı: %v", komutlar)
	}
	b, err := os.ReadFile(filepath.Join(hedefKok, "x", "dosya.txt"))
	if err != nil || string(b) != "icerik" {
		t.Fatalf("dosya geri yüklenmedi: %v %q", err, b)
	}
}
