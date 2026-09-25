package domains

// create_plan_test.go — bayi kapsamı (GL-04'ün Create yolu): yeni hosting açarken bayi
// kökün ya da başka kiracının hostinge ÖZEL planını seçemez; varsayılan plan seçimi de
// hostinge özel planı otomatik bağlamaz. Düzenek adalani_create_test.go ile ortak
// (gerçek MariaDB, GOSP_TEST_GECICI_DSN yoksa ATLANIR; yan etki kalkanları geçerli).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cp_olustur(t *testing.T, h *Handlers, tok, alan string, planID int64) (int, string) {
	t.Helper()
	g := map[string]any{"alan_adi": alan}
	if planID > 0 {
		g["plan_id"] = planID
	}
	b, _ := json.Marshal(g)
	req := httptest.NewRequest(http.MethodPost, "/domains", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	aa_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// cp_planlar: gerçek şemadaki gibi service_plans.domain_id (hostinge özel plan).
func cp_planlar(t *testing.T, h *Handlers) {
	t.Helper()
	aa_exec(t, h.DB, `ALTER TABLE service_plans ADD COLUMN domain_id BIGINT UNSIGNED NULL`)
	aa_exec(t, h.DB, `INSERT INTO service_plans (id, ad, varsayilan, reseller_id, domain_id) VALUES
		(100, 'kokdomain.test — Özel', 0, 0, 10),
		(101, 'Kök Katalog', 0, 0, NULL),
		(102, 'Bayi3 Planı', 0, 3, NULL),
		(103, 'bayi2.test — Özel', 0, 2, 20),
		(104, 'Bayi2 Katalog', 0, 2, NULL)`)
}

func TestCreate_BayiOzelVeYabanciPlaniSecemez(t *testing.T) {
	db := aa_db(t)
	provizyon := aa_kalkan(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	cp_planlar(t, h)
	bayi := aa_bayi(t, 2)
	for _, c := range []struct {
		ad   string
		plan int64
	}{
		{"kökün hostinge özel planı", 100},
		{"başka bayinin katalog planı", 102},
		{"kendi başka hostingine özel planı", 103},
		{"var olmayan plan (varlık sızmaz)", 999},
	} {
		kod, govde := cp_olustur(t, h, bayi, "yeni-bayi2.test", c.plan)
		if kod != http.StatusForbidden {
			t.Errorf("%s (plan %d): 403 beklenir, alınan %d %s", c.ad, c.plan, kod, govde)
		}
	}
	if n := provizyon(); n != 0 {
		t.Errorf("reddedilen %d istek sağlamaya ulaştı", n)
	}
	// Görebildiği katalog planları plan kapısından geçer (sonrası kalkana takılır).
	for _, plan := range []int64{101, 104} {
		if kod, govde := cp_olustur(t, h, bayi, "yeni-bayi2.test", plan); kod == http.StatusForbidden {
			t.Errorf("görebildiği katalog planı %d reddedilmemeli: %d %s", plan, kod, govde)
		}
	}
	// Yönetici kısıtsız (davranış değişmedi).
	if kod, govde := cp_olustur(t, h, aa_yonetici(t), "yeni-kok.test", 100); kod == http.StatusForbidden {
		t.Errorf("yönetici plan seçiminde kısıtlanmamalı: %d %s", kod, govde)
	}
}

// Varsayılan plan: tek "varsayılan" işaretli plan kökün hostinge özel planıysa bayinin
// yeni hostingine bağlanmaz (plansız devam eder) — önceden seçilip 403'e düşüyordu ya da
// (eski kontrolde) özel plan sessizce bağlanıyordu.
func TestCreate_VarsayilanPlanOzelPlanOlamaz(t *testing.T) {
	db := aa_db(t)
	aa_kalkan(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	cp_planlar(t, h)
	aa_exec(t, db, `UPDATE service_plans SET varsayilan=1 WHERE id=100`)
	if kod, govde := cp_olustur(t, h, aa_bayi(t, 2), "yeni-bayi2.test", 0); kod == http.StatusForbidden {
		t.Fatalf("hostinge özel plan varsayılan seçilmemeli (plansız devam): %d %s", kod, govde)
	}
}
