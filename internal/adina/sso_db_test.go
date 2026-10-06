package adina

// sso_db_test.go — POST /api/v1/domains/{id}/sso uçtan uca (WHMCS müşteri tek tık
// girişi): gerçek chi (/api/v1 altında, RoutePattern gerçek), RequireAuth + gerçek
// API anahtarı doğrulaması (gerçek 0128 göçü) + varsayılan-reddet izin tablosu +
// adına-oturum tüketicisi + geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

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

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

func ssoGocUygula(t *testing.T, db *sql.DB) {
	t.Helper()
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
}

// ssoKurulum: DB + router + sıfırlanmış paket durumu.
func ssoKurulum(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	db := adGeciciDB(t)
	ssoGocUygula(t, db)
	eskiHiz, eskiSimdi := ssoHizDk, simdiFn
	ssoMu.Lock()
	ssoKovasi = map[int64][]time.Time{}
	ssoMu.Unlock()
	kodMu.Lock()
	kodlar = map[string]kodKaydi{}
	kodMu.Unlock()
	t.Cleanup(func() { ssoHizDk, simdiFn = eskiHiz, eskiSimdi })

	h := &Handlers{DB: db, Secret: adSecret,
		SSOAdresi: func(*http.Request) (string, bool) { return "https://panel.test:9443", true }}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/musteri/adina-giris", h.Giris)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(adSecret))
			r.Post("/musteri/adina-cikis", h.Cikis)
			r.With(middleware.AdminVeyaReseller).Post("/domains/{id}/sso", h.SSO)
			r.With(middleware.MusteriScope).Get("/domains/{id}", adOK)
			r.With(middleware.MusteriScope, middleware.AdinaYasak).Put("/domains/{id}/ftp/password", adOK)
		})
	})
	return db, r
}

// ssoAnahtar: sahibi için API anahtarı ekler → "gosp <kullanıcı>:<anahtar>" başlık değeri.
func ssoAnahtar(t *testing.T, db *sql.DB, sahipID int64, rol, ad string, izinler ...string) string {
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
	if sahipID == 8 {
		kul = "bayi_b"
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

func ssoIstek(t *testing.T, h http.Handler, yol, yetki string, govde string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, yol, bytes.NewReader([]byte(govde)))
	req.RemoteAddr = "192.0.2.10:40000"
	if yetki != "" {
		req.Header.Set("Authorization", yetki)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ssoKod: yanıttan URL'yi çözer, kodu çıkarır.
func ssoKod(t *testing.T, rec *httptest.ResponseRecorder) (kod, tamURL string) {
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
	if err != nil || u.Scheme != "https" || u.Host != "panel.test:9443" || u.Path != "/adina" || u.Query().Has("kod") {
		t.Fatalf("url biçimi: %q", y.URL)
	}
	p, _ := url.ParseQuery(u.Fragment)
	return p.Get("kod"), y.URL
}

func ssoTuket(t *testing.T, h http.Handler, kod string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"kod": kod})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/musteri/adina-giris", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var y struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	return rec.Code, y.Token
}

func ssoGet(t *testing.T, h http.Handler, method, yol, tok string) int {
	t.Helper()
	req := httptest.NewRequest(method, yol, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestSSO_IzinYoksa403_VarsayilanKapali(t *testing.T) {
	db, h := ssoKurulum(t)
	// hesap:yaz tek başına SSO açmaz; sso izni ayrı.
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "WHMCS", "hesap:yaz", "denetim:oku")
	if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("sso izni yok → 403 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if len(kodlar) != 0 {
		t.Fatal("izinsiz istek kod üretti")
	}
	// Anahtarsız / JWT ile: yalnız API anahtarı.
	if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("kimliksiz 401: %d", rec.Code)
	}
	if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", "Bearer "+bayiTok(t, bayiA), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("JWT oturumuyla 403 beklenir: %d", rec.Code)
	}
	// Katalog: sso tek düzey ("yaz"), "sso:oku" reddedilir.
	if _, err := apianahtar.IzinleriKanonik([]string{"sso:oku"}, apianahtar.RolBayi); err == nil {
		t.Fatal("sso:oku kabul edilmemeli")
	}
	if got, err := apianahtar.IzinleriKanonik([]string{"sso:yaz"}, apianahtar.RolBayi); err != nil || len(got) != 1 {
		t.Fatalf("sso:yaz kabul edilmeli: %v %v", got, err)
	}
}

