package wordpress

// hedef_test.go — WordPress'in ek (addon) / alt alan adı hedefleri: hedef listesi, hedef
// farkında kurulum (docroot kilidi, URL, kota, IDOR), hesap geneli listeleme, karşılama
// sayfası tespiti, sembolik bağ/kaçış vektörleri, önbellek anahtarı, geriye uyum.
//
// DB'li testler YALNIZ geçici MariaDB (GOSP_TEST_GECICI_DSN; go_test db:true); yoksa ATLANIR.
// Dış etkili her şey sahte: ev dizini t.TempDir(), wp-cli/DB oluşturma/chown/silme kancaları.
// Canlı MariaDB'ye, runuser'a ya da gerçek wp-cli'ye ASLA gidilmez.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

const (
	wh_sk1, wh_d1 = "c_wpa", 1
	wh_sk2, wh_d2 = "c_wpb", 2
	wh_ana1       = "anaa.test"
	wh_ana2       = "anab.test"
	wh_ek1        = "ek-a.test"     // dom1, SSL var, karşılama sayfası
	wh_ek2        = "ek-nossl.test" // dom1, SSL yok, kullanıcı index.html'i
	wh_ek3        = "ek-b.test"     // dom2
	wh_alt1       = "blog.anaa.test"
	wh_eid1       = 11
	wh_eid2       = 12
	wh_eid3       = 21
	wh_sid1       = 31
)

// wh_stdout: TÜM testlerde varsayılan wp-cli okuması (gerçek runuser/wp-cli'ye gidilmez).
// Arka plan goroutine'leri (siteurl/ön-ısıtma) testten uzun yaşayabildiği için kanca test
// başına DEĞİŞTİRİLMEZ (yarış); yalnız TestMain'de bir kez atanır. Çağrılar kaydedilir.
var (
	whStdoutMu     sync.Mutex
	whStdoutYollar = map[string]bool{}
)

func wh_stdout(ctx context.Context, sk string, args ...string) ([]byte, error) {
	whStdoutMu.Lock()
	for _, a := range args {
		if strings.HasPrefix(a, "--path=") {
			whStdoutYollar[strings.TrimPrefix(a, "--path=")] = true
		}
	}
	whStdoutMu.Unlock()
	return []byte("[]"), nil
}

func TestMain(m *testing.M) {
	wpStdoutFn = wh_stdout
	os.Exit(m.Run())
}

type whOrtam struct {
	db     *sql.DB
	h      *Handlers
	ev     string
	rt     http.Handler
	mu     sync.Mutex
	calls  []string
	dbPass []string
}

