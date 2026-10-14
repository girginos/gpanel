package backups

// denetim_php_test.go — yedek uçlarının denetim kaydı (audit_log).
//
//	POST   /domains/{id}/backups          Create       → yedek.baslat
//	DELETE /domains/{id}/backups/{bid}    Delete       → yedek.sil (+ .lftp-yok / .uzak-yoksayildi)
//	PUT    /domains/{id}/backup-schedule  SetSchedule  → yedek.zamanlama
//
// Her uçta: eylem kodu, hedef, kapsam (domains.reseller_id — domainin sahibi), aktör,
// başarı/başarısızlık ve detayda sır (yol, host, ham lftp çıktısı) OLMADIĞI doğrulanır.
// Gerçek MariaDB (GOSP_TEST_GECICI_DSN yoksa ATLANIR). Yedek arşivi (tar/mysqldump) ve
// uzak depo SAHTE: arka plan görevi dikişle değiştirilir, `lftp` PATH'teki sahtedir.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
)

func bdnDB(t *testing.T) *sql.DB {
	t.Helper()
	db := ymDB(t, true) // domains (backup_freq…) + backups; 0115 koşulmuş
	db.SetMaxOpenConns(8)
	g113_exec(t, db, `ALTER TABLE domains ADD COLUMN reseller_id BIGINT UNSIGNED NULL`)
	g113_exec(t, db, `CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		ts TIMESTAMP NULL DEFAULT current_timestamp(), actor_user_id BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL,
		ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
		target VARCHAR(255) NOT NULL DEFAULT '', detail LONGTEXT NULL,
		ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`)
	// 42: bayi 9'un domaini · 43: kök domaini
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, reseller_id) VALUES
		(42,'bayi9.test','c_bayi9',9), (43,'kok.test','c_kok43',NULL)`)
	return db
}

func bdnRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(ymSir))
	r.Post("/domains/{id}/backups", h.Create)
	r.Delete("/domains/{id}/backups/{bid}", h.Delete)
	r.Put("/domains/{id}/backup-schedule", h.SetSchedule)
	return r
}

type bdnKayit struct {
	Eylem, Hedef, Not, Aktor, Ham string
	Ok                            int
	Kapsam                        int64
}

func bdnKayitlar(t *testing.T, db *sql.DB, eylem string) []bdnKayit {
	t.Helper()
	rows, err := db.Query(`SELECT action, target, COALESCE(detail,''), actor_username, ok, reseller_id
		FROM audit_log WHERE action=? ORDER BY id`, eylem)
	if err != nil {
		t.Fatalf("audit_log okunamadı: %v", err)
	}
	defer rows.Close()
	var out []bdnKayit
	for rows.Next() {
		var k bdnKayit
		if err := rows.Scan(&k.Eylem, &k.Hedef, &k.Ham, &k.Aktor, &k.Ok, &k.Kapsam); err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		_ = json.Unmarshal([]byte(k.Ham), &m)
		k.Not = m["not"]
		out = append(out, k)
	}
	return out
}

func bdnToplamKayit(t *testing.T, db *sql.DB) int {
	t.Helper()
	return g113_say(t, db, `SELECT COUNT(*) FROM audit_log`)
}

// bdnTek: eylem için TAM BİR kayıt; alanlar beklenenle aynı; detayda sır yok.
func bdnTek(t *testing.T, db *sql.DB, eylem, hedef, not string, ok int, kapsam int64, aktor string) {
	t.Helper()
	k := bdnKayitlar(t, db, eylem)
	if len(k) != 1 {
		t.Fatalf("%s: tam 1 denetim kaydı beklenir, alınan %d: %+v", eylem, len(k), k)
	}
	g := k[0]
	if g.Hedef != hedef {
		t.Errorf("%s: hedef %q, beklenen %q", eylem, g.Hedef, hedef)
	}
	if g.Not != not {
		t.Errorf("%s: detay %q, beklenen %q", eylem, g.Not, not)
	}
	if g.Ok != ok {
		t.Errorf("%s: ok=%d, beklenen %d", eylem, g.Ok, ok)
	}
	if g.Kapsam != kapsam {
		t.Errorf("%s: kapsam (reseller_id) %d, beklenen %d (domainin sahibi)", eylem, g.Kapsam, kapsam)
	}
	if g.Aktor != aktor {
		t.Errorf("%s: aktör %q, beklenen %q", eylem, g.Aktor, aktor)
	}
	for _, s := range []string{"/var/", "/tmp", "lftp", "ornek.invalid", "Login", "c_bayi9", "c_kok43", "parola"} {
		if strings.Contains(g.Ham, s) {
			t.Errorf("%s: denetim detayında sır/iç ayrıntı (%q) var: %s", eylem, s, g.Ham)
		}
	}
}

// ── Create ───────────────────────────────────────────────────────────────────

// bdnGorevDikis: arka plan yedek görevini sahteyle değiştirir; gerçek görevin
// çıkış temizliğini (kuyruk slotu + hesap kilidi + ilerleme) aynen yapar.
func bdnGorevDikis(t *testing.T) *[]string {
	t.Helper()
	var cagri []string
	eski := yedekGoreviBaslat
	t.Cleanup(func() { yedekGoreviBaslat = eski })
	yedekGoreviBaslat = func(h *Handlers, id int64, alanAdi, sk string) {
		cagri = append(cagri, alanAdi+"|"+sk)
		defer yedekBitir(id)
		if err := manuelKuyrugu.Bekle(t.Context(), id); err == nil {
			manuelKuyrugu.Birak()
		}
		IlerlemeBitir(id, "", nil)
	}
	return &cagri
}

func TestYedekCreate_Denetim(t *testing.T) {
	for _, c := range []struct {
		ad, tok, yol string
		kapsam       int64
		aktor        string
		hedef        string
	}{
		{"yönetici → bayi domaini", "admin", "/domains/42/backups", 9, "kok", "bayi9.test"},
		{"bayi kendi domaini", "bayi", "/domains/42/backups", 9, "bayi_a", "bayi9.test"},
		{"yönetici → kök domaini", "admin", "/domains/43/backups", 0, "kok", "kok.test"},
	} {
		t.Run(c.ad, func(t *testing.T) {
			db := bdnDB(t)
			cagri := bdnGorevDikis(t)
			tok := ymAdminTok(t)
			if c.tok == "bayi" {
				tok = ymBayiTok(t)
			}
			rec := ymIstek(t, bdnRouter(&Handlers{DB: db}), http.MethodPost, c.yol, tok, "")
			if rec.Code != http.StatusAccepted {
				t.Fatalf("202 beklenir: %d %s", rec.Code, rec.Body.String())
			}
			if len(*cagri) != 1 {
				t.Errorf("arka plan görevi tam 1 kez başlamalı: %v", *cagri)
			}
			bdnTek(t, db, "yedek.baslat", c.hedef, "tip=manuel", 1, c.kapsam, c.aktor)
		})
	}
}

// Kabul edilmeyen istek (404 / demo / güvenlik / zaten sürüyor) iş başlatmaz → kayıt yazmaz.
func TestYedekCreate_Reddedilen_KayitYazmaz(t *testing.T) {
	db := bdnDB(t)
	cagri := bdnGorevDikis(t)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, is_demo, reseller_id) VALUES (44,'demo.test','c_demo',1,9)`)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, reseller_id) VALUES (45,'yabanci.test','kok_hesap',9)`)
	hn := bdnRouter(&Handlers{DB: db})
	for _, c := range []struct {
		ad, yol string
		kod     int
	}{
		{"bilinmeyen domain", "/domains/999/backups", http.StatusNotFound},
		{"demo", "/domains/44/backups", http.StatusForbidden},
		{"c_ önekli değil", "/domains/45/backups", http.StatusBadRequest},
	} {
		if rec := ymIstek(t, hn, http.MethodPost, c.yol, ymAdminTok(t), ""); rec.Code != c.kod {
			t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, rec.Code, rec.Body.String())
		}
	}
	// Hesap kilidi dolu → 429.
	if _, ok := yedekBaslaTur(42, "manuel"); !ok {
		t.Fatal("kilit alınamadı")
	}
	t.Cleanup(func() { yedekBitir(42) })
	if rec := ymIstek(t, hn, http.MethodPost, "/domains/42/backups", ymAdminTok(t), ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("kilit doluyken 429 beklenir: %d", rec.Code)
	}
	if len(*cagri) != 0 {
		t.Errorf("reddedilen isteklerde görev başlamamalı: %v", *cagri)
	}
	if n := bdnToplamKayit(t, db); n != 0 {
		t.Errorf("reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
	}
}

// ── Delete ───────────────────────────────────────────────────────────────────

// bdnYedek: domainine bir yedek satırı + (kok altında) dosya ekler; (bid, yol) döner.
func bdnYedek(t *testing.T, db *sql.DB, kok string, id int64, sk, dosya string) (int64, string) {
	t.Helper()
	yol := silinenDosya(t, kok, sk, dosya)
	r, err := db.Exec(`INSERT INTO backups (domain_id, dosya) VALUES (?,?)`, id, dosya)
	if err != nil {
		t.Fatal(err)
	}
	bid, _ := r.LastInsertId()
	return bid, yol
}

func bdnSil(t *testing.T, db *sql.DB, tok string, id, bid int64, sorgu string) (int, string) {
	t.Helper()
	yol := "/domains/" + strconv.FormatInt(id, 10) + "/backups/" + strconv.FormatInt(bid, 10) + sorgu
	rec := ymIstek(t, bdnRouter(&Handlers{DB: db}), http.MethodDelete, yol, tok, "")
	return rec.Code, rec.Body.String()
}

func TestYedekSil_Basari_Denetim(t *testing.T) {
	for _, c := range []struct {
		ad, tok string
		id      int64
		sk      string
		hedef   string
		kapsam  int64
		aktor   string
	}{
		{"yönetici → bayi domaini", "admin", 42, "c_bayi9", "c_bayi9-20261001-000000.tar.gz", 9, "kok"},
		{"bayi kendi domaini", "bayi", 42, "c_bayi9", "c_bayi9-20261002-000000.tar.gz", 9, "bayi_a"},
		{"yönetici → kök domaini", "admin", 43, "c_kok43", "c_kok43-20261003-000000.tar.gz", 0, "kok"},
	} {
		t.Run(c.ad, func(t *testing.T) {
			db := bdnDB(t)
			kok := silinenOrtam(t)
			bid, yol := bdnYedek(t, db, kok, c.id, c.sk, c.hedef)
			tok := ymAdminTok(t)
			if c.tok == "bayi" {
				tok = ymBayiTok(t)
			}
			kod, g := bdnSil(t, db, tok, c.id, bid, "")
			if kod != http.StatusOK {
				t.Fatalf("200 beklenir: %d %s", kod, g)
			}
			if varMi(yol) || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=?`, bid) != 0 {
				t.Error("dosya ve kayıt silinmeliydi")
			}
			bdnTek(t, db, "yedek.sil", c.hedef, "", 1, c.kapsam, c.aktor)
		})
	}
}

