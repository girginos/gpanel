package php

// versions_test.go — B11: GET /php/versions (ve /domains/{id}/php-settings'in
// "surumler" listesi) PHP-FPM pool/soket dizinlerini ve servis adlarını YALNIZ
// yöneticiye verir. Müşteri/bayi ön yüzünün kullandığı surum/aciklama/eol korunur,
// yanıt biçimi (anahtarlar) değişmez.
//
// Sürüm keşfi (dnf/php exec) tumSurumler dikişiyle sahte veriyle değiştirilir —
// test ana makineye dokunmaz.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/phpsurum"
)

var pv_sir = []byte("php-versions-test-anahtari-0123456789ab")

func pv_sahteSurumler(t *testing.T) {
	t.Helper()
	eski := tumSurumler
	t.Cleanup(func() { tumSurumler = eski })
	tumSurumler = func() []phpsurum.Surum {
		return []phpsurum.Surum{
			{SurumMeta: phpsurum.SurumMeta{Surum: "8.3", Kaynak: "appstream"}, Yuklu: true,
				PoolDir: "/etc/php-fpm.d", SockDir: "/run/php-fpm", Service: "php-fpm", Aciklama: "x"},
			{SurumMeta: phpsurum.SurumMeta{Surum: "7.4", Kaynak: "remi", EOL: true}, Yuklu: true,
				PoolDir: "/etc/opt/remi/php74/php-fpm.d", SockDir: "/var/opt/remi/php74/run/php-fpm",
				Service: "php74-php-fpm", Aciklama: "Legacy"},
			{SurumMeta: phpsurum.SurumMeta{Surum: "8.4", Kaynak: "remi"}, Yuklu: false,
				PoolDir: "/etc/opt/remi/php84/php-fpm.d"},
		}
	}
}

func pv_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(pv_sir)).Get("/php/versions", h.Versions)
	return r
}

func pv_getir(t *testing.T, tok string) ([]map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/php/versions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	pv_router(&Handlers{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /php/versions: %d %s", rec.Code, rec.Body.String())
	}
	var l []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("yanıt çözülemedi: %v", err)
	}
	return l, rec.Body.String()
}

