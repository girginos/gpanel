package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 TOTP (HMAC-SHA1, 6 hane, 30sn period) — harici bağımlılık YOK.

// TOTPGenerateSecret: 160-bit rastgele base32 secret üretir (padding'siz).
// rand.Read hatasında ("", error) döner — hatayı yutup tahmin edilebilir (all-zero)
// bir secret üretmek 2FA'yı komple çökertirdi.
func TOTPGenerateSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "="), nil
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

// gecerliAdim: kodu ±1 pencerede SABİT ZAMANLI doğrular ve kabul edilen 30sn
// zaman-adımını döndürür. minAdim'dan küçük/eşit adımlar REDDEDİLİR (replay koruması:
// aynı kodun tekrar kullanımını engeller). ok=false ise adım=0.
func gecerliAdim(secret, code string, minAdim int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 || secret == "" {
		return 0, false
	}
	t := time.Now().Unix() / 30
	for _, c := range []int64{t - 1, t, t + 1} {
		if c <= minAdim {
			continue
		}
		if v, ok := hotp(secret, uint64(c)); ok && subtle.ConstantTimeCompare([]byte(v), []byte(code)) == 1 {
			return c, true
		}
	}
	return 0, false
}

// TOTPVerify: geriye-uyumlu, replay-korumasız doğrulama (2FA kapatma için).
func TOTPVerify(secret, code string) bool {
	_, ok := gecerliAdim(secret, code, -1)
	return ok
}

// TOTPVerifyAdim: login/enable için replay-korumalı doğrulama; sonAdim'dan SONRAKİ
// bir adımda eşleşen kodu kabul eder ve o adımı döndürür (çağıran DB'ye yazmalı).
func TOTPVerifyAdim(secret, code string, sonAdim int64) (int64, bool) {
	return gecerliAdim(secret, code, sonAdim)
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
