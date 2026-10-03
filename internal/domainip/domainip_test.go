package domainip

// domainip_test.go — tek domain IP değişim çekirdeği + HTTP yetki kapıları.
// Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR). Sunucu IP listesi ve
// zone yazıcı sahtedir: /var/named ve named'e dokunulmaz.
//
//	domains: 10 kök (.1)  11 bayi 7 (.1, ftp_host .1)  12 bayi 8 (.3)
//	sunucu : .1 birincil, .2 panel-ikincil, .3 ikincil (yalnız bayi 8 kullanıyor)

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"girginospanel/internal/ipyonetim"
	"girginospanel/internal/middleware"
)

const (
	ipBir   = "198.51.100.1"
	ipIki   = "198.51.100.2"
	ipUc    = "198.51.100.3"
	ipDisar = "192.0.2.77"
	ipElle  = "203.0.113.9"
)

var di_sir = []byte("domainip-test-anahtari-0123456789abcdef")

type di_zone struct {
	cagri int
	hata  error
}

func di_kur(t *testing.T) (*sql.DB, *di_zone) {
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
	ad := fmt.Sprintf("gosp_test_domainip_%d", time.Now().UnixNano())
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
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			full_name VARCHAR(128) NULL, role VARCHAR(16) NOT NULL DEFAULT 'user',
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NULL,
			last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			ipv4 VARCHAR(45) NOT NULL DEFAULT '', ftp_host VARCHAR(253) NOT NULL DEFAULT '',
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost', reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`CREATE TABLE dns_records (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			ad VARCHAR(255) NOT NULL, tip VARCHAR(10) NOT NULL, deger TEXT NOT NULL, ttl INT NOT NULL DEFAULT 3600,
			oncelik INT NOT NULL DEFAULT 0, aktif TINYINT NOT NULL DEFAULT 1)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (7,'bayi7','reseller'), (8,'bayi8','reseller')`,
		`INSERT INTO domains (id, alan_adi, ipv4, ftp_host, db_host, reseller_id) VALUES
			(10,'kok.test','` + ipBir + `','kok.test','localhost',0),
			(11,'b7.test','` + ipBir + `','` + ipBir + `','localhost',7),
			(12,'b8.test','` + ipUc + `','b8.test','localhost',8)`,
		`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES
			(11,'@','A','` + ipBir + `'), (11,'www','A','` + ipBir + `'), (11,'disari','A','` + ipElle + `'),
			(11,'@','TXT','v=spf1 a mx ip4:` + ipBir + ` ~all'), (11,'@','MX','mail.b7.test'),
			(10,'@','A','` + ipBir + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	z := &di_zone{}
	eskiIP, eskiZone := sunucuIPOku, zoneYazFn
	sunucuIPOku = func() []ipyonetim.SunucuIP {
		return []ipyonetim.SunucuIP{
			{IP: ipBir, Iface: "eth0", PrimaryMi: true},
			{IP: ipIki, Iface: "eth0", PanelIP: true},
			{IP: ipUc, Iface: "eth0"},
		}
	}
	zoneYazFn = func(_ context.Context, _ *sql.DB, _ int64) error { z.cagri++; return z.hata }
	middleware.Init(db)
	t.Cleanup(func() { sunucuIPOku, zoneYazFn = eskiIP, eskiZone; middleware.Init(nil) })
	return db, z
}

func di_str(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := db.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%v (%s)", err, q)
	}
	return s
}

