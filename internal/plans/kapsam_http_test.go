package plans_test

// kapsam_http_test.go — bayi (reseller) plan kapsamı, GERÇEK rota zinciriyle
// (RequireAuth + AdminVeyaReseller, cmd/server/main.go ile aynı) uçtan uca:
//
//   - GL-04 / B-02 / BAYI-UI-02: kökün domaine ÖZEL planı (ad/açıklama kökün
//     müşteri alan adını taşır, tüm limitler, nginx direktifleri) bayiye
//     GET /plans/{id} ve GET /plans?domain=<id> ile DÖNMEZ; bayi o planı kendi
//     hostingine BAĞLAYAMAZ (PUT /domains/{id}/plan).
//   - GL-03: eski bayi, başka kiracının hostingine bağlı kalmış planını
//     (devir artığı) DÜZENLEYEMEZ — o hostingin limitlerini değiştiremez.
//   - GL-05 / B-04 / BAYI-UI-04: plan detayındaki domain_sayisi ve silme
//     engeli mesajı bayiye YALNIZ kendi hostinglerini sayar.
//   - GL-06 / BAYI-UI-03: kökün global katalog planı bayiye YALNIZ seçim
//     özetiyle (ad + temel kotalar, global/duzenlenebilir işaretleri) döner;
//     nginx ek direktifleri, WAF, IO/DB ayarları, açıklama dönmez.
//   - Yönetici davranışı DEĞİŞMEZ (aynı uçlar, aynı sonuçlar).
//
// Gerçek MariaDB gerekir; yoksa ATLANIR:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/plans/
//
// 🔴 YAN ETKİ YOK: hiçbir istek gövdesi nginx_ek_direktifler taşımaz (dolu olsa
// provisioner.ValidateNginxDirectives /etc/nginx/conf.d'ye geçici dosya yazar);
// başarıyla güncellenen planlara wafPlanReapply'nin seçeceği (plan sahibiyle
// aynı kiracının) hosting BAĞLANMAZ; PUT /domains/{id}/plan yalnız VAR OLMAYAN
// hostinge (99) gönderilir → kapsam kapısını geçse bile "domain bulunamadı"da
// durur, UPDATE/kaynaklimit/WAF goroutine'ine ulaşmaz. Son emniyet: test
// domains tablosunda php_surum sütunu YOK — yanlışlıkla tetiklenen
// provisioner.RerenderVhost (WAFUygula) ilk sorgusunda hata verip vhost YAZMAZ.

import (
	"bytes"
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
	"girginospanel/internal/domains"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"
)

var pk_sir = []byte("plans-kapsam-test-anahtari-0123456789abcdef")