func wh_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_wph_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
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
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, max_db INT NOT NULL DEFAULT 0)`,
		`CREATE TABLE customers (id BIGINT UNSIGNED PRIMARY KEY, plan_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL UNIQUE,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', cert_path VARCHAR(255) NULL,
			is_demo TINYINT NOT NULL DEFAULT 0, customer_id BIGINT UNSIGNED NULL, reseller_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE ek_alan_adlari (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, alan_adi VARCHAR(253) NOT NULL)`,
		`CREATE TABLE subdomanlar (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, alt_ad VARCHAR(63) NOT NULL, tam_ad VARCHAR(253) NOT NULL)`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL, db_user VARCHAR(64) NOT NULL DEFAULT '')`,
		`CREATE TABLE cp_wp_kurulum (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, hedef VARCHAR(500) NOT NULL,
			admin_kullanici VARCHAR(191) NOT NULL, admin_parola VARCHAR(255) NOT NULL, olusturulma TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uq_domain_hedef (domain_id, hedef(190)))`,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT NULL, actor_username VARCHAR(64) NULL,
			adina_domain_id BIGINT NULL, ip VARCHAR(64) NULL, action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail TEXT NULL,
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO service_plans (id, max_db) VALUES (1, 0)`,
		`INSERT INTO customers (id, plan_id) VALUES (1, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

func wh_yaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wh_ortam(t *testing.T) *whOrtam {
	t.Helper()
	db := wh_db(t)
	ev := t.TempDir()
	o := &whOrtam{db: db, h: &Handlers{DB: db}, ev: ev}

	eskiEv, eskiK, eskiDO, eskiDD, eskiS, eskiSil := wpEv, wpKomutFn, dbOlusturFn, dbDusurFn, sistemKomutFn, silFn
	wpEv = ev
	t.Cleanup(func() {
		wpEv, wpKomutFn, dbOlusturFn, dbDusurFn, sistemKomutFn, silFn = eskiEv, eskiK, eskiDO, eskiDD, eskiS, eskiSil
	})
	wpKomutFn = func(sk string, args ...string) ([]byte, error) {
		o.mu.Lock()
		o.calls = append(o.calls, sk+" "+strings.Join(args, " "))
		o.mu.Unlock()
		path := ""
		for _, a := range args {
			if strings.HasPrefix(a, "--path=") {
				path = strings.TrimPrefix(a, "--path=")
			}
		}
		switch {
		case len(args) >= 2 && args[0] == "core" && args[1] == "download":
			_ = os.MkdirAll(path+"/wp-includes", 0o755)
			_ = os.WriteFile(path+"/wp-includes/version.php", []byte("<?php\n$wp_version = '6.8.1';\n"), 0o644)
		case len(args) >= 2 && args[0] == "config":
			_ = os.WriteFile(path+"/wp-config.php", []byte("<?php define('DB_NAME','x');"), 0o644)
		case len(args) >= 2 && args[0] == "core" && args[1] == "version":
			return []byte("6.8.1\n"), nil
		}
		return nil, nil
	}
	dbOlusturFn = func(db *sql.DB, domainID int64, dbName, dbUser, dbPass string) error {
		o.mu.Lock()
		o.dbPass = append(o.dbPass, dbPass)
		o.mu.Unlock()
		_, err := db.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (?,?,?)`, domainID, dbName, dbUser)
		return err
	}
	dbDusurFn = func(db *sql.DB, dbName, dbUser string) error { return nil }
	sistemKomutFn = func(string, ...string) {}
	silFn = func(sk, yol string) error { return os.Remove(yol) }

	for _, s := range []string{
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, cert_path) VALUES (1,'` + wh_ana1 + `','` + wh_sk1 + `','/x/ana.crt')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (2,'` + wh_ana2 + `','` + wh_sk2 + `')`,
		fmt.Sprintf(`INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (%d,1,'%s'),(%d,1,'%s'),(%d,2,'%s')`,
			wh_eid1, wh_ek1, wh_eid2, wh_ek2, wh_eid3, wh_ek3),
		fmt.Sprintf(`INSERT INTO subdomanlar (id, domain_id, alt_ad, tam_ad) VALUES (%d,1,'blog','%s')`, wh_sid1, wh_alt1),
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	// dosya sistemi: ana (karşılama), ek1 (karşılama + SSL), ek2 (kullanıcı index.html), ek3 (yabancı), alt
	wh_yaz(t, ev+"/"+wh_sk1+"/public_html/index.html", provisioner.WelcomeHTML(wh_ana1))
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek1)+"/index.html", provisioner.WelcomeHTML(wh_ek1))
	wh_yaz(t, ev+"/"+wh_sk1+"/ssl/"+wh_ek1+".crt", "crt")
	wh_yaz(t, ev+"/"+wh_sk1+"/ssl/"+wh_ek1+".key", "key")
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek2)+"/index.html", "<h1>benim sayfam</h1>")
	wh_yaz(t, o.ekRoot(wh_sk2, wh_ek3)+"/index.html", provisioner.WelcomeHTML(wh_ek3))
	wh_yaz(t, ev+"/"+wh_sk1+"/subdomains/"+wh_alt1+"/index.html", "alt")
	wh_yaz(t, ev+"/"+wh_sk2+"/public_html/index.html", "b")

	r := chi.NewRouter()
	for _, on := range []string{"/domains/{id}/wordpress", "/domains/{id}/ek-alan-adlari/{eid}/wordpress", "/domains/{id}/subdomain/{sid}/wordpress"} {
		r.Get(on, o.h.Liste)
		r.Post(on, o.h.Kur)
		r.Delete(on, o.h.Sil)
		r.Post(on+"/guncelle", o.h.Guncelle)
		r.Get(on+"/durum", o.h.Durum)
		r.Get(on+"/eklentiler", o.h.Eklentiler)
		r.Post(on+"/eklenti", o.h.EklentiIslem)
		r.Post(on+"/arac", o.h.AracIslem)
	}
	r.Get("/domains/{id}/wordpress/hedefler", o.h.Hedefler)
	r.Get("/domains/{id}/wordpress/hesap", o.h.HesapListe)
	o.rt = r
	return o
}

func (o *whOrtam) ekRoot(sk, ad string) string {
	return o.ev + "/" + sk + "/domains/" + ad + "/public_html"
}

