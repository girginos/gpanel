package hesaplar

import (
	"crypto/subtle"
	"os/exec"
	"strings"
)

// FTP parolalari artik DUZ METIN saklanmaz. Pure-FTPd `MYSQLCrypt crypt` modu
// crypt(3) ($6$ SHA-512, glibc) ile dogrular; asagidaki uretim openssl ile ayni
// $6$ formatini urettigi icin daemon ile uyumludur (canli dogrulandi: glibc
// crypt(3) openssl $6$ ciktisini kabul ediyor). Parola argv'de GORUNMEZ (stdin).

// FTPParolaHash: duz parolayi $6$ SHA-512 crypt hash'ine cevirir (rastgele tuz).
// Bos string veya hata → "" (cagiran bos hash'i reddetmeli).
func FTPParolaHash(duz string) string {
	if duz == "" {
		return ""
	}
	cmd := exec.Command("openssl", "passwd", "-6", "-stdin")
	cmd.Stdin = strings.NewReader(duz)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsFTPHash: deger zaten $6$ crypt hash'i mi? (migration/idempotency icin)
func IsFTPHash(s string) bool { return strings.HasPrefix(s, "$6$") }

// FTPParolaDogrula: duz parolayi saklanan $6$ hash'ine karsi sabit-zamanli dogrular.
// Saklanan hala DUZ METIN ise (migration oncesi eski satir) duz karsilastirir —
// boylece gecis penceresinde giris kirilmaz (graceful).
func FTPParolaDogrula(duz, saklanan string) bool {
	if saklanan == "" {
		return false
	}
	if !IsFTPHash(saklanan) {
		// Eski duz-metin satir (henuz migrate edilmemis) — duz sabit-zamanli.
		return subtle.ConstantTimeCompare([]byte(duz), []byte(saklanan)) == 1
	}
	// $6$<tuz>$<hash> → tuzu cikar, ayni tuzla yeniden hash'le, karsilastir.
	p := strings.Split(saklanan, "$")
	if len(p) < 4 || p[1] != "6" {
		return false
	}
	tuz := p[2]
	cmd := exec.Command("openssl", "passwd", "-6", "-stdin", "-salt", tuz)
	cmd.Stdin = strings.NewReader(duz)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	uretilen := strings.TrimSpace(string(out))
	return subtle.ConstantTimeCompare([]byte(uretilen), []byte(saklanan)) == 1
}
