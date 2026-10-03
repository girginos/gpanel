package php

// kok_sinir_db_test.go — kök sınırı + plan tavanı GERÇEK MariaDB ile
// (GOSP_TEST_GECICI_DSN; yoksa atlanır, canlı DB'ye asla gidilmez):
//   - müşterinin reddedilen isteği kaydı DEĞİŞTİRMEZ; kabul edilende iç alan ve
//     korumalı alanlar kayıttan korunur, izinli alan yazılır;
//   - paylaşılan havuz efektif limitle (plan ⊕ hostinge özel üst yazım) render
//     edilir: pm.max_children, upload/post (disk kotası), süreler;
//   - alt alan havuzu üst alan adının planıyla kırpılır, df tabanı düşmez.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/middleware"
)

const ksdb_ayarSutun = `memory_limit VARCHAR(32) NOT NULL, max_execution_time INT NOT NULL, max_input_time INT NOT NULL,
	post_max_size VARCHAR(32) NOT NULL, upload_max_filesize VARCHAR(32) NOT NULL, opcache_enable TINYINT NOT NULL,
	disable_functions TEXT NOT NULL, display_errors TINYINT NOT NULL, log_errors TINYINT NOT NULL,
	allow_url_fopen TINYINT NOT NULL, file_uploads TINYINT NOT NULL, short_open_tag TINYINT NOT NULL,
	error_reporting VARCHAR(255) NOT NULL, include_path VARCHAR(255) NOT NULL, open_basedir TEXT NOT NULL,
	session_save_path VARCHAR(255) NOT NULL, mail_force_extra_parameters VARCHAR(255) NOT NULL,
	pm_strategy VARCHAR(16) NOT NULL, pm_max_children INT NOT NULL, pm_max_requests INT NOT NULL,
	pm_start_servers INT NOT NULL, pm_min_spare_servers INT NOT NULL, pm_max_spare_servers INT NOT NULL,
	ek_direktifler TEXT NOT NULL, debug_mode TINYINT NOT NULL`

