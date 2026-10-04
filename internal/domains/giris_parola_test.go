package domains

// giris_parola_test.go — "Şifre değiştir" ucu (PUT /domains/{id}/giris-parola)
// UÇTAN UCA: gerçek RequireAuth + AdminVeyaReseller zinciri (main.go ile AYNI),
// gerçek işleyici, geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa ATLANIR). Şema/sahte
// SSH-eşitleme api_parola_test.go ile ortak.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

func gp_kur(t *testing.T) (*sql.DB, http.Handler, *ap_sahte) {
	t.Helper()
	girisParolaSifirla()
	db := ap_db(t)
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil); girisParolaSifirla() })
	s := &ap_sahte{kayt: map[string]int64{}}
	eS := apiSSHEsitle
	apiSSHEsitle = s.sshEsitle
	t.Cleanup(func() { apiSSHEsitle = eS })
	if _, err := db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, is_demo) VALUES (50,'demo.test','c_demo',2,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ftp_accounts (domain_id, username, password_md5, status) VALUES (50,'c_demo','x','active')`); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(ap_sir))
			r.With(middleware.AdminVeyaReseller).Put("/domains/{id}/giris-parola", h.GirisParola)
			r.With(middleware.MusteriScope, middleware.AdinaYasak).Put("/domains/{id}/ftp/password", h.SetFTPPassword)
			r.With(middleware.MusteriScope).Get("/domains/{id}/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
		})
	})
	return db, r, s
}

func gp_tok(t *testing.T, rol string, uid int64) string {
	t.Helper()
	var tok string
	var err error
	if rol == "admin" {
		tok, err = auth.Issue(ap_sir, 3600, uid, "admin", "admin")
	} else {
		tok, err = auth.IssueReseller(ap_sir, 3600, uid, "bayi", uid)
	}
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func gp_musteri(t *testing.T, db *sql.DB, domainID int64, adina bool) string {
	t.Helper()
	var fh int64
	if err := db.QueryRow(`SELECT id FROM ftp_accounts WHERE domain_id=? LIMIT 1`, domainID).Scan(&fh); err != nil {
		t.Fatal(err)
	}
	mc := auth.MusteriClaims{FTPHesapID: fh, DomainID: domainID, Kullanici: "x", Tip: "musteri"}
	if adina {
		mc.AktorUID, mc.AktorRol, mc.AktorRID, mc.AktorAd = 2, "reseller", 2, "bayi2"
	}
	omur := int64(3600)
	if adina {
		omur = auth.AdinaOmurSn
	}
	tok, _, err := auth.GenerateMusteri(ap_sir, mc, omur)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func gp_istek(t *testing.T, h http.Handler, metod, yetki, yol string, govde any) ap_yanit {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(metod, "/api/v1"+yol, bytes.NewReader(b))
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("Authorization", yetki)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	y := ap_yanit{kod: w.Code, ham: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &y.govde)
	return y
}

func gp_denetim(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	for i := 0; i < 20; i++ { // Denetim yazımı eşzamansız olabilir
		out = nil
		rows, err := db.Query(`SELECT CONCAT_WS('|', actor_username, COALESCE(target,''), COALESCE(CAST(detail AS CHAR),''), ok, reseller_id) FROM audit_log WHERE action='ftp.giris_parola'`)
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

func TestGirisParola_KokVeSahipBayi(t *testing.T) {
	db, h, s := gp_kur(t)
	eski, _ := ap_ftpHash(t, db, "c_bayi2")

	// bayi, KENDİ domaininde (SSH açık → eşitlenir)
	y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", map[string]string{"parola": apGuclu})
	if y.kod != 200 || y.govde["password"] != nil || y.govde["kullanici"] != "c_bayi2" {
		t.Fatalf("bayi sahibi: %d %s", y.kod, y.ham)
	}
	yeni, durum := ap_ftpHash(t, db, "c_bayi2")
	if yeni == eski || !hesaplar.FTPParolaDogrula(apGuclu, yeni) || durum != "active" {
		t.Fatal("ana FTP parolası $6$ olarak değişmeli")
	}
	if len(s.ssh) != 1 || s.ssh[0] != "c_bayi2" {
		t.Fatalf("SSH eşitlenmeli: %v", s.ssh)
	}
	// kök: sahibi bayi olan domainde de, sahibi kök olan domainde de (SSH kapalı → eşitleme yok)
	if y := gp_istek(t, h, "PUT", gp_tok(t, "admin", 1), "/domains/30/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("kök bayi domainine: %d %s", y.kod, y.ham)
	}
	if y := gp_istek(t, h, "PUT", gp_tok(t, "admin", 1), "/domains/40/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("kök kendi domainine: %d %s", y.kod, y.ham)
	}
	if len(s.ssh) != 1 {
		t.Fatalf("SSH kapalı hostingte eşitleme olmamalı: %v", s.ssh)
	}
}

func TestGirisParola_BayiBaskasininDomaini404(t *testing.T) {
	db, h, _ := gp_kur(t)
	onceki, _ := ap_ftpHash(t, db, "c_kurban")
	y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/30/giris-parola", map[string]string{"parola": apGuclu})
	yok := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/9999/giris-parola", map[string]string{"parola": apGuclu})
	if y.kod != 404 || yok.kod != 404 || y.ham != yok.ham {
		t.Fatalf("başkasının domaini = olmayan domain (404, aynı gövde): %d %q / %d %q", y.kod, y.ham, yok.kod, yok.ham)
	}
	if sonra, _ := ap_ftpHash(t, db, "c_kurban"); sonra != onceki {
		t.Fatal("başkasının parolası DEĞİŞMEMELİ")
	}
	// kök sahipli domain (reseller_id NULL) bayiye de 404
	if y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/40/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 404 {
		t.Fatalf("kök sahipli domain bayiye: %d", y.kod)
	}
}

func TestGirisParola_MusteriAdinaOturumReddedilir(t *testing.T) {
	db, h, _ := gp_kur(t)
	onceki, _ := ap_ftpHash(t, db, "c_bayi2")
	for ad, tok := range map[string]string{
		"müşteri":      gp_musteri(t, db, 20, false),
		"adına-oturum": gp_musteri(t, db, 20, true),
		"jetonsuz":     "",
		"bozuk jeton":  "Bearer abc",
	} {
		y := gp_istek(t, h, "PUT", tok, "/domains/20/giris-parola", map[string]string{"parola": apGuclu})
		if y.kod != 403 && y.kod != 401 {
			t.Errorf("%s reddedilmeli: %d %s", ad, y.kod, y.ham)
		}
	}
	if sonra, _ := ap_ftpHash(t, db, "c_bayi2"); sonra != onceki {
		t.Fatal("reddedilen isteklerde parola değişmemeli")
	}
	// mevcut müşteri kapısı (AdinaYasak) KORUNUR
	if y := gp_istek(t, h, "PUT", gp_musteri(t, db, 20, true), "/domains/20/ftp/password", map[string]string{"parola": apGuclu}); y.kod != 403 {
		t.Fatalf("adına-oturum ftp/password kapalı kalmalı: %d", y.kod)
	}
}

func TestGirisParola_GecersizParolaVeUretim(t *testing.T) {
	db, h, _ := gp_kur(t)
	onceki, _ := ap_ftpHash(t, db, "c_bayi2")
	for ad, pw := range map[string]string{
		"kısa": "Ab1", "harfsiz": "123456789012345", "rakamsız": "abcdefghijklmnop",
		"satır sonu": "Abcdefgh1234\nx", "uzun": strings.Repeat("a1", 70),
	} {
		if y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", map[string]string{"parola": pw}); y.kod != 400 {
			t.Errorf("%s: 400 beklenirdi, %d", ad, y.kod)
		}
	}
	if sonra, _ := ap_ftpHash(t, db, "c_bayi2"); sonra != onceki {
		t.Fatal("geçersiz parola yazılmamalı")
	}
	// boş gövde → sunucu üretir, bir kez döner, kurala uyar, no-store
	y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", nil)
	pw, _ := y.govde["password"].(string)
	if y.kod != 200 || y.govde["uretildi"] != true || len([]rune(pw)) < 16 {
		t.Fatalf("üretim: %d %s", y.kod, y.ham)
	}
	if ok, _ := hesaplar.ParolaGucluMu(pw); !ok {
		t.Fatal("üretilen parola panel kuralını geçmeli")
	}
	if h2, _ := ap_ftpHash(t, db, "c_bayi2"); !hesaplar.FTPParolaDogrula(pw, h2) {
		t.Fatal("üretilen parola yazılmış olmalı")
	}
}

func TestGirisParola_Demo_Aski_Oturumlar(t *testing.T) {
	db, h, _ := gp_kur(t)
	// demo reddedilir
	if y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/50/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 403 {
		t.Fatalf("demo: %d", y.kod)
	}
	// askıdaki hesapta çalışır, askı bozulmaz
	if y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/21/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("askıdaki hesap: %d %s", y.kod, y.ham)
	}
	if _, durum := ap_ftpHash(t, db, "c_askida"); durum != "suspended" {
		t.Fatalf("askı korunmalı: %s", durum)
	}
	// müşterinin AÇIK OTURUMU düşer
	tok := gp_musteri(t, db, 20, false)
	if y := gp_istek(t, h, "GET", tok, "/domains/20/ping", nil); y.kod != 200 {
		t.Fatalf("değişimden önce oturum geçerli olmalı: %d %s", y.kod, y.ham)
	}
	if y := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("değişim: %d", y.kod)
	}
	if y := gp_istek(t, h, "GET", tok, "/domains/20/ping", nil); y.kod != 401 {
		t.Fatalf("açık müşteri oturumu düşmeli (401): %d %s", y.kod, y.ham)
	}
}

func TestGirisParola_HizSiniri(t *testing.T) {
	_, h, _ := gp_kur(t)
	var son ap_yanit
	for i := 0; i < girisParolaMaks+1; i++ {
		son = gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", map[string]string{"parola": apGuclu})
		if i < girisParolaMaks && son.kod != 200 {
			t.Fatalf("istek %d: %d", i, son.kod)
		}
	}
	if son.kod != 429 {
		t.Fatalf("hesap başına hız sınırı (429): %d", son.kod)
	}
	// başka hesap etkilenmez
	if y := gp_istek(t, h, "PUT", gp_tok(t, "admin", 1), "/domains/40/giris-parola", map[string]string{"parola": apGuclu}); y.kod != 200 {
		t.Fatalf("başka hesap: %d", y.kod)
	}
}

func TestGirisParola_DenetimdeParolaYok(t *testing.T) {
	db, h, _ := gp_kur(t)
	const yazilan = "Denetimde-Gorunmemeli-77"
	gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", map[string]string{"parola": yazilan})
	uretilen := gp_istek(t, h, "PUT", gp_tok(t, "reseller", 2), "/domains/20/giris-parola", nil).govde["password"].(string)
	kayit := gp_denetim(t, db)
	if len(kayit) != 2 {
		t.Fatalf("2 denetim kaydı beklenirdi: %v", kayit)
	}
	for _, k := range kayit {
		if strings.Contains(k, yazilan) || strings.Contains(k, uretilen) || strings.Contains(k, "$6$") {
			t.Fatalf("🔴 denetimde parola/özet var: %s", k)
		}
		if !strings.Contains(k, "bayi") || !strings.Contains(k, "bayi2.test") || !strings.Contains(k, "kullanici=c_bayi2") {
			t.Fatalf("denetim kim/hangi hosting içermeli: %s", k)
		}
	}
}

func TestGirisParola_APIAnahtariErisemez(t *testing.T) {
	db, h, _ := gp_kur(t)
	onceki, _ := ap_ftpHash(t, db, "c_bayi2")
	k := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz", "hesap:yaz")
	y := gp_istek(t, h, "PUT", k.yetki(), "/domains/20/giris-parola", map[string]string{"parola": apGuclu})
	if y.kod != 403 {
		t.Fatalf("API anahtarı bu uca erişememeli: %d %s", y.kod, y.ham)
	}
	if sonra, _ := ap_ftpHash(t, db, "c_bayi2"); sonra != onceki {
		t.Fatal("anahtarla parola değişmemeli")
	}
}
