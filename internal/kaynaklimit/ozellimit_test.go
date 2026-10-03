package kaynaklimit

// ozellimit_test.go — D1: cgroup/FPM/kota/governor okuyucuları hostinge özel üst
// yazımı (efektiflimit) kullanıyor. Yalnız GEÇİCİ MariaDB; systemd/xfs çağrılmaz.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/efektiflimit"
)

func ol_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_kl_ust_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
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
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(sp, ", ") + ")",
		"CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL, disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0)",
		"CREATE TABLE db_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE, db_user VARCHAR(64) NOT NULL)",
		efektiflimit.TabloDDL,
		"INSERT INTO service_plans (id, cpu_yuzde, ram_mb, max_process, io_agirlik, disk_kota_mb, inode_kota, pm_max_children, db_max_query_seconds, io_read_mbps) VALUES (1, 100, 2048, 60, 100, 1024, 50000, 4, 30, 0)",
		"INSERT INTO domains (id, sistem_kullanici, plan_id) VALUES (1, 'c_a', 1), (2, 'c_b', NULL)",
		"INSERT INTO hosting_limit_ust (domain_id, ram_mb, cpu_yuzde, io_read_mbps, db_max_query_seconds, disk_kota_mb) VALUES (1, 4096, 250, 40, 5, 8000)",
		"INSERT INTO hosting_limit_ust (domain_id, disk_kota_mb, ram_mb) VALUES (2, 300, 1024)",
		"INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (1, 'c_a_db', 'c_a_db')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func TestPlanLimitleriGetir_UstYazimiKullanir(t *testing.T) {
	db := ol_db(t)
	l, err := PlanLimitleriGetir(context.Background(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if l.RAMMB != 4096 || l.CPUYuzde != 250 || l.IOReadMBps != 40 || l.DBMaxQuerySeconds != 5 || l.DiskKotaMB != 8000 {
		t.Errorf("üst yazım cgroup limitlerine yansımadı: %+v", l)
	}
	if l.MaxProcess != 60 || l.PMMaxChildren != 4 || l.InodeKota != 50000 {
		t.Errorf("üst yazılmayan alan plandan gelmeli: %+v", l)
	}
	if lim, ok := governorLimiti(db, "c_a_db"); !ok || lim != 5 {
		t.Errorf("governor üst yazımı kullanmadı: %d %v", lim, ok)
	}
	// Plansız + üst yazım: slice limiti uygulanır (plansiz sayılmaz), kota üst yazımdan.
	l2, _ := PlanLimitleriGetir(context.Background(), db, 2)
	if l2.RAMMB != 1024 || l2.IOAgirlik != 100 {
		t.Errorf("plansız hosting üst yazımı: %+v", l2)
	}
	s, _ := efektiflimit.Oku(context.Background(), db, 2)
	if d, i := efektifKotaSonuc(s); d != 300 || i != varsayilanInode {
		t.Errorf("plansız hosting kotası: disk=%d inode=%d (beklenen 300/%d)", d, i, varsayilanInode)
	}
	s1, _ := efektiflimit.Oku(context.Background(), db, 1)
	if d, i := efektifKotaSonuc(s1); d != 8000 || i != 50000 {
		t.Errorf("planlı hosting kotası: disk=%d inode=%d", d, i)
	}
}
