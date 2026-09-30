package subdomain

// hesap_aski_test.go — askı HESAP düzeyindedir (kullanıcı kararı 2026-09-30):
// hesap askıdayken alt alan adı vhost'u da 503 askı sayfasıdır ve alt alan adı
// oluşturma, SSL verme, PHP sürümü değişimi gibi yeniden render yolları askıyı
// DELEMEZ; askıdan alınınca vhost bayt bayt askı öncesi hâline döner.
//
// Gerçek (geçici) MariaDB: GOSP_TEST_GECICI_DSN (go_test db:true); yoksa ATLANIR.
// Dış etkili adımlar dikişlerle sahte: ev dizini, nginx/httpd conf.d ve ACME
// kökü geçici dizinde, komutlar (nginx -t, systemctl, openssl, chown …) kayda
// alınır — ana makinenin /etc, /home, /var'ına ve servislerine dokunulmaz.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

const (
	ha_sk    = "c_haskitest"
	ha_alan  = "haskitest.test"
	ha_domID = 4242
)

// ha_db — geçici MariaDB'de alt alan render'ının okuduğu tablolar.
func ha_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_altaski_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			is_demo TINYINT NOT NULL DEFAULT 0, askida TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE subdomanlar (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, alt_ad VARCHAR(63) NOT NULL, tam_ad VARCHAR(253) NOT NULL,
			php_surum VARCHAR(8) NOT NULL DEFAULT '8.3', web_backend VARCHAR(16) NOT NULL DEFAULT 'php-fpm',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE subdomain_erisim_kisit (subdomain_id BIGINT UNSIGNED PRIMARY KEY,
			aktif TINYINT NOT NULL DEFAULT 0, varsayilan VARCHAR(8) NOT NULL DEFAULT 'izin')`,
		`CREATE TABLE subdomain_erisim_kurallari (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			subdomain_id BIGINT UNSIGNED NOT NULL, tip VARCHAR(8) NOT NULL, cidr VARCHAR(64) NOT NULL,
			aciklama VARCHAR(190) NOT NULL DEFAULT '', sira INT NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (?,?,?)`,
		ha_domID, ha_alan, ha_sk); err != nil {
		t.Fatal(err)
	}
	return db
}

// ha_dikisler — ev dizini, nginx/httpd conf.d, ACME kökü geçici dizine; komutlar
// kayda alınır. openssl sahtesi -keyout/-out dosyalarını gerçekten yazar (SSL
// verme yolunun sertifikayı "bulması" için).
func ha_dikisler(t *testing.T) (kok string, calisan *[]string) {
	t.Helper()
	e1, e2, e3, e4, e5, e6 := komut, acmeKoku, evKoku, nginxConfD, httpdConfD, vhostYenidenKur
	t.Cleanup(func() { komut, acmeKoku, evKoku, nginxConfD, httpdConfD, vhostYenidenKur = e1, e2, e3, e4, e5, e6 })
	kok = t.TempDir()
	evKoku, nginxConfD, httpdConfD = kok+"/home", kok+"/nginx", kok+"/httpd"
	for _, d := range []string{evKoku, nginxConfD, httpdConfD} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	acmeKoku = func() string { return kok + "/acme" }
	vhostYenidenKur = rebuildVhostDB
	calisan = &[]string{}
	komut = func(ad string, arg ...string) ([]byte, error) {
		*calisan = append(*calisan, ad+" "+strings.Join(arg, " "))
		if ad == "openssl" {
			for i := 0; i+1 < len(arg); i++ {
				if arg[i] == "-keyout" || arg[i] == "-out" {
					if err := os.WriteFile(arg[i+1], []byte("sahte"), 0o600); err != nil {
						return nil, err
					}
				}
			}
		}
		return nil, nil
	}
	return kok, calisan
}

func ha_askiya(t *testing.T, db *sql.DB, askida int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE domains SET askida=? WHERE id=?`, askida, ha_domID); err != nil {
		t.Fatal(err)
	}
}

