package reseller

// olustur_plan_test.go — kullanıcı bildirimi 2026-10-12: "Reseller açtım ama otomatik
// olarak her reseller için plan oluşturuyor. 100 reseller demek 100 host planı demek."
// POST /resellers (panel arayüzü, WHMCS modülü ve API anahtarı AYNI uç) bayiye hosting
// planı AÇMAZ; kota/limitler paketten bayi kaydına kopyalanır (hosting açılışında
// domains.resellerKotaKontrol uygular). Şema canlıdaki service_plans'ın yazılabilir
// kopyasıdır: eski "<bayi> Varsayılan" INSERT'i burada BAŞARIYLA yazılırdı — test o
// davranışı gerçekten yakalar. Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

func opGeciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_bplan_%d", time.Now().UnixNano())
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
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL DEFAULT '', role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user',
			full_name VARCHAR(255) NULL, status ENUM('active','suspended') NOT NULL DEFAULT 'active',
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0, max_trafik_mb BIGINT NOT NULL DEFAULT 0,
			reseller_plan_id BIGINT NOT NULL DEFAULT 0, asim_ilkesi VARCHAR(16) NOT NULL DEFAULT 'yok',
			asim_bildirim TINYINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE reseller_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '',
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0, max_trafik_mb BIGINT NOT NULL DEFAULT 0,
			asim_ilkesi VARCHAR(16) NOT NULL DEFAULT 'yok', asim_bildirim TINYINT NOT NULL DEFAULT 0,
			fazla_satis TINYINT NOT NULL DEFAULT 0, mail_max_email INT NOT NULL DEFAULT 0,
			mail_saatlik_limit INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0)`,
		// Eski otomatik plan INSERT'inin yazdığı tüm sütunlar (canlı şemanın yazılabilir alt kümesi).
		`CREATE TABLE service_plans (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ad VARCHAR(64) NOT NULL UNIQUE,
			aciklama VARCHAR(255) NOT NULL DEFAULT '', disk_kota_mb INT NOT NULL DEFAULT 0, trafik_kota_mb INT NOT NULL DEFAULT 0,
			max_domain INT NOT NULL DEFAULT 0, max_db INT NOT NULL DEFAULT 0, max_email INT NOT NULL DEFAULT 0,
			saatlik_mail_limiti INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0, max_ftp INT NOT NULL DEFAULT 0,
			cpu_yuzde INT NOT NULL DEFAULT 100, ram_mb INT NOT NULL DEFAULT 512, max_process INT NOT NULL DEFAULT 50,
			inode_kota INT NOT NULL DEFAULT 50000, io_agirlik INT NOT NULL DEFAULT 100, mysql_max_baglanti INT NOT NULL DEFAULT 25,
			pm_max_children INT NOT NULL DEFAULT 0, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			fastcgi_cache TINYINT NOT NULL DEFAULT 0, client_max_body_mb INT NOT NULL DEFAULT 64, nginx_ek_direktifler TEXT NULL,
			waf_enabled TINYINT NOT NULL DEFAULT 0, waf_mode VARCHAR(10) NOT NULL DEFAULT 'on', waf_paranoia TINYINT NOT NULL DEFAULT 1,
			varsayilan TINYINT NOT NULL DEFAULT 0, reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
			target VARCHAR(255) NOT NULL DEFAULT '', detail TEXT NULL, ok TINYINT NOT NULL DEFAULT 1,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, role) VALUES (1, 'root', 'admin')`,
		`INSERT INTO reseller_plans (id, ad, max_domain, max_disk_mb, max_trafik_mb, fazla_satis) VALUES (1, 'Bayi S', 5, 10000, 50000, 0)`,
		`INSERT INTO service_plans (id, ad, disk_kota_mb, trafik_kota_mb, varsayilan, reseller_id) VALUES
			(1, 'Başlangıç', 1024, 5120, 1, 0), (2, 'Standart', 10240, 51200, 0, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func opSay(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%v: %s", err, q)
	}
	return n
}

func TestOlustur_BayiyeHostingPlaniAcmaz(t *testing.T) {
	db := opGeciciDB(t)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Post("/resellers", h.Olustur)
	once := opSay(t, db, `SELECT COUNT(*) FROM service_plans`)

	for _, kul := range []string{"bayi1", "bayi2", "bayi3"} {
		b, _ := json.Marshal(map[string]any{"paket_id": 1, "kullanici": kul, "parola": "test-parola-123", "ad_soyad": "Bayi " + kul})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/resellers", bytes.NewReader(b)))
		if w.Code != http.StatusCreated {
			t.Fatalf("POST /resellers %s → %d %s", kul, w.Code, w.Body.String())
		}
		var y map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &y); err != nil || y["id"] == nil || y["kullanici"] != kul {
			t.Fatalf("yanıt: %v %s", err, w.Body.String())
		}
		if _, var_ := y["varsayilan_plan_id"]; var_ {
			t.Errorf("yanıt hâlâ otomatik plan kimliği taşıyor: %s", w.Body.String())
		}
	}
	// 3 bayi → 0 yeni plan (eskiden her bayiye "<bayi> Varsayılan").
	if n := opSay(t, db, `SELECT COUNT(*) FROM service_plans`); n != once {
		t.Errorf("bayi oluşturma plan açtı: %d → %d", once, n)
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM service_plans WHERE reseller_id>0`); n != 0 {
		t.Errorf("bayilere ait %d plan var", n)
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='plan.varsayilan_olustur'`); n != 0 {
		t.Errorf("plan.varsayilan_olustur denetimi yazıldı: %d", n)
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='bayi.olustur' AND ok=1`); n != 3 {
		t.Errorf("bayi.olustur denetimi: %d", n)
	}
	// Kota/limitler bayi KAYDINDA (paketten anlık görüntü).
	var md int
	var dk, tr, paket int64
	var fazla int
	if err := db.QueryRow(`SELECT max_domain, max_disk_mb, max_trafik_mb, reseller_plan_id, fazla_satis FROM users WHERE username='bayi2' AND role='reseller'`).
		Scan(&md, &dk, &tr, &paket, &fazla); err != nil {
		t.Fatal(err)
	}
	if md != 5 || dk != 10000 || tr != 50000 || paket != 1 || fazla != 0 {
		t.Errorf("bayi kaydı limitleri: domain=%d disk=%d trafik=%d paket=%d fazla=%d", md, dk, tr, paket, fazla)
	}
	// Kökün planları yerinde (varsayılan dahil).
	if n := opSay(t, db, `SELECT COUNT(*) FROM service_plans WHERE reseller_id=0 AND varsayilan=1 AND id=1`); n != 1 {
		t.Errorf("kökün varsayılan planı değişti")
	}
}
