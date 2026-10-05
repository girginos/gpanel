package main

// rota_sitekopya_kaldirildi_test.go — "Site kopyası / Copy Website" (staging kopya) özelliği
// KALDIRILDI (kullanıcı kararı 2026-10-05). /domains/{id}/kopya* uçları artık kayıtlı
// değildir (chi → 404) ve API anahtarı tablosunda yoktur (varsayılan-reddet: uç kapalı).
//
// Yönlendirici main() içinde kurulduğu için testte ayağa kaldırılamaz; kayıt satırları
// main.go kaynağından, anahtar kararı apianahtar.Karar ile sınanır (rota_websec_test.go deseni).

import (
	"os"
	"regexp"
	"testing"

	"girginospanel/internal/apianahtar"
)

func TestSiteKopyaUclariKayitsiz(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`"/domains/\{id\}/kopya[/"]|internal/sitekopya`); re.Match(kaynak) {
		t.Errorf("main.go: kaldırılan site-kopya ucu/paketi geri gelmiş: %q", re.Find(kaynak))
	}
}

func TestSiteKopyaUclariAnahtardaKapali(t *testing.T) {
	for _, d := range []string{
		"GET /api/v1/domains/{id}/kopya",
		"POST /api/v1/domains/{id}/kopya",
		"DELETE /api/v1/domains/{id}/kopya/{ad}",
	} {
		if _, ok := apianahtar.Rotalar[d]; ok {
			t.Errorf("izin tablosunda kaldırılan uç var: %s", d)
		}
		if k := apianahtar.Karar(d, apianahtar.RolKok, []string{"hesap:yaz"}); k == apianahtar.KararIzinli {
			t.Errorf("%s: anahtar izinli (varsayılan-reddet bozuk)", d)
		}
	}
}
