package merkez

// Çıkış süzgeci — model metni Telegram'a gitmeden ÖNCE (deterministik; modelden bağımsız):
//
//  1. Kimlik avı reddi: aynı cümlede kimlik bilgisi (parola/şifre/kod/kart/anahtar)
//     + paylaşma/gönderme fiili → TÜM yanıt reddedilir (kullanıcıya güvenli metin).
//  2. Maske ikinci geçiş: internal/maske (merkezin kendi sırları birebir +
//     sır desenleri + IP/e-posta). Ajan zaten maskeler; bu ikinci hattır.
//  3. Bağlantılar: yalnız girginos.io (+alt alanlar) ve oturumun hesap alan
//     adları (+alt alanlar) tıklanabilir kalır; diğerleri etkisizleştirilir
//     (https → hxxps, "." → "[.]").

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"girginospanel/internal/maske"
)

// CikisSuzgeci: bilinen sırlar merkezin kendi sırlarıdır (Telegram/Anthropic/webhook).
type CikisSuzgeci struct {
	bilinen []string
}

func yeniCikisSuzgeci(bilinen []string) *CikisSuzgeci {
	return &CikisSuzgeci{bilinen: bilinen}
}

// Suz: (temiz metin, reddedildi, neden).
func (c *CikisSuzgeci) Suz(metin string, alanlar []string) (string, bool, string) {
	if kimlikAvi(metin) {
		return "", true, "kimlik_avi"
	}
	satirlar := strings.Split(metin, "\n")
	for i, s := range satirlar {
		satirlar[i] = maske.MaskeleSatir(s, maske.Secenek{BilinenSirlar: c.bilinen, IPKirp: true, EpostaMaskele: true})
	}
	out := strings.Join(satirlar, "\n")
	out = baglantilariSuz(out, alanlar)
	return out, false, ""
}

// ---- kimlik avı ----

var (
	// kimlik bilgisi sözcük kökleri (küçük harf, Türkçe ekler serbest)
	kimlikKokleri = []string{"parola", "şifre", "sifre", "password", "passwd", "passcode", "cvv", "cvc", "otp"}
	// iki sözcüklü kimlik bilgisi: ilk kök + ikinci kök
	kimlikIkilileri = [][2]string{
		{"kart", "numara"}, {"card", "number"}, {"doğrulama", "kod"}, {"dogrulama", "kod"},
		{"sms", "kod"}, {"onay", "kod"}, {"verification", "code"}, {"api", "anahtar"},
		{"api", "key"}, {"gizli", "anahtar"}, {"secret", "key"}, {"private", "key"},
		{"özel", "anahtar"}, {"ozel", "anahtar"}, {"2fa", "kod"}, {"recovery", "phrase"},
		{"seed", "phrase"}, {"kurtarma", "kod"},
	}
	// talep fiilleri — TAM sözcük (olumsuz biçimler "paylaşmayın", "göndermeyin" listede YOK)
	talepFiilleri = map[string]bool{
		"gönder": true, "gönderin": true, "gönderiniz": true, "gonder": true, "gonderin": true,
		"paylaş": true, "paylaşın": true, "paylaşınız": true, "paylas": true, "paylasin": true,
		"ilet": true, "iletin": true, "iletiniz": true, "yaz": true, "yazın": true, "yazınız": true, "yazin": true,
		"bildir": true, "bildirin": true, "bildiriniz": true, "söyle": true, "söyleyin": true, "soyleyin": true,
		"girin": true, "giriniz": true, "verin": true, "veriniz": true, "yollayın": true, "yollayin": true,
		"send": true, "share": true, "provide": true, "give": true, "enter": true, "type": true, "tell": true,
		"reply": true, "dm": true, "post": true, "paste": true,
	}
	cumleAyirici = regexp.MustCompile(`[.!?;\n]+`)
)