func TestSSO_TamAkis_KodTuketilir_TekKullanimlik_Denetim_Sizinti(t *testing.T) {
	db, h := ssoKurulum(t)
	var logTampon bytes.Buffer
	log.SetOutput(&logTampon)
	defer log.SetOutput(os.Stderr)

	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "WHMCS-A", "sso:yaz")
	rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("200 beklenir: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Cache-Control: no-store olmalı")
	}
	kod, tamURL := ssoKod(t, rec)
	// Gövde YALNIZ ok/url/gecerlilik_sn; kod yalnız URL parçasında.
	var alanlar map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &alanlar)
	if len(alanlar) != 3 || strings.Count(rec.Body.String(), kod) != 1 || !strings.Contains(tamURL, "#kod="+kod) {
		t.Fatalf("yanıt gövdesi sızıntı/biçim: %s", rec.Body.String())
	}
	if len(kod) < 32 {
		t.Fatalf("kod kısa: %d", len(kod))
	}

	// Kod GERÇEKTEN tüketilebilir → müşteri-adına token.
	g, tok := ssoTuket(t, h, kod)
	if g != http.StatusOK || tok == "" {
		t.Fatalf("kod tüketilemedi: %d", g)
	}
	mc, err := auth.ParseMusteri(adSecret, tok)
	if err != nil || mc.DomainID != domA || !mc.SSO || !mc.Adina() || mc.AktorUID != bayiA || mc.AktorRol != "reseller" {
		t.Fatalf("token claim'leri: %+v %v", mc, err)
	}
	if ssoGet(t, h, http.MethodGet, "/api/v1/domains/42", tok) != http.StatusOK {
		t.Fatal("müşteri kendi domaini 200 beklenir")
	}
	if ssoGet(t, h, http.MethodGet, "/api/v1/domains/43", tok) != http.StatusForbidden {
		t.Fatal("başka domain 403 beklenir")
	}
	// Müşteri parolası/kimlik bu oturumla değişmez.
	if c := ssoGet(t, h, http.MethodPut, "/api/v1/domains/42/ftp/password", tok); c != http.StatusForbidden {
		t.Fatalf("SSO oturumunda parola değişimi 403 beklenir: %d", c)
	}
	// İkinci kullanım reddedilir.
	if g, _ := ssoTuket(t, h, kod); g != http.StatusUnauthorized {
		t.Fatalf("ikinci kullanım 401 beklenir: %d", g)
	}

	// Denetim: üretim kaydı anahtar adıyla, kodsuz; oturum kaydı var.
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='musteri.sso_kod' AND actor_username='bayi_a (API anahtarı WHMCS-A)'
		AND actor_user_id=7 AND target='a-musteri.test' AND reseller_id=7 AND ok=1`).Scan(&n)
	if n != 1 {
		t.Fatalf("musteri.sso_kod denetim kaydı yok: %d", n)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='musteri.adina_oturum' AND adina_domain_id=42
		AND detail LIKE '%WHMCS-A%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("oturum denetim kaydı (anahtar adıyla) yok: %d", n)
	}
	// SIZINTI: kod ne denetimde ne log'da, ne anahtar gizlisi.
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE CONCAT_WS('|',actor_username,target,COALESCE(detail,''),COALESCE(ip,'')) LIKE ?`, "%"+kod+"%").Scan(&n)
	if n != 0 {
		t.Fatal("kod denetim kaydında")
	}
	if strings.Contains(logTampon.String(), kod) || strings.Contains(logTampon.String(), "adina#") {
		t.Fatalf("kod/URL log'a yazılmış: %s", logTampon.String())
	}
	gizli := k[strings.LastIndex(k, "_")+1:]
	if strings.Contains(logTampon.String(), gizli) || strings.Contains(rec.Body.String(), gizli) {
		t.Fatal("anahtar gizlisi sızdı")
	}
}

