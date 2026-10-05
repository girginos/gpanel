package subdomain

// ekalan_test.go — ek (addon) alan adı: doğrulayıcı, limit, takeover, IDOR, yol/dosya
// güvenliği, vhost şablonu + geri alma, SSL akışı, askı/silme, açılış onarımı.
//
// DB'li testler gerçek (geçici) MariaDB: GOSP_TEST_GECICI_DSN (go_test db:true);
// yoksa ATLANIR. Dış etkili adımlar dikişlerle sahte (ev dizini, conf.d, ACME kökü
// geçici dizinde; komutlar kayda alınır; bölge yazımı sahte).

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

const (
	ek_sk1, ek_dom1 = "c_ektest", 501
	ek_sk2, ek_dom2 = "c_ekbaska", 502
	ek_alan1        = "ektest.test"
	ek_alan2        = "ekbaska.test"
)

// ek_migrasyon — migrations/0160'ı noktalı virgülle bölüp koşar (göç koşucusunun kuralı:
// "--" ile başlayan satırlar atılır).
func ek_migrasyon(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/0160_ek_alan_adlari.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if st := strings.TrimSpace(s); st != "" && !strings.HasPrefix(st, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("göç 0160: %v\n%s", err, s)
			}
		}
	}
}

func ek_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	if cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("yalnız unix soketli geçici DSN desteklenir")
	}
	switch filepath.Clean(cfg.Addr) {
	case "/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock":
		t.Fatalf("GOSP_TEST_GECICI_DSN canlı MariaDB soketini gösteriyor — reddedildi")
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	ad := fmt.Sprintf("gosp_test_ekalan_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	cfg.DBName = ad
	cfg.Params = map[string]string{"parseTime": "false"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	t.Cleanup(func() { _ = db.Close() })

	// service_plans: efektiflimit'in okuduğu BÜTÜN limit sütunları (Alanlar'dan üretilir).
	kolonlar := ""
	for _, a := range efektiflimit.Alanlar {
		kolonlar += ", `" + a.Sutun + "` INT NULL"
	}
	for _, q := range []string{
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(100) NOT NULL DEFAULT '',
			reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL` + kolonlar + `)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL UNIQUE,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			is_demo TINYINT NOT NULL DEFAULT 0, askida TINYINT NOT NULL DEFAULT 0,
			reseller_id BIGINT NOT NULL DEFAULT 0, ipv4 VARCHAR(45) NOT NULL DEFAULT '',
			plan_id BIGINT UNSIGNED NULL, disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0)
			ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		efektiflimit.TabloDDL,
		`CREATE TABLE subdomanlar (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, alt_ad VARCHAR(63) NOT NULL, tam_ad VARCHAR(253) NOT NULL,
			php_surum VARCHAR(8) NOT NULL DEFAULT '8.3', web_backend VARCHAR(16) NOT NULL DEFAULT 'php-fpm',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO service_plans (id, ad, max_domain) VALUES (100, 'plan', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	for _, d := range []struct {
		id       int64
		alan, sk string
	}{{ek_dom1, ek_alan1, ek_sk1}, {ek_dom2, ek_alan2, ek_sk2}} {
		if _, err := db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id) VALUES (?,?,?,100)`, d.id, d.alan, d.sk); err != nil {
			t.Fatal(err)
		}
	}
	ek_migrasyon(t, db)
	return db
}

func ek_limit(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE service_plans SET max_domain=? WHERE id=100`, n); err != nil {
		t.Fatal(err)
	}
}

type ekDikis struct {
	kok     string
	calisan *[]string
	mu      *sync.Mutex
	// nginxHata: true iken `nginx -t` başarısız döner.
	nginxHata *bool
	// zone çağrıları
	zoneYaz, zoneSil *[]string
}

// ek_dikisler — ev dizini/conf.d/ACME kökü geçici dizinde; komutlar kayda alınır; bölge
// ve ad-alanı denetimi sahte. AdAlaniEkle: üretimdeki gibi süreç genelinde KİLİTLİ.
func ek_dikisler(t *testing.T) *ekDikis {
	t.Helper()
	e1, e2, e3, e4, e5, e6, e7, e8, e9, e10 := komut, acmeKoku, evKoku, nginxConfD, httpdConfD, ekZoneTohum, ekZoneYaz, ekZoneSil, AdAlaniEkle, ekVhostYenidenKur
	e11 := ekSimdi
	t.Cleanup(func() {
		komut, acmeKoku, evKoku, nginxConfD, httpdConfD = e1, e2, e3, e4, e5
		ekZoneTohum, ekZoneYaz, ekZoneSil, AdAlaniEkle, ekVhostYenidenKur, ekSimdi = e6, e7, e8, e9, e10, e11
		ekSSLHizMu.Lock()
		ekSSLDenemeler = map[string][]time.Time{}
		ekSSLHizMu.Unlock()
	})
	d := &ekDikis{kok: t.TempDir(), calisan: &[]string{}, mu: &sync.Mutex{}, nginxHata: new(bool),
		zoneYaz: &[]string{}, zoneSil: &[]string{}}
	evKoku, nginxConfD, httpdConfD = d.kok+"/home", d.kok+"/nginx", d.kok+"/httpd"
	for _, dz := range []string{evKoku, nginxConfD, httpdConfD} {
		if err := os.MkdirAll(dz, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	acmeKoku = func() string { return d.kok + "/acme" }
	ekVhostYenidenKur = ekVhostYenidenKurDB
	ekSSLHizMu.Lock()
	ekSSLDenemeler = map[string][]time.Time{}
	ekSSLHizMu.Unlock()
	komut = func(ad string, arg ...string) ([]byte, error) {
		d.mu.Lock()
		*d.calisan = append(*d.calisan, ad+" "+strings.Join(arg, " "))
		d.mu.Unlock()
		if ad == "nginx" && *d.nginxHata {
			return []byte("nginx: [emerg] unknown directive \"x\" in /etc/nginx/conf.d/komsu.conf:3"), fmt.Errorf("exit 1")
		}
		if ad == "openssl" || (strings.HasSuffix(ad, "acme.sh") && strings.Contains(strings.Join(arg, " "), "--install-cert")) {
			for i := 0; i+1 < len(arg); i++ {
				if arg[i] == "-keyout" || arg[i] == "-out" || arg[i] == "--key-file" || arg[i] == "--fullchain-file" {
					if err := os.WriteFile(arg[i+1], []byte("sahte"), 0o600); err != nil {
						return nil, err
					}
				}
			}
		}
		return nil, nil
	}
	ekZoneTohum = func(ctx context.Context, db *sql.DB, ekID int64, ad, ip string, rid int64) (int, error) {
		_, err := db.ExecContext(ctx, `INSERT INTO ek_dns_kayitlari (ek_alan_id, ad, tip, deger) VALUES (?,'@','A',?)`, ekID, ip)
		return 1, err
	}
	ekZoneYaz = func(ctx context.Context, db *sql.DB, ekID int64) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		*d.zoneYaz = append(*d.zoneYaz, fmt.Sprint(ekID))
		return nil
	}
	ekZoneSil = func(ctx context.Context, db *sql.DB, ad string) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		*d.zoneSil = append(*d.zoneSil, ad)
		return nil
	}
	var kmu sync.Mutex
	AdAlaniEkle = func(ctx context.Context, db *sql.DB, ad string, kiraci int64, ekle func() error) error {
		kmu.Lock()
		defer kmu.Unlock()
		return ekle()
	}
	return d
}

func (d *ekDikis) komutlar() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(*d.calisan, "\n")
}

func (d *ekDikis) say(oneki string) int {
	n := 0
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range *d.calisan {
		if strings.HasPrefix(c, oneki) {
			n++
		}
	}
	return n
}

// ek_cagir — yönlendirici + (isteğe bağlı) rol bağlamı yok: handler doğrudan çağrılır.
func ek_cagir(t *testing.T, db *sql.DB, yontem, desen, yol, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Method(yontem, desen, isle(&Handlers{DB: db, IPv4: "203.0.113.7"}))
	req := httptest.NewRequest(yontem, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func ek_ekle(t *testing.T, db *sql.DB, dom int64, alan string) (int, string) {
	t.Helper()
	return ek_cagir(t, db, http.MethodPost, "/domains/{id}/ek-alan-adlari",
		fmt.Sprintf("/domains/%d/ek-alan-adlari", dom), `{"alan_adi":"`+alan+`"}`,
		func(h *Handlers) http.HandlerFunc { return h.EkOlustur })
}

func ek_sayi(t *testing.T, db *sql.DB, q string, a ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ek_eid(t *testing.T, db *sql.DB, alan string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM ek_alan_adlari WHERE alan_adi=?`, alan).Scan(&id); err != nil {
		t.Fatalf("ek alan satırı (%s): %v", alan, err)
	}
	return id
}

