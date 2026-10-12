package pma

// pma_test.go — POST /databases/{dbId}/pma-token ÖN-DENETİMİ.
//
// 🔴 OLAY (2026-10-12, bir müşteri sunucusu / bayi): panelin db_accounts
// kaydındaki parola, hesabın MariaDB parolası panel dışından değiştiği için bayattı.
// Panel yine de token üretiyordu; phpMyAdmin "Access denied" (1045) alıp SignonURL'e
// TOKENSIZ dönüyor, kullanıcı yalnızca "Token eksik" görüyordu. Artık:
//   - kayıtlı kimlik MariaDB'de KESİN reddedilirse token VERİLMEZ, 409 + kod döner;
//   - ölçülemeyen durumda (soket yok, zaman aşımı) token yine verilir (fail-open);
//   - kullanıcısı olmayan DB ve çözülemeyen parola açık hatayla durur;
//   - sahibi olmayana ölçüm HİÇ yapılmaz (kimliğin geçerliliği sızdırılmaz).
//
// Zincir main.go ile AYNI: RequireAuth → DBSahipligiMusteri("dbId") → TokenIste.
// Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR); MariaDB girişi SAHTE
// (dbGirisDene dikişi) — canlı sunucunun hesaplarına gidilmez.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

	"girginospanel/internal/auth"
	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

var pt_sir = []byte("pma-token-test-anahtari-0123456789abcdef")

// pt_sahteGiris: dbGirisDene sahtesi — kimlerle denendiğini kaydeder, verilen sonucu döner.
type pt_sahteGiris struct {
	mu    sync.Mutex
	sonuc error
	cagri []string // "kullanici:parola"
}

func (s *pt_sahteGiris) dene(_ context.Context, kul, parola string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cagri = append(s.cagri, kul+":"+parola)
	return s.sonuc
}

func (s *pt_sahteGiris) sayi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cagri)
}

func pt_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

// pt_kur: geçici DB + şema + sahte giriş.
//
//	users   : 1 kök, 2 ve 3 bayi
//	domains : 20 (bayi 2), 30 (bayi 3), 40 (bayi 2, demo)
//	db_accounts: 200 bayi2'nin DB'si (u20_db, kayıtlı parola "Kayitli-Parola-1", şifreli)
//	             201 bayi2'nin kullanıcısız DB'si, 202 bayi2'nin çözülemeyen parolalı DB'si
//	             300 bayi3'ün DB'si, 400 demo DB
func pt_kur(t *testing.T, sonuc error) (*Handlers, *pt_sahteGiris) {
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
	ad := fmt.Sprintf("gosp_test_pma_%d", time.Now().UnixNano())
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
			role VARCHAR(16) NOT NULL DEFAULT 'user', status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NULL, last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0,
			is_demo TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL, db_pass_plain VARCHAR(255) NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost')`,
		// 0010_pma ile aynı sütunlar
		`CREATE TABLE pma_tokens (token VARCHAR(64) NOT NULL PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			db_kullanici VARCHAR(80) NOT NULL, db_parola VARCHAR(255) NOT NULL, db_adi VARCHAR(80) NOT NULL,
			olusturulma TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP, son_kullanma TIMESTAMP NOT NULL,
			kullanildi TINYINT(1) NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (2,'bayi2','reseller'), (3,'bayi3','reseller')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, is_demo) VALUES
			(20,'bayi2.test','c_bayi2',2,0), (30,'bayi3.test','c_bayi3',3,0), (40,'demo.test','c_demo',2,1)`,
	} {
		pt_exec(t, db, q)
	}
	pt_exec(t, db, `INSERT INTO db_accounts (id, domain_id, db_name, db_user, db_pass_plain) VALUES
		(200,20,'c_bayi2_main','u20_db',?), (201,20,'c_bayi2_bos','',''),
		(202,20,'c_bayi2_kilitli','u20_kilitli','gos1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'),
		(300,30,'c_bayi3_main','u30_db',?), (400,40,'c_demo_main','u40_db',?)`,
		gizli.SaklaBagli("Kayitli-Parola-1", "u20_db"), gizli.SaklaBagli("Kayitli-Parola-3", "u30_db"),
		gizli.SaklaBagli("Kayitli-Parola-4", "u40_db"))
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	sahte := &pt_sahteGiris{sonuc: sonuc}
	eski := dbGirisDene
	dbGirisDene = sahte.dene
	t.Cleanup(func() { dbGirisDene = eski })
	return &Handlers{DB: db}, sahte
}

func pt_istek(t *testing.T, h *Handlers, rid int64, dbID int64) (int, map[string]any) {
	t.Helper()
	tok, err := auth.IssueResellerAt(pt_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(pt_sir))
		r.With(middleware.DBSahipligiMusteri("dbId")).Post("/databases/{dbId}/pma-token", h.TokenIste)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/databases/%d/pma-token", dbID), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var govde map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &govde)
	return rec.Code, govde
}

