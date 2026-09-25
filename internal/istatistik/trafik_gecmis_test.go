package istatistik

// trafik_gecmis_test.go — GET /domains/{id}/trafik-gecmis.
//
// Gerçek RequireAuth + MusteriScope zinciri ve gerçek domain_trafik sorgusu.
// GOSP_TEST_GECICI_DSN yoksa atlanır. Yalnız kendi açtığı gosp_test_trafik_<zaman>
// veritabanını kullanır ve sonunda DROP eder.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

var tg_sir = []byte("trafik-gecmis-test-anahtari-0123456789ab")

// 42 bayi 7'nin (müşteri fhid=5), 43 ana hesabın.
func tg_geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_trafik_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
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
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NULL, last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NULL)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			askida TINYINT NULL, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		// migrations/0028_domain_trafik.sql ile aynı anahtar yapısı.
		`CREATE TABLE domain_trafik (domain_id BIGINT UNSIGNED NOT NULL, yil_ay CHAR(7) NOT NULL,
			bytes BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (domain_id, yil_ay))`,
		`INSERT INTO users (id) VALUES (1), (7)`,
		`INSERT INTO ftp_accounts (id) VALUES (5)`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (42, 'musteri.test', 7), (43, 'kok.test', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

func tg_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(tg_sir))
		r.With(middleware.MusteriScope).Get("/domains/{id}/trafik-gecmis", h.TrafikGecmis)
	})
	return r
}

func tg_istek(router http.Handler, tok, yol string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, yol, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func tg_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(tg_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(tg_sir, 3600, 7, "bayi", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(tg_sir, auth.MusteriClaims{
		FTPHesapID: 5, DomainID: 42, Kullanici: "m", AlanAdi: "musteri.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

// Eksik aylar 0, eskiden yeniye, pencere dışı ve başka domainin kovası yok;
// yıl sınırı doğru.
func TestAylikTrafik_DoldurmaVeSira(t *testing.T) {
	db := tg_geciciDB(t)
	if _, err := db.Exec(`INSERT INTO domain_trafik (domain_id, yil_ay, bytes) VALUES
		(42, '2025-11', 999), (42, '2025-12', 100), (42, '2026-02', 300), (42, '2026-03', 50),
		(42, '2026-04', 777), (43, '2026-01', 555)`); err != nil {
		t.Fatal(err)
	}
	simdi := time.Date(2026, 3, 31, 23, 59, 0, 0, time.UTC)
	k, err := aylikTrafik(context.Background(), db, 42, 4, simdi)
	if err != nil {
		t.Fatal(err)
	}
	var g []string
	for _, a := range k {
		if a.GelenB != 0 || a.ToplamB != a.GidenB {
			t.Errorf("%s: gelen ölçülmüyor (0), toplam=giden beklenir: %+v", a.Ay, a)
		}
		g = append(g, fmt.Sprintf("%s=%d", a.Ay, a.GidenB))
	}
	if s := strings.Join(g, " "); s != "2025-12=100 2026-01=0 2026-02=300 2026-03=50" {
		t.Fatalf("kalemler yanlış: %s", s)
	}
	// Ocak: ay-1 geriye yıl sınırını geçer.
	k, err = aylikTrafik(context.Background(), db, 42, 24, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 24 || k[0].Ay != "2024-02" || k[23].Ay != "2026-01" || k[21].Ay != "2025-11" || k[21].GidenB != 999 {
		t.Fatalf("24 ay penceresi yanlış: ilk=%s son=%s k[21]=%+v", k[0].Ay, k[23].Ay, k[21])
	}
}

func TestTrafikGecmis_YetkiVeParametre(t *testing.T) {
	db := tg_geciciDB(t)
	simdi := time.Now().UTC().Format("2006-01")
	if _, err := db.Exec(`INSERT INTO domain_trafik (domain_id, yil_ay, bytes) VALUES (42, ?, 12345), (43, ?, 1)`, simdi, simdi); err != nil {
		t.Fatal(err)
	}
	router := tg_router(&Handlers{DB: db})
	admin, bayi, musteri := tg_tokenlar(t)

	for _, tc := range []struct {
		ad, tok, yol string
		kod, kalem   int
	}{
		{"müşteri kendi domaini (varsayılan 12 ay)", musteri, "/domains/42/trafik-gecmis", 200, 12},
		{"müşteri başka domain", musteri, "/domains/43/trafik-gecmis", 403, 0},
		{"bayi kendi domaini", bayi, "/domains/42/trafik-gecmis?ay=3", 200, 3},
		{"bayi başkasının domaini", bayi, "/domains/43/trafik-gecmis", 403, 0},
		{"yönetici", admin, "/domains/43/trafik-gecmis?ay=24", 200, 24},
		{"yönetici ay=1", admin, "/domains/42/trafik-gecmis?ay=1", 200, 1},
		{"olmayan domain", admin, "/domains/999/trafik-gecmis", 404, 0},
		{"ay=0", admin, "/domains/42/trafik-gecmis?ay=0", 400, 0},
		{"ay=25", admin, "/domains/42/trafik-gecmis?ay=25", 400, 0},
		{"ay=abc", admin, "/domains/42/trafik-gecmis?ay=abc", 400, 0},
		{"kimliksiz", "", "/domains/42/trafik-gecmis", 401, 0},
	} {
		rec := tg_istek(router, tc.tok, tc.yol)
		if rec.Code != tc.kod {
			t.Errorf("%s: %d beklenir, gelen %d %s", tc.ad, tc.kod, rec.Code, rec.Body.String())
			continue
		}
		if tc.kod != 200 {
			if strings.Contains(rec.Body.String(), "kalemler") {
				t.Errorf("%s: reddedilen yanıtta veri var: %s", tc.ad, rec.Body.String())
			}
			continue
		}
		var y TrafikGecmisi
		if err := json.Unmarshal(rec.Body.Bytes(), &y); err != nil {
			t.Fatal(err)
		}
		if len(y.Kalemler) != tc.kalem || !y.GelenOlculmuyor {
			t.Errorf("%s: %d kalem + gelen_olculmuyor beklenir: %s", tc.ad, tc.kalem, rec.Body.String())
			continue
		}
		son := y.Kalemler[len(y.Kalemler)-1]
		want := int64(12345)
		if strings.HasPrefix(tc.yol, "/domains/43") {
			want = 1
		}
		if son.Ay != simdi || son.GidenB != want || son.ToplamB != want {
			t.Errorf("%s: son kalem bu ay (%s, %d) olmalı: %+v", tc.ad, simdi, want, son)
		}
	}
}

// Sözleşme: {"kalemler":[{"ay","gelen_b","giden_b","toplam_b"}]} (+ gelen_olculmuyor).
func TestTrafikGecmis_JSONSozlesmesi(t *testing.T) {
	b, _ := json.Marshal(TrafikGecmisi{Kalemler: []AyTrafik{{Ay: "2026-09"}}, GelenOlculmuyor: true})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	k := m["kalemler"].([]any)[0].(map[string]any)
	var a []string
	for x := range k {
		a = append(a, x)
	}
	sort.Strings(a)
	if s := strings.Join(a, ","); s != "ay,gelen_b,giden_b,toplam_b" {
		t.Fatalf("kalem anahtarları sözleşmeden farklı: %s", s)
	}
	if m["gelen_olculmuyor"] != true {
		t.Fatalf("gelen_olculmuyor yok: %s", b)
	}
}
