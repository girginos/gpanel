//go:build windows

package platform

import (
	"errors"
	"path/filepath"
	"testing"
)

// hesap deposu birim testi — Admin/Reseller/Hosting cok-katmanli yapinin cekirdegi.
// hesaplarYolu gecici dizine yonlendirilir (izole; ProgramData'ya dokunmaz).
func hazirla(t *testing.T) {
	t.Helper()
	eski := hesaplarYolu
	hesaplarYolu = filepath.Join(t.TempDir(), "hesaplar.json")
	t.Cleanup(func() { hesaplarYolu = eski })
}

func TestHesapOlusturVeDogrula(t *testing.T) {
	hazirla(t)
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi1", Rol: RolReseller, MaxSite: 10}, "parola123"); err != nil {
		t.Fatalf("reseller olusturulamadi: %v", err)
	}
	h, ok := HesapDogrula("bayi1", "parola123")
	if !ok {
		t.Fatal("dogru parola reddedildi")
	}
	if h.Rol != RolReseller || h.MaxSite != 10 {
		t.Fatalf("hesap alanlari yanlis: %+v", h)
	}
	if h.ParolaHash != "" {
		t.Fatal("ParolaHash siyrilmadi (sizinti)")
	}
	if _, ok := HesapDogrula("bayi1", "yanlis"); ok {
		t.Fatal("yanlis parola KABUL edildi")
	}
	if _, ok := HesapDogrula("yokboyle", "parola123"); ok {
		t.Fatal("olmayan kullanici KABUL edildi")
	}
}

func TestHesapAdminAyrilmis(t *testing.T) {
	hazirla(t)
	err := HesapOlustur(Hesap{KullaniciAdi: "admin", Rol: RolReseller}, "parola123")
	if !errors.Is(err, ErrGecersizIstek) {
		t.Fatalf("'admin' ayrilmis olmali (ErrGecersizIstek), gelen: %v", err)
	}
}

func TestHesapGecersizGirdi(t *testing.T) {
	hazirla(t)
	for _, tc := range []struct {
		ad, rol, parola string
	}{
		{"ab", RolReseller, "parola123"},    // kisa kullanici
		{"bayi2", "sudo", "parola123"},      // gecersiz rol
		{"bayi2", RolReseller, "kisa"},      // kisa parola
		{"BÜYÜK", RolReseller, "parola123"}, // gecersiz karakter
	} {
		if err := HesapOlustur(Hesap{KullaniciAdi: tc.ad, Rol: tc.rol}, tc.parola); !errors.Is(err, ErrGecersizIstek) {
			t.Fatalf("gecersiz girdi %+v ErrGecersizIstek dondurmeli, gelen: %v", tc, err)
		}
	}
}

func TestHesapCiftKullanici(t *testing.T) {
	hazirla(t)
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi3", Rol: RolReseller}, "parola123"); err != nil {
		t.Fatalf("ilk olusturma: %v", err)
	}
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi3", Rol: RolReseller}, "parola123"); !errors.Is(err, ErrGecersizIstek) {
		t.Fatal("cift kullanici adi kabul edildi")
	}
}

func TestHesapAskidaGirisEngeli(t *testing.T) {
	hazirla(t)
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi4", Rol: RolReseller}, "parola123"); err != nil {
		t.Fatal(err)
	}
	if err := HesapGuncelle("bayi4", "", "", "askida", 0, 0, false); err != nil {
		t.Fatalf("askiya alma: %v", err)
	}
	if _, ok := HesapDogrula("bayi4", "parola123"); ok {
		t.Fatal("askidaki hesap giris yapabildi")
	}
}

func TestHesapHostingBayiBagi(t *testing.T) {
	hazirla(t)
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi5", Rol: RolReseller}, "parola123"); err != nil {
		t.Fatal(err)
	}
	// olmayan bayiye baglama reddedilir
	if err := HesapOlustur(Hesap{KullaniciAdi: "host1", Rol: RolHosting, BayiKullanici: "yok"}, "parola123"); !errors.Is(err, ErrGecersizIstek) {
		t.Fatal("olmayan bayiye hosting baglandi")
	}
	// gecerli bayiye baglama ok
	if err := HesapOlustur(Hesap{KullaniciAdi: "host1", Rol: RolHosting, BayiKullanici: "bayi5"}, "parola123"); err != nil {
		t.Fatalf("gecerli bayiye hosting: %v", err)
	}
	// altinda hosting varken bayi silinemez
	if err := HesapSil("bayi5"); !errors.Is(err, ErrGecersizIstek) {
		t.Fatal("altinda hosting olan bayi silindi")
	}
	// hosting silinince bayi silinebilir
	if err := HesapSil("host1"); err != nil {
		t.Fatalf("hosting silme: %v", err)
	}
	if err := HesapSil("bayi5"); err != nil {
		t.Fatalf("bos bayi silme: %v", err)
	}
}

func TestHesaplariGetirHashSiyirir(t *testing.T) {
	hazirla(t)
	if err := HesapOlustur(Hesap{KullaniciAdi: "bayi6", Rol: RolReseller}, "parola123"); err != nil {
		t.Fatal(err)
	}
	hepsi, err := HesaplariGetir()
	if err != nil {
		t.Fatal(err)
	}
	if len(hepsi) != 1 || hepsi[0].ParolaHash != "" {
		t.Fatalf("HesaplariGetir hash sizdirdi veya sayi yanlis: %+v", hepsi)
	}
}
