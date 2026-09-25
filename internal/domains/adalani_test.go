package domains

// adalani_test.go — ad-alanı doğrulayıcısı (AdAlaniDenetle / AdAlaniEkle) ve
// Create'teki kullanımı. DB'li kısımlar adalani_create_test.go'daki geçici şemayı ve
// yan etki kalkanlarını kullanır; sağlama burada ayrıca SAHTE (saglama) ile değişir.

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/provisioner"
)

// aa_panel: panel adlarını test değerine sabitler (gerçek _panel.conf okunmaz).
func aa_panel(t *testing.T, adlar ...string) {
	t.Helper()
	eski := panelAdlariAl
	panelAdlariAl = func() []string { return adlar }
	t.Cleanup(func() { panelAdlariAl = eski })
}

// aa_sahteSaglama: sağlamayı sahteyle değiştirir; yan etki YOK. oncesi, sahte
// sağlama sırasında çalışır (ör. yarışan başka kiracının kaydı).
func aa_sahteSaglama(t *testing.T, oncesi func()) (saglanan, geriAlinan *[]string) {
	t.Helper()
	var s, g []string
	eskiS, eskiG := saglama, saglamaGeriAl
	saglama = func(alan, php string) (*provisioner.Result, error) {
		s = append(s, alan)
		if oncesi != nil {
			oncesi()
		}
		return &provisioner.Result{SistemKullanici: "c_adalanitestyok", WebRoot: "/nonexistent/adalani-test"}, nil
	}
	saglamaGeriAl = func(alan, sk string) error {
		g = append(g, alan)
		return nil
	}
	t.Cleanup(func() { saglama, saglamaGeriAl = eskiS, eskiG })
	return &s, &g
}

func TestAdAlani_SafYardimcilar(t *testing.T) {
	if got, bek := ustAdlar("a.b.c.com"), []string{"b.c.com", "c.com", "com"}; !reflect.DeepEqual(got, bek) {
		t.Errorf("ustAdlar: %v, beklenen %v", got, bek)
	}
	if got := ustAdlar("tek"); len(got) != 0 {
		t.Errorf("ustAdlar(tek): %v", got)
	}
	for _, c := range []struct {
		a, b string
		alt  bool
	}{
		{"mail.x.com", "x.com", true}, {"x.com", "x.com", false}, {"ax.com", "x.com", false},
		{"a.b.x.com", "x.com", true}, {"x.com", "mail.x.com", false}, {".x.com", "x.com", false},
	} {
		if altAdMi(c.a, c.b) != c.alt {
			t.Errorf("altAdMi(%q,%q) != %v", c.a, c.b, c.alt)
		}
	}
	for _, c := range []struct {
		a, b   string
		hizmet bool
	}{
		{"www.x.com", "x.com", true}, {"webmail.x.com", "x.com", true}, {"autodiscover.x.com", "x.com", true},
		{"blog.x.com", "x.com", false}, {"mail.a.x.com", "x.com", false}, {"x.com", "x.com", false},
	} {
		if hizmetAdiMi(c.a, c.b) != c.hizmet {
			t.Errorf("hizmetAdiMi(%q,%q) != %v", c.a, c.b, c.hizmet)
		}
	}
	panel := []string{"148.251.169.181", "localhost", "127.0.0.1", "panel.ornek.test", "vm-noktasiz"}
	for _, c := range []struct {
		ad     string
		kiraci int64
		red    bool
	}{
		{"panel.ornek.test", 0, true}, {"x.panel.ornek.test", 0, true}, {"panel.ornek.test", 2, true},
		{"ornek.test", 2, true},        // bayi panel adının üst adını açamaz
		{"ornek.test", 0, false},       // yönetici açabilir (kendi ana alan adı)
		{"baska.ornek.test", 2, false}, // panel adıyla ilgisiz kardeş
		{"vm-noktasiz.test", 2, false}, // noktasız makine adı yok sayılır
		{"181.test", 2, false},
	} {
		if got := panelAdlariDenetle(c.ad, c.kiraci, panel) != nil; got != c.red {
			t.Errorf("panelAdlariDenetle(%q, %d) red=%v, beklenen %v", c.ad, c.kiraci, got, c.red)
		}
	}
}

