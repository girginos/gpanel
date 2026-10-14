package provisioner

// tenantfpm_opcache_test.go — per-tenant pool.conf'ta OPcache satırı yalnız KAPALIYKEN yazılır.
//
// `php_admin_flag[opcache.enable] = on` no-op'tur ve her yeni işçide "Zend OPcache can't be temporary
// enabled" uyarısı üretir (180: tenant günlüklerinde binlerce satır); kapatma ise çalışmalıdır (WHMCS).

import (
	"database/sql"
	"strings"
	"testing"
)

func TestTenantOpcacheSatiri(t *testing.T) {
	if got := tenantOpcacheSatiri(true); got != "" {
		t.Fatalf("OPcache açıkken satır yazılmamalı (no-op + uyarı seli): %q", got)
	}
	if got, want := tenantOpcacheSatiri(false), "php_admin_flag[opcache.enable] = off\n"; got != want {
		t.Fatalf("OPcache kapalıyken php_admin_flag off yazılmalı: %q", got)
	}
}

// Satır yok (domain/ayar kaydı yok → varsayılan = açık): havuzda opcache.enable ASLA geçmemeli.
func TestRenderTenantPool_OpcacheVarsayilanAcik_SatirYok(t *testing.T) {
	out := renderTenantPool(nil, "c_opctest", 0)
	if strings.Contains(out, "opcache") {
		t.Fatalf("varsayılan (açık) havuzda opcache satırı olmamalı:\n%s", out)
	}
	// komşu sertleştirme satırları yerinde kalır (yanlışlıkla blok düşmedi)
	for _, w := range []string{"php_admin_value[upload_max_filesize] = ", "php_admin_flag[log_errors] = on\n", "catch_workers_output = yes\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("havuzda %q olmalı:\n%s", w, out)
		}
	}
}

func ot_ayar(t *testing.T, db *sql.DB, opcache int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS php_settings (
		domain_id BIGINT UNSIGNED PRIMARY KEY,
		memory_limit VARCHAR(16) NOT NULL DEFAULT '256M',
		max_execution_time INT NOT NULL DEFAULT 30,
		max_input_time INT NOT NULL DEFAULT 60,
		post_max_size VARCHAR(16) NOT NULL DEFAULT '64M',
		upload_max_filesize VARCHAR(16) NOT NULL DEFAULT '64M',
		disable_functions TEXT NOT NULL,
		pm_strategy VARCHAR(16) NOT NULL DEFAULT 'ondemand',
		pm_max_requests INT NOT NULL DEFAULT 500,
		opcache_enable TINYINT NOT NULL DEFAULT 1,
		display_errors TINYINT NOT NULL DEFAULT 0,
		log_errors TINYINT NOT NULL DEFAULT 1,
		error_reporting VARCHAR(128) NOT NULL DEFAULT '',
		debug_mode TINYINT NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`REPLACE INTO php_settings (domain_id, disable_functions, opcache_enable) VALUES (1, '', ?)`, opcache); err != nil {
		t.Fatal(err)
	}
}

// php_settings.opcache_enable gerçek bir satırdan okunur: 0 → tam bir `off` satırı, 1 → hiç satır yok.
func TestRenderTenantPool_OpcacheAyarSatirdan_KapaliYazilirAcikYazilmaz(t *testing.T) {
	db := ha_panel(t)

	ot_ayar(t, db, 0)
	kapali := renderTenantPool(db, ha_sk, 1)
	if n := strings.Count(kapali, "opcache"); n != 1 || !strings.Contains(kapali, "php_admin_flag[opcache.enable] = off\n") {
		t.Fatalf("OPcache kapalıyken tam bir `php_admin_flag[opcache.enable] = off` satırı olmalı (opcache geçen=%d):\n%s", n, kapali)
	}
	if !strings.Contains(kapali, "php_admin_flag[log_errors] = ") {
		t.Fatalf("komşu satırlar korunmalı:\n%s", kapali)
	}

	ot_ayar(t, db, 1)
	if acik := renderTenantPool(db, ha_sk, 1); strings.Contains(acik, "opcache") {
		t.Fatalf("OPcache açıkken havuzda opcache satırı olmamalı:\n%s", acik)
	}
}
