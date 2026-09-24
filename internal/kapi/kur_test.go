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
	// Rol portları kapı aralığından seçilir. (Eskiden burada "8443/7443/9443 ile
	// çakışmamalı" deniyordu: aralık dışı portlar için bu iddia hiç yanlış olamazdı.
	// İzli portları hariç tutma TestGecisAyariIzliPortuRolPortuYapmaz'da sınanır.)
	for _, rol := range Roller {
		if p := a.RolPortu(rol); p < PortAltSinir || p > PortUstSinir {
			t.Fatalf("%s portu kapı aralığı dışında: %d", rol, p)
		}
	}
	if err := a.Gecerli(); err != nil {
		t.Fatalf("geçersiz: %v", err)
	}
	if b, _ := GecisAyari(conf, nil, 1000, 0); b.EskiBitis != 1000+VarsayilanGecisGun*86400 {
		t.Fatal("gün < 1 → varsayılan geçiş süresi")
	}
}

// uygunKume: yalnız verilen portları uygun sayan uygun() — seçimi daraltır.
func uygunKume(portlar ...int) func(int) bool {
	k := map[int]bool{}
	for _, p := range portlar {
		k[p] = true
	}
	return func(p int) bool { return k[p] }
}

// GecisAyari rol portunu panel bloğunda İZİ olan bir porta (genel = eski kapı, yalnız
// loopback = SSH tüneli, erisim'in yoruma aldığı satır) VERMEMELİ: Uzlastir izi olan
// porta listen eklemez, rol kapısı hiç genel dinlenmezdi. İz portları kapı ARALIĞINDA
// seçildi ve uygun() seçimi iz portu + birkaç boş portla daraltıyor; böylece hariç
// tutma rastgelelikten bağımsız sınanır.
func TestGecisAyariIzliPortuRolPortuYapmaz(t *testing.T) {
	eskiG := geciciPortBaslangici
	t.Cleanup(func() { geciciPortBaslangici = eskiG })
	geciciPortBaslangici = func() int { return 32768 } // aralık makineden bağımsız

	const (
		genelP = 25000 // genel listen → eski kapı olur
		yerelP = 25001 // yalnız loopback (SSH tüneli)
		yorumP = 25002 // erisim masaüstü modunun yoruma aldığı satır
		bos1   = 25010
		bos2   = 25011
		bos3   = 25012
	)
	conf := "server {\n" +
		"    listen 25000 ssl;\n" +
		"    listen [::]:25000 ssl;\n" +
		"    listen 127.0.0.1:25001 ssl;\n" +
		"    # [erisim:masaustu] listen [::]:25002 ssl;\n" +
		"}"
	for _, tc := range []struct {
		ad string
		iz int
	}{
		{"genel (eski kapı)", genelP},
		{"yalnız loopback", yerelP},
		{"erisim-yorumlu", yorumP},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			// 1) İz portu + YALNIZ iki boş port: iz portu aday sayılmazsa üç rol sığmaz.
			//    Deterministik: hariç tutma bozulursa seçim BAŞARILI olur.
			if a, err := GecisAyari(conf, uygunKume(tc.iz, bos1, bos2), 1000, 30); err == nil {
				t.Fatalf("iz portu %d rol adayı sayıldı: admin=%d bayi=%d musteri=%d",
					tc.iz, a.Admin, a.Bayi, a.Musteri)
			} else if errors.Is(err, ErrDisariyaKapali) {
				t.Fatalf("yanlış hata: %v", err)
			}
			// 2) İz portu + üç boş port: roller TAM OLARAK boş portlar, iz portu seçilmez.
			a, err := GecisAyari(conf, uygunKume(tc.iz, bos1, bos2, bos3), 1000, 30)
			if err != nil {
				t.Fatal(err)
			}
			secilen := map[int]bool{}
			for _, rol := range Roller {
				p := a.RolPortu(rol)
				if p == tc.iz {
					t.Fatalf("%s kapısı izli porta verildi: %d", rol, p)
				}
				secilen[p] = true
			}
			if !secilen[bos1] || !secilen[bos2] || !secilen[bos3] {
				t.Fatalf("roller boş portlar olmalı, got admin=%d bayi=%d musteri=%d",
					a.Admin, a.Bayi, a.Musteri)
			}
			// Yalnız GENEL portlar eski kapı olur; loopback/yorumlu satır eski DEĞİL.
			if len(a.EskiPortlar) != 1 || a.EskiPortlar[0] != genelP {
				t.Fatalf("eski portlar yalnız [%d] olmalı, got %v", genelP, a.EskiPortlar)
			}
		})
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

// Belirli bir adrese (iç ağ/VPN IP'si) bağlı panel: rol kapıları joker listen'la eklenir,
// geçiş bu kısıtlamayı — yönetici yüzeyi dahil — sessizce kaldırırdı → atlanır.
func TestGecisAyariAdreseBagliAtlanir(t *testing.T) {
	conf := "server {\n    listen 10.8.0.1:8443 ssl;\n}"
	if _, err := GecisAyari(conf, nil, 1000, 30); !errors.Is(err, ErrAdreseBagli) {
		t.Fatalf("ErrAdreseBagli beklenir, got %v", err)
	}
	// Joker ve loopback adresler "adresli" değildir.
	for adr, bek := range map[string]bool{
		"8443": false, "0.0.0.0:8443": false, "*:8443": false, "[::]:8443": false,
		"127.0.0.1:9443": false, "[::1]:9443": false, "localhost:9443": false,
		"10.8.0.1:8443": true, "[fd00::1]:8443": true, "203.0.113.5:8443;": true,
	} {
		if got := adresBagli(adr); got != bek {
			t.Errorf("adresBagli(%q)=%v beklenen %v", adr, got, bek)
		}
	}
}
