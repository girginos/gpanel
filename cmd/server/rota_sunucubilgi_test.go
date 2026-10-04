package main

// rota_sunucubilgi_test.go — kök panosundaki "Sunucu bilgileri" kartı (hostname, OS,
// ana IP, panel sürümü, kernel, mimari) yeni uç AÇMAZ; mevcut GET /system/usage
// yanıtındaki "sistem" bloğunu kullanır. Bu blok hostname/IP/kernel sızdırır, bu yüzden
// uç YALNIZ kök yönetici (AdminOnly) kalmalı: bayi/müşteri 403, jetonsuz 401.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/system"
)

func TestSunucuBilgisi_YalnizKok(t *testing.T) {
	sir := []byte("rota-sunucubilgi-test-anahtari-0123456789")
	middleware.Init(nil)
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(sir))
		// Gerçek zincir: RequireAuth + AdminOnly; yük olarak yalnız SystemInfo (ağır
		// servis/disk taramasını atlar) — Handler'ın "sistem" bloğuyla aynı kaynak.
		r.With(middleware.AdminOnly).Get("/system/usage", func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"sistem": system.ReadInfo()})
		})
	})
	admin, _ := auth.Issue(sir, 600, 1, "root", "admin")
	bayi, _ := auth.IssueReseller(sir, 600, 7, "bayi", 7)
	musteri, _ := auth.Issue(sir, 600, 9, "musteri", "user")
	for _, c := range []struct {
		ad, tok string
		bek     int
	}{
		{"kök", admin, http.StatusOK},
		{"bayi", bayi, http.StatusForbidden},
		{"müşteri", musteri, http.StatusForbidden},
		{"jetonsuz", "", http.StatusUnauthorized},
		{"bozuk jeton", "x.y.z", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest("GET", "/system/usage", nil)
		if c.tok != "" {
			req.Header.Set("Authorization", "Bearer "+c.tok)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != c.bek {
			t.Errorf("%s → %d (beklenen %d)", c.ad, rec.Code, c.bek)
		}
		if c.bek != http.StatusOK && (strings.Contains(rec.Body.String(), "hostname") || strings.Contains(rec.Body.String(), "kernel")) {
			t.Errorf("%s: reddedilen yanıtta sunucu bilgisi sızdı", c.ad)
		}
		if c.bek == http.StatusOK {
			var y struct {
				Sistem map[string]any `json:"sistem"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &y); err != nil {
				t.Fatal(err)
			}
			for _, k := range []string{"hostname", "ip", "os_adi", "kernel", "mimari", "panel_surum"} {
				if _, ok := y.Sistem[k]; !ok {
					t.Errorf("sistem.%s alanı yok", k)
				}
			}
			if a, _ := y.Sistem["mimari"].(string); a == "amd64" || a == "arm64" {
				t.Errorf("mimari normalleşmemiş: %q", a)
			}
		}
	}

	// Gerçek kayıt satırı: main.go'da AdminOnly arkasında, tek kez.
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(kaynak), `r.With(middleware.AdminOnly).Get("/system/usage", system.Handler)`); n != 1 {
		t.Errorf("main.go: /system/usage AdminOnly arkasında tam bir kez kayıtlı olmalı (alınan %d)", n)
	}
	if n := strings.Count(string(kaynak), `"/system/usage"`); n != 1 {
		t.Errorf("main.go: /system/usage başka bir kapıyla ikinci kez kayıtlı (%d)", n)
	}
}
