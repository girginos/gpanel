package kapi

import (
	"errors"
	"path/filepath"
	"testing"
)

func eskiliAyar(t *testing.T) string {
	t.Helper()
	yol := filepath.Join(t.TempDir(), "kapi.json")
	a := ornekAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 1000
	if err := Yaz(yol, a); err != nil {
		t.Fatal(err)
	}
	return yol
}

func TestEskiUzat(t *testing.T) {
	yol := eskiliAyar(t)
	if err := EskiUzat(yol, 2, 500); err != nil { // bitiş (1000) şimdiden (500) sonra → bitişten uzat
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.EskiBitis != 1000+2*86400 {
		t.Fatalf("mevcut bitişten uzatılmalı, got %d", a.EskiBitis)
	}
	if err := EskiUzat(yol, 1, 9_999_999); err != nil { // şimdi bitişten sonra → şimdiden uzat
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.EskiBitis != 9_999_999+86400 {
		t.Fatalf("şimdiden uzatılmalı, got %d", a.EskiBitis)
	}
	if err := EskiUzat(yol, 0, 1); err == nil {
		t.Fatal("gün < 1 reddedilmeli")
	}
	yol2 := filepath.Join(t.TempDir(), "kapi.json")
	_ = Yaz(yol2, ornekAyar())
	if err := EskiUzat(yol2, 1, 1); !errors.Is(err, ErrEskiYok) {
		t.Fatalf("eski kapı yokken ErrEskiYok, got %v", err)
	}
}

func TestRolPortuDegistirBayiMusteriHemenTasinir(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kapi.json")
	_ = Yaz(yol, ornekAyar())
	eski, err := RolPortuDegistir(yol, Musteri, 24004, 100)
	if err != nil || eski != 23003 {
		t.Fatalf("eski=%d err=%v", eski, err)
	}
	a, _ := Oku(yol)
	if a.Musteri != 24004 || len(a.EskiPortlar) != 0 {
		t.Fatalf("müşteri hemen taşınmalı, eski kapı AÇILMAMALI: %+v", a)
	}
	if _, err := RolPortuDegistir(yol, Bayi, 22002, 100); !errors.Is(err, ErrAyniPort) {
		t.Fatalf("aynı port ErrAyniPort, got %v", err)
	}
	if _, err := RolPortuDegistir(yol, Bayi, 21001, 100); err == nil {
		t.Fatal("başka rolün portu reddedilmeli")
	}
	if _, err := RolPortuDegistir(yol, "root", 25005, 100); err == nil {
		t.Fatal("bilinmeyen rol reddedilmeli")
	}
}

// Admin portu değişince önceki admin portu eski kapı olur ve admin yeni porttan
// girene dek KAPANMAZ (dış firewall yeni portu kesiyorsa kilitlenme olmasın).
func TestRolPortuDegistirAdminEskiKapiOlur(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kapi.json")
	a := ornekAyar()
	a.AdminDogrulandi = 50
	_ = Yaz(yol, a)

	if _, err := RolPortuDegistir(yol, Admin, 25005, 1000); err != nil {
		t.Fatal(err)
	}
	a, _ = Oku(yol)
	if a.Admin != 25005 || len(a.EskiPortlar) != 1 || a.EskiPortlar[0] != 21001 {
		t.Fatalf("önceki admin portu eski kapı olmalı: %+v", a)
	}
	if a.AdminDogrulandi != 0 {
		t.Fatalf("admin kanıtı sıfırlanmalı: %+v", a)
	}
	if !a.EskiAcik(999_999) {
		t.Fatal("admin yeni porttan girmeden eski kapı süre dolsa da AÇIK kalmalı")
	}
}

// Açık eski port rol portu yapılabilir (müşteri bilinen 8443'te kalsın) — ama YALNIZ
// admin yeni admin kapısını doğruladıktan sonra: devralmak o adresteki geçiş kapısını
// kapatır; admin hâlâ oradan çalışıyor ve yeni portu dışarıdan kapalıysa kilitlenirdi.
func TestRolPortuDegistirEskiPortuDevralir(t *testing.T) {
	yol := eskiliAyar(t) // eski: [8443], admin doğrulamadı
	if _, err := RolPortuDegistir(yol, Musteri, 8443, 500); !errors.Is(err, ErrEskiDevralma) {
		t.Fatalf("admin doğrulamadan eski port devralınamamalı, got %v", err)
	}
	if a, _ := Oku(yol); a.Musteri != 23003 || len(a.EskiPortlar) != 1 {
		t.Fatalf("reddedilen değişiklik ayarı DEĞİŞTİRMEMELİ: %+v", a)
	}
	_ = Guncelle(yol, func(a *Ayar) error { a.AdminDogrulandi = 400; return nil })
	if _, err := RolPortuDegistir(yol, Musteri, 8443, 500); err != nil {
		t.Fatal(err)
	}
	a, _ := Oku(yol)
	if a.Musteri != 8443 || len(a.EskiPortlar) != 0 || a.EskiBitis != 0 {
		t.Fatalf("8443 müşteri kapısı olmalı, eski liste boşalmalı: %+v", a)
	}
}

func TestEskiKapat(t *testing.T) {
	yol := eskiliAyar(t)
	var adh *AdminDogrulanmadiHatasi
	if err := EskiKapat(yol, false, 500); !errors.As(err, &adh) || adh.AdminPort != 21001 {
		t.Fatalf("admin doğrulamadan kapatma REDDEDİLMELİ (kilitlenme), got %v", err)
	}
	if a, _ := Oku(yol); len(a.EskiPortlar) != 1 || a.EskiBitis != 1000 {
		t.Fatal("reddedilen kapatma ayarı DEĞİŞTİRMEMELİ")
	}
	if err := EskiKapat(yol, true, 500); err != nil {
		t.Fatalf("zorla kapatma: %v", err)
	}
	if a, _ := Oku(yol); len(a.EskiPortlar) != 0 {
		t.Fatal("zorla: eski portlar ayardan düşmeli")
	}

	yol = eskiliAyar(t)
	_ = Guncelle(yol, func(a *Ayar) error { a.AdminDogrulandi = 100; return nil })
	if err := EskiKapat(yol, false, 500); err != nil {
		t.Fatalf("admin doğruladıysa kapatılabilmeli: %v", err)
	}
	if a, _ := Oku(yol); a.EskiAcik(500) {
		t.Fatal("kapatma sonrası EskiAcik false olmalı")
	}
}
