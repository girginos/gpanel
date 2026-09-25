package toplu

// devir_plan_test.go — GL-03: sahiplik devrinden sonra hostingin planını YALNIZ
// yeni sahip (veya kök) yönetebilir; eski bayi planını düzenleyip yeni sahibin
// hostinginin kaynak limitlerini değiştiremez. Devir hostingin efektif
// limitlerini DEĞİŞTİRMEZ (veri/hizmet kaybı yok):
//
//   - eski bayinin katalog planı  → birebir kopya, bu hostinge ÖZEL, yeni sahip adına
//   - hostinge özel plan (yalnız o) → aynı plan, sahipliği yeni sahibe taşınır
//   - kökün global katalog planı   → dokunulmaz
//   - yalnız müşteri değişirse     → dokunulmaz
//
// Gerçek MariaDB (GOSP_TEST_GECICI_DSN) + devret'in sahte araçları (devir_test.go).
// 🔴 YAN ETKİ YOK: eski bayinin PUT /plans'ı yalnız, wafPlanReapply'nin hiçbir
// hosting seçmeyeceği planlarda yapılır (plan sahibiyle aynı kiracının hostingi
// yok); gövdelerde nginx direktifi yok.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"
)

// dt_servicePlansDDL — canlı şemanın (0006..0058 göçleri sonrası) birebir kopyası.
const dt_servicePlansDDL = "CREATE TABLE `service_plans` (" +
	"`id` bigint(20) unsigned NOT NULL AUTO_INCREMENT," +
	"`ad` varchar(64) NOT NULL," +
	"`aciklama` varchar(255) NOT NULL DEFAULT ''," +
	"`disk_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"`trafik_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"`max_domain` int(11) NOT NULL DEFAULT 0," +
	"`max_db` int(11) NOT NULL DEFAULT 0," +
	"`max_email` int(11) NOT NULL DEFAULT 0," +
	"`max_ftp` int(11) NOT NULL DEFAULT 0," +
	"`varsayilan` tinyint(1) NOT NULL DEFAULT 0," +
	"`created_at` timestamp NULL DEFAULT current_timestamp()," +
	"`updated_at` timestamp NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()," +
	"`cpu_yuzde` int(11) NOT NULL DEFAULT 100," +
	"`ram_mb` int(11) NOT NULL DEFAULT 512," +
	"`max_process` int(11) NOT NULL DEFAULT 50," +
	"`inode_kota` int(11) NOT NULL DEFAULT 50000," +
	"`io_agirlik` int(11) NOT NULL DEFAULT 100," +
	"`mysql_max_baglanti` int(11) NOT NULL DEFAULT 25," +
	"`php_surum` varchar(8) NOT NULL DEFAULT '8.3'," +
	"`fastcgi_cache` tinyint(1) NOT NULL DEFAULT 0," +
	"`client_max_body_mb` int(11) NOT NULL DEFAULT 64," +
	"`nginx_ek_direktifler` text DEFAULT NULL," +
	"`pm_max_children` int(11) NOT NULL DEFAULT 0," +
	"`io_read_mbps` int(11) NOT NULL DEFAULT 0," +
	"`io_write_mbps` int(11) NOT NULL DEFAULT 0," +
	"`io_read_iops` int(11) NOT NULL DEFAULT 0," +
	"`io_write_iops` int(11) NOT NULL DEFAULT 0," +
	"`db_max_queries_per_hour` int(11) NOT NULL DEFAULT 0," +
	"`db_max_updates_per_hour` int(11) NOT NULL DEFAULT 0," +
	"`db_max_query_seconds` int(11) NOT NULL DEFAULT 0," +
	"`waf_enabled` tinyint(4) NOT NULL DEFAULT 0," +
	"`waf_mode` varchar(10) NOT NULL DEFAULT 'on'," +
	"`waf_paranoia` tinyint(4) NOT NULL DEFAULT 1," +
	"`reseller_id` bigint(20) NOT NULL DEFAULT 0," +
	"`domain_id` bigint(20) unsigned DEFAULT NULL," +
	"`saatlik_mail_limiti` int(11) NOT NULL DEFAULT 0," +
	"`mail_kutu_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"PRIMARY KEY (`id`), UNIQUE KEY `ad` (`ad`), KEY `ix_sp_reseller` (`reseller_id`)," +
	"KEY `ix_service_plans_domain` (`domain_id`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