func pt_tokenSayisi(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pma_tokens`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 🔴 ÇEKİRDEK (olayın kendisi): kayıtlı parola MariaDB'de reddedilir → token YOK,
// 409 + makine okunur kod + çözüm yolunu söyleyen mesaj.
func TestTokenIste_ParolaUyusmuyor409TokenYok(t *testing.T) {
	h, sahte := pt_kur(t, hesaplar.ErrDBParolaUyusmuyor)
	kod, govde := pt_istek(t, h, 2, 200)
	if kod != http.StatusConflict {
		t.Fatalf("kod=%d govde=%v; 409 bekleniyordu", kod, govde)
	}
	if govde["kod"] != KodDBParolaUyusmuyor {
		t.Errorf("kod alanı %v; %q bekleniyordu", govde["kod"], KodDBParolaUyusmuyor)
	}
	if m, _ := govde["hata"].(string); !strings.Contains(m, "Yalnız panel kaydını güncelle") {
		t.Errorf("yönetebilen (bayi) için çözüm yolu mesajda yok: %q", m)
	}
	if n := pt_tokenSayisi(t, h.DB); n != 0 {
		t.Errorf("uyuşmayan kimlikle %d token üretildi (phpMyAdmin yine 'Token eksik' döngüsüne girerdi)", n)
	}
	// Ölçüm, phpMyAdmin'e gidecek ÇÖZÜLMÜŞ değerle yapılmalı (ciphertext ile değil).
	if sahte.sayi() != 1 || sahte.cagri[0] != "u20_db:Kayitli-Parola-1" {
		t.Errorf("giriş denemesi beklenen kimlikle yapılmadı: %v", sahte.cagri)
	}
}

// Pozitif kontrol: kimlik geçerliyse eski davranış (201 + token) aynen sürer.
func TestTokenIste_ParolaGecerli201(t *testing.T) {
	h, sahte := pt_kur(t, nil)
	kod, govde := pt_istek(t, h, 2, 200)
	if kod != http.StatusCreated {
		t.Fatalf("kod=%d govde=%v; 201 bekleniyordu", kod, govde)
	}
	if u, _ := govde["signon_url"].(string); !strings.HasPrefix(u, "/pma-signon.php?t=") {
		t.Errorf("signon_url beklenmedik: %v", govde["signon_url"])
	}
	if n := pt_tokenSayisi(t, h.DB); n != 1 {
		t.Errorf("token satırı %d; 1 bekleniyordu", n)
	}
	if sahte.sayi() != 1 {
		t.Errorf("ön-denetim %d kez çağrıldı; 1 bekleniyordu", sahte.sayi())
	}
}

// Ölçülemeyen durum (soket yok, zaman aşımı…) phpMyAdmin'i KİLİTLEMEZ: token verilir.
func TestTokenIste_OlcumHatasiFailOpen(t *testing.T) {
	h, _ := pt_kur(t, errors.New("MariaDB soketi bulunamadı"))
	kod, govde := pt_istek(t, h, 2, 200)
	if kod != http.StatusCreated {
		t.Fatalf("ölçüm hatasında kod=%d govde=%v; 201 (fail-open) bekleniyordu", kod, govde)
	}
}

// Kullanıcısız DB ve çözülemeyen parola: açık hata, ölçüm yapılmaz, token yok.
func TestTokenIste_KullaniciYokVeCozulemeyenParola(t *testing.T) {
	h, sahte := pt_kur(t, nil)
	for _, c := range []struct {
		db     int64
		durum  int
		kodAdi string
	}{
		{201, http.StatusConflict, KodDBKullaniciYok},
		{202, http.StatusInternalServerError, KodDBKayitCozulemedi},
	} {
		kod, govde := pt_istek(t, h, 2, c.db)
		if kod != c.durum || govde["kod"] != c.kodAdi {
			t.Errorf("db %d: kod=%d govde=%v; %d/%s bekleniyordu", c.db, kod, govde, c.durum, c.kodAdi)
		}
	}
	if sahte.sayi() != 0 {
		t.Errorf("kullanıcısız/çözülemeyen kayıt için MariaDB girişi denendi: %v", sahte.cagri)
	}
	if n := pt_tokenSayisi(t, h.DB); n != 0 {
		t.Errorf("%d token üretildi; 0 bekleniyordu", n)
	}
}

// 🔴 Sahibi olmayan (başka bayi) ve demo: ölçüm HİÇ yapılmaz — uç, başkasının
// DB kimliğinin geçerliliğini yoklamak için kullanılamaz.
func TestTokenIste_SahibiOlmayanVeDemoOlcumYapmaz(t *testing.T) {
	h, sahte := pt_kur(t, hesaplar.ErrDBParolaUyusmuyor)
	if kod, _ := pt_istek(t, h, 3, 200); kod != http.StatusNotFound {
		t.Errorf("başka bayinin DB'si: kod=%d; 404 bekleniyordu", kod)
	}
	if kod, _ := pt_istek(t, h, 2, 400); kod != http.StatusForbidden {
		t.Errorf("demo DB: kod=%d; 403 bekleniyordu", kod)
	}
	if sahte.sayi() != 0 {
		t.Errorf("sahibi olmayan/demo istekte MariaDB girişi denendi: %v", sahte.cagri)
	}
}

// Çözüm ucu (yalnız-kayıt eşitleme) artık müşteriye de açık: mesaj herkese çözüm yolunu gösterir.
func TestParolaUyusmuyorMesaji_CozumYolu(t *testing.T) {
	if m := parolaUyusmuyorMesaji(); !strings.Contains(m, "Yalnız panel kaydını güncelle") || !strings.Contains(m, "site etkilenmez") {
		t.Errorf("mesajda çözüm yolu yok: %q", m)
	}
}
