package domains

// ftp_ek_test.go — ek FTP hesapları uçları (kapı zinciri main.go ile aynı: RequireAuth +
// MusteriScope). Şema ap_db + üretimdeki ek kolonlar + GERÇEK göç 0150.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/ftpek"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

func fe_kur(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	ftpek.HizSifirla()
	db := ap_db(t)
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil); ftpek.HizSifirla() })
	for _, q := range []string{
		`ALTER TABLE ftp_accounts ADD COLUMN home_dir VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN uid_n INT NOT NULL DEFAULT 1001, ADD COLUMN gid_n INT NOT NULL DEFAULT 1001`,
		`ALTER TABLE ftp_accounts ADD COLUMN created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile("../../migrations/0150_ftp_ek_hesap.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range strings.Split(string(b), ";") {
		var satir []string
		for _, l := range strings.Split(s, "\n") {
			if ts := strings.TrimSpace(l); ts != "" && !strings.HasPrefix(ts, "--") {
				satir = append(satir, l)
			}
		}
		if st := strings.TrimSpace(strings.Join(satir, "\n")); st != "" {
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0150: %v", err)
			}
		}
	}
	kok := t.TempDir()
	eskiHome, eskiLimit := ftpek.HomeKok, ftpek.LimitOku
	ftpek.HomeKok = kok
	ftpek.LimitOku = func(context.Context, *sql.DB, int64) int { return 0 }
	t.Cleanup(func() { ftpek.HomeKok, ftpek.LimitOku = eskiHome, eskiLimit })
	for _, sk := range []string{"c_bayi2", "c_kurban", "c_askida"} {
		if err := os.MkdirAll(filepath.Join(kok, sk, "public_html"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(ap_sir))
			r.With(middleware.MusteriScope).Get("/domains/{id}/ftp-ek", h.FTPEkListe)
			r.With(middleware.MusteriScope).Post("/domains/{id}/ftp-ek", h.FTPEkOlustur)
			r.With(middleware.MusteriScope).Put("/domains/{id}/ftp-ek/{kullanici}", h.FTPEkGuncelle)
			r.With(middleware.MusteriScope).Put("/domains/{id}/ftp-ek/{kullanici}/password", h.FTPEkParola)
			r.With(middleware.MusteriScope).Delete("/domains/{id}/ftp-ek/{kullanici}", h.FTPEkSil)
		})
	})
	return db, r
}

func fe_denetimler(t *testing.T, db *sql.DB, eylem string) []string {
	t.Helper()
	var out []string
	for i := 0; i < 20; i++ {
		out = nil
		rows, err := db.Query(`SELECT CONCAT_WS('|', actor_username, COALESCE(target,''), COALESCE(CAST(detail AS CHAR),''), ok) FROM audit_log WHERE action=?`, eylem)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			out = append(out, s)
		}
		_ = rows.Close()
		if len(out) > 0 {
			return out
		}
		time.Sleep(100 * time.Millisecond)
	}
	return out
}

func TestFTPEk_MusteriAkisiVeParolaSizmaz(t *testing.T) {
	db, h := fe_kur(t)
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	m := gp_musteri(t, db, 20, false)

	// Parola verilmez → sunucu üretir ve YALNIZ yanıtta bir kez döner.
	y := gp_istek(t, h, "POST", m, "/domains/20/ftp-ek", map[string]any{"ek": "Foto", "dizin": "/public_html/foto", "dizin_olustur": true})
	if y.kod != 201 {
		t.Fatalf("oluştur: %d %s", y.kod, y.ham)
	}
	uretilen, _ := y.govde["parola"].(string)
	if len(uretilen) < 12 || y.govde["uretildi"] != true {
		t.Fatalf("üretilen parola yanıtta bir kez dönmeli: %s", y.ham)
	}
	hesap, _ := y.govde["hesap"].(map[string]any)
	if hesap["kullanici"] != "c_bayi2-foto" || hesap["dizin"] != "/public_html/foto" || hesap["izin"] != "rw" {
		t.Fatalf("hesap: %v", hesap)
	}
	var hash, dir string
	_ = db.QueryRow(`SELECT password_md5, home_dir FROM ftp_accounts WHERE username='c_bayi2-foto'`).Scan(&hash, &dir)
	if !strings.HasPrefix(hash, "$6$") || !hesaplar.FTPParolaDogrula(uretilen, hash) {
		t.Fatalf("parola $6$ crypt olarak saklanmalı: %q", hash)
	}
	if dir != filepath.Join(ftpek.HomeKok, "c_bayi2", "public_html", "foto") {
		t.Fatalf("home_dir: %s", dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("klasör oluşmadı: %v", err)
	}

	// Kullanıcı parolasını kendisi verir: yanıtta parola ALANI YOK.
	kendi := "Kendi-Parolam-2026x"
	y = gp_istek(t, h, "POST", m, "/domains/20/ftp-ek", map[string]any{"ek": "yedek", "dizin": "public_html", "parola": kendi})
	if y.kod != 201 || y.govde["parola"] != nil || strings.Contains(y.ham, kendi) {
		t.Fatalf("kendi parola: %d %s", y.kod, y.ham)
	}
	// Zayıf parola reddedilir.
	if y = gp_istek(t, h, "POST", m, "/domains/20/ftp-ek", map[string]any{"ek": "zayif", "dizin": "public_html", "parola": "kisa1"}); y.kod != 400 {
		t.Fatalf("zayıf parola: %d %s", y.kod, y.ham)
	}

	// Liste: parola/hash yok.
	y = gp_istek(t, h, "GET", m, "/domains/20/ftp-ek", nil)
	if y.kod != 200 || strings.Contains(y.ham, "$6$") || strings.Contains(y.ham, uretilen) || strings.Contains(y.ham, kendi) || strings.Contains(strings.ToLower(y.ham), "password") {
		t.Fatalf("liste: %d %s", y.kod, y.ham)
	}
	if y.govde["onek"] != "c_bayi2-" || y.govde["kullanim"] != float64(2) {
		t.Fatalf("liste alanları: %s", y.ham)
	}

	// Parola değiştir (üretilir) + eski parola geçersiz.
	y = gp_istek(t, h, "PUT", m, "/domains/20/ftp-ek/c_bayi2-foto/password", map[string]any{})
	yeni, _ := y.govde["parola"].(string)
	if y.kod != 200 || yeni == "" || yeni == uretilen {
		t.Fatalf("parola yenile: %d %s", y.kod, y.ham)
	}
	_ = db.QueryRow(`SELECT password_md5 FROM ftp_accounts WHERE username='c_bayi2-foto'`).Scan(&hash)
	if !hesaplar.FTPParolaDogrula(yeni, hash) || hesaplar.FTPParolaDogrula(uretilen, hash) {
		t.Fatal("parola değişmedi")
	}

	// Askıya al / aç, dizin değiştir, sil.
	if y = gp_istek(t, h, "PUT", m, "/domains/20/ftp-ek/c_bayi2-foto", map[string]any{"etkin": false}); y.kod != 200 {
		t.Fatalf("askı: %d %s", y.kod, y.ham)
	}
	var durum string
	_ = db.QueryRow(`SELECT status FROM ftp_accounts WHERE username='c_bayi2-foto'`).Scan(&durum)
	if durum != "suspended" {
		t.Fatalf("askıdaki ek hesap pure-ftpd'ye açık: %s", durum)
	}
	if y = gp_istek(t, h, "PUT", m, "/domains/20/ftp-ek/c_bayi2-foto", map[string]any{"dizin": "/public_html", "etkin": true}); y.kod != 200 {
		t.Fatalf("dizin+aç: %d %s", y.kod, y.ham)
	}
	if y = gp_istek(t, h, "PUT", m, "/domains/20/ftp-ek/c_bayi2-foto", map[string]any{"izin": "ro"}); y.kod != 400 {
		t.Fatalf("salt-okuma henüz yok, 400 beklenir: %d %s", y.kod, y.ham)
	}
	if y = gp_istek(t, h, "DELETE", m, "/domains/20/ftp-ek/c_bayi2-foto", nil); y.kod != 200 {
		t.Fatalf("sil: %d %s", y.kod, y.ham)
	}
	if y = gp_istek(t, h, "DELETE", m, "/domains/20/ftp-ek/c_bayi2-foto", nil); y.kod != 404 {
		t.Fatalf("ikinci sil 404: %d", y.kod)
	}

	// Denetim: eylemler yazıldı; hiçbir parola (üretilen/kendi/yeni) ne denetimde ne günlükte.
	for _, e := range []string{"ftp.ek_olustur", "ftp.ek_parola", "ftp.ek_guncelle", "ftp.ek_sil"} {
		if len(fe_denetimler(t, db, e)) == 0 {
			t.Fatalf("denetim yok: %s", e)
		}
	}
	rows, _ := db.Query(`SELECT CONCAT_WS('|', actor_username, COALESCE(target,''), COALESCE(CAST(detail AS CHAR),'')) FROM audit_log`)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		for _, gizli := range []string{uretilen, kendi, yeni, "$6$"} {
			if strings.Contains(s, gizli) {
				t.Fatalf("denetim kaydı parola/hash içeriyor: %s", s)
			}
		}
	}
	_ = rows.Close()
	for _, gizli := range []string{uretilen, kendi, yeni, "$6$"} {
		if strings.Contains(logBuf.String(), gizli) {
			t.Fatalf("günlük parola/hash içeriyor: %s", logBuf.String())
		}
	}
}

func TestFTPEk_KiraciIzolasyonuIDOR(t *testing.T) {
	db, h := fe_kur(t)
	m20 := gp_musteri(t, db, 20, false)
	m30 := gp_musteri(t, db, 30, false)
	if y := gp_istek(t, h, "POST", m30, "/domains/30/ftp-ek", map[string]any{"ek": "kurban", "dizin": "public_html"}); y.kod != 201 {
		t.Fatalf("hazırlık: %d %s", y.kod, y.ham)
	}
	kurban := "c_kurban-kurban"
	// Başka alan adının uçları: MusteriScope 403 (liste dahil).
	for _, c := range []struct{ metod, yol string }{
		{"GET", "/domains/30/ftp-ek"}, {"POST", "/domains/30/ftp-ek"},
		{"PUT", "/domains/30/ftp-ek/" + kurban}, {"PUT", "/domains/30/ftp-ek/" + kurban + "/password"},
		{"DELETE", "/domains/30/ftp-ek/" + kurban},
	} {
		if y := gp_istek(t, h, c.metod, m20, c.yol, map[string]any{"ek": "x", "dizin": "public_html", "etkin": false}); y.kod != 403 {
			t.Fatalf("%s %s: 403 beklenir, %d", c.metod, c.yol, y.kod)
		}
	}
	// Kendi alan adı + başkasının kullanıcı adı: 404 (varlık sızmaz) ve kurban değişmedi.
	for _, c := range []struct{ metod, yol string }{
		{"PUT", "/domains/20/ftp-ek/" + kurban}, {"PUT", "/domains/20/ftp-ek/" + kurban + "/password"},
		{"DELETE", "/domains/20/ftp-ek/" + kurban},
	} {
		if y := gp_istek(t, h, c.metod, m20, c.yol, map[string]any{"etkin": false, "parola": "Saldirgan-Parola-12"}); y.kod != 404 {
			t.Fatalf("%s %s: 404 beklenir, %d %s", c.metod, c.yol, y.kod, y.ham)
		}
	}
	var durum string
	_ = db.QueryRow(`SELECT status FROM ftp_accounts WHERE username=?`, kurban).Scan(&durum)
	if durum != "active" {
		t.Fatal("kurban hesap değişti")
	}
	// Bayi: yalnız kendi alan adları. bayi2 → domain 30 (bayi3'ün) 403; bayi3 → kendi 200.
	if y := gp_istek(t, h, "GET", gp_tok(t, "reseller", 2), "/domains/30/ftp-ek", nil); y.kod != 403 {
		t.Fatalf("bayi başka bayinin alan adı: %d", y.kod)
	}
	if y := gp_istek(t, h, "GET", gp_tok(t, "reseller", 3), "/domains/30/ftp-ek", nil); y.kod != 200 {
		t.Fatalf("bayi kendi alan adı: %d", y.kod)
	}
	if y := gp_istek(t, h, "GET", gp_tok(t, "admin", 1), "/domains/30/ftp-ek", nil); y.kod != 200 {
		t.Fatalf("kök: %d", y.kod)
	}
	// Olmayan alan adı: bayi 403 (MusteriScope), kök 404.
	if y := gp_istek(t, h, "GET", gp_tok(t, "admin", 1), "/domains/999/ftp-ek", nil); y.kod != 404 {
		t.Fatalf("olmayan alan adı: %d", y.kod)
	}
	// Token'sız.
	if y := gp_istek(t, h, "GET", "", "/domains/20/ftp-ek", nil); y.kod != 401 {
		t.Fatalf("token yok: %d", y.kod)
	}
}

func TestFTPEk_APIAnahtariGiremez(t *testing.T) {
	db, h := fe_kur(t)
	k := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz", "hesap:yaz", "hesap:oku")
	for _, c := range []struct{ metod, yol string }{
		{"GET", "/domains/20/ftp-ek"}, {"POST", "/domains/20/ftp-ek"},
		{"PUT", "/domains/20/ftp-ek/c_bayi2-x"}, {"PUT", "/domains/20/ftp-ek/c_bayi2-x/password"},
		{"DELETE", "/domains/20/ftp-ek/c_bayi2-x"},
	} {
		y := gp_istek(t, h, c.metod, k.yetki(), c.yol, map[string]any{"ek": "x", "dizin": "public_html"})
		if y.kod != 403 && y.kod != 401 {
			t.Fatalf("API anahtarı %s %s: %d %s", c.metod, c.yol, y.kod, y.ham)
		}
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE tur='ek'`).Scan(&n)
	if n != 0 {
		t.Fatalf("API anahtarı ek hesap oluşturdu: %d", n)
	}
}

