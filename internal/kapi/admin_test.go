package kapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func girisIstegi(kapi string, port int) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.Header.Set(Baslik, kapiBasligi(kapi, port))
	return r
}

func TestGirisiKaydetAdmin(t *testing.T) {
	_, ip := testOrtami(t)
	yol := filepath.Join(t.TempDir(), "kapi.json")

	if err := GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 100); err == nil {
		t.Fatal("ayar yokken hata dönmeli (kanıt yazılamadı → eski kapı açık kalmalı)")
	}
	if err := Yaz(yol, ornekAyar()); err != nil {
		t.Fatal(err)
	}
	// Admin kapısından ama ESKİ (reload öncesi) porttan işlenen bayat istek kanıt değil.
	_ = GirisiKaydet(yol, Admin, girisIstegi(Admin, 31355), 100)
	if a, _ := Oku(yol); a.AdminDogrulandi != 0 {
		t.Fatal("başlıktaki port güncel admin portu değilse kanıt SAYILMAMALI")
	}
	// SSH tüneli / yerel istemci dış erişilebilirliği kanıtlamaz.
	*ip = "127.0.0.1"
	_ = GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 100)
	if a, _ := Oku(yol); a.AdminDogrulandi != 0 {
		t.Fatal("loopback istemci kanıt SAYILMAMALI")
	}
	*ip = "203.0.113.7"
	// Eski kapıdan giriş de kanıt değil.
	_ = GirisiKaydet(yol, Admin, girisIstegi(Eski, 8443), 100)
	if a, _ := Oku(yol); a.AdminDogrulandi != 0 {
		t.Fatal("eski kapıdan giriş kanıt SAYILMAMALI")
	}
	if err := GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 100); err != nil {
		t.Fatalf("kayıt: %v", err)
	}
	if a, _ := Oku(yol); a.AdminDogrulandi != 100 {
		t.Fatalf("doğrulama zamanı yazılmalı, got %d", a.AdminDogrulandi)
	}
	// Sonraki giriş ilk kanıtı EZMEMELİ.
	if err := GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 999); err != nil {
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.AdminDogrulandi != 100 {
		t.Fatalf("ilk doğrulama zamanı korunmalı, got %d", a.AdminDogrulandi)
	}
}

// Admin portu değişince kanıt sıfırlanır; yeni porttan HEMEN yapılan giriş sayılır
// (eski 30 sn sezgisi yok — kesinlik başlıktaki gerçek porttan gelir).
func TestGirisiKaydetPortDegisimi(t *testing.T) {
	testOrtami(t)
	yol := filepath.Join(t.TempDir(), "kapi.json")
	a := ornekAyar()
	a.AdminDogrulandi = 50
	_ = Yaz(yol, a)
	if _, err := RolPortuDegistir(yol, Admin, 25005, 1000); err != nil {
		t.Fatal(err)
	}
	_ = GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 1001) // bayat: önceki admin portu
	if a, _ := Oku(yol); a.AdminDogrulandi != 0 {
		t.Fatal("önceki admin portundan (artık eski) gelen istek yeni portun kanıtı SAYILMAMALI")
	}
	_ = GirisiKaydet(yol, Admin, girisIstegi(Admin, 25005), 1002)
	if a, _ := Oku(yol); a.AdminDogrulandi != 1002 {
		t.Fatalf("yeni porttan hemen yapılan giriş kanıt sayılmalı, got %d", a.AdminDogrulandi)
	}
}

// Masaüstü modunda yönetici kapısı yalnız 127.0.0.1'de dinler: tünelden giriş O kapının
// kanıtıdır (yoksa eski kapı hiç kendiliğinden kapanamazdı). Bayi/müşteri için değil.
func TestGirisiKaydetMasaustuTuneli(t *testing.T) {
	_, ip := testOrtami(t)
	masaustuModu = func() bool { return true }
	*ip = "127.0.0.1"
	yol := filepath.Join(t.TempDir(), "kapi.json")
	_ = Yaz(yol, ornekAyar())
	_ = GirisiKaydet(yol, Bayi, girisIstegi(Bayi, 22002), 300)
	if a, _ := Oku(yol); a.IlkGiris[Bayi] != 0 {
		t.Fatal("masaüstü modu bayi kapısının loopback girişini kanıt YAPMAZ")
	}
	if err := GirisiKaydet(yol, Admin, girisIstegi(Admin, 21001), 300); err != nil {
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.AdminDogrulandi != 300 {
		t.Fatalf("masaüstü modunda tünelden yönetici girişi kanıt sayılmalı, got %d", a.AdminDogrulandi)
	}
}

func TestGirisiKaydetBayiMusteri(t *testing.T) {
	testOrtami(t)
	yol := filepath.Join(t.TempDir(), "kapi.json")
	_ = Yaz(yol, ornekAyar())
	_ = GirisiKaydet(yol, Musteri, girisIstegi(Musteri, 23003), 200)
	_ = GirisiKaydet(yol, Bayi, girisIstegi(Admin, 21001), 200) // rol ≠ kapı
	a, _ := Oku(yol)
	if a.IlkGiris[Musteri] != 200 || a.IlkGiris[Bayi] != 0 || a.AdminDogrulandi != 0 {
		t.Fatalf("yalnız kendi kapısındaki giriş, yalnız kendi rolüne yazılmalı: %+v", a)
	}
	if _, err := RolPortuDegistir(yol, Musteri, 24004, 300); err != nil {
		t.Fatal(err)
	}
	if a, _ := Oku(yol); a.IlkGiris[Musteri] != 0 {
		t.Fatal("müşteri portu değişince müşteri kapı kanıtı sıfırlanmalı")
	}
}
