// Package maske — panel dışına çıkacak metindeki (destek botu → Claude API,
// günlük, bildirim, uzak hata metni) sırları ve kişisel verileri maskeler.
//
// 🔴 NEDEN: alan adı logları, sunucu günlüğü ve alt süreç hata metinleri
// parola, jeton, çerez, DSN, wp-config/.env satırı, kart numarası, IP ve
// e-posta taşıyabilir. Bu paket hepsini tek yerde, satır bazlı maskeler.
//
// Sıra (her satır): girdi üst sınırı → bilinen sırlar (birebir + kodlanmış
// biçimler) → sır desenleri → kişisel veri (isteğe bağlı) → kontrol
// karakterleri. Kesme (KesSatirlar) AYRI ve maskeden SONRA çağrılır: kesme bir
// sırrı ikiye bölüp birebir eşleşmeyi kaçırtamaz.
//
// ReDoS: Go regexp (RE2) doğrusal zamanlıdır; ek olarak tek satır AzamiGirdi
// baytta kesilir (yalnız bu durumda kesme maskeden önce gelir; kesim noktası
// son boşluğa çekilir ki bir sırrın yarısı kalmasın).
package maske

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Isaret — maskelenen değerin yerine yazılan işaret.
const Isaret = "***"

// MinBilinenSir — satır API'sinde birebir değiştirilecek bilinen sırrın en kısa
// uzunluğu (daha kısası sıradan sözcükleri bozar).
const MinBilinenSir = 8

// AzamiGirdi — tek satırın maskelenmeden önceki üst sınırı (bayt).
const AzamiGirdi = 64 << 10

// kesikEk — AzamiGirdi aşıldığında satır sonuna eklenen işaret.
const kesikEk = " …[kesildi]"

// Secenek — satır maskesinin isteğe bağlı katmanları.
type Secenek struct {
	// BilinenSirlar — birebir (ve URL/JSON/kabuk kaçışlı biçimleriyle)
	// değiştirilecek sırlar; MinBilinenSir'den kısa olanlar yok sayılır.
	BilinenSirlar []string
	// IPKirp — IPv4 → a.b.c.0 (/24), IPv6 → /48 öneki (loopback dokunulmaz).
	IPKirp bool
	// EpostaMaskele — e-posta yerel kısmı: ilk harf + ***.
	EpostaMaskele bool
	// EvOnekSoy — /home/<kul>/ öneki düşer (göreli yol kalır).
	EvOnekSoy bool
}

type desen struct {
	ad string
	// on — ön süzgeç: satırın küçük harfli kopyasında bunlardan HİÇBİRİ yoksa
	// desen eşleşemez, regexp atlanır (zorunlu koşul olmalı; boş = her zaman).
	on     []string
	re     *regexp.Regexp
	yerine string
	fn     func(string) string
}

