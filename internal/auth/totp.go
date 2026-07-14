package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 TOTP (HMAC-SHA1, 6 hane, 30sn period) — harici bağımlılık YOK.

// TOTPGenerateSecret: 160-bit rastgele base32 secret üretir (padding'siz).
func TOTPGenerateSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")
}

func hotp(secret string, counter uint64) (string, bool) {
	s := strings.ToUpper(strings.TrimSpace(secret))
	if m := len(s) % 8; m != 0 {
		s += strings.Repeat("=", 8-m)
	}
	key, err := base32.StdEncoding.DecodeString(s)
	if err != nil || len(key) == 0 {
		return "", false
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	val := (uint32(sum[off]&0x7f) << 24) |
		(uint32(sum[off+1]) << 16) |
		(uint32(sum[off+2]) << 8) |
		uint32(sum[off+3])
	return fmt.Sprintf("%06d", val%1000000), true
}

// TOTPVerify: ±1 pencere (30sn saat kayması) toleransıyla kodu doğrular.
func TOTPVerify(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 || secret == "" {
		return false
	}
	t := uint64(time.Now().Unix()) / 30
	for _, c := range []uint64{t - 1, t, t + 1} {
		if v, ok := hotp(secret, c); ok && v == code {
			return true
		}
	}
	return false
}

// TOTPURI: authenticator uygulamalarının okuduğu otpauth:// URI'si (QR için).
func TOTPURI(secret, account, issuer string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return fmt.Sprintf("otpauth://totp/%s:%s?%s",
		url.PathEscape(issuer), url.PathEscape(account), v.Encode())
}
