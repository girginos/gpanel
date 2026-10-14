package dns

// txt_kacis_test.go — kaçışlı TXT değerleri (BIND/Cloudflare dışa aktarımındaki DKIM/DMARC
// "v=DKIM1\; k=rsa\; ..."): doğrulayıcı geçerli kaçışları kabul eder, zone yazımı kaçışı
// 255'lik parça sınırında bölmez, içe aktarıcı kaçışlı tırnağı tırnak sonu saymaz.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestTxtGecerli_DuzMetinKacislari(t *testing.T) {
	for _, d := range []string{`v=DKIM1\; k=rsa\; p=MIIBAQ`, `v=DMARC1\; p=none`, `a\\b`, `x\059y`} {
		if err := txtGecerli(d); err != nil {
			t.Errorf("%q kabul edilmeliydi: %v", d, err)
		}
	}
	for _, d := range []string{`a\`, `a\256`, `a\"b`, `v=DKIM1\; "p"`} {
		if err := txtGecerli(d); err == nil {
			t.Errorf("%q reddedilmeliydi", d)
		}
	}
}

// txtCoz — BIND'in tırnak içi kaçış çözümü (\DDD ondalık bayt, \X X).
func txtCoz(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if i+3 < len(s) && rakamMi(s[i+1]) && rakamMi(s[i+2]) && rakamMi(s[i+3]) {
				n, _ := strconv.Atoi(s[i+1 : i+4])
				b = append(b, byte(n))
				i += 3
				continue
			}
			b = append(b, s[i+1])
			i++
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}

// Bölünen her parça ≤255 bayt, kaçışı bölünmemiş ve parçalar çözülünce özgün metni verir.
func TestTxtQuote_KacisOrtadanBolunmez(t *testing.T) {
	for _, s := range []string{
		strings.Repeat("a", 254) + `\;` + strings.Repeat("b", 300),
		strings.Repeat("a", 253) + `\059` + strings.Repeat("b", 300),
		strings.Repeat("a", 252) + `\059` + strings.Repeat("b", 300),
		strings.Repeat(`\;`, 300),
	} {
		parcalar, err := tirnakliBol(txtQuote(s))
		if err != nil {
			t.Fatalf("txtQuote çıktısı ayrıştırılamadı: %v", err)
		}
		var cozulen strings.Builder
		for _, p := range parcalar {
			ic := p[1 : len(p)-1]
			if len(ic) > 255 {
				t.Errorf("parça 255 baytı aşıyor: %d", len(ic))
			}
			if err := kacisKontrol(ic); err != nil {
				t.Errorf("parça sonunda kaçış bölünmüş: %v", err)
			}
			cozulen.WriteString(txtCoz(ic))
		}
		if cozulen.String() != txtCoz(s) {
			t.Errorf("parçalar çözülünce özgün metni vermiyor (len %d)", len(s))
		}
	}
}

func TestUnquoteTXT_Kacislar(t *testing.T) {
	for _, d := range []struct {
		toks     []string
		beklenen string
	}{
		{[]string{`"v=DKIM1\;`, `k=rsa\;`, `"`, `"p=MIIB"`}, `v=DKIM1\; k=rsa\; p=MIIB`},
		{[]string{`"a\"b"`}, `"a\"b"`}, // kaçışlı tırnak: tırnaklı biçim korunur
		{[]string{`"ab"`, `"cd"`}, `abcd`},
		{[]string{`"x\\"`}, `x\\`},
		{[]string{`duz`}, `duz`},
	} {
		got := unquoteTXT(d.toks)
		if got != d.beklenen {
			t.Errorf("unquoteTXT(%q) = %q, beklenen %q", d.toks, got, d.beklenen)
		}
		if err := txtGecerli(got); err != nil {
			t.Errorf("unquoteTXT(%q) sonucu doğrulamadan geçmeli: %v", d.toks, err)
		}
	}
}

// Cloudflare/BIND dışa aktarımındaki kaçışlı DKIM/DMARC satırları içe aktarılır.
func TestIceAktar_KacisliDKIMKabulEdilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	zone := "$ORIGIN bayialan.test.\n" +
		`default._domainkey 3600 IN TXT "v=DKIM1\; k=rsa\; " "p=MIIBIjAN"` + "\n" +
		`_dmarc 3600 IN TXT "v=DMARC1\; p=none"` + "\n"
	kod, govde := zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns/ice-aktar",
		"/domains/42/dns/ice-aktar?mod=degistir", "text/plain", zone, func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	if kod != http.StatusOK {
		t.Fatalf("kaçışlı DKIM/DMARC içe aktarılmalı: %d %s", kod, govde)
	}
	var deger string
	if err := db.QueryRow(`SELECT deger FROM dns_records WHERE domain_id=42 AND tip='TXT' AND ad LIKE 'default._domainkey%'`).Scan(&deger); err != nil {
		t.Fatal(err)
	}
	if deger != `v=DKIM1\; k=rsa\; p=MIIBIjAN` {
		t.Errorf("DKIM değeri kaçışlarıyla saklanmalı: %q", deger)
	}
}
