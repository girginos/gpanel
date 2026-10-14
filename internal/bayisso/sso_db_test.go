package bayisso

// sso_db_test.go — bayi paneline tek tık giriş uçtan uca: gerçek chi (/api/v1 altında,
// RoutePattern gerçek), RequireAuth + gerçek API anahtarı doğrulaması (gerçek 0128 göçü)
// + varsayılan-reddet izin tablosu + bayi kapısı tüketicisi + geçici MariaDB
// (GOSP_TEST_GECICI_DSN yoksa atlanır; yalnız kendi açtığı veritabanını DROP eder).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"
)

var bsSecret = []byte("bayisso-test-jwt-anahtari-0123456789-abcdef")

const (
	bayiA, bayiB = int64(7), int64(8)
	adres        = "https://bayi.test:9444"
)

func geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_bayisso_%d", time.Now().UnixNano())
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
			full_name VARCHAR(128) NULL, role VARCHAR(16) NOT NULL DEFAULT 'user',
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NULL,
			last_activity_ts BIGINT NOT NULL DEFAULT 0, last_login_at DATETIME NULL, last_login_ip VARCHAR(64) NULL)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0, askida TINYINT NULL)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL,
			ip VARCHAR(64) NULL, action VARCHAR(64) NOT NULL,
			target VARCHAR(255) NULL, detail JSON NULL, ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, full_name, role) VALUES (1,'root',NULL,'admin'),
			(7,'bayi_a','Bayi A Ltd','reseller'), (8,'bayi_b',NULL,'reseller')`,
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
		if ts := strings.TrimSpace(s); ts == "" || strings.HasPrefix(ts, "--") {
			continue
		}
		temiz = append(temiz, s)
	}
	for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if st = strings.TrimSpace(st); st == "" {
			continue
		}
		if _, err := db.Exec(st); err != nil {
			t.Fatalf("göç 0128: %v", err)
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

func tamam(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// kurulum: DB + router + sıfırlanmış paket durumu.
func kurulum(t *testing.T) (*sql.DB, http.Handler, *Handlers) {
	t.Helper()
	db := geciciDB(t)
	eskiHiz, eskiSimdi, eskiKapi, eskiKanca := ssoHizDk, simdiFn, kapiCoz, auth.HostKiraciKanca
	hizMu.Lock()
	hizKovasi = map[int64][]time.Time{}
	hizMu.Unlock()
	kodMu.Lock()
	kodlar = map[string]kodKaydi{}
	kodMu.Unlock()
	t.Cleanup(func() { ssoHizDk, simdiFn, kapiCoz, auth.HostKiraciKanca = eskiHiz, eskiSimdi, eskiKapi, eskiKanca })

	h := &Handlers{DB: db, Secret: bsSecret, LifetimeSec: 3600,
		BayiAdresi: func(*http.Request) (string, bool) { return adres, true }}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/sso-giris", h.Giris)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(bsSecret))
			r.With(middleware.AdminOnly).Post("/resellers/{id}/sso", h.Uret)
			r.With(middleware.AdminVeyaReseller).Get("/domains", tamam)
			r.With(middleware.AdminVeyaReseller).Post("/me/parola", tamam)
			r.With(middleware.AdminVeyaReseller, middleware.YalnizOturum).Post("/api-anahtarlari", tamam)
			r.With(middleware.AdminVeyaReseller, middleware.YalnizOturum).Post("/me/2fa/enable", tamam)
		})
	})
	return db, r, h
}

// anahtar: sahibi için API anahtarı ekler → "gosp <kullanıcı>:<anahtar>" başlık değeri.
func anahtar(t *testing.T, db *sql.DB, sahipID int64, rol, ad string, izinler ...string) string {
	t.Helper()
	u, err := apianahtar.Uret()
	if err != nil {
		t.Fatal(err)
	}
	ipler := []string{}
	kul := "bayi_a"
	if rol == apianahtar.RolKok {
		ipler, kul = []string{"192.0.2.0/24"}, apianahtar.KokKullaniciAdi
	}
	_, err = apianahtar.Ekle(context.Background(), db, &apianahtar.Kayit{
		SahipID: sahipID, SahipRol: rol, Ad: ad, Onek: u.Onek, Ozet: u.Ozet, Son4: u.Son4,
		Izinler: izinler, IPler: ipler, HizDk: 600, SonKullanma: time.Now().Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return "gosp " + kul + ":" + u.Tam
}

func istek(t *testing.T, h http.Handler, method, yol, yetki, govde string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, yol, bytes.NewReader([]byte(govde)))
	req.RemoteAddr = "192.0.2.10:40000"
	if yetki != "" {
		req.Header.Set("Authorization", yetki)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// kodAl: üretim yanıtından URL'yi doğrular, kodu çıkarır.
func kodAl(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var y struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
		Sn  int    `json:"gecerlilik_sn"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &y); err != nil || !y.OK || y.Sn != 60 {
		t.Fatalf("yanıt: %d %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(y.URL)
	if err != nil || u.Scheme != "https" || u.Host != "bayi.test:9444" || u.Path != "/sso" || u.RawQuery != "" {
		t.Fatalf("url biçimi: %q", y.URL)
	}
	p, _ := url.ParseQuery(u.Fragment)
	return p.Get("kod")
}

type girisYanit struct {
	Token     string `json:"token"`
	Bitis     int64  `json:"bitis"`
	SSO       bool   `json:"sso"`
	Hata      string `json:"hata"`
	Kullanici struct {
		ID      int64  `json:"id"`
		Adi     string `json:"adi"`
		Rol     string `json:"rol"`
		AdSoyad string `json:"ad_soyad"`
	} `json:"kullanici"`
}

func tuket(t *testing.T, h http.Handler, kod string) (int, girisYanit) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"kod": kod})
	rec := istek(t, h, http.MethodPost, "/api/v1/auth/sso-giris", "", string(b))
	var y girisYanit
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	return rec.Code, y
}