// pk_servicePlansDDL — canlı şemanın (0006..0058 göçleri sonrası) birebir kopyası.
const pk_servicePlansDDL = "CREATE TABLE `service_plans` (" +
	"`id` bigint(20) unsigned NOT NULL AUTO_INCREMENT," +
	"`ad` varchar(64) NOT NULL," +
	"`aciklama` varchar(255) NOT NULL DEFAULT ''," +
	"`disk_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"`trafik_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"`max_domain` int(11) NOT NULL DEFAULT 0," +
	"`max_db` int(11) NOT NULL DEFAULT 0," +
	"`max_email` int(11) NOT NULL DEFAULT 0," +
	"`max_ftp` int(11) NOT NULL DEFAULT 0," +
	"`varsayilan` tinyint(1) NOT NULL DEFAULT 0," +
	"`created_at` timestamp NULL DEFAULT current_timestamp()," +
	"`updated_at` timestamp NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()," +
	"`cpu_yuzde` int(11) NOT NULL DEFAULT 100," +
	"`ram_mb` int(11) NOT NULL DEFAULT 512," +
	"`max_process` int(11) NOT NULL DEFAULT 50," +
	"`inode_kota` int(11) NOT NULL DEFAULT 50000," +
	"`io_agirlik` int(11) NOT NULL DEFAULT 100," +
	"`mysql_max_baglanti` int(11) NOT NULL DEFAULT 25," +
	"`php_surum` varchar(8) NOT NULL DEFAULT '8.3'," +
	"`fastcgi_cache` tinyint(1) NOT NULL DEFAULT 0," +
	"`client_max_body_mb` int(11) NOT NULL DEFAULT 64," +
	"`nginx_ek_direktifler` text DEFAULT NULL," +
	"`pm_max_children` int(11) NOT NULL DEFAULT 0," +
	"`io_read_mbps` int(11) NOT NULL DEFAULT 0," +
	"`io_write_mbps` int(11) NOT NULL DEFAULT 0," +
	"`io_read_iops` int(11) NOT NULL DEFAULT 0," +
	"`io_write_iops` int(11) NOT NULL DEFAULT 0," +
	"`db_max_queries_per_hour` int(11) NOT NULL DEFAULT 0," +
	"`db_max_updates_per_hour` int(11) NOT NULL DEFAULT 0," +
	"`db_max_query_seconds` int(11) NOT NULL DEFAULT 0," +
	"`waf_enabled` tinyint(4) NOT NULL DEFAULT 0," +
	"`waf_mode` varchar(10) NOT NULL DEFAULT 'on'," +
	"`waf_paranoia` tinyint(4) NOT NULL DEFAULT 1," +
	"`reseller_id` bigint(20) NOT NULL DEFAULT 0," +
	"`domain_id` bigint(20) unsigned DEFAULT NULL," +
	"`saatlik_mail_limiti` int(11) NOT NULL DEFAULT 0," +
	"`mail_kutu_kota_mb` int(11) NOT NULL DEFAULT 0," +
	"PRIMARY KEY (`id`), UNIQUE KEY `ad` (`ad`), KEY `ix_sp_reseller` (`reseller_id`)," +
	"KEY `ix_service_plans_domain` (`domain_id`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

func pk_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

// pk_geciciDB — kendi gosp_test_plans_<zaman> veritabanı + fikstür.
//
// Kiracılar: kök (0), bayi A (2), bayi B (3).
//
//	plan 1   "Standart"             kök GLOBAL katalog (nginx/WAF/IO ayarlı)
//	plan 100 "kok-site.test — Özel" kökün hostingi 10'a ÖZEL
//	plan 200 "A Plani"              A katalog — A'nın 21'i + B'nin 32'si (devir artığı)
//	plan 201 "A Ikinci"             A katalog — hiçbir hostingde yok
//	plan 202 "A Devir Artigi"       A katalog — YALNIZ B'nin 33'ü (devir artığı)
//	plan 210 "a-site.test — Özel"   A'nın hostingi 20'ye ÖZEL
//	plan 300 "B Plani"              B katalog — B'nin 31'i
//
//	domain 10 kok-site.test (kök, 100)   11 kok-diger.test (kök, 1)
//	domain 20 a-site.test (A, 210)       21 a-iki.test (A, 200)     22 a-uc.test (A, 1)
//	domain 30 b-site.test (B, 1)         31 b-iki.test (B, 300)
//	domain 32 b-devir.test (B, 200)      33 b-devir2.test (B, 202)
func pk_geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_plans_%d", time.Now().UnixNano())
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
		pk_servicePlansDDL,
		`CREATE TABLE users (
			id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			role VARCHAR(16) NOT NULL DEFAULT 'reseller',
			status VARCHAR(16) NOT NULL DEFAULT 'active',
			max_disk_mb BIGINT NOT NULL DEFAULT 0, max_trafik_mb BIGINT NOT NULL DEFAULT 0,
			fazla_satis TINYINT NOT NULL DEFAULT 0, reseller_plan_id BIGINT NOT NULL DEFAULT 0
		) ENGINE=InnoDB`,
		`CREATE TABLE domains (
			id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL UNIQUE,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			durum VARCHAR(16) NOT NULL DEFAULT 'aktif',
			olusturulma DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			plan_id BIGINT UNSIGNED NULL,
			boyut_kb BIGINT NOT NULL DEFAULT 0,
			customer_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT NOT NULL DEFAULT 0,
			KEY ix_domains_plan (plan_id), KEY ix_dom_reseller (reseller_id)
		) ENGINE=InnoDB`,
		`CREATE TABLE audit_log (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			actor_user_id BIGINT NULL, actor_username VARCHAR(64) NULL, ip VARCHAR(64) NULL,
			action VARCHAR(64) NULL, target VARCHAR(255) NULL, detail TEXT NULL, ok TINYINT NULL,
			reseller_id BIGINT NOT NULL DEFAULT 0
		) ENGINE=InnoDB`,
		// fazla_satis=1: SetPlan'in bayi disk/trafik havuzu kapisi kapsam testini gölgelemesin.
		`INSERT INTO users (id, role, fazla_satis) VALUES (1,'admin',0), (2,'reseller',1), (3,'reseller',1)`,
		`INSERT INTO service_plans (id, ad, aciklama, disk_kota_mb, trafik_kota_mb, cpu_yuzde, ram_mb,
			max_process, io_agirlik, io_read_mbps, db_max_query_seconds, nginx_ek_direktifler,
			waf_enabled, waf_mode, waf_paranoia, varsayilan, reseller_id, domain_id) VALUES
			(1,  'Standart', 'KOK-IC-NOTU', 10240, 51200, 100, 2048, 60, 150, 40, 30, '# KOK-GLOBAL-DIREKTIF', 1, 'detect', 3, 1, 0, NULL),
			(100,'kok-site.test — Özel', 'Yalnız kok-site.test için özel plan', 0, 0, 400, 8192, 500, 900, 0, 0, '# KOK-OZEL-DIREKTIF', 1, 'on', 2, 0, 0, 10),
			(200,'A Plani', 'A aciklama', 1000, 1000, 200, 2048, 50, 100, 0, 0, '', 0, 'on', 1, 0, 2, NULL),
			(201,'A Ikinci', 'A ikinci', 1000, 1000, 200, 2048, 50, 100, 0, 0, '', 0, 'on', 1, 0, 2, NULL),
			(202,'A Devir Artigi', 'A devir', 1000, 1000, 200, 2048, 50, 100, 0, 0, '', 0, 'on', 1, 0, 2, NULL),
			(210,'a-site.test — Özel', 'Yalnız a-site.test için özel plan', 500, 500, 100, 1024, 50, 100, 0, 0, '', 0, 'on', 1, 0, 2, 20),
			(300,'B Plani', 'B-GIZLI-ACIKLAMA', 2000, 2000, 100, 1024, 50, 100, 0, 0, '# B-DIREKTIF', 0, 'on', 1, 0, 3, NULL)`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id, reseller_id) VALUES
			(10,'kok-site.test','c_kok1',100,0), (11,'kok-diger.test','c_kok2',1,0),
			(20,'a-site.test','c_a1',210,2), (21,'a-iki.test','c_a2',200,2), (22,'a-uc.test','c_a3',1,2),
			(30,'b-site.test','c_b1',1,3), (31,'b-iki.test','c_b2',300,3),
			(32,'b-devir.test','c_b3',200,3), (33,'b-devir2.test','c_b4',202,3)`,
	} {
		pk_exec(t, db, q)
	}
	return db
}

