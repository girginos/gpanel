package reseller

// ozet_test.go — bayi panosu listeleri: son 10 alan adı (oluşturulma yeni→eski) ve
// dikkat kalemleri YALNIZ bayinin kendi hostinglerinden; başka bayinin/kökün
// hostingi sızmaz. Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/efektiflimit"
)

func ozGeciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_ozet_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	cfg.ParseTime = false
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := []string{
		`CREATE TABLE service_plans (id BIGINT PRIMARY KEY, ad VARCHAR(100) NOT NULL DEFAULT '', disk_kota_mb BIGINT NULL,
			trafik_kota_mb BIGINT NULL)`,
		efektiflimit.TabloDDL, // 0120: efektif limit (plan ⊕ üst yazım)
		`CREATE TABLE domains (id BIGINT PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL, reseller_id BIGINT NOT NULL DEFAULT 0,
			disk_kota_mb INT NOT NULL DEFAULT 0,
			plan_id BIGINT NULL, boyut_kb BIGINT NOT NULL DEFAULT 0, trafik_kb BIGINT NOT NULL DEFAULT 0, askida TINYINT NULL, is_demo TINYINT NOT NULL DEFAULT 0,
			ssl_aktif TINYINT NOT NULL DEFAULT 0, ssl_bitis DATETIME NULL, olusturulma DATETIME NOT NULL)`,
		`INSERT INTO service_plans (id, ad, disk_kota_mb, trafik_kota_mb) VALUES (1,'Mini',100,0)`,
	}
	for i := 1; i <= 12; i++ { // bayi 7: 12 hosting, oluşturulma gün gün artar
		q = append(q, fmt.Sprintf(`INSERT INTO domains (id, alan_adi, reseller_id, plan_id, olusturulma) VALUES (%d,'b7-%02d.test',7,1,'2026-01-%02d')`, i, i, i))
	}
	q = append(q,
		`INSERT INTO domains (id, alan_adi, reseller_id, olusturulma) VALUES (100,'yabanci.test',8,'2026-12-01'), (101,'kok.test',0,'2026-12-02')`,
		`UPDATE domains SET ssl_aktif=1, ssl_bitis='2020-01-01' WHERE id IN (3,100)`, // süresi dolmuş SSL
		`UPDATE domains SET boyut_kb=95*1024 WHERE id IN (4,101)`,                    // %95 disk
		`UPDATE domains SET askida=1 WHERE id IN (5,100)`,
	)
	for _, s := range q {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("şema: %v\n%s", err, s)
		}
	}
	return db
}

func TestPanoListeleri_SonOnVeDikkat_YalnizKendiKapsami(t *testing.T) {
	db := ozGeciciDB(t)
	h := &Handlers{DB: db}
	son, dikkat := h.panoListeleri(context.Background(), 7)
	if len(son) != 10 || son[0].AlanAdi != "b7-12.test" || son[9].AlanAdi != "b7-03.test" {
		t.Fatalf("son 10 yeni→eski beklenir: %+v", son)
	}
	for _, s := range son {
		if s.ID >= 100 {
			t.Fatalf("başka kapsamın hostingi listede: %+v", s)
		}
	}
	tur := map[string]Dikkat{}
	for _, d := range dikkat {
		if d.DomainID >= 100 {
			t.Fatalf("başka kapsamın dikkat kalemi sızdı: %+v", d)
		}
		tur[d.Tur] = d
	}
	if d, ok := tur["ssl_alinamadi"]; !ok || d.DomainID != 3 || d.Onem != "kritik" {
		t.Fatalf("SSL alınamadı kalemi (id 3) beklenir: %+v", dikkat)
	}
	if d, ok := tur["disk_yuksek"]; !ok || d.DomainID != 4 || d.Yuzde != 95 {
		t.Fatalf("disk %%95 kalemi (id 4) beklenir: %+v", dikkat)
	}
	if d, ok := tur["askida"]; !ok || d.Adet != 1 || len(d.Ornekler) != 1 || d.Ornekler[0] != "b7-05.test" {
		t.Fatalf("askı kalemi (yalnız b7-05) beklenir: %+v", dikkat)
	}
}
