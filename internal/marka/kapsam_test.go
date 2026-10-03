package marka

// kapsam_test.go — bayi kapsamı (IDOR), çözümleme sırası ve moderasyon; GERÇEK
// MariaDB (GOSP_TEST_GECICI_DSN; yoksa ATLANIR) + gerçek RequireAuth + imzalı
// jetonlar + main.go ile aynı rota yerleşimi (GenelRotalar dışarıda,
// KimlikliRotalar RequireAuth grubunda).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

var kt_sir = []byte("marka-kapsam-test-anahtari-0123456789abcdef")

const (
	ktBayiA = int64(7)
	ktBayiB = int64(8)
)

func ktDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	k := cfg.Clone()
	k.DBName = ""
	kok, err := sql.Open("mysql", k.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_marka_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	c := cfg.Clone()
	c.DBName = ad
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL,
		  role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user', status ENUM('active','suspended') NOT NULL DEFAULT 'active') ENGINE=InnoDB`,
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL, reseller_id BIGINT UNSIGNED NULL) ENGINE=InnoDB`,
		`CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) NOT NULL PRIMARY KEY, deger TEXT NOT NULL) ENGINE=InnoDB`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'),(7,'bayiA','reseller'),(8,'bayiB','reseller')`,
		`INSERT INTO domains VALUES (42,'a-musteri.test',7),(43,'b-musteri.test',8),(44,'kok-musteri.test',NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("hazırlık: %v\n%s", err, q)
		}
	}
	for _, dosya := range []string{"0122_bayi_marka.sql", "0123_bayi_marka_alanadi.sql"} {
		ktGoc(t, db, dosya)
	}
	return db
}

// ktGoc — runMigrations ile aynı ayrıştırma (satır başı -- atılır, ; ile bölünür).
func ktGoc(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", dosya))
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if x := strings.TrimSpace(s); x == "" || strings.HasPrefix(x, "--") {
			continue
		}
		temiz = append(temiz, s)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("GÖÇ HATASI (%s): %v", dosya, err)
			}
		}
	}
}

type ktOrtam struct {
	t                         *testing.T
	db                        *sql.DB
	r                         http.Handler
	admin, bayiA, bayiB, musA string
	musB, musKok              string
}

func ktKur(t *testing.T) *ktOrtam {
	t.Helper()
	db := ktDB(t)
	Kok = t.TempDir()
	OnbellekTemizle()
	t.Cleanup(OnbellekTemizle)
	kok := Varsayilan()
	kok.PanelAdi, kok.Baslik = "KokMarka", "KokMarka"
	ham, _ := json.Marshal(kok)
	if err := os.WriteFile(filepath.Join(Kok, AyarDosya), ham, 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db, SunucuIPleri: []string{"203.0.113.10"}}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h.GenelRotalar(r)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(kt_sir))
			h.KimlikliRotalar(r)
		})
	})
	o := &ktOrtam{t: t, db: db, r: r}
	var err error
	if o.admin, err = auth.IssueAt(kt_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if o.bayiA, err = auth.IssueResellerAt(kt_sir, 3600, ktBayiA, "bayiA", ktBayiA, 0, ""); err != nil {
		t.Fatal(err)
	}
	if o.bayiB, err = auth.IssueResellerAt(kt_sir, 3600, ktBayiB, "bayiB", ktBayiB, 0, ""); err != nil {
		t.Fatal(err)
	}
	mus := func(did int64) string {
		s, _, err := auth.GenerateMusteri(kt_sir, auth.MusteriClaims{FTPHesapID: did * 10, DomainID: did, Kullanici: "m", AlanAdi: "x.test"}, 3600)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	o.musA, o.musB, o.musKok = mus(42), mus(43), mus(44)
	return o
}

func (o *ktOrtam) istek(yontem, yol, tok string, govde any, basliklar ...string) (int, string) {
	o.t.Helper()
	var rd *bytes.Reader
	switch g := govde.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(g))
	default:
		b, _ := json.Marshal(g)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(yontem, yol, rd)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(basliklar); i += 2 {
		if basliklar[i] == "Host" {
			req.Host = basliklar[i+1]
		} else {
			req.Header.Set(basliklar[i], basliklar[i+1])
		}
	}
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (o *ktOrtam) bayiAd(rid int64) (string, bool) {
	var ham string
	var etkin int
	err := o.db.QueryRow(`SELECT ayar_json, etkin FROM cp_marka WHERE reseller_id=?`, rid).Scan(&ham, &etkin)
	if err != nil {
		return "", false
	}
	var m Marka
	_ = json.Unmarshal([]byte(ham), &m)
	return m.PanelAdi, etkin == 1
}

func (o *ktOrtam) anahtar(rid int64) string {
	var a string
	_ = o.db.QueryRow(`SELECT anahtar FROM cp_marka WHERE reseller_id=?`, rid).Scan(&a)
	return a
}

// Bayi A yalnız KENDİ satırını yazar; sorgu parametresi/gövde sahibi seçemez.
func TestYonet_BayiYalnizKendiSatiri(t *testing.T) {
	o := ktKur(t)
	if k, g := o.istek("PUT", "/api/v1/marka/yonet", o.bayiB, map[string]any{"panel_adi": "Beta"}); k != 200 {
		t.Fatalf("B kaydı: %d %s", k, g)
	}
	k, g := o.istek("PUT", "/api/v1/marka/yonet?reseller_id=8", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	if k != 200 {
		t.Fatalf("A kaydı: %d %s", k, g)
	}
	if ad, _ := o.bayiAd(ktBayiA); ad != "Alfa" {
		t.Errorf("A satırı Alfa olmalı: %q", ad)
	}
	if ad, _ := o.bayiAd(ktBayiB); ad != "Beta" {
		t.Errorf("?reseller_id=8 B'nin satırını değiştirdi: %q", ad)
	}
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, `{"panel_adi":"X","reseller_id":8}`); k != 400 {
		t.Errorf("gövdede reseller_id → 400 beklenir, gelen %d", k)
	}
	if ad, _ := o.bayiAd(ktBayiB); ad != "Beta" {
		t.Errorf("gövdedeki reseller_id B'yi değiştirdi: %q", ad)
	}
	k, g = o.istek("GET", "/api/v1/marka/yonet?reseller_id=8", o.bayiA, nil)
	if k != 200 || !strings.Contains(g, `"Alfa"`) || strings.Contains(g, "Beta") || strings.Contains(g, o.anahtar(ktBayiB)) {
		t.Errorf("A'nın yönetim görünümü yalnız A'yı göstermeli: %d %s", k, g)
	}
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"webmail_marka": true}); k != 400 {
		t.Errorf("bayiden webmail_marka → 400 beklenir, gelen %d", k)
	}
	// Sürüm çakışması → 409.
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa2", "surum": 999}); k != 409 {
		t.Errorf("eski sürümle yazım 409 olmalı, gelen %d", k)
	}
}

// Moderasyon uçları yalnız kök; müşteri yönetim uçlarına giremez.
func TestModerasyon_YalnizKok(t *testing.T) {
	o := ktKur(t)
	for _, c := range []struct{ y, yol string }{
		{"GET", "/api/v1/marka/bayiler"}, {"PUT", "/api/v1/marka/bayiler/8"},
		{"POST", "/api/v1/marka/bayiler/8/sifirla"}, {"DELETE", "/api/v1/marka/bayiler/8/alan-adi"},
		{"PUT", "/api/v1/marka/ayar"},
	} {
		if k, _ := o.istek(c.y, c.yol, o.bayiA, `{"izin":false}`); k != 403 {
			t.Errorf("bayi %s %s → 403 beklenir, gelen %d", c.y, c.yol, k)
		}
	}
	if _, var_ := o.bayiAd(ktBayiB); var_ {
		t.Error("bayinin moderasyon denemesi B satırı oluşturdu")
	}
	for _, c := range []struct{ y, yol string }{{"GET", "/api/v1/marka/yonet"}, {"PUT", "/api/v1/marka/yonet"}, {"POST", "/api/v1/marka/yonet/sifirla"}, {"GET", "/api/v1/marka/alan-adi"}} {
		if k, _ := o.istek(c.y, c.yol, o.musA, `{}`); k != 403 {
			t.Errorf("müşteri %s %s → 403 beklenir, gelen %d", c.y, c.yol, k)
		}
	}
	// Kök izni kapatınca bayi yönetemez; olmayan/bayi olmayan id → 404.
	if k, g := o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"izin": false}); k != 200 {
		t.Fatalf("kök izin kapatma: %d %s", k, g)
	}
	if k, _ := o.istek("GET", "/api/v1/marka/yonet", o.bayiA, nil); k != 403 {
		t.Errorf("izin kapalıyken bayi GET → 403, gelen %d", k)
	}
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Z"}); k != 403 {
		t.Errorf("izin kapalıyken bayi PUT → 403, gelen %d", k)
	}
	for _, id := range []string{"1", "999", "abc"} {
		if k, _ := o.istek("PUT", "/api/v1/marka/bayiler/"+id, o.admin, map[string]any{"izin": true}); k != 404 {
			t.Errorf("bayi olmayan hedef %s → 404, gelen %d", id, k)
		}
	}
}

// Giriş öncesi: ?m= bayi markasını seçer; bilinmeyen/pasif anahtar kökle BAYT BAYT aynı.
func TestGenel_AnahtarVeAyniYanit(t *testing.T) {
	o := ktKur(t)
	o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa", "destek_eposta": "destek@alfa.test"})
	an := o.anahtar(ktBayiA)
	_, kok := o.istek("GET", "/api/v1/marka", "", nil)
	if !strings.Contains(kok, `"KokMarka"`) || !strings.Contains(kok, `"anahtar":"kok"`) {
		t.Fatalf("kök yanıtı: %s", kok)
	}
	k, g := o.istek("GET", "/api/v1/marka?m="+an, "", nil)
	if k != 200 || !strings.Contains(g, `"Alfa"`) || !strings.Contains(g, `"anahtar":"`+an+`"`) || !strings.Contains(g, "destek@alfa.test") {
		t.Errorf("?m=A → A markası: %d %s", k, g)
	}
	if strings.Contains(g, `"7"`) || strings.Contains(g, "bayiA") || strings.Contains(g, "reseller") {
		t.Errorf("kimliksiz yanıtta bayi kimliği sızdı: %s", g)
	}
	for _, m := range []string{"bilinmeyenanahtarxyz", "abcdefghijklmnopqrst", "7", "bayiA", "../x"} {
		if _, g := o.istek("GET", "/api/v1/marka?m="+m, "", nil); g != kok {
			t.Errorf("?m=%s kök yanıtıyla aynı olmalı:\n%s\n%s", m, g, kok)
		}
	}
	o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"kok_kapatti": true})
	if _, g := o.istek("GET", "/api/v1/marka?m="+an, "", nil); g != kok {
		t.Errorf("kökün kapattığı marka kök yanıtıyla aynı olmalı: %s", g)
	}
}

// Host: yalnız AKTİF ve izni açık özel alan adı markayı seçer.
func TestGenel_HostYalnizAktif(t *testing.T) {
	o := ktKur(t)
	o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	_, kok := o.istek("GET", "/api/v1/marka", "", nil)
	if _, err := o.db.Exec(`INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum) VALUES ('panel.alfa.test', 7, 'x', 'ssl_bekliyor')`); err != nil {
		t.Fatal(err)
	}
	OnbellekTemizle()
	if _, g := o.istek("GET", "/api/v1/marka", "", nil, "Host", "panel.alfa.test:2083"); g != kok {
		t.Errorf("aktif olmayan Host kök vermeli: %s", g)
	}
	o.db.Exec(`UPDATE cp_marka_alanadi SET durum='aktif'`)
	OnbellekTemizle()
	if _, g := o.istek("GET", "/api/v1/marka", "", nil, "Host", "panel.alfa.test:2083"); g != kok {
		t.Errorf("alanadi_izni kapalıyken Host kök vermeli: %s", g)
	}
	o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"alanadi_izni": true})
	OnbellekTemizle()
	if _, g := o.istek("GET", "/api/v1/marka", "", nil, "Host", "Panel.Alfa.Test:2083"); !strings.Contains(g, `"Alfa"`) {
		t.Errorf("aktif + izinli Host A markası vermeli: %s", g)
	}
	h := &Handlers{DB: o.db}
	req := httptest.NewRequest("POST", "/x", nil)
	req.Host = "panel.alfa.test:2083"
	if rid, ok := h.HostKiraciRID(req); !ok || rid != ktBayiA {
		t.Errorf("HostKiraciRID: %d %v", rid, ok)
	}
	req.Host = "panel.baska.test"
	if _, ok := h.HostKiraciRID(req); ok {
		t.Error("bilinmeyen Host kiracı vermemeli")
	}
}

// Oturum markası jetondan: müşteri bayisinin markasını görür.
func TestOturum_JetondanCozum(t *testing.T) {
	o := ktKur(t)
	o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	kontrol := func(tok, beklenen, ad string) {
		t.Helper()
		k, g := o.istek("GET", "/api/v1/marka/oturum?m="+o.anahtar(ktBayiA), tok, nil)
		if k != 200 || !strings.Contains(g, `"`+beklenen+`"`) {
			t.Errorf("%s oturum markası %q olmalı: %d %s", ad, beklenen, k, g)
		}
	}
	kontrol(o.musA, "Alfa", "müşteri(A)")
	kontrol(o.musKok, "KokMarka", "müşteri(kök)")
	kontrol(o.admin, "KokMarka", "kök")
	kontrol(o.bayiA, "Alfa", "bayi A")
	kontrol(o.musB, "KokMarka", "müşteri(B, marka yok)")
	o.istek("PUT", "/api/v1/marka/ayar", o.admin, map[string]any{"bayi_yedek": "notr"})
	kontrol(o.musB, "Kontrol Paneli", "müşteri(B, nötr yedek)")
	kontrol(o.musKok, "KokMarka", "müşteri(kök, nötr yedek)")
	if k, _ := o.istek("GET", "/api/v1/marka/oturum", "", nil); k != 401 {
		t.Errorf("oturumsuz /marka/oturum 401 olmalı, gelen %d", k)
	}
}

func ktCok(t *testing.T, o *ktOrtam, tok, yuva string, veri []byte) (int, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("dosya", "logo.png")
	fw.Write(veri)
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/marka/yonet/gorsel/"+yuva, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Görsel: bayi kendi dizinine yazar; servis yalnız geçerli anahtar+yuva; SVG red.
func TestGorsel_YuklemeVeServis(t *testing.T) {
	o := ktKur(t)
	o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	if k, g := ktCok(t, o, o.bayiA, "logo", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); k != 400 {
		t.Errorf("SVG yüklemesi 400 olmalı: %d %s", k, g)
	}
	if k, g := ktCok(t, o, o.bayiA, "logo", pngYap(t, 4, 4)); k != 200 || !strings.Contains(g, `"logo":true`) {
		t.Fatalf("PNG yüklemesi: %d %s", k, g)
	}
	an := o.anahtar(ktBayiA)
	if _, err := os.Stat(filepath.Join(Kok, "bayi", an, LogoDosya)); err != nil {
		t.Errorf("görsel bayi dizinine yazılmadı: %v", err)
	}
	if _, err := os.Stat(filepath.Join(Kok, LogoDosya)); err == nil {
		t.Error("bayi görseli KÖK yuvasına yazıldı")
	}
	req := httptest.NewRequest("GET", "/api/v1/marka/gorsel/"+an+"/logo", nil)
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("görsel servisi: %d %v", rec.Code, rec.Header())
	}
	for _, yol := range []string{"/api/v1/marka/gorsel/" + an + "/../marka.json", "/api/v1/marka/gorsel/..%2f..%2fetc/logo",
		"/api/v1/marka/gorsel/" + an + "/sifre", "/api/v1/marka/gorsel/abcdefghijklmnopqrst/logo"} {
		if k, _ := o.istek("GET", yol, "", nil); k != 404 {
			t.Errorf("%s → 404 beklenir, gelen %d", yol, k)
		}
	}
	// B, A'nın görselini silemez (yalnız kendi yuvası; A dosyası yerinde kalır).
	o.istek("DELETE", "/api/v1/marka/yonet/gorsel/logo", o.bayiB, nil)
	if _, err := os.Stat(filepath.Join(Kok, "bayi", an, LogoDosya)); err != nil {
		t.Error("B'nin silme isteği A'nın logosunu sildi")
	}
	o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"kok_kapatti": true})
	if k, _ := o.istek("GET", "/api/v1/marka/gorsel/"+an+"/logo", "", nil); k != 404 {
		t.Errorf("kapatılmış bayinin görseli 404 olmalı, gelen %d", k)
	}
}

// Özel alan adı: izin şart; başka bayinin adı genel metinle reddedilir.
func TestAlanAdi_IzinVeCakisma(t *testing.T) {
	o := ktKur(t)
	if k, _ := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "panel.alfa.test"}); k != 403 {
		t.Errorf("izin kapalıyken (varsayılan) ekleme 403 olmalı, gelen %d", k)
	}
	for _, rid := range []string{"7", "8"} {
		o.istek("PUT", "/api/v1/marka/bayiler/"+rid, o.admin, map[string]any{"alanadi_izni": true})
	}
	if k, g := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiB, map[string]any{"alan_adi": "panel.ortak.test"}); k != 200 {
		t.Fatalf("B ekleme: %d %s", k, g)
	}
	k, g := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "PANEL.ortak.test"})
	if k != 409 || strings.Contains(g, "bayiB") || strings.Contains(g, `"8"`) || strings.Contains(g, "#8") {
		t.Errorf("başka bayinin adı 409 + genel metin olmalı: %d %s", k, g)
	}
	if k, _ := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "1.2.3.4"}); k != 400 {
		t.Errorf("IP adı 400 olmalı, gelen %d", k)
	}
	if k, g := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "panel.alfa.test"}); k != 200 {
		t.Fatalf("A ekleme: %d %s", k, g)
	}
	if k, _ := o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "panel2.alfa.test"}); k != 409 {
		t.Errorf("bayi başına 1 sınırı: 409 beklenir, gelen %d", k)
	}
	// A'nın görünümü B'nin adını/jetonunu içermez.
	if _, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil); strings.Contains(g, "ortak") {
		t.Errorf("A görünümünde B'nin adı: %s", g)
	}
	// Doğrulama: DNS taklitleri.
	var jeton string
	o.db.QueryRow(`SELECT dogrulama FROM cp_marka_alanadi WHERE reseller_id=7`).Scan(&jeton)
	eskiA, eskiT := aBuSunucuya, txtCoz
	t.Cleanup(func() { aBuSunucuya, txtCoz = eskiA, eskiT })
	aBuSunucuya = func(string, []string) bool { return true }
	txtCoz = func(context.Context, string) ([]string, error) { return []string{"yanlis"}, nil }
	if k, _ := o.istek("POST", "/api/v1/marka/alan-adi/dogrula", o.bayiA, nil); k != 422 {
		t.Errorf("yanlış TXT 422 olmalı, gelen %d", k)
	}
	txtCoz = func(context.Context, string) ([]string, error) { return []string{jeton}, nil }
	if k, g := o.istek("POST", "/api/v1/marka/alan-adi/dogrula", o.bayiA, nil); k != 200 || !strings.Contains(g, "ssl_bekliyor") {
		t.Errorf("doğru TXT ssl_bekliyor olmalı: %d %s", k, g)
	}
	// A silme yalnız kendi satırını siler.
	o.istek("DELETE", "/api/v1/marka/alan-adi", o.bayiA, nil)
	var n int
	o.db.QueryRow(`SELECT COUNT(*) FROM cp_marka_alanadi WHERE reseller_id=8`).Scan(&n)
	if n != 1 {
		t.Errorf("A'nın silmesi B'nin adını sildi (kalan=%d)", n)
	}
}

// Bayi silinince FK iki tabloyu da temizler; yetim görsel dizini taranır.
func TestBayiSilme_CascadeVeYetim(t *testing.T) {
	o := ktKur(t)
	o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	ktCok(t, o, o.bayiA, "logo", pngYap(t, 2, 2))
	an := o.anahtar(ktBayiA)
	o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"alanadi_izni": true})
	o.istek("POST", "/api/v1/marka/alan-adi", o.bayiA, map[string]any{"alan_adi": "panel.alfa.test"})
	if _, err := o.db.Exec(`DELETE FROM users WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	var n1, n2 int
	o.db.QueryRow(`SELECT COUNT(*) FROM cp_marka WHERE reseller_id=7`).Scan(&n1)
	o.db.QueryRow(`SELECT COUNT(*) FROM cp_marka_alanadi WHERE reseller_id=7`).Scan(&n2)
	if n1+n2 != 0 {
		t.Errorf("FK cascade çalışmadı: %d %d", n1, n2)
	}
	YetimGorselTemizle(context.Background(), o.db)
	if _, err := os.Stat(filepath.Join(Kok, "bayi", an)); !os.IsNotExist(err) {
		t.Errorf("yetim görsel dizini silinmedi: %v", err)
	}
	// json_valid CHECK.
	if _, err := o.db.Exec(`INSERT INTO cp_marka (reseller_id, anahtar, ayar_json) VALUES (8, 'bbbbbbbbbbbbbbbbbbbb', 'bozuk{')`); err == nil {
		t.Error("geçersiz JSON CHECK ile reddedilmeli")
	}
	// Göç iki kez koşar (idempotent).
	ktGoc(t, o.db, "0122_bayi_marka.sql")
	ktGoc(t, o.db, "0123_bayi_marka_alanadi.sql")
}

