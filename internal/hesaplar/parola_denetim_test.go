package hesaplar

import "testing"

// Belge ve ileti "satır sonu ya da denetim karakteri" der: HER kontrol karakteri reddedilir
// (VM doğrulaması 2026-10-13: TAB, U+0001, U+007F kabul ediliyordu).
func TestParolaGecerli_DenetimKarakterleri(t *testing.T) {
	for _, kotu := range []string{"abc123abc123\r", "abc123\nabc123", "abc\x00123abc123", "abc123\tabc123",
		"abc123\x01abc123", "abc123\x7fabc123", "abc123\u0085abc123", "abc123\x1babc123"} {
		if ParolaGecerli(kotu) {
			t.Errorf("denetim karakterli parola kabul edildi: %q", kotu)
		}
	}
	for _, iyi := range []string{"Abc123abc123", "Çğış-Öü 123 xyz", "p@ss w0rd!#$%^&*()", "ü1234567890a", ""} {
		if !ParolaGecerli(iyi) {
			t.Errorf("geçerli parola reddedildi: %q", iyi)
		}
	}
}
