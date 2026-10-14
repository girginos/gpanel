package dns

// zone_ad_test.go — zone dışı mutlak kayıt adı reddedilir (BIND "out-of-zone data" diye yok
// sayıyordu: kayıt panelde görünüp yayınlanmıyordu). Göreli ve zone içi mutlak adlar serbest.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAdZoneIcinde(t *testing.T) {
	for _, d := range []struct {
		ad   string
		icde bool
	}{
		{"www", true}, {"@", true}, {"a.b", true}, {"bayialan.test.", true}, {"www.bayialan.test.", true},
		{"WWW.BayiAlan.Test.", true}, {"x.y.bayialan.test.", true},
		{"foo.example.org.", false}, {"bayialan.test.evil.", false}, {"xbayialan.test.", false}, {"test.", false},
	} {
		err := AdZoneIcinde(d.ad, "bayialan.test")
		if (err == nil) != d.icde {
			t.Errorf("AdZoneIcinde(%q) = %v, zone içi beklenen %t", d.ad, err, d.icde)
		}
	}
}

func TestCreate_ZoneDisiMutlakAdReddedilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	ekle := func(ad string) (int, string) {
		return zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns", "/domains/42/dns",
			"application/json", `{"ad":"`+ad+`","tip":"A","deger":"192.0.2.9"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	}
	if kod, govde := ekle("foo.example.org."); kod != http.StatusBadRequest || !strings.Contains(govde, "dışında") {
		t.Errorf("zone dışı mutlak ad 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE ad LIKE 'foo%'`); n != 0 {
		t.Error("zone dışı kayıt DB'ye yazıldı")
	}
	if kod, govde := ekle("www.bayialan.test."); kod != http.StatusCreated {
		t.Errorf("zone içi mutlak ad kabul edilmeli: %d %s", kod, govde)
	}
}

func TestIceAktar_ZoneDisiKayitReddedilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	zone := "$ORIGIN bayialan.test.\n@ 3600 IN A 203.0.113.10\nfoo.example.org. 3600 IN A 192.0.2.9\n"
	kod, govde := zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns/ice-aktar",
		"/domains/42/dns/ice-aktar", "text/plain", zone, func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	if kod != http.StatusBadRequest || !strings.Contains(govde, "dışında") {
		t.Errorf("zone dışı satır 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 0 {
		t.Errorf("reddedilen dosyadan kayıt eklendi: %d", n)
	}
}

// Update ve toplu etkinleştirme de zone dışı adı reddeder (eski bir zone dışı pasif kayıt
// etkinleştirilemez; ada zone dışı değer yazılamaz).
func TestUpdateTopluDurum_ZoneDisiAdReddedilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "www", "A", "192.0.2.9", 600, 0, 1)
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ad":"foo.example.org."}`); kod != http.StatusBadRequest || !strings.Contains(govde, "dışında") {
		t.Errorf("PUT zone dışı ad 400 beklenirdi: %d %s", kod, govde)
	}
	eski := zi_ekle(t, db, 42, "foo.example.org.", "A", "192.0.2.10", 600, 0, 0)
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/toplu-durum", "/domains/42/dns/toplu-durum",
		fmt.Sprintf(`{"ids":[%d],"aktif":true}`, eski), func(h *Handlers) http.HandlerFunc { return h.TopluDurum })
	if kod != http.StatusBadRequest || !strings.Contains(govde, "dışında") {
		t.Errorf("zone dışı pasif kaydı etkinleştirme 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM dns_records WHERE id=%d AND aktif=0`, eski)); n != 1 {
		t.Error("zone dışı kayıt etkinleştirildi")
	}
}

// Şablondaki zone dışı mutlak ad satırı atlanır ve bildirilir; {DOMAIN} ile zone içi mutlak ad eklenir.
func TestSablon_ZoneDisiMutlakAdAtlanir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES
		('@', 'NS', 'ns1.{DOMAIN}', 86400, 0, 10, 1, 0), ('dis.example.org.', 'A', '{IP}', 3600, 0, 20, 1, 0),
		('ic.{DOMAIN}.', 'A', '{IP}', 3600, 0, 30, 1, 0)`)
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/sablon", "/domains/42/dns/sablon", "",
		func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate })
	if kod != http.StatusOK || !strings.Contains(govde, "dis.example.org.") {
		t.Errorf("zone dışı şablon satırı atlanıp bildirilmeli: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad LIKE 'dis.%'`); n != 0 {
		t.Error("zone dışı şablon satırı DB'ye yazıldı")
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad LIKE 'ic.%'`); n != 1 {
		t.Errorf("zone içi mutlak şablon satırı eklenmeli: %d", n)
	}
}