// Kök yönetimi: dosyaya yazar, eklentinin bilmediği alanlar korunur, webmail yalnız kökte.
func TestKok_YazmaYolu(t *testing.T) {
	o := ktKur(t)
	k, g := o.istek("PUT", "/api/v1/marka/yonet", o.admin, map[string]any{"destek_telefon": "+90 212 555 0000", "webmail_marka": true})
	if k != 200 {
		t.Fatalf("kök PUT: %d %s", k, g)
	}
	m, err := OkuKesin()
	if err != nil || m.PanelAdi != "KokMarka" || m.DestekTelefon != "+90 212 555 0000" || !m.WebmailMarka {
		t.Errorf("kısmi güncelleme mevcut alanları korumalı: %+v %v", m, err)
	}
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.admin, map[string]any{"panel_adi": "  "}); k != 400 {
		t.Errorf("boş panel adı 400 olmalı, gelen %d", k)
	}
	if k, _ := o.istek("PUT", "/api/v1/marka/yonet", o.admin, map[string]any{"vurgu_renk": "red;background:url(x)"}); k != 400 {
		t.Errorf("CSS enjeksiyonlu renk 400 olmalı, gelen %d", k)
	}
	if k, g := ktCok(t, o, o.admin, "favicon", pngYap(t, 2, 2)); k != 200 || !strings.Contains(g, `"favicon":true`) {
		t.Errorf("kök favicon: %d %s", k, g)
	}
	if _, g := o.istek("GET", "/api/v1/marka", "", nil); !strings.Contains(g, `"favicon":true`) || strings.Contains(g, "webmail") {
		t.Errorf("kök genel görünüm: %s", g)
	}
}

