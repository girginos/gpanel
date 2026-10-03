package domains

// api_parola_test.go — otomasyon parola uçları UÇTAN UCA: gerçek API anahtarı
// (göç 0128 + apianahtar.Ekle), gerçek RequireAuth → rol ara yazılımı (main.go ile
// AYNI zincir) → gerçek işleyici, geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa ATLANIR).
// FTP yazımı GERÇEK (ftp_accounts); MariaDB ALTER USER ve chpasswd SAHTE (canlı
// hesaplara dokunulmaz — gerçek ALTER hesaplar/parola_api_test.go'da).
//
// Kanıtlanan:
//   - izin ayrımı: ftp.parola DB ucunu, db.parola FTP uçlarını AÇMAZ;
//   - bayi YALNIZ kendi hostinginde (IDOR 403, başka domainin hesap adı 404);
//     kök hepsinde;
//   - FTP değişimi DB'ye, DB değişimi FTP'ye DOKUNMAZ;
//   - boş gövde → güçlü parola üretilir, yanıtta bir kez döner, denetime YAZILMAZ;
//   - denetim: eylem kodu, "<sahip> (API anahtarı <ad>)" aktörü, hedef alan adı;
//   - askıdaki hesapta parola değişir, FTP askısı (status) korunur;
//   - eski "parola:yaz" anahtarı iki yeni izne eşlenir.

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
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

var ap_sir = []byte("domains-api-parola-test-anahtari-0123456789")

type ap_sahte struct {
	mu   sync.Mutex
	db   []string // "domain:kullanici:parola"
	ssh  []string
	kayt map[string]int64 // db kullanıcısı → domain
}

func (s *ap_sahte) dbYaz(_ *sql.DB, domainID int64, kul, pw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.kayt[kul]; !ok || d != domainID {
		return hesaplar.ErrDBKullaniciYok
	}
	s.db = append(s.db, fmt.Sprintf("%d:%s:%s", domainID, kul, pw))
	return nil
}

func (s *ap_sahte) sshEsitle(_ *sql.DB, sk string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ssh = append(s.ssh, sk)
	return nil
}

func (s *ap_sahte) dbSayi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.db)
}

func ap_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_apiparola_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eski := hesaplar.FTPParolaHash("Eski-Parola-12345")
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			role VARCHAR(16) NOT NULL DEFAULT 'user', status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NOT NULL DEFAULT 0, last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NULL,
			is_demo TINYINT NOT NULL DEFAULT 0, ssh_erisim TINYINT NOT NULL DEFAULT 0,
			askida TINYINT NOT NULL DEFAULT 0, askida_kaynak VARCHAR(8) NOT NULL DEFAULT '')`,
		`CREATE TABLE ftp_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL,
			username VARCHAR(64) NOT NULL UNIQUE, password_md5 VARCHAR(255) NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL, db_pass_plain TEXT NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost')`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail JSON NULL,
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0, adina_domain_id BIGINT UNSIGNED NULL)`,
		`INSERT INTO users (id, username, role) VALUES (1,'admin','admin'), (2,'bayi2','reseller'), (3,'bayi3','reseller')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, ssh_erisim, askida) VALUES
			(20,'bayi2.test','c_bayi2',2,1,0), (21,'askida.test','c_askida',2,1,1), (30,'kurban.test','c_kurban',3,0,0),
			(40,'kok.test','c_kok',NULL,0,0)`,
		`INSERT INTO ftp_accounts (domain_id, username, password_md5, status) VALUES
			(20,'c_bayi2','` + eski + `','active'), (21,'c_askida','` + eski + `','suspended'),
			(30,'c_kurban','` + eski + `','active'), (40,'c_kok','` + eski + `','active')`,
		`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES
			(20,'bayi2_wp','bayi2_db','DB-ESKI'), (30,'kurban_wp','kurban_db','DB-ESKI')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	b, err := os.ReadFile("../../migrations/0128_api_anahtarlari.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(b), "\n") {
		if ts := strings.TrimSpace(s); ts != "" && !strings.HasPrefix(ts, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if st = strings.TrimSpace(st); st != "" {
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0128: %v", err)
			}
		}
	}
	return db
}

