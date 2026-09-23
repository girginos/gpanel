package middleware

// kapi_http_test.go — RequireAuth'un panel kapısı kuralı (ADR-GP-001).
// Token alındığı kapıya bağlıdır: başka kapıda "geçersiz oturum". mwRouter ile
// aynı DB'siz kurulum (scopeDB=nil → iptal kontrolleri fail-open), böylece
// yalnız kapı kuralı ölçülür.

import (
	"net/http"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/kapi"

	"github.com/go-chi/chi/v5"
)

func kapiRouter() http.Handler {
	scopeDB = nil
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(RequireAuth(mwSecret))
		r.Get("/x", mwOK)
	})
	return r
}

// kapidanGelsin: testin süresince isteklerin geldiği kapıyı sabitler.
func kapidanGelsin(t *testing.T, k string) {
	t.Helper()
	eski := kapiCoz
	kapiCoz = func(*http.Request) string { return k }
	t.Cleanup(func() { kapiCoz = eski })
}

func kapiAdminTok(t *testing.T, kapiAdi string) string {
	t.Helper()
	tok, err := auth.IssueAt(mwSecret, 3600, 1, "root", "admin", 0, kapiAdi)
	if err != nil {
		t.Fatalf("IssueAt: %v", err)
	}
	return tok
}

func kapiMusteriTok(t *testing.T, kapiAdi string) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(mwSecret, auth.MusteriClaims{FTPHesapID: 7, DomainID: 42, Kapi: kapiAdi}, 3600)
	if err != nil {
		t.Fatalf("GenerateMusteri: %v", err)
	}
	return tok
}

func TestRequireAuthKapiMatrisi(t *testing.T) {
	cases := []struct {
		ad        string
		istekKapi string
		tok       string
		beklenen  int
	}{
		// Pozitif: token kendi kapısında.
		{"admin token admin kapısında", kapi.Admin, kapiAdminTok(t, kapi.Admin), 200},
		{"müşteri token müşteri kapısında", kapi.Musteri, kapiMusteriTok(t, kapi.Musteri), 200},
		// Geçiş: bağsız (geçiş öncesi) token'lar eski kapıda çalışmaya devam eder.
		{"bağsız admin token eski kapıda", kapi.Eski, kapiAdminTok(t, ""), 200},
		{"bağsız müşteri token eski kapıda", kapi.Eski, kapiMusteriTok(t, ""), 200},
		{"eski-kapı token'ı eski kapıda", kapi.Eski, kapiAdminTok(t, kapi.Eski), 200},
		// Zorlama kapalı (geri alma / ilk açılış): kimse atılmaz.
		{"zorlama kapalı: kapı-bağlı token", "", kapiAdminTok(t, kapi.Admin), 200},
		{"zorlama kapalı: bağsız token", "", kapiAdminTok(t, ""), 200},
		// Negatif: token taşınamaz.
		{"admin token müşteri kapısında", kapi.Musteri, kapiAdminTok(t, kapi.Admin), 401},
		{"admin token bayi kapısında", kapi.Bayi, kapiAdminTok(t, kapi.Admin), 401},
		{"müşteri token admin kapısında", kapi.Admin, kapiMusteriTok(t, kapi.Musteri), 401},
		{"müşteri token bayi kapısında", kapi.Bayi, kapiMusteriTok(t, kapi.Musteri), 401},
		{"bağsız admin token admin kapısında", kapi.Admin, kapiAdminTok(t, ""), 401},
		{"bağsız müşteri token müşteri kapısında", kapi.Musteri, kapiMusteriTok(t, ""), 401},
		{"admin-kapısı token'ı eski kapıda", kapi.Eski, kapiAdminTok(t, kapi.Admin), 401},
		{"bilinmeyen kapı (doğrudan :8080)", kapi.Bilinmeyen, kapiAdminTok(t, kapi.Admin), 401},
		{"bilinmeyen kapı + bağsız token", kapi.Bilinmeyen, kapiAdminTok(t, ""), 401},
	}
	h := kapiRouter()
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			kapidanGelsin(t, c.istekKapi)
			rec := mwDo(h, http.MethodGet, "/x", "Bearer "+c.tok)
			if rec.Code != c.beklenen {
				t.Fatalf("kod=%d, beklenen=%d (gövde=%q)", rec.Code, c.beklenen, rec.Body.String())
			}
		})
	}
}

// Kapı reddi token'ın hangi kapıya ait olduğunu SIZDIRMAMALI: yanıt, bozuk bir
// token'la birebir aynı olmalı.
func TestRequireAuthKapiReddiGenelYanit(t *testing.T) {
	h := kapiRouter()
	kapidanGelsin(t, kapi.Musteri)
	kapiRed := mwDo(h, http.MethodGet, "/x", "Bearer "+kapiAdminTok(t, kapi.Admin))
	bozuk := mwDo(h, http.MethodGet, "/x", "Bearer bu.gecerli.degil")
	if kapiRed.Code != bozuk.Code || kapiRed.Body.String() != bozuk.Body.String() {
		t.Fatalf("kapı reddi ayırt edilebilir: kapı=(%d %q) bozuk=(%d %q)",
			kapiRed.Code, kapiRed.Body.String(), bozuk.Code, bozuk.Body.String())
	}
}
