package middleware

import (
	"strings"
	"testing"
)

// DestekYolIzinli: köprünün bağımsız varsayılan-red kapısı.
func TestDestekYolIzinli(t *testing.T) {
	ok := []string{
		"/api/v1/domains/42",
		"/api/v1/domains/42/ssl",
		"/api/v1/domains/1/logs/oku",
		"/api/v1/admin/system/loglar",
	}
	for _, y := range ok {
		if d, g := DestekYolIzinli("GET", y); !g || DestekRotalari[d] == "" {
			t.Errorf("izinli olmalı: %s (desen=%q)", y, d)
		}
	}
	red := []string{
		"/api/v1/domains",                     // listeleme tabloda yok
		"/api/v1/domains/42/ssl/yenile",       // yazma/başka uç
		"/api/v1/domains/abc/ssl",             // {id} rakam değil
		"/api/v1/domains/42/../admin/users",   // yol kaçışı
		"/api/v1/domains//ssl",                // boş bölüm
		"/api/v1/domains/42/ssl?x=1",          // sorgu yola girmez
		"/api/v1/domains/42%2Fssl",            // kodlanmış
		"/api/v1/domains/42/ssl#a",            // parça
		"/api/v1/domains/42/ssl\n",            // kontrol karakteri
		"/api/v1/auth/login",                  // kimlik ucu
		"/api/v1/admin/users",                 // tablo dışı
		"api/v1/domains/42",                   // göreli
		"/api/v2/domains/42",                  // başka sürüm
		"/api/v1/domains/1234567890123456789", // 19 hane
		"/api/v1/domains/42/" + strings.Repeat("a", 600),
	}
	for _, y := range red {
		if d, g := DestekYolIzinli("GET", y); g {
			t.Errorf("reddedilmeli: %q (desen=%q)", y, d)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "HEAD", "PATCH", ""} {
		if _, g := DestekYolIzinli(m, "/api/v1/domains/42"); g {
			t.Errorf("GET dışı yöntem geçti: %q", m)
		}
	}
}

// Tablodaki HER kalıp kendi somut örneğiyle geçmeli (kalıp/eşleyici kayması yakalanır).
func TestDestekYolIzinliTumKaliplar(t *testing.T) {
	for desen := range DestekRotalari {
		_, kalip, _ := strings.Cut(desen, " ")
		yol := strings.ReplaceAll(kalip, "{id}", "7")
		if d, g := DestekYolIzinli("GET", yol); !g || d != desen {
			t.Errorf("%s → %q/%v", desen, d, g)
		}
	}
}
