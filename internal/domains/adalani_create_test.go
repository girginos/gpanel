package domains

// adalani_create_test.go — GL-02 / YK-01: POST /domains ad-alanı ele geçirme.
// Bayi, kökün ya da başka bayinin alan adının ALT adıyla (mail./webmail./blog. …),
// ÜST adıyla ya da bir alt alan adının (subdomanlar.tam_ad) tam adıyla hosting
// açamaz. Uç main.go ile AYNI zincirde (RequireAuth → AdminVeyaReseller → Create).
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR.
//
// 🔴 YAN ETKİ KALKANLARI (test hiçbir koşulda useradd/nginx/BIND'e ulaşamaz):
//  1. provisioner yasak-listesi geri çağrısı: çağrı yığınında provisioner.Provision
//     varsa (Provision'ın ilk satırı ValidateDomain) reddeder → gerçek Provision
//     useradd'e varmadan döner; Create'in kendi ValidateDomain'i etkilenmez.
//  2. Test şemasındaki domains tablosunda web_root YOK → Create'in INSERT'i her
//     durumda başarısız olur; FTP/DNS/zone/kaynaklimit adımlarına geçilemez.
// Bu dosya yalnız tabanda da var olan API'yi kullanır: aynı test düzeltmesiz
// tabanda koşup açığı yeniden üretebilir (orada 500 "sağlama başarısız" döner).

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
)

var aa_sir = []byte("domains-adalani-test-anahtari-0123456789ab")

func aa_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

func aa_sayi(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

// aa_provisionIcinde: çağrı yığınında provisioner.Provision var mı?
func aa_provisionIcinde() bool {
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

// aa_kalkan: yan etki kalkanı 1 (bkz. dosya başı). Provision İÇİNDEN gelen
// ValidateDomain çağrısı reddedilir (useradd'den önceki ilk satır); Create'in kendi
// denetimi etkilenmez. Provision'a varan çağrı sayısını döner.
func aa_kalkan(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	provizyonDenemesi := 0
	provisioner.SetYasakliKontrolu(func(h string) (bool, string) {
		if !aa_provisionIcinde() {
			return false, ""
		}
		mu.Lock()
		provizyonDenemesi++
		mu.Unlock()
		return true, "test-kalkani"
	})
	t.Cleanup(func() { provisioner.SetYasakliKontrolu(nil) })
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return provizyonDenemesi
	}
}

// aa_db: geçici veritabanı + şema + başlangıç verisi.
//
//	users   : 1 kök (admin), 2 ve 3 etkin bayiler
//	domains : 10 kokdomain.test (kök), 11 shop.kurban.test (kök),
//	          20 bayi2.test (bayi 2), 21 www.ikiz2.test (bayi 2), 30 bayi3.test (bayi 3)
//	subdomanlar: blog.kokdomain.test (kök, domain 10), sub.bayi2.test (bayi 2, domain 20)
func aa_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_adalani_%d", time.Now().UnixNano())
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
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			role VARCHAR(16) NOT NULL DEFAULT 'user', status VARCHAR(16) NOT NULL DEFAULT 'active',
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0,
			max_trafik_mb BIGINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(100) NOT NULL DEFAULT '',
			varsayilan TINYINT NOT NULL DEFAULT 0, reseller_id BIGINT NOT NULL DEFAULT 0,
			php_surum VARCHAR(8) NOT NULL DEFAULT '', trafik_kota_mb BIGINT NULL, disk_kota_mb BIGINT NULL)`,
		// 🔴 web_root / is_demo / ftp_* YOK: Create'in INSERT'i burada HER ZAMAN düşer (kalkan 2).
		`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL UNIQUE, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			reseller_id BIGINT NOT NULL DEFAULT 0, customer_id BIGINT UNSIGNED NULL,
			plan_id BIGINT UNSIGNED NULL, boyut_kb BIGINT NOT NULL DEFAULT 0,
			ipv4 VARCHAR(45) NOT NULL DEFAULT '') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (2,'bayi2','reseller'), (3,'bayi3','reseller')`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES
			(10,'kokdomain.test',0), (11,'shop.kurban.test',0),
			(20,'bayi2.test',2), (21,'www.ikiz2.test',2), (30,'bayi3.test',3)`,
	} {
		aa_exec(t, db, q)
	}
	// subdomanlar: gerçek göç (0020 — tam_ad utf8mb4_general_ci, domains'ten farklı).
	body, err := os.ReadFile("../../migrations/0020_subdomanlar.sql")
	if err != nil {
		t.Fatalf("göç okunamadı: %v", err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if st := strings.TrimSpace(s); st != "" && !strings.HasPrefix(st, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			aa_exec(t, db, s)
		}
	}
	aa_exec(t, db, `INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES
		(10,'blog','blog.kokdomain.test'), (20,'sub','sub.bayi2.test')`)
	return db
}

