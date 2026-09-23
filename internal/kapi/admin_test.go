package kapi

import (
	"path/filepath"
	"testing"
)

func TestAdminGirisiniKaydet(t *testing.T) {
	adminKaydedildi.Store(false)
	t.Cleanup(func() { adminKaydedildi.Store(false) })
	yol := filepath.Join(t.TempDir(), "kapi.json")

	if err := AdminGirisiniKaydet(yol, 100); err == nil {
		t.Fatal("ayar yokken hata dönmeli (kanıt yazılamadı → eski kapı açık kalmalı)")
	}
	if adminKaydedildi.Load() {
		t.Fatal("başarısız kayıt bellek bayrağını KALDIRMAMALI; yoksa sonraki girişler hiç yazılmaz")
	}
	if err := Yaz(yol, ornekAyar()); err != nil {
		t.Fatal(err)
	}
	if err := AdminGirisiniKaydet(yol, 100); err != nil {
		t.Fatalf("kayıt: %v", err)
	}
	if a, _ := Oku(yol); a.AdminDogrulandi != 100 {
		t.Fatalf("doğrulama zamanı yazılmalı, got %d", a.AdminDogrulandi)
	}
	// Sonraki giriş ilk kanıtı EZMEMELİ (bellek bayrağı sıfırlansa bile).
	adminKaydedildi.Store(false)
	if err := AdminGirisiniKaydet(yol, 999); err != nil {
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.AdminDogrulandi != 100 {
		t.Fatalf("ilk doğrulama zamanı korunmalı, got %d", a.AdminDogrulandi)
	}
}
