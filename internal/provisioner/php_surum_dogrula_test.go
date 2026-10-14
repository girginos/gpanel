package provisioner

import (
	"errors"
	"strings"
	"testing"
)

// PHPSurumDogrula: istemciden gelen php_surum değeri yönetilen listede mi.
// Geçersiz değer eskiden SESSİZCE 8.3 havuzuna düşer, DB ham değeri tutardı.
func TestPHPSurumDogrula_Tablo(t *testing.T) {
	gecerli := []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5", "8.6"}
	for _, v := range gecerli {
		got, err := PHPSurumDogrula(v)
		if err != nil || got != v {
			t.Errorf("PHPSurumDogrula(%q) = (%q,%v); geçerli sürüm kabul edilmeli", v, got, err)
		}
	}
	// Boşluk kırpılır, kanonik değer döner.
	if got, err := PHPSurumDogrula("  8.2\t"); err != nil || got != "8.2" {
		t.Errorf("boşluklu geçerli değer kırpılmalı: (%q,%v)", got, err)
	}

	gecersiz := []string{
		"",       // boş: anlamı çağırana aittir, doğrulayıcıda geçersiz
		"   ",    // yalnız boşluk
		"8.30",   // WHMCS'te sık görülen yazım hatası
		"php8.9", // paket adı + olmayan sürüm
		"8.33",   // iki haneli küçük sürüm
		"08.3",   // baştaki sıfır
		"v8.3",   // tanınmayan önek
		"830",    // üç hane
		"8",      // yalnız ana sürüm
		"php",
		"-8.3",
		"8.9",      // olmayan sürüm
		"9.0",      // gelecek sürüm
		"5.6",      // çok eski
		"8,3",      // virgül
		"8.3;rm",   // enjeksiyon denemesi
		"../8.3",   // yol gezintisi
		"8.3\n8.2", // satır sonu
		"abc",
		"null",
	}
	for _, v := range gecersiz {
		got, err := PHPSurumDogrula(v)
		if err == nil {
			t.Errorf("PHPSurumDogrula(%q) = %q; geçersiz değer REDDEDİLMELİ", v, got)
			continue
		}
		if !errors.Is(err, ErrPHPSurumGecersiz) {
			t.Errorf("PHPSurumDogrula(%q) hatası ErrPHPSurumGecersiz sarmalamalı: %v", v, err)
		}
		if got != "" {
			t.Errorf("PHPSurumDogrula(%q) hata ile birlikte değer döndürmemeli: %q", v, got)
		}
		// Hata metni istemciye gider: desteklenen sürümler listelenmeli.
		if !strings.Contains(err.Error(), "7.4") || !strings.Contains(err.Error(), "8.6") {
			t.Errorf("hata metni desteklenen sürümleri listelemeli: %v", err)
		}
	}
}

// normalizePHP ile tutarlılık: doğrulayıcının kabul ettiği her değer normalizePHP'de
// DEĞİŞMEZ (havuz ile DB aynı sürümde), reddettiği değerler normalizePHP'de 8.3'e düşer
// (eski sessiz ayrışmanın tam tarifi — doğrulayıcı olmadan DB ile havuz ayrışırdı).
func TestPHPSurumDogrula_NormalizeIleTutarli(t *testing.T) {
	for _, v := range PHPSurumleri() {
		got, err := PHPSurumDogrula(v)
		if err != nil || normalizePHP(got) != got {
			t.Errorf("%s: doğrulanan değer normalizePHP'de değişmemeli (%q → %q, err=%v)", v, got, normalizePHP(got), err)
		}
	}
	for _, v := range []string{"8.30", "php8.9", "8.9"} {
		if _, err := PHPSurumDogrula(v); err == nil {
			t.Fatalf("%q reddedilmeliydi", v)
		}
		if normalizePHP(v) != "8.3" {
			t.Errorf("normalizePHP(%q) sessizce 8.3'e düşmeli (varsayım değişti → doğrulayıcıyı gözden geçir)", v)
		}
	}
}

// PHPSurumleri artan sırada ve phpMap'in TÜM anahtarlarını taşır; hata metni aynı
// sırayla listeler (istemci "desteklenen sürümler: 7.4, 8.0, …" okur).
func TestPHPSurumleri_SiraliVeTam(t *testing.T) {
	got := PHPSurumleri()
	if len(got) != len(phpMap) {
		t.Fatalf("PHPSurumleri %d sürüm, phpMap %d", len(got), len(phpMap))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("artan sırada olmalı: %v", got)
		}
	}
	_, err := PHPSurumDogrula("8.99")
	if err == nil || !strings.HasSuffix(err.Error(), strings.Join(got, ", ")) {
		t.Errorf("hata metni sıralı listeyle bitmeli (%s): %v", strings.Join(got, ", "), err)
	}
}

// Açık yazımlar (WHMCS ürün ayarı serbest metindi) kanonik sürüme indirgenir; eski panel
// bunları sessizce 8.3 yapıyordu. WHMCS modülü (Yardimci::phpSurum) aynı tabloyu sınar.
func TestPHPSurumDogrula_AcikYazimlarKanoniklesir(t *testing.T) {
	for v, beklenen := range map[string]string{
		"83": "8.3", "81": "8.1", "74": "7.4", "php83": "8.3", "php81": "8.1", "PHP81": "8.1",
		"PHP 8.1": "8.1", "php-8.2": "8.2", "PHP_7.4": "7.4", "php8.4": "8.4",
		"8.3.31": "8.3", "8.1.2": "8.1", "php8.2.30": "8.2", " php 8.5 ": "8.5",
	} {
		got, err := PHPSurumDogrula(v)
		if err != nil || got != beklenen {
			t.Errorf("PHPSurumDogrula(%q) = (%q,%v); %q beklenirdi", v, got, err, beklenen)
		}
	}
}