func kodSayisi() int {
	kodMu.Lock()
	defer kodMu.Unlock()
	return len(kodlar)
}

func TestBayiSSO_YalnizKokAnahtari_IkiIzinBirden(t *testing.T) {
	db, h, _ := kurulum(t)
	durumlar := []struct {
		ad, yetki string
		beklenen  int
	}{
		{"kimliksiz", "", http.StatusUnauthorized},
		{"bayi anahtarı (sso+bayi istese de)", anahtar(t, db, bayiA, apianahtar.RolBayi, "B", "sso:yaz"), http.StatusForbidden},
		{"kök: yalnız sso", anahtar(t, db, 1, apianahtar.RolKok, "K1", "sso:yaz"), http.StatusForbidden},
		{"kök: yalnız bayi:yaz", anahtar(t, db, 1, apianahtar.RolKok, "K2", "bayi:yaz"), http.StatusForbidden},
		{"kök: sso + bayi:oku", anahtar(t, db, 1, apianahtar.RolKok, "K3", "sso:yaz", "bayi:oku"), http.StatusForbidden},
	}
	jwt, err := auth.IssueAt(bsSecret, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	durumlar = append(durumlar, struct {
		ad, yetki string
		beklenen  int
	}{"yönetici JWT oturumu", "Bearer " + jwt, http.StatusForbidden})
	for _, d := range durumlar {
		if rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", d.yetki, ""); rec.Code != d.beklenen {
			t.Errorf("%s: %d beklenir, %d %s", d.ad, d.beklenen, rec.Code, rec.Body.String())
		}
	}
	if kodSayisi() != 0 {
		t.Fatal("yetkisiz istek kod üretti")
	}
}

