package middleware

// destek_test.go — destek botunun süreç içi kimliği (DB'siz; dikişlerle). Gerçek
// chi yönlendiricisi main.go ile aynı biçimde: Route("/api/v1") + Group(RequireAuth).
//
// Kanıtlanan: kimlik YALNIZ context işaretinden kurulur (başlık taklidi 401),
// varsayılan-red izin listesi (tablo dışı GET ve her yazma 403), rol → Claims
// eşlemesi (bayi rid = kendi id), bayi/kök hesabı her istekte doğrulanır
// (askı 403, rol uyuşmazlığı 401, DB hatası 503), denetim aktörü biçimi.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"

	"github.com/go-chi/chi/v5"
)

type dsSahne struct {
	claims   *auth.Claims
	mc       *auth.MusteriClaims
	desen    string
	aktor    string
	cagrildi bool
}

func dsKur(t *testing.T, hesap func(context.Context, int64) (string, string, error)) (*dsSahne, http.Handler) {
	t.Helper()
	eski, eskiDB, eskiKapi := destekHesapSorgu, scopeDB, kapiCoz
	destekHesapSorgu = hesap
	scopeDB = nil
	kapiCoz = func(*http.Request) string { return "" }
	t.Cleanup(func() { destekHesapSorgu, scopeDB, kapiCoz = eski, eskiDB, eskiKapi })
	s := &dsSahne{}
	yakala := func(w http.ResponseWriter, r *http.Request) {
		s.cagrildi = true
		s.claims = ClaimsFrom(r)
		s.mc = MusteriClaimsFrom(r)
		s.desen = chi.RouteContext(r.Context()).RoutePattern()
		_, s.aktor = Aktor(r)
		w.WriteHeader(http.StatusOK)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(RequireAuth(mwSecret))
			r.Get("/me", yakala)
			r.With(AdminVeyaReseller).Get("/domains", yakala)
			r.With(MusteriScope).Get("/domains/{id}", yakala)
			r.With(MusteriScope).Delete("/domains/{id}", yakala)
			r.With(MusteriScope).Get("/domains/{id}/kaynak", yakala)
			r.With(MusteriScope).Post("/domains/{id}/cron", yakala)
			r.With(MusteriScope).Get("/domains/{id}/files/oku", yakala)
			r.With(AdminOnly).Get("/admin/system/loglar", yakala)
			// Rol ara yazılımı OLMAYAN izinli uç: hesap doğrulamasını tek başına ölçer
			// (AdminOnly/MusteriScope'un 403'ü askı kararını örtmesin).
			r.Get("/domains/{id}/dns", yakala)
		})
	})
	return s, r
}

func dsHesap(rol, durum string) func(context.Context, int64) (string, string, error) {
	return func(context.Context, int64) (string, string, error) { return rol, durum, nil }
}

