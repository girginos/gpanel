package adina

// adina_db_test.go — müşteri ADINA giriş (impersonation) uçtan uca: gerçek chi
// router + RequireAuth/SahipYonetim/MusteriScope/AdinaYasak + gerçek imzalı JWT +
// geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır; yalnız kendi açtığı
// gosp_test_adina_<zaman> veritabanını kullanır ve DROP eder).
//
// Kanıtlanan: IDOR (bayi A → bayi B domaini), müşteri bu ucu çağıramaz, kod tek
// kullanımlık ve süreli, değişim yalnız müşteri kapısında, kimlik işlemi 403,
// askıdaki hesaba girilir ama kök askısına bayi giremez, devir/aktör askısı
// açık oturumu düşürür, uzun ömürlü adına-token reddedilir, denetim aktörle yazılır.

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
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"
)

var adSecret = []byte("adina-test-jwt-anahtari-0123456789-abcdef")

const (
	bayiA, bayiB = int64(7), int64(8)
	domA, domB   = int64(42), int64(43)
)

func adGeciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_adina_%d", time.Now().UnixNano())
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
			last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			ftp_user VARCHAR(64) NOT NULL DEFAULT '', askida TINYINT NULL, askida_kaynak VARCHAR(8) NULL,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			username VARCHAR(64) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NULL)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL,
			ip VARCHAR(64) NULL, action VARCHAR(64) NOT NULL,
			target VARCHAR(255) NULL, detail JSON NULL, ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (7,'bayi_a','reseller'), (8,'bayi_b','reseller')`,
		`INSERT INTO domains (id, alan_adi, ftp_user, reseller_id) VALUES (42,'a-musteri.test','amus',7), (43,'b-musteri.test','bmus',8)`,
		`INSERT INTO ftp_accounts (id, domain_id, username) VALUES (5,42,'amus'), (6,43,'bmus')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

func adOK(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func adRouter(db *sql.DB) http.Handler {
	h := &Handlers{DB: db, Secret: adSecret}
	r := chi.NewRouter()
	r.Post("/musteri/adina-giris", h.Giris)
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(adSecret))
		r.Post("/musteri/adina-cikis", h.Cikis)
		r.With(middleware.SahipYonetim).Post("/domains/{id}/adina-oturum", h.Baslat)
		r.With(middleware.MusteriScope).Get("/domains/{id}", adOK)
		r.With(middleware.MusteriScope, middleware.AdinaYasak).Put("/domains/{id}/ftp/password", adOK)
	})
	return r
}

