package main

// rota_giris_parola_test.go — "Şifre değiştir" ucu YALNIZ BAYİ içindir (kullanıcı kararı
// 2026-10-04). Yönlendirici main() içinde kurulduğu için testte ayağa kaldırılamaz; bu kilit
// main.go kaynağındaki kayıt satırını okur (rota_websec_test.go deseni). Davranış (kök 403,
// bayi sahip 200, başka bayi 404, müşteri/adına-oturum reddi) internal/domains
// giris_parola_test.go içinde sınanır.

import (
	"os"
	"strings"
	"testing"
)

func TestGirisParolaRotasi_YalnizBayi(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	var kayitlar []string
	for _, satir := range strings.Split(string(kaynak), "\n") {
		if strings.Contains(satir, `"/domains/{id}/giris-parola"`) {
			kayitlar = append(kayitlar, strings.TrimSpace(satir))
		}
	}
	if len(kayitlar) != 1 {
		t.Fatalf("tek kayıt beklenir, %d bulundu: %q", len(kayitlar), kayitlar)
	}
	const beklenen = `r.With(middleware.RequireRole("reseller")).Put("/domains/{id}/giris-parola", domainsH.GirisParola)`
	if kayitlar[0] != beklenen {
		t.Errorf("giris-parola YALNIZ bayi kapısı arkasında olmalı (kök 403): %s", kayitlar[0])
	}
}