func TestSSO_KodAltmisSaniyedeGecersiz(t *testing.T) {
	db, h := ssoKurulum(t)
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	kod, _ := ssoKod(t, ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""))
	simdiFn = func() time.Time { return time.Now().Add(KodOmru + time.Second) }
	if g, _ := ssoTuket(t, h, kod); g != http.StatusUnauthorized {
		t.Fatalf("60 sn sonra 401 beklenir: %d", g)
	}
	// Süre dolmadan: 59 sn → geçerli.
	simdiFn = time.Now
	k2, _ := ssoKod(t, ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""))
	simdiFn = func() time.Time { return time.Now().Add(KodOmru - 3*time.Second) }
	if g, _ := ssoTuket(t, h, k2); g != http.StatusOK {
		t.Fatalf("süre içinde 200 beklenir: %d", g)
	}
}

func TestSSO_KapsamIDOR_404_VarlikSizdirmaYok(t *testing.T) {
	db, h := ssoKurulum(t)
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	baska := ssoIstek(t, h, "/api/v1/domains/43/sso", k, "") // bayi B'nin domaini
	yok := ssoIstek(t, h, "/api/v1/domains/9999/sso", k, "")
	sacma := ssoIstek(t, h, "/api/v1/domains/abc/sso", k, "")
	for ad, rec := range map[string]*httptest.ResponseRecorder{"başka bayi": baska, "yok": yok, "geçersiz": sacma} {
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: 404 beklenir: %d %s", ad, rec.Code, rec.Body.String())
		}
	}
	if baska.Body.String() != yok.Body.String() {
		t.Fatalf("404 gövdeleri farklı (varlık sızıntısı): %q vs %q", baska.Body.String(), yok.Body.String())
	}
	if len(kodlar) != 0 {
		t.Fatal("kapsam dışı istek kod üretti")
	}
	// Kök anahtarı her domaine; olmayana 404.
	kok := ssoAnahtar(t, db, 1, apianahtar.RolKok, "KOK", "sso:yaz")
	if rec := ssoIstek(t, h, "/api/v1/domains/43/sso", kok, ""); rec.Code != http.StatusOK {
		t.Fatalf("kök anahtarı herhangi domain: %d %s", rec.Code, rec.Body.String())
	}
	if rec := ssoIstek(t, h, "/api/v1/domains/9999/sso", kok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("kök olmayan domain 404: %d", rec.Code)
	}
}

func TestSSO_AskidaHesap409_VeAcikOturumAskidaDuser(t *testing.T) {
	db, h := ssoKurulum(t)
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	// Askıda → 409 (her askı kaynağı).
	for _, kaynak := range []string{"bayi_el", "manuel", "bayi"} {
		if _, err := db.Exec(`UPDATE domains SET askida=1, askida_kaynak=? WHERE id=42`, kaynak); err != nil {
			t.Fatal(err)
		}
		rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, "")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "askıda") {
			t.Fatalf("askıda (%s) 409 + net mesaj beklenir: %d %s", kaynak, rec.Code, rec.Body.String())
		}
	}
	if len(kodlar) != 0 {
		t.Fatal("askıdaki hesap için kod üretildi")
	}
	// Kod üretildikten SONRA askıya alınırsa tüketim reddedilir.
	_, _ = db.Exec(`UPDATE domains SET askida=0, askida_kaynak=NULL WHERE id=42`)
	kod, _ := ssoKod(t, ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""))
	_, _ = db.Exec(`UPDATE domains SET askida=1, askida_kaynak='bayi_el' WHERE id=42`)
	if g, tok := ssoTuket(t, h, kod); g == http.StatusOK || tok != "" {
		t.Fatalf("üretimden sonra askıya alınan hesaba giriş: %d", g)
	}
	// Açık SSO oturumu da askıda düşer.
	_, _ = db.Exec(`UPDATE domains SET askida=0, askida_kaynak=NULL WHERE id=42`)
	kod, _ = ssoKod(t, ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""))
	_, tok := ssoTuket(t, h, kod)
	if ssoGet(t, h, http.MethodGet, "/api/v1/domains/42", tok) != http.StatusOK {
		t.Fatal("askı öncesi oturum 200")
	}
	_, _ = db.Exec(`UPDATE domains SET askida=1, askida_kaynak='bayi_el' WHERE id=42`)
	if c := ssoGet(t, h, http.MethodGet, "/api/v1/domains/42", tok); c != http.StatusForbidden {
		t.Fatalf("SSO oturumu askıda 403 beklenir: %d", c)
	}
}