// desenler — sır biçimli kalıplar. Her grup tek satır: neg_control bir satırın
// silinmesinin testte yakalandığını doğrular.
var desenler = []desen{
	{ad: "ozel-anahtar", on: []string{"private key"}, re: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|$)`), yerine: Isaret},
	{ad: "url-kullanici", on: []string{"://"}, re: regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://)[^\s/@]+@`), yerine: "${1}" + Isaret + "@"},
	{ad: "dsn-tcp", on: []string{"@"}, re: regexp.MustCompile(`\b([A-Za-z0-9_.\-]+):[^\s@/]+@((?:tcp|udp|unix)\()`), yerine: "${1}:" + Isaret + "@${2}"},
	{ad: "authorization", on: []string{"authorization"}, re: regexp.MustCompile(`(?i)\b((?:proxy-)?authorization"?\s*[:=]\s*)[^\r\n]+`), yerine: "${1}" + Isaret},
	{ad: "cerez-baslik", on: []string{"cookie"}, re: regexp.MustCompile(`(?i)\b((?:set-)?cookie"?\s*[:=]\s*)[^\r\n]+`), yerine: "${1}" + Isaret},
	{ad: "bearer", on: []string{"bearer"}, re: regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-._~+/]+=*`), yerine: "Bearer " + Isaret},
	{ad: "jwt", on: []string{"eyj"}, re: regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`), yerine: Isaret},
	{ad: "gosp-api", on: []string{"gosp_"}, re: regexp.MustCompile(`\bgosp_([a-z2-7]{12})_[A-Za-z0-9_-]{20,}`), yerine: "gosp_${1}_" + Isaret},
	{ad: "telegram", on: []string{":"}, re: regexp.MustCompile(`(^|[^0-9])[0-9]{8,10}:[A-Za-z0-9_-]{35}`), yerine: "${1}" + Isaret},
	{ad: "anthropic", on: []string{"sk-ant-"}, re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{10,}`), yerine: "sk-ant-" + Isaret},
	{ad: "saglayici-onek", on: []string{"sk-proj-", "k_live_", "gh", "xox"}, re: regexp.MustCompile(`\b(?:sk-proj-[A-Za-z0-9_-]{20,}|[sr]k_live_[A-Za-z0-9]{16,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|xox[abprs]-[A-Za-z0-9-]{10,})`), yerine: Isaret},
	{ad: "wp-config", on: []string{"define"}, re: regexp.MustCompile(`(?i)(define\s*\(\s*['"][A-Z0-9_]*(?:PASSWORD|PASS|KEY|SALT|SECRET|TOKEN)['"]\s*,\s*)(?:'(?:[^'\\]|\\.)*'?|"(?:[^"\\]|\\.)*"?)`), yerine: "${1}'" + Isaret + "'"},
	{ad: "env-anahtar", on: []string{"_"}, re: regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:_KEY|_SECRET|_PASSWORD|_PASSWD|_TOKEN|_PASS|_PWD|_SALT))(['"]?\s*(?:=>|[:=])\s*)(?:"(?:[^"\\]|\\.)*"?|'(?:[^'\\]|\\.)*'?|[^\s"'&,;]+)`), yerine: "${1}${2}" + Isaret},
	{ad: "anahtar-deger", on: []string{"token", "secret", "verifier", "pass", "pwd", "parola", "key", "sig", "credential"}, re: regexp.MustCompile(`(?i)\b(access_token|refresh_token|id_token|client_secret|code_verifier|password|passwd|pwd|parola|secret_access_key|secret_key|secretkey|secret|token|api_?key|x-amz-signature|x-amz-credential|x-amz-security-token|signature|sig)("?\s*[:=]\s*"?)([^\s"'&,;]+)`), yerine: "${1}${2}" + Isaret},
	{ad: "sifre", on: []string{"fre"}, re: regexp.MustCompile(`(?i)(şifre|sifre)("?\s*[:=]\s*"?)([^\s"'&,;]+)`), yerine: "${1}${2}" + Isaret},
	{ad: "oauth-code", on: []string{"code="}, re: regexp.MustCompile(`([?&]code=)[^&\s"']+`), yerine: "${1}" + Isaret},
	{ad: "sorgu", on: []string{"token", "key", "pass", "secret", "sig", "auth", "pwd", "nonce", "sid"}, re: regexp.MustCompile(`(?i)([?&;][a-z0-9_.\-]*(?:token|key|pass|passwd|password|secret|sig|signature|auth|pwd|nonce|sid))=[^&\s"'#]+`), yerine: "${1}=" + Isaret},
	{ad: "cerez-ad", on: []string{"sess", "xsrf", "wordpress_"}, re: regexp.MustCompile(`(?i)\b(PHPSESSID|laravel_session|ci_session|XSRF-TOKEN|wordpress_logged_in_[0-9a-f]*|wordpress_sec_[0-9a-f]*|wordpress_[0-9a-f]{32}|wp_woocommerce_session_[0-9a-f]*|[a-z0-9_]+_session)=[^\s;,"'&]+`), yerine: "${1}=" + Isaret},
	{ad: "age", on: []string{"age-"}, re: regexp.MustCompile(`(?i)(AGE-(?:SECRET-KEY|PLUGIN)-)(?:[0-9A-Z]+-)*1[0-9A-Z]+`), yerine: "${1}" + Isaret},
	{ad: "aws", on: []string{"akia", "asia"}, re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), yerine: Isaret},
	{ad: "google-erisim", on: []string{"ya29."}, re: regexp.MustCompile(`\bya29\.[0-9A-Za-z\-_.]+`), yerine: "ya29." + Isaret},
	{ad: "google-yenileme", on: []string{"1//"}, re: regexp.MustCompile(`\b1//[0-9A-Za-z\-_]{10,}`), yerine: "1//" + Isaret},
	{ad: "dropbox", on: []string{"sl."}, re: regexp.MustCompile(`\bsl\.[0-9A-Za-z\-_.]{20,}`), yerine: "sl." + Isaret},
	{ad: "base64-anahtar", on: []string{"base64:"}, re: regexp.MustCompile(`\bbase64:[A-Za-z0-9+/]{32,}={0,2}`), yerine: "base64:" + Isaret},
	{ad: "pem-govde", re: regexp.MustCompile(`^\s*[A-Za-z0-9+/]{60,}={0,2}\s*$`), yerine: Isaret},
	{ad: "iban", re: ibanAdayRe, fn: ibanMaskele},
	{ad: "tckn", re: tcknAdayRe, fn: tcknMaskele},
	{ad: "kart", re: regexp.MustCompile(`\b[2-69](?:[ -]?[0-9]){12,18}\b`), fn: kartMaskele},
}

var (
	ozelBasRe = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
	ozelSonRe = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
	ipv4Re    = regexp.MustCompile(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`)
	ipv6Re    = regexp.MustCompile(`[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}`)
	epostaRe  = regexp.MustCompile(`([A-Za-z0-9])[A-Za-z0-9._%+\-]*@((?:[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63})\b`)
	evOnekRe  = regexp.MustCompile(`/home/[^/\s'"():;,\]]+(/?)`)
)

// DesenleMaskele — yalnız sır desenlerini uygular (bilinen sır, kişisel veri,
// kontrol karakteri ve kesme YOK). Çok satırlı metinde özel anahtar bloğunu da
// tümüyle maskeler.
func DesenleMaskele(s string) string { return desenleriUygula(s, "") }

// desenleriUygula — atla adlı grup dışındaki desenler (test: her grubun tekil
// katkısını kanıtlamak için).
func desenleriUygula(s, atla string) string {
	// Küçük harfli kopya bir kez: yer değiştirmeler yalnız sabit işaret ve
	// özgün metinden yakalanan parçaları yazar, yeni anahtar sözcük üretmez.
	// (?i) katlaması: K (Kelvin) ToLower ile k olur; ſ ayrıca s'ye çevrilir.
	kucuk := strings.ToLower(s)
	if strings.ContainsRune(kucuk, 'ſ') {
		kucuk = strings.ReplaceAll(kucuk, "ſ", "s")
	}
	for _, d := range desenler {
		// MatchString yalnız DFA koşar; alt eşleşme konumları (yavaş yol)
		// yalnız eşleşen satırda hesaplanır.
		if d.ad == atla || !onVar(kucuk, d.on) || !d.re.MatchString(s) {
			continue
		}
		if d.fn != nil {
			s = d.re.ReplaceAllStringFunc(s, d.fn)
		} else {
			s = d.re.ReplaceAllString(s, d.yerine)
		}
	}
	return s
}

func onVar(kucuk string, on []string) bool {
	if len(on) == 0 {
		return true
	}
	for _, o := range on {
		if strings.Contains(kucuk, o) {
			return true
		}
	}
	return false
}

// kartMaskele — aday rakam dizisinin herhangi bir ardışık grup aralığı Luhn
// geçerli 13-19 haneli kart numarasıysa tümü maskelenir.
func kartMaskele(m string) string {
	gruplar := strings.FieldsFunc(m, func(r rune) bool { return r == ' ' || r == '-' })
	for i := range gruplar {
		var b strings.Builder
		for j := i; j < len(gruplar); j++ {
			b.WriteString(gruplar[j])
			if kartMu(b.String()) {
				return Isaret
			}
		}
	}
	return m
}

// kartMu — 13-19 hane, kart önekiyle (2-6; Troy 9792) başlar, Luhn geçerli.
func kartMu(d string) bool {
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	switch d[0] {
	case '2', '3', '4', '5', '6':
	default:
		if !strings.HasPrefix(d, "9792") {
			return false
		}
	}
	top, cift := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if n < 0 || n > 9 {
			return false
		}
		if cift {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		top += n
		cift = !cift
	}
	return top%10 == 0
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

// bicimlerHazirla — en az min uzunluktaki (boşluk dışı) sırların tüm biçimleri,
// tekilleştirilmiş ve uzundan kısaya sıralı (bir sır diğerinin alt dizgesiyse
// önce uzunu değişsin).
func bicimlerHazirla(sirlar []string, min int) []string {
	gor := map[string]bool{}
	var out []string
	for _, s := range sirlar {
		if strings.TrimSpace(s) == "" || len(s) < min {
			continue
		}
		for _, b := range sirBicimleri(s) {
			if b != "" && !gor[b] {
				gor[b] = true
				out = append(out, b)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func bicimleriDegistir(s string, bicimler []string) string {
	for _, b := range bicimler {
		if strings.Contains(s, b) {
			s = strings.ReplaceAll(s, b, Isaret)
		}
	}
	return s
}

// SirlariDegistir — verilen sırları (en az min bayt; boş/boşluk olanlar yok
// sayılır) ve kodlanmış biçimlerini birebir Isaret ile değiştirir.
func SirlariDegistir(s string, sirlar []string, min int) string {
	if s == "" || len(sirlar) == 0 {
		return s
	}
	return bicimleriDegistir(s, bicimlerHazirla(sirlar, min))
}

// TekSatir — kontrol karakterlerini boşluğa çevirir ve boşlukları tek boşluğa
// indirir (günlük alanı / hata ayrıntısı için).
func TekSatir(s string) string {
	return strings.Join(strings.Fields(kontrolTemizle(s, false)), " ")
}

// KesRune — en çok n rune; kesilirse "…" eklenir.
func KesRune(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// KesSatirlar — maskelenmiş satırların SON n tanesini (n<=0: hepsi) tutar ve
// her birini en çok maxRune rune'a (maxRune<=0: sınırsız) keser. Maskeden SONRA
// çağrılmalıdır. Girdi değiştirilmez.
func KesSatirlar(satirlar []string, n, maxRune int) []string {
	if n > 0 && len(satirlar) > n {
		satirlar = satirlar[len(satirlar)-n:]
	}
	out := make([]string, len(satirlar))
	for i, s := range satirlar {
		out[i] = KesRune(s, maxRune)
	}
	return out
}

// kontrolTemizle — C0/C1 kontrol karakterleri boşluk olur; sekmeKalsin ise \t korunur.
func kontrolTemizle(s string, sekmeKalsin bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' && sekmeKalsin {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

// girdiSiniri — geçersiz UTF-8 onarılır; AzamiGirdi aşılırsa son boşlukta
// (son 4 KiB içinde yoksa 4 KiB geriden) kesilir ve işaret eklenir.
func girdiSiniri(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if len(s) <= AzamiGirdi {
		return s
	}
	k := s[:AzamiGirdi]
	if i := strings.LastIndexAny(k, " \t"); i >= AzamiGirdi-4096 {
		k = k[:i]
	} else {
		k = k[:AzamiGirdi-4096]
	}
	return strings.ToValidUTF8(k, "�") + kesikEk
}

// Maskeleyici — bir Secenek için hazırlanmış (bilinen sır biçimleri bir kez
// hesaplanır) maskeleyici. Eşzamanlı kullanım güvenlidir.
type Maskeleyici struct {
	sec      Secenek
	bicimler []string
}

// Yeni — Secenek için maskeleyici.
func Yeni(sec Secenek) *Maskeleyici {
	return &Maskeleyici{sec: sec, bicimler: bicimlerHazirla(sec.BilinenSirlar, MinBilinenSir)}
}

// Satir — tek satırı maskeler (kesmez; sekme korunur, öteki kontrol
// karakterleri boşluk olur).
func (m *Maskeleyici) Satir(s string) string {
	if s == "" {
		return ""
	}
	s = girdiSiniri(s)
	s = bicimleriDegistir(s, m.bicimler)
	s = DesenleMaskele(s)
	if m.sec.EvOnekSoy {
		s = evOnekSoy(s)
	}
	if m.sec.EpostaMaskele && strings.Contains(s, "@") {
		s = epostaRe.ReplaceAllString(s, "${1}"+Isaret+"@${2}")
	}
	if m.sec.IPKirp {
		s = ipv6Kirp(ipv4Kirp(s))
	}
	return kontrolTemizle(s, true)
}

// Satirlar — satırları maskeler; satırlara bölünmüş özel anahtar bloğunun
// (BEGIN … END) gövde satırları da tümüyle maskelenir. Girdi değiştirilmez.
func (m *Maskeleyici) Satirlar(satirlar []string) []string {
	out := make([]string, len(satirlar))
	blok := false
	for i, s := range satirlar {
		if blok {
			j := ozelSonRe.FindStringIndex(s)
			if j == nil {
				out[i] = Isaret
				continue
			}
			s = Isaret + s[j[1]:]
			blok = false
		}
		if b := ozelBasRe.FindAllStringIndex(s, -1); b != nil && !ozelSonRe.MatchString(s[b[len(b)-1][1]:]) {
			blok = true
		}
		out[i] = m.Satir(s)
	}
	return out
}

// MaskeleSatir — tek satırı maskeler. Seçenek verilmezse yalnız sır desenleri
// uygulanır (kişisel veri katmanları kapalı).
func MaskeleSatir(s string, sec ...Secenek) string {
	var o Secenek
	if len(sec) > 0 {
		o = sec[0]
	}
	return Yeni(o).Satir(s)
}

// MaskeleSatirlar — satırları sec ile maskeler (kesme yok; ardından KesSatirlar).
func MaskeleSatirlar(satirlar []string, sec Secenek) []string {
	return Yeni(sec).Satirlar(satirlar)
}

func harfRakam(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func rakam(c byte) bool { return c >= '0' && c <= '9' }

// ipv4Kirp — bağımsız IPv4 adresleri a.b.c.0 olur. Sürüm dizgeleri (1.2.3.4.5,
// v1.2.3.4), geçersiz oktet, loopback ve 0.0.0.0 dokunulmaz.
func ipv4Kirp(s string) string {
	if strings.Count(s, ".") < 3 {
		return s
	}
	idx := ipv4Re.FindAllStringIndex(s, -1)
	if idx == nil {
		return s
	}
	var b strings.Builder
	son := 0
	for _, ix := range idx {
		a, z := ix[0], ix[1]
		if a > 0 && (harfRakam(s[a-1]) || s[a-1] == '.') {
			continue
		}
		if z < len(s) && (harfRakam(s[z]) || (s[z] == '.' && z+1 < len(s) && rakam(s[z+1]))) {
			continue
		}
		adr, err := netip.ParseAddr(s[a:z])
		if err != nil || !adr.Is4() || adr.IsLoopback() || adr.IsUnspecified() {
			continue
		}
		o := adr.As4()
		b.WriteString(s[son:a])
		b.WriteString(strconv.Itoa(int(o[0])) + "." + strconv.Itoa(int(o[1])) + "." + strconv.Itoa(int(o[2])) + ".0")
		son = z
	}
	b.WriteString(s[son:])
	return b.String()
}

// ipv6Kirp — bağımsız IPv6 adresleri /48 önekine indirilir. Saat (12:34:56),
// MAC, PHP "Sinif::yontem" ve loopback dokunulmaz; IPv4 eşlemeli biçimin IPv4
// kısmı ipv4Kirp'e kalır.
func ipv6Kirp(s string) string {
	// "::" kısaltması yoksa tam biçim 7 iki nokta ister (saat damgalı satırlar
	// regexp'e hiç girmez).
	if !strings.Contains(s, "::") && strings.Count(s, ":") < 7 {
		return s
	}
	idx := ipv6Re.FindAllStringIndex(s, -1)
	var b strings.Builder
	son := 0
	for _, ix := range idx {
		a, z := ix[0], ix[1]
		if a > 0 && (harfRakam(s[a-1]) || s[a-1] == ':' || s[a-1] == '.') {
			continue
		}
		if z < len(s) && (harfRakam(s[z]) || s[z] == ':' || s[z] == '.') {
			continue
		}
		aday := s[a:z]
		hane := 0
		for i := 0; i < len(aday); i++ {
			if aday[i] != ':' {
				hane++
			}
		}
		if hane < 4 {
			continue
		}
		adr, err := netip.ParseAddr(aday)
		if err != nil || !adr.Is6() || adr.Is4In6() || adr.IsLoopback() || adr.IsUnspecified() {
			continue
		}
		b.WriteString(s[son:a])
		b.WriteString(netip.PrefixFrom(adr, 48).Masked().Addr().String())
		son = z
	}
	if son == 0 {
		return s
	}
	b.WriteString(s[son:])
	return b.String()
}

// evOnekSoy — /home/<kul>/ öneki düşer; eğik çizgisiz /home/<kul> → "~".
func evOnekSoy(s string) string {
	if !strings.Contains(s, "/home/") {
		return s
	}
	return evOnekRe.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasSuffix(m, "/") {
			return ""
		}
		return "~"
	})
}
