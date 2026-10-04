package maske

// kisisel.go — KVKK kişisel veri maskeleri.
//
// İKİ KATMAN (kasıtlı ayrım):
//
//  1. desenler listesindeki checksum'lı tanımlayıcılar (T.C. kimlik no, IBAN —
//     kart zaten orada): her yerde çalışır, araç çıktısında da. Checksum
//     (TCKN algoritması, ISO 13616 mod-97, Luhn) geçmeyen sayı DOKUNULMAZ; bu
//     yüzden günlük/teşhis çıktısındaki PID, port, zaman damgası, sürüm gibi
//     rastgele sayılar bozulmaz (yanlış pozitif ≈ yüzde bir × nadir biçim).
//
//  2. KullaniciMetni: YALNIZ Telegram kullanıcısının serbest metni için. Telefon
//     numarası ve "şifrem …" gibi doğal dil parola ifadeleri burada maskelenir;
//     araç çıktısında (port/PID/IP teşhisi) ASLA uygulanmaz.
//
// Hepsi belirlenimci (aynı girdi → aynı çıktı) ve idempotenttir (maskeli metin
// ikinci geçişte değişmez: yerine yazılan "***" hiçbir desene uymaz).

import (
	"regexp"
	"strings"
)

// ---- T.C. kimlik no ----

// tcknAdayRe — 11 ve daha uzun rakam dizisi; yalnız TAM 11 hane + sağlama geçerse maskelenir
// (uzun rakam dizileri — zaman damgası, kimlik — dokunulmaz).
var tcknAdayRe = regexp.MustCompile(`[0-9]{11,}`)

// tcknGecerli — resmi T.C. kimlik no algoritması: 11 hane, ilk hane 0 değil,
// 10. hane = ((tek konum toplamı×7) − çift konum toplamı) mod 10,
// 11. hane = ilk 10 hane toplamı mod 10.
func tcknGecerli(d string) bool {
	if len(d) != 11 || d[0] == '0' {
		return false
	}
	var r [11]int
	for i := 0; i < 11; i++ {
		if !rakam(d[i]) {
			return false
		}
		r[i] = int(d[i] - '0')
	}
	tek := r[0] + r[2] + r[4] + r[6] + r[8]
	cift := r[1] + r[3] + r[5] + r[7]
	if ((tek*7-cift)%10+10)%10 != r[9] {
		return false
	}
	top := 0
	for i := 0; i < 10; i++ {
		top += r[i]
	}
	return top%10 == r[10]
}

func tcknMaskele(m string) string {
	if tcknGecerli(m) {
		return Isaret
	}
	return m
}

// ---- IBAN ----

// ibanAdayRe — 2 harf + 2 hane + 11-30 harf/rakam (ayırıcı boşluk ya da tire olabilir).
// Aday taşabilir (arkasındaki sözcükleri yutabilir): ibanMaskele en uzun GEÇERLİ öneki bulur.
var ibanAdayRe = regexp.MustCompile(`(?i)\b[a-z]{2}[0-9]{2}(?:[ -]?[a-z0-9]){11,30}\b`)

func harfMi(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// ibanGecerli — ISO 13616: 15-34 karakter, ülke (2 harf) + 2 kontrol hanesi, mod-97 = 1.
// Türkiye için uzunluk tam 26 olmalıdır.
func ibanGecerli(s string) bool {
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	if !harfMi(s[0]) || !harfMi(s[1]) || !rakam(s[2]) || !rakam(s[3]) {
		return false
	}
	if (s[0] == 'T' || s[0] == 't') && (s[1] == 'R' || s[1] == 'r') && len(s) != 26 {
		return false
	}
	kalan := 0
	yaz := func(v int) {
		if v >= 10 {
			kalan = (kalan*100 + v) % 97
		} else {
			kalan = (kalan*10 + v) % 97
		}
	}
	for _, c := range []byte(s[4:] + s[:4]) {
		switch {
		case rakam(c):
			yaz(int(c - '0'))
		case c >= 'A' && c <= 'Z':
			yaz(int(c-'A') + 10)
		case c >= 'a' && c <= 'z':
			yaz(int(c-'a') + 10)
		default:
			return false
		}
	}
	return kalan == 1
}

// ibanMaskele — adayın en uzun geçerli IBAN önekini maskeler; kalan metin korunur.
func ibanMaskele(m string) string {
	var konum []int
	for i := 0; i < len(m); i++ {
		if harfMi(m[i]) || rakam(m[i]) {
			konum = append(konum, i)
		}
	}
	en := len(konum)
	if en > 34 {
		en = 34
	}
	for n := en; n >= 15; n-- {
		var b strings.Builder
		for _, p := range konum[:n] {
			b.WriteByte(m[p])
		}
		if ibanGecerli(b.String()) {
			return Isaret + m[konum[n-1]+1:]
		}
	}
	return m
}

// ---- telefon (YALNIZ kullanıcı metni) ----

// Türkiye: +90/0090 ön ekli, 0 ön ekli ulusal, ön eksiz cep (5xx, en az bir ayırıcı ile).
// Ayırıcı olarak NOKTA bilerek yok: 1.2.3.4 / sürüm / IP desenleriyle çakışır.
var telefonTRRe = []*regexp.Regexp{
	regexp.MustCompile(`(?:\+|\b00)90[ -]?\(?[2-5][0-9]{2}\)?[ -]?[0-9]{3}[ -]?[0-9]{2}[ -]?[0-9]{2}\b`),
	regexp.MustCompile(`\b0\(?[2-5][0-9]{2}\)?[ -]?[0-9]{3}[ -]?[0-9]{2}[ -]?[0-9]{2}\b`),
	regexp.MustCompile(`\b5[0-9]{2}[ -][0-9]{3}[ -]?[0-9]{2}[ -]?[0-9]{2}\b`),
	regexp.MustCompile(`\(5[0-9]{2}\)[ -]?[0-9]{3}[ -]?[0-9]{2}[ -]?[0-9]{2}\b`),
}

// telefonUlusRe — "+" ile başlayan uluslararası numara (E.164: 8-15 hane toplam).
var telefonUlusRe = regexp.MustCompile(`\+[1-9][0-9 ().-]{6,20}[0-9]`)

func rakamSay(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if rakam(s[i]) {
			n++
		}
	}
	return n
}

