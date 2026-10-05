package onyuzkapsam

// duzenle_dugme_test.go — tablo satırlarındaki "Düzenle" eylemi düz metin/çerçevesiz (flat)
// görünmez; çerçeveli (outlined) buton dilinde olmalı.

import (
	"regexp"
	"strings"
	"testing"
)

func TestDuzenleDugmesiDuzMetinDegil(t *testing.T) {
	re := regexp.MustCompile(`<Button[^\n]*?>\{cevir\("Düzenle"\)\}</Button>`)
	for _, f := range []string{"pages/DomainDNSPage.tsx", "pages/DomainCronPage.tsx", "pages/ResellerlarPage.tsx", "pages/BayiPlanlariPage.tsx"} {
		src := oku(t, f)
		m := re.FindAllString(src, -1)
		if len(m) == 0 {
			t.Errorf("%s: Düzenle düğmesi bulunamadı", f)
		}
		for _, d := range m {
			if !strings.Contains(d, `variant="outlined"`) || strings.Contains(d, `variant="flat"`) {
				t.Errorf("%s: Düzenle düğmesi çerçeveli (outlined) değil: %s", f, d)
			}
		}
	}
}
