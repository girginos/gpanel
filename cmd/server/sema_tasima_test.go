package main

// sema_tasima_test.go — semaDogrula 0109'un taşıma kolonlarını denetler: göç düşerse
// açılıştaki "SEMA EKSIK" satırı bu kolonları adıyla verir; göç koşunca susar (girdi
// adları göçün gerçekten ürettiği adlarla aynı).
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR. Test yalnız kendi açtığı
// gosp_test_sema_<zaman> veritabanını kullanır ve sonunda siler.

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func semaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	ad := fmt.Sprintf("gosp_test_sema_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	c := cfg.Clone()
	c.DBName = ad
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// semaTestGoc: runMigrations ile aynı ayrıştırma; temiz veritabanında hiçbir hata yutulmaz.
func semaTestGoc(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", dosya))
	if err != nil {
		t.Fatalf("göç okunamadı (%s): %v", dosya, err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		if s := strings.TrimSpace(satir); s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("GÖÇ HATASI (%s): %v\n%s", dosya, err, s)
			}
		}
	}
}

func TestSemaDogrula_Tasima0109(t *testing.T) {
	db := semaTestDB(t)
	var gunluk bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&gunluk)
	t.Cleanup(func() { log.SetOutput(eski) })

	kolonlar := []string{
		"tasima_isleri.sahip_rid", "tasima_isleri.baslatan_uid",
		"tasima_kalemleri.hesap_json", "tasima_kaynak_ayar.kod",
	}
	semaDogrula(db)
	for _, k := range kolonlar {
		if !strings.Contains(gunluk.String(), k) {
			t.Errorf("boş şemada %s eksik diye raporlanmadı — semaDogrula onu denetlemiyor: %q", k, gunluk.String())
		}
	}

	for _, f := range []string{"0051_tasima.sql", "0055_eklenti_lisans.sql", "0096_tasima_oturum.sql", "0109_tasima_kaynak_kapsam.sql"} {
		semaTestGoc(t, db, f)
	}
	gunluk.Reset()
	semaDogrula(db)
	for _, k := range kolonlar {
		if strings.Contains(gunluk.String(), k) {
			t.Errorf("0109 sonrası %s hâlâ eksik görünüyor — girdi göçün ürettiği adla uyuşmuyor: %q", k, gunluk.String())
		}
	}
}

// 0114: audit_log.adina_domain_id — boş şemada eksik raporlanır, göç sonrası raporlanmaz.
// Göç dosyasının gerçek MariaDB'de hatasız koştuğunu da kanıtlar.
func TestSemaDogrula_DenetimAdina0114(t *testing.T) {
	db := semaTestDB(t)
	var gunluk bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&gunluk)
	t.Cleanup(func() { log.SetOutput(eski) })

	const k = "audit_log.adina_domain_id"
	semaDogrula(db)
	if !strings.Contains(gunluk.String(), k) {
		t.Fatalf("boş şemada %s eksik diye raporlanmadı: %q", k, gunluk.String())
	}
	if _, err := db.Exec(`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	semaTestGoc(t, db, "0114_denetim_adina.sql")
	semaTestGoc(t, db, "0114_denetim_adina.sql") // idempotent
	gunluk.Reset()
	semaDogrula(db)
	if strings.Contains(gunluk.String(), k) {
		t.Fatalf("0114 sonrası %s hâlâ eksik görünüyor: %q", k, gunluk.String())
	}
}
