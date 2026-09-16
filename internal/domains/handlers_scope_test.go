package domains

// handlers_scope_test.go — domains katmaninda CROSS-TENANT IDOR regresyon kilidi.
//
// domains.Handlers.Get gercek uretim rotasindaki gibi RequireAuth + MusteriScope
// arkasina monte edilir ve BASKA bir musterinin domain id-siyle cagrilir.
// Beklenti: MusteriScope 403 dondurur ve GERCEK handler (h.Get, DB gerektirir)
// HIC CALISMAZ. Handler-in DB-si bilerek nil: 403 yolunda ona dokunulmadigi
// boylece KANITLANIR (dokunulsaydi nil-deref panic olurdu). DB gerekmez
// (sqlmock/MariaDB yok) — 403 karari saf JWT + kapsamdan gelir.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

var ds_secret = []byte("domains-scope-test-anahtari-0123456789xx")

func ds_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(ds_secret))
		r.With(middleware.MusteriScope).Get("/domains/{id}", h.Get)
	})
	return r
}

func ds_musteriToken(t *testing.T, fhid, domainID int64) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(ds_secret, auth.MusteriClaims{
		FTPHesapID: fhid, DomainID: domainID, Kullanici: "m", AlanAdi: "ornek.com",
	}, 3600)
	if err != nil {
		t.Fatalf("GenerateMusteri: %v", err)
	}
	return tok
}

// 🔴 Musteri (domain 42 sahibi) BASKA domainin (99) Get ucuna erisemez -> 403,
// ve h.Get cagrilmaz (DB nil olsa da panic yok).
func TestDomainGet_CaprazKiraciIDOR_403(t *testing.T) {
	h := &Handlers{DB: nil, IPv4: "203.0.113.10"} // DB nil: 403 yolunda kullanilmamali
	tok := ds_musteriToken(t, 5, 42)

	req := httptest.NewRequest(http.MethodGet, "/domains/99", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	ds_router(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("CROSS-TENANT IDOR: baska domaine 403 beklenir, alinan=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Kimlik dogrulamasiz istek de handler-a ulasmadan 401 alir (h.Get cagrilmaz).
func TestDomainGet_TokenYok_401(t *testing.T) {
	h := &Handlers{DB: nil, IPv4: "203.0.113.10"}
	req := httptest.NewRequest(http.MethodGet, "/domains/42", nil)
	rec := httptest.NewRecorder()
	ds_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token yok -> 401 beklenir, alinan=%d", rec.Code)
	}
}
