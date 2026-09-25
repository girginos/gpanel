package dns

// template_kapsam_test.go — GL-01 / B-01 / BAYI-UI-01: DNS şablonu uçlarında bayi
// kapsamı. Bayi (AdminVeyaReseller) kendi kayıt satırlarını yönetir; SUNUCU GENELİ
// meta'yı (dns_template_meta id=1: SOA + DKIM) ne görür ne yazar. Yönetici
// davranışı değişmez.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/dns/
//
// Test yalnız kendi açtığı gosp_test_dnssablon_<zaman> veritabanını kullanır ve
// siler. Şema gerçek göç dosyalarından (0025, 0027) + 0044'ün dns_template satırıyla kurulur.
// 🔴 SeedDefaults ÇAĞRILMAZ: {DKIM} satırında EnsureDKIM → /etc/opendkim yazar ve
// opendkim'i yeniden yükler. SOA tohumlaması SeedDefaults'un kullandığı
// seedSOAFromMeta ile ölçülür (yalnız DB).

import (
	"bytes"
	"context"
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

var dk_sir = []byte("dns-sablon-kapsam-test-anahtari-0123456789")

func dk_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

func dk_sayi(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

// dk_goc: cmd/server göç ayrıştırmasıyla aynı ("--" satırları atılır, ";" bölünür).
func dk_goc(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/" + dosya)
	if err != nil {
		t.Fatalf("göç okunamadı (%s): %v", dosya, err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if st := strings.TrimSpace(s); st == "" || strings.HasPrefix(st, "--") {
			continue
		}
		temiz = append(temiz, s)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				m := strings.ToLower(err.Error())
				if strings.Contains(m, "duplicate") || strings.Contains(m, "already exists") {
					continue
				}
				t.Fatalf("GÖÇ HATASI (%s): %v\n%s", dosya, err, s)
			}
		}
	}
}

// dk_db: geçici veritabanı + şema + başlangıç verisi.
//
//	domains: 10 = kök (reseller 0), 11 = bayi 9, 12 = bayi 7
//	dns_template: 2 sunucu geneli satır (reseller_id=0), bayi satırı yok
//	dns_template_meta: göçün tohumladığı tek satır (varsayılanlar)
func dk_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_dnssablon_%d", time.Now().UnixNano())
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
	dk_exec(t, db, `CREATE TABLE domains (
		id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL UNIQUE,
		reseller_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`)
	dk_goc(t, db, "0025_dns_soa.sql")
	dk_goc(t, db, "0027_dns_template.sql")
	// 0044_reseller.sql'deki dns_template satırı (dosyanın geri kalanı başka tablolar).
	dk_exec(t, db, `ALTER TABLE dns_template ADD COLUMN IF NOT EXISTS reseller_id BIGINT NOT NULL DEFAULT 0`)
	dk_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES
		(10, 'kokdomain.test', 0), (11, 'bayi9.test', 9), (12, 'bayi7.test', 7)`)
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES
		('@', 'A', '{IP}', 3600, 0, 10, 1, 0), ('www', 'A', '{IP}', 3600, 0, 20, 1, 0)`)
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template_meta`); n != 1 {
		t.Fatalf("göç meta satırını tohumlamadı: %d", n)
	}
	return db
}

type dk_meta struct {
	refresh, retry, expire, minimum, ttl int
	selector                             string
	dkim                                 int
}

func dk_metaOku(t *testing.T, db *sql.DB) dk_meta {
	t.Helper()
	var m dk_meta
	if err := db.QueryRow(`SELECT soa_refresh, soa_retry, soa_expire, soa_minimum, soa_ttl, dkim_selector, dkim_aktif
		FROM dns_template_meta WHERE id=1`).Scan(&m.refresh, &m.retry, &m.expire, &m.minimum, &m.ttl, &m.selector, &m.dkim); err != nil {
		t.Fatalf("meta okunamadı: %v", err)
	}
	return m
}

var dk_varsayilan = dk_meta{3600, 900, 1209600, 3600, 3600, "default", 1}

// dk_router: main.go ile AYNI zincir (RequireAuth → AdminVeyaReseller → handler).
func dk_router(db *sql.DB) http.Handler {
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(dk_sir))
		r.With(middleware.AdminVeyaReseller).Get("/dns-template", h.GetTemplate)
		r.With(middleware.AdminVeyaReseller).Put("/dns-template", h.PutTemplate)
	})
	return r
}

func dk_yonetici(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueAt(dk_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func dk_bayi(t *testing.T, rid int64) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(dk_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func dk_istek(t *testing.T, db *sql.DB, tok, metot string, govde any) (int, map[string]any, string) {
	t.Helper()
	var body *bytes.Reader
	if govde != nil {
		b, _ := json.Marshal(govde)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(metot, "/dns-template", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	dk_router(db).ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, rec.Body.String()
}

// Bayinin eski istemcisinin gönderdiği gövde: kendi satırı + sunucu geneli meta.
func dk_bayiGovde() map[string]any {
	return map[string]any{
		"kayitlar": []map[string]any{{"ad": "@", "tip": "A", "deger": "{IP}", "ttl": 300, "sira": 10, "aktif": true}},
		"meta": map[string]any{"soa_refresh": 60, "soa_retry": 60, "soa_expire": 600, "soa_minimum": 60,
			"soa_ttl": 60, "dkim_selector": "bayi7sel", "dkim_aktif": false},
	}
}

// 🔴 GL-01 / B-01 / BAYI-UI-01: bayinin PUT'u sunucu geneli meta'yı DEĞİŞTİRMEZ;
// kökün ve başka bayinin domainleri SOA'yı yine sunucu geneli değerlerle tohumlar.
// Bayinin kendi satırları kaydedilir (bayi kendi şablonunu yönetmeye devam eder).
func TestDNSSablon_BayiSunucuGeneliMetaYazamaz(t *testing.T) {
	db := dk_db(t)
	kod, m, govde := dk_istek(t, db, dk_bayi(t, 7), http.MethodPut, dk_bayiGovde())
	if kod != http.StatusOK || m["ok"] != true {
		t.Fatalf("bayi kendi şablonunu kaydedebilmeli: %d %s", kod, govde)
	}
	if got := dk_metaOku(t, db); got != dk_varsayilan {
		t.Errorf("GL-01: bayi PUT sunucu geneli meta'yı değiştirdi: %+v (beklenen %+v)", got, dk_varsayilan)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template_meta`); n != 1 {
		t.Errorf("meta satır sayısı 1 olmalı: %d", n)
	}
	// Satır kapsamı: bayinin satırı ayrı, sunucu geneli satırlar dokunulmadı.
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=7`); n != 1 {
		t.Errorf("bayi satırı kaydedilmedi: %d", n)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=0`); n != 2 {
		t.Errorf("sunucu geneli satırlar değişti: %d", n)
	}
	// Etki: kökün (10) ve başka bayinin (11) domaini SOA'yı sunucu geneli değerlerle alır.
	ctx := context.Background()
	for _, d := range []struct {
		id int64
		ad string
	}{{10, "kokdomain.test"}, {11, "bayi9.test"}} {
		seedSOAFromMeta(ctx, db, d.id, d.ad, LoadTemplateMeta(ctx, db))
		if r := dk_sayi(t, db, `SELECT refresh FROM dns_soa WHERE domain_id=?`, d.id); r != 3600 {
			t.Errorf("GL-01: %s SOA refresh=%d — bayinin değeri (60) başka kiracıya uygulandı", d.ad, r)
		}
	}
	if got := LoadTemplateMeta(ctx, db); !got.DKIMAktif || got.DKIMSelector != "default" {
		t.Errorf("GL-01: DKIM ayarı bayi tarafından değişti: %+v", got)
	}
}

