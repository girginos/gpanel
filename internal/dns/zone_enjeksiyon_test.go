package dns

// zone_enjeksiyon_test.go — SOA ve şablon alanlarından zone satır/yönerge enjeksiyonu.

import (
	"net/http"
	"strings"
	"testing"

	"girginospanel/internal/sahtedb"
)

func TestSOAAlanlariGecerli(t *testing.T) {
	kotu := [][2]string{
		{"ns1.x.com", "a@x.com\n$INCLUDE /etc/passwd"},
		{"ns1.x.com\n$INCLUDE /etc/passwd", "a@x.com"},
		{"ns1.x.com ; yorum", "a@x.com"},
		{"ns1.x.com", "a@x.com ("},
		{"ns1.x.com", "$GENERATE 1-9 a A 1.2.3.$"},
		{"ns1.x.com", "a\"@x.com"},
	}
	for _, c := range kotu {
		if SOAAlanlariGecerli(c[0], c[1]) == nil {
			t.Errorf("kabul edildi: %q %q", c[0], c[1])
		}
	}
	if err := SOAAlanlariGecerli("ns1.ornek-alan.com.", "admin@ornek-alan.com"); err != nil {
		t.Errorf("geçerli reddedildi: %v", err)
	}
}

func TestKayitAlanlariGecerli_KontrolKarakteri(t *testing.T) {
	if KayitAlanlariGecerli("www", "1.2.3.4\x00") == nil {
		t.Error("NUL kabul edildi")
	}
	if KayitAlanlariGecerli("w\x01w", "1.2.3.4") == nil {
		t.Error("kontrol karakteri adda kabul edildi")
	}
}

func TestPutSOA_EnjeksiyonuReddeder(t *testing.T) {
	tok := zh_tokenlar(t)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT alan_adi, is_demo FROM domains", []any{"bayialan.test", int64(0)})
	kod, govde := zh_cagir(t, db, tok["müşteri"], http.MethodPut, "/domains/{id}/dns/soa", "/domains/42/dns/soa",
		"", `{"hostmaster":"a@b.test\n$INCLUDE /etc/passwd"}`, func(h *Handlers) http.HandlerFunc { return h.PutSOA })
	if kod != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d: %s", kod, govde)
	}
	if !strings.Contains(govde, "hostmaster") {
		t.Errorf("hata metni: %s", govde)
	}
}

func TestPutTemplate_EnjeksiyonuReddeder(t *testing.T) {
	tok := zh_tokenlar(t)
	_, db := sahtedb.Yeni()
	kod, govde := zh_cagir(t, db, tok["bayi"], http.MethodPut, "/dns-template", "/dns-template",
		"", `{"kayitlar":[{"ad":"@","tip":"TXT","deger":"\"x\"\n$INCLUDE /etc/passwd","ttl":300,"aktif":true}]}`,
		func(h *Handlers) http.HandlerFunc { return h.PutTemplate })
	if kod != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d: %s", kod, govde)
	}
}
