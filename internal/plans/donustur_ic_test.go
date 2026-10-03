package plans

// donustur_ic_test.go — dönüşümün SON EMNİYETİ: kaynak satırı hostingin gerçek
// planıyla uyuşmazsa (ör. eşzamanlı düzenleme, hatalı çağrı) hesaplanan üst yazım
// efektif değeri değiştirir → değişmez denetimi işlemi REDDEDER.

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

func TestUstYazimaTasi_DegismezIhlalindeReddeder(t *testing.T) {
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
	ad := fmt.Sprintf("gosp_test_dnic_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sp []string
	for _, a := range efektiflimit.Alanlar {
		sp = append(sp, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, waf_enabled TINYINT NOT NULL DEFAULT 0, waf_mode VARCHAR(10) NOT NULL DEFAULT 'on', waf_paranoia TINYINT NOT NULL DEFAULT 1, " + strings.Join(sp, ", ") + ")",
		"CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL, disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0, waf_enabled TINYINT NULL, waf_mode VARCHAR(10) NULL, waf_paranoia TINYINT NULL)",
		efektiflimit.TabloDDL,
		"INSERT INTO service_plans (id, ram_mb) VALUES (1, 2048), (2, 1024)",
		"INSERT INTO domains (id, plan_id) VALUES (1, 1)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	ctx := context.Background()
	taban, err := planSatirOku(ctx, db, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	// Kaynak satırı hostingin GERÇEK planı (1, ram 2048) değil; ram 4096 sanılıyor.
	sahte, _ := planSatirOku(ctx, db, 1, false)
	sahte.limit["ram_mb"] = 4096
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, _, _, err = ustYazimaTasi(ctx, tx, 1, sahte, &taban, false, false, nil)
	if err == nil || !strings.Contains(err.Error(), "değişmez bozuldu") || !strings.Contains(err.Error(), "ram_mb") {
		t.Fatalf("değişmez ihlali reddedilmedi: %v", err)
	}
	// Doğru kaynakla geçer.
	gercek, _ := planSatirOku(ctx, db, 1, false)
	if _, _, _, err := ustYazimaTasi(ctx, tx, 1, gercek, &taban, false, false, nil); err != nil {
		t.Fatalf("doğru kaynak reddedildi: %v", err)
	}
}
