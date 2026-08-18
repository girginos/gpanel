package antivirus

import (
	"os"
	"path/filepath"
	"testing"
)

// Meşru geri yükleme: gerçek dizinler → dosya orijinal yerine taşınmalı.
func TestGuvenliGeriTasi_MesruGeriYukleme(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "c_x")
	if err := os.MkdirAll(filepath.Join(home, ".karantina"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "public_html", "stg"), 0o755); err != nil {
		t.Fatal(err)
	}
	kar := filepath.Join(home, ".karantina", "q_evil.php")
	if err := os.WriteFile(kar, []byte("<?php evil"), 0o000); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "stg", "evil.php")

	if err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("meşru geri yükleme başarısız: %v", err)
	}
	if _, err := os.Lstat(orij); err != nil {
		t.Fatalf("dosya orijinal yerde değil: %v", err)
	}
	if _, err := os.Lstat(kar); !os.IsNotExist(err) {
		t.Errorf("karantina kaynağı hâlâ var (taşınmadı)")
	}
}

// Çakışma: hedef zaten varsa ErrExist dönmeli, üzerine yazmamalı.
func TestGuvenliGeriTasi_CakismaReddedilir(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "c_x")
	os.MkdirAll(filepath.Join(home, ".karantina"), 0o700)
	os.MkdirAll(filepath.Join(home, "public_html"), 0o755)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("yeni"), 0o644)
	orij := filepath.Join(home, "public_html", "evil.php")
	os.WriteFile(orij, []byte("mevcut"), 0o644) // hedef zaten var

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if !os.IsExist(err) {
		t.Fatalf("çakışma reddedilmedi, hata=%v (beklenen ErrExist)", err)
	}
	b, _ := os.ReadFile(orij)
	if string(b) != "mevcut" {
		t.Errorf("mevcut dosya EZİLDİ: %q", string(b))
	}
}

// SYMLINK SALDIRISI: ara dizin ev dışına symlink → reddedilmeli, ev dışına yazılmamalı.
func TestGuvenliGeriTasi_SymlinkSaldirisiEngellenir(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "c_x")
	attack := filepath.Join(base, "attack") // ev DIŞI
	os.MkdirAll(filepath.Join(home, ".karantina"), 0o700)
	os.MkdirAll(filepath.Join(home, "public_html"), 0o755)
	os.MkdirAll(attack, 0o755)
	kar := filepath.Join(home, ".karantina", "q_evil.php")
	os.WriteFile(kar, []byte("<?php evil"), 0o644)
	// stg = ev dışına symlink (saldırgan kiracı kendi public_html'inde yapar)
	os.Symlink(attack, filepath.Join(home, "public_html", "stg"))
	orij := filepath.Join(home, "public_html", "stg", "evil.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("symlink saldırısı engellenmedi, hata=%v (beklenen errGuvenliYol)", err)
	}
	if _, e := os.Lstat(filepath.Join(attack, "evil.php")); !os.IsNotExist(e) {
		t.Fatalf("KRİTİK: dosya ev DIŞINA (attack) yazıldı — fix çalışmıyor!")
	}
	if _, e := os.Lstat(kar); e != nil {
		t.Errorf("kaynak dosya kaybedildi: %v", e)
	}
}

// Daha derin ara bileşen symlink (public_html/a gerçek, a/b symlink).
func TestGuvenliGeriTasi_DerinSymlinkEngellenir(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "c_x")
	attack := filepath.Join(base, "attack")
	os.MkdirAll(filepath.Join(home, ".karantina"), 0o700)
	os.MkdirAll(filepath.Join(home, "public_html", "a"), 0o755)
	os.MkdirAll(attack, 0o755)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("x"), 0o644)
	os.Symlink(attack, filepath.Join(home, "public_html", "a", "b")) // a/b -> attack
	orij := filepath.Join(home, "public_html", "a", "b", "evil.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("derin symlink engellenmedi: %v", err)
	}
	if _, e := os.Lstat(filepath.Join(attack, "evil.php")); !os.IsNotExist(e) {
		t.Fatalf("KRİTİK: derin symlink ile ev dışına yazıldı!")
	}
}

// Ev dışına doğrudan orij (leksik) → errGuvenliYol.
func TestGuvenliGeriTasi_EvDisiYolReddedilir(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "c_x")
	os.MkdirAll(filepath.Join(home, ".karantina"), 0o700)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("x"), 0o644)
	orij := filepath.Join(base, "baska", "evil.php") // /home/c_x dışında

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("ev dışı yol reddedilmedi: %v", err)
	}
}