// pk_router — cmd/server/main.go'daki /plans zinciri + PUT /domains/{id}/plan.
// SahipYonetim middleware'i burada YOK (scopeDB'siz): hedef hosting 99 zaten yok;
// test edilen şey planın KAPSAM kapısıdır, hosting sahipliği değil.
func pk_router(db *sql.DB) http.Handler {
	ph := &plans.Handlers{DB: db}
	dh := &domains.Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(pk_sir))
		r.With(middleware.AdminVeyaReseller).Get("/plans", ph.List)
		r.With(middleware.AdminVeyaReseller).Get("/plans/{id}", ph.Get)
		r.With(middleware.AdminVeyaReseller).Post("/plans", ph.Create)
		r.With(middleware.AdminVeyaReseller).Put("/plans/{id}", ph.Update)
		r.With(middleware.AdminVeyaReseller).Delete("/plans/{id}", ph.Delete)
		r.With(middleware.AdminVeyaReseller).Get("/plans/{id}/domains", ph.DomainlerAra)
		r.Put("/domains/{id}/plan", dh.SetPlan)
	})
	return r
}

func pk_jeton(t *testing.T, kim string) string {
	t.Helper()
	var s string
	var err error
	switch kim {
	case "admin":
		s, err = auth.IssueAt(pk_sir, 3600, 1, "root", "admin", 0, "")
	case "A":
		s, err = auth.IssueResellerAt(pk_sir, 3600, 2, "bayiA", 2, 0, "")
	case "B":
		s, err = auth.IssueResellerAt(pk_sir, 3600, 3, "bayiB", 3, 0, "")
	default:
		t.Fatalf("bilinmeyen kimlik %q", kim)
	}
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type pk_yanit struct {
	kod   int
	govde string
}

func pk_istek(t *testing.T, h http.Handler, kim, yontem, yol string, govde any) pk_yanit {
	t.Helper()
	var b []byte
	if govde != nil {
		var err error
		if b, err = json.Marshal(govde); err != nil {
			t.Fatal(err)
		}
		// Güvenlik: dolu nginx direktifi canlı /etc/nginx/conf.d'ye dokunur.
		if strings.Contains(string(b), `"nginx_ek_direktifler":"`) && !strings.Contains(string(b), `"nginx_ek_direktifler":""`) {
			t.Fatalf("güvenlik: test gövdesi nginx direktifi taşıyamaz: %s", b)
		}
	}
	req := httptest.NewRequest(yontem, yol, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+pk_jeton(t, kim))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return pk_yanit{rec.Code, rec.Body.String()}
}

// pk_planlar — GET /plans yanıtını id → alanlar eşlemine çevirir.
func pk_planlar(t *testing.T, y pk_yanit) map[int64]map[string]any {
	t.Helper()
	if y.kod != http.StatusOK {
		t.Fatalf("GET /plans %d: %s", y.kod, y.govde)
	}
	var liste []map[string]any
	if err := json.Unmarshal([]byte(y.govde), &liste); err != nil {
		t.Fatalf("liste çözülemedi: %v (%s)", err, y.govde)
	}
	out := map[int64]map[string]any{}
	for _, p := range liste {
		id, _ := p["id"].(float64)
		out[int64(id)] = p
	}
	return out
}

func pk_detay(t *testing.T, y pk_yanit) (map[string]any, int) {
	t.Helper()
	if y.kod != http.StatusOK {
		t.Fatalf("GET /plans/{id} %d: %s", y.kod, y.govde)
	}
	var g struct {
		Plan         map[string]any `json:"plan"`
		DomainSayisi int            `json:"domain_sayisi"`
	}
	if err := json.Unmarshal([]byte(y.govde), &g); err != nil {
		t.Fatalf("detay çözülemedi: %v (%s)", err, y.govde)
	}
	return g.Plan, g.DomainSayisi
}

// iç ayrıntılar: bayiye global planda DÖNMEMESİ gereken alanlar.
var pk_icAlanlar = []string{"aciklama", "nginx_ek_direktifler", "waf_enabled", "waf_mode", "waf_paranoia",
	"io_agirlik", "io_read_mbps", "io_write_mbps", "io_read_iops", "io_write_iops", "max_process",
	"mysql_max_baglanti", "pm_max_children", "db_max_queries_per_hour", "db_max_updates_per_hour",
	"db_max_query_seconds", "fastcgi_cache", "client_max_body_mb", "varsayilan", "olusturulma"}

// ── GL-04 / B-02 / BAYI-UI-02 ───────────────────────────────────────────────

func TestPlanKapsam_KokOzelPlanBayiyeKapali(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)

	// Doğrudan kimlikle: bayi A kökün hostingine özel planı OKUYAMAZ.
	if y := pk_istek(t, h, "A", "GET", "/plans/100", nil); y.kod != http.StatusNotFound || strings.Contains(y.govde, "kok-site.test") {
		t.Errorf("GL-04: bayi A GET /plans/100 → %d %s (beklenen 404, kök alan adı yok)", y.kod, y.govde)
	}
	// ?domain=<kökün hostingi>: kök özel planı listeye GİRMEZ.
	if y := pk_istek(t, h, "A", "GET", "/plans?domain=10", nil); strings.Contains(y.govde, "kok-site.test") || pk_planlar(t, y)[100] != nil {
		t.Errorf("B-02: bayi A GET /plans?domain=10 kökün özel planını döndü: %s", y.govde)
	}
	// ?domain=<başka bayinin hostingi>: yabancı plan yok.
	if y := pk_istek(t, h, "A", "GET", "/plans?domain=31", nil); strings.Contains(y.govde, "B Plani") {
		t.Errorf("bayi A GET /plans?domain=31 B'nin planını döndü: %s", y.govde)
	}
	// Kimlik taraması: 1..400 arası tüm kimliklerde yalnız kendi+global görünür.
	for id := 1; id <= 400; id++ {
		y := pk_istek(t, h, "A", "GET", fmt.Sprintf("/plans/%d", id), nil)
		if y.kod == http.StatusOK && !(id == 1 || id == 200 || id == 201 || id == 202 || id == 210) {
			t.Errorf("BAYI-UI-02: tarama — bayi A GET /plans/%d → 200 %s", id, y.govde)
		}
	}
	// Kontrol: kendi hostinginin özel planı görünür (?domain=20 ve doğrudan).
	if p := pk_planlar(t, pk_istek(t, h, "A", "GET", "/plans?domain=20", nil)); p[210] == nil {
		t.Errorf("kontrol: bayi A kendi hostingi 20'nin özel planını (210) göremedi")
	}
	if y := pk_istek(t, h, "A", "GET", "/plans/210", nil); y.kod != http.StatusOK {
		t.Errorf("kontrol: bayi A GET /plans/210 → %d %s", y.kod, y.govde)
	}
	// Yönetici DEĞİŞMEZ: kök özel planı tüm ayrıntısıyla görür.
	if y := pk_istek(t, h, "admin", "GET", "/plans/100", nil); y.kod != http.StatusOK || !strings.Contains(y.govde, "# KOK-OZEL-DIREKTIF") {
		t.Errorf("yönetici GET /plans/100 → %d %s", y.kod, y.govde)
	}
	if p := pk_planlar(t, pk_istek(t, h, "admin", "GET", "/plans?domain=10", nil)); p[100] == nil {
		t.Errorf("yönetici GET /plans?domain=10 kök özel planını listelemedi")
	}
}