func ap_kur(t *testing.T) (*sql.DB, http.Handler, *ap_sahte) {
	t.Helper()
	db := ap_db(t)
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	s := &ap_sahte{kayt: map[string]int64{"bayi2_db": 20, "kurban_db": 30}}
	eD, eS := apiDBParolaYaz, apiSSHEsitle
	apiDBParolaYaz, apiSSHEsitle = s.dbYaz, s.sshEsitle
	t.Cleanup(func() { apiDBParolaYaz, apiSSHEsitle = eD, eS })
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(ap_sir))
			// main.go ile AYNI zincirler.
			r.With(middleware.MusteriScope, middleware.AdinaYasak).Put("/domains/{id}/ftp/password", h.SetFTPPassword)
			r.With(middleware.SahipYonetim).Put("/domains/{id}/ftp-accounts/{kullanici}/password", h.FTPHesapParola)
			r.With(middleware.SahipYonetim).Put("/domains/{id}/databases/users/{kullanici}/password", h.DBKullaniciParola)
		})
	})
	return db, r, s
}

type ap_anahtar struct{ tam, kul string }

func ap_anahtarEkle(t *testing.T, db *sql.DB, sahip int64, rol string, izinler ...string) ap_anahtar {
	t.Helper()
	u, err := apianahtar.Uret()
	if err != nil {
		t.Fatal(err)
	}
	k := &apianahtar.Kayit{SahipID: sahip, SahipRol: rol, Ad: "WHMCS", Onek: u.Onek, Ozet: u.Ozet, Son4: u.Son4,
		Izinler: izinler, HizDk: 600, SonKullanma: time.Now().Add(time.Hour).Unix()}
	kul := fmt.Sprintf("bayi%d", sahip)
	if rol == apianahtar.RolKok {
		k.IPler = []string{"192.0.2.0/24"}
		kul = "root"
	}
	if _, err := apianahtar.Ekle(context.Background(), db, k); err != nil {
		t.Fatal(err)
	}
	return ap_anahtar{tam: u.Tam, kul: kul}
}

type ap_yanit struct {
	kod   int
	govde map[string]any
	ham   string
}

func ap_istek(t *testing.T, h http.Handler, yetki, yol string, govde any) ap_yanit {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1"+yol, bytes.NewReader(b))
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("Authorization", yetki)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	y := ap_yanit{kod: w.Code, ham: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &y.govde)
	return y
}

func (a ap_anahtar) yetki() string { return "gosp " + a.kul + ":" + a.tam }

func ap_ftpHash(t *testing.T, db *sql.DB, kul string) (hash, durum string) {
	t.Helper()
	if err := db.QueryRow(`SELECT password_md5, status FROM ftp_accounts WHERE username=?`, kul).Scan(&hash, &durum); err != nil {
		t.Fatal(err)
	}
	return
}

