package tasima

// adalani_test.go — GL-02: site taşıma da alan adı açar; domains.Create ile AYNI
// ad-alanı kuralı (domains.AdAlaniDenetle / AdAlaniEkle) oluşacak domainin sahibine
// göre uygulanır.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR.
//
// 🔴 YAN ETKİ KALKANLARI: (1) sağlama sahteyle değişir; (2) provisioner yasak-listesi
// geri çağrısı çağrı yığınında provisioner.Provision varsa reddeder (sahte devreden
// çıkarılsa bile gerçek Provision useradd'e varmadan döner); (3) test şemasındaki
// domains tablosunda web_backend YOK → taşımanın INSERT'i her durumda düşer.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/domains"
	"girginospanel/internal/provisioner"
)

func ta_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_tasima_adalani_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
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
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, role VARCHAR(16) NOT NULL DEFAULT 'user')`,
		// 🔴 web_backend YOK: HesapAktar'ın INSERT'i burada HER ZAMAN düşer (kalkan 3).
		`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL UNIQUE, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			web_root VARCHAR(255) NULL, reseller_id BIGINT NOT NULL DEFAULT 0,
			ipv4 VARCHAR(45) NOT NULL DEFAULT '203.0.113.10') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE subdomanlar (id INT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			alt_ad VARCHAR(63) NOT NULL, tam_ad VARCHAR(253) NOT NULL, UNIQUE KEY uq_sub_tam (tam_ad))
			ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`INSERT INTO users (id, role) VALUES (1,'admin'), (7,'reseller')`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (10,'kokdomain.test',0), (20,'bayi7.test',7)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

func ta_provisionIcinde() bool {
	pc := make([]uintptr, 64)
	n := runtime.Callers(2, pc)
	fr := runtime.CallersFrames(pc[:n])
	for {
		f, daha := fr.Next()
		if f.Function == "girginospanel/internal/provisioner.Provision" {
			return true
		}
		if !daha {
			return false
		}
	}
}

// ta_kalkan: kalkan 1 + 2. Sahte sağlamanın ve gerçek Provision'a varan
// çağrıların listesini döner.
func ta_kalkan(t *testing.T, oncesi func()) (saglanan, geriAlinan *[]string, gercek *int) {
	t.Helper()
	var s, g []string
	var n int
	provisioner.SetYasakliKontrolu(func(string) (bool, string) {
		if ta_provisionIcinde() {
			n++
			return true, "test-kalkani"
		}
		return false, ""
	})
	eskiS, eskiG := saglama, saglamaGeriAl
	saglama = func(alan, php string) (*provisioner.Result, error) {
		s = append(s, alan)
		if oncesi != nil {
			oncesi()
		}
		return &provisioner.Result{SistemKullanici: "c_tasimatestyok", WebRoot: "/nonexistent/tasima-test"}, nil
	}
	saglamaGeriAl = func(alan, sk string) error {
		g = append(g, alan)
		return nil
	}
	t.Cleanup(func() {
		saglama, saglamaGeriAl = eskiS, eskiG
		provisioner.SetYasakliKontrolu(nil)
	})
	return &s, &g, &n
}

func ta_aktar(h *Handlers, alan string, rid int64) error {
	_, err := h.HesapAktar(context.Background(), &Kaynak{}, Hesap{AlanAdi: alan, PHPSurum: "8.3"},
		Ayarlar{ResellerID: rid}, func(string, ...any) {})
	return err
}

// 🔴 GL-02: taşıma, bir bayiye başka kiracının alt adını sağlayamaz; sağlamadan
// ÖNCE reddedilir.
func TestHesapAktar_BaskaKiracininAltAdi_Reddedilir(t *testing.T) {
	db := ta_db(t)
	saglanan, _, gercek := ta_kalkan(t, nil)
	h := &Handlers{DB: db}
	for _, c := range []struct {
		alan string
		rid  int64
	}{{"shop.kokdomain.test", 7}, {"webmail.kokdomain.test", 7}, {"x.bayi7.test", 0}} {
		err := ta_aktar(h, c.alan, c.rid)
		var ah *domains.AdAlaniHatasi
		if !errors.As(err, &ah) {
			t.Errorf("%s (sahip %d): ad-alanı reddi beklenir, alınan %v", c.alan, c.rid, err)
		}
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("ret sağlamadan önce olmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
}

// Aynı sahibin alt adı (cPanel/Plesk alt alanlarının ayrı domain olarak taşınması)
// SERBEST: akış sağlamaya ulaşır (INSERT test şemasında düşer).
func TestHesapAktar_AyniSahibinAltAdi_Serbest(t *testing.T) {
	db := ta_db(t)
	saglanan, _, gercek := ta_kalkan(t, nil)
	h := &Handlers{DB: db}
	err := ta_aktar(h, "wp.kokdomain.test", 0)
	var ah *domains.AdAlaniHatasi
	if errors.As(err, &ah) {
		t.Errorf("aynı sahip reddedilmemeli: %v", err)
	}
	if len(*saglanan) != 1 || *gercek != 0 {
		t.Errorf("sahte sağlamaya bir kez ulaşmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
	if err == nil || !strings.Contains(err.Error(), "domain kaydi") {
		t.Errorf("INSERT kalkanı çalışmalı (domain kaydi hatası): %v", err)
	}
}

// 🔴 Yarış: sağlama sürerken başka kiracı üst adı açarsa INSERT anındaki denetim
// yakalar; sağlama geri alınır.
func TestHesapAktar_YarisanUstAd_GeriAlinir(t *testing.T) {
	db := ta_db(t)
	saglanan, geriAlinan, gercek := ta_kalkan(t, func() {
		if _, err := db.Exec(`INSERT INTO domains (alan_adi, reseller_id) VALUES ('yaris.test', 0)`); err != nil {
			t.Errorf("yarış kaydı: %v", err)
		}
	})
	h := &Handlers{DB: db}
	err := ta_aktar(h, "a.yaris.test", 7)
	var ah *domains.AdAlaniHatasi
	if !errors.As(err, &ah) {
		t.Errorf("yarışan üst ad: ad-alanı reddi beklenir, alınan %v", err)
	}
	if len(*saglanan) != 1 || len(*geriAlinan) != 1 || *gercek != 0 {
		t.Errorf("sağlama bir kez yapılıp geri alınmalı: sağlanan=%v geri=%v gerçek=%d", *saglanan, *geriAlinan, *gercek)
	}
}
