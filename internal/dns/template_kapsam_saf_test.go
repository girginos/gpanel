package dns

// template_kapsam_saf_test.go — sablonKapsami (DB'siz): kim hangi şablon
// satırlarını yönetir, kim sunucu geneli meta'ya erişir.

import (
	"testing"

	"girginospanel/internal/auth"
)

func TestSablonKapsami(t *testing.T) {
	cases := []struct {
		ad       string
		c        *auth.Claims
		rid      int64
		yonetici bool
		ok       bool
	}{
		{"yönetici", &auth.Claims{Role: "admin", UserID: 1}, 0, true, true},
		{"bayi", &auth.Claims{Role: "reseller", UserID: 7, ResellerID: 7}, 7, false, true},
		// Rol ya da anahtardan biri yeter: bayi yönetici kapsamına DÜŞMEZ.
		{"anahtarlı ama rolü boş", &auth.Claims{UserID: 7, ResellerID: 7}, 7, false, true},
		{"yönetici rolü + bayi anahtarı", &auth.Claims{Role: "admin", UserID: 7, ResellerID: 7}, 7, false, true},
		// Kimliksiz bayi: hiçbir şey (sunucu geneli rid=0 DEĞİL).
		{"kimliksiz bayi", &auth.Claims{Role: "reseller", UserID: 7}, 0, false, false},
		{"bilinmeyen rol", &auth.Claims{Role: "user", UserID: 5}, 0, false, false},
		{"kimlik yok", nil, 0, false, false},
	}
	for _, c := range cases {
		rid, yon, ok := sablonKapsami(c.c)
		if rid != c.rid || yon != c.yonetici || ok != c.ok {
			t.Errorf("%s: (rid=%d yönetici=%v ok=%v), beklenen (%d %v %v)", c.ad, rid, yon, ok, c.rid, c.yonetici, c.ok)
		}
	}
}