func ksdb(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_phpkok_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var sp []string
	for _, a := range efektiflimit.Alanlar {
		sp = append(sp, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL, " +
			"reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(sp, ", ") + ") ENGINE=InnoDB",
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL, reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL,
			php_surum VARCHAR(8) NOT NULL DEFAULT '8.3', is_demo TINYINT NOT NULL DEFAULT 0, askida TINYINT NOT NULL DEFAULT 0,
			disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		efektiflimit.TabloDDL,
		"CREATE TABLE php_settings (domain_id BIGINT UNSIGNED PRIMARY KEY, " + ksdb_ayarSutun + ") ENGINE=InnoDB",
		"CREATE TABLE subdomain_php_settings (subdomain_id BIGINT UNSIGNED PRIMARY KEY, " + ksdb_ayarSutun + ") ENGINE=InnoDB",
		`CREATE TABLE subdomanlar (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL) ENGINE=InnoDB`,
		// Plan: 6 işçi, 400 MB disk → upload tavanı 100M, post 200M.
		"INSERT INTO service_plans (id, ad, pm_max_children, ram_mb, disk_kota_mb) VALUES (1, 'P', 6, 1024, 400)",
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id) VALUES (42, 'kok.test', 'c_kok', 1)`,
		`INSERT INTO php_settings VALUES (42, '256M', 30, 60, '64M', '32M', 1, 'exec,system', 0, 1, 1, 1, 0,
			'E_ALL', '.:/usr/share/php', '/home/c_kok/:/tmp/', '', '', 'dynamic', 6, 500, 2, 1, 3, '', 0)`,
		`INSERT INTO subdomanlar VALUES (5, 42)`,
		`INSERT INTO subdomain_php_settings VALUES (5, '256M', 9000, 9000, '9000M', '5000M', 1, '', 0, 1, 1, 1, 0,
			'E_ALL', '.:/usr/share/php', '', '', '', 'static', 500, 500, 100, 50, 200, '', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

func ksdb_put(t *testing.T, db *sql.DB, tok, govde string) (int, string, *Settings) {
	t.Helper()
	e1, e3, e4 := tenantFPMAktif, havuzUygula, vhostUygula
	t.Cleanup(func() { tenantFPMAktif, havuzUygula, vhostUygula = e1, e3, e4 })
	var havuz *Settings
	tenantFPMAktif = func(string) bool { return false }
	havuzUygula = func(_ string, _ string, s Settings) (string, error) { havuz = &s; return "/run/sahte.sock", nil }
	vhostUygula = func(*sql.DB, int64, string, string) error { return nil }
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(ks_sir)).Put("/domains/{id}/php-settings", (&Handlers{DB: db}).PutAyarlar)
	req := httptest.NewRequest(http.MethodPut, "/domains/42/php-settings", strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), havuz
}

func TestPHPAyarDB_MusteriKokSiniri_VePlanTavani(t *testing.T) {
	db := ksdb(t)
	tok := ks_tokenlar(t)
	ctx := context.Background()

	// Ret: kayıt hiç değişmez (izinli memory_limit dahil).
	if kod, g, havuz := ksdb_put(t, db, tok["müşteri"], `{"ayarlar":{"pm_max_children":500,"memory_limit":"1G"}}`); kod != http.StatusForbidden || havuz != nil {
		t.Fatalf("403 beklenirdi: %d %s", kod, g)
	}
	if s, _ := Get(ctx, db, 42); s.MemoryLimit != "256M" || s.PMMaxChildren != 6 {
		t.Fatalf("ret sonrası kayıt değişmemeli: %+v", s)
	}

	// Kabul: izinli alanlar yazılır; iç/korumalı alanlar (gövdede YOK — ön yüz
	// göndermez) kayıttan korunur, boş değere düşmez.
	kod, g, havuz := ksdb_put(t, db, tok["müşteri"],
		`{"ayarlar":{"memory_limit":"1G","upload_max_filesize":"2000M","post_max_size":"8000M","max_execution_time":3000,"max_input_time":0,"pm_strategy":"dynamic"}}`)
	if kod != http.StatusOK || havuz == nil {
		t.Fatalf("200 beklenirdi: %d %s", kod, g)
	}
	s, _ := Get(ctx, db, 42)
	if s.MemoryLimit != "1G" || s.PMMaxChildren != 6 || s.PMStartServers != 2 || s.PMMaxSpareServers != 3 || s.DisableFunctions != "exec,system" ||
		s.OpenBasedir != "/home/c_kok/:/tmp/" {
		t.Errorf("kayıt: izinli alan yazılmalı, iç/korumalı korunmalı: %+v", s)
	}
	if havuz.PMMaxChildren != 6 || havuz.UploadMaxFilesize != "100M" || havuz.PostMaxSize != "200M" ||
		havuz.MaxExecutionTime != 300 || havuz.MaxInputTime != 600 || havuz.MemoryLimit != "1G" {
		t.Errorf("havuz plan tavanıyla render edilmeli (memory_limit bilerek serbest): %+v", *havuz)
	}

	// Hostinge özel üst yazım (kökün /limitler'i) efektif değerdir.
	if _, err := db.Exec(`INSERT INTO hosting_limit_ust (domain_id, pm_max_children) VALUES (42, 3)`); err != nil {
		t.Fatal(err)
	}
	// Yönetici kaydı 500 yazar, havuz yine efektif limiti (3) aşamaz.
	if kod, g, havuz := ksdb_put(t, db, tok["admin"], `{"ayarlar":{"pm_max_children":500,"memory_limit":"1G","disable_functions":"exec,system"}}`); kod != http.StatusOK || havuz == nil || havuz.PMMaxChildren != 3 {
		t.Fatalf("yönetici 200 + havuz efektif limit (3): %d %s %+v", kod, g, havuz)
	}
	if s, _ := Get(ctx, db, 42); s.PMMaxChildren != 500 {
		t.Errorf("yönetici kaydı yazılmalı: %d", s.PMMaxChildren)
	}

	// Alt alan havuzu: üst alanın efektif limiti + df tabanı (kayıtta df boş, 500 işçi).
	alt, err := altHavuzAyarlari(ctx, db, "c_kok", 5)
	if err != nil {
		t.Fatal(err)
	}
	if alt.PMMaxChildren != 3 || alt.PMStartServers != 1 || alt.PMMaxSpareServers != 1 ||
		alt.UploadMaxFilesize != "100M" || alt.PostMaxSize != "200M" || alt.MaxExecutionTime != 300 ||
		!strings.Contains(alt.DisableFunctions, "symlink") {
		t.Errorf("alt alan havuzu plan tavanı + df tabanı almalı: %+v", alt)
	}
}