// Uzak kopya silinemedi: 409, yedek KORUNUR, ok=0 kayıt (ham lftp çıktısı/host detaya GİRMEZ).
func TestYedekSil_UzakHatasi_BasarisizKayit(t *testing.T) {
	db := bdnDB(t)
	kok := silinenOrtam(t)
	silinenUzakHedef(t, db) // genel uzak hedef: host ornek.invalid
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "lftp"), []byte("#!/bin/sh\necho 'Login failed: 530 Login incorrect (ornek.invalid)' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	bid, yol := bdnYedek(t, db, kok, 42, "c_bayi9", "c_bayi9-20261004-000000.tar.gz")
	kod, g := bdnSil(t, db, ymAdminTok(t), 42, bid, "")
	if kod != http.StatusConflict || !strings.Contains(g, `"ok":false`) {
		t.Fatalf("409 beklenir: %d %s", kod, g)
	}
	if !varMi(yol) || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=?`, bid) != 1 {
		t.Error("uzak silinemediyse yerel dosya ve kayıt KALMALI")
	}
	bdnTek(t, db, "yedek.sil", "c_bayi9-20261004-000000.tar.gz", "uzak kopya silinemedi, yedek korundu", 0, 9, "kok")
}

// Yerel dosya silinemedi (uzak ve DB tamam): 200 + uyarı; kayıt ok=1 ama detayı söyler.
func TestYedekSil_YerelDosyaSilinemedi_Detayli(t *testing.T) {
	db := bdnDB(t)
	kok := silinenOrtam(t)
	const dosya = "c_bayi9-20261005-000000.tar.gz"
	// Dosya yerine DOLU bir dizin: os.Remove "directory not empty" ile düşer (yok değil).
	if err := os.MkdirAll(filepath.Join(kok, "c_bayi9", dosya), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kok, "c_bayi9", dosya, "i"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := db.Exec(`INSERT INTO backups (domain_id, dosya) VALUES (42,?)`, dosya)
	if err != nil {
		t.Fatal(err)
	}
	bid, _ := r.LastInsertId()
	kod, g := bdnSil(t, db, ymAdminTok(t), 42, bid, "")
	if kod != http.StatusOK || !strings.Contains(g, "yerel dosya silinemedi") {
		t.Fatalf("200 + uyarı beklenir: %d %s", kod, g)
	}
	bdnTek(t, db, "yedek.sil", dosya, "yerel dosya silinemedi", 1, 9, "kok")
}

// "lftp kurulu değil" ve "uzak kopyayı yoksay" kayıtlarının KAPSAMI domainin sahibidir.
// (Eskiden domain KİMLİĞİ kapsam yerine yazılıyordu → kayıt yanlış kiracıya düşüyordu.)
func TestYedekSil_UzakAtlanan_KapsamDomaininSahibi(t *testing.T) {
	t.Run("lftp yok", func(t *testing.T) {
		db := bdnDB(t)
		kok := silinenOrtam(t)
		silinenUzakHedef(t, db)
		t.Setenv("PATH", t.TempDir()) // lftp yok → ErrLftpYok
		bid, _ := bdnYedek(t, db, kok, 42, "c_bayi9", "c_bayi9-20261006-000000.tar.gz")
		if kod, g := bdnSil(t, db, ymAdminTok(t), 42, bid, ""); kod != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", kod, g)
		}
		bdnTek(t, db, "yedek.sil.lftp-yok", "c_bayi9-20261006-000000.tar.gz", "uzak kopya kaldı", 1, 9, "kok")
	})
	t.Run("uzağı yoksay", func(t *testing.T) {
		db := bdnDB(t)
		kok := silinenOrtam(t)
		bid, _ := bdnYedek(t, db, kok, 42, "c_bayi9", "c_bayi9-20261007-000000.tar.gz")
		if kod, g := bdnSil(t, db, ymAdminTok(t), 42, bid, "?uzagi_yoksay=1"); kod != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", kod, g)
		}
		bdnTek(t, db, "yedek.sil.uzak-yoksayildi", "c_bayi9-20261007-000000.tar.gz", "uzak kopya BIRAKILDI (kullanıcı onayı)", 1, 9, "kok")
	})
	t.Run("kök domain kapsam 0", func(t *testing.T) {
		db := bdnDB(t)
		kok := silinenOrtam(t)
		bid, _ := bdnYedek(t, db, kok, 43, "c_kok43", "c_kok43-20261008-000000.tar.gz")
		if kod, _ := bdnSil(t, db, ymAdminTok(t), 43, bid, "?uzagi_yoksay=1"); kod != http.StatusOK {
			t.Fatalf("200 beklenir: %d", kod)
		}
		bdnTek(t, db, "yedek.sil.uzak-yoksayildi", "c_kok43-20261008-000000.tar.gz", "uzak kopya BIRAKILDI (kullanıcı onayı)", 1, 0, "kok")
	})
}

// Bulunamayan yedek: 404, kayıt yok.
func TestYedekSil_Bulunamadi_KayitYazmaz(t *testing.T) {
	db := bdnDB(t)
	silinenOrtam(t)
	if kod, _ := bdnSil(t, db, ymAdminTok(t), 42, 12345, ""); kod != http.StatusNotFound {
		t.Fatalf("404 beklenir: %d", kod)
	}
	if n := bdnToplamKayit(t, db); n != 0 {
		t.Errorf("404'te kayıt yazılmamalı: %d", n)
	}
}

// ── SetSchedule ──────────────────────────────────────────────────────────────

func TestYedekZamanlama_Denetim(t *testing.T) {
	t.Run("yönetici başarı", func(t *testing.T) {
		db := bdnDB(t)
		hn := bdnRouter(&Handlers{DB: db})
		rec := ymIstek(t, hn, http.MethodPut, "/domains/42/backup-schedule", ymAdminTok(t), `{"freq":"weekly","hour":4,"retention":200}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", rec.Code, rec.Body.String())
		}
		// saklama 90'a kırpılır: denetim UYGULANAN değeri yazar.
		bdnTek(t, db, "yedek.zamanlama", "bayi9.test", "siklik=weekly saat=4 saklama=90", 1, 9, "kok")
	})
	t.Run("kök domain", func(t *testing.T) {
		db := bdnDB(t)
		hn := bdnRouter(&Handlers{DB: db})
		rec := ymIstek(t, hn, http.MethodPut, "/domains/43/backup-schedule", ymAdminTok(t), `{"freq":"none","hour":0,"retention":0}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", rec.Code, rec.Body.String())
		}
		bdnTek(t, db, "yedek.zamanlama", "kok.test", "siklik=none saat=0 saklama=1", 1, 0, "kok")
	})
	t.Run("DB hatası", func(t *testing.T) {
		db := bdnDB(t)
		g113_exec(t, db, `ALTER TABLE domains DROP COLUMN backup_hour`) // UPDATE düşer; lookupDomain etkilenmez
		hn := bdnRouter(&Handlers{DB: db})
		rec := ymIstek(t, hn, http.MethodPut, "/domains/42/backup-schedule", ymAdminTok(t), `{"freq":"daily","hour":3,"retention":7}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d %s", rec.Code, rec.Body.String())
		}
		bdnTek(t, db, "yedek.zamanlama", "bayi9.test", "siklik=daily saat=3 saklama=7", 0, 9, "kok")
	})
	t.Run("reddedilenler kayıtsız", func(t *testing.T) {
		db := bdnDB(t)
		g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, is_demo, reseller_id) VALUES (44,'demo.test','c_demo',1,9)`)
		hn := bdnRouter(&Handlers{DB: db})
		for _, c := range []struct {
			ad, tok, yol, govde string
			kod                 int
		}{
			{"bayi yetkisiz", "bayi", "/domains/42/backup-schedule", `{"freq":"daily","hour":3,"retention":7}`, http.StatusForbidden},
			{"bilinmeyen domain", "admin", "/domains/999/backup-schedule", `{"freq":"daily","hour":3,"retention":7}`, http.StatusNotFound},
			{"demo", "admin", "/domains/44/backup-schedule", `{"freq":"daily","hour":3,"retention":7}`, http.StatusForbidden},
			{"geçersiz sıklık", "admin", "/domains/42/backup-schedule", `{"freq":"hourly","hour":3,"retention":7}`, http.StatusBadRequest},
			{"geçersiz saat", "admin", "/domains/42/backup-schedule", `{"freq":"daily","hour":24,"retention":7}`, http.StatusBadRequest},
			{"bozuk gövde", "admin", "/domains/42/backup-schedule", `{`, http.StatusBadRequest},
		} {
			tok := ymAdminTok(t)
			if c.tok == "bayi" {
				tok = ymBayiTok(t)
			}
			if rec := ymIstek(t, hn, http.MethodPut, c.yol, tok, c.govde); rec.Code != c.kod {
				t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, rec.Code, rec.Body.String())
			}
		}
		if n := bdnToplamKayit(t, db); n != 0 {
			t.Errorf("reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
		}
	})
}