func TestPlanKapsam_BayiYabanciPlaniBaglayamaz(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)
	// Hosting 99 YOK: kapsamı geçen istek "domain bulunamadı"da durur (yan etki yok).
	bagla := func(kim string, planID int64) pk_yanit {
		return pk_istek(t, h, kim, "PUT", "/domains/99/plan", map[string]any{"plan_id": planID})
	}
	// GL-04: kökün (hosting 10'a) özel planı bayinin hostingine bağlanamaz.
	if y := bagla("A", 100); y.kod != http.StatusForbidden || !strings.Contains(y.govde, "bu plana erişiminiz yok") {
		t.Errorf("GL-04: bayi A kökün özel planını (100) bağlayabildi → %d %s", y.kod, y.govde)
	}
	// Bayinin kendi hostingi 20'ye özel plan başka hostinge bağlanamaz.
	if y := bagla("A", 210); y.kod != http.StatusForbidden {
		t.Errorf("bayi A hosting 20'nin özel planını (210) başka hostinge bağlayabildi → %d %s", y.kod, y.govde)
	}
	// Başka bayinin planı (değişmeyen davranış).
	if y := bagla("A", 300); y.kod != http.StatusForbidden {
		t.Errorf("bayi A B'nin planını (300) bağlayabildi → %d %s", y.kod, y.govde)
	}
	// Kontrol: global katalog + kendi katalog planı kapıdan GEÇER (hosting yok → 404).
	for _, pid := range []int64{1, 201} {
		if y := bagla("A", pid); y.kod != http.StatusNotFound || !strings.Contains(y.govde, "domain bulunamadı") {
			t.Errorf("kontrol: bayi A plan %d → %d %s (beklenen kapıdan geçip 404)", pid, y.kod, y.govde)
		}
	}
	// Yönetici DEĞİŞMEZ: her planı bağlayabilir (hosting yok → 404).
	for _, pid := range []int64{100, 210, 300} {
		if y := bagla("admin", pid); y.kod != http.StatusNotFound {
			t.Errorf("yönetici plan %d → %d %s (beklenen 404)", pid, y.kod, y.govde)
		}
	}
	// Olmayan plan (değişmeyen davranış).
	if y := bagla("A", 9999); y.kod != http.StatusBadRequest {
		t.Errorf("olmayan plan → %d %s", y.kod, y.govde)
	}
}

