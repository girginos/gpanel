package gizli

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func anahtarSifirla(t *testing.T, yol string) {
	t.Helper()
	eskiYol := anahtarYol
	anahtarYol = yol
	birKez = sync.Once{}
	anahtar = nil
	t.Cleanup(func() { anahtarYol = eskiYol; birKez = sync.Once{}; anahtar = nil })
}

func TestAnahtarUret_YoksaUretirVarsaDokunmaz(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "db.key")
	anahtarSifirla(t, yol)

	if err := AnahtarHazir(); err == nil {
		t.Fatal("db.key yokken AnahtarHazir hata dönmeli (üretmemeli)")
	}
	if err := AnahtarUret(); err != nil {
		t.Fatalf("AnahtarUret: %v", err)
	}
	b, err := os.ReadFile(yol)
	if err != nil || len(b) != 32 {
		t.Fatalf("db.key üretilmedi: %v len=%d", err, len(b))
	}
	if st, _ := os.Stat(yol); st.Mode().Perm() != 0o600 {
		t.Fatalf("izin 0600 olmalı: %v", st.Mode().Perm())
	}
	// Var olan anahtara dokunmaz.
	birKez = sync.Once{}
	anahtar = nil
	if err := AnahtarUret(); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(yol)
	if string(b) != string(b2) {
		t.Fatal("var olan db.key değişti")
	}
}
