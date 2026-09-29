package hedef

// maske.go — sağlayıcıdan/alt süreçten gelen HAM hata metninin günlüğe
// yazılmadan önce maskelenmesi.
//
// 🔴 NEDEN: uzak depo istemcileri hata metnine istek URL'sini (imzalı S3 URL'si,
// ?access_token=, kullanıcı:parola@host), başlıkları (Authorization) ya da
// sunucunun geri yansıttığı gövdeyi koyabilir. Bu metin son_hata/cp_bildirim/
// log/HTTP yanıtına ulaşırsa kimlik bilgisi sızar. İki katman:
//  1. bilinen sırlar (çağıranın verdiği parola/anahtar/jeton) ve kodlanmış
//     biçimleri birebir değiştirilir;
//  2. sır biçimli desenler (URL kullanıcı bilgisi, Authorization, anahtar=değer,
//     özel anahtar blokları, sağlayıcı jeton önekleri) kalıpla maskelenir.
// Maskeleme KESMEDEN önce yapılır: kesme bir sırrı ikiye bölüp birebir
// eşleşmeyi kaçırtamaz.

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaskeIsareti — maskelenen değerin yerine yazılan işaret.
const MaskeIsareti = "***"

// AzamiAyrinti — maskelenmiş ayrıntının en çok rune sayısı.
const AzamiAyrinti = 512

var maskeDesenleri = []struct {
	re     *regexp.Regexp
	yerine string
}{
	// Özel anahtar blokları (çok satırlı) — kontrol karakterleri temizlenmeden önce.
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`), MaskeIsareti},
	// URL kullanıcı bilgisi: sftp://kul:parola@host → sftp://***@host
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://)[^\s/@]+@`), "${1}" + MaskeIsareti + "@"},
	// Authorization / Proxy-Authorization başlıkları (satır sonuna dek).
	{regexp.MustCompile(`(?i)\b((?:proxy-)?authorization"?\s*[:=]\s*)[^\r\n]+`), "${1}" + MaskeIsareti},
	// Bearer jetonları.
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-._~+/]+=*`), "Bearer " + MaskeIsareti},
	// anahtar=değer / "anahtar": "değer" biçimleri.
	{regexp.MustCompile(`(?i)\b(access_token|refresh_token|id_token|client_secret|code_verifier|password|passwd|pwd|parola|secret_access_key|secret_key|secretkey|secret|token|api_?key|x-amz-signature|x-amz-credential|x-amz-security-token|signature|sig)("?\s*[:=]\s*"?)([^\s"'&,;]+)`), "${1}${2}" + MaskeIsareti},
	// Türkçe "şifre" (\b ASCII dışı harfte çalışmaz).
	{regexp.MustCompile(`(?i)(şifre|sifre)("?\s*[:=]\s*"?)([^\s"'&,;]+)`), "${1}${2}" + MaskeIsareti},
	// OAuth yetki kodu URL'de.
	{regexp.MustCompile(`([?&]code=)[^&\s"']+`), "${1}" + MaskeIsareti},
	// age gizli anahtarları (Bech32; HRP'nin sonu "-", ayırıcı "1"): X25519
	// "AGE-SECRET-KEY-1...", hibrit ML-KEM-768+X25519 "AGE-SECRET-KEY-PQ-1..." ve
	// eklenti kimlikleri "AGE-PLUGIN-<AD>-1...". Bech32 tümü küçük harf de geçerli.
	{regexp.MustCompile(`(?i)(AGE-(?:SECRET-KEY|PLUGIN)-)(?:[0-9A-Z]+-)*1[0-9A-Z]+`), "${1}" + MaskeIsareti},
	// AWS erişim anahtarı kimliği.
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), MaskeIsareti},
	// Google erişim/yenileme jetonları, Dropbox kısa ömürlü jetonu.
	{regexp.MustCompile(`\bya29\.[0-9A-Za-z\-_.]+`), "ya29." + MaskeIsareti},
	{regexp.MustCompile(`\b1//[0-9A-Za-z\-_]{10,}`), "1//" + MaskeIsareti},
	{regexp.MustCompile(`\bsl\.[0-9A-Za-z\-_.]{20,}`), "sl." + MaskeIsareti},
}

// sirBicimleri — bir sırrın metinde görünebileceği kodlanmış biçimleri.
func sirBicimleri(s string) []string {
	bicim := []string{s, url.QueryEscape(s), url.PathEscape(s)}
	// JSON gövdesi: HTML kaçışlı (json.Marshal: & → &) ve kaçışsız biçim.
	if b, err := json.Marshal(s); err == nil && len(b) >= 2 {
		bicim = append(bicim, string(b[1:len(b)-1]))
	}
	var jb strings.Builder
	enc := json.NewEncoder(&jb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err == nil {
		if j := strings.TrimSpace(jb.String()); len(j) >= 2 {
			bicim = append(bicim, j[1:len(j)-1])
		}
	}
	// lftp/curl betiklerindeki çift tırnak kaçışı (\\ ve \").
	bicim = append(bicim, strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s))
	return bicim
}

// Maskele — ham metni günlüğe yazılabilir hale getirir: verilen sırlar ve
// kodlanmış biçimleri, sır biçimli desenler maskelenir; kontrol karakterleri
// boşluğa çevrilir; en çok AzamiAyrinti rune'a kesilir.
func Maskele(ham string, sirlar ...string) string {
	if ham == "" {
		return ""
	}
	var bicimler []string
	for _, s := range sirlar {
		if strings.TrimSpace(s) == "" {
			continue
		}
		bicimler = append(bicimler, sirBicimleri(s)...)
	}
	// Uzundan kısaya: bir sır diğerinin alt dizgesiyse önce uzunu değişsin.
	sort.Slice(bicimler, func(i, j int) bool { return len(bicimler[i]) > len(bicimler[j]) })
	out := ham
	for _, b := range bicimler {
		if b != "" {
			out = strings.ReplaceAll(out, b, MaskeIsareti)
		}
	}
	for _, d := range maskeDesenleri {
		out = d.re.ReplaceAllString(out, d.yerine)
	}
	out = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, out)
	out = strings.Join(strings.Fields(out), " ")
	if utf8.RuneCountInString(out) > AzamiAyrinti {
		r := []rune(out)
		out = string(r[:AzamiAyrinti]) + "…"
	}
	return out
}

// MaskeleHata — err.Error()'ı maskeler (nil → "").
func MaskeleHata(err error, sirlar ...string) string {
	if err == nil {
		return ""
	}
	return Maskele(err.Error(), sirlar...)
}