func TestBayiSSO_TamAkis_TekKullanimlik_Denetim_Sizinti(t *testing.T) {
	db, h, _ := kurulum(t)
	var logTampon bytes.Buffer
	log.SetOutput(&logTampon)
	defer log.SetOutput(os.Stderr)

	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")
	rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Cache-Control: no-store olmalı")
	}
	kod := kodAl(t, rec)
	var alanlar map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &alanlar)
	if len(alanlar) != 3 || strings.Count(rec.Body.String(), kod) != 1 || len(kod) < 32 {
		t.Fatalf("yanıt gövdesi biçimi/sızıntı: %s", rec.Body.String())
	}

	g, y := tuket(t, h, kod)
	if g != http.StatusOK || y.Token == "" || !y.SSO || y.Kullanici.ID != bayiA || y.Kullanici.Rol != "reseller" ||
		y.Kullanici.Adi != "bayi_a" || y.Kullanici.AdSoyad != "Bayi A Ltd" || y.Bitis <= time.Now().Unix() {
		t.Fatalf("giriş yanıtı: %d %+v", g, y)
	}
	c, err := auth.Parse(bsSecret, y.Token)
	if err != nil || c.Role != "reseller" || c.UserID != bayiA || c.ResellerID != bayiA || !c.SSO || c.Username != "bayi_a" {
		t.Fatalf("token claim'leri: %+v %v", c, err)
	}
	// Olağan bayi işi çalışır; mevcut parolayı isteyen parola ucu açık.
	if rec := istek(t, h, http.MethodGet, "/api/v1/domains", "Bearer "+y.Token, ""); rec.Code != http.StatusOK {
		t.Fatalf("SSO oturumu bayi ucunda 200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if rec := istek(t, h, http.MethodPost, "/api/v1/me/parola", "Bearer "+y.Token, "{}"); rec.Code != http.StatusOK {
		t.Fatalf("parola ucu SSO oturumunda açık olmalı: %d", rec.Code)
	}
	// İkinci kullanım reddedilir.
	if g, _ := tuket(t, h, kod); g != http.StatusUnauthorized {
		t.Fatalf("ikinci kullanım 401 beklenir: %d", g)
	}
	// Giriş "boşta" damgasını tazeler (bayat damgayla ilk istek 401 almasın).
	var son int64
	_ = db.QueryRow(`SELECT last_activity_ts FROM users WHERE id=7`).Scan(&son)
	if son < time.Now().Unix()-60 {
		t.Fatalf("last_activity_ts tazelenmedi: %d", son)
	}

	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='bayi.sso_kod' AND actor_user_id=1
		AND actor_username='root (API anahtarı WHMCS)' AND target='bayi_a' AND reseller_id=7 AND ok=1`).Scan(&n)
	if n != 1 {
		t.Fatalf("bayi.sso_kod denetim kaydı yok: %d", n)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='auth.sso' AND actor_user_id=7 AND reseller_id=7
		AND ok=1 AND detail LIKE '%WHMCS%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("auth.sso denetim kaydı (anahtar adıyla) yok: %d", n)
	}
	// SIZINTI: kod ne denetimde ne log'da; anahtar gizlisi hiçbir yerde.
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE CONCAT_WS('|',actor_username,target,COALESCE(detail,''),COALESCE(ip,'')) LIKE ?`, "%"+kod+"%").Scan(&n)
	if n != 0 {
		t.Fatal("kod denetim kaydında")
	}
	if strings.Contains(logTampon.String(), kod) || strings.Contains(logTampon.String(), "sso#") {
		t.Fatalf("kod/URL log'a yazılmış: %s", logTampon.String())
	}
	gizli := k[strings.LastIndex(k, "_")+1:]
	if strings.Contains(logTampon.String(), gizli) || strings.Contains(rec.Body.String(), gizli) {
		t.Fatal("anahtar gizlisi sızdı")
	}
}

func TestBayiSSO_OturumdaAnahtarVe2FAYonetimiYok(t *testing.T) {
	db, h, _ := kurulum(t)
	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")
	_, y := tuket(t, h, kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, "")))
	if y.Token == "" {
		t.Fatal("token yok")
	}
	for _, yol := range []string{"/api/v1/api-anahtarlari", "/api/v1/me/2fa/enable"} {
		if rec := istek(t, h, http.MethodPost, yol, "Bearer "+y.Token, "{}"); rec.Code != http.StatusForbidden {
			t.Errorf("%s SSO oturumunda 403 beklenir: %d", yol, rec.Code)
		}
	}
	// Parolalı (olağan) bayi oturumu etkilenmez.
	olagan, err := auth.IssueResellerAt(bsSecret, 3600, bayiA, "bayi_a", bayiA, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, yol := range []string{"/api/v1/api-anahtarlari", "/api/v1/me/2fa/enable"} {
		if rec := istek(t, h, http.MethodPost, yol, "Bearer "+olagan, "{}"); rec.Code != http.StatusOK {
			t.Errorf("%s olağan oturumda 200 beklenir: %d", yol, rec.Code)
		}
	}
}

