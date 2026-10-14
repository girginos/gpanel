package provisioner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func psh_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_psh_%d", time.Now().UnixNano())
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
	// ek_alan_adlari BİLEREK yok: eski şemada tablo eksikse atlanmalı.
	for _, q := range []string{
		`CREATE TABLE domains (id INT AUTO_INCREMENT PRIMARY KEY, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3')`,
		`CREATE TABLE subdomanlar (id INT AUTO_INCREMENT PRIMARY KEY, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3')`,
		`CREATE TABLE service_plans (id INT AUTO_INCREMENT PRIMARY KEY, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3')`,
		`CREATE TABLE cp_laravel_apps (id INT AUTO_INCREMENT PRIMARY KEY, php_surum VARCHAR(8) NOT NULL DEFAULT '')`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL, ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail JSON NULL, ok TINYINT NOT NULL DEFAULT 1,
			reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO domains (php_surum) VALUES ('8.2'), ('8.2 '), ('php83'), ('9.9'), ('8.3'), ('')`,
		`INSERT INTO subdomanlar (php_surum) VALUES ('7.4'), (' 8.1')`,
		`INSERT INTO service_plans (php_surum) VALUES ('PHP 8.3')`,
		`INSERT INTO cp_laravel_apps (php_surum) VALUES (''), ('php82'), ('8.2')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func psh_degerler(t *testing.T, db *sql.DB, tablo string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT CAST(php_surum AS BINARY) FROM ` + tablo + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

// Geçersiz değer, kaydın BUGÜN kullandığı sürüme çekilir; boşluklu geçerli değer kırpılır
// (PAD SPACE harmanlamasında "8.2 " = "8.2" sayılır — ikili karşılaştırma şart); boş değere
// dokunulmaz; Laravel'de geçersiz → boş (sistem PHP'si). İkinci koşu hiçbir şey değiştirmez.
func TestPHPSurumHeal(t *testing.T) {
	db := psh_db(t)
	ctx := context.Background()
	n, err := PHPSurumHeal(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Errorf("düzeltilen kayıt %d, beklenen 6", n)
	}
	for tablo, bekl := range map[string][]string{
		"domains":         {"8.2", "8.2", "8.3", "8.3", "8.3", ""},
		"subdomanlar":     {"7.4", "8.1"},
		"service_plans":   {"8.3"},
		"cp_laravel_apps": {"", "", "8.2"},
	} {
		if g := psh_degerler(t, db, tablo); !reflect.DeepEqual(g, bekl) {
			t.Errorf("%s: %q, beklenen %q", tablo, g, bekl)
		}
	}
	var denetim int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='php_surum.duzeltildi'`).Scan(&denetim); err != nil {
		t.Fatal(err)
	}
	if denetim != 6 {
		t.Errorf("denetim satırı %d, beklenen 6 (her ayrı değer için bir)", denetim)
	}
	if n, err := PHPSurumHeal(ctx, db); err != nil || n != 0 {
		t.Errorf("ikinci koşu idempotent değil: n=%d err=%v", n, err)
	}
}

func TestPHPSurumHedef(t *testing.T) {
	for _, d := range []struct {
		v, hedef string
		laravel  bool
		degisir  bool
	}{
		{"8.3", "8.3", false, false}, {"8.2 ", "8.2", false, true}, {"php83", "8.3", false, true},
		{"7.4", "7.4", false, false}, {"php82", "", true, true}, {"8.2", "8.2", true, false},
	} {
		h, deg := phpSurumHedef(d.v, d.laravel)
		if h != d.hedef || deg != d.degisir {
			t.Errorf("phpSurumHedef(%q, %v) = %q,%v; beklenen %q,%v", d.v, d.laravel, h, deg, d.hedef, d.degisir)
		}
	}
}
