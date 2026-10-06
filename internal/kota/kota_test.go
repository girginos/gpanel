package kota

// kota_test.go — DB sayısı kapısı EFEKTİF limiti (plan ⊕ hostinge özel üst yazım)
// uygular. Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır; yalnız kendi açtığı
// gosp_test_kota_<zaman> veritabanını kullanır ve DROP eder).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/efektiflimit"
)

func kotaDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_kota_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, max_db INT NOT NULL DEFAULT 0, max_domain INT NOT NULL DEFAULT 0)`,
		`CREATE TABLE customers (id BIGINT UNSIGNED PRIMARY KEY, plan_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, plan_id BIGINT UNSIGNED NULL, customer_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE db_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL)`,
		efektiflimit.TabloDDL,
		// plan 1: max_db=2 · plan 2: sınırsız
		`INSERT INTO service_plans (id, max_db) VALUES (1,2),(2,0)`,
		// 10: planlı, üst yazımsız · 11: planlı + üst yazım(5) · 12: plan 2'li üst yazım(1) · 13: plansız
		`INSERT INTO domains (id, plan_id) VALUES (10,1),(11,1),(12,2),(13,NULL)`,
		`INSERT INTO hosting_limit_ust (domain_id, max_db) VALUES (11,5),(12,1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

func dbEkle(t *testing.T, db *sql.DB, domainID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := db.Exec(`INSERT INTO db_accounts (domain_id, db_name) VALUES (?,?)`, domainID, fmt.Sprintf("d%d_%d", domainID, i)); err != nil {
			t.Fatal(err)
		}
	}
}

func limitHatasiMi(err error) bool {
	var lh *LimitHatasi
	return errors.As(err, &lh)
}

// Plan limiti (üst yazım yok): 2 DB var → üçüncüsü reddedilir; 1 DB varken kabul.
func TestCheckDBEklenebilir_PlanLimiti(t *testing.T) {
	db := kotaDB(t)
	ctx := context.Background()
	dbEkle(t, db, 10, 1)
	if err := CheckDBEklenebilir(ctx, db, 10); err != nil {
		t.Fatalf("1/2 kabul edilmeli: %v", err)
	}
	dbEkle(t, db, 10, 1)
	if err := CheckDBEklenebilir(ctx, db, 10); !limitHatasiMi(err) {
		t.Fatalf("2/2 plan limiti reddedilmeli: %v", err)
	}
}

// Üst yazım plandan YÜKSEK (5): plan 2'de dolsa da hosting 5'e kadar DB açabilir;
// üst yazım plandan DÜŞÜK (1): plan sınırsızken bile 1 DB'den sonra ret.
func TestCheckDBEklenebilir_UstYazimUygulanir(t *testing.T) {
	db := kotaDB(t)
	ctx := context.Background()
	dbEkle(t, db, 11, 3)
	if err := CheckDBEklenebilir(ctx, db, 11); err != nil {
		t.Fatalf("üst yazım 5, 3 DB var: kabul edilmeli (plan 2 tavanı geçerli olmamalı): %v", err)
	}
	dbEkle(t, db, 11, 2)
	if err := CheckDBEklenebilir(ctx, db, 11); !limitHatasiMi(err) {
		t.Fatalf("5/5 üst yazım limiti reddedilmeli: %v", err)
	}
	dbEkle(t, db, 12, 1)
	if err := CheckDBEklenebilir(ctx, db, 12); !limitHatasiMi(err) {
		t.Fatalf("plan sınırsız ama üst yazım 1: 1/1 reddedilmeli: %v", err)
	}
}

// 0 / NULL = sınırsız; olmayan domain yan etkisiz geçer; limit yalnız KENDİ hostingin DB'lerini sayar.
func TestCheckDBEklenebilir_SinirsizVeYalitim(t *testing.T) {
	db := kotaDB(t)
	ctx := context.Background()
	dbEkle(t, db, 13, 50)
	if err := CheckDBEklenebilir(ctx, db, 13); err != nil {
		t.Fatalf("plansız = sınırsız: %v", err)
	}
	if err := CheckDBEklenebilir(ctx, db, 9999); err != nil {
		t.Fatalf("olmayan domain: %v", err)
	}
	dbEkle(t, db, 10, 1)
	dbEkle(t, db, 11, 5) // başka hostingin DB'leri 10'u etkilememeli
	if err := CheckDBEklenebilir(ctx, db, 10); err != nil {
		t.Fatalf("komşunun DB'leri sayılmamalı: %v", err)
	}
}
