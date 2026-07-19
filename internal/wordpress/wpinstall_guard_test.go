package wordpress

import (
	"os"
	"path/filepath"
	"testing"
)

// #369 çift-kurulum koruması: kurulumZatenVar hedef-dizin ön-kontrolü.
func TestKurulumZatenVar(t *testing.T) {
	t.Run("olmayan dizin temiz", func(t *testing.T) {
		yol := filepath.Join(t.TempDir(), "yok")
		if _, kurulu := kurulumZatenVar(yol); kurulu {
			t.Fatalf("olmayan dizin temiz sayılmalı")
		}
	})

	t.Run("bos dizin temiz", func(t *testing.T) {
		if _, kurulu := kurulumZatenVar(t.TempDir()); kurulu {
			t.Fatalf("boş dizin temiz sayılmalı")
		}
	})

	t.Run("sadece placeholder temiz", func(t *testing.T) {
		d := t.TempDir()
		for _, f := range []string{"index.html", "favicon.ico", ".htaccess", "robots.txt"} {
			if err := os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, kurulu := kurulumZatenVar(d); kurulu {
			t.Fatalf("yalnız placeholder dosyalı dizin temiz sayılmalı")
		}
	})

	t.Run("wp-config bloklar", func(t *testing.T) {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "wp-config.php"), []byte("<?php"), 0o644); err != nil {
			t.Fatal(err)
		}
		msg, kurulu := kurulumZatenVar(d)
		if !kurulu || msg == "" {
			t.Fatalf("wp-config.php mevcut → kurulu:true beklenir (msg=%q kurulu=%v)", msg, kurulu)
		}
	})

	t.Run("baska icerik bloklar", func(t *testing.T) {
		d := t.TempDir()
		// başka bir uygulamanın izi (ör. mevcut PHP sitesi) — ezilmemeli
		if err := os.WriteFile(filepath.Join(d, "index.php"), []byte("<?php"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, kurulu := kurulumZatenVar(d); !kurulu {
			t.Fatalf("dolu dizin (index.php) kurulu:true olmalı — mevcut içerik ezilmemeli")
		}
	})
}