func aa_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(aa_sir))
		r.With(middleware.AdminVeyaReseller).Post("/domains", h.Create)
	})
	return r
}

func aa_yonetici(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueAt(aa_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func aa_bayi(t *testing.T, rid int64) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(aa_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func aa_olustur(t *testing.T, h *Handlers, tok, alan string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"alan_adi": alan})
	req := httptest.NewRequest(http.MethodPost, "/domains", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	aa_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// 🔴 GL-02 / YK-01: bayi başka kiracının ad-alanında hosting AÇAMAZ — 409, sağlamaya
// (useradd/nginx/zone) hiç varılmaz, domains'e satır eklenmez; yanıt çakışan adı ya
// da sahibini SIZDIRMAZ.
func TestCreate_BayiBaskaKiracininAdAlaniniAlamaz(t *testing.T) {
	db := aa_db(t)
	provizyon := aa_kalkan(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	once := aa_sayi(t, db, `SELECT COUNT(*) FROM domains`)
	for _, c := range []struct{ ad, alan string }{
		{"kökün mail. adı", "mail.kokdomain.test"},
		{"kökün webmail. adı", "webmail.kokdomain.test"},
		{"kökün alt alan adı (subdomanlar)", "blog.kokdomain.test"},
		{"kökün derin alt adı", "a.b.kokdomain.test"},
		{"başka bayinin alt adı", "shop.bayi3.test"},
		{"kök alt domaininin üst adı", "kurban.test"},
		{"büyük harf + sondaki boşluk", "  MAIL.KokDomain.test "},
	} {
		kod, govde := aa_olustur(t, h, aa_bayi(t, 2), c.alan)
		if kod != http.StatusConflict {
			t.Errorf("GL-02 %s (%q): 409 beklenir, alınan %d %s", c.ad, c.alan, kod, govde)
			continue
		}
		for _, sizinti := range []string{"kokdomain", "kurban", "bayi3", "kök", "bayi #", "sahibi"} {
			if strings.Contains(strings.ToLower(govde), sizinti) && !strings.Contains(strings.ToLower(c.alan), sizinti) {
				t.Errorf("GL-02 %s: bayi yanıtı başka kiracıyı sızdırıyor (%q): %s", c.ad, sizinti, govde)
			}
		}
		if strings.Contains(govde, "shop.kurban.test") {
			t.Errorf("GL-02 %s: bayi yanıtı çakışan alt adı sızdırıyor: %s", c.ad, govde)
		}
	}
	if n := provizyon(); n != 0 {
		t.Errorf("GL-02: %d istek sağlamaya (useradd/nginx) ulaştı", n)
	}
	if sonra := aa_sayi(t, db, `SELECT COUNT(*) FROM domains`); sonra != once {
		t.Errorf("GL-02: domains satır sayısı değişti %d → %d", once, sonra)
	}
}

// Yönetici: bayinin ad-alanına da hosting açamaz; ayrıntılı neden görür. Birebir
// kayıt eskisi gibi 409 "zaten kayıtlı" (davranış değişmedi).
func TestCreate_YoneticiAdAlani(t *testing.T) {
	db := aa_db(t)
	provizyon := aa_kalkan(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	kod, govde := aa_olustur(t, h, aa_yonetici(t), "shop.bayi2.test")
	if kod != http.StatusConflict || !strings.Contains(govde, "bayi2.test") {
		t.Errorf("yönetici bayinin alt adı: 409 + ayrıntı beklenir, alınan %d %s", kod, govde)
	}
	kod, govde = aa_olustur(t, h, aa_yonetici(t), "kokdomain.test")
	if kod != http.StatusConflict || !strings.Contains(govde, "zaten kayıtlı") {
		t.Errorf("birebir kayıt: 409 zaten kayıtlı beklenir, alınan %d %s", kod, govde)
	}
	kod, govde = aa_olustur(t, h, aa_yonetici(t), "blog.kokdomain.test")
	if kod != http.StatusConflict {
		t.Errorf("yönetici kendi alt alan adının tam adı: 409 beklenir, alınan %d %s", kod, govde)
	}
	if n := provizyon(); n != 0 {
		t.Errorf("%d istek sağlamaya ulaştı", n)
	}
}