func TestBayiSSO_Aski_Silinme_Olmayan(t *testing.T) {
	db, h, _ := kurulum(t)
	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")
	// Olmayan / bayi olmayan (kök) / bozuk kimlik → 404, kod yok.
	for _, yol := range []string{"/api/v1/resellers/999/sso", "/api/v1/resellers/1/sso", "/api/v1/resellers/0/sso", "/api/v1/resellers/abc/sso"} {
		if rec := istek(t, h, http.MethodPost, yol, k, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: 404 beklenir, %d %s", yol, rec.Code, rec.Body.String())
		}
	}
	// Askıdaki bayi → 409, kod yok.
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=8`); err != nil {
		t.Fatal(err)
	}
	if rec := istek(t, h, http.MethodPost, "/api/v1/resellers/8/sso", k, ""); rec.Code != http.StatusConflict {
		t.Fatalf("askıdaki bayi 409 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if kodSayisi() != 0 {
		t.Fatal("geçersiz hedefe kod üretildi")
	}
	// Kod alındıktan SONRA askıya alınan bayi giremez (403 hesap askıda), denetim ok=0.
	kod := kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if g, y := tuket(t, h, kod); g != http.StatusForbidden || y.Token != "" {
		t.Fatalf("araya giren askı 403 beklenir: %d %+v", g, y)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='auth.sso' AND actor_user_id=7 AND ok=0`).Scan(&n)
	if n != 1 {
		t.Fatalf("başarısız giriş denetimi yok: %d", n)
	}
	// Kod alındıktan sonra silinen bayi → 401 (geçersiz kodla aynı yanıt).
	if _, err := db.Exec(`UPDATE users SET status='active' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	kod = kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	if _, err := db.Exec(`DELETE FROM users WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if g, y := tuket(t, h, kod); g != http.StatusUnauthorized || y.Token != "" {
		t.Fatalf("silinen bayi 401 beklenir: %d %+v", g, y)
	}
}