// 🔴 GL-01 (okuma): bayi GET yanıtında sunucu geneli meta YOK; kendi satırları var.
func TestDNSSablon_BayiMetaGormez(t *testing.T) {
	db := dk_db(t)
	kod, m, govde := dk_istek(t, db, dk_bayi(t, 7), http.MethodGet, nil)
	if kod != http.StatusOK {
		t.Fatalf("bayi GET: %d %s", kod, govde)
	}
	if _, var_ := m["meta"]; var_ {
		t.Errorf("GL-01: bayi yanıtında sunucu geneli meta var: %s", govde)
	}
	for _, sizinti := range []string{"soa_refresh", "dkim_selector", "dkim_aktif"} {
		if strings.Contains(govde, sizinti) {
			t.Errorf("GL-01: bayi yanıtında %q sızıyor: %s", sizinti, govde)
		}
	}
	if _, var_ := m["kayitlar"]; !var_ {
		t.Errorf("bayi yanıtında kayitlar olmalı: %s", govde)
	}
	// Bayi kendi şablonunu kaydedince GET yalnız KENDİ satırlarını döner.
	if kod, _, g := dk_istek(t, db, dk_bayi(t, 7), http.MethodPut, dk_bayiGovde()); kod != http.StatusOK {
		t.Fatalf("bayi PUT: %d %s", kod, g)
	}
	_, m, govde = dk_istek(t, db, dk_bayi(t, 7), http.MethodGet, nil)
	if k, _ := m["kayitlar"].([]any); len(k) != 1 || strings.Contains(govde, `"www"`) {
		t.Errorf("bayi GET kendi satırlarını dönmeli (1 satır, www yok): %s", govde)
	}
	if _, var_ := m["meta"]; var_ {
		t.Errorf("GL-01: bayi yanıtında (kendi şablonuyla) meta var: %s", govde)
	}
}