// Kural tablosu (gerçek SQL). tur: "" izin, "kayitli", "capraz", "hizmet", "altalan", "panel".
func TestAdAlaniDenetle_Kurallar(t *testing.T) {
	db := aa_db(t)
	aa_panel(t, "148.251.169.181", "localhost", "panel.hostingadresi.test")
	ctx := context.Background()
	cases := []struct {
		ad     string
		kiraci int64
		tur    string
	}{
		// Bayi 2 → başka kiracının ad-alanı (GL-02 a/b, YK-01)
		{"mail.kokdomain.test", 2, "capraz"},
		{"webmail.kokdomain.test", 2, "capraz"},
		{"blog.kokdomain.test", 2, "capraz"},
		{"a.b.kokdomain.test", 2, "capraz"},
		{"shop.bayi3.test", 2, "capraz"},
		{"kurban.test", 2, "capraz"}, // kökün shop.kurban.test'inin ÜST adı
		{"MAIL.KokDomain.test.", 2, "capraz"},
		{"kokdomain.test", 2, "kayitli"},
		// Bayi 2 → panel adları (GL-02 c)
		{"panel.hostingadresi.test", 2, "panel"},
		{"x.panel.hostingadresi.test", 2, "panel"},
		{"hostingadresi.test", 2, "panel"},
		// Bayi 2 → kendi ad-alanı
		{"shop.bayi2.test", 2, ""}, // aynı kiracı: SERBEST (taşıma deseni)
		{"www.bayi2.test", 2, "hizmet"},
		{"mail.bayi2.test", 2, "hizmet"},
		{"autodiscover.bayi2.test", 2, "hizmet"},
		{"ikiz2.test", 2, "hizmet"}, // www.ikiz2.test zaten ayrı hosting
		{"sub.bayi2.test", 2, "altalan"},
		{"yeni-bayi2.test", 2, ""},
		// Kök (yönetici)
		{"shop2.kokdomain.test", 0, ""},
		{"www.kokdomain.test", 0, "hizmet"},
		{"blog.kokdomain.test", 0, "altalan"},
		{"sub.bayi2.test", 0, "capraz"},
		{"bayi2.test", 0, "kayitli"},
		{"hostingadresi.test", 0, ""}, // yönetici panel adının üst adını açabilir
		{"panel.hostingadresi.test", 0, "panel"},
		{"kurban.test", 0, ""}, // kökün kendi alt domaininin üst adı: aynı kiracı
		// Bayi 3 → bayi 2'nin adları
		{"x.bayi2.test", 3, "capraz"},
	}
	for _, c := range cases {
		err := AdAlaniDenetle(ctx, db, c.ad, c.kiraci)
		var ah *AdAlaniHatasi
		if c.tur == "" {
			if err != nil {
				t.Errorf("%q (kiracı %d): izin beklenir, hata: %v", c.ad, c.kiraci, err)
			}
			continue
		}
		if !errors.As(err, &ah) {
			t.Errorf("%q (kiracı %d): %s reddi beklenir, alınan %v", c.ad, c.kiraci, c.tur, err)
			continue
		}
		var tur string
		switch ah.Genel {
		case adAlaniMsjKayitli:
			tur = "kayitli"
		case adAlaniMsjCapraz:
			tur = "capraz"
		case adAlaniMsjPanel:
			tur = "panel"
		case adAlaniMsjAltAlan:
			tur = "altalan"
		default:
			if strings.Contains(ah.Genel, "hizmet adı") || strings.Contains(ah.Genel, "www adı") {
				tur = "hizmet"
			}
		}
		if tur != c.tur {
			t.Errorf("%q (kiracı %d): tür %q, beklenen %q (%v)", c.ad, c.kiraci, tur, c.tur, err)
		}
		// Bayiye giden metin başka kiracıyı ASLA adlandırmaz.
		if c.kiraci > 0 && tur == "capraz" {
			genel := ah.Mesaj(false)
			for _, s := range []string{"kokdomain", "kurban", "bayi3", "bayi2", "kök", "bayi #", "sahibi"} {
				if strings.Contains(genel, s) {
					t.Errorf("%q: bayi mesajı %q içeriyor: %s", c.ad, s, genel)
				}
			}
			if !strings.Contains(ah.Mesaj(true), "sahibi") {
				t.Errorf("%q: yönetici mesajı ayrıntı içermeli: %s", c.ad, ah.Mesaj(true))
			}
		}
	}
}