// ── GL-03 ────────────────────────────────────────────────────────────────────

func TestPlanKapsam_YabanciHostingeBagliPlanDuzenlenemez(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)
	govde := map[string]any{"ad": "A Devir Artigi", "disk_kota_mb": 1000, "trafik_kota_mb": 1000,
		"cpu_yuzde": 5, "ram_mb": 64, "max_process": 2, "db_max_query_seconds": 1, "nginx_ek_direktifler": ""}

	// Plan 202 A'nın ama YALNIZ B'nin hostingi 33'te: A düzenlerse B'nin sitesi boğulur.
	y := pk_istek(t, h, "A", "PUT", "/plans/202", govde)
	var cpu, dbq int
	if err := db.QueryRow(`SELECT cpu_yuzde, db_max_query_seconds FROM service_plans WHERE id=202`).Scan(&cpu, &dbq); err != nil {
		t.Fatal(err)
	}
	if y.kod == http.StatusOK || cpu != 200 || dbq != 0 {
		t.Errorf("GL-03: eski bayi B'nin hostingine bağlı planı değiştirdi → %d %s (cpu=%d dbq=%d)", y.kod, y.govde, cpu, dbq)
	}
	if strings.Contains(y.govde, "b-devir2") || strings.Contains(y.govde, "33") {
		t.Errorf("GL-03: ret mesajı yabancı hostingi sızdırıyor: %s", y.govde)
	}
	// Kontrol: kendi hostinglerinde (ya da hiçbir yerde) kullanılan kendi planı düzenlenir.
	govde["ad"] = "A Ikinci"
	if y := pk_istek(t, h, "A", "PUT", "/plans/201", govde); y.kod != http.StatusOK {
		t.Errorf("kontrol: bayi A kendi planını (201) düzenleyemedi → %d %s", y.kod, y.govde)
	}
	// Yabancı planlar (değişmeyen davranış): global, kök özel, B'nin planı → 403.
	for _, pid := range []int64{1, 100, 300} {
		govde["ad"] = fmt.Sprintf("x%d", pid)
		if y := pk_istek(t, h, "A", "PUT", fmt.Sprintf("/plans/%d", pid), govde); y.kod != http.StatusForbidden {
			t.Errorf("bayi A PUT /plans/%d → %d %s", pid, y.kod, y.govde)
		}
	}
	// Yönetici DEĞİŞMEZ: aynı planı düzenleyebilir.
	govde["ad"] = "A Devir Artigi"
	govde["cpu_yuzde"] = 150
	if y := pk_istek(t, h, "admin", "PUT", "/plans/202", govde); y.kod != http.StatusOK {
		t.Errorf("yönetici PUT /plans/202 → %d %s", y.kod, y.govde)
	}
}

