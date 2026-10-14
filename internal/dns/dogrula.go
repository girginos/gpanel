package dns

// dogrula.go — DNS kaydı için TEK doğrulayıcı (KayitGecerli).
//
// Create, Update, toplu-durum (etkinleştirme), şablon uygulama ve zone içe aktarma
// uçlarının HEPSİ kaydı buradan geçirir. Amaç, zone'u (named-checkzone) bozacak bir
// kaydın DB'ye hiç girmemesidir: tek bozuk kayıt o hesabın sonraki tüm DNS
// güncellemelerinin zone yazımında takılmasına yol açar.
//
// Mevcut enjeksiyon korumaları (satır sonu, kontrol karakteri, '$' yönergesi:
// KayitAlanlariGecerli) AYNEN korunur ve ilk adımda çalışır.

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	// enBuyukTTL — BIND/RFC 2181 üst sınırı (2^31-1); fazlası zone'u reddettirir.
	enBuyukTTL = 2147483647
	// degerEnFazla — TXT dışındaki türlerde değer uzunluğu sınırı (bayt).
	degerEnFazla = 2048
	// txtEnFazla — TXT değerinin toplam uzunluğu sınırı (bayt). 4096-bit RSA DKIM
	// anahtarı (~800 bayt) ve uzun SPF zincirleri rahatça sığar.
	txtEnFazla = 4096
)

// KayitGecerli — bir DNS kaydının (tür + ad + değer + öncelik + TTL) zone'a yazılabilir
// olup olmadığını söyler. Hata metni istemciye 400 ile aynen döner (Türkçe, açık).
// Tür büyük/küçük harf duyarsızdır. Ad boşsa '@' sayılır (Create'in varsayılanı).
func KayitGecerli(k Kayit) error {
	tip := strings.ToUpper(strings.TrimSpace(k.Tip))
	if !gecerliTip(tip) {
		return errors.New("geçersiz DNS tipi")
	}
	ad := k.Ad
	if ad == "" {
		ad = "@"
	}
	if err := KayitAlanlariGecerli(ad, k.Deger); err != nil {
		return err
	}
	if err := adGecerli(ad, tip); err != nil {
		return err
	}
	if k.TTL < 0 || k.TTL > enBuyukTTL {
		return fmt.Errorf("TTL 0 ile %d arasında olmalı", enBuyukTTL)
	}
	if tip == "MX" || tip == "SRV" {
		if k.Oncelik < 0 || k.Oncelik > 65535 {
			return errors.New(tip + " önceliği 0 ile 65535 arasında olmalı")
		}
	}
	if tip == "DS" && ad == "@" {
		return errors.New("DS kaydı bölge tepesinde (@) olamaz; alt alan delegasyonu içindir (alan adının DS'i kayıt kuruluşuna girilir)")
	}
	deger := strings.TrimSpace(k.Deger)
	if deger == "" {
		return errors.New(tip + " kaydının değeri boş olamaz")
	}
	sinir := degerEnFazla
	if tip == "TXT" {
		sinir = txtEnFazla
	}
	if len(deger) > sinir {
		return fmt.Errorf("%s kaydının değeri en fazla %d karakter olabilir", tip, sinir)
	}
	if err := degerGecerli(tip, deger); err != nil {
		return fmt.Errorf("%s kaydı değeri geçersiz: %w", tip, err)
	}
	return nil
}

// adGecerli — kayıt adı: '@', ya da '.' ile ayrılmış etiketler (a-z A-Z 0-9 '-' '_',
// tek başına '*' joker). Zone söz dizimini bozan ';' '(' ')' '"' '\' reddedilir.
func adGecerli(ad, tip string) error {
	if ad == "@" {
		if tip == "CNAME" {
			return errors.New("ana alan adında (@) CNAME kullanılamaz (SOA/NS ile birlikte bulunamaz)")
		}
		return nil
	}
	a := strings.TrimSuffix(ad, ".") // mutlak yazım serbest
	if a == "" {
		return errors.New("kayıt adı geçersiz")
	}
	if len(a) > 253 {
		return errors.New("kayıt adı en fazla 253 karakter olabilir")
	}
	for _, e := range strings.Split(a, ".") {
		if e == "" {
			return errors.New("kayıt adında boş etiket var (art arda nokta)")
		}
		if len(e) > 63 {
			return errors.New("kayıt adındaki her etiket en fazla 63 karakter olabilir")
		}
		if e == "*" {
			continue
		}
		for _, c := range e {
			if c > 0x7e {
				return errors.New("kayıt adı yalnız ASCII karakter içerebilir (Unicode adlar için punycode, xn--, kullanın)")
			}
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("kayıt adı %q karakterini içeremez (yalnız harf, rakam, '-', '_', joker '*')", string(c))
			}
		}
		if (tip == "A" || tip == "AAAA") && strings.Contains(e, "_") {
			return errors.New("A/AAAA kayıt adı alt çizgi (_) içeremez")
		}
	}
	return nil
}

