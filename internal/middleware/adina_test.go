package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"girginospanel/internal/auth"
)

func TestAdinaKarar_Tablo(t *testing.T) {
	sahip := adinaDomain{bulundu: true, resellerID: 7}
	cases := []struct {
		ad  string
		rol string
		rid int64
		d   adinaDomain
		gec bool
	}{
		{"yonetici her domain", "admin", 0, adinaDomain{bulundu: true, resellerID: 9}, true},
		{"yonetici kok askisi", "admin", 0, adinaDomain{bulundu: true, askida: true, kaynak: "manuel"}, true},
		{"bayi kendi", "reseller", 7, sahip, true},
		{"bayi baskasinin", "reseller", 8, sahip, false},
		{"bayi kimliksiz", "reseller", 0, adinaDomain{bulundu: true}, false},
		{"bayi kendi askisi", "reseller", 7, adinaDomain{bulundu: true, resellerID: 7, askida: true, kaynak: "bayi_el"}, true},
		{"bayi kok askisi", "reseller", 7, adinaDomain{bulundu: true, resellerID: 7, askida: true, kaynak: "manuel"}, false},
		{"bayi bos kaynak askisi", "reseller", 7, adinaDomain{bulundu: true, resellerID: 7, askida: true}, false},
		{"domain yok", "admin", 0, adinaDomain{}, false},
		{"musteri rolu", "user", 7, sahip, false},
	}
	for _, c := range cases {
		kod, _ := AdinaKarar(c.rol, c.rid, c.d)
		if (kod == 0) != c.gec {
			t.Errorf("%s: kod=%d, geçmeli=%v", c.ad, kod, c.gec)
		}
	}
}

func TestAdinaYasak(t *testing.T) {
	h := AdinaYasak(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, c := range []struct {
		mc  *auth.MusteriClaims
		kod int
	}{
		{&auth.MusteriClaims{DomainID: 1, AktorUID: 7}, http.StatusForbidden},
		{&auth.MusteriClaims{DomainID: 1}, http.StatusOK},
		{nil, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodPut, "/x", nil)
		if c.mc != nil {
			req = req.WithContext(context.WithValue(req.Context(), musteriClaimsKey, c.mc))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.kod {
			t.Errorf("mc=%+v: %d beklenen %d", c.mc, rec.Code, c.kod)
		}
	}
}

func TestAdinaAktorAdi_SigarVeEkKorunur(t *testing.T) {
	uzun := strings.Repeat("ş", 80)
	s := AdinaAktorAdi(uzun, 123456789012345)
	if utf8.RuneCountInString(s) > 64 || !strings.HasSuffix(s, " (adına musteri:123456789012345)") {
		t.Fatalf("aktör adı 64 karaktere sığmalı ve eki korumalı: %q (%d)", s, utf8.RuneCountInString(s))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), musteriClaimsKey, &auth.MusteriClaims{DomainID: 42, AktorUID: 7, AktorAd: "bayi_a"}))
	if uid, ad := Aktor(req); uid != 7 || ad != "bayi_a (adına musteri:42)" {
		t.Fatalf("Aktor adına-oturumda gerçek aktörü döndürmeli: %d %q", uid, ad)
	}
}