func pv_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(pv_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(pv_sir, 3600, 7, "bayi", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(pv_sir, auth.MusteriClaims{FTPHesapID: 5, DomainID: 42, Kullanici: "m"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

// 🔴 B11: müşteri ve bayi iç yolları/servis adlarını görmez; ön yüzün kullandığı
// alanlar ve anahtar kümesi aynı kalır. Yönetici tam listeyi görür.
func TestVersions_IcYollarYalnizYoneticiye(t *testing.T) {
	pv_sahteSurumler(t)
	admin, bayi, musteri := pv_tokenlar(t)

	for ad, tok := range map[string]string{"müşteri": musteri, "bayi": bayi} {
		l, govde := pv_getir(t, tok)
		if len(l) != 2 {
			t.Fatalf("%s: 2 kurulu sürüm beklenir, gelen=%d", ad, len(l))
		}
		for _, sizinti := range []string{"/etc/", "/run/", "/var/", "php-fpm"} {
			if strings.Contains(govde, sizinti) {
				t.Errorf("B11: %s yanıtında iç yapı bilgisi var (%q): %s", ad, sizinti, govde)
			}
		}
		for _, s := range l {
			for _, k := range []string{"pool_dir", "sock_dir", "service"} {
				v, ok := s[k]
				if !ok {
					t.Errorf("%s: %q anahtarı kalkmış — yanıt biçimi değişmemeli", ad, k)
				} else if v != "" {
					t.Errorf("B11: %s yanıtında %s=%v", ad, k, v)
				}
			}
		}
		if l[0]["surum"] != "8.3" || l[0]["aciklama"] != "AppStream · OPcache" || l[0]["eol"] != false {
			t.Errorf("%s: ön yüz alanları bozuldu: %v", ad, l[0])
		}
		if l[1]["surum"] != "7.4" || l[1]["aciklama"] != "Remi · Legacy" || l[1]["eol"] != true {
			t.Errorf("%s: ön yüz alanları bozuldu: %v", ad, l[1])
		}
	}

	l, _ := pv_getir(t, admin)
	if len(l) != 2 || l[1]["pool_dir"] != "/etc/opt/remi/php74/php-fpm.d" ||
		l[1]["sock_dir"] != "/var/opt/remi/php74/run/php-fpm" || l[1]["service"] != "php74-php-fpm" {
		t.Fatalf("yönetici tam listeyi görmeli: %v", l)
	}
}

// GET /domains/{id}/php-settings aynı sürüm listesini müşteriye (MusteriScope)
// döndürür — aynı süzgeçten geçmeli. DB'li; GOSP_TEST_GECICI_DSN yoksa atlanır.
// Test yalnız kendi açtığı gosp_test_php_<zaman> veritabanını kullanır, sonunda DROP eder.
func TestGetAyarlar_SurumlerIcYollarYalnizYoneticiye(t *testing.T) {
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
	ad := fmt.Sprintf("gosp_test_php_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı: %v", err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	kolonlar := []string{"memory_limit", "max_execution_time", "max_input_time", "post_max_size",
		"upload_max_filesize", "opcache_enable", "disable_functions", "display_errors", "log_errors",
		"allow_url_fopen", "file_uploads", "short_open_tag", "error_reporting", "include_path",
		"open_basedir", "session_save_path", "mail_force_extra_parameters", "pm_strategy",
		"pm_max_children", "pm_max_requests", "pm_start_servers", "pm_min_spare_servers",
		"pm_max_spare_servers", "ek_direktifler", "debug_mode"}
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255), sistem_kullanici VARCHAR(64), php_surum VARCHAR(8))`,
		`CREATE TABLE php_settings (domain_id BIGINT UNSIGNED PRIMARY KEY, ` + strings.Join(kolonlar, " TEXT NULL, ") + ` TEXT NULL)`,
		`INSERT INTO domains VALUES (42, 'musteri.test', 'musteri', '8.3')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	pv_sahteSurumler(t)
	// `php -m` çalıştırılmasın: modül önbelleğini doldur.
	modulOnbellekMu.Lock()
	modulOnbellek["/usr/bin/php"] = modulKayit{moduller: []string{"Core"}, zaman: time.Now()}
	modulOnbellekMu.Unlock()

	admin, _, musteri := pv_tokenlar(t)
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(pv_sir), middleware.MusteriScope).
		Get("/domains/{id}/php-settings", (&Handlers{DB: db}).GetAyarlar)
	getir := func(tok string) (string, []map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/domains/42/php-settings", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET php-settings: %d %s", rec.Code, rec.Body.String())
		}
		var y struct {
			Surumler []map[string]any `json:"surumler"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &y); err != nil {
			t.Fatal(err)
		}
		return rec.Body.String(), y.Surumler
	}
	govde, l := getir(musteri)
	if len(l) != 2 {
		t.Fatalf("müşteri 2 sürüm görmeli: %v", l)
	}
	for _, s := range l {
		for _, k := range []string{"pool_dir", "sock_dir", "service"} {
			if v, ok := s[k]; !ok || v != "" {
				t.Errorf("B11: müşteri php-settings surumler[%v].%s=%v (anahtar=%v)", s["surum"], k, v, ok)
			}
		}
	}
	if strings.Contains(govde, "/etc/opt/remi") || strings.Contains(govde, "php74-php-fpm") {
		t.Errorf("B11: müşteri php-settings yanıtında iç yol/servis adı: %s", govde)
	}
	if _, la := getir(admin); la[1]["service"] != "php74-php-fpm" {
		t.Errorf("yönetici php-settings tam listeyi görmeli: %v", la)
	}
}
