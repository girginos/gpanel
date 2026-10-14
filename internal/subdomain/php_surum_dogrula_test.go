package subdomain

import (
	"net/http"
	"strings"
	"testing"
)

// Alt alan PHP ayarı: geçersiz php_surum 400 alır ve HİÇBİR şey kaydedilmez/uygulanmaz;
// geçerli sürüm eskisi gibi kabul edilir.
func TestPHPAyarPut_PHPSurumDogrulanir(t *testing.T) {
	tok := hk_tokenlar(t)
	for _, d := range []struct {
		surum string
		kod   int
	}{{"9.9", http.StatusBadRequest}, {"php8.9", http.StatusBadRequest}, {"8.30", http.StatusBadRequest}, {"8.3", http.StatusOK}} {
		hk_dikisler(t, nil, nil)
		u := hk_phpAyarPut
		u.govde = `{"php_surum":"` + d.surum + `","ayarlar":{}}`
		k, govde, _ := hk_cagir(t, u, tok["admin"])
		if k != d.kod {
			t.Errorf("php_surum=%q: %d beklenirdi, %d %s", d.surum, d.kod, k, govde)
			continue
		}
		if d.kod == http.StatusBadRequest && !strings.Contains(hk_hataMetni(t, govde), "desteklenen") {
			t.Errorf("php_surum=%q: açıklayıcı ileti yok: %s", d.surum, govde)
		}
	}
}