func TestFTPEk_AskidaHostingteOlusturulamaz(t *testing.T) {
	db, h := fe_kur(t)
	// Müşteri askıdaki hostingte kapıda durur (MusteriScope askı kapısı).
	if y := gp_istek(t, h, "POST", gp_musteri(t, db, 21, false), "/domains/21/ftp-ek", map[string]any{"ek": "x", "dizin": "public_html"}); y.kod != 403 {
		t.Fatalf("askıdaki müşteri: %d %s", y.kod, y.ham)
	}
	// Yönetici de askıdaki hostingte hesap AÇAMAZ (pure-ftpd status=suspended olurdu; baştan 409).
	if y := gp_istek(t, h, "POST", gp_tok(t, "admin", 1), "/domains/21/ftp-ek", map[string]any{"ek": "x", "dizin": "public_html"}); y.kod != 409 {
		t.Fatalf("yönetici askıdaki hosting: %d %s", y.kod, y.ham)
	}
}

func TestFTPEk_DizinKacisiVeHizSiniri(t *testing.T) {
	db, h := fe_kur(t)
	m := gp_musteri(t, db, 20, false)
	dis := t.TempDir()
	_ = os.Symlink(dis, filepath.Join(ftpek.HomeKok, "c_bayi2", "kacak"))
	for _, d := range []string{"../c_kurban/public_html", "/etc", "/home/c_kurban/public_html", ".ssh", "/", "kacak/x", "a\x00b"} {
		y := gp_istek(t, h, "POST", m, "/domains/20/ftp-ek", map[string]any{"ek": "x", "dizin": d, "dizin_olustur": true})
		if y.kod != 400 {
			t.Fatalf("dizin %q: 400 beklenir, %d %s", d, y.kod, y.ham)
		}
	}
	if ents, _ := os.ReadDir(dis); len(ents) != 0 {
		t.Fatalf("dışarıda oluşturma: %v", ents)
	}
	// Hız sınırı: pencere dolunca 429 + Retry-After.
	var son ap_yanit
	for i := 0; i < ftpek.HizMaks+1; i++ {
		son = gp_istek(t, h, "PUT", m, fmt.Sprintf("/domains/20/ftp-ek/c_bayi2-yok%d", i), map[string]any{"etkin": false})
	}
	if son.kod != 429 {
		t.Fatalf("hız sınırı: %d", son.kod)
	}
}