func ha_altEkle(t *testing.T, db *sql.DB, alt string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES (?,?,?)`,
		ha_domID, alt, alt+"."+ha_alan)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func ha_conf(t *testing.T, alt string) string {
	t.Helper()
	b, err := os.ReadFile(confPath(ha_sk, alt))
	if err != nil {
		t.Fatalf("alt alan vhost'u okunamadı: %v", err)
	}
	return string(b)
}

// ha_askiVhostu — 503 askı vhost'u mu? (yayında içerik/PHP yok, ACME açık)
func ha_askiVhostu(t *testing.T, govde, tamAd string) {
	t.Helper()
	if !strings.Contains(govde, "return 503;") || !strings.Contains(govde, "_gosp_askida.html") {
		t.Fatalf("askıdaki hesabın alt alan adı 503 askı vhost'u olmalı:\n%s", govde)
	}
	for _, yasak := range []string{"fastcgi_pass", "proxy_pass", "try_files $uri $uri/", "root " + evKoku} {
		if strings.Contains(govde, yasak) {
			t.Fatalf("askı vhost'unda yayın izi %q var:\n%s", yasak, govde)
		}
	}
	if !strings.Contains(govde, "server_name "+tamAd+";") {
		t.Fatalf("askı vhost'u yalnız alt alan adını dinlemeli (%s):\n%s", tamAd, govde)
	}
	if !strings.Contains(govde, "location /.well-known/acme-challenge/ {\n        root /var/www/acme;") {
		t.Fatalf("askıda ACME HTTP-01 ana vhost'la aynı kökten açık kalmalı:\n%s", govde)
	}
}

func ha_cagir(t *testing.T, db *sql.DB, yontem, desen, yol, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Method(yontem, desen, isle(&Handlers{DB: db}))
	req := httptest.NewRequest(yontem, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Askıdaki hesaba alt alan adı eklenirse yayında DOĞMAZ.
func TestHesapAskida_AltAlanOlusturma_503Dogar(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	ha_askiya(t, db, 1)
	kod, govde := ha_cagir(t, db, http.MethodPost, "/domains/{id}/subdomain",
		fmt.Sprintf("/domains/%d/subdomain", ha_domID), `{"alt_ad":"blog"}`,
		func(h *Handlers) http.HandlerFunc { return h.Olustur })
	if kod != http.StatusOK {
		t.Fatalf("oluşturma 200 beklenirdi: %d %s", kod, govde)
	}
	ha_askiVhostu(t, ha_conf(t, "blog"), "blog."+ha_alan)
}

// Askıdaki hesabın alt alan vhost'u yeniden kurulunca (sapma iyileştirici,
// koruma/erişim, web sunucu değişimi — hepsi bu üreticiden geçer) 503 olur.
func TestHesapAskida_AltAlanYenidenKur_503(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "magaza")
	ha_askiya(t, db, 1)
	if err := rebuildVhostDB(context.Background(), db, sid, ha_sk, "magaza", "magaza."+ha_alan, "8.3"); err != nil {
		t.Fatalf("yeniden kurma: %v", err)
	}
	ha_askiVhostu(t, ha_conf(t, "magaza"), "magaza."+ha_alan)
}

// PHP sürümü değişimi askıyı delmez (vhost 503 kalır, alt alan havuzu kurulmaz).
func TestHesapAskida_AltAlanPHPDegisimi_AskiyiDelmez(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "api")
	ha_askiya(t, db, 1)
	kod, govde := ha_cagir(t, db, http.MethodPut, "/domains/{id}/subdomain/{sid}/php",
		fmt.Sprintf("/domains/%d/subdomain/%d/php", ha_domID, sid), `{"php_surum":"8.3"}`,
		func(h *Handlers) http.HandlerFunc { return h.PHPDegistir })
	if kod != http.StatusOK {
		t.Fatalf("PHP değişimi 200 beklenirdi: %d %s", kod, govde)
	}
	ha_askiVhostu(t, ha_conf(t, "api"), "api."+ha_alan)
}

// SSL verme askıyı delmez: sertifika kurulur, 80 VE 443 blokları 503 döner.
func TestHesapAskida_AltAlanSSL_AskiyiDelmez(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "shop")
	if err := os.MkdirAll(sslDir(ha_sk), 0o750); err != nil {
		t.Fatal(err)
	}
	ha_askiya(t, db, 1)
	kod, govde := ha_cagir(t, db, http.MethodPost, "/domains/{id}/subdomain/{sid}/ssl",
		fmt.Sprintf("/domains/%d/subdomain/%d/ssl", ha_domID, sid), `{"tip":"self-signed"}`,
		func(h *Handlers) http.HandlerFunc { return h.SSLKur })
	if kod != http.StatusOK {
		t.Fatalf("SSL kurma 200 beklenirdi: %d %s", kod, govde)
	}
	conf := ha_conf(t, "shop")
	ha_askiVhostu(t, conf, "shop."+ha_alan)
	crt, _ := certYolu(ha_sk, "shop."+ha_alan)
	if !strings.Contains(conf, "listen 443 ssl;") || !strings.Contains(conf, "ssl_certificate     "+crt+";") ||
		strings.Count(conf, "return 503;") != 2 {
		t.Fatalf("sertifikalı askı vhost'unda 443 bloğu da 503 olmalı:\n%s", conf)
	}
}

// Gidiş-dönüş: askıya al → 503; askıdan al → vhost BAYT BAYT askı öncesi hâli.
func TestHesapAskida_AltAlanGidisDonus_AyniVhost(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "panel")
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO subdomain_erisim_kisit (subdomain_id, aktif, varsayilan) VALUES (?,1,'red')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subdomain_erisim_kurallari (subdomain_id, tip, cidr, sira) VALUES (?, 'izin', '198.51.100.0/24', 10)`, sid); err != nil {
		t.Fatal(err)
	}
	kur := func() string {
		t.Helper()
		if err := rebuildVhostDB(ctx, db, sid, ha_sk, "panel", "panel."+ha_alan, "8.3"); err != nil {
			t.Fatalf("yeniden kurma: %v", err)
		}
		return ha_conf(t, "panel")
	}
	once := kur()
	if !strings.Contains(once, "fastcgi_pass unix:") || !strings.Contains(once, "allow 198.51.100.0/24;") {
		t.Fatalf("askı öncesi vhost yayında (PHP + erişim kısıtı) olmalı:\n%s", once)
	}
	ha_askiya(t, db, 1)
	ha_askiVhostu(t, kur(), "panel."+ha_alan)
	ha_askiya(t, db, 0)
	if sonra := kur(); sonra != once {
		t.Fatalf("askıdan alınınca vhost askı öncesiyle AYNI olmalı:\n--- önce\n%s\n--- sonra\n%s", once, sonra)
	}
}