func (o *whOrtam) istek(method, yol, govde string) (int, string) {
	req := httptest.NewRequest(method, yol, strings.NewReader(govde))
	w := httptest.NewRecorder()
	o.rt.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func (o *whOrtam) komutlar() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

func (o *whOrtam) kurGovde(altDizin string) string {
	b, _ := json.Marshal(map[string]string{"alt_dizin": altDizin, "site_basligi": "Test", "admin_kullanici": "yonetici1", "admin_email": "a@b.test"})
	return string(b)
}

func wh_kur(o *whOrtam, yol, altDizin string) (int, string) {
	return o.istek("POST", yol, o.kurGovde(altDizin))
}

func (o *whOrtam) sayi(t *testing.T, q string, a ...any) int {
	t.Helper()
	var n int
	if err := o.db.QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (o *whOrtam) komutIcerir(parca string) bool {
	for _, c := range o.komutlar() {
		if strings.Contains(c, parca) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- hedef listesi

func wh_hedefler(t *testing.T, o *whOrtam, id int) map[string]Hedef {
	t.Helper()
	kod, govde := o.istek("GET", fmt.Sprintf("/domains/%d/wordpress/hedefler", id), "")
	if kod != 200 {
		t.Fatalf("hedefler %d: %s", kod, govde)
	}
	var l []Hedef
	if err := json.Unmarshal([]byte(govde), &l); err != nil {
		t.Fatal(err)
	}
	m := map[string]Hedef{}
	for _, h := range l {
		m[h.AlanAdi] = h
	}
	if len(m) != len(l) {
		t.Fatalf("yinelenen hedef: %v", l)
	}
	return m
}

func TestHedefler_AnaEkAlt_SSLVeDurum(t *testing.T) {
	o := wh_ortam(t)
	m := wh_hedefler(t, o, wh_d1)
	if len(m) != 4 {
		t.Fatalf("4 hedef bekleniyordu (ana+2 ek+alt): %v", m)
	}
	for _, c := range []struct {
		ad, tur, url, durum string
		ssl                 bool
		eid, sid            int64
	}{
		{wh_ana1, "ana", "https://" + wh_ana1, DocBos, true, 0, 0},
		{wh_ek1, "ek", "https://" + wh_ek1, DocBos, true, wh_eid1, 0},
		{wh_ek2, "ek", "http://" + wh_ek2, DocBos, false, wh_eid2, 0},
		{wh_alt1, "alt", "http://" + wh_alt1, DocBos, false, 0, wh_sid1},
	} {
		h, ok := m[c.ad]
		if !ok {
			t.Fatalf("%s hedefte yok", c.ad)
		}
		if h.Tur != c.tur || h.URLOnizleme != c.url || h.DocrootDurum != c.durum || h.SSL != c.ssl ||
			h.EkAlanID != c.eid || h.AltAlanID != c.sid || !h.Kurulabilir || !h.KokKurulabilir {
			t.Errorf("%s: beklenmeyen %+v", c.ad, h)
		}
	}
}

func TestHedefler_YabanciHesapYok_MutlakYolDonmez(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := o.istek("GET", "/domains/1/wordpress/hedefler", "")
	if kod != 200 {
		t.Fatal(govde)
	}
	if strings.Contains(govde, wh_ek3) || strings.Contains(govde, wh_ana2) || strings.Contains(govde, wh_sk2) {
		t.Fatalf("yabancı hesap hedefi sızdı: %s", govde)
	}
	if strings.Contains(govde, o.ev) || strings.Contains(govde, "public_html") || strings.Contains(govde, "/home") {
		t.Fatalf("mutlak docroot yolu müşteriye döndü: %s", govde)
	}
	if kod, _ := o.istek("GET", "/domains/999/wordpress/hedefler", ""); kod != 404 {
		t.Fatalf("olmayan hesap 404 olmalı, %d", kod)
	}
}

func TestHedefler_DocrootDurumlari(t *testing.T) {
	o := wh_ortam(t)
	// ek2: dolu (başka uygulama izi), ek1: wp_var
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek2)+"/app.php", "<?php")
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek1)+"/wp-config.php", "<?php")
	m := wh_hedefler(t, o, wh_d1)
	if m[wh_ek2].DocrootDurum != DocDolu || m[wh_ek2].KokKurulabilir || !m[wh_ek2].Kurulabilir || m[wh_ek2].Neden == "" {
		t.Errorf("dolu ek: %+v", m[wh_ek2])
	}
	if m[wh_ek1].DocrootDurum != DocWP || m[wh_ek1].KokKurulabilir || !m[wh_ek1].Kurulabilir || m[wh_ek1].Neden == "" {
		t.Errorf("wp_var ek: %+v", m[wh_ek1])
	}
	// ek docroot hiç yok → yok + kurulabilir=false
	if err := os.RemoveAll(o.ev + "/" + wh_sk1 + "/domains/" + wh_ek2); err != nil {
		t.Fatal(err)
	}
	m = wh_hedefler(t, o, wh_d1)
	if h := m[wh_ek2]; h.DocrootDurum != DocYok || h.Kurulabilir || h.KokKurulabilir || h.Neden == "" {
		t.Errorf("olmayan ek docroot: %+v", h)
	}
}