// hedefAdiGecerli — CNAME/NS/PTR/MX/SRV hedefi: alan adı (sondaki nokta serbest).
// noktaIzin: tek başına "." (MX null-hedef, SRV "hizmet yok") kabul edilir.
func hedefAdiGecerli(deger string, noktaIzin bool) error {
	if deger == "." {
		if noktaIzin {
			return nil
		}
		return errors.New("'.' bu türde kullanılamaz, bir alan adı girin")
	}
	if strings.ContainsAny(deger, " \t") {
		return errors.New("alan adı boşluk içeremez")
	}
	d := strings.TrimSuffix(deger, ".")
	if len(d) > 253 {
		return errors.New("alan adı en fazla 253 karakter olabilir")
	}
	if _, err := netip.ParseAddr(d); err == nil {
		return errors.New("IP adresi olamaz, bir alan adı girin (IP için A/AAAA kaydı kullanın)")
	}
	etiketler := strings.Split(d, ".")
	for _, e := range etiketler {
		if e == "" {
			return errors.New("alan adında boş etiket var (art arda nokta)")
		}
		if len(e) > 63 {
			return errors.New("alan adındaki her etiket en fazla 63 karakter olabilir")
		}
		for _, c := range e {
			if c > 0x7e {
				return errors.New("alan adı yalnız ASCII karakter içerebilir (Unicode adlar için punycode, xn--, kullanın)")
			}
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("alan adı %q karakterini içeremez", string(c))
			}
		}
	}
	if son := etiketler[len(etiketler)-1]; strings.Trim(son, "0123456789") == "" {
		return errors.New("son etiket yalnız rakamdan oluşamaz (IP adresi gibi görünüyor), bir alan adı girin")
	}
	return nil
}

// sayiAraligi — "0".."max" arası yalnız rakamlardan oluşan tamsayı.
func sayiAraligi(s string, max uint64) (uint64, bool) {
	n, err := strconv.ParseUint(s, 10, 64) // işaret, boşluk, '_' ve boş dize kabul etmez
	return n, err == nil && n <= max
}

// hexGecerli — boşlukla bölünmüş onaltılık parçalar (DS özeti, TLSA verisi, SSHFP).
// beklenen>0 ise toplam hex karakter sayısı tam bu olmalı (özet algoritmasının uzunluğu:
// kısa özet zone'u "unexpected end of input" ile reddettirir).
func hexGecerli(parcalar []string, beklenen int) error {
	h := strings.Join(parcalar, "")
	if h == "" || len(h)%2 != 0 {
		return errors.New("onaltılık (hex) veri çift sayıda karakter olmalı")
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return errors.New("onaltılık (hex) veride geçersiz karakter var")
		}
	}
	if beklenen > 0 && len(h) != beklenen {
		return fmt.Errorf("bu özet türü için veri %d onaltılık karakter olmalı (%d verildi)", beklenen, len(h))
	}
	return nil
}

// hexUzunlukları — özet/eşleşme türüne göre beklenen hex karakter sayısı (bilinmeyen tür: serbest).
var (
	dsOzetHex    = map[uint64]int{1: 40, 2: 64, 3: 64, 4: 96}
	tlsaEslesHex = map[uint64]int{1: 64, 2: 128}
	sshfpOzetHex = map[uint64]int{1: 40, 2: 64}
)

// tirnakliBol — boşlukla böler; "..." arası (\" kaçışlı) tek parçadır ve tırnaklarıyla
// döner. Kapanmamış tırnak ya da tırnaktan sonra bitişik karakter hatadır.
func tirnakliBol(s string) ([]string, error) {
	var out []string
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		j := i
		if c == '"' {
			j = i + 1
			for {
				if j >= len(s) {
					return nil, errors.New("kapanmamış tırnak")
				}
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			if j < len(s) && s[j] != ' ' && s[j] != '\t' {
				return nil, errors.New("kapanış tırnağından sonra boşluk bekleniyordu")
			}
		} else {
			for j < len(s) && s[j] != ' ' && s[j] != '\t' {
				if s[j] == '"' {
					return nil, errors.New("tırnak işareti bir parçanın ortasında olamaz")
				}
				j++
			}
		}
		out = append(out, s[i:j])
		i = j
	}
	return out, nil
}

