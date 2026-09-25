package main

// rota_musteri_pano_test.go — müşteri panosunun alan adı uçları rota kaydında
// MusteriScope arkasında mı (müşteri yalnız kendi domaini, bayi yalnız kendi
// domainleri, yönetici hepsi)?
//
// Yönlendirici main() içinde kurulduğu için testte ayağa kaldırılamaz. Bu kilit
// main.go kaynağındaki kayıt satırını okur. Davranış testleri (403/200 matrisi)
// internal/kaynak ve internal/istatistik'te aynı zincirle koşar; bu test,
// üretim kaydının o zincirden sapmadığını garanti eder.

import (
	"os"
	"strings"
	"testing"
)

func TestMusteriPanoUclari_MusteriScopeArkasinda(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, yol := range []string{"/domains/{id}/kaynak", "/domains/{id}/canli", "/domains/{id}/trafik-gecmis"} {
		var kayitlar []string
		for _, satir := range strings.Split(string(kaynak), "\n") {
			if strings.Contains(satir, `"`+yol+`"`) {
				kayitlar = append(kayitlar, strings.TrimSpace(satir))
			}
		}
		if len(kayitlar) != 1 {
			t.Errorf("%s: tek kayıt beklenir, %d bulundu: %q", yol, len(kayitlar), kayitlar)
			continue
		}
		if !strings.HasPrefix(kayitlar[0], `r.With(middleware.MusteriScope).Get("`+yol+`", `) {
			t.Errorf("%s MusteriScope arkasında değil: %s", yol, kayitlar[0])
		}
	}
}
