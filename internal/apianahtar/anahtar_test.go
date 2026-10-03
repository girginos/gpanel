package apianahtar

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var bicim = regexp.MustCompile(`^gosp_[a-z2-7]{12}_[A-Za-z0-9_-]{43}$`)

func TestUretBicimVeOzet(t *testing.T) {
	gor := map[string]bool{}
	for i := 0; i < 200; i++ {
		u, err := Uret()
		if err != nil {
			t.Fatal(err)
		}
		if !bicim.MatchString(u.Tam) || len(u.Tam) != TamUzunluk {
			t.Fatalf("biçim: %q", u.Tam)
		}
		onek, ok := Ayristir(u.Tam)
		if !ok || onek != u.Onek {
			t.Fatalf("ayrıştırma: %v %q", ok, onek)
		}
		if len(u.Ozet) != 64 || u.Ozet != Ozet(u.Tam) || strings.Contains(u.Ozet, u.Tam[len(On)+OnekUzunluk+1:]) {
			t.Fatal("özet SHA-256 onaltılık olmalı ve gizliyi içermemeli")
		}
		if !strings.HasSuffix(u.Tam, u.Son4) {
			t.Fatal("son4")
		}
		if gor[u.Onek] {
			t.Fatal("önek tekrarı")
		}
		gor[u.Onek] = true
	}
}

func TestUretRastgeleHatasi(t *testing.T) {
	eski := randOku
	randOku = func([]byte) (int, error) { return 0, errors.New("yok") }
	defer func() { randOku = eski }()
	if _, err := Uret(); !errors.Is(err, ErrRastgele) {
		t.Fatalf("rastgele kaynak yokken anahtar ÜRETİLMEMELİ: %v", err)
	}
}

func TestAyristirBozuk(t *testing.T) {
	u, _ := Uret()
	for _, b := range []string{
		"", "gosp_", "eyJhbGciOi", u.Tam + "x", u.Tam[:len(u.Tam)-1],
		strings.Replace(u.Tam, "gosp_", "gpk__", 1),
		u.Tam[:len(On)] + strings.ToUpper(u.Onek) + u.Tam[len(On)+OnekUzunluk:],
		u.Tam[:len(On)+OnekUzunluk] + "-" + u.Tam[len(On)+OnekUzunluk+1:],
		u.Tam[:len(u.Tam)-1] + "=",
	} {
		if _, ok := Ayristir(b); ok {
			t.Errorf("bozuk kabul edildi: %q", b)
		}
	}
}

func TestOzetEsit(t *testing.T) {
	a := Ozet("x")
	if !OzetEsit(a, a) || OzetEsit(a, Ozet("y")) || OzetEsit(a, "") {
		t.Fatal("OzetEsit")
	}
}

func TestIzinleriKanonik(t *testing.T) {
	got, err := IzinleriKanonik([]string{"dns:oku", "hesap:oku", "hesap:yaz", " DNS:oku "}, RolBayi)
	if err != nil || strings.Join(got, ",") != "dns:oku,hesap:yaz" {
		t.Fatalf("kanonik: %v %v", got, err)
	}
	for ad, c := range map[string]struct {
		ham []string
		rol string
	}{
		"boş":                     {nil, RolKok},
		"bilinmeyen grup":         {[]string{"dosya:yaz"}, RolKok},
		"bilinmeyen erişim":       {[]string{"hesap:sil"}, RolKok},
		"bayi bayi grubunu":       {[]string{"bayi:oku"}, RolBayi},
		"yalnız-oku grubunda yaz": {[]string{"denetim:yaz"}, RolKok},
		"biçimsiz":                {[]string{"hesap"}, RolKok},
	} {
		if _, err := IzinleriKanonik(c.ham, c.rol); err == nil {
			t.Errorf("%s kabul edildi", ad)
		}
	}
	if _, err := IzinleriKanonik([]string{"bayi:yaz"}, RolKok); err != nil {
		t.Fatalf("kök bayi:yaz seçebilmeli: %v", err)
	}
}

func TestIPListesiKanonik(t *testing.T) {
	got, err := IPListesiKanonik([]string{"203.0.113.9, 203.0.113.9", "10.1.2.3/8", "2001:db8::1"}, true)
	if err != nil || strings.Join(got, ",") != "10.0.0.0/8,2001:db8::1/128,203.0.113.9/32" {
		t.Fatalf("kanonik: %v %v", got, err)
	}
	if _, err := IPListesiKanonik(nil, true); !errors.Is(err, ErrIPBos) {
		t.Fatalf("kökte boş liste reddedilmeli: %v", err)
	}
	if l, err := IPListesiKanonik(nil, false); err != nil || len(l) != 0 {
		t.Fatalf("bayide boş liste serbest: %v %v", l, err)
	}
	for _, g := range []string{"0.0.0.0/0", "::/0"} {
		if _, err := IPListesiKanonik([]string{g}, false); !errors.Is(err, ErrIPCokGenis) {
			t.Errorf("%s bayide de reddedilmeli: %v", g, err)
		}
	}
	for _, g := range []string{"1.0.0.0/7", "2001::/15"} {
		if _, err := IPListesiKanonik([]string{g}, true); !errors.Is(err, ErrIPCokGenis) {
			t.Errorf("%s kökte reddedilmeli: %v", g, err)
		}
	}
	for _, g := range []string{"300.1.1.1", "abc", "1.2.3.4/33", "fe80::1%eth0"} {
		if _, err := IPListesiKanonik([]string{g}, false); err == nil {
			t.Errorf("%s kabul edildi", g)
		}
	}
	var cok []string
	for i := 0; i < MaksIP+1; i++ {
		cok = append(cok, fmt.Sprintf("192.0.2.%d", i+1))
	}
	if _, err := IPListesiKanonik(cok, false); !errors.Is(err, ErrIPCok) {
		t.Errorf("üst sınır: %v", err)
	}
}