// ── GL-05 / B-04 / BAYI-UI-04 ───────────────────────────────────────────────

func TestPlanKapsam_DomainSayisiBayiKapsaminda(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)
	// Global plan 1: kök 11, A 22, B 30 → bayi A yalnız 1 görmeli.
	if _, n := pk_detay(t, pk_istek(t, h, "A", "GET", "/plans/1", nil)); n != 1 {
		t.Errorf("GL-05: bayi A GET /plans/1 domain_sayisi=%d (beklenen yalnız kendi: 1)", n)
	}
	// Kendi planı 200: A 21 + B 32 (devir artığı) → A yalnız 1 görmeli.
	if _, n := pk_detay(t, pk_istek(t, h, "A", "GET", "/plans/200", nil)); n != 1 {
		t.Errorf("BAYI-UI-04: bayi A GET /plans/200 domain_sayisi=%d (beklenen 1)", n)
	}
	// Silme engeli mesajı da kapsamlı sayar.
	if y := pk_istek(t, h, "A", "DELETE", "/plans/200", nil); y.kod != http.StatusConflict || !strings.Contains(y.govde, "1 abonelikte") {
		t.Errorf("BAYI-UI-04: bayi A DELETE /plans/200 → %d %s (beklenen 409, '1 abonelikte')", y.kod, y.govde)
	}
	// Plan 202 yalnız yabancı hostingde: silinemez, ama sayı/yabancı bilgi sızmaz.
	if y := pk_istek(t, h, "A", "DELETE", "/plans/202", nil); y.kod != http.StatusConflict || strings.ContainsAny(y.govde, "0123456789") {
		t.Errorf("bayi A DELETE /plans/202 → %d %s (beklenen 409, sayısız)", y.kod, y.govde)
	}
	// Liste ucu (değişmeyen davranış): yalnız kendi hostingleri.
	if y := pk_istek(t, h, "A", "GET", "/plans/1/domains", nil); y.kod != http.StatusOK || !strings.Contains(y.govde, "a-uc.test") ||
		strings.Contains(y.govde, "b-site") || strings.Contains(y.govde, "kok-diger") {
		t.Errorf("bayi A GET /plans/1/domains → %d %s", y.kod, y.govde)
	}
	// Kök özel planının hosting listesi bayiye kapalı.
	if y := pk_istek(t, h, "A", "GET", "/plans/100/domains", nil); y.kod == http.StatusOK {
		t.Errorf("bayi A GET /plans/100/domains → %d %s (beklenen ret)", y.kod, y.govde)
	}
	// Yönetici DEĞİŞMEZ: sunucu geneli sayım ve mesaj.
	if _, n := pk_detay(t, pk_istek(t, h, "admin", "GET", "/plans/1", nil)); n != 3 {
		t.Errorf("yönetici GET /plans/1 domain_sayisi=%d (beklenen 3)", n)
	}
	if y := pk_istek(t, h, "admin", "DELETE", "/plans/200", nil); y.kod != http.StatusConflict || !strings.Contains(y.govde, "2 abonelikte") {
		t.Errorf("yönetici DELETE /plans/200 → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "admin", "GET", "/plans/1/domains", nil); !strings.Contains(y.govde, "kok-diger.test") || !strings.Contains(y.govde, "b-site.test") {
		t.Errorf("yönetici GET /plans/1/domains → %s", y.govde)
	}
}