func TestDomainIPDegistir_DNSveKolonlar_Idempotent(t *testing.T) {
	db, z := di_kur(t)
	s, err := DomainIPDegistir(context.Background(), db, 11, ipIki)
	if err != nil || !s.Degisti || s.DNSKayit != 2 || s.SPFKayit != 1 || !s.FTPHost || s.DBHost {
		t.Fatalf("beklenmeyen sonuç: %+v err=%v", s, err)
	}
	if got := di_str(t, db, `SELECT CONCAT(ipv4,'|',ftp_host,'|',db_host) FROM domains WHERE id=11`); got != ipIki+"|"+ipIki+"|localhost" {
		t.Fatalf("domains kolonları: %s", got)
	}
	if got := di_str(t, db, `SELECT GROUP_CONCAT(deger ORDER BY ad) FROM dns_records WHERE domain_id=11 AND tip='A'`); got != ipIki+","+ipElle+","+ipIki {
		t.Fatalf("A kayıtları (elle yöneltilen korunmalı): %s", got)
	}
	if got := di_str(t, db, `SELECT deger FROM dns_records WHERE domain_id=11 AND tip='TXT'`); got != "v=spf1 a mx ip4:"+ipBir+" ip4:"+ipIki+" ~all" {
		t.Fatalf("SPF: %s", got)
	}
	if got := di_str(t, db, `SELECT deger FROM dns_records WHERE domain_id=10`); got != ipBir {
		t.Fatalf("başka domainin kaydı değişti: %s", got)
	}
	if z.cagri != 1 {
		t.Fatalf("zone 1 kez yazılmalı: %d", z.cagri)
	}
	s, err = DomainIPDegistir(context.Background(), db, 11, ipIki)
	if err != nil || s.Degisti || z.cagri != 1 {
		t.Fatalf("ikinci çağrı no-op olmalı: %+v err=%v zone=%d", s, err, z.cagri)
	}
}

func TestDomainIPDegistir_ZoneHatasi_GeriAlir(t *testing.T) {
	db, z := di_kur(t)
	z.hata = errors.New("named-checkzone: bozuk")
	_, err := DomainIPDegistir(context.Background(), db, 11, ipIki)
	if err == nil || !strings.Contains(err.Error(), "geri alındı") {
		t.Fatalf("geri alma hatası beklenir: %v", err)
	}
	if got := di_str(t, db, `SELECT CONCAT(ipv4,'|',ftp_host) FROM domains WHERE id=11`); got != ipBir+"|"+ipBir {
		t.Fatalf("domains geri alınmadı: %s", got)
	}
	if got := di_str(t, db, `SELECT GROUP_CONCAT(deger ORDER BY id) FROM dns_records WHERE domain_id=11 AND tip IN ('A','TXT')`); got != ipBir+","+ipBir+","+ipElle+",v=spf1 a mx ip4:"+ipBir+" ~all" {
		t.Fatalf("dns geri alınmadı: %s", got)
	}
	if z.cagri != 2 {
		t.Fatalf("zone eski içerikle yeniden üretilmeli (2 çağrı): %d", z.cagri)
	}
}

func TestDomainIPDegistir_SunucudaOlmayanIP(t *testing.T) {
	db, z := di_kur(t)
	if _, err := DomainIPDegistir(context.Background(), db, 11, ipDisar); !errors.Is(err, ErrSunucudaYok) {
		t.Fatalf("ErrSunucudaYok beklenir: %v", err)
	}
	if _, err := DomainIPDegistir(context.Background(), db, 11, "::1"); !errors.Is(err, ErrGecersizIP) {
		t.Fatalf("v6 ErrGecersizIP beklenir: %v", err)
	}
	if got := di_str(t, db, `SELECT ipv4 FROM domains WHERE id=11`); got != ipBir || z.cagri != 0 {
		t.Fatalf("hiçbir şey değişmemeli: %s zone=%d", got, z.cagri)
	}
}

func TestSPFIP4Ekle(t *testing.T) {
	cases := []struct {
		in, out string
		ok      bool
	}{
		{"v=spf1 a mx ip4:1.1.1.1 ~all", "v=spf1 a mx ip4:1.1.1.1 ip4:2.2.2.2 ~all", true},
		{`"v=spf1 ip4:1.1.1.1/32 -all"`, `"v=spf1 ip4:1.1.1.1/32 ip4:2.2.2.2 -all"`, true},
		{"v=spf1 ip4:1.1.1.1 ip4:2.2.2.2 ~all", "", false}, // zaten var
		{"v=spf1 a mx ~all", "", false},                    // eski yok
		{"google-site-verification=ip4:1.1.1.1", "", false},
	}
	for _, c := range cases {
		out, ok := spfIP4Ekle(c.in, "1.1.1.1", "2.2.2.2")
		if ok != c.ok || out != c.out {
			t.Errorf("%q → %q,%v; beklenen %q,%v", c.in, out, ok, c.out, c.ok)
		}
	}
}

// ── HTTP: main.go ile aynı zincir ────────────────────────────────────────────

