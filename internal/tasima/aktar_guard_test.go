package tasima

import (
	"strings"
	"testing"
)

// reDBAd, dbKullanicisiVarMi'nin ilk kapisi: SQL/kabuk metakarakterlerini
// kesin reddetmeli. Sorgu artik sabit olsa da allowlist sozlesmesini sabitler.
func TestReDBAdEnjeksiyonKarakterleriniReddeder(t *testing.T) {
	kotu := []string{
		"a'b", "a\\b", "a;b", "a b", "a\x60b", "", "a\"b",
		"a\nb", "a\rb", "a\x00b", strings.Repeat("x", 65),
	}
	for _, k := range kotu {
		if reDBAd.MatchString(k) {
			t.Errorf("reDBAd kabul etmemeliydi: %q", k)
		}
	}
	iyi := []string{"wp_abc", "user$1", "a-b_c", "A9", strings.Repeat("x", 64)}
	for _, k := range iyi {
		if !reDBAd.MatchString(k) {
			t.Errorf("reDBAd reddetmemeliydi: %q", k)
		}
	}
}
