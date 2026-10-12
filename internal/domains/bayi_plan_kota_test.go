package domains

// bayi_plan_kota_test.go — kullanıcı bildirimi 2026-10-12: bayiye otomatik hosting
// planı ("<bayi> Varsayılan") artık AÇILMAZ. Kendi planı olmayan bayi yine hosting
// açabilmeli (kökün paylaşılan varsayılan planı bağlanır); kendi planı olunca onun
// varsayılanı bağlanır; kota planda değil BAYİ KAYDINDA (users.max_domain /
// max_disk_mb, fazla satış kapalı) ve hosting açılışında UYGULANIR. Başka bayinin
// planı bağlanamaz.
//
// Düzenek: olustur_kopuk_test.go (ok_db) + sahte sağlama/dış adımlar — useradd,
// nginx, BIND, FTP ve kaynaklimit'e ulaşılmaz. Gerçek MariaDB; GOSP_TEST_GECICI_DSN
// yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
)

// bk_dikisler — sağlama + sağlama sonrası dış adımlar sahte; sağlama çağrı sayacı döner.
func bk_dikisler(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	saglanan := 0
	eS, eG := saglama, saglamaGeriAl
	eK, eF, eSS, eT, eZ := olusturKaynakUygula, olusturFTP, olusturSertifikaSart, olusturDNSTohumla, olusturZoneYaz
	t.Cleanup(func() {
		saglama, saglamaGeriAl = eS, eG
		olusturKaynakUygula, olusturFTP, olusturSertifikaSart, olusturDNSTohumla, olusturZoneYaz = eK, eF, eSS, eT, eZ
	})
	saglama = func(alan, php string) (*provisioner.Result, error) {
		mu.Lock()
		saglanan++
		mu.Unlock()
		return &provisioner.Result{SistemKullanici: "c_bayiplantest", WebRoot: "/nonexistent/bayi-plan-test"}, nil
	}
	saglamaGeriAl = func(alan, sk string) error {
		t.Errorf("geri alma beklenmezdi (%s)", alan)
		return nil
	}
	olusturKaynakUygula = func(context.Context, *sql.DB, int64) error { return nil }
	olusturFTP = func(*sql.DB, int64, string, string, int, int) error { return nil }
	olusturSertifikaSart = func(string) (bool, string) { return false, "" }
	olusturDNSTohumla = func(context.Context, *sql.DB, int64, string, string) (int, error) { return 0, nil }
	olusturZoneYaz = func(context.Context, *sql.DB, int64) error { return nil }
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return saglanan
	}
}