// kacisKontrol — zone kaçış dizileri: '\' sonrası ya tek karakter ya da 3 rakam (≤255).
// Sondaki tek '\' kapanış tırnağını kaçırır ve zone'u bozar.
func kacisKontrol(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			continue
		}
		if i+1 >= len(s) {
			return errors.New("ters eğik çizgi (\\) ile bitemez")
		}
		if i+3 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && s[i+2] >= '0' && s[i+2] <= '9' && s[i+3] >= '0' && s[i+3] <= '9' {
			if n, _ := strconv.Atoi(s[i+1 : i+4]); n > 255 {
				return errors.New("\\DDD kaçış dizisi 255'i aşamaz")
			}
		}
		i++ // kaçırılan karakteri (\\ \" ya da \DDD'nin ilk rakamı) atla
	}
	return nil
}

// txtGecerli — TXT değeri. İki biçim kabul edilir:
//   - düz metin: zone yazımında tırnaklanır/255'lik parçalara bölünür (txtQuote;
//     kaçış ortadan bölünmez); kaçışlar geçerli olmalı, tırnakla birlikte kullanılamaz;
//   - tırnaklı: "a" "b" — her parça en çok 255 bayt, kaçışlar geçerli, parçalar arası yalnız boşluk.
//
// Parantez ve ';' tırnaklı ya da düz metinde sorun değildir; dengesiz tırnak ve dengesiz
// parantez (tırnak dışı) sonraki satırları yutup zone'u bozar, reddedilir.
func txtGecerli(deger string) error {
	if !strings.HasPrefix(deger, `"`) {
		if strings.Contains(deger, `\`) {
			// Geçerli kaçışlar (BIND/Cloudflare dışa aktarımındaki DKIM "v=DKIM1\; k=rsa\; ...")
			// zone'a aynen yazılır. Tırnakla birlikte olamaz: txtQuote tırnakları siler,
			// '\"' tek '\' bırakıp kapanış tırnağını kaçırırdı.
			if strings.Contains(deger, `"`) {
				return errors.New("düz metinde ters eğik çizgi (\\) ile tırnak (\") birlikte kullanılamaz; değeri tırnak içinde verin")
			}
			return kacisKontrol(deger)
		}
		return nil
	}
	parcalar, err := tirnakliBol(deger)
	if err != nil {
		return err
	}
	toplam := 0
	for _, p := range parcalar {
		if !strings.HasPrefix(p, `"`) {
			return errors.New("tırnaklı TXT parçaları arasında yalnız boşluk olabilir")
		}
		ic := p[1 : len(p)-1]
		toplam += len(ic)
		if len(ic) > 255 {
			return errors.New("tırnaklı her TXT parçası en fazla 255 bayt olabilir (daha uzunsa tırnaksız yazın, panel böler)")
		}
		if err := kacisKontrol(ic); err != nil {
			return err
		}
	}
	if toplam == 0 {
		return errors.New("değer boş olamaz")
	}
	return nil
}