// ───────────────────────── doğrulayıcı ─────────────────────────

func TestEkAlanAdiDogrula_Tablo(t *testing.T) {
	gecerli := map[string]string{
		"ornek.com":        "ornek.com",
		"  ORNEK.Com ":     "ornek.com",
		"ornek.com.":       "ornek.com",
		"alt.ornek.com.tr": "alt.ornek.com.tr",
		"a-b.c-d.org":      "a-b.c-d.org",
		"bücher.example":   "xn--bcher-kva.example",
		"123.example.net":  "123.example.net",
	}
	for girdi, beklenen := range gecerli {
		got, err := ekAlanAdiDogrula(girdi)
		if err != nil || got != beklenen {
			t.Errorf("%q: beklenen %q, alınan %q (%v)", girdi, beklenen, got, err)
		}
		if err == nil && !ekAdGuvenli(got) {
			t.Errorf("%q: doğrulanmış ad güvenli biçimde değil: %q", girdi, got)
		}
	}
	uzunEtiket := strings.Repeat("a", 64) + ".com"
	uzunAd := strings.Repeat("abcdefghi.", 26) + "com"
	gecersiz := []string{
		"", " ", "localhost", "ornek", "http://ornek.com", "ornek.com/yol", "ornek.com:8080", "*.ornek.com",
		"kisi@ornek.com", "1.2.3.4", "::1", "-a.com", "a-.com", "a..com", ".ornek.com", "www.ornek.com",
		"ornek.local", "ornek.localhost", "ornek.internal", "x.in-addr.arpa", "ornek_x.com", "ornek com",
		"ornek.com\nserver_name evil", "ornek.com;", "ornek.com{", "ornek.com\"", "ornek.c", "ornek.123",
		"ornek.com#x", "orn$ek.com", "../../etc/x.com", "ornek.com\\x", uzunEtiket, uzunAd,
	}
	for _, g := range gecersiz {
		if got, err := ekAlanAdiDogrula(g); err == nil {
			t.Errorf("%q kabul edilmemeliydi (çıktı %q)", g, got)
		}
	}
}

func TestEkAlanAdiDogrula_YasakliListe(t *testing.T) {
	provisioner.SetYasakliKontrolu(func(h string) (bool, string) { return h == "marka.com", "marka.com" })
	t.Cleanup(func() { provisioner.SetYasakliKontrolu(nil) })
	if _, err := ekAlanAdiDogrula("marka.com"); err == nil {
		t.Fatal("yasaklı alan adı (cp_banned_domains) kabul edilmemeli")
	}
	if _, err := ekAlanAdiDogrula("serbest.com"); err != nil {
		t.Fatal(err)
	}
}

// ───────────────────────── limit ─────────────────────────

func TestEkLimitDurumu(t *testing.T) {
	for _, c := range []struct {
		ad          string
		limit, ek   int
		demo        bool
		eklenebilir bool
		kullanim    int
	}{
		{"limit 1: ana dahil dolu", 1, 0, false, false, 1},
		{"limit 0 sınırsız", 0, 50, false, true, 51},
		{"limit 3, 1 ek", 3, 1, false, true, 2},
		{"limit 3, 2 ek (dolu)", 3, 2, false, false, 3},
		{"limit aşılmış", 2, 5, false, false, 6},
		{"demo", 0, 0, true, false, 1},
	} {
		d := ekLimitDurumu(c.limit, c.ek, c.demo)
		if d.Eklenebilir != c.eklenebilir || d.Kullanim != c.kullanim {
			t.Errorf("%s: %+v", c.ad, d)
		}
		if !d.Eklenebilir && d.Neden == "" {
			t.Errorf("%s: eklenemiyorsa neden yazılmalı", c.ad)
		}
	}
}

