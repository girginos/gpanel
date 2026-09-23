package auth

// login_kapi_test.go — giriş ucunun panel kapısı kuralı (ADR-GP-001).

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

// Müşteri / bilinmeyen kapıda admin+bayi giriş ucu YOKTUR: var-olmayan rotayla
// BİREBİR aynı yanıt, ve kimlik bilgisi hiç işlenmez (DB nil → dokunulsaydı panik).
func TestLoginMusteriKapisindaYok(t *testing.T) {
	for _, k := range []string{kapi.Musteri, kapi.Bilinmeyen} {
		t.Run(k, func(t *testing.T) {
			kapidanGelsin(t, k)
			h := &Handlers{Secret: ct_secret, LifetimeSec: 60} // DB bilerek nil
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
				strings.NewReader(`{"kullanici":"root","parola":"dogru-olsa-bile"}`))
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

// Yönetim kapılarında (ve zorlama kapalıyken) uç var: kapı engeli yok, istek gövde
// doğrulamasına kadar ilerler (400 — DB'ye inmeden).
func TestLoginYonetimKapisindaIlerler(t *testing.T) {
	for _, k := range []string{"", kapi.Admin, kapi.Bayi, kapi.Eski} {
		t.Run("kapi="+k, func(t *testing.T) {
			kapidanGelsin(t, k)
			h := &Handlers{Secret: ct_secret, LifetimeSec: 60}
			rec := httptest.NewRecorder()
			h.Login(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("kapı engeli olmamalı (400 beklenir), kod=%d", rec.Code)
			}
		})
	}
}

// Token'a yazılan kapı tur-gidişte korunur; kapısız üretilen (geçiş öncesi) boş kalır.
func TestTokenKapiTurGidis(t *testing.T) {
	tok, err := IssueAt(ct_secret, 3600, 1, "root", "admin", 0, kapi.Admin)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := Parse(ct_secret, tok); err != nil || c.Kapi != kapi.Admin {
		t.Fatalf("admin kapısı korunmadı: %+v err=%v", c, err)
	}
	eski, _ := Issue(ct_secret, 3600, 1, "root", "admin")
	if c, _ := Parse(ct_secret, eski); c.Kapi != "" {
		t.Fatalf("kapısız token boş kapı taşımalı, got %q", c.Kapi)
	}
	bt, _ := IssueResellerAt(ct_secret, 3600, 5, "bayi1", 5, 0, kapi.Bayi)
	if c, _ := Parse(ct_secret, bt); c.Kapi != kapi.Bayi || c.Role != "reseller" {
		t.Fatalf("bayi token kapısı/rolü bozuk: %+v", c)
	}
	mt, _, _ := GenerateMusteri(ct_secret, MusteriClaims{FTPHesapID: 7, DomainID: 42, Kapi: kapi.Musteri}, 3600)
	if mc, err := ParseMusteri(ct_secret, mt); err != nil || mc.Kapi != kapi.Musteri {
		t.Fatalf("müşteri token kapısı korunmadı: %+v err=%v", mc, err)
	}
}
