package dns

// dogrula_test.go — KayitGecerli: tür bazlı değer doğrulaması (saf, DB'siz).
// Her "geçersiz" satırın hata metninde belirtilen iz bulunmalı (açık ileti).

import (
	"fmt"
	"strings"
	"testing"
)

func k(tip, ad, deger string, oncelik int) Kayit {
	return Kayit{Tip: tip, Ad: ad, Deger: deger, TTL: 3600, Oncelik: oncelik, Aktif: true}
}

// gecerliOrnekler — KayitGecerli'nin kabul etmesi gereken örnekler (named-checkzone
// uyumu zone_dosya_test.go'da gerçek ikiliyle de sınanır).
func gecerliOrnekler() []Kayit {
	uzunTXT := strings.Repeat("a", 2000)
	dkim := "v=DKIM1; k=rsa; p=" + strings.Repeat("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A", 12)
	return []Kayit{
		k("A", "@", "203.0.113.10", 0),
		k("a", "www", "203.0.113.10", 0), // tür küçük harf
		k("A", "", "203.0.113.10", 0),    // boş ad '@' sayılır
		k("A", "*", "203.0.113.10", 0),
		k("A", "*.dev", "203.0.113.10", 0),
		k("A", "xn--bcher-kva", "203.0.113.10", 0),
		k("A", "www.ornek.test.", "203.0.113.10", 0), // mutlak ad
		k("AAAA", "@", "2a01:4f8:1c1c::1", 0),
		k("AAAA", "www", "::1", 0),
		k("CNAME", "www", "hedef.example.com", 0),
		k("CNAME", "www", "hedef.example.com.", 0),
		k("CNAME", "sel._domainkey", "sel.dkim.saglayici.net", 0),
		k("CNAME", "x", "_acme-challenge.example.net", 0),
		k("NS", "@", "ns1.example.com", 0),
		k("NS", "alt", "ns1.example.com.", 0),
		k("PTR", "5", "host.example.com.", 0),
		k("MX", "@", "mail.example.com", 10),
		k("MX", "@", "mail.example.com.", 0),
		k("MX", "@", ".", 0), // null MX (RFC 7505)
		k("MX", "@", "mail.example.com", 65535),
		k("TXT", "@", "v=spf1 mx ~all", 0),
		k("TXT", "_dmarc", "v=DMARC1; p=quarantine; rua=mailto:postmaster@example.com", 0),
		k("TXT", "@", "a;b (c) d", 0), // düz metin tırnaklanır: ';' '(' zararsız
		k("TXT", "@", uzunTXT, 0),
		k("TXT", "default._domainkey", dkim, 0),
		k("TXT", "@", `"v=spf1 -all"`, 0),
		k("TXT", "@", `"parca1" "parca2"`, 0),
		k("TXT", "@", `"kaçışlı \" tırnak"`, 0),
		k("SRV", "_sip._tcp", "5 5060 sip.example.com", 10),
		k("SRV", "_autodiscover._tcp", "0 443 autodiscover.example.com", 0),
		k("SRV", "_x._tcp", "0 0 .", 0),
		k("CAA", "@", `0 issue "letsencrypt.org"`, 0),
		k("CAA", "@", `0 issue letsencrypt.org`, 0),
		k("CAA", "@", `0 issuewild ";"`, 0),
		k("CAA", "@", `128 iodef "mailto:guvenlik@example.com"`, 0),
		k("DS", "alt", "12345 13 2 "+strings.Repeat("AB", 32), 0),
		k("DS", "alt", "12345 13 1 "+strings.Repeat("ab", 20), 0),
		k("DS", "alt", "12345 13 2 "+strings.Repeat("AB", 16)+" "+strings.Repeat("CD", 16), 0), // bölünmüş hex
		k("TLSA", "_443._tcp", "3 1 1 0B9FA5A59EED715C26C1020C711B4F6EC42D58B0015E14337A39DAD301C5AFC3", 0),
		k("SSHFP", "@", "4 2 123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0", 0),
		k("NAPTR", "@", `100 10 "u" "E2U+sip" "!^.*$!sip:info@example.com!" .`, 0),
	}
}

