package kapi

import (
	"errors"
	"testing"
)

func TestKurulumAyari(t *testing.T) {
	a, err := KurulumAyari(nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Gecerli(); err != nil {
		t.Fatalf("üretilen ayar geçersiz: %v", err)
	}
	if len(a.EskiPortlar) != 0 {
		t.Fatal("taze kurulumda eski kapı olmamalı")
	}
}

func TestGecisAyariTumGenelPortlarEskiOlur(t *testing.T) {
	conf := "server {\n    listen 127.0.0.1:9443 ssl;\n    listen 8443 ssl;\n    listen 7443 ssl;\n    listen [::]:8443 ssl;\n}"
	a, err := GecisAyari(conf, nil, 1000, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.EskiPortlar) != 2 || a.EskiPortlar[0] != 8443 || a.EskiPortlar[1] != 7443 {
		t.Fatalf("TÜM genel portlar eski olmalı (loopback hariç), got %v", a.EskiPortlar)
	}
	if a.EskiBitis != 1000+30*86400 {
		t.Fatalf("bitiş yanlış: %d", a.EskiBitis)
	}
	for _, rol := range Roller {
		if p := a.RolPortu(rol); p == 8443 || p == 7443 || p == 9443 {
			t.Fatalf("%s portu mevcut bir portla çakışmamalı: %d", rol, p)
		}
	}
	if err := a.Gecerli(); err != nil {
		t.Fatalf("geçersiz: %v", err)
	}
	if b, _ := GecisAyari(conf, nil, 1000, 0); b.EskiBitis != 1000+VarsayilanGecisGun*86400 {
		t.Fatal("gün < 1 → varsayılan geçiş süresi")
	}
}

// Operatör paneli dışarıya kapattıysa (yalnız loopback) geçiş 3 genel port açıp
// bu seçimi tersine çevirmemeli.
func TestGecisAyariDisariyaKapaliAtlanir(t *testing.T) {
	conf := "server {\n    listen 127.0.0.1:9443 ssl;\n    # [erisim:masaustu] listen [::]:9443 ssl;\n}"
	if _, err := GecisAyari(conf, nil, 1000, 30); !errors.Is(err, ErrDisariyaKapali) {
		t.Fatalf("ErrDisariyaKapali beklenir, got %v", err)
	}
}
