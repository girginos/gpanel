package musteri

// login_kapi_test.go — müşteri giriş ucunun panel kapısı kuralı (ADR-GP-001).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/kapi"
)

func kapidanGelsin(t *testing.T, k string) {
	t.Helper()
	eski := kapiCoz
	kapiCoz = func(*http.Request) string { return k }
	t.Cleanup(func() { kapiCoz = eski })
}

// Yönetici / bayi / bilinmeyen kapıda müşteri giriş ucu YOKTUR (DB nil → kimlik
// işlenseydi panik; var-olmayan rotayla birebir aynı yanıt).
func TestMusteriLoginYonetimKapisindaYok(t *testing.T) {
	for _, k := range []string{kapi.Admin, kapi.Bayi, kapi.Bilinmeyen} {
		t.Run(k, func(t *testing.T) {
			kapidanGelsin(t, k)
			h := &Handlers{Secret: []byte("musteri-test-anahtari-0123456789-abcdef")}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login",
				strings.NewReader(`{"kullanici":"c_ornek","parola":"x"}`))
			rec := httptest.NewRecorder()
			h.Login(rec, req)
			ref := httptest.NewRecorder()
			http.NotFound(ref, req)
			if rec.Code != http.StatusNotFound || rec.Body.String() != ref.Body.String() {
				t.Fatalf("var-olmayan rotadan ayırt edilebilir: kod=%d gövde=%q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMusteriLoginMusteriKapisindaIlerler(t *testing.T) {
	for _, k := range []string{"", kapi.Musteri, kapi.Eski} {
		t.Run("kapi="+k, func(t *testing.T) {
			kapidanGelsin(t, k)
			h := &Handlers{Secret: []byte("musteri-test-anahtari-0123456789-abcdef")}
			rec := httptest.NewRecorder()
			h.Login(rec, httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login", strings.NewReader(`{}`)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("kapı engeli olmamalı (400 beklenir), kod=%d", rec.Code)
			}
		})
	}
}
