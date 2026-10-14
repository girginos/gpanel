package kaynaklimit

import (
	"strings"
	"testing"
)

// Panel DB kullanıcı adı üretimi önek + "_" + sonek ≤ 64 karaktere izin verir (hesaplar.GecerliDBKimlik).
// Allowlist 32'de kalırsa 33-64 karakterlik KİRACI kullanıcıları plan limitinden ve yavaş sorgu
// KILL'inden sessizce muaf kalırdı; 64 sınırı dahil, 65 ve güvensiz karakterler hâlâ atlanır.
func TestGovernorKullaniciAdiUzunluk(t *testing.T) {
	for _, n := range []int{1, 16, 32, 33, 48, 64} {
		u := strings.Repeat("a", n)
		if dbGovernorUserAtlanir(u) {
			t.Errorf("%d karakterlik kiracı kullanıcısı atlanmamalı", n)
		}
	}
	for ad, u := range map[string]string{
		"65 karakter":    strings.Repeat("a", 65),
		"boş":            "",
		"tırnak":         "a'b",
		"backtick":       "a`b",
		"boşluk":         "a b",
		"noktalı virgül": "a;b",
		"yeni satır":     "ab\n",
		"root":           "root",
		"ROOT büyük":     "ROOT",
		"panel":          "panel",
		"mariadb.sys":    "mariadb.sys",
	} {
		if !dbGovernorUserAtlanir(u) {
			t.Errorf("%s: atlanmalı (%q)", ad, u)
		}
	}
	// host allowlist'i
	for _, h := range []string{"localhost", "127.0.0.1", "%", "10.%", strings.Repeat("h", 64)} {
		if !reGovernorHost.MatchString(h) {
			t.Errorf("host geçerli olmalı: %q", h)
		}
	}
	for _, h := range []string{"", strings.Repeat("h", 65), "a'b", "a b", "a;b", "a\n"} {
		if reGovernorHost.MatchString(h) {
			t.Errorf("host geçersiz olmalı: %q", h)
		}
	}
}
