package main

// rota_db_musteri_test.go — {dbid} türev kaynak uçlarının kapı kilidi. Yönlendirici
// main() içinde kurulduğu için main.go kaynak satırları okunur (rota_websec_test.go deseni).
//
// 🔴 2026-10-12 (.180): müşteri ekranındaki "Parola Sıfırla" / "Optimize Et" düğmeleri
// DBSahipligi (yalnız kök/bayi) arkasındaki uçlara gidiyordu → müşteri kendi DB'sinde
// 404 alıyordu. Kullanıcı kararıyla (silme dahil) hepsi DBSahipligiMusteri: kök her
// DB'de, bayi kendi hosting hesaplarında, hosting sahibi KENDİ DB'sinde (askı zorlamalı).

import (
	"os"
	"strings"
	"testing"
)

func TestDBTurevUclari_Kapilari(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	satirlar := strings.Split(string(kaynak), "\n")
	for _, u := range []struct{ yol, beklenen string }{
		{`"/databases/{dbid}"`, `r.With(middleware.DBSahipligiMusteri("dbid")).Delete("/databases/{dbid}", domainsH.DeleteDatabase)`},
		{`"/databases/{dbid}/password"`, `r.With(middleware.DBSahipligiMusteri("dbid")).Put("/databases/{dbid}/password", domainsH.SetDatabasePassword)`},
		{`"/databases/{dbid}/optimize"`, `r.With(middleware.DBSahipligiMusteri("dbid")).Post("/databases/{dbid}/optimize", domainsH.OptimizeDatabase)`},
		{`"/databases/{dbId}/pma-token"`, `r.With(middleware.DBSahipligiMusteri("dbId")).Post("/databases/{dbId}/pma-token", pmaH.TokenIste)`},
	} {
		var kayitlar []string
		for _, satir := range satirlar {
			if strings.Contains(satir, u.yol) {
				kayitlar = append(kayitlar, strings.TrimSpace(satir))
			}
		}
		if len(kayitlar) != 1 {
			t.Fatalf("%s: tek kayıt beklenir, %d bulundu: %q", u.yol, len(kayitlar), kayitlar)
		}
		if kayitlar[0] != u.beklenen {
			t.Errorf("%s kapısı yanlış:\n  var:    %s\n  olmalı: %s", u.yol, kayitlar[0], u.beklenen)
		}
	}
	// Yeni bir "/databases/{…}" türev ucu bu denetime girmeden eklenemez.
	n := 0
	for _, satir := range satirlar {
		if strings.Contains(satir, `"/databases/{db`) {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("/databases/{dbid} türev uç sayısı %d, beklenen 4 — yeni uç kapı denetiminden geçmeli", n)
	}
}