// dp_plan — her sütunu varsayılandan FARKLI bir plan (kopya doğruluğu ölçülebilsin).
func dp_plan(t *testing.T, db *sql.DB, id int64, ad string, sahip int64, domainID any) {
	t.Helper()
	dt_exec(t, db, `INSERT INTO service_plans (id, ad, aciklama, disk_kota_mb, trafik_kota_mb, max_domain, max_db,
		max_email, max_ftp, varsayilan, cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
		php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler, pm_max_children, io_read_mbps,
		io_write_mbps, io_read_iops, io_write_iops, db_max_queries_per_hour, db_max_updates_per_hour,
		db_max_query_seconds, waf_enabled, waf_mode, waf_paranoia, reseller_id, domain_id, saatlik_mail_limiti,
		mail_kutu_kota_mb)
		VALUES (?,?,?, 777,888, 3,4, 5,6, 1, 250,3072, 77,123456, 333,44, '8.2',1,99, 'add_header X-Test 1;', 9,
		11,12, 13,14, 15,16, 17, 1,'detect',3, ?,?, 18, 19)`,
		id, ad, "iç açıklama "+ad, sahip, domainID)
}

// dp_etkin — hostingin (plan_id üzerinden) efektif limitleri + planın sahibi/özel işareti.
type dp_etkin struct {
	planID, sahip       int64
	ozel                sql.NullInt64
	cpu, ram, dbq, disk int
}

func dp_etkinOku(t *testing.T, db *sql.DB, did int64) dp_etkin {
	t.Helper()
	var e dp_etkin
	if err := db.QueryRow(`SELECT p.id, p.reseller_id, p.domain_id, p.cpu_yuzde, p.ram_mb, p.db_max_query_seconds, p.disk_kota_mb
		FROM domains d JOIN service_plans p ON p.id=d.plan_id WHERE d.id=?`, did).
		Scan(&e.planID, &e.sahip, &e.ozel, &e.cpu, &e.ram, &e.dbq, &e.disk); err != nil {
		t.Fatalf("hosting %d efektif planı okunamadı: %v", did, err)
	}
	return e
}

// dp_satir — planın tüm sütunları (ad → değer), kimlik/sahiplik sütunları hariç.
func dp_satir(t *testing.T, db *sql.DB, id int64) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM service_plans WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		t.Fatalf("plan %d yok", id)
	}
	ham := make([]sql.RawBytes, len(cols))
	ptr := make([]any, len(cols))
	for i := range ham {
		ptr[i] = &ham[i]
	}
	if err := rows.Scan(ptr...); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for i, c := range cols {
		switch c {
		case "id", "ad", "aciklama", "varsayilan", "reseller_id", "domain_id", "created_at", "updated_at":
			continue
		}
		out[c] = string(ham[i])
	}
	return out
}

func dp_devret(t *testing.T, db *sql.DB, hedef int64) *devirSonuc {
	t.Helper()
	h := &Handlers{DB: db}
	ds, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root")
	if err != nil {
		t.Fatalf("devret: %v", err)
	}
	return ds
}