func di_router(db *sql.DB) http.Handler {
	h := &Handler{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(di_sir))
		r.With(middleware.SahipYonetim).Put("/domains/{id}/ip", h.DomainIP)
		r.With(middleware.AdminVeyaReseller).Get("/domains/ip-secenekleri", h.IPSecenekleri)
	})
	return r
}

func di_tok(t *testing.T, kim int64) string {
	t.Helper()
	var s string
	var err error
	if kim == 1 {
		s, err = auth.IssueAt(di_sir, 3600, 1, "root", "admin", 0, "")
	} else {
		s, err = auth.IssueResellerAt(di_sir, 3600, kim, fmt.Sprintf("bayi%d", kim), kim, 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func di_istek(t *testing.T, db *sql.DB, kim int64, yontem, yol string, govde any) (int, string) {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(yontem, yol, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+di_tok(t, kim))
	rec := httptest.NewRecorder()
	di_router(db).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHTTP_DomainIP_YetkiKapilari(t *testing.T) {
	db, _ := di_kur(t)
	for _, c := range []struct {
		ad  string
		kim int64
		id  int64
		ip  string
		kod int
	}{
		{"bayi başka bayinin domaini", 7, 12, ipBir, http.StatusForbidden},
		{"bayi kökün domaini", 7, 10, ipBir, http.StatusForbidden},
		{"bayi izinsiz sunucu IP'si (başka bayinin)", 7, 11, ipUc, http.StatusForbidden},
		{"bayi izinsiz panel-ikincil IP", 7, 11, ipIki, http.StatusForbidden},
		{"bayi sunucuda olmayan IP", 7, 11, ipDisar, http.StatusBadRequest},
		{"bayi geçersiz IP", 7, 11, "1.2.3", http.StatusBadRequest},
		{"yönetici sunucuda olmayan IP", 1, 11, ipDisar, http.StatusBadRequest},
	} {
		if kod, body := di_istek(t, db, c.kim, http.MethodPut, fmt.Sprintf("/domains/%d/ip", c.id), map[string]string{"ipv4": c.ip}); kod != c.kod {
			t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, kod, body)
		}
	}
	if got := di_str(t, db, `SELECT GROUP_CONCAT(ipv4 ORDER BY id) FROM domains`); got != ipBir+","+ipBir+","+ipUc {
		t.Fatalf("reddedilen istekler hiçbir şey değiştirmemeli: %s", got)
	}
	// Yönetici her sunucu IP'sine taşır; bayi birincil IP'ye geri alır.
	if kod, body := di_istek(t, db, 1, http.MethodPut, "/domains/11/ip", map[string]string{"ipv4": ipUc}); kod != 200 {
		t.Fatalf("yönetici .3: %d %s", kod, body)
	}
	if kod, body := di_istek(t, db, 7, http.MethodPut, "/domains/11/ip", map[string]string{"ipv4": ipBir}); kod != 200 {
		t.Fatalf("bayi birincil IP: %d %s", kod, body)
	}
	if got := di_str(t, db, `SELECT ipv4 FROM domains WHERE id=11`); got != ipBir {
		t.Fatalf("son durum: %s", got)
	}
}

func TestHTTP_IPSecenekleri_Kapsam(t *testing.T) {
	db, _ := di_kur(t)
	liste := func(kim int64) string {
		kod, body := di_istek(t, db, kim, http.MethodGet, "/domains/ip-secenekleri", nil)
		if kod != 200 {
			t.Fatalf("liste %d: %d %s", kim, kod, body)
		}
		var y struct {
			IPler []IPSecenek `json:"ipler"`
		}
		_ = json.Unmarshal([]byte(body), &y)
		var p []string
		for _, s := range y.IPler {
			p = append(p, fmt.Sprintf("%s:%d", s.IP, s.Kullanim))
		}
		return strings.Join(p, ",")
	}
	if got := liste(1); got != ipBir+":2,"+ipIki+":0,"+ipUc+":1" {
		t.Fatalf("yönetici: %s", got)
	}
	if got := liste(7); got != ipBir+":1" {
		t.Fatalf("bayi 7 yalnız birincil + kendi kullandığı: %s", got)
	}
	if got := liste(8); got != ipBir+":0,"+ipUc+":1" {
		t.Fatalf("bayi 8: %s", got)
	}
}