func TestHedefler_SembolikBagDocroot_YokSayilir(t *testing.T) {
	o := wh_ortam(t)
	// ek1'in domains/<alan> klasörü başka hesabın ağacına sembolik bağ
	dal := o.ev + "/" + wh_sk1 + "/domains/" + wh_ek1
	if err := os.RemoveAll(dal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(o.ev+"/"+wh_sk2+"/domains/"+wh_ek3, dal); err != nil {
		t.Fatal(err)
	}
	h := wh_hedefler(t, o, wh_d1)[wh_ek1]
	if h.DocrootDurum != DocYok || h.Kurulabilir {
		t.Fatalf("sembolik bağlı docroot 'yok' + kurulamaz olmalı: %+v", h)
	}
}

// ---------------------------------------------------------------- kurulum

func TestKur_EkAlan_DocrootKilitli_URLDogru_Karsilama(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	if kod != 200 {
		t.Fatalf("kurulum %d: %s", kod, govde)
	}
	root := o.ekRoot(wh_sk1, wh_ek1)
	for _, c := range o.komutlar() {
		if !strings.HasPrefix(c, wh_sk1+" ") {
			t.Errorf("komut hesap kimliğinde değil: %s", c)
		}
		if strings.Contains(c, "--path=") && !strings.Contains(c, "--path="+root) {
			t.Errorf("--path ek alan docroot'u değil: %s", c)
		}
	}
	if !o.komutIcerir("core install --url=https://"+wh_ek1+" ") || !o.komutIcerir("--path="+root) {
		t.Fatalf("kurulum --url=https://%s / --path=%s ile yapılmalı: %v", wh_ek1, root, o.komutlar())
	}
	var y struct {
		SiteURL  string `json:"site_url"`
		AdminURL string `json:"admin_url"`
	}
	_ = json.Unmarshal([]byte(govde), &y)
	if y.SiteURL != "https://"+wh_ek1 || y.AdminURL != "https://"+wh_ek1+"/wp-admin" {
		t.Errorf("yanıt URL'leri: %+v", y)
	}
	// panelin karşılama sayfası temizlendi, WP dosyaları ek docroot'unda
	if _, err := os.Stat(root + "/index.html"); err == nil {
		t.Error("panelin karşılama index.html'i temizlenmeliydi")
	}
	if _, err := os.Stat(root + "/wp-config.php"); err != nil {
		t.Error("wp-config.php ek alan docroot'unda olmalı")
	}
	// ana docroot'a ve başka hesaba dokunulmadı
	if _, err := os.Stat(o.ev + "/" + wh_sk1 + "/public_html/wp-config.php"); err == nil {
		t.Error("ana docroot'a WordPress yazıldı")
	}
	if n := o.sayi(t, `SELECT COUNT(*) FROM cp_wp_kurulum WHERE domain_id=1 AND hedef=?`, root); n != 1 {
		t.Errorf("cp_wp_kurulum kaydı ek docroot'a göre olmalı, n=%d", n)
	}
}

func TestKur_EkAlan_AltDizinli_URL(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid2), "blog")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	// ek2 SSL yok → http
	if !o.komutIcerir("--url=http://"+wh_ek2+"/blog ") || !o.komutIcerir("--path="+o.ekRoot(wh_sk1, wh_ek2)+"/blog") {
		t.Fatalf("beklenen http url/path yok: %v", o.komutlar())
	}
	// kullanıcının kendi index.html'i (karşılama değil) KORUNDU
	if b, err := os.ReadFile(o.ekRoot(wh_sk1, wh_ek2) + "/index.html"); err != nil || !strings.Contains(string(b), "benim sayfam") {
		t.Fatal("kullanıcı index.html'i silindi/değişti")
	}
}

func TestKur_EkAlan_KullaniciIndexKorunur_KokKurulum(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid2), "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	if b, err := os.ReadFile(o.ekRoot(wh_sk1, wh_ek2) + "/index.html"); err != nil || !strings.Contains(string(b), "benim sayfam") {
		t.Fatal("kullanıcının index.html'i (panel karşılaması değil) silinmemeli")
	}
}

func TestKur_EkAlan_DoluDocroot_Reddedilir(t *testing.T) {
	o := wh_ortam(t)
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek2)+"/uygulama.php", "<?php")
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid2), "")
	if kod != 409 || !strings.Contains(govde, "boş değil") {
		t.Fatalf("dolu docroot 409 olmalı: %d %s", kod, govde)
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("reddedilen kurulumda wp-cli çalışmamalı: %v", o.komutlar())
	}
	if _, err := os.Stat(o.ekRoot(wh_sk1, wh_ek2) + "/uygulama.php"); err != nil {
		t.Fatal("mevcut dosya silindi")
	}
}

func TestKur_EkAlan_ZatenWordPress_Reddedilir(t *testing.T) {
	o := wh_ortam(t)
	wh_yaz(t, o.ekRoot(wh_sk1, wh_ek1)+"/wp-config.php", "<?php")
	kod, _ := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	if kod != 409 || len(o.komutlar()) != 0 {
		t.Fatalf("mevcut WP ezilmemeli: %d %v", kod, o.komutlar())
	}
}

func TestKur_EkAlan_IDOR_YabanciVeOlmayanKimlik(t *testing.T) {
	o := wh_ortam(t)
	for _, yol := range []string{
		fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid3), // dom2'nin ek alanı, dom1 yolunda
		fmt.Sprintf("/domains/2/ek-alan-adlari/%d/wordpress", wh_eid1), // dom1'in ek alanı, dom2 yolunda
		"/domains/1/ek-alan-adlari/9999/wordpress",
		"/domains/1/ek-alan-adlari/abc/wordpress",
		fmt.Sprintf("/domains/2/subdomain/%d/wordpress", wh_sid1), // yabancı alt alan
	} {
		if kod, _ := wh_kur(o, yol, ""); kod != 404 {
			t.Errorf("POST %s: 404 bekleniyordu, %d", yol, kod)
		}
		for _, m := range []string{"GET", "DELETE"} {
			if kod, _ := o.istek(m, yol, `{}`); kod != 404 {
				t.Errorf("%s %s: 404 bekleniyordu, %d", m, yol, kod)
			}
		}
		for _, alt := range []string{"/durum", "/eklentiler"} {
			if kod, _ := o.istek("GET", yol+alt, ""); kod != 404 {
				t.Errorf("GET %s%s: 404 bekleniyordu, %d", yol, alt, kod)
			}
		}
		if kod, _ := o.istek("POST", yol+"/arac", `{"dizin":"/","islem":"cache-temizle"}`); kod != 404 {
			t.Errorf("POST %s/arac: 404 bekleniyordu, %d", yol, kod)
		}
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("IDOR denemelerinde hiçbir wp-cli çalışmamalı: %v", o.komutlar())
	}
	if _, err := os.Stat(o.ekRoot(wh_sk2, wh_ek3) + "/wp-config.php"); err == nil {
		t.Fatal("yabancı hesabın docroot'una kurulum yapıldı")
	}
}