func TestBayiSSO_Sure_Kapi_Marka_Govde(t *testing.T) {
	db, h, _ := kurulum(t)
	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")

	// Süre: 60 sn sonra geçersiz.
	kod := kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	simdiFn = func() time.Time { return time.Now().Add(KodOmru + time.Second) }
	if g, _ := tuket(t, h, kod); g != http.StatusUnauthorized {
		t.Fatalf("süresi dolan kod 401 beklenir: %d", g)
	}
	simdiFn = time.Now

	// Kapı: müşteri/yönetici/bilinmeyen kapıda uç YOK (404) ve kod TÜKETİLMEZ.
	kod = kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	for _, kp := range []string{kapi.Musteri, kapi.Admin, kapi.Bilinmeyen} {
		kapiCoz = func(*http.Request) string { return kp }
		if g, _ := tuket(t, h, kod); g != http.StatusNotFound {
			t.Errorf("%s kapısında 404 beklenir: %d", kp, g)
		}
	}
	kapiCoz = func(*http.Request) string { return kapi.Bayi }
	g, y := tuket(t, h, kod)
	if g != http.StatusOK {
		t.Fatalf("bayi kapısında 200 beklenir: %d", g)
	}
	if c, err := auth.Parse(bsSecret, y.Token); err != nil || c.Kapi != kapi.Bayi {
		t.Fatalf("token bayi kapısına bağlı olmalı: %+v %v", c, err)
	}
	kapiCoz = kapi.Istek

	// Marka: başka bayinin doğrulanmış özel adresinde giriş yok (401), denetimde iz.
	kod = kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	auth.HostKiraciKanca = func(*http.Request) (int64, bool) { return bayiB, true }
	if g, y := tuket(t, h, kod); g != http.StatusUnauthorized || y.Token != "" {
		t.Fatalf("başka bayinin marka adresi 401 beklenir: %d", g)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='auth.sso.marka_host' AND ok=0`).Scan(&n)
	if n != 1 {
		t.Fatalf("marka_host denetimi yok: %d", n)
	}
	// Kendi marka adresi: giriş olur.
	auth.HostKiraciKanca = func(*http.Request) (int64, bool) { return bayiA, true }
	kod = kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""))
	if g, _ := tuket(t, h, kod); g != http.StatusOK {
		t.Fatalf("kendi marka adresinde 200 beklenir: %d", g)
	}

	// Gövde: bozuk JSON 400; kısa/uzun/bilinmeyen kod 401.
	if rec := istek(t, h, http.MethodPost, "/api/v1/auth/sso-giris", "", "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk gövde 400: %d", rec.Code)
	}
	for _, kd := range []string{"", "kisa", strings.Repeat("a", 129), strings.Repeat("b", 43)} {
		if g, _ := tuket(t, h, kd); g != http.StatusUnauthorized {
			t.Errorf("kod %q: 401 beklenir, %d", kd[:min(len(kd), 8)], g)
		}
	}
	// Üretim ucunda büyük gövde 400.
	if rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, `{"x":"`+strings.Repeat("y", 2000)+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("büyük gövde 400 beklenir: %d", rec.Code)
	}
}

func TestBayiSSO_HizSiniri_AdresYok(t *testing.T) {
	db, h, hd := kurulum(t)
	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")
	ssoHizDk = 2
	for i := 0; i < 2; i++ {
		if rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""); rec.Code != http.StatusOK {
			t.Fatalf("istek %d: %d", i, rec.Code)
		}
	}
	rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("hız sınırı 429 + Retry-After beklenir: %d", rec.Code)
	}
	ssoHizDk = 30
	hizMu.Lock()
	hizKovasi = map[int64][]time.Time{}
	hizMu.Unlock()
	// Bayi paneli adresi çözülemezse kod üretilmez (503).
	once := kodSayisi()
	hd.BayiAdresi = func(*http.Request) (string, bool) { return "", false }
	if rec := istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("adres yok 503 beklenir: %d", rec.Code)
	}
	if kodSayisi() != once {
		t.Fatal("adres yokken kod üretildi")
	}
}

// Az önce düşürülen oturumlar (parola değişimi/askı: token_gecersiz_ts = şimdi+1) ve bayat
// boşta damgası: tek tık girişin tokenı damganın gerisinde kalıp ilk istekte 401 almamalı.
func TestBayiSSO_OturumDamgasi_BostaSayaci(t *testing.T) {
	db, h, _ := kurulum(t)
	k := anahtar(t, db, 1, apianahtar.RolKok, "WHMCS", "sso:yaz", "bayi:yaz")
	if _, err := db.Exec(`UPDATE users SET token_gecersiz_ts=UNIX_TIMESTAMP()+1, last_activity_ts=UNIX_TIMESTAMP()-864000 WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	g, y := tuket(t, h, kodAl(t, istek(t, h, http.MethodPost, "/api/v1/resellers/7/sso", k, "")))
	if g != http.StatusOK || y.Token == "" {
		t.Fatalf("giriş: %d %+v", g, y)
	}
	// Başka istek atılmadan: giriş boşta damgasını kendisi tazeler.
	var son int64
	_ = db.QueryRow(`SELECT last_activity_ts FROM users WHERE id=7`).Scan(&son)
	if son < time.Now().Unix()-60 {
		t.Fatalf("last_activity_ts tazelenmedi: %d", son)
	}
	if rec := istek(t, h, http.MethodGet, "/api/v1/domains", "Bearer "+y.Token, ""); rec.Code != http.StatusOK {
		t.Fatalf("damga sonrası alınan token ilk istekte 200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
}