func ap_dbParola(t *testing.T, db *sql.DB, kul string) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE db_user=?`, kul).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

const apGuclu = "Yeni-Guclu-Parola-2026"

func TestAPIParola_FTPIzinVeBagimsizlik(t *testing.T) {
	db, h, s := ap_kur(t)
	ftp := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz")
	eskiHash, _ := ap_ftpHash(t, db, "c_bayi2")

	y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp/password", map[string]string{"password": apGuclu})
	if y.kod != 200 || y.govde["password"] != nil || y.govde["uretildi"] != false {
		t.Fatalf("bayi ftp.parola kendi hostingi: %d %s", y.kod, y.ham)
	}
	yeni, _ := ap_ftpHash(t, db, "c_bayi2")
	if yeni == eskiHash || !hesaplar.FTPParolaDogrula(apGuclu, yeni) {
		t.Fatal("FTP parolası $6$ olarak değişmeli")
	}
	// 🔴 FTP ↔ DB BAĞIMSIZ: DB parolasına/MariaDB'ye dokunulmadı.
	if s.dbSayi() != 0 || ap_dbParola(t, db, "bayi2_db") != "DB-ESKI" {
		t.Fatal("FTP parola değişimi DB parolasına DOKUNMAMALI")
	}
	if len(s.ssh) != 1 || s.ssh[0] != "c_bayi2" {
		t.Fatalf("SSH açık hostingte aynı Linux hesabının SSH parolası eşitlenmeli: %v", s.ssh)
	}
	// Ek FTP hesabı ucu (ana hesap adıyla da çalışır).
	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp-accounts/c_bayi2/password", map[string]string{"password": apGuclu + "x1"}); y.kod != 200 {
		t.Fatalf("ftp-accounts ucu: %d %s", y.kod, y.ham)
	}
	// İzin ayrımı: ftp.parola DB ucunu açmaz.
	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/databases/users/bayi2_db/password", map[string]string{"password": apGuclu}); y.kod != 403 {
		t.Fatalf("ftp.parola ile DB ucu 403 olmalı: %d %s", y.kod, y.ham)
	}
	if s.dbSayi() != 0 {
		t.Fatal("izinsiz DB değişimi yapıldı")
	}
	// Denetim: eylem, aktör, hedef; parola yok.
	var aktor, hedef, detay string
	var okv int
	if err := db.QueryRow(`SELECT actor_username, target, COALESCE(detail,''), ok FROM audit_log WHERE action='ftp.parola_api' ORDER BY id LIMIT 1`).
		Scan(&aktor, &hedef, &detay, &okv); err != nil {
		t.Fatalf("denetim kaydı yok: %v", err)
	}
	if aktor != "bayi2 (API anahtarı WHMCS)" || hedef != "bayi2.test" || okv != 1 || !strings.Contains(detay, "c_bayi2") {
		t.Fatalf("denetim kaydı yanlış: aktör=%q hedef=%q detay=%q ok=%d", aktor, hedef, detay, okv)
	}
	var sizinti int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE detail LIKE ? OR target LIKE ?`, "%"+apGuclu+"%", "%"+apGuclu+"%").Scan(&sizinti)
	if sizinti != 0 {
		t.Fatal("parola denetime yazılmamalı")
	}
}