func adIstek(t *testing.T, h http.Handler, method, yol, tok string, govde any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(method, yol, bytes.NewReader(b))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bayiTok(t *testing.T, uid int64) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(adSecret, 3600, uid, fmt.Sprintf("bayi_%d", uid), uid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func adminTok(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueAt(adSecret, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// kodAl: Baslat → (HTTP kodu, giriş kodu).
func kodAl(t *testing.T, h http.Handler, tok string, domainID int64) (int, string) {
	t.Helper()
	rec := adIstek(t, h, http.MethodPost, fmt.Sprintf("/domains/%d/adina-oturum", domainID), tok, nil)
	var y struct {
		Kod string `json:"kod"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	return rec.Code, y.Kod
}

// degistir: Giris → (HTTP kodu, adına-token).
func degistir(t *testing.T, h http.Handler, kod string) (int, string) {
	t.Helper()
	rec := adIstek(t, h, http.MethodPost, "/musteri/adina-giris", "", map[string]string{"kod": kod})
	var y struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	return rec.Code, y.Token
}

func adinaTokenAl(t *testing.T, h http.Handler, aktorTok string, domainID int64) string {
	t.Helper()
	kod, k := kodAl(t, h, aktorTok, domainID)
	if kod != http.StatusOK || k == "" {
		t.Fatalf("kod alınamadı: %d", kod)
	}
	g, tok := degistir(t, h, k)
	if g != http.StatusOK || tok == "" {
		t.Fatalf("değişim başarısız: %d", g)
	}
	return tok
}

func TestAdina_BayiKendiDomaini_TamAkis_VeDenetim(t *testing.T) {
	db := adGeciciDB(t)
	h := adRouter(db)
	tok := adinaTokenAl(t, h, bayiTok(t, bayiA), domA)
	mc, err := auth.ParseMusteri(adSecret, tok)
	if err != nil || mc.DomainID != domA || mc.AktorUID != bayiA || mc.AktorRol != "reseller" || mc.AktorRID != bayiA {
		t.Fatalf("token claim'leri hatalı: %+v err=%v", mc, err)
	}
	if omur := mc.ExpiresAt.Sub(mc.IssuedAt.Time); omur > time.Duration(auth.AdinaOmurSn)*time.Second {
		t.Fatalf("adına-token ömrü %v > %ds", omur, auth.AdinaOmurSn)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("kendi domaini 200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/43", tok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("adına-token başka domain 403 beklenir: %d", rec.Code)
	}
	adIstek(t, h, http.MethodPost, "/musteri/adina-cikis", tok, nil)
	var n int
	// 0114: aktör adı ve "kimin adına" AYRI kolonlarda (eski tek-dizge biçim yok).
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_user_id=? AND actor_username='bayi_7'
		AND adina_domain_id=42
		AND action IN ('musteri.adina_oturum','musteri.adina_cikis') AND reseller_id=?`, bayiA, bayiA).Scan(&n)
	if n != 2 {
		t.Fatalf("denetim kaydı aktörle yazılmalı (oturum+çıkış), bulunan=%d", n)
	}
}

func TestAdina_IDOR_BayiBaskaBayininDomaini_403(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	if kod, k := kodAl(t, h, bayiTok(t, bayiA), domB); kod != http.StatusForbidden || k != "" {
		t.Fatalf("bayi A → bayi B domaini 403 ve kodsuz beklenir: %d kod=%q", kod, k)
	}
}

func TestAdina_MusteriBuUcuCagiramaz(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	mtok, _, err := auth.GenerateMusteri(adSecret, auth.MusteriClaims{FTPHesapID: 5, DomainID: domA, Kullanici: "amus", AlanAdi: "a-musteri.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if kod, _ := kodAl(t, h, mtok, domA); kod != http.StatusForbidden {
		t.Fatalf("müşteri kendi domaini için bile kod alamaz (403), alınan=%d", kod)
	}
	// Adına-token ile de yeni kod alınamaz (zincirleme kimlik yok).
	atok := adinaTokenAl(t, h, bayiTok(t, bayiA), domA)
	if kod, _ := kodAl(t, h, atok, domA); kod != http.StatusForbidden {
		t.Fatalf("adına-token ile kod üretilemez (403), alınan=%d", kod)
	}
}

func TestAdina_KodTekKullanimlik_VeSureli(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	_, k := kodAl(t, h, bayiTok(t, bayiA), domA)
	if g, _ := degistir(t, h, k); g != http.StatusOK {
		t.Fatalf("ilk değişim 200: %d", g)
	}
	if g, _ := degistir(t, h, k); g != http.StatusUnauthorized {
		t.Fatalf("ikinci değişim 401 beklenir: %d", g)
	}
	_, k2 := kodAl(t, h, bayiTok(t, bayiA), domA)
	eski := simdiFn
	simdiFn = func() time.Time { return time.Now().Add(KodOmru + time.Second) }
	defer func() { simdiFn = eski }()
	if g, _ := degistir(t, h, k2); g != http.StatusUnauthorized {
		t.Fatalf("süresi dolan kod 401 beklenir: %d", g)
	}
	if g, _ := degistir(t, h, strings.Repeat("A", 43)); g != http.StatusUnauthorized {
		t.Fatalf("uydurma kod 401 beklenir: %d", g)
	}
}

func TestAdina_DegisimYalnizMusteriKapisinda(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	_, k := kodAl(t, h, bayiTok(t, bayiA), domA)
	eski := kapiCoz
	kapiCoz = func(*http.Request) string { return kapi.Bayi }
	g, _ := degistir(t, h, k)
	kapiCoz = eski
	if g != http.StatusNotFound {
		t.Fatalf("bayi kapısında değişim 404 beklenir: %d", g)
	}
}

func TestAdina_KimlikIslemi403_OlaganMusteriEtkilenmez(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	atok := adinaTokenAl(t, h, bayiTok(t, bayiA), domA)
	rec := adIstek(t, h, http.MethodPut, "/domains/42/ftp/password", atok, nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "kimlik") {
		t.Fatalf("adına-oturumda parola değişimi 403 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	mtok, _, _ := auth.GenerateMusteri(adSecret, auth.MusteriClaims{FTPHesapID: 5, DomainID: domA, Kullanici: "amus", AlanAdi: "a-musteri.test"}, 3600)
	if rec := adIstek(t, h, http.MethodPut, "/domains/42/ftp/password", mtok, nil); rec.Code != http.StatusOK {
		t.Fatalf("olağan müşteri parola ucuna geçmeli: %d", rec.Code)
	}
}

func TestAdina_AskidakiHesabaGirilir_KokAskisinaBayiGiremez(t *testing.T) {
	db := adGeciciDB(t)
	h := adRouter(db)
	// Bayinin kendi aldığı askı: bayi girer; olağan müşteri token'ı askı 403'ü alır.
	if _, err := db.Exec(`UPDATE domains SET askida=1, askida_kaynak='bayi_el' WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	atok := adinaTokenAl(t, h, bayiTok(t, bayiA), domA)
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", atok, nil); rec.Code != http.StatusOK {
		t.Fatalf("askıdaki hesaba adına-oturum 200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	mtok, _, _ := auth.GenerateMusteri(adSecret, auth.MusteriClaims{FTPHesapID: 5, DomainID: domA, Kullanici: "amus", AlanAdi: "a-musteri.test"}, 3600)
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", mtok, nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "hesap_askida") {
		t.Fatalf("olağan müşteri askıda 403 hesap_askida almalı: %d %s", rec.Code, rec.Body.String())
	}
	// Kök askısı: bayi kod alamaz, AÇIK oturumu da düşer; yönetici girer.
	if _, err := db.Exec(`UPDATE domains SET askida_kaynak='manuel' WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	if kod, _ := kodAl(t, h, bayiTok(t, bayiA), domA); kod != http.StatusForbidden {
		t.Fatalf("kök askısında bayi kodu 403 beklenir: %d", kod)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", atok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("kök askıya alınca açık bayi oturumu 403 beklenir: %d", rec.Code)
	}
	ytok := adinaTokenAl(t, h, adminTok(t), domA)
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", ytok, nil); rec.Code != http.StatusOK {
		t.Fatalf("yönetici kök askısındaki hesaba girer: %d", rec.Code)
	}
}

func TestAdina_DevirVeAktorAskisi_AcikOturumuDusurur(t *testing.T) {
	db := adGeciciDB(t)
	h := adRouter(db)
	atok := adinaTokenAl(t, h, bayiTok(t, bayiA), domA)
	if _, err := db.Exec(`UPDATE domains SET reseller_id=8 WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", atok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("devirden sonra eski bayinin oturumu 403 beklenir: %d", rec.Code)
	}
	if _, err := db.Exec(`UPDATE domains SET reseller_id=7 WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", atok, nil); rec.Code != http.StatusOK {
		t.Fatalf("geri devirde oturum yeniden geçerli: %d", rec.Code)
	}
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", atok, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("askıya alınan bayinin adına-oturumu 401 beklenir: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdina_UzunOmurluAdinaToken_Reddedilir(t *testing.T) {
	h := adRouter(adGeciciDB(t))
	tok, _, err := auth.GenerateMusteri(adSecret, auth.MusteriClaims{FTPHesapID: 5, DomainID: domA, Kullanici: "amus",
		AlanAdi: "a-musteri.test", AktorUID: bayiA, AktorAd: "bayi_7", AktorRol: "reseller", AktorRID: bayiA}, 24*3600)
	if err != nil {
		t.Fatal(err)
	}
	if rec := adIstek(t, h, http.MethodGet, "/domains/42", tok, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("24 saatlik adına-token 401 beklenir: %d", rec.Code)
	}
}
