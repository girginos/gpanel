package main

// rota_imza_guncelle_test.go — POST /domains/{id}/antivirus/imza-guncelle SUNUCU
// GENELİ freshclam imza güncellemesini tetikler; kullanıcı kuralı gereği sunucu
// geneli eylem yalnız yöneticinindir. Uç MusteriScope arkasındaydı: bayi ve
// müşteri kendi alan adı kimliğiyle tetikleyebiliyordu.
//
// Yönlendirici main() içinde kurulduğu için testte ayağa kaldırılamaz; bu kilit
// main.go kaynağındaki kayıt satırını okur (rota_musteri_pano_test.go deseni).
// Davranış (403/200 matrisi, freshclam'ın hiç çalışmadığı) internal/antivirus
// TestImzaGuncelle_YalnizYonetici'de sınanır.

import (
	"os"
	"strings"
	"testing"
)

func TestImzaGuncelle_AdminOnlyArkasinda(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	const yol = "/domains/{id}/antivirus/imza-guncelle"
	var kayitlar []string
	for _, satir := range strings.Split(string(kaynak), "\n") {
		if strings.Contains(satir, `"`+yol+`"`) {
			kayitlar = append(kayitlar, strings.TrimSpace(satir))
		}
	}
	if len(kayitlar) != 1 {
		t.Fatalf("%s: tek kayıt beklenir, %d bulundu: %q", yol, len(kayitlar), kayitlar)
	}
	if !strings.HasPrefix(kayitlar[0], `r.With(middleware.AdminOnly).Post("`+yol+`", avH.ImzaGuncelle)`) {
		t.Errorf("%s AdminOnly arkasında değil: %s", yol, kayitlar[0])
	}
}
