package kaynaklimit

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

// 🔴 Governor sorgu sahibinin planını harf DUYARLI ve tek-domain koşuluyla okur: eski
// sürümden kalan büyük harfli varyant satır ('K_X_DB', düşük limitli başka kiracının
// planı) komşunun 'k_x_db' sorgularına uygulanıp KILL QUERY attırmamalı; iki domainin
// satırında geçen (devralınmış) ad için hiçbir plan uygulanmaz.
// Yalnız GEÇİCİ MariaDB (GOSP_TEST_GECICI_DSN); mysql CLI / KILL çağrılmaz.
func TestGovernorLimiti_HarfVaryantiVeCokDomain(t *testing.T) {
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
	defer kok.Close()
	ad := fmt.Sprintf("gosp_test_gov_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE service_plans (id BIGINT PRIMARY KEY, db_max_query_seconds INT NULL)`,
		`CREATE TABLE domains (id BIGINT PRIMARY KEY, plan_id BIGINT NULL)`,
		`CREATE TABLE db_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE, db_user VARCHAR(64) NOT NULL)`,
		`INSERT INTO service_plans VALUES (10, 1), (20, 30)`,
		`INSERT INTO domains VALUES (1, 10), (2, 20)`,
		`INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES
			(1, 'k_saldiri', 'K_X_DB'), (2, 'k_x_main', 'k_x_db'), (2, 'k_x_blog', 'k_x_db'),
			(1, 'k_eski', 'iki_db'), (2, 'k_x_iki', 'iki_db')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	for _, c := range []struct {
		kul   string
		limit int
		ok    bool
	}{
		{"k_x_db", 30, true}, // kurbanın kendi planı (varyant satır karışmaz; iki DB aynı domain)
		{"K_X_DB", 1, true},  // varyant hesabın kendi planı
		{"iki_db", 0, false}, // iki domaine kayıtlı → sahibi belirsiz → KILL yok
		{"yok_db", 0, false}, // panelde kayıtsız
	} {
		limit, ok := governorLimiti(db, c.kul)
		if limit != c.limit || ok != c.ok {
			t.Errorf("governorLimiti(%q) = (%d,%v), beklenen (%d,%v)", c.kul, limit, ok, c.limit, c.ok)
		}
	}
}

// TestKotaLimitArgs: xfs_quota arg-slice'ının doğru kurulduğunu (soft=hard*0.95, 0=sınırsız,
// -u user quota, kök mount) DOĞRULAR. Shell yok — arg-slice bütünlüğü kritiktir.
func TestKotaLimitArgs(t *testing.T) {
	cases := []struct {
		sk           string
		diskMB       int
		inode        int
		wantLimitArg string
	}{
		// tam limit: soft = %95
		{"c_ornek", 5120, 500000, "limit -u bsoft=4864m bhard=5120m isoft=475000 ihard=500000 c_ornek"},
		// disk limit + inode sınırsız (0)
		{"c_foo", 1024, 0, "limit -u bsoft=972m bhard=1024m isoft=0 ihard=0 c_foo"},
		// her ikisi sınırsız (0 = limit yok)
		{"c_bar", 0, 0, "limit -u bsoft=0m bhard=0m isoft=0 ihard=0 c_bar"},
		// yalnız inode limiti
		{"c_baz", 0, 100000, "limit -u bsoft=0m bhard=0m isoft=95000 ihard=100000 c_baz"},
		// negatif → 0'a sıkıştır
		{"c_neg", -5, -9, "limit -u bsoft=0m bhard=0m isoft=0 ihard=0 c_neg"},
	}
	for _, c := range cases {
		got := kotaLimitArgs(c.sk, c.diskMB, c.inode)
		want := []string{"-x", "-c", c.wantLimitArg, "/"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("kotaLimitArgs(%q,%d,%d)\n  got  = %#v\n  want = %#v", c.sk, c.diskMB, c.inode, got, want)
		}
	}
}

// TestReKotaSK: sistem kullanıcı allowlist'i geçerli c_<slug> kabul eder, injection/geçersiz reddeder.
func TestReKotaSK(t *testing.T) {
	valid := []string{"c_foo", "c_reg_kalici_test_local", "c_a1b2c3", "c_x"}
	for _, v := range valid {
		if !reKotaSK.MatchString(v) {
			t.Errorf("reKotaSK geçerli sk'yı reddetti: %q", v)
		}
	}
	invalid := []string{
		"", "root", "foo", "c_", "c_Foo", "c_foo bar", "c_foo;rm -rf /",
		"c_foo`id`", "c_foo\nx", "../c_foo", "c_foo/../bar", "admin",
	}
	for _, v := range invalid {
		if reKotaSK.MatchString(v) {
			t.Errorf("reKotaSK geçersiz/tehlikeli sk'yı kabul etti: %q", v)
		}
	}
}

// TestEfektifKota: override > plan > varsayılan çözümleme mantığı.
func TestEfektifKota(t *testing.T) {
	cases := []struct {
		ad                  string
		dOver, iOver        int
		planVar             bool
		pDisk, pInode       int
		wantDisk, wantInode int
	}{
		{"plan yok → varsayılan", 0, 0, false, 0, 0, varsayilanDiskMB, varsayilanInode},
		{"plan var, açıkça sınırsız", 0, 0, true, 0, 0, 0, 0},
		{"plan değerleri", 0, 0, true, 1024, 50000, 1024, 50000},
		{"disk override plan'ı ezer", 2048, 0, true, 1024, 50000, 2048, 50000},
		{"inode override planless'ı ezer", 0, 99999, false, 0, 0, varsayilanDiskMB, 99999},
		{"her iki override", 3072, 123456, true, 1024, 50000, 3072, 123456},
	}
	for _, c := range cases {
		gd, gi := efektifKota(c.dOver, c.iOver, c.planVar, c.pDisk, c.pInode)
		if gd != c.wantDisk || gi != c.wantInode {
			t.Errorf("%s: efektifKota=(%d,%d) want=(%d,%d)", c.ad, gd, gi, c.wantDisk, c.wantInode)
		}
	}
}

// TestKotaUygulaNoquotaGracefulLive: 181 gibi noquota fs'te KotaUygula HATA DÖNMEMELİ
// (log + return nil = graceful skip). Yalnız KOTA_LIVE=1 ile çalışır (gerçek xfs_quota çağırır).
func TestKotaUygulaNoquotaGracefulLive(t *testing.T) {
	if os.Getenv("KOTA_LIVE") != "1" {
		t.Skip("KOTA_LIVE=1 ile gerçek (noquota) fs üstünde çalıştır")
	}
	acc, enf := mountKotaAktif()
	t.Logf("mountKotaAktif(): accounting=%v enforcement=%v", acc, enf)
	if err := KotaUygula(context.Background(), "c_kotatest_noquota", 1024, 50000); err != nil {
		t.Fatalf("noquota fs'te KotaUygula HATA döndürdü (graceful-skip beklendi): %v", err)
	}
	t.Log("KotaUygula noquota'da graceful skip: err=nil ✓")
}