// Paket limiti 1 → sunucu ek alan adını REDDEDER (UI'da düğme gizli olsa da).
func TestEkOlustur_Limit1_Reddedilir(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	ek_limit(t, db, 1)
	kod, govde := ek_ekle(t, db, ek_dom1, "ek1.example.org")
	if kod != http.StatusConflict || !strings.Contains(govde, "hakkı kalmadı") {
		t.Fatalf("limit 1: 409 beklenir: %d %s", kod, govde)
	}
	if n := ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`); n != 0 {
		t.Fatalf("satır eklenmemeli: %d", n)
	}
	if _, err := os.Stat(ekConfPath(ek_sk1, "ek1.example.org")); err == nil {
		t.Fatal("vhost yazılmamalı")
	}
}

// Limit N → N-1 ek alan adı.
func TestEkOlustur_LimitN_NMinus1Ek(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	ek_limit(t, db, 3)
	for i, alan := range []string{"bir.example.org", "iki.example.org"} {
		if kod, govde := ek_ekle(t, db, ek_dom1, alan); kod != http.StatusOK {
			t.Fatalf("%d. ek alan adı 200 beklenir: %d %s", i+1, kod, govde)
		}
	}
	if kod, govde := ek_ekle(t, db, ek_dom1, "uc.example.org"); kod != http.StatusConflict {
		t.Fatalf("limit dolu: 409 beklenir: %d %s", kod, govde)
	}
	// Başka hesabın sayacı etkilenmez.
	if kod, govde := ek_ekle(t, db, ek_dom2, "baska.example.org"); kod != http.StatusOK {
		t.Fatalf("diğer hesap: 200 beklenir: %d %s", kod, govde)
	}
	// Sınırsız (0): limit kalkınca eklenir.
	ek_limit(t, db, 0)
	if kod, govde := ek_ekle(t, db, ek_dom1, "uc.example.org"); kod != http.StatusOK {
		t.Fatalf("sınırsız: 200 beklenir: %d %s", kod, govde)
	}
}

// Hostinge özel üst yazım plana baskındır (efektif limit).
func TestEkOlustur_UstYazimEfektifLimit(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	ek_limit(t, db, 10)
	if _, err := db.Exec(`INSERT INTO hosting_limit_ust (domain_id, max_domain) VALUES (?, 1)`, ek_dom1); err != nil {
		t.Fatal(err)
	}
	if kod, govde := ek_ekle(t, db, ek_dom1, "bir.example.org"); kod != http.StatusConflict {
		t.Fatalf("üst yazım 1: 409 beklenir: %d %s", kod, govde)
	}
}

// 🔴 Yarış: aynı hesaba eşzamanlı eklemeler limiti AŞAMAZ. Süreç genelindeki kilit
// (AdAlaniEkle) BİLEREK yok: yalnız domains satırı FOR UPDATE kilidi sıralar.
func TestEkOlustur_Yaris_LimitAsilmaz(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	AdAlaniEkle = func(ctx context.Context, db *sql.DB, ad string, kiraci int64, ekle func() error) error {
		return ekle() // kilitsiz
	}
	ek_limit(t, db, 4) // ana + 3 ek
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, ret := 0, 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			kod, _ := ek_ekle(t, db, ek_dom1, fmt.Sprintf("yaris%d.example.org", i))
			mu.Lock()
			defer mu.Unlock()
			switch kod {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				ret++
			default:
				t.Errorf("beklenmeyen kod %d", kod)
			}
		}(i)
	}
	wg.Wait()
	if n := ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari WHERE domain_id=?`, ek_dom1); n != 3 || ok != 3 || ret != 9 {
		t.Fatalf("yarış: satır=%d ok=%d red=%d (beklenen 3/3/9)", n, ok, ret)
	}
}

// ───────────────────────── takeover / ad alanı ─────────────────────────

func TestEkHesapAdAlani_Takeover(t *testing.T) {
	db := ek_db(t)
	// hesap 1: ektest.test (+ ek kurban-ek.org), hesap 2: ekbaska.test; aynı bayi (reseller_id=0)
	if _, err := db.Exec(`INSERT INTO ek_alan_adlari (domain_id, alan_adi) VALUES (?, 'kurban-ek.org'), (?, 'derin.ustad-ek.net')`, ek_dom1, ek_dom1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES (?, 'blog', 'blog.ektest.test')`, ek_dom1); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, c := range []struct {
		ad, alan string
		hesap    int64
		beklenen string // "" = serbest, aksi halde hata metni parçası
	}{
		{"başka hesabın alt adı", "mal." + ek_alan1, ek_dom2, ekMsjCapraz},
		{"başka hesabın derin alt adı", "a.b." + ek_alan1, ek_dom2, ekMsjCapraz},
		{"başka hesabın ana adı birebir", ek_alan1, ek_dom2, ekMsjKayitli},
		{"başka hesabın ek alan adı birebir", "kurban-ek.org", ek_dom2, ekMsjKayitli},
		{"başka hesabın ek alan adının alt adı", "x.kurban-ek.org", ek_dom2, ekMsjCapraz},
		{"başka hesabın ek alan adının ÜST adı", "org-kok.kurban-ek.org", ek_dom2, ekMsjCapraz},
		{"başka hesabın ek alanının ÜST adını açma (ad, mevcut ek alanın üst adı)", "ustad-ek.net", ek_dom2, ekMsjCapraz},
		{"başka hesabın ana alan adının ÜST adını açma", "test", ek_dom2, ekMsjCapraz},
		{"üst ad (mevcut ek alan alt adı)", "kurban-ek.org", ek_dom2, ekMsjKayitli},
		{"alt alan adı olarak kullanımda", "blog." + ek_alan1, ek_dom2, ekMsjCapraz},
		{"kendi ana adının alt adı", "yeni." + ek_alan1, ek_dom1, ekMsjKendi},
		{"kendi ek alanıyla birebir", "kurban-ek.org", ek_dom1, ekMsjKayitli},
		{"serbest", "tamamen-ayri.net", ek_dom2, ""},
	} {
		err := ekHesapAdAlaniDenetle(ctx, db, c.hesap, c.alan)
		if c.beklenen == "" {
			if err != nil {
				t.Errorf("%s: serbest beklenirdi: %v", c.ad, err)
			}
			continue
		}
		var mh mesajliHata
		if err == nil || !asMesajli(err, &mh) || !strings.Contains(mh.Mesaj(false), c.beklenen) {
			t.Errorf("%s (%s): %q bekleniyordu, alınan %v", c.ad, c.alan, c.beklenen, err)
			continue
		}
		// Bayi/müşteri metni başka hesabın adını/sahibini içermez; yönetici ayrıntı görür.
		if s := mh.Mesaj(false); strings.Contains(s, "hesap #") || strings.Contains(s, ek_alan1) && !strings.Contains(c.alan, ek_alan1) {
			t.Errorf("%s: müşteri metni başka hesabı sızdırıyor: %s", c.ad, s)
		}
	}
	// Uçtan uca: takeover isteği 409 döner, satır/vhost/bölge oluşmaz.
	d := ek_dikisler(t)
	kod, govde := ek_ekle(t, db, ek_dom2, "mal."+ek_alan1)
	if kod != http.StatusConflict || strings.Contains(govde, ek_alan1) {
		t.Fatalf("takeover: 409 (sızıntısız) beklenir: %d %s", kod, govde)
	}
	if _, err := os.Stat(ekConfPath(ek_sk2, "mal."+ek_alan1)); err == nil || len(*d.zoneYaz) != 0 {
		t.Fatal("takeover reddinde vhost/bölge oluşmamalı")
	}
}

func asMesajli(err error, m *mesajliHata) bool {
	for e := err; e != nil; {
		if x, ok := e.(mesajliHata); ok {
			*m = x
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// AdAlaniEkle bağlanmamışsa ekleme REDDEDİLİR (fail-closed).
func TestEkOlustur_AdAlaniDenetimiYoksa_Reddeder(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	AdAlaniEkle = nil
	kod, govde := ek_ekle(t, db, ek_dom1, "yeni.example.org")
	if kod != http.StatusInternalServerError || ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 0 {
		t.Fatalf("denetim yokken ekleme reddedilmeli: %d %s", kod, govde)
	}
}

// Ad-alanı denetleyicisi (domains.AdAlaniEkle) reddederse satır eklenmez, 409 + yönetici ayrıntısı.
func TestEkOlustur_AdAlaniDenetleyicisiReddederse409(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	AdAlaniEkle = func(ctx context.Context, db *sql.DB, ad string, kiraci int64, ekle func() error) error {
		return &ekCakismaHatasi{"bu ad panelin kendi adına ayrılmış", "panel adı"}
	}
	kod, govde := ek_ekle(t, db, ek_dom1, "panel.example.org")
	if kod != http.StatusConflict || strings.Contains(govde, "panel adı") {
		t.Fatalf("409 (ayrıntısız) beklenir: %d %s", kod, govde)
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 0 {
		t.Fatal("satır eklenmemeli")
	}
}

// ───────────────────────── IDOR ─────────────────────────

func TestEk_IDOR_BaskaHesabinEkAlani(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "gizli.example.org"); kod != http.StatusOK {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	eid := ek_eid(t, db, "gizli.example.org")
	// Saldırgan hesap 2'nin {id}'siyle hesap 1'in {eid}'sine erişmeye çalışır.
	yol := fmt.Sprintf("/domains/%d/ek-alan-adlari/%d", ek_dom2, eid)
	for _, c := range []struct {
		yontem, desen, sfx string
		isle               func(*Handlers) http.HandlerFunc
	}{
		{"GET", "/domains/{id}/ek-alan-adlari/{eid}", "", func(h *Handlers) http.HandlerFunc { return h.EkDetay }},
		{"DELETE", "/domains/{id}/ek-alan-adlari/{eid}", "?onay=gizli.example.org", func(h *Handlers) http.HandlerFunc { return h.EkSil }},
		{"GET", "/domains/{id}/ek-alan-adlari/{eid}/ssl", "", func(h *Handlers) http.HandlerFunc { return h.EkSSLDurum }},
		{"POST", "/domains/{id}/ek-alan-adlari/{eid}/ssl", "", func(h *Handlers) http.HandlerFunc { return h.EkSSLKur }},
		{"DELETE", "/domains/{id}/ek-alan-adlari/{eid}/ssl", "", func(h *Handlers) http.HandlerFunc { return h.EkSSLKaldir }},
		{"GET", "/domains/{id}/ek-alan-adlari/{eid}/dns", "", func(h *Handlers) http.HandlerFunc { return h.EkDNS }},
	} {
		sfx := ""
		if strings.HasSuffix(c.desen, "/ssl") {
			sfx = "/ssl"
		} else if strings.HasSuffix(c.desen, "/dns") {
			sfx = "/dns"
		}
		kod, govde := ek_cagir(t, db, c.yontem, c.desen, yol+sfx+c.sfx, `{"tip":"letsencrypt"}`, c.isle)
		if kod != http.StatusNotFound {
			t.Errorf("IDOR %s %s: 404 beklenir: %d %s", c.yontem, c.desen, kod, govde)
		}
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari WHERE id=?`, eid) != 1 {
		t.Fatal("başka hesabın ek alan adı SİLİNDİ")
	}
	if _, err := os.Stat(ekConfPath(ek_sk1, "gizli.example.org")); err != nil {
		t.Fatal("başka hesabın vhost'u silindi")
	}
	if len(*d.zoneSil) != 0 || d.say("/root/.acme.sh/acme.sh") != 0 {
		t.Fatal("IDOR denemesi yan etki üretti")
	}
	// Liste: yalnız kendi hesabının satırları.
	kod, govde := ek_cagir(t, db, "GET", "/domains/{id}/ek-alan-adlari", fmt.Sprintf("/domains/%d/ek-alan-adlari", ek_dom2), "",
		func(h *Handlers) http.HandlerFunc { return h.EkListe })
	if kod != 200 || strings.Contains(govde, "gizli.example.org") {
		t.Fatalf("liste başka hesabı sızdırıyor: %d %s", kod, govde)
	}
}