// dp_bayiPut — bayi jetonuyla PUT /plans/{id} (cmd/server zinciri).
func dp_bayiPut(t *testing.T, db *sql.DB, rid, planID int64, govde map[string]any) int {
	t.Helper()
	ph := &plans.Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(dt_sir))
		r.With(middleware.AdminVeyaReseller).Put("/plans/{id}", ph.Update)
	})
	jeton, err := auth.IssueResellerAt(dt_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(govde)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/plans/%d", planID), bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+jeton)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// 🔴 GL-03 (bulgudaki senaryo): eski bayinin, yalnız devredilen hostingde
// kullanılan katalog planı. Devirden sonra eski bayi planını düzenleyince
// YENİ sahibin hostinginin limitleri DEĞİŞMEMELİ.
func TestDevirPlan_EskiBayiYeniSahibinLimitleriniDegistiremez(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 202, "A Devir Artigi", 7, nil)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=202 WHERE id=?`, dt_did)
	once := dp_etkinOku(t, db, dt_did)

	dp_devret(t, db, 8)

	sonra := dp_etkinOku(t, db, dt_did)
	if sonra.sahip != 8 {
		t.Errorf("GL-03: devirden sonra hostingin planı hâlâ eski bayinin (plan %d sahibi %d, beklenen 8)", sonra.planID, sonra.sahip)
	}
	if sonra.cpu != once.cpu || sonra.ram != once.ram || sonra.dbq != once.dbq || sonra.disk != once.disk {
		t.Errorf("devir hostingin efektif limitlerini değiştirdi: önce %+v sonra %+v", once, sonra)
	}
	// Eski bayi kendi planını düzenler (yan etkisiz: plan artık hiçbir hostingde değil).
	kod := dp_bayiPut(t, db, 7, 202, map[string]any{"ad": "A Devir Artigi", "disk_kota_mb": 1000, "trafik_kota_mb": 1000,
		"cpu_yuzde": 5, "ram_mb": 64, "max_process": 2, "db_max_query_seconds": 1, "nginx_ek_direktifler": ""})
	son := dp_etkinOku(t, db, dt_did)
	if son.cpu != once.cpu || son.ram != once.ram || son.dbq != once.dbq {
		t.Errorf("GL-03: eski bayi (PUT → %d) YENİ sahibin hostinginin limitlerini değiştirdi: önce cpu=%d ram=%d dbq=%d, sonra cpu=%d ram=%d dbq=%d",
			kod, once.cpu, once.ram, once.dbq, son.cpu, son.ram, son.dbq)
	}
}

// Paylaşılan katalog planı: eski bayinin DİĞER hostingi planında kalır, devredilen
// hosting birebir kopyaya (yeni sahip adına, bu hostinge özel) geçer.
func TestDevirPlan_PaylasilanKatalogPlaniKopyalanir(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 200, "A Plani", 7, nil)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=200 WHERE id=?`, dt_did)
	dt_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES (31, 'a-diger.test', 7, 200)`)
	// Ad çakışması: "<alan> — Özel" zaten var → sıradaki ad seçilmeli.
	dp_plan(t, db, 900, "ornek.test — Özel", 0, 999)
	eski := dp_satir(t, db, 200)

	ds := dp_devret(t, db, 8)

	e := dp_etkinOku(t, db, dt_did)
	if e.planID == 200 || e.sahip != 8 || !e.ozel.Valid || e.ozel.Int64 != dt_did {
		t.Fatalf("GL-03: devredilen hosting kopyaya geçmedi: plan=%d sahip=%d ozel=%v", e.planID, e.sahip, e.ozel)
	}
	yeni := dp_satir(t, db, e.planID)
	for k, v := range eski {
		if yeni[k] != v {
			t.Errorf("kopya sütunu %s farklı: eski %q yeni %q", k, v, yeni[k])
		}
	}
	var ad, aciklama string
	var vars int
	_ = db.QueryRow(`SELECT ad, aciklama, varsayilan FROM service_plans WHERE id=?`, e.planID).Scan(&ad, &aciklama, &vars)
	if ad != "ornek.test — Özel 2" || aciklama != "Yalnız ornek.test için özel plan" || vars != 0 {
		t.Errorf("kopya adı/açıklaması: %q / %q / varsayilan=%d (eski bayinin plan adı sızmamalı)", ad, aciklama, vars)
	}
	// Eski plan ve eski bayinin diğer hostingi etkilenmedi.
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE id=200 AND reseller_id=7 AND domain_id IS NULL`); n != 1 {
		t.Errorf("eski katalog planı değişti")
	}
	if p := dp_etkinOku(t, db, 31).planID; p != 200 {
		t.Errorf("eski bayinin diğer hostingi planından koptu: %d", p)
	}
	t.Logf("devir uyarıları: %v", ds.Uyarilar)
}

// Hostinge özel plan (yalnız bu hosting): aynı plan kalır, sahipliği taşınır.
func TestDevirPlan_OzelPlanSahipligiTasinir(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 210, "ornek.test — Özel", 7, dt_did)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=210 WHERE id=?`, dt_did)
	eski := dp_satir(t, db, 210)

	dp_devret(t, db, 8)

	e := dp_etkinOku(t, db, dt_did)
	if e.planID != 210 || e.sahip != 8 {
		t.Fatalf("GL-03: özel plan yeni sahibe taşınmadı: plan=%d sahip=%d (beklenen 210/8)", e.planID, e.sahip)
	}
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM service_plans`); n != 1 {
		t.Errorf("gereksiz kopya üretildi: %d plan", n)
	}
	for k, v := range dp_satir(t, db, 210) {
		if eski[k] != v {
			t.Errorf("özel plan sütunu %s değişti: %q → %q", k, eski[k], v)
		}
	}
	// varsayilan işareti yeni sahibin kataloğunu bozmasın.
	if n := dt_sayi(t, db, `SELECT varsayilan FROM service_plans WHERE id=210`); n != 0 {
		t.Errorf("taşınan özel plan varsayılan işaretli kaldı")
	}
}