func TestSSO_HizSiniri429(t *testing.T) {
	db, h := ssoKurulum(t)
	ssoHizDk = 3
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	k2 := ssoAnahtar(t, db, bayiB, apianahtar.RolBayi, "W2", "sso:yaz")
	for i := 0; i < 3; i++ {
		if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""); rec.Code != http.StatusOK {
			t.Fatalf("istek %d: %d", i, rec.Code)
		}
	}
	rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("4. istek 429 + Retry-After: %d %v", rec.Code, rec.Header())
	}
	// Anahtar başına: başka anahtar etkilenmez.
	if rec := ssoIstek(t, h, "/api/v1/domains/43/sso", k2, ""); rec.Code != http.StatusOK {
		t.Fatalf("başka anahtar etkilenmemeli: %d", rec.Code)
	}
}

func TestSSO_GecersizGovde400_BosVeBosObjeKabul(t *testing.T) {
	db, h := ssoKurulum(t)
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	for _, g := range []string{"", "{}", " {} "} {
		if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, g); rec.Code != http.StatusOK {
			t.Fatalf("gövde %q 200 beklenir: %d", g, rec.Code)
		}
	}
	if rec := ssoIstek(t, h, "/api/v1/domains/42/sso", k, "{bozuk"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk gövde 400: %d", rec.Code)
	}
}

func TestSSO_AdresCozulemezse503_KodUretilmez(t *testing.T) {
	db, h := ssoKurulum(t)
	_ = h
	hh := &Handlers{DB: db, Secret: adSecret, SSOAdresi: func(*http.Request) (string, bool) { return "", false }}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(adSecret))
			r.With(middleware.AdminVeyaReseller).Post("/domains/{id}/sso", hh.SSO)
		})
	})
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	if rec := ssoIstek(t, r, "/api/v1/domains/42/sso", k, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("503 beklenir: %d", rec.Code)
	}
	if len(kodlar) != 0 {
		t.Fatal("adres çözülemezken kod üretildi")
	}
	_ = fmt.Sprint
}

// Giris yanıtı SSO bayrağını taşır (ön yüz şeridi/dönüş düğmesini buna göre gizler);
// bayinin olağan "panele gir" akışında false kalır.
func TestSSO_GirisYanitiBayragiTasir(t *testing.T) {
	db, h := ssoKurulum(t)
	k := ssoAnahtar(t, db, bayiA, apianahtar.RolBayi, "W", "sso:yaz")
	kod, _ := ssoKod(t, ssoIstek(t, h, "/api/v1/domains/42/sso", k, ""))
	b, _ := json.Marshal(map[string]string{"kod": kod})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/musteri/adina-giris", bytes.NewReader(b)))
	var y struct {
		SSO bool `json:"sso"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	if rec.Code != 200 || !y.SSO {
		t.Fatalf("SSO girişi yanıtında sso=true beklenir: %d %s", rec.Code, rec.Body.String())
	}
	// Olağan adına-oturum (bayi): sso=false.
	adh := adRouter(db)
	_, k2 := kodAl(t, adh, bayiTok(t, bayiA), domA)
	rec = adIstek(t, adh, http.MethodPost, "/musteri/adina-giris", "", map[string]string{"kod": k2})
	y.SSO = true
	_ = json.Unmarshal(rec.Body.Bytes(), &y)
	if rec.Code != 200 || y.SSO {
		t.Fatalf("olağan adına-oturumda sso=false beklenir: %d %s", rec.Code, rec.Body.String())
	}
}
