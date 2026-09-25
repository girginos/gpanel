package domains

// handlers_musteri_gorunum_test.go — B8: GET /domains/{id} müşteri oturumunda
// sahiplik bilgisini (sahip_ad, sahip_tur, reseller_id) VERMEZ; yönetici ve
// bayi yanıtı değişmez.
//
// DB'li kısım gerçek selectAll sorgusunu koşar; GOSP_TEST_GECICI_DSN yoksa
// atlanır. Test yalnız kendi açtığı gosp_test_domains_<zaman> veritabanını
// kullanır ve sonunda DROP eder (bkz. internal/bildirim/bildirim_test.go).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

func mg_geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_domains_%d", time.Now().UnixNano())
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
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(100) NOT NULL DEFAULT '')`,
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			full_name VARCHAR(128) NULL, role VARCHAR(16) NOT NULL DEFAULT 'user')`,
		`CREATE TABLE domains (
			id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			ssl_aktif TINYINT NOT NULL DEFAULT 0, ssl_bitis DATETIME NULL, durum VARCHAR(16) NOT NULL DEFAULT 'aktif',
			ipv4 VARCHAR(45) NOT NULL DEFAULT '', ftp_host VARCHAR(255) NOT NULL DEFAULT '',
			ftp_user VARCHAR(64) NOT NULL DEFAULT '', db_host VARCHAR(255) NOT NULL DEFAULT '',
			db_user VARCHAR(64) NOT NULL DEFAULT '', db_adi VARCHAR(64) NOT NULL DEFAULT '',
			web_root VARCHAR(255) NOT NULL DEFAULT '', boyut_kb BIGINT NOT NULL DEFAULT 0,
			trafik_kb BIGINT NOT NULL DEFAULT 0, is_demo TINYINT NOT NULL DEFAULT 0, notlar TEXT NULL,
			olusturulma DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, plan_id BIGINT UNSIGNED NULL,
			ssh_erisim TINYINT NOT NULL DEFAULT 0, askida TINYINT NULL,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, full_name, role) VALUES
			(1, 'root', 'Kok Yonetici Adi', 'admin'), (7, 'bayi_ahmet', 'Bayi Ahmet Yilmaz', 'reseller')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES
			(42, 'musteri.test', 'musteri', 7), (43, 'kok.test', 'kok', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	KokDB = db
	return db
}

func mg_get(t *testing.T, router http.Handler, tok string, id int64) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/domains/%d", id), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /domains/%d: %d %s", id, rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %v", err)
	}
	m["__govde"] = rec.Body.String()
	return m
}

// DB'siz: müşteri görünümünün JSON'unda sahiplik anahtarları YOK, geri kalan
// anahtar kümesi Domain ile aynı (ön yüz sözleşmesi korunur).
func TestMusteriGorunumu_JSON(t *testing.T) {
	plan := int64(3)
	d := Domain{ID: 42, AlanAdi: "musteri.test", PHPSurum: "8.3", PlanID: &plan, PlanAd: "Pro",
		SahipAd: "Bayi Ahmet", SahipTur: "bayi", ResellerID: 7, SSL: true}
	tam, _ := json.Marshal(d)
	mus, _ := json.Marshal(musteriGorunumu(d))
	var mt, mm map[string]any
	_ = json.Unmarshal(tam, &mt)
	_ = json.Unmarshal(mus, &mm)
	for _, k := range []string{"sahip_ad", "sahip_tur", "reseller_id"} {
		if _, ok := mm[k]; ok {
			t.Errorf("müşteri görünümünde %q var: %s", k, mus)
		}
		delete(mt, k)
	}
	if len(mt) != len(mm) {
		t.Fatalf("müşteri görünümü başka alanları da değiştirdi:\n tam=%s\n müş=%s", tam, mus)
	}
	for k, v := range mt {
		if fmt.Sprint(mm[k]) != fmt.Sprint(v) {
			t.Errorf("%q değişti: %v → %v", k, v, mm[k])
		}
	}
	if strings.Contains(string(mus), "Bayi Ahmet") {
		t.Errorf("sahip adı sızıyor: %s", mus)
	}
}

// 🔴 B8: müşteri kendi domaininde sahibin adını/kullanıcı adını ve bayi
// kimliğini görmez; yönetici ve bayi aynı alanları görmeye devam eder.
func TestDomainGet_MusteriSahiplikAlanlariniGormez(t *testing.T) {
	db := mg_geciciDB(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}

	for _, did := range []int64{42, 43} {
		m := mg_get(t, ds_router(h), ds_musteriToken(t, 5, did), did)
		for _, k := range []string{"sahip_ad", "sahip_tur", "reseller_id"} {
			if _, ok := m[k]; ok {
				t.Errorf("B8: müşteri yanıtında %q alanı var (domain %d): %v", k, did, m[k])
			}
		}
		govde := m["__govde"].(string)
		for _, sizinti := range []string{"Bayi Ahmet", "bayi_ahmet", "Kok Yonetici"} {
			if strings.Contains(govde, sizinti) {
				t.Errorf("B8: müşteri yanıtında sahip bilgisi sızıyor (%q, domain %d)", sizinti, did)
			}
		}
		// Müşterinin kullandığı alanlar KORUNUR.
		if m["id"] != float64(did) || m["alan_adi"] == "" || m["php_surum"] != "8.3" {
			t.Errorf("müşteri yanıtında temel alanlar bozuldu: %v", m)
		}
	}

	// Yönetici: değişmez.
	adminTok, err := auth.IssueAt(ds_secret, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a := mg_get(t, ds_router(h), adminTok, 42)
	if a["sahip_ad"] != "Bayi Ahmet Yilmaz" || a["sahip_tur"] != "bayi" || a["reseller_id"] != float64(7) {
		t.Errorf("yönetici yanıtı değişmemeli: sahip_ad=%v sahip_tur=%v reseller_id=%v", a["sahip_ad"], a["sahip_tur"], a["reseller_id"])
	}
	k := mg_get(t, ds_router(h), adminTok, 43)
	if k["sahip_tur"] != "admin" || k["reseller_id"] != float64(0) || k["sahip_ad"] == nil {
		t.Errorf("yönetici (kök domain) yanıtı değişmemeli: %v / %v / %v", k["sahip_ad"], k["sahip_tur"], k["reseller_id"])
	}

	// Bayi: değişmez (sahiplik kapsamı ayrı testlerde; burada yalnız yanıt biçimi).
	bayiTok, err := auth.IssueResellerAt(ds_secret, 3600, 7, "bayi_ahmet", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	br := chi.NewRouter()
	br.With(middleware.RequireAuth(ds_secret)).Get("/domains/{id}", h.Get)
	b := mg_get(t, br, bayiTok, 42)
	if b["sahip_ad"] != "Bayi Ahmet Yilmaz" || b["sahip_tur"] != "bayi" || b["reseller_id"] != float64(7) {
		t.Errorf("bayi yanıtı değişmemeli: %v / %v / %v", b["sahip_ad"], b["sahip_tur"], b["reseller_id"])
	}
}