// Özel plan başka hostingde de kullanılıyorsa (eski veri) taşınmaz, kopyalanır:
// eski bayinin diğer hostingi yeni sahibin kontrolüne geçmemeli.
func TestDevirPlan_PaylasilanOzelPlanKopyalanir(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 210, "ornek.test — Özel", 7, dt_did)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=210 WHERE id=?`, dt_did)
	dt_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES (31, 'a-diger.test', 7, 210)`)

	dp_devret(t, db, 8)

	if e := dp_etkinOku(t, db, 31); e.planID != 210 || e.sahip != 7 {
		t.Errorf("eski bayinin diğer hostinginin planı yeni sahibe geçti: plan=%d sahip=%d", e.planID, e.sahip)
	}
	if e := dp_etkinOku(t, db, dt_did); e.planID == 210 || e.sahip != 8 || e.ozel.Int64 != dt_did {
		t.Errorf("devredilen hosting kopyaya geçmedi: %+v", e)
	}
}

// Kökün global katalog planı ve yalnız-müşteri devri: plana DOKUNULMAZ.
func TestDevirPlan_GlobalPlanVeMusteriDevrindeDokunulmaz(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 1, "Standart", 0, nil)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=1 WHERE id=?`, dt_did)

	dp_devret(t, db, 8)
	if e := dp_etkinOku(t, db, dt_did); e.planID != 1 {
		t.Errorf("global plan değiştirildi: %d", e.planID)
	}
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM service_plans`); n != 1 {
		t.Errorf("global planda gereksiz kopya: %d plan", n)
	}

	// Yalnız müşteri devri (bayi aynı): eski bayinin planına dokunulmaz.
	dp_plan(t, db, 202, "B Katalog", 8, nil)
	dt_exec(t, db, `UPDATE domains SET plan_id=202 WHERE id=?`, dt_did)
	h := &Handlers{DB: db}
	musteri := int64(0)
	if _, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, nil, &musteri, 1, "root"); err != nil {
		t.Fatalf("müşteri devri: %v", err)
	}
	if e := dp_etkinOku(t, db, dt_did); e.planID != 202 || e.sahip != 8 {
		t.Errorf("yalnız müşteri devrinde plan değişti: %+v", e)
	}
}

// Kökün ÖZEL planları: başka bir hostinge ait olan kopyalanır (kök planı bayiye
// geçmez); devredilen hostingin kendi özel planı ise sahipliğiyle taşınır.
func TestDevirPlan_KokOzelPlanlari(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dt_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (10, 'kok-site.test', 0)`)
	dp_plan(t, db, 100, "kok-site.test — Özel", 0, 10)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=100 WHERE id=?`, dt_did)

	dp_devret(t, db, 8)

	if e := dp_etkinOku(t, db, dt_did); e.planID == 100 || e.sahip != 8 || e.ozel.Int64 != dt_did {
		t.Errorf("kökün başka hostinge özel planı kopyalanmadı: %+v", e)
	}
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE id=100 AND reseller_id=0 AND domain_id=10`); n != 1 {
		t.Errorf("kökün özel planı değişti")
	}

	// Kökün kendi hostingi (özel planıyla) bayiye devredilir → plan onunla gider.
	dt_exec(t, db, `UPDATE domains SET reseller_id=0 WHERE id=?`, dt_did)
	dp_plan(t, db, 110, "ornek.test — Kök Özel", 0, dt_did)
	dt_exec(t, db, `UPDATE domains SET plan_id=110 WHERE id=?`, dt_did)
	dp_devret(t, db, 8)
	if e := dp_etkinOku(t, db, dt_did); e.planID != 110 || e.sahip != 8 {
		t.Errorf("kökün bu hostinge özel planı yeni sahibe taşınmadı: %+v", e)
	}
}

// Kök hesaba devir: eski bayinin katalog planı kök adına özel kopyaya geçer
// (eski bayi artık kökün hostinginin limitlerini yönetemez).
func TestDevirPlan_KokeDevir(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	dp_plan(t, db, 200, "A Plani", 7, nil)
	dt_exec(t, db, `UPDATE domains SET alan_adi='ornek.test', plan_id=200 WHERE id=?`, dt_did)

	dp_devret(t, db, 0)

	if e := dp_etkinOku(t, db, dt_did); e.planID == 200 || e.sahip != 0 || e.ozel.Int64 != dt_did {
		t.Errorf("köke devirde plan kök kapsamına alınmadı: %+v", e)
	}
}
