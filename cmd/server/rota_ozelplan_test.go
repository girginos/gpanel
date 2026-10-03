package main

// rota_ozelplan_test.go — D1 hostinge özel limit uçları SahipYonetim arkasında:
// bayi yalnız kendi hostingi (scopeDB yokken hiçbiri), müşteri asla; yönetici
// handler'a ulaşır. main.go kaydı kimlik doğrulamalı grubun içinde ve eski
// /ozel-plan ucu duruyor (410 döner — önbellekteki eski arayüz için).

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/domains"
	"girginospanel/internal/middleware"
)

func TestOzelPlanRotalari_SahipYonetimArkasinda(t *testing.T) {
	sir := []byte("rota-ozelplan-test-anahtari-0123456789abcd")
	middleware.Init(nil) // kapsam DB'si yok: bayi hiçbir hostingin sahibi sayılmaz
	// Ulaşılamaz DB: handler'a ulaşan istek 500 döner (kapı atlanırsa görünür).
	db, err := sql.Open("mysql", "x:y@tcp(127.0.0.1:1)/yok?timeout=200ms")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(sir))
		ozelPlanRotalari(r, &domains.Handlers{DB: db})
	})
	admin, _ := auth.Issue(sir, 600, 1, "root", "admin")
	bayi, _ := auth.IssueReseller(sir, 600, 7, "bayi", 7)
	for _, yon := range []string{"GET", "PUT", "DELETE"} {
		for _, c := range []struct {
			kim, tok string
			bek      int
		}{{"bayi", bayi, http.StatusForbidden}, {"admin", admin, http.StatusInternalServerError}, {"anonim", "", http.StatusUnauthorized}} {
			req := httptest.NewRequest(yon, "/domains/5/limitler", strings.NewReader(`{"ust":{}}`))
			if c.tok != "" {
				req.Header.Set("Authorization", "Bearer "+c.tok)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != c.bek {
				t.Errorf("%s %s → %d %s (beklenen %d)", c.kim, yon, rec.Code, rec.Body.String(), c.bek)
			}
		}
	}
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(kaynak)
	if strings.Count(s, "ozelPlanRotalari(r, domainsH)") != 1 || !strings.Contains(s,
		`r.With(middleware.SahipYonetim).Post("/domains/{id}/ozel-plan", domainsH.OzelPlan)`) {
		t.Error("main.go: ozelPlanRotalari tek kez kayıtlı değil ya da eski /ozel-plan kaydı SahipYonetim dışında")
	}
	if strings.Count(s, "ozelPlanAcilis(d)") != 1 {
		t.Error("main.go: açılış dönüşümü (ozelPlanAcilis) tek kez çağrılmalı")
	}
}
