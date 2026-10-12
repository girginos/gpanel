package panelhost

import "testing"

func TestIlkHostAdi(t *testing.T) {
	durumlar := []struct {
		ad    string
		girdi []string
		bek   string
	}{
		{"yakala-hepsi _ (ad ayarlanmamış kurulum)", []string{"_"}, ""},
		{"_ sonra gerçek ad", []string{"_", "panel.ornek.com"}, "panel.ornek.com"},
		{"IP ve localhost atlanır", []string{"203.0.113.181", "localhost", "127.0.0.1", "panel.ornek.com"}, "panel.ornek.com"},
		{"IPv6 atlanır", []string{"[2a01:4f8::1]", "::1", "p.ornek.com"}, "p.ornek.com"},
		{"joker atlanır", []string{"*.ornek.com", ".ornek.com", "p.ornek.com"}, "p.ornek.com"},
		{"düzenli ifade atlanır", []string{`~^(www\.)?ornek\.com$`, "p.ornek.com"}, "p.ornek.com"},
		{"tek etiket (noktasız) atlanır", []string{"gp-prova-b"}, ""},
		{"boş liste", nil, ""},
		{"ilk gerçek ad seçilir", []string{"a.ornek.com", "b.ornek.com"}, "a.ornek.com"},
	}
	for _, d := range durumlar {
		if g := ilkHostAdi(d.girdi); g != d.bek {
			t.Errorf("%s: ilkHostAdi(%q) = %q, beklenen %q", d.ad, d.girdi, g, d.bek)
		}
	}
}