func sozcukler(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func kokVar(sz []string, kok string) bool {
	for _, w := range sz {
		if strings.HasPrefix(w, kok) {
			return true
		}
	}
	return false
}

// kimlikAvi: bir cümle hem kimlik bilgisi hem talep fiili içeriyorsa true.
func kimlikAvi(metin string) bool {
	for _, cumle := range cumleAyirici.Split(metin, -1) {
		sz := sozcukler(cumle)
		talep := false
		for _, w := range sz {
			if talepFiilleri[w] {
				talep = true
				break
			}
		}
		if !talep {
			continue
		}
		for _, k := range kimlikKokleri {
			if kokVar(sz, k) {
				return true
			}
		}
		for i := 0; i+1 < len(sz); i++ {
			for _, ik := range kimlikIkilileri {
				if strings.HasPrefix(sz[i], ik[0]) && strings.HasPrefix(sz[i+1], ik[1]) {
					return true
				}
			}
		}
	}
	return false
}

// ---- bağlantılar ----

var (
	semaURL = regexp.MustCompile(`(?i)\b(?:https?|ftp|tg)://[^\s<>"'\x60]+`)
	// çıplak ana makine adı: en az iki etiket, alfabetik TLD
	ciplakAd = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b(?:/[^\s<>"'\x60]*)?`)
	// TLD sanılabilecek dosya uzantıları ve yapılandırma adları — dokunulmaz
	uzantiTLD = map[string]bool{
		"php": true, "html": true, "htm": true, "js": true, "css": true, "log": true, "txt": true, "conf": true,
		"json": true, "xml": true, "sql": true, "sh": true, "ini": true, "env": true, "zip": true, "gz": true,
		"tar": true, "png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "webp": true, "pem": true,
		"crt": true, "key": true, "sock": true, "pid": true, "service": true, "socket": true, "timer": true,
		"yml": true, "yaml": true, "md": true, "bak": true, "old": true, "tmp": true, "lock": true, "map": true,
		"ts": true, "go": true, "py": true, "rb": true, "pl": true, "cgi": true, "htaccess": true, "d": true,
		"so": true, "mo": true, "po": true, "woff": true, "woff2": true, "ttf": true, "ico": true, "csv": true,
	}
)

func izinliAd(ad string, alanlar []string) bool {
	ad = strings.TrimSuffix(strings.ToLower(ad), ".")
	if ad == "girginos.io" || strings.HasSuffix(ad, ".girginos.io") {
		return true
	}
	for _, a := range alanlar {
		a = strings.ToLower(a)
		if a != "" && (ad == a || strings.HasSuffix(ad, "."+a)) {
			return true
		}
	}
	return false
}

func etkisizlestir(s string) string {
	s = strings.Replace(s, "://", "[:]//", 1)
	if i := strings.Index(strings.ToLower(s), "http"); i == 0 {
		s = "hxxp" + s[4:]
	}
	if strings.HasPrefix(strings.ToLower(s), "ftp") {
		s = "fxp" + s[3:]
	}
	// yalnız ana makine kısmındaki noktalar
	host, kalan := s, ""
	if j := strings.Index(s, "//"); j >= 0 {
		h := s[j+2:]
		k := strings.IndexAny(h, "/?#")
		if k < 0 {
			k = len(h)
		}
		return s[:j+2] + strings.ReplaceAll(h[:k], ".", "[.]") + h[k:]
	}
	if k := strings.IndexAny(s, "/?#"); k >= 0 {
		host, kalan = s[:k], s[k:]
	}
	return strings.ReplaceAll(host, ".", "[.]") + kalan
}

func baglantilariSuz(metin string, alanlar []string) string {
	metin = semaURL.ReplaceAllStringFunc(metin, func(u string) string {
		p, err := url.Parse(u)
		if err == nil && (p.Scheme == "https" || p.Scheme == "http") && p.User == nil && izinliAd(p.Hostname(), alanlar) {
			return u
		}
		return etkisizlestir(u)
	})
	return ciplakAd.ReplaceAllStringFunc(metin, func(s string) string {
		if strings.Contains(s, "[.]") {
			return s
		}
		host := s
		if k := strings.IndexByte(s, '/'); k >= 0 {
			host = s[:k]
		}
		tld := host[strings.LastIndexByte(host, '.')+1:]
		if uzantiTLD[strings.ToLower(tld)] || izinliAd(host, alanlar) {
			return s
		}
		return etkisizlestir(s)
	})
}