// Bayi plan adı çakışmasında ham SQL hatası ("Duplicate entry '<başka kiracının
// plan adı>' for key 'ad'") görmez; yönetici yanıtı değişmedi.
func TestPlanKapsam_BayiHamSQLHatasiGormez(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)
	govde := map[string]any{"ad": "B Plani", "disk_kota_mb": 100, "trafik_kota_mb": 100, "nginx_ek_direktifler": ""}
	y := pk_istek(t, h, "A", "POST", "/plans", govde)
	if y.kod != http.StatusConflict || strings.Contains(y.govde, "Duplicate") || strings.Contains(y.govde, "1062") {
		t.Errorf("bayi A plan adı çakışması → %d %s (beklenen 409, ham SQL yok)", y.kod, y.govde)
	}
	govde["ad"] = "A Ucuncu"
	if y := pk_istek(t, h, "A", "PUT", "/plans/201", map[string]any{"ad": "A Plani", "disk_kota_mb": 100, "trafik_kota_mb": 100, "nginx_ek_direktifler": ""}); y.kod != http.StatusConflict || strings.Contains(y.govde, "Duplicate") {
		t.Errorf("bayi A PUT ad çakışması → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "A", "POST", "/plans", govde); y.kod != http.StatusCreated || !strings.Contains(y.govde, `"duzenlenebilir":true`) {
		t.Errorf("kontrol: bayi A yeni plan → %d %s", y.kod, y.govde)
	}
	// Yönetici DEĞİŞMEZ: ham hata.
	govde["ad"] = "B Plani"
	if y := pk_istek(t, h, "admin", "POST", "/plans", govde); y.kod != http.StatusInternalServerError || !strings.Contains(y.govde, "Duplicate") {
		t.Errorf("yönetici ad çakışması → %d %s", y.kod, y.govde)
	}
}

