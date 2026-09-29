package main

// rota_websec_test.go — Website Security Monitor uçları bayiye açık: kök tüm sunucuyu,
// bayi YALNIZ kendi domainlerini görür/taratır. Kapsamı handler zorlar
// (internal/websec/kapsam.go); rota yalnız müşteri jetonunu dışarıda tutar.
//
// Yönlendirici main() içinde kurulduğu için testte ayağa kaldırılamaz; bu kilit
// main.go kaynağındaki kayıt satırlarını okur (rota_imza_guncelle_test.go deseni).
// Davranış (bayi kapsamı, kök regresyonu, kuyruk sınırları) internal/websec
// testlerinde sınanır.

import (
	"os"
	"strings"
	"testing"
)

func TestWebsecRotalari_AdminVeyaResellerArkasinda(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	satirlar := strings.Split(string(kaynak), "\n")
	uclar := []struct{ yontem, yol, handler string }{
		{"Get", "/websec/status", "websecH.Status"},
		{"Get", "/websec/findings", "websecH.Findings"},
		{"Get", "/websec/apps", "websecH.Apps"},
		{"Post", "/websec/rescan", "websecH.Rescan"},
		{"Post", "/websec/rescan-many", "websecH.RescanMany"},
	}
	for _, u := range uclar {
		var kayitlar []string
		for _, satir := range satirlar {
			if strings.Contains(satir, `"`+u.yol+`"`) {
				kayitlar = append(kayitlar, strings.TrimSpace(satir))
			}
		}
		if len(kayitlar) != 1 {
			t.Fatalf("%s: tek kayıt beklenir, %d bulundu: %q", u.yol, len(kayitlar), kayitlar)
		}
		beklenen := `r.With(middleware.AdminVeyaReseller).` + u.yontem + `("` + u.yol + `", ` + u.handler + `)`
		if kayitlar[0] != beklenen {
			t.Errorf("%s AdminVeyaReseller arkasında değil: %s", u.yol, kayitlar[0])
		}
	}
	// Yeni bir websec ucu kapsam gözden geçirilmeden eklenmesin: kayıt sayısı sabit.
	n := 0
	for _, satir := range satirlar {
		if strings.Contains(satir, `"/websec/`) {
			n++
		}
	}
	if n != len(uclar) {
		t.Fatalf("websec rota sayısı %d, beklenen %d — yeni uç kapsam denetiminden geçmeli", n, len(uclar))
	}
}
