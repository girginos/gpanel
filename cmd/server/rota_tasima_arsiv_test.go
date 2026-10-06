package main

// rota_tasima_arsiv_test.go — arşiv taşıma uçlarının GERÇEK rota zinciri: RequireAuth +
// AdminVeyaReseller (kök ve bayi geçer; müşteri 403, jetonsuz 401); kaynak anahtarı ucu
// AdminOnly (bayi 403). Kapsam/IDOR/bayi_etkin kararları handler içindedir ve DB'li testlerde
// (internal/tasima/arsiv_uc_test.go) sınanır; burada kapıların rota tablosunda DURDUĞU kanıtlanır.
// Handler'a ulaşan istek DB'siz Handlers'ta panikler; panik 599 olarak yakalanır = "kapı geçildi".

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/tasima"
)

func TestTasimaArsivRotalari_RolKapilari(t *testing.T) {
	sir := []byte("rota-tasima-arsiv-test-anahtari-0123456789")
	middleware.Init(nil)
	const gecildi = 599
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(sir))
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				defer func() {
					if recover() != nil {
						w.WriteHeader(gecildi)
					}
				}()
				next.ServeHTTP(w, q)
			})
		})
		(&tasima.Handlers{}).Rotalar(r)
	})
	admin, _ := auth.Issue(sir, 600, 1, "root", "admin")
	bayi, _ := auth.IssueReseller(sir, 600, 7, "bayi", 7)
	musteri, _ := auth.Issue(sir, 600, 9, "musteri", "user")

	arsivUclari := [][2]string{
		{http.MethodPost, "/tasima/arsiv/yukle"},
		{http.MethodGet, "/tasima/arsiv/yuklemeler"},
		{http.MethodGet, "/tasima/arsiv/yukle/1"},
		{http.MethodPut, "/tasima/arsiv/yukle/1?konum=0"},
		{http.MethodPost, "/tasima/arsiv/yukle/1/bitir"},
		{http.MethodDelete, "/tasima/arsiv/yukle/1"},
		{http.MethodPost, "/tasima/arsiv/kesif"},
		{http.MethodGet, "/tasima/arsiv/kesif/1"},
		{http.MethodPost, "/tasima/arsiv/baslat"},
	}
	cagri := func(tok, yontem, yol string) int {
		req := httptest.NewRequest(yontem, yol, strings.NewReader("{}"))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, u := range arsivUclari {
		for ad, c := range map[string]struct {
			tok string
			bek int
		}{
			"kök":      {admin, gecildi},
			"bayi":     {bayi, gecildi},
			"müşteri":  {musteri, http.StatusForbidden},
			"jetonsuz": {"", http.StatusUnauthorized},
			"bozuk":    {"x.y.z", http.StatusUnauthorized},
		} {
			if got := cagri(c.tok, u[0], u[1]); got != c.bek {
				t.Errorf("%s %s [%s] → %d (beklenen %d)", u[0], u[1], ad, got, c.bek)
			}
		}
	}
	// Kaynak anahtarları (etkin/bayi_etkin) yalnız kök.
	for ad, c := range map[string]struct {
		tok string
		bek int
	}{"kök (gövde {} → handler 400)": {admin, http.StatusBadRequest}, "bayi": {bayi, http.StatusForbidden}, "müşteri": {musteri, http.StatusForbidden}, "jetonsuz": {"", http.StatusUnauthorized}} {
		if got := cagri(c.tok, http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel"); got != c.bek {
			t.Errorf("PUT /tasima/kaynaklar/arsiv-cpanel [%s] → %d (beklenen %d)", ad, got, c.bek)
		}
	}
	// Eski /system/tasima takma adı arşiv uçlarını AÇMAZ (arşiv yalnız /tasima/arsiv altında).
	for _, y := range []string{"/system/tasima/arsiv/yukle", "/system/tasima/arsiv/kesif"} {
		if got := cagri(admin, http.MethodPost, y); got != http.StatusNotFound && got != http.StatusMethodNotAllowed {
			t.Errorf("%s: takma ad arşiv ucu açmamalı (%d)", y, got)
		}
	}
}
