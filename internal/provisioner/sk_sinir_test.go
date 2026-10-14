package provisioner

import (
	"strings"
	"testing"

	"girginospanel/internal/kiraci"
)

// Kural (arşiv taşıma kararı 4): sk çakışmasında ad sonuna 2, 3, 4… eklenir; üst sınır vardır,
// sonuç her zaman geçerli bir kiracı adıdır (uzunluk dahil); tükenince boş döner ve Provision
// hesabı açmaz.
func TestBenzersizKullanici_SayacSiniriVeGecerlilik(t *testing.T) {
	eski := skKullanimda
	t.Cleanup(func() { skKullanimda = eski })

	dolu := map[string]bool{}
	skKullanimda = func(u string) bool { return dolu[u] }
	if u := BenzersizKullanici("ornek.com"); u != "c_ornek" {
		t.Fatalf("boşta: %q", u)
	}
	dolu["c_ornek"] = true
	if u := BenzersizKullanici("ornek.com"); u != "c_ornek2" {
		t.Fatalf("yalnız taban doluyken 2 beklenir: %q", u)
	}
	dolu["c_ornek2"], dolu["c_ornek3"] = true, true
	if u := BenzersizKullanici("ornek.com"); u != "c_ornek4" {
		t.Fatalf("2,3 doluyken 4 beklenir: %q", u)
	}
	// En uzun taban (24 karakter) + en büyük sayaç hâlâ geçerli sk (≤ 32).
	uzun := strings.Repeat("a", 40) + ".com"
	taban := SlugFromDomain(uzun)
	skKullanimda = func(u string) bool { return u != taban+"9999" }
	u := BenzersizKullanici(uzun)
	if u != taban+"9999" || !kiraci.GecerliSK(u) || len(u) > 32 {
		t.Fatalf("uzun taban + 9999: %q (%d)", u, len(u))
	}
	// Sınır: 9999 da doluysa boş döner (sonsuz döngü yok).
	skKullanimda = func(string) bool { return true }
	eskiKurulu := phpKuruluMu
	phpKuruluMu = func(string) bool { return true } // sunucudaki PHP kurulumundan bağımsız
	defer func() { phpKuruluMu = eskiKurulu }()
	if u := BenzersizKullanici("ornek.com"); u != "" {
		t.Fatalf("adlar tükenince boş beklenir: %q", u)
	}
	// Provision tükenen adla hesabı AÇMAZ (useradd'e varmaz).
	if _, err := Provision("ornek.com", "8.3"); err == nil || !strings.Contains(err.Error(), "sistem kullanıcı adı üretilemedi") {
		t.Fatalf("Provision tükenen adla reddetmeli: %v", err)
	}
}