func dsIstek(h http.Handler, metod, yol string, dk *DestekKimlik, basliklar ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metod, yol, nil)
	if dk != nil {
		req = req.WithContext(DestekBagla(req.Context(), *dk))
	}
	for i := 0; i+1 < len(basliklar); i += 2 {
		req.Header.Set(basliklar[i], basliklar[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestDestekBaslikTaklidiKimlikKuramaz(t *testing.T) {
	s, h := dsKur(t, dsHesap("admin", "active"))
	for _, b := range [][]string{
		{"X-Destek-Kimlik", "kok"},
		{"X-Destek", "1", "X-Destek-Rol", "kok", "X-Destek-Kullanici", "1"},
		{"Authorization", "destek kok:1"},
		{"Authorization", "Bearer destek-bot"},
	} {
		s.cagrildi = false
		w := dsIstek(h, "GET", "/api/v1/domains/5", nil, b...)
		if w.Code != http.StatusUnauthorized || s.cagrildi {
			t.Fatalf("başlık taklidi (%v) kimlik kurdu: %d", b, w.Code)
		}
	}
}

func TestDestekKimlikRolEslemesi(t *testing.T) {
	s, h := dsKur(t, dsHesap("admin", "active"))
	w := dsIstek(h, "GET", "/api/v1/domains/42", &DestekKimlik{Rol: DestekMusteri, DomainID: 42, AlanAdi: "a.com", TgKisa: "k1"})
	if w.Code != 200 || s.mc == nil || s.mc.DomainID != 42 || s.claims != nil || s.mc.Adina() {
		t.Fatalf("müşteri kimliği: %d mc=%+v c=%+v", w.Code, s.mc, s.claims)
	}
	if s.aktor != "destek-bot (tg:k1)" {
		t.Fatalf("aktör: %q", s.aktor)
	}
	if s.desen != "/api/v1/domains/{id}" {
		t.Fatalf("RoutePattern biçimi: %q", s.desen)
	}
	if _, ok := DestekRotalari["GET "+s.desen]; !ok {
		t.Fatal("ölçülen desen izin tablosunda yok")
	}
	// Müşteri başka domaine MusteriScope'tan geçemez (ikinci katman).
	s.cagrildi = false
	if w := dsIstek(h, "GET", "/api/v1/domains/43", &DestekKimlik{Rol: DestekMusteri, DomainID: 42, TgKisa: "k1"}); w.Code != 403 || s.cagrildi {
		t.Fatalf("müşteri başka domain: %d", w.Code)
	}

	destekHesapSorgu = dsHesap("reseller", "active")
	w = dsIstek(h, "GET", "/api/v1/domains", &DestekKimlik{Rol: DestekBayi, KullaniciID: 7, Kullanici: "bayi7", TgKisa: "k2"})
	if w.Code != 403 {
		t.Fatalf("tablo dışı uç (GET /domains) destek kimliğine açık: %d", w.Code)
	}
	scopeDB = nil // resellerDomainSahibi → false: bayi kapsamı DB olmadan geçmez
	s.cagrildi = false
	if w := dsIstek(h, "GET", "/api/v1/domains/42", &DestekKimlik{Rol: DestekBayi, KullaniciID: 7, Kullanici: "bayi7", TgKisa: "k2"}); w.Code != 403 || s.cagrildi {
		t.Fatalf("bayi sahipliği doğrulanmadan geçti: %d", w.Code)
	}

	destekHesapSorgu = dsHesap("admin", "active")
	w = dsIstek(h, "GET", "/api/v1/admin/system/loglar", &DestekKimlik{Rol: DestekKok, KullaniciID: 1, TgKisa: "k3"})
	if w.Code != 200 || s.claims == nil || s.claims.Role != "admin" || s.claims.UserID != 1 || s.claims.Username != "root" {
		t.Fatalf("kök kimliği: %d %+v", w.Code, s.claims)
	}
	if s.aktor != "destek-bot (tg:k3)" {
		t.Fatalf("kök aktör: %q", s.aktor)
	}
	// Bayi kimliği rid = kendi id (kök ucunda AdminOnly reddeder).
	destekHesapSorgu = dsHesap("reseller", "active")
	if w := dsIstek(h, "GET", "/api/v1/admin/system/loglar", &DestekKimlik{Rol: DestekBayi, KullaniciID: 7, TgKisa: "k2"}); w.Code != 403 {
		t.Fatalf("bayi kök ucuna geçti: %d", w.Code)
	}
	// Müşteri kök ucuna: AdminOnly.
	if w := dsIstek(h, "GET", "/api/v1/admin/system/loglar", &DestekKimlik{Rol: DestekMusteri, DomainID: 42}); w.Code != 403 {
		t.Fatalf("müşteri kök ucuna geçti: %d", w.Code)
	}
}

func TestDestekVarsayilanRed(t *testing.T) {
	s, h := dsKur(t, dsHesap("admin", "active"))
	kok := &DestekKimlik{Rol: DestekKok, KullaniciID: 1, TgKisa: "k"}
	for _, c := range []struct{ metod, yol string }{
		{"DELETE", "/api/v1/domains/42"},        // yazma
		{"POST", "/api/v1/domains/42/cron"},     // yazma
		{"GET", "/api/v1/me"},                   // kimlik ucu, tabloda yok
		{"GET", "/api/v1/domains/42/files/oku"}, // keyfi dosya okuma (R3)
	} {
		s.cagrildi = false
		w := dsIstek(h, c.metod, c.yol, kok)
		if w.Code != http.StatusForbidden || s.cagrildi {
			t.Fatalf("%s %s destek kimliğine açık: %d", c.metod, c.yol, w.Code)
		}
	}
	if w := dsIstek(h, "GET", "/api/v1/domains/42/kaynak", kok); w.Code != 200 {
		t.Fatalf("izinli uç: %d", w.Code)
	}
}

func TestDestekHesapDogrulama(t *testing.T) {
	s, h := dsKur(t, nil)
	bayi := &DestekKimlik{Rol: DestekBayi, KullaniciID: 7, TgKisa: "k"}
	kok := &DestekKimlik{Rol: DestekKok, KullaniciID: 1, TgKisa: "k"}
	cases := []struct {
		ad    string
		dk    *DestekKimlik
		hesap func(context.Context, int64) (string, string, error)
		kod   int
	}{
		{"bayi askıda", bayi, dsHesap("reseller", "suspended"), 403},
		{"bayi rolü admin", bayi, dsHesap("admin", "active"), 401},
		{"kök rolü bayi", kok, dsHesap("reseller", "active"), 401},
		{"hesap yok", kok, func(context.Context, int64) (string, string, error) { return "", "", sql.ErrNoRows }, 401},
		{"DB hatası", kok, func(context.Context, int64) (string, string, error) { return "", "", errors.New("bağlantı") }, 503},
		{"bayi uid yok", &DestekKimlik{Rol: DestekBayi, TgKisa: "k"}, dsHesap("reseller", "active"), 401},
		{"müşteri domain yok", &DestekKimlik{Rol: DestekMusteri, TgKisa: "k"}, dsHesap("admin", "active"), 401},
		{"bilinmeyen rol", &DestekKimlik{Rol: "admin", KullaniciID: 1}, dsHesap("admin", "active"), 401},
	}
	destekHesapSorgu = dsHesap("reseller", "active")
	if w := dsIstek(h, "GET", "/api/v1/domains/42/dns", bayi); w.Code != 200 || !s.cagrildi || s.claims.ResellerID != 7 {
		t.Fatalf("etkin bayi (taban): %d %+v", w.Code, s.claims)
	}
	for _, c := range cases {
		destekHesapSorgu = c.hesap
		s.cagrildi = false
		w := dsIstek(h, "GET", "/api/v1/domains/42/dns", c.dk)
		if w.Code != c.kod || s.cagrildi {
			t.Fatalf("%s: %d (beklenen %d) çağrıldı=%v", c.ad, w.Code, c.kod, s.cagrildi)
		}
	}
}

func TestDestekRotalariYalnizOkuma(t *testing.T) {
	if len(DestekRotalari) == 0 {
		t.Fatal("boş tablo")
	}
	for d, arac := range DestekRotalari {
		if !strings.HasPrefix(d, "GET /api/v1/") {
			t.Errorf("destek tablosunda okuma dışı/yanlış önekli uç: %s", d)
		}
		if apianahtar.KimlikUclari[d] {
			t.Errorf("kimlik ucu destek tablosunda: %s", d)
		}
		if strings.TrimSpace(arac) == "" {
			t.Errorf("gerekçesiz girdi: %s", d)
		}
		for _, yasak := range []string{"/indir", "/icerik", "/files", "/env", "/parola", "/password", "/token", "/pma", "/adina"} {
			if strings.Contains(d, yasak) {
				t.Errorf("R3 (keyfi dosya/sır/kimlik) uç destek tablosunda: %s", d)
			}
		}
	}
}

func TestDestekAktorAdiBicimi(t *testing.T) {
	if got := DestekAktorAdi("ab_C-9"); got != "destek-bot (tg:ab_C-9)" {
		t.Fatal(got)
	}
	for _, kotu := range []string{"", "a b", "x)\n(adina", strings.Repeat("a", 25)} {
		if got := DestekAktorAdi(kotu); got != "destek-bot (tg:?)" {
			t.Fatalf("%q → %q", kotu, got)
		}
	}
}