// 🔴 Kimliksiz bayi jetonu (rol reseller, anahtar 0) sunucu geneli şablona DÜŞMEZ:
// eskiden rid=0 ile `DELETE FROM dns_template WHERE reseller_id=0` sunucu geneli
// satırları siliyor, meta'yı da eziyordu.
func TestDNSSablon_KimliksizBayiSunucuGeneline_Dusmez(t *testing.T) {
	db := dk_db(t)
	tok, err := auth.IssueResellerAt(dk_sir, 3600, 7, "bayi7", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	kod, _, govde := dk_istek(t, db, tok, http.MethodPut, map[string]any{"kayitlar": []any{},
		"meta": map[string]any{"soa_refresh": 61}})
	if kod != http.StatusForbidden {
		t.Errorf("kimliksiz bayi PUT 403 almalı: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=0`); n != 2 {
		t.Errorf("kimliksiz bayi sunucu geneli satırları sildi: %d kaldı", n)
	}
	if got := dk_metaOku(t, db); got != dk_varsayilan {
		t.Errorf("kimliksiz bayi meta'yı değiştirdi: %+v", got)
	}
	kod, _, govde = dk_istek(t, db, tok, http.MethodGet, nil)
	if kod != http.StatusForbidden {
		t.Errorf("kimliksiz bayi GET 403 almalı: %d %s", kod, govde)
	}
}

// Yönetici davranışı DEĞİŞMEDİ: meta'yı görür ve yazar; sunucu geneli satırları
// değiştirir; bayinin satırlarına dokunmaz; meta gönderilmezse varsayılanlar yazılır.
func TestDNSSablon_YoneticiDavranisiDegismedi(t *testing.T) {
	db := dk_db(t)
	// Bayinin kendi satırı olsun (yönetici kaydı ona dokunmamalı).
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, sira, aktif, reseller_id) VALUES ('@','A','1.2.3.4',300,10,1,7)`)
	adm := dk_yonetici(t)

	kod, m, govde := dk_istek(t, db, adm, http.MethodGet, nil)
	if kod != http.StatusOK {
		t.Fatalf("yönetici GET: %d %s", kod, govde)
	}
	mt, _ := m["meta"].(map[string]any)
	if mt == nil || mt["soa_refresh"] != float64(3600) || mt["dkim_selector"] != "default" || mt["dkim_aktif"] != true {
		t.Errorf("yönetici GET meta'yı görmeli: %s", govde)
	}
	if k, _ := m["kayitlar"].([]any); len(k) != 2 {
		t.Errorf("yönetici GET sunucu geneli 2 satırı görmeli: %s", govde)
	}

	kod, m, govde = dk_istek(t, db, adm, http.MethodPut, map[string]any{
		"kayitlar": []map[string]any{{"ad": "@", "tip": "A", "deger": "{IP}", "ttl": 3600, "aktif": true},
			{"ad": "mail", "tip": "A", "deger": "{IP}", "ttl": 3600, "aktif": true},
			{"ad": "@", "tip": "MX", "deger": "mail.{DOMAIN}", "ttl": 3600, "oncelik": 10, "aktif": true}},
		"meta": map[string]any{"soa_refresh": 7200, "soa_retry": 1800, "soa_expire": 604800, "soa_minimum": 300,
			"soa_ttl": 600, "dkim_selector": "yon2026", "dkim_aktif": false},
	})
	if kod != http.StatusOK || m["ok"] != true {
		t.Fatalf("yönetici PUT: %d %s", kod, govde)
	}
	if _, var_ := m["meta_yok_sayildi"]; var_ {
		t.Errorf("yönetici meta'sı yok sayılmamalı: %s", govde)
	}
	if got, bek := dk_metaOku(t, db), (dk_meta{7200, 1800, 604800, 300, 600, "yon2026", 0}); got != bek {
		t.Errorf("yönetici meta'yı yazabilmeli: %+v (beklenen %+v)", got, bek)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=0`); n != 3 {
		t.Errorf("yönetici sunucu geneli satırları değiştirebilmeli: %d", n)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=7 AND deger='1.2.3.4'`); n != 1 {
		t.Errorf("yönetici kaydı bayinin satırına dokunmamalı: %d", n)
	}
	// Yöneticinin meta'sı SOA tohumlamasına uygulanır (sunucu geneli).
	ctx := context.Background()
	seedSOAFromMeta(ctx, db, 12, "bayi7.test", LoadTemplateMeta(ctx, db))
	if r := dk_sayi(t, db, `SELECT refresh FROM dns_soa WHERE domain_id=12`); r != 7200 {
		t.Errorf("yönetici meta'sı tohumlamaya uygulanmalı: refresh=%d", r)
	}

	// Meta gönderilmeyen yönetici PUT'u eskisi gibi sıfır değerli meta yazar: SOA
	// varsayılanları + dkim_aktif=0 (bool'un sıfır değeri — tabanla birebir aynı).
	if kod, _, g := dk_istek(t, db, adm, http.MethodPut, map[string]any{"kayitlar": []any{}}); kod != http.StatusOK {
		t.Fatalf("yönetici PUT (meta'sız): %d %s", kod, g)
	}
	if got, bek := dk_metaOku(t, db), (dk_meta{3600, 900, 1209600, 3600, 3600, "default", 0}); got != bek {
		t.Errorf("meta'sız yönetici PUT'u eski davranışı korumalı: %+v (beklenen %+v)", got, bek)
	}
}