func TestIPIzinli(t *testing.T) {
	l := []string{"203.0.113.0/24", "2001:db8::/32"}
	for ip, b := range map[string]bool{
		"203.0.113.77": true, "203.0.114.1": false, "2001:db8:1::1": true, "::ffff:203.0.113.5": true,
		"": false, "x": false,
	} {
		if IPIzinli(l, ip) != b {
			t.Errorf("%q: %v bekleniyordu", ip, b)
		}
	}
	if !IPIzinli(nil, "") {
		t.Error("boş liste = her IP")
	}
}

func TestKararVarsayilanReddet(t *testing.T) {
	cases := []struct {
		desen, rol string
		izin       []string
		beklenen   int
	}{
		{"GET /api/v1/domains", RolBayi, []string{"hesap:oku"}, KararIzinli},
		{"POST /api/v1/domains", RolBayi, []string{"hesap:oku"}, KararIzinYetmez},
		{"POST /api/v1/domains", RolBayi, []string{"hesap:yaz"}, KararIzinli},
		{"GET /api/v1/domains", RolBayi, []string{"dns:yaz"}, KararIzinYetmez},
		{"GET /api/v1/resellers", RolBayi, []string{"bayi:yaz"}, KararIzinYetmez},
		{"GET /api/v1/resellers", RolKok, []string{"bayi:oku"}, KararIzinli},
		{"GET /api/v1/domains/{id}/files", RolKok, []string{"hesap:yaz"}, KararUcKapali},
		{"POST /api/v1/me/parola", RolKok, []string{"hesap:yaz"}, KararUcKapali},
		{"PATCH /api/v1/eklenti/{ad}/*", RolKok, []string{"hesap:yaz"}, KararUcKapali},
		{"GET /api/v1/api-anahtari/ben", RolBayi, nil, KararIzinli},
		{"", RolKok, []string{"hesap:yaz"}, KararUcKapali},
		{"GET /api/v1/domains/", RolKok, []string{"hesap:yaz"}, KararUcKapali},
	}
	for _, c := range cases {
		if got := Karar(c.desen, c.rol, c.izin); got != c.beklenen {
			t.Errorf("%q %s %v: %d (beklenen %d)", c.desen, c.rol, c.izin, got, c.beklenen)
		}
	}
}

// TestKimlikUclariAnahtaraKapali: asla sınıfı hiçbir izin tablosunda yer alamaz.
func TestKimlikUclariAnahtaraKapali(t *testing.T) {
	var hepsi []string
	for _, g := range Gruplar {
		hepsi = append(hepsi, g.Ad+":yaz")
	}
	for d := range KimlikUclari {
		if _, ok := Rotalar[d]; ok {
			t.Errorf("kimlik ucu izin tablosunda: %s", d)
		}
		if HerZaman[d] {
			t.Errorf("kimlik ucu HerZaman'da: %s", d)
		}
		if Karar(d, RolKok, hepsi) != KararUcKapali {
			t.Errorf("kimlik ucu açık: %s", d)
		}
	}
	// Her rota bilinen bir gruba bağlı ve erişim düzeyi geçerli.
	for d, k := range Rotalar {
		g, ok := GrupBul(k.Grup)
		if !ok || (k.Erisim != Oku && k.Erisim != Yaz) || (g.YalnizOku && k.Erisim == Yaz) {
			t.Errorf("geçersiz rota kuralı: %s %+v", d, k)
		}
		if !strings.HasPrefix(d, "GET /api/v1/") && !strings.HasPrefix(d, "POST /api/v1/") &&
			!strings.HasPrefix(d, "PUT /api/v1/") && !strings.HasPrefix(d, "DELETE /api/v1/") {
			t.Errorf("rota anahtarı biçimi: %q", d)
		}
	}
}

func TestDurum(t *testing.T) {
	k := &Kayit{SonKullanma: 200}
	if k.Durum(100) != DurumEtkin || !k.Gecerli(100) {
		t.Fatal("etkin")
	}
	if k.Durum(200) != DurumSuresi || k.Gecerli(200) {
		t.Fatal("süre sınırı dahil dolmuş sayılmalı")
	}
	k.IptalAt = 150
	if k.Durum(120) != DurumGecis || !k.Gecerli(120) {
		t.Fatal("geçiş")
	}
	if k.Durum(150) != DurumIptal || k.Gecerli(150) {
		t.Fatal("iptal")
	}
}