func telefonMaskele(s string) string {
	if !strings.ContainsAny(s, "0123456789") {
		return s
	}
	s = telefonUlusRe.ReplaceAllStringFunc(s, func(m string) string {
		if n := rakamSay(m); n >= 8 && n <= 15 {
			return Isaret
		}
		return m
	})
	for _, re := range telefonTRRe {
		s = re.ReplaceAllString(s, Isaret)
	}
	return s
}

// ---- doğal dil parola ifadesi (YALNIZ kullanıcı metni) ----

// dogalSifreRe — "şifrem 1234", "parolam: x", "my password is Abc!9", "password hunter2".
// Anahtar sözcüğün hemen ardından boşluk ya da :/= gelmelidir ("şifremi unuttum" eşleşmez).
// Öncesi harf/rakam olmamalı (Go \b yalnız ASCII'dir; ş/ı için elle sınır).
var dogalSifreRe = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])(şifrem|sifrem|parolam|pinim|password|passcode)(\s*[:=]\s*|\s+)((?:(?:is|şu|şudur|olarak|ise)\s+)?)([^\s,;]+)`)

func dogalSifreMaskele(s string) string {
	if len(s) < 8 {
		return s
	}
	return dogalSifreRe.ReplaceAllStringFunc(s, func(m string) string {
		g := dogalSifreRe.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		// Açık ayırıcı (:/=) varsa değer kesin parola; yoksa yalnız rakam/simge taşıyan
		// belirteç parola sayılır ("şifrem çalışmıyor" bozulmaz).
		if !strings.ContainsAny(g[3], ":=") && !strings.ContainsAny(g[5], "0123456789!@#$%^&*()_+-=[]{}|\\/<>?~`'\"") {
			return m
		}
		if strings.Trim(g[5], "*") == "" { // zaten maskeli (idempotent)
			return m
		}
		return g[1] + g[2] + g[3] + g[4] + Isaret
	})
}

// KullaniciMetni — Telegram kullanıcısının serbest metnini model çağrısından,
// depodan ya da devir mesajından ÖNCE maskeler. Sırayla:
//
//	bilinen sırlar (birebir) → sır desenleri → T.C. kimlik no / IBAN / kart
//	(sağlamalı) → e-posta (yerel kısım ilk harf+***, alan adı kalır: model
//	"gmail mi, kendi alan adı mı" ayrımını yapabilsin) → IPv4 /24, IPv6 /48
//	(araç çıktısıyla aynı) → telefon → "şifrem …" ifadeleri.
//
// Çok satırlı metin satırlara bölünür (özel anahtar blokları dahil); satır
// sonları korunur. Kesme YAPMAZ. Belirlenimci ve idempotenttir.
//
// MASKELENMEYENLER (hukuki karar/sınır): ad-soyad, adres, alan adları, serbest
// metindeki başka kişisel veriler — desenle güvenilir yakalanamaz.
func KullaniciMetni(s string, bilinenSirlar []string) string {
	if s == "" {
		return ""
	}
	m := Yeni(Secenek{BilinenSirlar: bilinenSirlar, IPKirp: true, EpostaMaskele: true})
	satirlar := m.Satirlar(strings.Split(s, "\n"))
	for i, l := range satirlar {
		l = telefonMaskele(l)
		satirlar[i] = dogalSifreMaskele(l)
	}
	return strings.Join(satirlar, "\n")
}