// degerGecerli — türe göre değer söz dizimi.
func degerGecerli(tip, deger string) error {
	switch tip {
	case "A":
		if a, _ := netip.ParseAddr(deger); !a.Is4() { // ayrıştırma hatasında a sıfır değerdir: Is4() false
			return errors.New("geçerli bir IPv4 adresi olmalı (örn. 203.0.113.10)")
		}
	case "AAAA":
		if a, _ := netip.ParseAddr(deger); !a.Is6() || a.Zone() != "" { // hatada a sıfır değer: Is6() false
			return errors.New("geçerli bir IPv6 adresi olmalı (örn. 2a01:4f8:1c1c::1)")
		}
	case "CNAME", "NS", "PTR":
		return hedefAdiGecerli(deger, false)
	case "MX":
		if err := hedefAdiGecerli(deger, true); err != nil {
			return fmt.Errorf("%w (MX değeri yalnız posta sunucusunun alan adıdır; öncelik 'oncelik' alanında verilir)", err)
		}
	case "TXT":
		return txtGecerli(deger)
	case "SRV":
		f := strings.Fields(deger)
		if len(f) != 3 {
			return errors.New("\"ağırlık port hedef\" biçiminde olmalı (örn. 5 5060 sip.example.com); öncelik 'oncelik' alanında verilir")
		}
		if _, ok := sayiAraligi(f[0], 65535); !ok {
			return errors.New("ağırlık 0 ile 65535 arasında olmalı")
		}
		if _, ok := sayiAraligi(f[1], 65535); !ok {
			return errors.New("port 0 ile 65535 arasında olmalı")
		}
		return hedefAdiGecerli(f[2], true)
	case "CAA":
		f, err := tirnakliBol(deger)
		if err != nil {
			return err
		}
		if len(f) != 3 {
			return errors.New("\"bayrak etiket değer\" biçiminde olmalı (örn. 0 issue \"letsencrypt.org\")")
		}
		if _, ok := sayiAraligi(f[0], 255); !ok {
			return errors.New("bayrak 0 ile 255 arasında olmalı")
		}
		if strings.Trim(f[1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
			return errors.New("etiket yalnız harf ve rakam içerebilir (issue, issuewild, iodef)")
		}
		if strings.HasPrefix(f[2], `"`) {
			return kacisKontrol(f[2][1 : len(f[2])-1])
		}
		if strings.ContainsAny(f[2], `;()\`) {
			return errors.New("tırnaksız değer ';' '(' ')' '\\' içeremez; değeri tırnak içinde verin")
		}
	case "DS":
		f := strings.Fields(deger)
		if len(f) < 4 {
			return errors.New("\"anahtar-etiketi algoritma özet-türü özet\" biçiminde olmalı")
		}
		var ozetTuru uint64
		for i, ad := range []string{"anahtar etiketi", "algoritma", "özet türü"} {
			max := uint64(255)
			if i == 0 {
				max = 65535
			}
			n, ok := sayiAraligi(f[i], max)
			if !ok {
				return fmt.Errorf("%s 0 ile %d arasında olmalı", ad, max)
			}
			ozetTuru = n
		}
		return hexGecerli(f[3:], dsOzetHex[ozetTuru])
	case "TLSA":
		f := strings.Fields(deger)
		if len(f) < 4 {
			return errors.New("\"kullanım seçici eşleşme veri\" biçiminde olmalı")
		}
		var eslesme uint64
		for i, ad := range []string{"kullanım", "seçici", "eşleşme türü"} {
			n, ok := sayiAraligi(f[i], 255)
			if !ok {
				return fmt.Errorf("%s 0 ile 255 arasında olmalı", ad)
			}
			eslesme = n
		}
		return hexGecerli(f[3:], tlsaEslesHex[eslesme])
	case "SSHFP":
		f := strings.Fields(deger)
		if len(f) < 3 {
			return errors.New("\"algoritma parmak-izi-türü parmak-izi\" biçiminde olmalı")
		}
		var izTuru uint64
		for i, ad := range []string{"algoritma", "parmak izi türü"} {
			n, ok := sayiAraligi(f[i], 255)
			if !ok {
				return fmt.Errorf("%s 0 ile 255 arasında olmalı", ad)
			}
			izTuru = n
		}
		return hexGecerli(f[2:], sshfpOzetHex[izTuru])
	case "NAPTR":
		f, err := tirnakliBol(deger)
		if err != nil {
			return err
		}
		if len(f) != 6 {
			return errors.New("order pref \"flags\" \"servis\" \"regexp\" değiştirme biçiminde 6 alan olmalı")
		}
		if _, ok := sayiAraligi(f[0], 65535); !ok {
			return errors.New("order 0 ile 65535 arasında olmalı")
		}
		if _, ok := sayiAraligi(f[1], 65535); !ok {
			return errors.New("pref 0 ile 65535 arasında olmalı")
		}
		for _, q := range f[2:5] {
			if !strings.HasPrefix(q, `"`) {
				return errors.New("flags, servis ve regexp alanları tırnak içinde olmalı")
			}
			if len(q)-2 > 255 {
				return errors.New("NAPTR alanları en fazla 255 bayt olabilir")
			}
			if err := kacisKontrol(q[1 : len(q)-1]); err != nil {
				return err
			}
		}
		return hedefAdiGecerli(f[5], true)
	}
	return nil
}

// AdZoneIcinde — sonda nokta ile MUTLAK yazılmış kayıt adı bu zone'un içinde mi. Göreli
// adlar zone'a eklenir (her zaman içeride). Zone dışı mutlak ad (ör. "foo.example.org.")
// BIND'de "out-of-zone data" olarak YOK SAYILIR: kayıt panelde görünür ama yayınlanmaz
// (canlı API doğrulaması 2026-10-13: 201 dönüyor, dig REFUSED).
func AdZoneIcinde(ad, alanAdi string) error {
	a := strings.TrimSpace(ad)
	if !strings.HasSuffix(a, ".") {
		return nil
	}
	a = strings.ToLower(strings.TrimSuffix(a, "."))
	z := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(alanAdi), "."))
	if a == z || strings.HasSuffix(a, "."+z) {
		return nil
	}
	return fmt.Errorf("kayıt adı %q bu alan adının (%s) dışında; zone dışı kayıt yayınlanmaz — göreli ad yazın (örn. \"www\")", ad, z)
}

// kayitZoneGecerli — KayitGecerli + adın bu zone'un içinde olması (zone adını bilen yollar).
func kayitZoneGecerli(k Kayit, alanAdi string) error {
	if err := KayitGecerli(k); err != nil {
		return err
	}
	return AdZoneIcinde(k.Ad, alanAdi)
}