func TestCreate_BayiPlansizKokVarsayilaniVeKotaUygulanir(t *testing.T) {
	db := ok_db(t)
	aa_panel(t)
	saglanan := bk_dikisler(t)
	for _, q := range []string{
		efektiflimit.TabloDDL,
		`ALTER TABLE service_plans ADD COLUMN domain_id BIGINT UNSIGNED NULL`,
		`ALTER TABLE domains ADD COLUMN disk_kota_mb INT NOT NULL DEFAULT 0`,
		`INSERT INTO service_plans (id, ad, varsayilan, reseller_id, disk_kota_mb, trafik_kota_mb) VALUES
			(1, 'Başlangıç', 1, 0, 1024, 5120), (2, 'Standart', 0, 0, 10240, 51200)`,
		// Bayi 2: 2 hostingi var (20, 21; plansız), hakkı 4, disk havuzu 5000 MB, fazla satış KAPALI.
		`UPDATE users SET max_domain=4, max_disk_mb=5000, fazla_satis=0 WHERE id=2`,
	} {
		aa_exec(t, db, q)
	}
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	bayi2 := aa_bayi(t, 2)
	planSayisi := aa_sayi(t, db, `SELECT COUNT(*) FROM service_plans`)
	planOf := func(alan string) int64 {
		t.Helper()
		var p sql.NullInt64
		if err := db.QueryRow(`SELECT plan_id FROM domains WHERE alan_adi=?`, alan).Scan(&p); err != nil {
			t.Fatalf("%s okunamadı: %v", alan, err)
		}
		return p.Int64
	}

	// 1) Kendi planı OLMAYAN bayi hosting açar → kökün paylaşılan varsayılanı (1).
	if kod, g := cp_olustur(t, h, bayi2, "b2-bir.test", 0); kod != http.StatusCreated {
		t.Fatalf("kendi planı olmayan bayi hosting açamadı: %d %s", kod, g)
	}
	if p := planOf("b2-bir.test"); p != 1 {
		t.Errorf("plansız bayi hostingi kökün varsayılanına bağlanmalı: plan=%d", p)
	}
	if n := aa_sayi(t, db, `SELECT COUNT(*) FROM service_plans`); n != planSayisi {
		t.Errorf("hosting açılışı bayiye plan açtı: %d → %d", planSayisi, n)
	}

	// 2) Bayinin KENDİ varsayılan planı varsa o bağlanır (kökünkinden önce).
	aa_exec(t, db, `INSERT INTO service_plans (id, ad, varsayilan, reseller_id, disk_kota_mb, trafik_kota_mb)
		VALUES (50, 'Bayi2 Kucuk', 1, 2, 3000, 1000), (60, 'Bayi3 Plan', 1, 3, 100, 100)`)
	if kod, g := cp_olustur(t, h, bayi2, "b2-iki.test", 0); kod != http.StatusCreated {
		t.Fatalf("bayi kendi planıyla hosting açamadı: %d %s", kod, g)
	}
	if p := planOf("b2-iki.test"); p != 50 {
		t.Errorf("bayinin kendi varsayılanı bağlanmalı: plan=%d", p)
	}
	once := saglanan()

	// 3) Hosting hakkı (bayi kaydı max_domain=4; 20, 21 + 2 yeni = 4) → dolu, sağlamaya varılmaz.
	if kod, g := cp_olustur(t, h, bayi2, "b2-uc.test", 0); kod != http.StatusForbidden || !strings.Contains(g, "limitiniz dolu") {
		t.Errorf("hosting hakkı uygulanmadı: %d %s", kod, g)
	}
	// 4) Disk havuzu (taahhüt 1024 + 3000; +3000 ≥ 5000) → yetersiz.
	aa_exec(t, db, `UPDATE users SET max_domain=10 WHERE id=2`)
	if kod, g := cp_olustur(t, h, bayi2, "b2-dort.test", 50); kod != http.StatusForbidden || !strings.Contains(g, "disk kotanız yetersiz") {
		t.Errorf("disk havuzu uygulanmadı: %d %s", kod, g)
	}
	// 5) Başka bayinin planı bağlanamaz.
	if kod, g := cp_olustur(t, h, bayi2, "b2-bes.test", 60); kod != http.StatusForbidden {
		t.Errorf("bayi 3'ün planı bayi 2'ye bağlandı: %d %s", kod, g)
	}
	if n := saglanan(); n != once {
		t.Errorf("reddedilen %d istek sağlamaya ulaştı", n-once)
	}
	if n := aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE reseller_id=2`); n != 4 {
		t.Errorf("bayi 2 hosting sayısı %d (beklenen 4)", n)
	}
	// Havuzun altında kalan plan yine geçer (kota DOĞRU hesaplanıyor; yalnız ret değil).
	if kod, g := cp_olustur(t, h, bayi2, "b2-alti.test", 2); kod != http.StatusForbidden {
		t.Errorf("Standart (10240 MB) havuzu (5000 MB) aşmalı: %d %s", kod, g)
	}
	aa_exec(t, db, `UPDATE service_plans SET disk_kota_mb=900 WHERE id=50`)
	if kod, g := cp_olustur(t, h, bayi2, "b2-alti.test", 50); kod != http.StatusCreated {
		t.Errorf("havuza sığan plan reddedildi: %d %s", kod, g)
	}
}

// Kökün menü sayacı (hizmet_planlari) GET /plans ile AYNI kapsam: yalnız kökün katalog
// planları (bayi planları kökün listesinde yok — sayaç da saymaz); bayi kendininkini.
func TestNavSayac_PlanSayisiKendiKapsami(t *testing.T) {
	db := aa_db(t)
	aa_exec(t, db, `ALTER TABLE service_plans ADD COLUMN domain_id BIGINT UNSIGNED NULL`)
	aa_exec(t, db, `INSERT INTO service_plans (id, ad, reseller_id, domain_id) VALUES
		(1, 'Kök 1', 0, NULL), (2, 'Kök 2', 0, NULL), (3, 'kokdomain.test — Özel', 0, 10),
		(50, 'Bayi2 A', 2, NULL), (51, 'Bayi2 B', 2, NULL), (60, 'Bayi3 A', 3, NULL)`)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(aa_sir)).Get("/nav-sayac", h.NavSayac)
	sayac := func(tok string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/nav-sayac", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var m map[string]int
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &m) != nil {
			t.Fatalf("nav sayaç %d %s", rec.Code, rec.Body.String())
		}
		return m["hizmet_planlari"]
	}
	if n := sayac(aa_yonetici(t)); n != 2 {
		t.Errorf("kök hizmet planı sayacı %d (beklenen 2 — bayi planları sayılmaz)", n)
	}
	if n := sayac(aa_bayi(t, 2)); n != 2 {
		t.Errorf("bayi 2 sayacı %d (beklenen 2)", n)
	}
	if n := sayac(aa_bayi(t, 3)); n != 1 {
		t.Errorf("bayi 3 sayacı %d (beklenen 1)", n)
	}
}
