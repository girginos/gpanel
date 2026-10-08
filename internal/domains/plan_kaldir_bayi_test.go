package domains

// plan_kaldir_bayi_test.go — PUT /domains/{id}/plan {"plan_id":null}: bayi hostingini
// plansız (= sınırsız) bırakamaz; veritabanına hiç yazılmaz.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

func TestSetPlan_BayiPlaniKaldiramaz(t *testing.T) {
	s, db := sahtedb.Yeni()
	defer db.Close()
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(aa_sir))
		r.Put("/domains/{id}/plan", h.SetPlan)
	})
	req := httptest.NewRequest(http.MethodPut, "/domains/5/plan", strings.NewReader(`{"plan_id":null}`))
	req.Header.Set("Authorization", "Bearer "+aa_bayi(t, 7))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bayi plan_id=null 403 almalı: %d %s", rec.Code, rec.Body)
	}
	if s.Yurutuldu("UPDATE domains SET plan_id") {
		t.Fatal("bayi hostingi plansız bırakıldı (veritabanına yazıldı)")
	}
}
