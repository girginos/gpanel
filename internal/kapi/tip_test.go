package kapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// KanitliKapi zorlamadan BAĞIMSIZ çalışır: açılış ölçümü zorlama açılmadan önce
// zinciri test edebilmeli.
func TestKanitliKapiZorlamadanBagimsiz(t *testing.T) {
	eski := nginxtenMi
	t.Cleanup(func() { nginxtenMi = eski; ZorlamaAyarla(false) })
	nginxtenMi = func(*http.Request) bool { return true }
	ZorlamaAyarla(false)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/kapi", nil)
	r.Header.Set(Baslik, Musteri)
	if k := KanitliKapi(r); k != Musteri {
		t.Fatalf("zorlama kapalıyken de kanıtlı kapı okunmalı, got %q", k)
	}
	if k := Istek(r); k != "" {
		t.Fatalf("Istek ise zorlama kapalıyken \"\" dönmeli, got %q", k)
	}
	r.Header.Set(Baslik, Bilinmeyen) // map varsayılanı: tanınmayan değer
	if k := KanitliKapi(r); k != "" {
		t.Fatalf("tanımsız değer → \"\", got %q", k)
	}
}

func TestTipHandler(t *testing.T) {
	eski := nginxtenMi
	t.Cleanup(func() { nginxtenMi = eski })
	guven := true
	nginxtenMi = func(*http.Request) bool { return guven }

	tipAl := func(baslik string) (string, *httptest.ResponseRecorder) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/kapi", nil)
		if baslik != "" {
			r.Header.Set(Baslik, baslik)
		}
		rec := httptest.NewRecorder()
		TipHandler(rec, r)
		var v struct {
			Tip string `json:"tip"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
			t.Fatalf("JSON: %v", err)
		}
		return v.Tip, rec
	}
	cases := map[string]string{Musteri: "musteri", Admin: "yonetim", Bayi: "yonetim", Eski: "karma", "": "karma"}
	for baslik, bek := range cases {
		if got, rec := tipAl(baslik); got != bek {
			t.Errorf("başlık %q → %q beklenir, got %q", baslik, bek, got)
		} else if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("tip önbelleğe alınmamalı (port değişince bayat kalır)")
		}
	}
	// Admin ve bayi portu AYNI yanıtı almalı: yönetici portu bu uçtan ayırt edilemez.
	a, _ := tipAl(Admin)
	b, _ := tipAl(Bayi)
	if a != b {
		t.Fatalf("admin (%q) ve bayi (%q) ayırt edilebilir", a, b)
	}
	guven = false
	if got, _ := tipAl(Musteri); got != "karma" {
		t.Fatalf("nginx kanıtı yoksa başlığa güvenilmez → karma, got %q", got)
	}
}