// Hedefsiz (yanlış kimlikli) istek ANA alana düşmemeli: alt alan {sid} yanlışsa eskiden
// sessizce public_html'e düşüyordu.
func TestKur_YanlisAltAlan_AnaAlanaDusmez(t *testing.T) {
	o := wh_ortam(t)
	if kod, _ := wh_kur(o, "/domains/1/subdomain/9999/wordpress", ""); kod != 404 {
		t.Fatalf("404 bekleniyordu, %d", kod)
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("wp-cli çalışmamalı: %v", o.komutlar())
	}
}

func TestKur_EkAlan_DocrootYok_Reddedilir_DizinAcilmaz(t *testing.T) {
	o := wh_ortam(t)
	dal := o.ev + "/" + wh_sk1 + "/domains/" + wh_ek2
	if err := os.RemoveAll(dal); err != nil {
		t.Fatal(err)
	}
	kod, _ := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid2), "")
	if kod != 409 || len(o.komutlar()) != 0 {
		t.Fatalf("docroot yok → 409 + wp-cli yok: %d %v", kod, o.komutlar())
	}
	if _, err := os.Stat(dal); err == nil {
		t.Fatal("kurulum reddedilirken docroot sessizce yeniden oluşturuldu")
	}
}

func TestKur_EkAlan_SembolikBag_BaskaHesabaYonlendirme(t *testing.T) {
	o := wh_ortam(t)
	kurban := o.ev + "/" + wh_sk2 + "/domains/" + wh_ek3
	dal := o.ev + "/" + wh_sk1 + "/domains/" + wh_ek1
	if err := os.RemoveAll(dal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kurban, dal); err != nil {
		t.Fatal(err)
	}
	kod, _ := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	if kod != 400 && kod != 409 {
		t.Fatalf("sembolik bağ reddedilmeli: %d", kod)
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("wp-cli çalışmamalı: %v", o.komutlar())
	}
	if _, err := os.Stat(kurban + "/public_html/wp-config.php"); err == nil {
		t.Fatal("kurban hesabın docroot'una yazıldı")
	}
}

func TestKur_EkAlan_AltDizinKacisVektorleri(t *testing.T) {
	o := wh_ortam(t)
	yol := fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1)
	for ad, v := range map[string]string{
		"nokta-nokta": "..", "yukari": "../kardes", "gomulu-yukari": "a/../../x", "mutlak-ama-slash-kesilir-etc": "etc/passwd",
		"gizli": ".gizli", "buyuk-harf": "Blog", "bosluk": "a b", "nul": "x\x00y", "uzun": strings.Repeat("a", 64),
		"unicode": "blög", "ters-bolu": `a\b`, "noktali-bitis": "blog.", "tire-basta": "-x", "sembol": "a;b", "alt-dizin-icinde-dizin": "a/b",
	} {
		if kod, g := wh_kur(o, yol, v); kod != 400 {
			t.Errorf("%s (%q): 400 bekleniyordu, %d %s", ad, v, kod, g)
		}
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("hiçbir wp-cli çalışmamalı: %v", o.komutlar())
	}
	if _, err := os.Stat(o.ev + "/" + wh_sk1 + "/domains/" + wh_ek1 + "/kardes"); err == nil {
		t.Fatal("docroot dışına dizin açıldı")
	}
}

func TestKur_EkAlan_KotaAsildi(t *testing.T) {
	o := wh_ortam(t)
	for _, s := range []string{
		`UPDATE service_plans SET max_db=1 WHERE id=1`,
		`UPDATE domains SET customer_id=1 WHERE id=1`,
		`INSERT INTO db_accounts (domain_id, db_name) VALUES (1,'mevcut_db')`,
	} {
		if _, err := o.db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	if kod != 403 || !strings.Contains(govde, "veritabanı") {
		t.Fatalf("kota aşımı 403 + sade hata: %d %s", kod, govde)
	}
	if len(o.komutlar()) != 0 || len(o.dbPass) != 0 {
		t.Fatalf("kota aşımında DB/wp-cli çalışmamalı: %v", o.komutlar())
	}
	// kota boşsa (limit 2) kurulum geçer
	_, _ = o.db.Exec(`UPDATE service_plans SET max_db=2 WHERE id=1`)
	if kod, g := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), ""); kod != 200 {
		t.Fatalf("limit altında kurulum geçmeli: %d %s", kod, g)
	}
}