// DB'ye elle yazılmış (doğrulanmamış) değer OKURKEN de süzülür: CSS enjeksiyonu,
// BIDI, javascript: bağlantısı kimliksiz yanıta geçmez.
func TestGenel_OkurkenNormalize(t *testing.T) {
	o := ktKur(t)
	ham := `{"panel_adi":"Nova‮Kotu","vurgu_renk":"red;background:url(//x)","destek_url":"javascript:alert(1)","destek_eposta":"a@b.com?cc=x@y.z"}`
	if _, err := o.db.Exec(`INSERT INTO cp_marka (reseller_id, anahtar, ayar_json, etkin) VALUES (7, 'cccccccccccccccccccc', ?, 1)`, ham); err != nil {
		t.Fatal(err)
	}
	_, g := o.istek("GET", "/api/v1/marka?m=cccccccccccccccccccc", "", nil)
	for _, yasak := range []string{"‮", `‮`, "red;", "javascript:", "cc=x"} {
		if strings.Contains(g, yasak) {
			t.Errorf("okurken süzülmeyen değer (%q): %s", yasak, g)
		}
	}
	if !strings.Contains(g, `"NovaKotu"`) || !strings.Contains(g, `"vurgu_renk":"#2563eb"`) {
		t.Errorf("normalize edilmiş değerler beklenir: %s", g)
	}
}
