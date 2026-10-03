package httpx

import (
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/sahtedb"
)

// AktorAyir, AdinaAktorAdi'nin ürettiği HER dizgeyi geri ayırır (kısaltılmış aktör
// adı dahil); biçime uymayan dizgeye dokunmaz.
func TestAktorAyir(t *testing.T) {
	uzun := strings.Repeat("ş", 80)
	for _, c := range []struct {
		aktor string
		id    int64
	}{{"girgin", 999999038}, {"bayi_a", 42}, {"a (b)", 7}, {uzun, 123456789012345}} {
		s := AdinaAktorAdi(c.aktor, c.id)
		if utf8.RuneCountInString(s) > 64 {
			t.Fatalf("%q 64 karakteri aşıyor", s)
		}
		ad, id := AktorAyir(s)
		if id != c.id || !strings.HasPrefix(c.aktor, ad) || ad == "" {
			t.Fatalf("AktorAyir(%q) = (%q,%d), beklenen (%q…,%d)", s, ad, id, c.aktor, c.id)
		}
	}
	if ad, id := AktorAyir("girgin (adına musteri:42)"); ad != "girgin" || id != 42 {
		t.Fatalf("eski kayıt biçimi ayrışmadı: %q %d", ad, id)
	}
	for _, s := range []string{"girgin", "musteri:42", "sistem", "", "x (adına musteri:)",
		"x (adına musteri:abc)", "x (adına musteri:0)", "x (adına musteri:-3)", "x (adına musteri:42", "(adına musteri:42) x"} {
		if ad, id := AktorAyir(s); ad != s || id != 0 {
			t.Fatalf("AktorAyir(%q) dokunmamalıydı: (%q,%d)", s, ad, id)
		}
	}
}

// Yazım: adına-oturum kaydı aktörü ve hostingi AYRI kolonlara yazar; normal kayıt
// eski INSERT'i (adına kolonu olmadan) kullanır.
func TestDenetimYaz_AdinaAyriKolon(t *testing.T) {
	sd, db := sahtedb.Yeni()
	r := httptest.NewRequest("POST", "/", nil)
	Denetim(db, r, 7, "girgin (adına musteri:42)", "erisim.ayar", "a.test", "", 7, true)
	if !sd.Yurutuldu("adina_domain_id") {
		t.Fatal("adına kaydı adina_domain_id kolonuyla yazılmalı")
	}
	if sd.Yurutuldu("adına musteri") {
		t.Fatal("aktör adı ham \"(adına musteri:N)\" ekiyle yazılmamalı")
	}
	sd, db = sahtedb.Yeni()
	DenetimSistem(db, 7, "girgin", "hosting.sil", "a.test", "", 7, true)
	if sd.Yurutuldu("adina_domain_id") || !sd.Yurutuldu("INSERT INTO audit_log") {
		t.Fatal("normal kayıt adına kolonu olmadan yazılmalı")
	}
}

func geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_httpx_%d", time.Now().UnixNano())
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
	return db
}

// Gerçek MariaDB: 0114 kolonu varken ayrı kolona yazılır; kolon YOKKEN (göç
// uygulanmamış) kayıt düşmez, eski tek-dizge biçimiyle yazılır.
func TestDenetimYaz_DB_KolonVeGeriDonus(t *testing.T) {
	db := geciciDB(t)
	if _, err := db.Exec(`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY,
		ts TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP, actor_user_id BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(45) NOT NULL DEFAULT '',
		action VARCHAR(64) NOT NULL, target VARCHAR(255) NOT NULL DEFAULT '',
		detail LONGTEXT NULL CHECK (json_valid(detail)), ok TINYINT(1) NOT NULL DEFAULT 1,
		reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", nil)
	// 1) Kolon yok → eski biçim, kayıt korunur.
	Denetim(db, r, 7, "girgin (adına musteri:42)", "erisim.ayar", "a.test", "not", 7, true)
	var ad string
	if err := db.QueryRow(`SELECT actor_username FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&ad); err != nil {
		t.Fatalf("kolonsuz şemada kayıt DÜŞTÜ: %v", err)
	}
	if ad != "girgin (adına musteri:42)" {
		t.Fatalf("geri dönüş eski biçimi yazmalı: %q", ad)
	}
	// 2) Kolon var → ayrı alanlar.
	if _, err := db.Exec(`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS adina_domain_id BIGINT UNSIGNED NULL DEFAULT NULL AFTER actor_username`); err != nil {
		t.Fatal(err)
	}
	Denetim(db, r, 7, "girgin (adına musteri:42)", "erisim.ayar", "a.test", "not", 7, true)
	var adina sql.NullInt64
	var uid int64
	if err := db.QueryRow(`SELECT actor_user_id, actor_username, adina_domain_id FROM audit_log ORDER BY id DESC LIMIT 1`).
		Scan(&uid, &ad, &adina); err != nil {
		t.Fatal(err)
	}
	if uid != 7 || ad != "girgin" || !adina.Valid || adina.Int64 != 42 {
		t.Fatalf("ayrı kolonlar beklenirdi: uid=%d aktör=%q adına=%v", uid, ad, adina)
	}
	// 3) Normal kayıt: adına NULL.
	DenetimSistem(db, 7, "girgin", "hosting.sil", "a.test", "", 7, true)
	if err := db.QueryRow(`SELECT actor_username, adina_domain_id FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&ad, &adina); err != nil {
		t.Fatal(err)
	}
	if ad != "girgin" || adina.Valid {
		t.Fatalf("normal kayıtta adına NULL olmalı: %q %v", ad, adina)
	}
}
