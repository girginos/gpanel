package plans

// php_surum_test.go — plan oluştur/güncelle php_surum'u hosting yoluyla aynı kuralla
// doğrular (canlı API doğrulaması 2026-10-13: ham yazılıyordu, plan geçersiz sürümü yeni
// hostinglere taşıyordu). Boş değer varsayılan 8.3'tür.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

func TestPlan_OlusturGuncelle_PHPSurumDogrulanir(t *testing.T) {
	tok, err := auth.IssueAt(dh_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, uc := range []struct{ yontem, yol string }{{http.MethodPost, "/plans"}, {http.MethodPut, "/plans/3"}} {
		for _, v := range []string{"9.9", "php8.9", "8.30", "../x"} {
			sd, db := sahtedb.Yeni()
			h := &Handlers{DB: db}
			r := chi.NewRouter()
			r.With(middleware.RequireAuth(dh_sir)).Post("/plans", h.Create)
			r.With(middleware.RequireAuth(dh_sir)).Put("/plans/{id}", h.Update)
			req := httptest.NewRequest(uc.yontem, uc.yol, strings.NewReader(`{"ad":"p","php_surum":"`+v+`"}`))
			req.Header.Set("Authorization", "Bearer "+tok)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "desteklenen") {
				t.Errorf("%s %s php_surum=%q: 400 + desteklenen sürümler beklenirdi: %d %s", uc.yontem, uc.yol, v, rec.Code, rec.Body.String())
			}
			if sd.Yurutuldu("service_plans") {
				t.Errorf("%s php_surum=%q: reddedilen plan yazıldı", uc.yontem, v)
			}
		}
	}
}
