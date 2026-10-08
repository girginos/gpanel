package maske

import (
	"strings"
	"testing"
)

// Boşluk içeren, tırnaklı parola değeri BÜTÜNÜYLE maskelenmeli (eskiden yalnız
// ilk sözcük maskelenir, kalanı günlüğe/Claude'a sızardı).
func TestDesenleMaskele_BoslukluTirnakliParola(t *testing.T) {
	cases := []struct{ girdi, sizmamali string }{
		{`password="correct horse battery"`, "horse"},
		{`{"password":"correct horse battery","user":"a"}`, "horse"},
		{`parola: 'dogru at pil'`, "at pil"},
		{`secret = "iki kelime"`, "kelime"},
		{`şifre: "iki kelime"`, "kelime"},
	}
	for _, c := range cases {
		cikti := DesenleMaskele(c.girdi)
		if strings.Contains(cikti, c.sizmamali) {
			t.Errorf("sızdı: %q -> %q", c.girdi, cikti)
		}
	}
}