// ── GL-06 / BAYI-UI-03 ───────────────────────────────────────────────────────

func TestPlanKapsam_GlobalPlanBayiyeYalnizOzet(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)

	liste := pk_planlar(t, pk_istek(t, h, "A", "GET", "/plans", nil))
	g := liste[1]
	if g == nil {
		t.Fatalf("bayi A listesinde seçilebilir global plan (1) yok")
	}
	for _, alan := range pk_icAlanlar {
		if _, var_ := g[alan]; var_ {
			t.Errorf("GL-06: bayi listesinde global planın iç ayrıntısı %q dönüyor: %v", alan, g[alan])
		}
	}
	if g["global"] != true || g["duzenlenebilir"] != false {
		t.Errorf("BAYI-UI-03: global plan işaretsiz (global=%v duzenlenebilir=%v)", g["global"], g["duzenlenebilir"])
	}
	// Seçim için gereken özet duruyor.
	for alan, bek := range map[string]float64{"disk_kota_mb": 10240, "trafik_kota_mb": 51200, "ram_mb": 2048, "cpu_yuzde": 100} {
		if g[alan] != bek {
			t.Errorf("global plan özeti %s=%v (beklenen %v)", alan, g[alan], bek)
		}
	}
	if g["ad"] != "Standart" {
		t.Errorf("global plan adı %v", g["ad"])
	}
	// Kendi planı TAM ve düzenlenebilir.
	if k := liste[200]; k == nil || k["duzenlenebilir"] != true || k["nginx_ek_direktifler"] == nil || k["aciklama"] != "A aciklama" {
		t.Errorf("bayi A kendi planını (200) tam göremedi: %v", k)
	}
	// Başka bayinin planı ve kök özel planı listede yok.
	if liste[300] != nil || liste[100] != nil {
		t.Errorf("bayi A listesinde yabancı plan: 300=%v 100=%v", liste[300], liste[100])
	}
	// Detay ucu da özet döner.
	p, _ := pk_detay(t, pk_istek(t, h, "A", "GET", "/plans/1", nil))
	for _, alan := range pk_icAlanlar {
		if _, var_ := p[alan]; var_ {
			t.Errorf("GL-06: bayi GET /plans/1 iç ayrıntı %q dönüyor", alan)
		}
	}
	if y := pk_istek(t, h, "A", "GET", "/plans/1", nil); strings.Contains(y.govde, "KOK-") {
		t.Errorf("GL-06: bayi GET /plans/1 kökün iç notunu/direktifini taşıyor: %s", y.govde)
	}
	// Sil/Düzenle global planda 403 (değişmeyen davranış).
	if y := pk_istek(t, h, "A", "DELETE", "/plans/1", nil); y.kod != http.StatusForbidden {
		t.Errorf("bayi A DELETE /plans/1 → %d", y.kod)
	}
	// Yönetici DEĞİŞMEZ: global plan tüm ayrıntısıyla.
	ya := pk_planlar(t, pk_istek(t, h, "admin", "GET", "/plans", nil))
	if a := ya[1]; a == nil || a["nginx_ek_direktifler"] != "# KOK-GLOBAL-DIREKTIF" || a["waf_mode"] != "detect" || a["aciklama"] != "KOK-IC-NOTU" {
		t.Errorf("yönetici global planı tam göremedi: %v", ya[1])
	}
	for _, id := range []int64{1, 200, 201, 202, 300} {
		if ya[id] == nil {
			t.Errorf("yönetici listesinde plan %d yok", id)
		}
	}
	if ya[100] != nil || ya[210] != nil {
		t.Errorf("yönetici kataloğunda özel plan listelendi (değişmemeli)")
	}
}
