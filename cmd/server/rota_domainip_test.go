package main

// rota_domainip_test.go — alan adı IP değiştirme / IP seçimi YALNIZ kök yönetici
// (kullanıcı kararı 2026-10-03). Bayi tekil (PUT /domains/{id}/ip), seçenek listesi
// (GET /domains/ip-secenekleri) ve toplu ("ip" tipli POST /domains/toplu/is) yolların
// HEPSİNDE 403 alır.
//
// Davranış: domainip.Rotalar (main.go'nun çağırdığı TEK kayıt) gerçek zincirle sınanır;
// ulaşılamaz DB ile yöneticinin handler'a ulaştığı (400/500) görünür. Toplu uçlar ve
// eski /admin ucu main.go kaynağındaki kayıt satırından kilitlenir.

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
	"girginospanel/internal/domainip"
	"girginospanel/internal/middleware"
)

func TestDomainIPRotalari_YalnizYonetici(t *testing.T) {
	sir := []byte("rota-domainip-test-anahtari-0123456789abcd")
	middleware.Init(nil)
	db, err := sql.Open("mysql", "x:y@tcp(127.0.0.1:1)/yok?timeout=200ms")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(sir))
		(&domainip.Handler{DB: db}).Rotalar(r)
	})
	admin, _ := auth.Issue(sir, 600, 1, "root", "admin")
	bayi, _ := auth.IssueReseller(sir, 600, 7, "bayi", 7)
	for _, c := range []struct {
		ad, yon, yol, govde, tok string
		bek                      int
	}{
		{"bayi tekil", "PUT", "/domains/5/ip", `{"ipv4":"1.2.3"}`, bayi, http.StatusForbidden},
		{"bayi seçenekler", "GET", "/domains/ip-secenekleri", "", bayi, http.StatusForbidden},
		{"anonim tekil", "PUT", "/domains/5/ip", `{"ipv4":"1.2.3"}`, "", http.StatusUnauthorized},
		{"yönetici tekil (handler doğrular)", "PUT", "/domains/5/ip", `{"ipv4":"1.2.3"}`, admin, http.StatusBadRequest},
		{"yönetici seçenekler (DB yok → 500)", "GET", "/domains/ip-secenekleri", "", admin, http.StatusInternalServerError},
	} {
		req := httptest.NewRequest(c.yon, c.yol, strings.NewReader(c.govde))
		if c.tok != "" {
			req.Header.Set("Authorization", "Bearer "+c.tok)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != c.bek {
			t.Errorf("%s → %d %s (beklenen %d)", c.ad, rec.Code, rec.Body.String(), c.bek)
		}
	}

	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(kaynak)
	if strings.Count(s, "domainIPH.Rotalar(r)") != 1 {
		t.Error("main.go: domainIPH.Rotalar(r) tam bir kez çağrılmalı")
	}
	if strings.Contains(s, `"/domains/{id}/ip"`) || strings.Contains(s, `"/domains/ip-secenekleri"`) {
		t.Error("main.go: IP uçları yalnız domainip.Rotalar ile kaydedilmeli (AdminOnly dışı ikinci kayıt riski)")
	}
	// Eski yönetici ucu + toplu iş uçları (tip "ip" dahil) AdminOnly arkasında.
	for _, satir := range []string{
		`r.With(middleware.AdminOnly).Put("/domains/{id}/ipv4", domainIPH.DomainIP)`,
		`r.With(middleware.AdminOnly).Post("/domains/toplu/is", topluH.Baslat)`,
		`r.With(middleware.AdminOnly).Get("/domains/toplu/is", topluH.Durum)`,
		`r.With(middleware.AdminOnly).Post("/domains/toplu/is/iptal", topluH.Iptal)`,
		`r.With(middleware.AdminOnly).Get("/domains/toplu/aktif", topluH.Aktif)`,
	} {
		if strings.Count(s, satir) != 1 {
			t.Errorf("main.go: kayıt AdminOnly arkasında değil ya da eksik/çift: %s", satir)
		}
	}
	// Handler'lar main.go'da başka bir kapıyla ikinci kez kaydedilmemeli
	// (DomainIP yalnız eski /admin ucu; IPSecenekleri yalnız Rotalar içinde).
	for h, bek := range map[string]int{"domainIPH.DomainIP": 1, "domainIPH.IPSecenekleri": 0, "topluH.Baslat": 1} {
		if n := strings.Count(s, h); n != bek {
			t.Errorf("main.go: %s %d kez kayıtlı olmalı (alınan %d)", h, bek, n)
		}
	}
}