func TestKayitGecerli_GecerliKayitlar(t *testing.T) {
	for _, c := range gecerliOrnekler() {
		if err := KayitGecerli(c); err != nil {
			t.Errorf("geçerli kayıt reddedildi %s %q %q: %v", c.Tip, c.Ad, c.Deger, err)
		}
	}
}

func TestKayitGecerli_GecersizKayitlar_AcikIleti(t *testing.T) {
	uzun := strings.Repeat("a", txtEnFazla+1)
	etiket64 := strings.Repeat("a", 64)
	gecersiz := []struct {
		k  Kayit
		iz string
	}{
		// --- boş değer (tüm türler)
		{k("A", "@", "", 0), "boş"},
		{k("AAAA", "@", "  ", 0), "boş"},
		{k("CNAME", "www", "", 0), "boş"},
		{k("MX", "@", "", 10), "boş"},
		{k("TXT", "@", "", 0), "boş"},
		{k("TXT", "@", `""`, 0), "boş"}, // yalnız tırnak
		{k("SRV", "_x._tcp", "", 0), "boş"},
		{k("CAA", "@", "", 0), "boş"},
		{k("NS", "@", "", 0), "boş"},
		// --- A / AAAA
		{k("A", "@", "abc", 0), "IPv4"},
		{k("A", "@", "256.1.1.1", 0), "IPv4"},
		{k("A", "@", "1.2.3", 0), "IPv4"},
		{k("A", "@", "001.2.3.4", 0), "IPv4"},
		{k("A", "@", "2a01:4f8::1", 0), "IPv4"},
		{k("A", "@", "::ffff:1.2.3.4", 0), "IPv4"},
		{k("A", "@", "1.2.3.4 5.6.7.8", 0), "IPv4"},
		{k("AAAA", "@", "203.0.113.1", 0), "IPv6"},
		{k("AAAA", "@", "2a01:4f8::zz", 0), "IPv6"},
		{k("AAAA", "@", "fe80::1%eth0", 0), "IPv6"},
		// --- CNAME / NS / PTR / MX
		{k("CNAME", "www", "203.0.113.1", 0), "IP adresi"},
		{k("CNAME", "www", "1.2.3.4.", 0), "IP adresi"},
		{k("NS", "@", "2a01:4f8::1", 0), "IP adresi"},
		{k("CNAME", "www", "hedef..example.com", 0), "boş etiket"},
		{k("CNAME", "www", "hedef example.com", 0), "boşluk"},
		{k("CNAME", "www", ".", 0), "'.'"},
		{k("CNAME", "www", "hedef.example.com;x", 0), "karakterini"},
		{k("CNAME", "www", "bücher.example.com", 0), "ASCII"},
		{k("CNAME", "www", strings.Repeat("a", 64)+".example.com", 0), "63"},
		{k("NS", "@", "ns1/x.example.com", 0), "karakterini"},
		{k("PTR", "5", "host.example.com\"", 0), "karakterini"},
		{k("MX", "@", "10 mail.example.com", 10), "öncelik"},
		{k("MX", "@", "mail.example.com", 65536), "65535"},
		{k("MX", "@", "mail.example.com", -1), "65535"},
		{k("MX", "@", "203.0.113.5", 10), "IP adresi"},
		{k("CNAME", "@", "hedef.example.com", 0), "CNAME"}, // apex CNAME zone'u bozar
		{k("CNAME", "", "hedef.example.com", 0), "CNAME"},
		// --- TXT
		{k("TXT", "@", uzun, 0), "en fazla"},
		{k("TXT", "@", `abc\`, 0), "ters eğik"},
		{k("TXT", "@", `a\"b`, 0), "ters eğik"},
		{k("TXT", "@", `a\`, 0), "ters eğik"},
		{k("TXT", "@", `"abc`, 0), "tırnak"},
		{k("TXT", "@", `"abc" ( `, 0), "tırnak"},
		{k("TXT", "@", `"abc" def`, 0), "tırnak"},
		{k("TXT", "@", `"abc"def`, 0), "kapanış"},
		{k("TXT", "@", `"`+strings.Repeat("a", 256)+`"`, 0), "255"},
		{k("TXT", "@", `"abc\"`, 0), "tırnak"},
		// --- SRV
		{k("SRV", "_x._tcp", "5060 sip.example.com", 0), "ağırlık port hedef"},
		{k("SRV", "_x._tcp", "1 2 3 sip.example.com", 0), "ağırlık port hedef"},
		{k("SRV", "_x._tcp", "5 99999 sip.example.com", 0), "port"},
		{k("SRV", "_x._tcp", "-1 5060 sip.example.com", 0), "ağırlık"},
		{k("SRV", "_x._tcp", "5 5060 203.0.113.1", 0), "IP adresi"},
		{k("SRV", "_x._tcp", "5 5060 sip.example.com", 70000), "65535"},
		// --- CAA
		{k("CAA", "@", `issue letsencrypt.org`, 0), "bayrak etiket değer"},
		{k("CAA", "@", `300 issue "x.org"`, 0), "bayrak"},
		{k("CAA", "@", `0 is-sue "x.org"`, 0), "etiket"},
		{k("CAA", "@", `0 issue x.org;foo`, 0), "tırnak"},
		{k("CAA", "@", `0 issue "x.org`, 0), "tırnak"},
		// --- DS / TLSA / SSHFP / NAPTR
		{k("DS", "alt", "12345 13 2", 0), "biçiminde"},
		{k("DS", "alt", "70000 13 2 AABB", 0), "anahtar etiketi"},
		{k("DS", "alt", "12345 13 2 XYZ1", 0), "hex"},
		{k("DS", "alt", "12345 13 2 ABC", 0), "çift"},
		{k("DS", "alt", "12345 13 2 AABB", 0), "64 onaltılık"}, // SHA-256 özeti 32 bayt
		{k("DS", "@", "12345 13 2 "+strings.Repeat("AB", 32), 0), "bölge tepesinde"},
		{k("TLSA", "@", "3 1 AABB", 0), "biçiminde"},
		{k("TLSA", "@", "3 1 1 GG", 0), "hex"},
		{k("TLSA", "@", "3 1 1 AABB", 0), "64 onaltılık"},
		{k("SSHFP", "@", "4 2", 0), "biçiminde"},
		{k("SSHFP", "@", "300 2 AABB", 0), "algoritma"},
		{k("SSHFP", "@", "4 2 AABB", 0), "64 onaltılık"},
		{k("NAPTR", "@", `100 10 "u" "E2U" "regexp"`, 0), "6 alan"},
		{k("NAPTR", "@", `100 10 u E2U regexp .`, 0), "tırnak içinde"},
		{k("NAPTR", "@", `100 10 u "E2U" "r" .`, 0), "tırnak"},
		// --- ad
		{k("A", "w w", "203.0.113.1", 0), "boşluk"},
		{k("A", "w;w", "203.0.113.1", 0), "karakterini"},
		{k("A", "w(w)", "203.0.113.1", 0), "karakterini"},
		{k("A", `w"w`, "203.0.113.1", 0), "karakterini"},
		{k("A", `w\w`, "203.0.113.1", 0), "karakterini"},
		{k("A", "a..b", "203.0.113.1", 0), "boş etiket"},
		{k("A", "büro", "203.0.113.1", 0), "ASCII"},
		{k("A", etiket64, "203.0.113.1", 0), "63"},
		{k("A", "_x", "203.0.113.1", 0), "alt çizgi"},
		{k("A", "w*", "203.0.113.1", 0), "karakterini"},
		{k("A", ".", "203.0.113.1", 0), "geçersiz"},
		// --- tür, TTL
		{k("BOGUS", "@", "x", 0), "tip"},
		{Kayit{Tip: "A", Ad: "@", Deger: "203.0.113.1", TTL: -1}, "TTL"},
		{Kayit{Tip: "A", Ad: "@", Deger: "203.0.113.1", TTL: 2147483648}, "TTL"},
		// --- MEVCUT ENJEKSİYON KORUMALARI KALIR
		{k("A", "@", "203.0.113.1\n$INCLUDE /etc/passwd", 0), "satır sonu"},
		{k("A", "@", "203.0.113.1\r", 0), "satır sonu"},
		{k("TXT", "@", "x\n$GENERATE 1-9 a A 1.2.3.$", 0), "satır sonu"},
		{k("A", "w\nw", "203.0.113.1", 0), "boşluk veya satır sonu"},
		{k("TXT", "@", "$INCLUDE /etc/passwd", 0), "'$'"},
		{k("A", "$ORIGIN", "203.0.113.1", 0), "'$'"},
		{k("A", "@", "203.0.113.1\x00", 0), "kontrol karakteri"},
		{k("A", "w\x01w", "203.0.113.1", 0), "kontrol karakteri"},
	}
	for _, c := range gecersiz {
		err := KayitGecerli(c.k)
		if err == nil {
			t.Errorf("geçersiz kayıt KABUL edildi %s %q %q (oncelik %d)", c.k.Tip, c.k.Ad, c.k.Deger, c.k.Oncelik)
			continue
		}
		if !strings.Contains(err.Error(), c.iz) {
			t.Errorf("%s %q %q: hata metninde %q yok: %v", c.k.Tip, c.k.Ad, c.k.Deger, c.iz, err)
		}
	}
}

// Öncelik yalnız MX/SRV'de doğrulanır; diğer türlerde (oncelikNormalize 0'a çeker) yok sayılır.
func TestKayitGecerli_OncelikYalnizMXveSRV(t *testing.T) {
	if err := KayitGecerli(k("A", "@", "203.0.113.1", 999999)); err != nil {
		t.Errorf("A kaydında öncelik doğrulanmamalı: %v", err)
	}
	if err := KayitGecerli(k("SRV", "_x._tcp", "0 5060 sip.example.com", 65536)); err == nil {
		t.Error("SRV önceliği 65535'i aşamaz")
	}
}

// Hata metni 400 ile istemciye aynen gider: Türkçe ve tür adı geçmeli.
func TestKayitGecerli_IletiTuruSoyler(t *testing.T) {
	err := KayitGecerli(k("A", "www", "abc", 0))
	if err == nil || !strings.Contains(err.Error(), "A kaydı") || !strings.Contains(err.Error(), "203.0.113.10") {
		t.Errorf("ileti tür adını ve örneği içermeli: %v", err)
	}
}

// ---------------------------------------------------------------- sınır değerleri ve yardımcılar

func TestKayitGecerli_SinirDegerleri(t *testing.T) {
	etiket := func(n int) string { return strings.Repeat("a", n) }
	ad253 := strings.Join([]string{etiket(63), etiket(63), etiket(63), etiket(61)}, ".")
	ad254 := strings.Join([]string{etiket(63), etiket(63), etiket(63), etiket(62)}, ".")
	ad255 := strings.Join([]string{etiket(63), etiket(63), etiket(63), etiket(63)}, ".")
	gecerli := []Kayit{
		{Tip: "A", Ad: "@", Deger: "203.0.113.1", TTL: 0},
		{Tip: "A", Ad: "@", Deger: "203.0.113.1", TTL: 2147483647},
		k("SRV", "_x._tcp", "0 0 .", 65535),
		k("SRV", "_x._tcp", "65535 65535 sip.example.com", 0),
		k("TXT", "@", strings.Repeat("a", 3000), 0),
		k("TXT", "@", strings.Repeat("a", txtEnFazla), 0),
		k("TLSA", "_443._tcp", "3 1 0 "+strings.Repeat("ab", 1021), 0), // değer tam 2048 bayt
		k("A", etiket(63), "203.0.113.1", 0),
		k("A", ad253, "203.0.113.1", 0),
		k("TXT", "az.AZ.09.a-z_Z9", "x", 0),
		k("CNAME", "www", ad253, 0),
		k("CNAME", "www", "hedef-1.example.com", 0),
		k("CNAME", "www", "azAZ09.example.com", 0), // hedef etiketinde sınır karakterleri
		k("CNAME", "www", "x.9a", 0),               // son etiket rakamla BAŞLAYIP harf içerirse alan adıdır
		k("CAA", "@", `255 issue "x.org"`, 0),
		k("CAA", "@", `0 azAZ09 "x.org"`, 0),
		k("TLSA", "@", "3 1 0 0123456789abcdefABCDEF", 0),
		k("TLSA", "@", "255 255 255 aabb", 0),
		k("SSHFP", "@", "255 255 aabb", 0),
		k("TLSA", "@", "3 1 1 "+strings.Repeat("ab", 16)+" "+strings.Repeat("cd", 16), 0), // özet birden çok parçaya bölünmüş
		k("SSHFP", "@", "4 2 "+strings.Repeat("ab", 16)+" "+strings.Repeat("cd", 16), 0),  // özet birden çok parçaya bölünmüş
		k("DS", "alt", "65535 255 255 aabb", 0),
		k("DS", "alt", "12345 13 7 aabb", 0), // bilinmeyen özet türü: uzunluk serbest
		k("NAPTR", "@", `65535 65535 "u" "E2U" "r" .`, 0),
		k("NAPTR", "@", `1 1 "`+strings.Repeat("a", 255)+`" "E2U" "r" .`, 0),
		k("TXT", "@", `"`+strings.Repeat("a", 255)+`"`, 0),
		k("TXT", "@", `"a\255b"`, 0),
		k("TXT", "@", `"a\025b"`, 0),
		k("TXT", "@", `"a\12b"`, 0), // iki rakam: \1 ve "2"
		k("TXT", "@", `"abc\\"`, 0), // kaçışlı ters bölü
	}
	// Özet uzunlukları: her bilinen tür TAM beklenen uzunlukta geçerli, ±2 hex karakterde geçersizdir.
	hx := func(n int) string { return strings.Repeat("a", n) }
	var gecersizUzunluk []Kayit
	for ot, n := range map[int]int{1: 40, 2: 64, 3: 64, 4: 96} {
		gecerli = append(gecerli, k("DS", "alt", fmt.Sprintf("12345 13 %d %s", ot, hx(n)), 0))
		gecersizUzunluk = append(gecersizUzunluk, k("DS", "alt", fmt.Sprintf("12345 13 %d %s", ot, hx(n-2)), 0),
			k("DS", "alt", fmt.Sprintf("12345 13 %d %s", ot, hx(n+2)), 0))
	}
	for et, n := range map[int]int{1: 64, 2: 128} {
		gecerli = append(gecerli, k("TLSA", "_443._tcp", fmt.Sprintf("3 1 %d %s", et, hx(n)), 0))
		gecersizUzunluk = append(gecersizUzunluk, k("TLSA", "_443._tcp", fmt.Sprintf("3 1 %d %s", et, hx(n-2)), 0),
			k("TLSA", "_443._tcp", fmt.Sprintf("3 1 %d %s", et, hx(n+2)), 0))
	}
	for it, n := range map[int]int{1: 40, 2: 64} {
		gecerli = append(gecerli, k("SSHFP", "@", fmt.Sprintf("4 %d %s", it, hx(n)), 0))
		gecersizUzunluk = append(gecersizUzunluk, k("SSHFP", "@", fmt.Sprintf("4 %d %s", it, hx(n-2)), 0),
			k("SSHFP", "@", fmt.Sprintf("4 %d %s", it, hx(n+2)), 0))
	}
	for _, c := range gecerli {
		if err := KayitGecerli(c); err != nil {
			t.Errorf("sınırda GEÇERLİ kayıt reddedildi %s %.40q %.60q: %v", c.Tip, c.Ad, c.Deger, err)
		}
	}
	for _, c := range gecersizUzunluk {
		if err := KayitGecerli(c); err == nil || !strings.Contains(err.Error(), "onaltılık karakter") {
			t.Errorf("yanlış özet uzunluğu reddedilmeliydi %s %q: %v", c.Tip, c.Deger, err)
		}
	}

	gecersiz := []struct {
		k  Kayit
		iz string
	}{
		{k("SRV", "_x._tcp", "0 0 .", 65536), "65535"},
		{k("SRV", "_x._tcp", "65536 5060 sip.example.com", 0), "ağırlık"},
		{k("SRV", "_x._tcp", "5 65536 sip.example.com", 0), "port"},
		{k("TLSA", "_443._tcp", "3 1 0 "+strings.Repeat("ab", 1022), 0), "en fazla 2048"},
		{k("A", ad254, "203.0.113.1", 0), "253"},
		{k("A", ad255, "203.0.113.1", 0), "253"},
		{k("CNAME", "www", ad254, 0), "253"},
		{k("CNAME", "www", ad255, 0), "253"},
		{k("CNAME", "www", "host.0", 0), "son etiket"},
		{k("CNAME", "www", "host.9", 0), "son etiket"},
		{k("CNAME", "www", "host.90", 0), "son etiket"},
		{k("A", "a`b", "203.0.113.1", 0), "karakterini"},
		{k("A", "a{b", "203.0.113.1", 0), "karakterini"},
		{k("A", "a@b", "203.0.113.1", 0), "karakterini"},
		{k("A", "a[b", "203.0.113.1", 0), "karakterini"},
		{k("A", "a/b", "203.0.113.1", 0), "karakterini"},
		{k("A", "a:b", "203.0.113.1", 0), "karakterini"},
		{k("AAAA", "_x", "::1", 0), "alt çizgi"},
		{k("CNAME", "www", "hedef\texample.com", 0), "boşluk"},
		{k("CNAME", "www", "hedef.123", 0), "son etiket"},
		{k("CNAME", "www", "1.2.3", 0), "son etiket"},
		{k("CNAME", "www", "a@b.example.com", 0), "karakterini"},
		{k("CNAME", "www", "a:b.example.com", 0), "karakterini"},
		{k("CNAME", "www", "a`b.example.com", 0), "karakterini"},
		{k("CNAME", "www", "a{b.example.com", 0), "karakterini"},
		{k("CAA", "@", `256 issue "x.org"`, 0), "bayrak"},
		{k("CAA", "@", `0 is_sue "x.org"`, 0), "etiket"},
		{k("CAA", "@", `0 issue x(y`, 0), "tırnak"},
		{k("CAA", "@", `0 issue x)y`, 0), "tırnak"},
		{k("CAA", "@", `0 issue x\y`, 0), "tırnak"},
		{k("CAA", "@", `0 issue "x.org" "y.org"`, 0), "bayrak etiket değer"},
		{k("CAA", "@", `0 issue`, 0), "bayrak etiket değer"},
		{k("CAA", "@", `0 issue "x\400"`, 0), "255"},
		{k("DS", "alt", "65536 13 2 aabb", 0), "anahtar etiketi"},
		{k("DS", "alt", "12345 256 2 aabb", 0), "algoritma"},
		{k("DS", "alt", "12345 13 256 aabb", 0), "özet türü"},
		{k("TLSA", "@", "256 1 0 aabb", 0), "kullanım"},
		{k("TLSA", "@", "3 256 0 aabb", 0), "seçici"},
		{k("TLSA", "@", "3 1 256 aabb", 0), "eşleşme türü"},
		{k("TLSA", "@", "3 1 0 0g", 0), "hex"},
		{k("TLSA", "@", "3 1 0 0G", 0), "hex"},
		{k("TLSA", "@", "3 1 0 0/", 0), "hex"},
		{k("TLSA", "@", "3 1 0 0:", 0), "hex"},
		{k("TLSA", "@", "3 1 0 0@", 0), "hex"},
		{k("TLSA", "@", "3 1 0 0`", 0), "hex"},
		{k("SSHFP", "@", "256 1 aabb", 0), "algoritma"},
		{k("SSHFP", "@", "4 256 aabb", 0), "parmak izi türü"},
		{k("NAPTR", "@", `65536 1 "u" "E2U" "r" .`, 0), "order"},
		{k("NAPTR", "@", `1 65536 "u" "E2U" "r" .`, 0), "pref"},
		{k("NAPTR", "@", `1 1 "`+strings.Repeat("a", 256)+`" "E2U" "r" .`, 0), "255"},
		{k("NAPTR", "@", `1 1 "u" "E2U" "a\256" .`, 0), "255"},
		{k("NAPTR", "@", `1 1 "u" "E2U" "r" 203.0.113.1`, 0), "IP adresi"},
		{k("NAPTR", "@", `1 1 "u" "E2U" "r" . fazla`, 0), "6 alan"},
		{k("TXT", "@", `"a\256b"`, 0), "255"},
		{k("TXT", "@", `"" ""`, 0), "boş"},
	}
	for _, c := range gecersiz {
		err := KayitGecerli(c.k)
		if err == nil {
			t.Errorf("sınırda GEÇERSİZ kayıt kabul edildi %s %.40q %.60q", c.k.Tip, c.k.Ad, c.k.Deger)
			continue
		}
		if !strings.Contains(err.Error(), c.iz) {
			t.Errorf("%s %.40q %.60q: hata metninde %q yok: %v", c.k.Tip, c.k.Ad, c.k.Deger, c.iz, err)
		}
	}
}

func TestDogrulaYardimcilari_Dogrudan(t *testing.T) {
	// hexGecerli
	for _, p := range [][]string{nil, {""}, {"abc"}, {"zz"}} {
		if err := hexGecerli(p, 0); err == nil {
			t.Errorf("hexGecerli(%q) hata vermeliydi", p)
		}
	}
	if err := hexGecerli([]string{"aa", "BB"}, 4); err != nil {
		t.Errorf("parçalı geçerli hex reddedildi: %v", err)
	}
	// sayiAraligi
	for _, c := range []struct {
		s   string
		max uint64
		n   uint64
		ok  bool
	}{
		{"0", 255, 0, true}, {"255", 255, 255, true}, {"256", 255, 0, false}, {"", 255, 0, false},
		{"12a", 255, 0, false}, {"-1", 255, 0, false}, {"99999999999999999999", 255, 0, false}, {"007", 255, 7, true},
	} {
		n, ok := sayiAraligi(c.s, c.max)
		if ok != c.ok || (ok && n != c.n) {
			t.Errorf("sayiAraligi(%q,%d) = %d,%v; beklenen %d,%v", c.s, c.max, n, ok, c.n, c.ok)
		}
	}
	// kacisKontrol
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{`abc`, true}, {`a\b`, true}, {`a\\`, true}, {`a\255`, true}, {`a\256`, false}, {`a\25`, true}, {`abc\`, false}, {`\999x`, false},
	} {
		if err := kacisKontrol(c.s); (err == nil) != c.ok {
			t.Errorf("kacisKontrol(%q): hata=%v, beklenen ok=%v", c.s, err, c.ok)
		}
	}
	// tirnakliBol
	for _, c := range []struct {
		s   string
		bek []string
		iz  string
	}{
		{`a "b c" d`, []string{`a`, `"b c"`, `d`}, ""},
		{"  a \t b  ", []string{"a", "b"}, ""},
		{`"a\"b"`, []string{`"a\"b"`}, ""},
		{`"a\\"`, []string{`"a\\"`}, ""},
		{"\"a\"\t\"b\"", []string{`"a"`, `"b"`}, ""},
		{`"abc`, nil, "kapanmamış"},
		{`"abc"def`, nil, "kapanış"},
		{`ab"c`, nil, "ortasında"},
	} {
		got, err := tirnakliBol(c.s)
		if c.iz != "" {
			if err == nil || !strings.Contains(err.Error(), c.iz) {
				t.Errorf("tirnakliBol(%q): %q içeren hata beklenirdi: %v", c.s, c.iz, err)
			}
			continue
		}
		if err != nil || strings.Join(got, "|") != strings.Join(c.bek, "|") {
			t.Errorf("tirnakliBol(%q) = %q, %v; beklenen %q", c.s, got, err, c.bek)
		}
	}
}
