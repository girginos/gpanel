package uc

// uc_kapsam_db_test.go — kullanıcı kararı 2026-10-04: herkes YALNIZ kendi API
// anahtarlarını görür/yönetir (kök dahil). Matris: kök, bayi A, bayi B, müşteri.

import (
	"fmt"
	"strings"
	"testing"

	"girginospanel/internal/auth"
)

func TestAPIAnahtarKapsamMatrisi(t *testing.T) {
	db := ucGeciciDB(t)
	h := ucRouter(db)
	jA, jB, jK := jwtBayi(t, 7, "bayi_a"), jwtBayi(t, 8, "bayi_b"), jwtKok(t)
	jM, err := auth.IssueAt(ucSecret, 3600, 55, "musteri1", "customer", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a := olustur(t, h, jA, map[string]any{"ad": "ANAHTAR-A", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-a"})
	b := olustur(t, h, jB, map[string]any{"ad": "ANAHTAR-B", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-b"})
	k := olustur(t, h, jK, map[string]any{"ad": "ANAHTAR-K", "izinler": []string{"hesap:oku"}, "izinli_ipler": []string{"192.0.2.0/24"}, "parola": "kok-parola"})

	gorenler := map[string]string{"A": jA, "B": jB, "K": jK}
	benim := map[string]string{"A": "ANAHTAR-A", "B": "ANAHTAR-B", "K": "ANAHTAR-K"}
	for kim, tok := range gorenler {
		for _, yol := range []string{"/api/v1/api-anahtarlari", "/api/v1/api-anahtarlari?sahip=bayiler", "/api/v1/api-anahtarlari?owner=1&user_id=7&sahip_id=8"} {
			s := ucIstek(t, h, "GET", yol, tok, nil)
			if s.kod != 200 || strings.Count(s.govde, "ANAHTAR-") != 1 || !strings.Contains(s.govde, benim[kim]) {
				t.Fatalf("%s yalnız kendi anahtarını görmeli (%s): %d %s", kim, yol, s.kod, s.govde)
			}
		}
	}
	// Müşteri: yönetim uçlarına hiç giremez.
	if s := ucIstek(t, h, "GET", "/api/v1/api-anahtarlari", jM, nil); s.kod == 200 {
		t.Fatalf("müşteri listeleyememeli: %d", s.kod)
	}

	// Çapraz erişim: her ordered çift → 404 (liste dışı uçlar), anahtar çalışmaya devam eder.
	id := map[string]int64{"A": a.Kayit.ID, "B": b.Kayit.ID, "K": k.Kayit.ID}
	for kim, tok := range gorenler {
		for sahip, kid := range id {
			if kim == sahip {
				continue
			}
			yol := fmt.Sprintf("/api/v1/api-anahtarlari/%d", kid)
			parola := map[string]any{"parola": "kok-parola", "onay_ad": "x"}
			for _, c := range [][2]string{{"POST", yol + "/iptal"}, {"POST", yol + "/yenile"}, {"DELETE", yol}} {
				if s := ucIstek(t, h, c[0], c[1], tok, parola); s.kod != 404 || strings.Contains(s.govde, "etkin") {
					t.Fatalf("%s→%s %s %s: 404 beklenirdi: %d %s", kim, sahip, c[0], c[1], s.kod, s.govde)
				}
			}
		}
	}
	// Var olmayan kimlik ile yabancı kimlik yanıtı birebir aynı (varlık sızmaz).
	y1 := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/iptal", b.Kayit.ID), jK, nil)
	y2 := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari/999999/iptal", jK, nil)
	if y1 != y2 {
		t.Fatalf("yabancı ile var olmayan yanıt farklı: %v %v", y1, y2)
	}
	for kim, tok := range map[string]string{"A": a.Anahtar, "B": b.Anahtar} {
		if s := ucIstek(t, h, "GET", "/api/v1/domains", tok, nil); s.kod != 200 {
			t.Fatalf("%s anahtarı etkilenmemeli: %d", kim, s.kod)
		}
	}
	// Kaldırılan "?sahip=bayiler" için ayrı uç yok: olmayan yollar 404/405.
	for _, c := range [][2]string{{"GET", "/api/v1/api-anahtarlari/bayiler"}, {"GET", "/api/v1/admin/api-anahtarlari"}, {"PUT", "/api/v1/api-anahtarlari"}, {"GET", fmt.Sprintf("/api/v1/api-anahtarlari/%d", a.Kayit.ID)}} {
		if s := ucIstek(t, h, c[0], c[1], jK, nil); s.kod != 404 && s.kod != 405 {
			t.Fatalf("%s %s 404/405 olmalı: %d", c[0], c[1], s.kod)
		}
	}
	// Kendi anahtarı: iptal çalışır.
	if s := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/iptal", a.Kayit.ID), jA, nil); s.kod != 200 {
		t.Fatalf("kendi anahtarını iptal: %d %s", s.kod, s.govde)
	}
}
