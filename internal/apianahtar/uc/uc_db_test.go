package uc

// uc_db_test.go — API anahtarı uçtan uca: gerçek chi yönlendiricisi (main.go ile
// aynı kurulum: Route("/api/v1") + Group(RequireAuth) + rol ara yazılımları) +
// gerçek göç dosyası (migrations/0128) + geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa
// atlanır; yalnız kendi açtığı gosp_test_apianahtar_<zaman> veritabanını kullanır).
//
// Kanıtlanan: hash'li saklama (gizli DB'de yok), bir kez gösterim, yanlış parola 403,
// kökte IP zorunlu, bayi kapsamı/IDOR (liste, iptal, döndürme, domain), anahtar
// anahtar üretemez, denetim aktörü "API anahtarı <ad>" ve bayi görünürlüğü, kök
// iptali, döndürme, bayi parola sıfırlama/silme → anahtarlar iptal, anahtar bayi
// parolası değiştiremez, anahtar trafiği oturum boşta damgasına dokunmaz.

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
	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/denetim"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/reseller"
)

var ucSecret = []byte("api-anahtar-test-jwt-sirri-0123456789-abcdef")

func ucGeciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_apianahtar_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı: %v", err)
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
	hashA, _ := bcrypt.GenerateFromPassword([]byte("dogru-parola-a"), bcrypt.MinCost)
	hashB, _ := bcrypt.GenerateFromPassword([]byte("dogru-parola-b"), bcrypt.MinCost)
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL,
			password_hash VARCHAR(255) NOT NULL DEFAULT '', full_name VARCHAR(255) NOT NULL DEFAULT '',
			role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user',
			status ENUM('active','suspended') NOT NULL DEFAULT 'active',
			totp_secret VARCHAR(64) NOT NULL DEFAULT '', totp_enabled TINYINT(1) NOT NULL DEFAULT 0,
			totp_last_step BIGINT NOT NULL DEFAULT 0, token_gecersiz_ts BIGINT NOT NULL DEFAULT 0,
			last_activity_ts BIGINT NOT NULL DEFAULT 0, reseller_plan_id BIGINT NOT NULL DEFAULT 0,
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0,
			max_trafik_mb BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			reseller_id BIGINT UNSIGNED NULL, plan_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, reseller_id BIGINT NULL)`,
		`CREATE TABLE dns_template (id BIGINT UNSIGNED PRIMARY KEY, reseller_id BIGINT NULL)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail JSON NULL CHECK (json_valid(detail)),
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0, adina_domain_id BIGINT UNSIGNED NULL)`,
		`INSERT INTO users (id, username, role, password_hash) VALUES (1,'admin','admin','')`,
		`INSERT INTO users (id, username, role, password_hash) VALUES (7,'bayi_a','reseller','` + string(hashA) + `'),
			(8,'bayi_b','reseller','` + string(hashB) + `'), (9,'bayi_c','reseller','` + string(hashB) + `'),
			(10,'bayi_d','reseller','` + string(hashB) + `')`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (42,'a.test',7), (43,'b.test',8), (44,'kok.test',NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	// GERÇEK göç dosyası — koşucuyla aynı ayrıştırma (satır başı -- yorumları at, ; ile böl).
	b, err := os.ReadFile("../../../migrations/0128_api_anahtarlari.sql")
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
	for kez := 0; kez < 2; kez++ { // iki kez: idempotent
		for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if st = strings.TrimSpace(st); st == "" {
				continue
			}
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0128 (tur %d): %v", kez+1, err)
			}
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	eskiYD := yenidenDogrula
	yenidenDogrula = func(db *sql.DB, uid int64, rol, parola, kod string) auth.YDSonuc {
		if rol == "admin" { // kök /etc/shadow kullanır — testte sabit parola
			if uid == 1 && parola == "kok-parola" {
				return auth.YDTamam
			}
			return auth.YDParolaHatali
		}
		return auth.YenidenDogrula(db, uid, rol, parola, kod) // bayi: GERÇEK bcrypt yolu
	}
	t.Cleanup(func() { yenidenDogrula = eskiYD })
	return db
}

type ucSonuc struct {
	kod   int
	govde string
}

func ucRouter(db *sql.DB) http.Handler {
	h := &Handlers{DB: db}
	rh := &reseller.Handlers{DB: db}
	dh := &denetim.Handlers{DB: db}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(ucSecret))
			yonetim := r.With(middleware.AdminVeyaReseller, middleware.YalnizOturum)
			yonetim.Get("/api-anahtarlari", h.Liste)
			yonetim.Get("/api-anahtarlari/izin-gruplari", h.Katalog)
			yonetim.Post("/api-anahtarlari", h.Olustur)
			yonetim.Post("/api-anahtarlari/{id}/yenile", h.Yenile)
			yonetim.Post("/api-anahtarlari/{id}/iptal", h.Iptal)
			yonetim.Delete("/api-anahtarlari/{id}", h.Sil)
			r.With(middleware.AdminVeyaReseller).Get("/api-anahtari/ben", h.Ben)
			r.With(middleware.AdminVeyaReseller).Get("/domains", func(w http.ResponseWriter, r *http.Request) {
				httpx.WriteJSON(w, 200, map[string]any{"rid": middleware.ResellerIDFrom(r)})
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}", func(w http.ResponseWriter, r *http.Request) {
				httpx.WriteJSON(w, 200, map[string]any{"ok": true})
			})
			r.With(middleware.SahipYonetim).Post("/domains/{id}/askiya-al", func(w http.ResponseWriter, r *http.Request) {
				uid, kul := middleware.Aktor(r)
				httpx.Denetim(db, r, uid, kul, "test.islem", chi.URLParam(r, "id"), "", middleware.ResellerIDFrom(r), true)
				httpx.WriteJSON(w, 200, map[string]any{"ok": true})
			})
			r.With(middleware.AdminVeyaReseller).Get("/denetim", dh.Liste)
			r.With(middleware.AdminOnly).Put("/resellers/{id}", rh.Guncelle)
			r.With(middleware.AdminOnly).Delete("/resellers/{id}", rh.Sil)
		})
	})
	return r
}

func ucIstek(t *testing.T, h http.Handler, metod, yol, tok string, govde any) ucSonuc {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(metod, yol, bytes.NewReader(b))
	req.RemoteAddr = "192.0.2.10:40000" // doğrudan (loopback olmayan) istemci
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return ucSonuc{w.Code, w.Body.String()}
}

func jwtBayi(t *testing.T, id int64, ad string) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(ucSecret, 3600, id, ad, id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func jwtKok(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueAt(ucSecret, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type olusturYanit struct {
	Anahtar string  `json:"anahtar"`
	Kayit   Gorunum `json:"anahtar_kaydi"`
}

func olustur(t *testing.T, h http.Handler, tok string, govde map[string]any) olusturYanit {
	t.Helper()
	s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", tok, govde)
	if s.kod != 201 {
		t.Fatalf("oluşturma: %d %s", s.kod, s.govde)
	}
	var y olusturYanit
	if err := json.Unmarshal([]byte(s.govde), &y); err != nil {
		t.Fatal(err)
	}
	if !apianahtar.AnahtarMi(y.Anahtar) || y.Kayit.ID == 0 {
		t.Fatalf("yanıtta tam anahtar yok: %s", s.govde)
	}
	return y
}

func TestAPIAnahtarUctanUca(t *testing.T) {
	db := ucGeciciDB(t)
	h := ucRouter(db)
	jA, jB, jC, jK := jwtBayi(t, 7, "bayi_a"), jwtBayi(t, 8, "bayi_b"), jwtBayi(t, 9, "bayi_c"), jwtKok(t)

	// ── Oluşturma doğrulamaları ──
	if s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", jA, map[string]any{"ad": "x", "izinler": []string{"hesap:oku"}, "parola": "yanlis"}); s.kod != 403 || !strings.Contains(s.govde, "parola_hatali") {
		t.Fatalf("yanlış parola 403 olmalı (401 ön yüzü çıkışa atar): %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", jA, map[string]any{"ad": "x", "izinler": []string{"bayi:oku"}, "parola": "dogru-parola-a"}); s.kod != 400 {
		t.Fatalf("bayi bayi grubunu seçememeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", jK, map[string]any{"ad": "k", "izinler": []string{"hesap:oku"}, "parola": "kok-parola"}); s.kod != 400 {
		t.Fatalf("kökte IP listesi zorunlu: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", jK, map[string]any{"ad": "k", "izinler": []string{"hesap:oku"}, "izinli_ipler": []string{"0.0.0.0/0"}, "parola": "kok-parola"}); s.kod != 400 {
		t.Fatalf("kökte 0.0.0.0/0 reddedilmeli: %d %s", s.kod, s.govde)
	}

	a := olustur(t, h, jA, map[string]any{"ad": "WHMCS-A", "izinler": []string{"hesap:yaz", "denetim:oku"}, "parola": "dogru-parola-a"})
	b := olustur(t, h, jB, map[string]any{"ad": "WHMCS-B", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-b"})
	c := olustur(t, h, jC, map[string]any{"ad": "WHMCS-C", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-b"})
	k := olustur(t, h, jK, map[string]any{"ad": "Kok", "izinler": []string{"hesap:oku", "bayi:yaz"}, "izinli_ipler": []string{"192.0.2.0/24"}, "parola": "kok-parola"})

	// ── Hash'li saklama: gizli DB'de hiçbir sütunda yok; yalnız SHA-256 ──
	var ozet, izinler string
	if err := db.QueryRow(`SELECT ozet, izinler FROM api_anahtarlari WHERE id=?`, a.Kayit.ID).Scan(&ozet, &izinler); err != nil {
		t.Fatal(err)
	}
	if ozet != apianahtar.Ozet(a.Anahtar) || izinler != "denetim:oku,hesap:yaz" {
		t.Fatalf("saklanan özet/izin yanlış: %s %s", ozet, izinler)
	}
	gizli := a.Anahtar[len(apianahtar.On)+apianahtar.OnekUzunluk+1:]
	var sizinti int
	_ = db.QueryRow(`SELECT COUNT(*) FROM api_anahtarlari WHERE CONCAT_WS('|',ad,onek,ozet,son4,izinler,izinli_ipler) LIKE ?`, "%"+gizli+"%").Scan(&sizinti)
	_ = db.QueryRow(`SELECT ?+COUNT(*) FROM audit_log WHERE CONCAT_WS('|',actor_username,target,detail) LIKE ?`, sizinti, "%"+gizli+"%").Scan(&sizinti)
	if sizinti != 0 {
		t.Fatal("gizli değer DB'de (anahtar tablosu ya da denetim) bulundu")
	}

	// ── Liste: bayi yalnız kendisininkini görür; sahip parametresi yok sayılır; ozet/anahtar alanı YOK ──
	s := ucIstek(t, h, "GET", "/api/v1/api-anahtarlari?sahip=bayiler", jA, nil)
	var liste struct{ Anahtarlar []map[string]any }
	_ = json.Unmarshal([]byte(s.govde), &liste)
	if s.kod != 200 || len(liste.Anahtarlar) != 1 || liste.Anahtarlar[0]["sahip_id"].(float64) != 7 {
		t.Fatalf("bayi listesi kapsam dışı: %d %s", s.kod, s.govde)
	}
	for alan := range liste.Anahtarlar[0] {
		if alan == "ozet" || alan == "anahtar" {
			t.Fatalf("liste yanıtında %q alanı var", alan)
		}
	}
	if strings.Contains(s.govde, gizli) {
		t.Fatal("liste yanıtında gizli var")
	}

	// ── Anahtar kullanımı + kapsam/IDOR ──
	if s := ucIstek(t, h, "GET", "/api/v1/domains", a.Anahtar, nil); s.kod != 200 || !strings.Contains(s.govde, `"rid":7`) {
		t.Fatalf("bayi anahtarı rid=7 üretmeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/42", a.Anahtar, nil); s.kod != 200 {
		t.Fatalf("kendi domaini: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/43", a.Anahtar, nil); s.kod != 403 {
		t.Fatalf("IDOR: bayi A anahtarı bayi B domaini: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/44", a.Anahtar, nil); s.kod != 403 {
		t.Fatalf("IDOR: bayi A anahtarı kök domaini: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/44", k.Anahtar, nil); s.kod != 200 {
		t.Fatalf("kök anahtarı kök domaini: %d %s", s.kod, s.govde)
	}
	// Anahtar anahtar üretemez / listeleyemez.
	if s := ucIstek(t, h, "POST", "/api/v1/api-anahtarlari", a.Anahtar, map[string]any{"ad": "x", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-a"}); s.kod != 403 {
		t.Fatalf("anahtar anahtar üretmemeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/api-anahtarlari", a.Anahtar, nil); s.kod != 403 {
		t.Fatalf("anahtar anahtar listelememeli: %d", s.kod)
	}
	// Ben: yalnız anahtarla.
	if s := ucIstek(t, h, "GET", "/api/v1/api-anahtari/ben", a.Anahtar, nil); s.kod != 200 || !strings.Contains(s.govde, "WHMCS-A") {
		t.Fatalf("ben (anahtar): %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/api-anahtari/ben", jA, nil); s.kod != 404 {
		t.Fatalf("ben (JWT) 404 olmalı: %d", s.kod)
	}

	// ── Denetim: aktör "API anahtarı <ad>", bayi görünürlüğü ──
	if s := ucIstek(t, h, "POST", "/api/v1/domains/42/askiya-al", a.Anahtar, nil); s.kod != 200 {
		t.Fatalf("anahtarla işlem: %d %s", s.kod, s.govde)
	}
	var aktorID int64
	var aktorAd string
	var kapsam int64
	if err := db.QueryRow(`SELECT actor_user_id, actor_username, reseller_id FROM audit_log WHERE action='test.islem'`).Scan(&aktorID, &aktorAd, &kapsam); err != nil {
		t.Fatal(err)
	}
	if aktorID != 7 || aktorAd != "bayi_a (API anahtarı WHMCS-A)" || kapsam != 7 {
		t.Fatalf("denetim aktörü yanlış: %d %q %d", aktorID, aktorAd, kapsam)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/denetim", jB, nil); strings.Contains(s.govde, "test.islem") || strings.Contains(s.govde, "WHMCS-A") {
		t.Fatalf("bayi B, bayi A'nın anahtar işlemlerini görmemeli: %s", s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/denetim", a.Anahtar, nil); s.kod != 200 || !strings.Contains(s.govde, "test.islem") {
		t.Fatalf("bayi A (denetim:oku anahtarı) kendi kaydını görmeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/denetim", b.Anahtar, nil); s.kod != 403 {
		t.Fatalf("denetim izni olmayan anahtar: %d", s.kod)
	}
	var olusturKaydi int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.olustur' AND reseller_id=7 AND target LIKE ?`, "gosp_"+a.Kayit.Onek+"%").Scan(&olusturKaydi)
	if olusturKaydi != 1 {
		t.Fatalf("oluşturma denetim kaydı (önekle, bayi kapsamında) yok: %d", olusturKaydi)
	}

	// ── İptal: bayi yabancı anahtarı iptal edemez (404); kök bayi anahtarını edebilir ──
	// Depo katmanı ikinci kilit: sahip koşullu iptal yabancı satıra DOKUNMAZ.
	if degisti, err := apianahtar.Iptal(t.Context(), db, b.Kayit.ID, 7, 7, "deneme", time.Now().Unix()); err != nil || degisti {
		t.Fatalf("depo: sahip koşullu iptal yabancı anahtarı etkiledi: %v %v", degisti, err)
	}
	if s := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/iptal", b.Kayit.ID), jA, nil); s.kod != 404 {
		t.Fatalf("IDOR: bayi A, B'nin anahtarını iptal: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/43", b.Anahtar, nil); s.kod != 200 {
		t.Fatalf("B anahtarı hâlâ çalışmalı: %d", s.kod)
	}
	if s := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/yenile", b.Kayit.ID), jA, map[string]any{"parola": "dogru-parola-a"}); s.kod != 404 {
		t.Fatalf("IDOR: bayi A, B'nin anahtarını döndürme: %d", s.kod)
	}
	s = ucIstek(t, h, "GET", "/api/v1/api-anahtarlari?sahip=bayiler", jK, nil)
	if !strings.Contains(s.govde, "WHMCS-A") || !strings.Contains(s.govde, "WHMCS-B") || !strings.Contains(s.govde, `"sahip_ad":"bayi_b"`) || strings.Contains(s.govde, `"Kok"`) {
		t.Fatalf("kök bayi anahtarları listesi: %s", s.govde)
	}
	if s := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/yenile", b.Kayit.ID), jK, map[string]any{"parola": "kok-parola"}); s.kod != 404 {
		t.Fatalf("kök bayi anahtarını DÖNDÜREMEZ (gizli sahibinde doğar): %d", s.kod)
	}
	if s := ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/iptal", b.Kayit.ID), jK, nil); s.kod != 200 {
		t.Fatalf("kök bayi anahtarını iptal: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/43", b.Anahtar, nil); s.kod != 401 {
		t.Fatalf("iptal edilen anahtar ilk istekte 401: %d", s.kod)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/api-anahtarlari", jB, nil); !strings.Contains(s.govde, `"iptal_yonetim":true`) || !strings.Contains(s.govde, `"durum":"iptal"`) {
		t.Fatalf("bayi B listesinde yönetim iptali görünmeli: %s", s.govde)
	}

	// ── Döndürme: eski anında düşer, yeni çalışır ──
	s = ucIstek(t, h, "POST", fmt.Sprintf("/api/v1/api-anahtarlari/%d/yenile", a.Kayit.ID), jA, map[string]any{"parola": "dogru-parola-a", "gecis_saat": 0})
	if s.kod != 201 {
		t.Fatalf("döndürme: %d %s", s.kod, s.govde)
	}
	var a2 olusturYanit
	_ = json.Unmarshal([]byte(s.govde), &a2)
	if ucIstek(t, h, "GET", "/api/v1/domains/42", a.Anahtar, nil).kod != 401 || ucIstek(t, h, "GET", "/api/v1/domains/42", a2.Anahtar, nil).kod != 200 {
		t.Fatal("döndürme sonrası eski 401, yeni 200 olmalı")
	}
	if strings.Join(a2.Kayit.Izinler, ",") != "denetim:oku,hesap:yaz" {
		t.Fatalf("döndürülen anahtar izinleri korumalı: %v", a2.Kayit.Izinler)
	}

	// ── Anahtar bayi parolasını değiştiremez; diğer alanlar serbest ──
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/8", k.Anahtar, map[string]any{"parola": "ele-gecirme-123"}); s.kod != 403 {
		t.Fatalf("anahtarla bayi parolası değişmemeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/8", k.Anahtar, map[string]any{"ad_soyad": "Bayi B"}); s.kod != 200 {
		t.Fatalf("anahtarla bayi adı: %d %s", s.kod, s.govde)
	}

	// ── Kök bayi parolasını sıfırlayınca bayinin anahtarları iptal ──
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/7", jK, map[string]any{"parola": "yeni-parola-123"}); s.kod != 200 {
		t.Fatalf("parola sıfırlama: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/42", a2.Anahtar, nil); s.kod != 401 {
		t.Fatalf("parolası sıfırlanan bayinin anahtarı düşmeli: %d", s.kod)
	}
	var neden string
	_ = db.QueryRow(`SELECT iptal_neden FROM api_anahtarlari WHERE id=?`, a2.Kayit.ID).Scan(&neden)
	if neden != "parola_sifirlama" {
		t.Fatalf("iptal nedeni: %q", neden)
	}
	// ── Bayi silinince anahtarları iptal ──
	if s := ucIstek(t, h, "DELETE", "/api/v1/resellers/9", jK, nil); s.kod != 200 {
		t.Fatalf("bayi silme: %d %s", s.kod, s.govde)
	}
	_ = db.QueryRow(`SELECT iptal_neden FROM api_anahtarlari WHERE id=?`, c.Kayit.ID).Scan(&neden)
	if neden != "bayi_silindi" || ucIstek(t, h, "GET", "/api/v1/domains", c.Anahtar, nil).kod != 401 {
		t.Fatalf("silinen bayinin anahtarı: neden=%q", neden)
	}

	// ── Anahtar trafiği oturum boşta damgasına dokunmaz (hiç JWT kullanmamış bayi_d) ──
	u, _ := apianahtar.Uret()
	if _, err := apianahtar.Ekle(t.Context(), db, &apianahtar.Kayit{SahipID: 10, SahipRol: "reseller", Ad: "d", Onek: u.Onek,
		Ozet: u.Ozet, Son4: u.Son4, Izinler: []string{"hesap:oku"}, HizDk: 120, SonKullanma: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if s := ucIstek(t, h, "GET", "/api/v1/domains", u.Tam, nil); s.kod != 200 {
			t.Fatalf("bayi_d anahtarı: %d %s", s.kod, s.govde)
		}
	}
	time.Sleep(300 * time.Millisecond)
	var sonAkt int64
	var sonKullanim sql.NullString
	_ = db.QueryRow(`SELECT last_activity_ts FROM users WHERE id=10`).Scan(&sonAkt)
	_ = db.QueryRow(`SELECT son_kullanim_ip FROM api_anahtarlari WHERE onek=?`, u.Onek).Scan(&sonKullanim)
	if sonAkt != 0 {
		t.Fatalf("anahtar trafiği last_activity_ts'e yazdı: %d", sonAkt)
	}
	if sonKullanim.String != "192.0.2.10" {
		t.Fatalf("son kullanım IP'si yazılmadı: %q", sonKullanim.String)
	}
}
