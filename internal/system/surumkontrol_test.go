package system

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSurumYeniMi(t *testing.T) {
	tablo := []struct {
		ad, son, mevcut string
		bek             bool
	}{
		{"esit", "0.3.0-20261009", "0.3.0-20261009", false},
		{"eski", "0.3.0-20261006", "0.3.0-20261009", false},
		{"yeni", "0.3.0-20261010", "0.3.0-20261009", true},
		{"dev f3 -> tarih", "0.3.0-20261006", "0.3.0-f3", true},
		{"tarih -> f3 (son ayrisamaz)", "0.3.0-f3", "0.3.0-20261009", true},
		{"iki dev ayni", "0.3.0-f3", "0.3.0-f3", false},
		{"iki dev farkli", "0.3.0-f4", "0.3.0-f3", true},
		{"duz esit", "0.3.0", "0.3.0", false},
		{"duz -> tarihli", "0.3.0-20261006", "0.3.0", true},
		{"tarihli -> duz", "0.3.0", "0.3.0-20261006", false},
		{"yama yukseldi", "0.3.1-20260101", "0.3.0-20261009", true},
		{"ikincil dustu", "0.2.9-20271231", "0.3.0-20261009", false},
		{"ana yukseldi", "1.0.0", "0.9.9-20261009", true},
		{"v oneki", "v0.3.1", "0.3.0", true},
		{"son bos", "", "0.3.0-1", false},
		{"mevcut bos", "0.3.0-1", "", true},
		{"bozuk ikisi", "abc", "xyz", true},
		{"bozuk ayni", "abc", "abc", false},
		{"bozuk cekirdek", "0.3.x-1", "0.3.0-1", true},
	}
	for _, c := range tablo {
		if g := surumYeniMi(c.son, c.mevcut); g != c.bek {
			t.Errorf("%s: surumYeniMi(%q,%q)=%v, beklenen %v", c.ad, c.son, c.mevcut, g, c.bek)
		}
	}
}

func TestSurumKontrolDurumKuruluYeni(t *testing.T) {
	surumMu.RLock()
	oY, oM, oA := surumYayin, surumMevcut, surumAcik
	surumMu.RUnlock()
	t.Cleanup(func() {
		surumMu.Lock()
		surumYayin, surumMevcut, surumAcik = oY, oM, oA
		surumMu.Unlock()
	})
	cases := []struct {
		ad, son, mevcut string
		acik            bool
		var_, kYeni     bool
	}{
		{"kurulu yeni", "0.3.0-20261006", "0.3.0-20261009", true, false, true},
		{"yayin yeni", "0.3.0-20261012", "0.3.0-20261009", true, true, false},
		{"esit", "0.3.0-20261009", "0.3.0-20261009", true, false, false},
		{"kapali", "0.3.0-20261012", "0.3.0-20261009", false, false, false},
		{"dev kurulum", "0.3.0-20261006", "0.3.0-f3", true, true, false},
	}
	for _, c := range cases {
		surumMu.Lock()
		surumYayin, surumMevcut, surumAcik = SurumYayin{SonSurum: c.son}, c.mevcut, c.acik
		surumMu.Unlock()
		w := httptest.NewRecorder()
		SurumKontrolDurum(w, httptest.NewRequest("GET", "/system/surum-kontrol", nil))
		var m map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatalf("%s: %v", c.ad, err)
		}
		if m["guncelleme_var"] != c.var_ || m["kurulu_yeni"] != c.kYeni {
			t.Errorf("%s: guncelleme_var=%v kurulu_yeni=%v, beklenen %v/%v", c.ad, m["guncelleme_var"], m["kurulu_yeni"], c.var_, c.kYeni)
		}
	}
}
