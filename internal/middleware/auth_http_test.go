package middleware

// auth_http_test.go — httptest tabanli UCTAN-UCA yetkilendirme testleri.
//
// Gercek chi router uzerinde RequireAuth + MusteriScope zinciri, uretimdeki
// /domains/{id} rotasiyla AYNI kurulur ve GERCEK imzali JWT-lerle surulur.
// scopeDB=nil (test) oldugundan sunucu-tarafli iptal kontrolleri (oturum/askii)
// fail-open olur; boylece saf JWT-cozumleme + kapsam davranisi DB OLMADAN test
// edilir (sqlmock/MariaDB gerekmez). Fail-CLOSED iptal mantigi ayrica
// oturum_cache_test.go / domain_aski_test.go-da (stub sorgu) kanitlanir.
//
// ONCELIK: cross-tenant (IDOR) ve auth negatif kontrolleri — kirik/sizintili bir
// surumun musteriye gitmesine karsi regresyon kalkani.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

var mwSecret = []byte("mw-http-test-jwt-anahtari-0123456789-abc")

func mwOK(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// mwRouter: uretimdeki desenin aynisi — korumali /domains/{id} (RequireAuth +
// MusteriScope) ve auth-disi /healthz.
func mwRouter() http.Handler {
	scopeDB = nil // DB-siz yol: iptal kontrolleri fail-open
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(RequireAuth(mwSecret))
		r.With(MusteriScope).Get("/domains/{id}", mwOK)
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"durum": "ayakta"})
	})
	return r
}

func mwMusteriToken(t *testing.T, fhid, domainID int64, secret []byte) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(secret, auth.MusteriClaims{
		FTPHesapID: fhid, DomainID: domainID, Kullanici: "m", AlanAdi: "ornek.com",
	}, 3600)
	if err != nil {
		t.Fatalf("GenerateMusteri: %v", err)
	}
	return tok
}

func mwDo(h http.Handler, method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---- AUTH: gecerli JWT -> 200 -------------------------------------------------

// Gecerli musteri JWT-i KENDI domainine -> 200 (token cozuldu, kapsam gecti).
func TestRequireAuth_GecerliMusteriToken_200(t *testing.T) {
	h := mwRouter()
	tok := mwMusteriToken(t, 5, 42, mwSecret)
	rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("gecerli token 200 vermeli, alinan=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- AUTH: JWT yok / bozuk -> 401 --------------------------------------------

func TestRequireAuth_TokenYok_401(t *testing.T) {
	rec := mwDo(mwRouter(), http.MethodGet, "/domains/42", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token yok -> 401 beklenir, alinan=%d", rec.Code)
	}
}

func TestRequireAuth_BozukToken_401(t *testing.T) {
	rec := mwDo(mwRouter(), http.MethodGet, "/domains/42", "Bearer bu.gecerli.bir.jwt.degil")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bozuk token -> 401 beklenir, alinan=%d", rec.Code)
	}
}

// Dogru yapida ama YANLIS ANAHTARLA imzalanmis token -> 401 (imza dogrulaniyor).
func TestRequireAuth_YanlisImza_401(t *testing.T) {
	yabanci := mwMusteriToken(t, 5, 42, []byte("baska-tamamen-farkli-anahtar-0987654321"))
	rec := mwDo(mwRouter(), http.MethodGet, "/domains/42", "Bearer "+yabanci)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yanlis imza -> 401 beklenir, alinan=%d", rec.Code)
	}
}

// Bearer disi sema -> 401.
func TestRequireAuth_BearerDisiSema_401(t *testing.T) {
	rec := mwDo(mwRouter(), http.MethodGet, "/domains/42", "Basic YWxhZGRpbjpvcGVu")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Bearer-disi -> 401 beklenir, alinan=%d", rec.Code)
	}
}

// Asiri-uzun token dogrulama ONCESI reddedilir (CVE-2025-30204 pre-auth DoS savunmasi).
func TestRequireAuth_AsiriUzunToken_401(t *testing.T) {
	rec := mwDo(mwRouter(), http.MethodGet, "/domains/42", "Bearer "+strings.Repeat("a", 9000))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("asiri-uzun token -> 401 beklenir, alinan=%d", rec.Code)
	}
}

// ---- AUTHZ (MusteriScope): cross-tenant IDOR ---------------------------------

// 🔴 ONCELIKLI REGRESYON KILIDI: musteri KENDI domainine erisebilir...
func TestMusteriScope_KendiDomain_200(t *testing.T) {
	h := mwRouter()
	tok := mwMusteriToken(t, 5, 42, mwSecret)
	rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("kendi domain -> 200 beklenir, alinan=%d body=%s", rec.Code, rec.Body.String())
	}
}

// 🔴 ONCELIKLI REGRESYON KILIDI: ...ama BASKA musterinin domainine ASLA (403).
// Cross-tenant IDOR negatif testi.
func TestMusteriScope_CaprazKiraciIDOR_403(t *testing.T) {
	h := mwRouter()
	tok := mwMusteriToken(t, 5, 42, mwSecret)                    // musteri 42 numarali domainin sahibi
	rec := mwDo(h, http.MethodGet, "/domains/99", "Bearer "+tok) // 99 BASKASININ
	if rec.Code != http.StatusForbidden {
		t.Fatalf("CROSS-TENANT IDOR: baska domaine 403 beklenir, alinan=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Admin token kapsamdan MUAF (tum domainlere erisir). MusteriScope-un admin dali
// izole test edilir: RequireAuth admin yolu IdleMi->DB gerektirdiginden, admin
// claim-i dogrudan context-e enjekte edilir (ayni paket, white-box).
func TestMusteriScope_AdminSerbest_200(t *testing.T) {
	scopeDB = nil
	req := httptest.NewRequest(http.MethodGet, "/domains/99", nil)
	req = mwWithChiParam(req, "id", "99")
	req = req.WithContext(context.WithValue(req.Context(), claimsKey,
		&auth.Claims{UserID: 1, Username: "root", Role: "admin"}))
	rec := httptest.NewRecorder()
	MusteriScope(http.HandlerFunc(mwOK)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin herhangi bir domaine erisebilmeli (200), alinan=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Kimlik hic yoksa (context bos) MusteriScope 401 verir (savunma katmani).
func TestMusteriScope_KimlikYok_401(t *testing.T) {
	scopeDB = nil
	req := httptest.NewRequest(http.MethodGet, "/domains/42", nil)
	req = mwWithChiParam(req, "id", "42")
	rec := httptest.NewRecorder()
	MusteriScope(http.HandlerFunc(mwOK)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("kimliksiz -> 401 beklenir, alinan=%d", rec.Code)
	}
}

// ---- HEALTH: auth-disi basit GET -> 200 --------------------------------------

func TestHealthz_BasitGet_200(t *testing.T) {
	rec := mwDo(mwRouter(), http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz 200 vermeli, alinan=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ayakta") {
		t.Fatalf("/healthz govdesi beklenmeyen: %s", rec.Body.String())
	}
}

// mwWithChiParam: chi URL param-ini istek context-ine enjekte eder (router-suz test).
func mwWithChiParam(r *http.Request, k, v string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(k, v)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