// AdAlaniEkle: denetim başarısızsa ekle ÇAĞRILMAZ; başarılıysa çağrılır.
func TestAdAlaniEkle_DenetimSonraEkle(t *testing.T) {
	db := aa_db(t)
	aa_panel(t)
	ctx := context.Background()
	cagrildi := false
	err := AdAlaniEkle(ctx, db, "mail.kokdomain.test", 2, func() error { cagrildi = true; return nil })
	var ah *AdAlaniHatasi
	if !errors.As(err, &ah) || cagrildi {
		t.Errorf("çakışmada ekle çağrılmamalı: err=%v çağrıldı=%v", err, cagrildi)
	}
	err = AdAlaniEkle(ctx, db, "temiz.test", 2, func() error { cagrildi = true; return nil })
	if err != nil || !cagrildi {
		t.Errorf("temiz adda ekle çağrılmalı: err=%v çağrıldı=%v", err, cagrildi)
	}
}

// 🔴 Yarış: sağlama sürerken başka kiracı üst adı açarsa INSERT anındaki ikinci
// denetim yakalar → 409 + sağlama geri alınır, satır yazılmaz.
func TestCreate_SaglamaSirasindaYarisanUstAd_409(t *testing.T) {
	db := aa_db(t)
	provizyon := aa_kalkan(t)
	aa_panel(t)
	saglanan, geriAlinan := aa_sahteSaglama(t, func() {
		aa_exec(t, db, `INSERT INTO domains (alan_adi, reseller_id) VALUES ('yarisma.test', 0)`)
	})
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	kod, govde := aa_olustur(t, h, aa_bayi(t, 2), "shop.yarisma.test")
	if kod != http.StatusConflict || !strings.Contains(govde, adAlaniMsjCapraz) {
		t.Errorf("yarışan üst ad: 409 + genel mesaj beklenir, alınan %d %s", kod, govde)
	}
	if len(*saglanan) != 1 || len(*geriAlinan) != 1 || (*geriAlinan)[0] != "shop.yarisma.test" {
		t.Errorf("sağlama bir kez yapılıp geri alınmalı: sağlanan=%v geri=%v", *saglanan, *geriAlinan)
	}
	if n := aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE alan_adi='shop.yarisma.test'`); n != 0 {
		t.Errorf("çakışan satır yazıldı")
	}
	if n := provizyon(); n != 0 {
		t.Errorf("gerçek sağlamaya %d kez ulaşıldı", n)
	}
}

// Aynı kiracının kendi alan adının alt adı SERBEST: denetim geçer, akış sağlamaya
// (sahte) ulaşır. (INSERT test şemasında düşer — kalkan 2 — bu yüzden 500 beklenir.)
func TestCreate_AyniKiracininAltAdiSerbest(t *testing.T) {
	db := aa_db(t)
	aa_kalkan(t)
	aa_panel(t)
	saglanan, _ := aa_sahteSaglama(t, nil)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	for _, c := range []struct {
		tok  string
		alan string
	}{{aa_bayi(t, 2), "shop.bayi2.test"}, {aa_yonetici(t), "wp.kokdomain.test"}} {
		kod, govde := aa_olustur(t, h, c.tok, c.alan)
		if kod == http.StatusConflict || kod == http.StatusForbidden || kod == http.StatusBadRequest {
			t.Errorf("%s: aynı kiracı reddedilmemeli: %d %s", c.alan, kod, govde)
		}
	}
	if len(*saglanan) != 2 {
		t.Errorf("iki istek de sağlamaya ulaşmalı: %v", *saglanan)
	}
}

// Kimliksiz bayi jetonu (rol reseller, anahtar 0) kök adına hosting AÇAMAZ.
func TestCreate_KimliksizBayi_403(t *testing.T) {
	db := aa_db(t)
	provizyon := aa_kalkan(t)
	aa_panel(t)
	saglanan, _ := aa_sahteSaglama(t, nil)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	tok := aa_bayi(t, 0)
	kod, govde := aa_olustur(t, h, tok, "shop.kokdomain.test")
	if kod != http.StatusForbidden {
		t.Errorf("kimliksiz bayi: 403 beklenir, alınan %d %s", kod, govde)
	}
	if len(*saglanan) != 0 || provizyon() != 0 {
		t.Errorf("kimliksiz bayi sağlamaya ulaştı")
	}
}
