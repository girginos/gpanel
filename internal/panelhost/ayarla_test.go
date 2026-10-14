package panelhost

import "testing"

// adimBasarili: betik çıktısı İngilizce; "✗" öneki ve failed/error sözcükleri hata sayılır.
// Eski Türkçe betik sürümleri de tanınmaya devam eder.
func TestAdimBasarili(t *testing.T) {
	vakalar := []struct {
		satir  string
		beklen bool
	}{
		{"✓ nginx -t passed", true},
		{"✓ verified: unknown name is closed (code=000), panel is up (200)", true},
		{"! catch-all block MISSING", true},
		{"✗ nginx -t failed — CHANGE ROLLED BACK", false},
		{"✗ must be run as root", false},
		{"reload failed — rolled back", false},
		{"nginx: configuration error", false},
		{"✗ panel ERİŞİLEMEZ oldu", false},
		{"nginx -t başarısız — DEĞİŞİKLİK GERİ ALINDI", false},
		{"bir hata oluştu", false},
	}
	for _, v := range vakalar {
		if got := adimBasarili(v.satir); got != v.beklen {
			t.Errorf("adimBasarili(%q) = %v, beklenen %v", v.satir, got, v.beklen)
		}
	}
}