func TestAPIParola_DBIzinVeBagimsizlik(t *testing.T) {
	db, h, s := ap_kur(t)
	dbk := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "db.parola:yaz")
	eskiHash, _ := ap_ftpHash(t, db, "c_bayi2")
	y := ap_istek(t, h, dbk.yetki(), "/domains/20/databases/users/bayi2_db/password", map[string]string{"password": apGuclu})
	if y.kod != 200 || y.govde["db_kullanici"] != "bayi2_db" {
		t.Fatalf("bayi db.parola kendi kullanıcısı: %d %s", y.kod, y.ham)
	}
	if s.dbSayi() != 1 || s.db[0] != "20:bayi2_db:"+apGuclu {
		t.Fatalf("DB parola çağrısı: %v", s.db)
	}
	// 🔴 DB ↔ FTP BAĞIMSIZ.
	if h2, _ := ap_ftpHash(t, db, "c_bayi2"); h2 != eskiHash || len(s.ssh) != 0 {
		t.Fatal("DB parola değişimi FTP/SSH parolasına DOKUNMAMALI")
	}
	// İzin ayrımı: db.parola FTP uçlarını açmaz.
	for _, yol := range []string{"/domains/20/ftp/password", "/domains/20/ftp-accounts/c_bayi2/password"} {
		if y := ap_istek(t, h, dbk.yetki(), yol, map[string]string{"password": apGuclu}); y.kod != 403 {
			t.Fatalf("db.parola ile %s 403 olmalı: %d", yol, y.kod)
		}
	}
	if h2, _ := ap_ftpHash(t, db, "c_bayi2"); h2 != eskiHash {
		t.Fatal("izinsiz FTP değişimi yapıldı")
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='db.parola_api' AND target='bayi2.test' AND actor_username='bayi2 (API anahtarı WHMCS)'`).Scan(&n)
	if n != 1 {
		t.Fatalf("db.parola_api denetim kaydı: %d", n)
	}
}

// Bayi kapsamı (IDOR): başka bayinin hostingi 403; kendi hostinginde başka
// domainin hesap adı 404; hiçbir şey değişmez. Kök her hostingde.
func TestAPIParola_BayiKapsamiVeKok(t *testing.T) {
	db, h, s := ap_kur(t)
	bayi := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz", "db.parola:yaz")
	kurbanHash, _ := ap_ftpHash(t, db, "c_kurban")
	g := map[string]string{"password": apGuclu}
	for _, c := range []struct {
		ad, yol string
		kod     int
	}{
		{"başka bayinin ana FTP'si", "/domains/30/ftp/password", 403},
		{"başka bayinin FTP hesabı", "/domains/30/ftp-accounts/c_kurban/password", 403},
		{"başka bayinin DB kullanıcısı", "/domains/30/databases/users/kurban_db/password", 403},
		{"kök hostingi", "/domains/40/ftp/password", 403},
		{"olmayan domain", "/domains/999/ftp-accounts/c_kurban/password", 403},
		{"kendi domaininde komşunun FTP adı", "/domains/20/ftp-accounts/c_kurban/password", 404},
		{"kendi domaininde komşunun DB kullanıcısı", "/domains/20/databases/users/kurban_db/password", 404},
		{"sistem DB hesabı", "/domains/20/databases/users/root/password", 404},
	} {
		if y := ap_istek(t, h, bayi.yetki(), c.yol, g); y.kod != c.kod {
			t.Errorf("%s: %d (beklenen %d) %s", c.ad, y.kod, c.kod, y.ham)
		}
	}
	if hh, _ := ap_ftpHash(t, db, "c_kurban"); hh != kurbanHash || s.dbSayi() != 0 || ap_dbParola(t, db, "kurban_db") != "DB-ESKI" {
		t.Fatal("bayi başka hostingin parolasını değiştirdi (IDOR)")
	}
	kok := ap_anahtarEkle(t, db, 1, apianahtar.RolKok, "ftp.parola:yaz", "db.parola:yaz")
	if y := ap_istek(t, h, kok.yetki(), "/domains/30/ftp-accounts/c_kurban/password", g); y.kod != 200 {
		t.Fatalf("kök her hostingte: %d %s", y.kod, y.ham)
	}
	if y := ap_istek(t, h, kok.yetki(), "/domains/30/databases/users/kurban_db/password", g); y.kod != 200 {
		t.Fatalf("kök DB: %d %s", y.kod, y.ham)
	}
	if y := ap_istek(t, h, kok.yetki(), "/domains/999/ftp/password", g); y.kod != 404 {
		t.Fatalf("kök olmayan domain 404: %d", y.kod)
	}
}

func TestAPIParola_UretimPolitikaVeAski(t *testing.T) {
	db, h, _ := ap_kur(t)
	k := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz", "db.parola:yaz")
	// Boş gövde → üretilen güçlü parola yanıtta BİR KEZ; denetime yazılmaz.
	y := ap_istek(t, h, k.yetki(), "/domains/20/ftp/password", nil)
	pw, _ := y.govde["password"].(string)
	if y.kod != 200 || y.govde["uretildi"] != true || pw == "" {
		t.Fatalf("boş gövde: %d %s", y.kod, y.ham)
	}
	if ok, neden := hesaplar.ParolaGucluMu(pw); !ok {
		t.Fatalf("üretilen parola panel kuralını geçmeli: %s", neden)
	}
	if hh, _ := ap_ftpHash(t, db, "c_bayi2"); !hesaplar.FTPParolaDogrula(pw, hh) {
		t.Fatal("üretilen parola FTP'ye yazılmalı")
	}
	var sizinti int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE detail LIKE ?`, "%"+pw+"%").Scan(&sizinti)
	if sizinti != 0 {
		t.Fatal("üretilen parola denetime yazılmamalı")
	}
	if y := ap_istek(t, h, k.yetki(), "/domains/20/databases/users/bayi2_db/password", map[string]string{}); y.kod != 200 || y.govde["password"] == nil {
		t.Fatalf("DB boş parola → üretim: %d %s", y.kod, y.ham)
	}
	// Politika: panelin kuralı (>=12, harf+rakam) ve şifreli değer kâhini kapısı.
	for _, kotu := range []string{"kisa1", "sadeceharflerdenoluşan", "123456789012345", "gos1:Abcdef123456789", strings.Repeat("a1", 70)} {
		if y := ap_istek(t, h, k.yetki(), "/domains/20/ftp/password", map[string]string{"password": kotu}); y.kod != 400 {
			t.Errorf("zayıf/geçersiz parola %q: %d", kotu, y.kod)
		}
	}
	if y := ap_istek(t, h, k.yetki(), "/domains/20/ftp/password", map[string]string{"password": apGuclu, "parola": "Baska-Parola-123"}); y.kod != 400 {
		t.Errorf("çelişen password/parola 400 olmalı: %d", y.kod)
	}
	// Askıdaki hesap: parola değişir, FTP askısı (status) korunur.
	if y := ap_istek(t, h, k.yetki(), "/domains/21/ftp/password", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("askıdaki hesap: %d %s", y.kod, y.ham)
	}
	hh, durum := ap_ftpHash(t, db, "c_askida")
	if !hesaplar.FTPParolaDogrula(apGuclu, hh) || durum != "suspended" {
		t.Fatalf("askıda parola değişmeli, FTP girişi kapalı kalmalı: durum=%s", durum)
	}
}