// ───────────────────────── yol / dosya güvenliği ─────────────────────────

func TestEk_YolGuvenligi_SembolikBaglanti(t *testing.T) {
	d := ek_dikisler(t)
	dis := filepath.Join(d.kok, "dis-hedef")
	if err := os.MkdirAll(dis, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dis, "dokunma.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := evKoku + "/" + ek_sk1
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// (1) ek-alanlar dizininin KENDİSİ dışarıya sembolik bağlantı.
	if err := os.Symlink(dis, home+"/ek-alanlar"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ekDocrootHazirla(ek_sk1, "ornek.org"); err == nil {
		t.Fatal("ek-alanlar sembolik bağlantıyken docroot oluşturulmamalı")
	}
	if _, err := os.Stat(filepath.Join(dis, "ornek.org")); err == nil {
		t.Fatal("dış hedefte dizin oluştu (kök işlemi sızdı)")
	}
	_ = os.Remove(home + "/ek-alanlar")
	// (2) alan dizini dışarıya sembolik bağlantı (kiracı oluşturabilir).
	if err := os.MkdirAll(home+"/ek-alanlar", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dis, home+"/ek-alanlar/kurban.org"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ekDocrootHazirla(ek_sk1, "kurban.org"); err == nil {
		t.Fatal("alan dizini sembolik bağlantıyken docroot kabul edilmemeli")
	}
	ekAlanDizininiSil(ek_sk1, "kurban.org")
	if _, err := os.Stat(filepath.Join(dis, "dokunma.txt")); err != nil {
		t.Fatal("sembolik bağlantı üzerinden dış dosya SİLİNDİ")
	}
	// (3) geçersiz adlar yola girmez.
	for _, kotu := range []string{"../x.org", "a/b.org", "x.org/../..", ""} {
		if _, _, err := ekDocrootHazirla(ek_sk1, kotu); err == nil {
			t.Errorf("%q kabul edilmemeli", kotu)
		}
	}
	for _, kotuSK := range []string{"root", "c_../x", "../c_x", "c_"} {
		if _, _, err := ekDocrootHazirla(kotuSK, "ornek.org"); err == nil {
			t.Errorf("sk %q kabul edilmemeli", kotuSK)
		}
	}
}

func TestEk_YolGuvenligi_Cikis(t *testing.T) {
	ek_dikisler(t)
	for _, y := range []string{evKoku + "/c_baska/ek-alanlar/x", evKoku + "/" + ek_sk1 + "/../c_baska/x", "/etc/passwd", evKoku + "/" + ek_sk1 + "x/y"} {
		if err := ekYolGuvenli(ek_sk1, y); err == nil {
			t.Errorf("%q hesap dizini dışında, kabul edilmemeli", y)
		}
	}
	if err := ekYolGuvenli(ek_sk1, ekDocroot(ek_sk1, "ornek.org")); err != nil {
		t.Errorf("geçerli docroot reddedildi: %v", err)
	}
}

func TestEkOlustur_KotuAlanAdiDosyaYazmaz(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	for _, kotu := range []string{"../../etc/x.com", "a.com\\nserver_name x", "x.com/../../y", "x.com;y"} {
		kod, govde := ek_ekle(t, db, ek_dom1, kotu)
		if kod != http.StatusBadRequest {
			t.Errorf("%q: 400 beklenir: %d %s", kotu, kod, govde)
		}
	}
	ents, _ := os.ReadDir(nginxConfD)
	if len(ents) != 0 {
		t.Fatalf("conf.d'de dosya oluştu: %v", ents)
	}
	if d.say("systemctl reload nginx") != 0 {
		t.Fatal("reddedilen istek nginx'e dokunmamalı")
	}
}

func TestEkConfPath_DosyaAdiKalibi(t *testing.T) {
	ek_dikisler(t)
	if got, want := ekConfPath("c_shop", "ornek.com"), nginxConfD+"/ek_c_shop_ornek.com.conf"; got != want {
		t.Fatalf("%s != %s", got, want)
	}
	// Komşu hesap öneki ayrıştırılabilir: alan adı '_' içermez.
	if filepath.Base(ekConfPath("c_shop", "store.com")) == filepath.Base(ekConfPath("c_shop_store", "com.conf")) {
		t.Fatal("dosya adı kalıbı komşu hesapla çakışıyor")
	}
}

// ───────────────────────── vhost şablonu / geri alma ─────────────────────────

func TestEkVhostGovde_SablonKurallari(t *testing.T) {
	ek_dikisler(t)
	govde, err := ekVhostGovde(ek_sk1, "ornek.com", "8.3")
	if err != nil {
		t.Fatal(err)
	}
	// subdomain-acme birleşiminden sonra TEK üretici acmeLocation(): alan sırası onun.
	dogruKalip := acmeLocation()
	if !strings.Contains(govde, dogruKalip) {
		t.Fatalf("ACME challenge doğru kalıpta değil (root-sahipli kök + token biçimi):\n%s", govde)
	}
	docroot := ekDocroot(ek_sk1, "ornek.com")
	// challenge, kiracının docroot'undan SUNULMAZ.
	if strings.Contains(govde, "location /.well-known/acme-challenge/ {") {
		t.Fatalf("geniş ACME konumu (docroot'tan) kullanılmamalı:\n%s", govde)
	}
	for _, satir := range strings.Split(govde, "\n") {
		if strings.Contains(satir, "acme-challenge") && strings.Contains(satir, docroot) {
			t.Fatalf("ACME konumu docroot'a işaret ediyor: %s", satir)
		}
	}
	if !strings.Contains(govde, "server_name ornek.com www.ornek.com;") {
		t.Fatalf("server_name apex + www olmalı:\n%s", govde)
	}
	if strings.Contains(govde, "server_name ornek.com;") {
		t.Fatalf("takma adsız server_name kalmamalı:\n%s", govde)
	}
	if !strings.Contains(govde, "access_log /var/log/nginx/ornek.com.access.log;") ||
		!strings.Contains(govde, "error_log  /var/log/nginx/ornek.com.error.log warn;") {
		t.Fatalf("günlük dosya adı alan adı olmalı (www takma adı sızmamalı):\n%s", govde)
	}
	if !strings.Contains(govde, "root "+docroot+";") || !strings.Contains(govde, "fastcgi_pass unix:/run/php-fpm/"+ek_sk1+".sock;") {
		t.Fatalf("docroot + hesap PHP havuzu soketi:\n%s", govde)
	}
	if !strings.Contains(govde, "X-Content-Type-Options") {
		t.Fatalf("güvenlik başlıkları (alt alanla aynı) eksik:\n%s", govde)
	}
}

// SSL'li şablon: 443 bloğunda da server_name apex+www ve sertifika yolları.
func TestEkVhostGovde_SSL(t *testing.T) {
	ek_dikisler(t)
	crt, key := certYolu(ek_sk1, "ornek.com")
	if err := os.MkdirAll(filepath.Dir(crt), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(crt, []byte("x"), 0o644)
	_ = os.WriteFile(key, []byte("x"), 0o600)
	govde, err := ekVhostGovde(ek_sk1, "ornek.com", "8.3")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(govde, "server_name ornek.com www.ornek.com;") != 2 || !strings.Contains(govde, "listen 443 ssl;") ||
		!strings.Contains(govde, "ssl_certificate     "+crt+";") || !strings.Contains(govde, "return 301 https://$host$request_uri;") {
		t.Fatalf("SSL şablonu:\n%s", govde)
	}
}

// 🔴 `nginx -t` başarısızsa TÜM değişiklik geri alınır: satır, vhost, docroot.
func TestEkOlustur_NginxTBasarisiz_TumDegisiklikGeriAlinir(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	*d.nginxHata = true
	kod, govde := ek_ekle(t, db, ek_dom1, "bozuk.example.org")
	if kod != http.StatusInternalServerError {
		t.Fatalf("500 beklenir: %d %s", kod, govde)
	}
	if strings.Contains(govde, "komsu.conf") || strings.Contains(govde, "unknown directive") {
		t.Fatalf("ham nginx çıktısı (komşu yapılandırma) müşteriye sızdı: %s", govde)
	}
	if n := ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`); n != 0 {
		t.Fatalf("satır geri alınmadı: %d", n)
	}
	if _, err := os.Stat(ekConfPath(ek_sk1, "bozuk.example.org")); err == nil {
		t.Fatal("bozuk vhost dosyası kaldı")
	}
	if _, err := os.Stat(ekDocrootTaban(ek_sk1) + "/bozuk.example.org"); err == nil {
		t.Fatal("docroot geri alınmadı")
	}
	if len(*d.zoneYaz) != 0 {
		t.Fatal("başarısız oluşturmada bölge yazılmamalı")
	}
	// Mevcut (çalışan) vhost: yeniden kurma başarısızsa ESKİ içerik geri konur.
	*d.nginxHata = false
	if kod, govde := ek_ekle(t, db, ek_dom1, "calisan.example.org"); kod != http.StatusOK {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	conf := ekConfPath(ek_sk1, "calisan.example.org")
	once, _ := os.ReadFile(conf)
	*d.nginxHata = true
	crt, key := certYolu(ek_sk1, "calisan.example.org")
	_ = os.MkdirAll(filepath.Dir(crt), 0o755)
	_ = os.WriteFile(crt, []byte("x"), 0o644)
	_ = os.WriteFile(key, []byte("x"), 0o600)
	if err := ekVhostYenidenKurDB(context.Background(), db, ek_eid(t, db, "calisan.example.org")); err == nil {
		t.Fatal("nginx -t başarısızken hata beklenir")
	}
	sonra, _ := os.ReadFile(conf)
	if string(once) != string(sonra) {
		t.Fatal("başarısız yeniden kurma eski vhost'u geri koymadı")
	}
}

// ───────────────────────── oluşturma / DNS / silme ─────────────────────────

func TestEkOlustur_Tamam_DNSVeVhost(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	kod, govde := ek_ekle(t, db, ek_dom1, "  YENI.Example.ORG ")
	if kod != http.StatusOK || !strings.Contains(govde, `"alan_adi":"yeni.example.org"`) {
		t.Fatalf("200 beklenir: %d %s", kod, govde)
	}
	if _, err := os.Stat(ekConfPath(ek_sk1, "yeni.example.org")); err != nil {
		t.Fatal("vhost yazılmadı")
	}
	if _, err := os.Stat(ekDocroot(ek_sk1, "yeni.example.org") + "/index.html"); err != nil {
		t.Fatal("docroot/başlangıç sayfası yok")
	}
	if n := ek_sayi(t, db, `SELECT COUNT(*) FROM ek_dns_kayitlari`); n != 1 || len(*d.zoneYaz) != 1 {
		t.Fatalf("DNS tohumu/bölge yazımı: kayıt=%d yazım=%d", n, len(*d.zoneYaz))
	}
	if !strings.Contains(d.komutlar(), "nginx -t") || d.say("systemctl reload nginx") == 0 {
		t.Fatalf("nginx -t + reload bekleniyordu:\n%s", d.komutlar())
	}
	// Aynı ad ikinci kez → 409 (satır sayısı artmaz).
	if kod, _ := ek_ekle(t, db, ek_dom1, "yeni.example.org"); kod != http.StatusConflict {
		t.Fatalf("aynı ad ikinci kez: 409 beklenir: %d", kod)
	}
	if kod, _ := ek_ekle(t, db, ek_dom2, "yeni.example.org"); kod != http.StatusConflict {
		t.Fatalf("başka hesapta aynı ad: 409 beklenir: %d", kod)
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 1 {
		t.Fatal("tekrar eklemede satır arttı")
	}
}

func TestEkOlustur_BolgeHatasiOlumcuDegil(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	ekZoneYaz = func(ctx context.Context, db *sql.DB, ekID int64) error {
		return fmt.Errorf("named-checkzone başarısız")
	}
	kod, govde := ek_ekle(t, db, ek_dom1, "zonehata.example.org")
	if kod != http.StatusOK || !strings.Contains(govde, "uyarilar") {
		t.Fatalf("bölge hatası uyarı olmalı, istek başarılı: %d %s", kod, govde)
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 1 {
		t.Fatal("bölge hatası ek alan adını geri almamalı")
	}
}

func TestEkOlustur_DemoVeGecersizHesap(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	if _, err := db.Exec(`UPDATE domains SET is_demo=1 WHERE id=?`, ek_dom2); err != nil {
		t.Fatal(err)
	}
	if kod, _ := ek_ekle(t, db, ek_dom2, "demo.example.org"); kod != http.StatusForbidden {
		t.Fatalf("demo: 403 beklenir: %d", kod)
	}
	if kod, _ := ek_ekle(t, db, 99999, "yok.example.org"); kod != http.StatusNotFound {
		t.Fatalf("olmayan hesap: 404 beklenir: %d", kod)
	}
	if _, err := db.Exec(`UPDATE domains SET sistem_kullanici='root' WHERE id=?`, ek_dom1); err != nil {
		t.Fatal(err)
	}
	if kod, _ := ek_ekle(t, db, ek_dom1, "kok.example.org"); kod != http.StatusBadRequest {
		t.Fatalf("c_ öneksiz sistem kullanıcısı: 400 beklenir: %d", kod)
	}
}

func TestEkSil_TemizlikVeSira(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "silinecek.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	eid := ek_eid(t, db, "silinecek.example.org")
	crt, key := certYolu(ek_sk1, "silinecek.example.org")
	_ = os.MkdirAll(filepath.Dir(crt), 0o755)
	_ = os.WriteFile(crt, []byte("x"), 0o644)
	_ = os.WriteFile(key, []byte("x"), 0o600)
	// Bölge silme çağrıldığında satır ARTIK yok olmalı (include DB'den üretilir).
	ekZoneSil = func(ctx context.Context, db *sql.DB, ad string) error {
		if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari WHERE alan_adi=?`, ad) != 0 {
			t.Error("bölge silinirken satır hâlâ duruyor")
		}
		*d.zoneSil = append(*d.zoneSil, ad)
		return nil
	}
	yol := fmt.Sprintf("/domains/%d/ek-alan-adlari/%d", ek_dom1, eid)
	// Onay metni yoksa (yönetici dışı) ya da yanlışsa 400, hiçbir şey silinmez.
	for _, sfx := range []string{"", "?onay=baska.org"} {
		if kod, _ := ek_cagir(t, db, "DELETE", "/domains/{id}/ek-alan-adlari/{eid}", yol+sfx, "",
			func(h *Handlers) http.HandlerFunc { return h.EkSil }); kod != http.StatusBadRequest {
			t.Fatalf("onaysız silme 400 olmalı (%q): %d", sfx, kod)
		}
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 1 {
		t.Fatal("onaysız istek sildi")
	}
	if kod, govde := ek_cagir(t, db, "DELETE", "/domains/{id}/ek-alan-adlari/{eid}", yol+"?onay=SILINECEK.example.org", "",
		func(h *Handlers) http.HandlerFunc { return h.EkSil }); kod != 200 {
		t.Fatalf("silme: %d %s", kod, govde)
	}
	for _, p := range []string{ekConfPath(ek_sk1, "silinecek.example.org"), crt, key, ekDocrootTaban(ek_sk1) + "/silinecek.example.org"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("kalıntı: %s", p)
		}
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 0 || ek_sayi(t, db, `SELECT COUNT(*) FROM ek_dns_kayitlari`) != 0 {
		t.Fatal("satır/DNS kayıtları (CASCADE) silinmedi")
	}
	if len(*d.zoneSil) != 1 || (*d.zoneSil)[0] != "silinecek.example.org" {
		t.Fatalf("bölge silme: %v", *d.zoneSil)
	}
	if !strings.Contains(d.komutlar(), "acme.sh --remove") {
		t.Fatalf("acme kaydı kaldırılmadı:\n%s", d.komutlar())
	}
}

// Hesap silinirken: tüm ek alan adlarının vhost/cert/bölge/satırı temizlenir.
func TestEkAlanlariTemizle_HesapSilme(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	for _, a := range []string{"bir.example.org", "iki.example.org"} {
		if kod, govde := ek_ekle(t, db, ek_dom1, a); kod != 200 {
			t.Fatalf("kurulum: %d %s", kod, govde)
		}
	}
	if kod, govde := ek_ekle(t, db, ek_dom2, "baskasi.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	EkAlanlariTemizle(context.Background(), db, ek_dom1, ek_sk1)
	for _, a := range []string{"bir.example.org", "iki.example.org"} {
		if _, err := os.Stat(ekConfPath(ek_sk1, a)); err == nil {
			t.Errorf("%s vhost'u kaldı", a)
		}
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari WHERE domain_id=?`, ek_dom1) != 0 {
		t.Fatal("satırlar silinmedi")
	}
	if _, err := os.Stat(ekConfPath(ek_sk2, "baskasi.example.org")); err != nil || ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari WHERE domain_id=?`, ek_dom2) != 1 {
		t.Fatal("başka hesabın ek alan adı etkilendi")
	}
	if len(*d.zoneSil) != 2 {
		t.Fatalf("2 bölge silinmeliydi: %v", *d.zoneSil)
	}
}

// ───────────────────────── SSL ─────────────────────────

func TestEkAcmeHataSinifla(t *testing.T) {
	for _, c := range []struct{ cikti, parca string }{
		{"Error creating new order :: too many certificates already issued rateLimited", "hız sınırına"},
		{"DNS problem: NXDOMAIN looking up A for ornek.com", "çözümlenmiyor"},
		{"Verify error:Fetching http://x/.well-known/acme-challenge/y: Timeout during connect", "doğrulama dosyasına erişemedi"},
		{"Invalid response from http://x: 404", "doğrulama dosyasına erişemedi"},
		{"bilinmeyen bir hata", "alınamadı"},
	} {
		if got := ekAcmeHataSinifla(c.cikti); !strings.Contains(got, c.parca) {
			t.Errorf("%q → %q (%q bekleniyordu)", c.cikti, got, c.parca)
		}
	}
}

func TestEkSSLHizSiniri(t *testing.T) {
	ek_dikisler(t)
	t0 := time.Now()
	ekSimdi = func() time.Time { return t0 }
	for i := 0; i < ekSSLSaatlikDeneme; i++ {
		if !ekSSLHizIzin(ek_sk1) {
			t.Fatalf("%d. deneme izinli olmalı", i+1)
		}
	}
	if ekSSLHizIzin(ek_sk1) {
		t.Fatal("sınır aşıldı: reddedilmeli")
	}
	if !ekSSLHizIzin(ek_sk2) {
		t.Fatal("sınır HESAP başınadır; başka hesap etkilenmez")
	}
	ekSimdi = func() time.Time { return t0.Add(ekSSLPencere + time.Minute) }
	if !ekSSLHizIzin(ek_sk1) {
		t.Fatal("pencere geçince yeniden izin verilmeli")
	}
}

func ek_ssl(t *testing.T, db *sql.DB, eid int64, dom int64, tip string) (int, string) {
	t.Helper()
	return ek_cagir(t, db, "POST", "/domains/{id}/ek-alan-adlari/{eid}/ssl",
		fmt.Sprintf("/domains/%d/ek-alan-adlari/%d/ssl", dom, eid), `{"tip":"`+tip+`"}`,
		func(h *Handlers) http.HandlerFunc { return h.EkSSLKur })
}

func TestEkSSL_OzImza_VeKaldir(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "ssl.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	eid := ek_eid(t, db, "ssl.example.org")
	if kod, govde := ek_ssl(t, db, eid, ek_dom1, "self-signed"); kod != 200 {
		t.Fatalf("öz-imza: %d %s", kod, govde)
	}
	if !strings.Contains(d.komutlar(), "subjectAltName=DNS:ssl.example.org,DNS:www.ssl.example.org") {
		t.Fatalf("öz-imza apex + www kapsamalı:\n%s", d.komutlar())
	}
	b, _ := os.ReadFile(ekConfPath(ek_sk1, "ssl.example.org"))
	if !strings.Contains(string(b), "listen 443 ssl;") {
		t.Fatalf("SSL sonrası vhost HTTPS bloğu yok:\n%s", b)
	}
	if ek_sayi(t, db, `SELECT ssl_aktif FROM ek_alan_adlari WHERE id=?`, eid) != 1 {
		t.Fatal("ssl_aktif işaretlenmedi")
	}
	kod, govde := ek_cagir(t, db, "DELETE", "/domains/{id}/ek-alan-adlari/{eid}/ssl",
		fmt.Sprintf("/domains/%d/ek-alan-adlari/%d/ssl", ek_dom1, eid), "",
		func(h *Handlers) http.HandlerFunc { return h.EkSSLKaldir })
	if kod != 200 {
		t.Fatalf("kaldır: %d %s", kod, govde)
	}
	b, _ = os.ReadFile(ekConfPath(ek_sk1, "ssl.example.org"))
	if strings.Contains(string(b), "listen 443") {
		t.Fatalf("SSL kaldırınca HTTP'ye dönmeli:\n%s", b)
	}
	if ek_sayi(t, db, `SELECT ssl_aktif FROM ek_alan_adlari WHERE id=?`, eid) != 0 {
		t.Fatal("ssl_aktif temizlenmedi")
	}
}

func TestEkSSL_LetsEncrypt_KurulumVeHataSiniflama(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "le.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	eid := ek_eid(t, db, "le.example.org")
	norm := komut
	// (1) başarı: acme.sh --issue + --install-cert; apex + www; kök-sahipli webroot.
	if kod, govde := ek_ssl(t, db, eid, ek_dom1, "letsencrypt"); kod != 200 {
		t.Fatalf("LE: %d %s", kod, govde)
	}
	k := d.komutlar()
	if !strings.Contains(k, "-d le.example.org -d www.le.example.org") || !strings.Contains(k, "--webroot "+acmeKoku()) ||
		!strings.Contains(k, "--install-cert") {
		t.Fatalf("acme.sh çağrısı:\n%s", k)
	}
	if strings.Contains(k, "--webroot "+ekDocroot(ek_sk1, "le.example.org")) {
		t.Fatal("challenge kiracının docroot'una yazılmamalı")
	}
	// (2) DNS/challenge hatası: sınıflanmış sade mesaj, ham çıktı müşteriye gitmez.
	komut = func(ad string, arg ...string) ([]byte, error) {
		if strings.HasSuffix(ad, "acme.sh") && len(arg) > 0 && arg[0] == "--issue" {
			return []byte("Verify error: Fetching http://le/.well-known: Timeout during connect (likely firewall problem) /root/.acme.sh/secret"), fmt.Errorf("exit 1")
		}
		return norm(ad, arg...)
	}
	kod, govde := ek_ssl(t, db, eid, ek_dom1, "letsencrypt")
	if kod != http.StatusBadRequest || !strings.Contains(govde, "doğrulama dosyasına erişemedi") || strings.Contains(govde, "/root/.acme.sh") {
		t.Fatalf("LE hata sınıflama: %d %s", kod, govde)
	}
	// (3) RENEW_SKIP (çıkış kodu 2) hata DEĞİL.
	renewErr := exec.Command("sh", "-c", "exit 2").Run()
	komut = func(ad string, arg ...string) ([]byte, error) {
		if strings.HasSuffix(ad, "acme.sh") && len(arg) > 0 && arg[0] == "--issue" {
			return []byte("Skipping. Next renewal time is"), renewErr
		}
		return norm(ad, arg...)
	}
	if kod, govde := ek_ssl(t, db, eid, ek_dom1, "letsencrypt"); kod != 200 {
		t.Fatalf("RENEW_SKIP başarı sayılmalı: %d %s", kod, govde)
	}
	// (4) saatlik hız sınırı: LE tüketilmez, 429.
	komut = norm
	var son int
	for i := 0; i < ekSSLSaatlikDeneme+1; i++ {
		son, _ = ek_ssl(t, db, eid, ek_dom1, "letsencrypt")
	}
	if son != http.StatusTooManyRequests {
		t.Fatalf("hız sınırı: 429 beklenir, alınan %d", son)
	}
	// Geçersiz tip.
	if kod, _ := ek_ssl(t, db, eid, ek_dom1, "x509"); kod != http.StatusBadRequest {
		t.Fatalf("geçersiz tip: 400 beklenir: %d", kod)
	}
}

// Oluştururken "SSL'i hemen dene": LE başarısız olsa da ek alan adı oluşur, hata sonuçta.
func TestEkOlustur_SSLHemenDene_HataOlumcuDegil(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	norm := komut
	komut = func(ad string, arg ...string) ([]byte, error) {
		if strings.HasSuffix(ad, "acme.sh") && len(arg) > 0 && arg[0] == "--issue" {
			return []byte("DNS problem: NXDOMAIN looking up A"), fmt.Errorf("exit 1")
		}
		return norm(ad, arg...)
	}
	kod, govde := ek_cagir(t, db, http.MethodPost, "/domains/{id}/ek-alan-adlari",
		fmt.Sprintf("/domains/%d/ek-alan-adlari", ek_dom1), `{"alan_adi":"henuz-dns-yok.example.org","ssl":"letsencrypt"}`,
		func(h *Handlers) http.HandlerFunc { return h.EkOlustur })
	if kod != 200 || !strings.Contains(govde, `"ok":false`) || !strings.Contains(govde, "çözümlenmiyor") {
		t.Fatalf("ssl hatası sonuçta, istek başarılı olmalı: %d %s", kod, govde)
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_alan_adlari`) != 1 {
		t.Fatal("ek alan adı oluşmalıydı")
	}
}

// ───────────────────────── askı / heal ─────────────────────────

func ek_askiya(t *testing.T, db *sql.DB, dom int64, v int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE domains SET askida=? WHERE id=?`, v, dom); err != nil {
		t.Fatal(err)
	}
}

func TestEk_HesapAskisi_503_GidisDonusAyniVhost(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	for _, a := range []string{"bir.example.org", "iki.example.org"} {
		if kod, govde := ek_ekle(t, db, ek_dom1, a); kod != 200 {
			t.Fatalf("kurulum: %d %s", kod, govde)
		}
	}
	oku := func(a string) string {
		b, err := os.ReadFile(ekConfPath(ek_sk1, a))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	once := oku("bir.example.org")
	if !strings.Contains(once, "fastcgi_pass") {
		t.Fatalf("askı öncesi vhost yayında olmalı:\n%s", once)
	}
	ek_askiya(t, db, ek_dom1, 1)
	// Hesap askısının kullandığı giriş noktası (ana + alt alan + ek alan üreticileri).
	if err := AltAlanVhostlariniYenidenKur(context.Background(), db, ek_dom1); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"bir.example.org", "iki.example.org"} {
		g := oku(a)
		if !strings.Contains(g, "return 503;") || strings.Contains(g, "fastcgi_pass") || !strings.Contains(g, "server_name "+a+" www."+a+";") {
			t.Fatalf("askıdaki hesabın ek vhost'u 503 olmalı (%s):\n%s", a, g)
		}
	}
	// Askıdayken ek alan SSL'i / yeniden kurma askıyı DELMEZ.
	if err := ekVhostYenidenKurDB(context.Background(), db, ek_eid(t, db, "bir.example.org")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(oku("bir.example.org"), "return 503;") {
		t.Fatal("yeniden kurma askıyı deldi")
	}
	ek_askiya(t, db, ek_dom1, 0)
	if err := AltAlanVhostlariniYenidenKur(context.Background(), db, ek_dom1); err != nil {
		t.Fatal(err)
	}
	if sonra := oku("bir.example.org"); sonra != once {
		t.Fatalf("askıdan alınca vhost BAYT BAYT askı öncesi olmalı:\n--- önce\n%s\n--- sonra\n%s", once, sonra)
	}
	// Askı durumu okunamazsa vhost'a DOKUNULMAZ.
	ek_askiya(t, db, ek_dom1, 1)
	if err := AltAlanVhostlariniYenidenKur(context.Background(), db, ek_dom1); err != nil {
		t.Fatal(err)
	}
	g := oku("bir.example.org")
	_ = db.Close()
	if err := ekVhostYenidenKurDB(context.Background(), db, 1); err == nil {
		t.Fatal("DB yokken hata beklenir")
	}
	if oku("bir.example.org") != g {
		t.Fatal("durum okunamazken vhost değişmemeli")
	}
}

// Açılış onarımı: eksik/bozuk vhost yeniden kurulur, DNS tohumlanır; İKİNCİ koşu
// sapma yoksa nginx'e DOKUNMAZ (idempotent).
func TestHealEkAlanlar_Idempotent(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "heal.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	conf := ekConfPath(ek_sk1, "heal.example.org")
	asil, _ := os.ReadFile(conf)
	// (1) vhost elle silindi (yedekten dönüş vb.) + DNS kayıtları yok.
	_ = os.Remove(conf)
	if _, err := db.Exec(`DELETE FROM ek_dns_kayitlari`); err != nil {
		t.Fatal(err)
	}
	HealEkAlanlar(db, "203.0.113.7")
	if b, err := os.ReadFile(conf); err != nil || string(b) != string(asil) {
		t.Fatal("heal vhost'u aynen yeniden kurmalı")
	}
	if ek_sayi(t, db, `SELECT COUNT(*) FROM ek_dns_kayitlari`) == 0 {
		t.Fatal("heal DNS kayıtlarını tohumlamalı")
	}
	// (2) bozulmuş içerik.
	_ = os.WriteFile(conf, []byte("server { bozuk; }\n"), 0o644)
	HealEkAlanlar(db, "203.0.113.7")
	if b, _ := os.ReadFile(conf); string(b) != string(asil) {
		t.Fatal("heal sapmış vhost'u düzeltmeli")
	}
	// (3) sapma yok → reload YOK.
	once := d.say("systemctl reload nginx")
	HealEkAlanlar(db, "203.0.113.7")
	HealEkAlanlar(db, "203.0.113.7")
	if d.say("systemctl reload nginx") != once {
		t.Fatal("sapma yokken heal nginx'e dokunmamalı (idempotent)")
	}
	// güvensiz kayıt (DB elle bozulmuş) atlanır.
	if _, err := db.Exec(`INSERT INTO ek_alan_adlari (domain_id, alan_adi) VALUES (?, 'x.com/../../evil')`, ek_dom1); err != nil {
		t.Fatal(err)
	}
	HealEkAlanlar(db, "203.0.113.7")
	ents, _ := os.ReadDir(nginxConfD)
	for _, e := range ents {
		if strings.Contains(e.Name(), "evil") {
			t.Fatalf("güvensiz kayıt için dosya yazıldı: %s", e.Name())
		}
	}
}

// Liste: limit ve alt limit alanları, askı durumu, sızıntısız.
func TestEkListe_LimitVeDurum(t *testing.T) {
	db := ek_db(t)
	ek_dikisler(t)
	ek_limit(t, db, 2)
	if kod, govde := ek_ekle(t, db, ek_dom1, "liste.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	kod, govde := ek_cagir(t, db, "GET", "/domains/{id}/ek-alan-adlari", fmt.Sprintf("/domains/%d/ek-alan-adlari", ek_dom1), "",
		func(h *Handlers) http.HandlerFunc { return h.EkListe })
	if kod != 200 {
		t.Fatalf("liste: %d %s", kod, govde)
	}
	for _, parca := range []string{`"alan_adi":"liste.example.org"`, `"eklenebilir":false`, `"kullanim":2`, `"alt_limit":{"limit":0`, `"ip":"203.0.113.7"`, `"durum":"aktif"`} {
		if !strings.Contains(govde, parca) {
			t.Errorf("liste yanıtında %s yok: %s", parca, govde)
		}
	}
	ek_askiya(t, db, ek_dom1, 1)
	_, govde = ek_cagir(t, db, "GET", "/domains/{id}/ek-alan-adlari", fmt.Sprintf("/domains/%d/ek-alan-adlari", ek_dom1), "",
		func(h *Handlers) http.HandlerFunc { return h.EkListe })
	if !strings.Contains(govde, `"durum":"askida"`) {
		t.Errorf("askı durumu: %s", govde)
	}
}