func TestKur_EkAlan_Sizinti_ParolaDenetimde_YokDBParolasiYanitta_Yok(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	if kod != 200 {
		t.Fatal(govde)
	}
	var y map[string]any
	_ = json.Unmarshal([]byte(govde), &y)
	adminParola, _ := y["admin_parola"].(string)
	if adminParola == "" {
		t.Fatal("admin parolası kurulumda BİR KEZ dönmeli (mevcut davranış)")
	}
	if len(o.dbPass) != 1 || strings.Contains(govde, o.dbPass[0]) {
		t.Fatalf("DB parolası yanıtta olmamalı (dbPass=%d)", len(o.dbPass))
	}
	var eylem, hedef, detay string
	if err := o.db.QueryRow(`SELECT action, target, COALESCE(detail,'') FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&eylem, &hedef, &detay); err != nil {
		t.Fatalf("denetim kaydı yok: %v", err)
	}
	if eylem != "wordpress.kur" || hedef != wh_ek1 {
		t.Errorf("denetim: eylem=%q hedef=%q", eylem, hedef)
	}
	if strings.Contains(detay, adminParola) || strings.Contains(detay, o.dbPass[0]) || strings.Contains(hedef, adminParola) {
		t.Fatalf("denetimde parola var: %s", detay)
	}
	for _, c := range o.komutlar() { // kayda geçen komutlar test sahtesine aittir; günlüğe değil
		_ = c
	}
}

func TestKur_AnaAlan_GeriyeUyum(t *testing.T) {
	o := wh_ortam(t)
	kod, govde := wh_kur(o, "/domains/1/wordpress", "blog")
	if kod != 200 {
		t.Fatal(govde)
	}
	root := o.ev + "/" + wh_sk1 + "/public_html"
	if !o.komutIcerir("--url=https://"+wh_ana1+"/blog ") || !o.komutIcerir("--path="+root+"/blog") {
		t.Fatalf("ana alan kurulumu aynı kalmalı: %v", o.komutlar())
	}
	// Liste: eski biçim + dizin anahtarı
	kod, g := o.istek("GET", "/domains/1/wordpress", "")
	var l []Kurulum
	_ = json.Unmarshal([]byte(g), &l)
	if kod != 200 || len(l) != 1 || l[0].Dizin != "/blog" || l[0].AlanAdi != wh_ana1 || l[0].Tur != "ana" {
		t.Fatalf("ana liste: %d %s", kod, g)
	}
}

// ---------------------------------------------------------------- listeleme / toolkit

func wh_wp(t *testing.T, dizin string) {
	t.Helper()
	wh_yaz(t, dizin+"/wp-config.php", "<?php")
	wh_yaz(t, dizin+"/wp-includes/version.php", "<?php\n$wp_version = '6.7.2';\n")
}

func TestHesapListe_TumDocrootlar_DogruAlanAdi(t *testing.T) {
	o := wh_ortam(t)
	wh_wp(t, o.ev+"/"+wh_sk1+"/public_html")
	wh_wp(t, o.ekRoot(wh_sk1, wh_ek1))
	wh_wp(t, o.ekRoot(wh_sk1, wh_ek1)+"/blog") // ek alan + 1 alt dizin
	wh_wp(t, o.ekRoot(wh_sk1, wh_ek2)+"/shop")
	wh_wp(t, o.ev+"/"+wh_sk1+"/subdomains/"+wh_alt1)
	wh_wp(t, o.ekRoot(wh_sk2, wh_ek3)) // yabancı hesap
	wh_wp(t, o.ev+"/"+wh_sk2+"/public_html")
	kod, govde := o.istek("GET", "/domains/1/wordpress/hesap", "")
	if kod != 200 {
		t.Fatal(govde)
	}
	var l []Kurulum
	if err := json.Unmarshal([]byte(govde), &l); err != nil {
		t.Fatal(err)
	}
	type k struct{ ad, tur, dizin string }
	var gor []k
	for _, x := range l {
		gor = append(gor, k{x.AlanAdi, x.Tur, x.Dizin})
		if x.Surum != "6.7.2" {
			t.Errorf("sürüm dosyadan okunmalı: %+v", x)
		}
	}
	bek := map[k]bool{
		{wh_ana1, "ana", "/ (kök)"}: true, {wh_ek1, "ek", "/ (kök)"}: true, {wh_ek1, "ek", "/blog"}: true,
		{wh_ek2, "ek", "/shop"}: true, {wh_alt1, "alt", "/ (kök)"}: true,
	}
	if len(gor) != len(bek) {
		t.Fatalf("beklenen %d kayıt, gelen %v", len(bek), gor)
	}
	for _, g := range gor {
		if !bek[g] {
			t.Errorf("beklenmeyen kayıt %v", g)
		}
	}
	if strings.Contains(govde, wh_ek3) || strings.Contains(govde, wh_sk2) {
		t.Fatalf("yabancı hesap sızdı: %s", govde)
	}
	// kimlikler: ek1 → ek_alan_id, alt → alt_alan_id
	for _, x := range l {
		if x.AlanAdi == wh_ek1 && x.EkAlanID != wh_eid1 || x.AlanAdi == wh_alt1 && x.AltAlanID != wh_sid1 {
			t.Errorf("kimlik eşleşmesi: %+v", x)
		}
	}
	// ek alan URL'si ek alan adından (ssl var → https), ana alan adı değil
	for _, x := range l {
		if x.AlanAdi == wh_ek1 && x.Dizin == "/ (kök)" && x.SiteURL != "https://"+wh_ek1 {
			t.Errorf("ek alan site_url: %s", x.SiteURL)
		}
	}
}

func TestListe_EkAlanRotasi_YalnizKendiDocrootu(t *testing.T) {
	o := wh_ortam(t)
	wh_wp(t, o.ev+"/"+wh_sk1+"/public_html")
	wh_wp(t, o.ekRoot(wh_sk1, wh_ek1))
	_, g := o.istek("GET", fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "")
	var l []Kurulum
	_ = json.Unmarshal([]byte(g), &l)
	if len(l) != 1 || l[0].AlanAdi != wh_ek1 || l[0].Tur != "ek" {
		t.Fatalf("ek alan listesi yalnız kendi docroot'u olmalı: %s", g)
	}
	_, g = o.istek("GET", "/domains/1/wordpress", "")
	l = nil
	_ = json.Unmarshal([]byte(g), &l)
	if len(l) != 1 || l[0].AlanAdi != wh_ana1 {
		t.Fatalf("ana liste yalnız public_html olmalı (geriye uyum): %s", g)
	}
}

func TestToolkit_EkAlanHedefi_DizinKilitli(t *testing.T) {
	o := wh_ortam(t)
	wh_wp(t, o.ev+"/"+wh_sk1+"/public_html")
	wh_wp(t, o.ekRoot(wh_sk1, wh_ek1))
	ek := fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1)
	// geçerli: ek docroot kökü
	if kod, g := o.istek("GET", ek+"/durum?dizin=/", ""); kod != 200 || !strings.Contains(g, `"surum":"6.7.2"`) {
		t.Fatalf("durum ek alan: %d %s", kod, g)
	}
	// kaçışlar: ana docroot'a / başka hesaba çıkış → 400
	for _, d := range []string{"/../../../public_html", "../../" + wh_sk2 + "/public_html", "/../../../../" + wh_sk2, "/yok"} {
		if kod, _ := o.istek("GET", ek+"/durum?dizin="+d, ""); kod != 400 {
			t.Errorf("dizin %q: 400 bekleniyordu, %d", d, kod)
		}
		if kod, _ := o.istek("POST", ek+"/arac", `{"dizin":"`+d+`","islem":"cache-temizle"}`); kod != 400 {
			t.Errorf("arac dizin %q: 400 bekleniyordu, %d", d, kod)
		}
	}
	// ana alan rotası (geriye uyum): dizin anahtarı aynı çalışır; ek docroot'una uzanamaz
	if kod, _ := o.istek("GET", "/domains/1/wordpress/durum?dizin=/", ""); kod != 200 {
		t.Fatalf("ana alan durum: %d", kod)
	}
	if kod, _ := o.istek("GET", "/domains/1/wordpress/durum?dizin=/../domains/"+wh_ek1+"/public_html", ""); kod != 400 {
		t.Fatalf("ana alan rotası ek docroot'una uzanamaz: %d", kod)
	}
}

// ---------------------------------------------------------------- saf birimler

func TestOnbellekAnahtari_HedefVeKiraciKapsamli(t *testing.T) {
	ana := "/home/c_a/public_html"
	ek1 := "/home/c_a/domains/x.test/public_html"
	ek2 := "/home/c_a/domains/y.test/public_html"
	diger := "/home/c_b/domains/x.test/public_html"
	seen := map[string]string{}
	for _, c := range []struct{ sk, dir string }{{"c_a", ana}, {"c_a", ek1}, {"c_a", ek2}, {"c_b", diger}, {"c_a", ek1 + "/blog"}} {
		for _, tur := range []string{"plugin", "theme", "user", "siteurl", "dbmb"} {
			key := onbAnahtar(c.sk, c.dir, tur)
			if prev, ok := seen[key]; ok {
				t.Fatalf("önbellek anahtarı çakıştı: %q hem %s hem %s/%s", key, prev, c.dir, tur)
			}
			seen[key] = c.dir + "/" + tur
		}
	}
	// aynı göreli yol, farklı kiracı → farklı anahtar
	if onbAnahtar("c_a", "/x/public_html", "plugin") == onbAnahtar("c_b", "/x/public_html", "plugin") {
		t.Fatal("kiracılar arası önbellek sızıntısı")
	}
}

func TestOnIsitma_EkAlanKurulumuIsitilir(t *testing.T) {
	dir := t.TempDir() + "/domains/isit.test/public_html"
	wpIsit("c_isit", dir)
	bitis := time.Now().Add(10 * time.Second)
	for time.Now().Before(bitis) {
		if _, surer := isitDevam.Load("c_isit\x00" + dir); !surer {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	whStdoutMu.Lock()
	defer whStdoutMu.Unlock()
	if !whStdoutYollar[dir] {
		t.Fatal("ek alan docroot'u ön-ısıtılmadı")
	}
}

func TestKarsilamaMi(t *testing.T) {
	tam := provisioner.WelcomeHTML("x.test")
	for ad, c := range map[string]struct {
		icerik string
		bek    bool
	}{
		"tam eşleşme":                 {tam, true},
		"eski sürüm işaretli":         {`<html><h1>Web siteniz hazır</h1><div class="dev">girginos</div></html>`, true},
		"kullanıcı sayfası":           {"<h1>Merhaba dünya</h1>", false},
		"tek işaret":                  {"<h1>Web siteniz hazır</h1>", false},
		"boş":                         {"", false},
		"işaretli ama çok büyük":      {"Web siteniz hazır" + `<div class="dev">girginos</div>` + strings.Repeat("a", 70<<10), false},
		"wordpress index.php içeriği": {"<?php require __DIR__.'/wp-blog-header.php';", false},
	} {
		if got := karsilamaMi([]byte(c.icerik), tam); got != c.bek {
			t.Errorf("%s: got %v want %v", ad, got, c.bek)
		}
	}
}

func TestDocrootGuvenli_Tablo(t *testing.T) {
	ev := t.TempDir()
	eski := wpEv
	wpEv = ev
	t.Cleanup(func() { wpEv = eski })
	wh_yaz(t, ev+"/c_a/domains/ok.test/public_html/x", "x")
	wh_yaz(t, ev+"/c_b/domains/kurban.test/public_html/x", "x")
	_ = os.Symlink(ev+"/c_b/domains/kurban.test", ev+"/c_a/domains/kacak.test")
	_ = os.Symlink(ev+"/c_a/domains/ok.test", ev+"/c_a/domains/ic.test") // hesap İÇİ bağ serbest
	for yol, bek := range map[string]bool{
		ev + "/c_a/domains/ok.test/public_html":            true,
		ev + "/c_a/domains/ok.test/public_html/yeni/alt":   true, // olmayan alt dizin: en yakın var olan üst çözülür
		ev + "/c_a/domains/kacak.test/public_html":         false,
		ev + "/c_a/domains/kacak.test/public_html/yeni":    false,
		ev + "/c_a/domains/ic.test/public_html":            true,
		ev + "/c_b/domains/kurban.test/public_html":        false,
		ev + "/c_a/../c_b/domains/kurban.test/public_html": false,
		"/etc": false,
	} {
		if err := docrootGuvenli("c_a", yol); (err == nil) != bek {
			t.Errorf("%s: err=%v bek ok=%v", yol, err, bek)
		}
	}
}

func TestDocrootDurum_Tablo(t *testing.T) {
	ev := t.TempDir()
	eski := wpEv
	wpEv = ev
	t.Cleanup(func() { wpEv = eski })
	root := ev + "/c_a/domains/x.test/public_html"
	if d := docrootDurum("c_a", root); d != DocYok {
		t.Fatalf("olmayan: %s", d)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if d := docrootDurum("c_a", root); d != DocBos {
		t.Fatalf("boş: %s", d)
	}
	wh_yaz(t, root+"/index.html", "x")
	wh_yaz(t, root+"/.htaccess", "x")
	if d := docrootDurum("c_a", root); d != DocBos {
		t.Fatalf("placeholder: %s", d)
	}
	wh_yaz(t, root+"/a.php", "x")
	if d := docrootDurum("c_a", root); d != DocDolu {
		t.Fatalf("dolu: %s", d)
	}
	wh_yaz(t, root+"/wp-config.php", "x")
	if d := docrootDurum("c_a", root); d != DocWP {
		t.Fatalf("wp: %s", d)
	}
	// docroot dosya ise / hesap dışı ise yok
	wh_yaz(t, ev+"/c_a/dosya", "x")
	if d := docrootDurum("c_a", ev+"/c_a/dosya"); d != DocYok {
		t.Fatalf("dosya: %s", d)
	}
	if d := docrootDurum("c_a", ev+"/c_b/x"); d != DocYok {
		t.Fatalf("dış: %s", d)
	}
}

func TestHedefAdGuvenli(t *testing.T) {
	for ad, bek := range map[string]bool{
		"ek.test": true, "a-b.example.org": true,
		"": false, "..": false, "a..b": false, ".gizli": false, "a/b": false, `a\b`: false, "a b": false, "x\x00": false,
		strings.Repeat("a", 254): false,
	} {
		if hedefAdGuvenli(ad) != bek {
			t.Errorf("%q: %v bekleniyordu", ad, bek)
		}
	}
}

// docrootDurum yalnız ek alan için kapı; alt alan ve ana alan için kök adımların güvenliği
// docrootGuvenli'ye dayanır — sembolik bağ ile başka hesaba yönlendirme reddedilmeli.
func TestKur_AltAlan_SembolikBag_BaskaHesabaYonlendirme(t *testing.T) {
	o := wh_ortam(t)
	kurban := o.ev + "/" + wh_sk2 + "/public_html"
	dal := o.ev + "/" + wh_sk1 + "/subdomains/" + wh_alt1
	if err := os.RemoveAll(dal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kurban, dal); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(kurban + "/index.html") // kurban boş: kurulumZatenVar geçerdi
	kod, _ := wh_kur(o, fmt.Sprintf("/domains/1/subdomain/%d/wordpress", wh_sid1), "")
	if kod != 400 || len(o.komutlar()) != 0 {
		t.Fatalf("alt alan docroot sembolik bağı reddedilmeli: %d %v", kod, o.komutlar())
	}
	if _, err := os.Stat(kurban + "/wp-config.php"); err == nil {
		t.Fatal("kurban hesabın docroot'una yazıldı")
	}
}

func TestKur_AltDizinSembolikBag_BaskaHesabaYonlendirme(t *testing.T) {
	o := wh_ortam(t)
	kurban := o.ev + "/" + wh_sk2 + "/public_html/bos"
	if err := os.MkdirAll(kurban, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kurban, o.ekRoot(wh_sk1, wh_ek1)+"/blog"); err != nil {
		t.Fatal(err)
	}
	kod, _ := wh_kur(o, fmt.Sprintf("/domains/1/ek-alan-adlari/%d/wordpress", wh_eid1), "blog")
	if kod != 400 || len(o.komutlar()) != 0 {
		t.Fatalf("alt dizin sembolik bağı reddedilmeli: %d %v", kod, o.komutlar())
	}
	if _, err := os.Stat(kurban + "/wp-config.php"); err == nil {
		t.Fatal("kurban dizine yazıldı")
	}
}