// Eski "parola:yaz" anahtarı iki yeni izne eşlenir; yanlış kullanıcı adı / eski
// Bearer biçimi reddedilir; arayüz (JWT) yolu değişmedi.
func TestAPIParola_EskiIzinBicimVeJWT(t *testing.T) {
	db, h, s := ap_kur(t)
	eski := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "parola:yaz")
	g := map[string]string{"password": apGuclu}
	if y := ap_istek(t, h, eski.yetki(), "/domains/20/ftp/password", g); y.kod != 200 {
		t.Fatalf("eski parola:yaz → ftp.parola: %d %s", y.kod, y.ham)
	}
	if y := ap_istek(t, h, eski.yetki(), "/domains/20/databases/users/bayi2_db/password", g); y.kod != 200 {
		t.Fatalf("eski parola:yaz → db.parola: %d %s", y.kod, y.ham)
	}
	k := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz")
	if y := ap_istek(t, h, "gosp bayi3:"+k.tam, "/domains/20/ftp/password", g); y.kod != 401 {
		t.Fatalf("yanlış kullanıcı adı 401: %d", y.kod)
	}
	if y := ap_istek(t, h, "Bearer "+k.tam, "/domains/20/ftp/password", g); y.kod != 401 {
		t.Fatalf("eski Bearer biçimi 401: %d", y.kod)
	}
	// JWT bayi: arayüz ucu eski sözleşmesiyle (parola alanı, üretim YOK).
	tok, err := auth.IssueResellerAt(ap_sir, 3600, 2, "bayi2", 2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if y := ap_istek(t, h, "Bearer "+tok, "/domains/20/ftp/password", map[string]string{}); y.kod != 400 {
		t.Fatalf("arayüz yolu boş parolada 400 kalmalı: %d %s", y.kod, y.ham)
	}
	// JWT bayi yeni uçlarda da kapsamlı (eylem kodu oturum kodu).
	if y := ap_istek(t, h, "Bearer "+tok, "/domains/30/databases/users/kurban_db/password", g); y.kod != 403 {
		t.Fatalf("JWT bayi IDOR: %d", y.kod)
	}
	once := s.dbSayi()
	if y := ap_istek(t, h, "Bearer "+tok, "/domains/20/databases/users/bayi2_db/password", g); y.kod != 200 || s.dbSayi() != once+1 {
		t.Fatalf("JWT bayi kendi DB kullanıcısı: %d %s", y.kod, y.ham)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='db.parola' AND actor_username='bayi2'`).Scan(&n)
	if n != 1 {
		t.Fatalf("JWT yolu db.parola kodu ve düz aktörle denetlenmeli: %d", n)
	}
}
