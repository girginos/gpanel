package protokol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"time"
)

// Eşleştirme kodu (panel → Telegram /start <kod>) — ajan ve merkez AYNI kuralı
// kullanmalı: kod_ozet = sha256(EslesmeKoduNormal(kod)) küçük hex.
//
//   - EslesmeKoduUzunlugu karakter, IsKoduAlfabesi'nden (31^16 ≈ 2^79).
//   - Telegram start parametresine (A-Za-z0-9_-, ≤64) olduğu gibi sığar.
//   - Elle yazımda tire/boşluk ve küçük harf serbesttir; Normal bunları düzeltir.
//   - Kodun kendisi sunucudan çıkmaz; merkeze yalnız özeti (KodOzet) gider.
const (
	EslesmeKoduUzunlugu = 16
	EslesmeKoduOmru     = 10 * time.Minute
)

// EslesmeKoduUret: yeni kod (r nil → crypto/rand). Sapmasız seçim (YeniIsKodu ile aynı yöntem).
func EslesmeKoduUret(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	out := make([]byte, EslesmeKoduUzunlugu)
	var tek [1]byte
	for i := 0; i < len(out); {
		if _, err := io.ReadFull(r, tek[:]); err != nil {
			return "", err
		}
		if int(tek[0]) >= 8*len(IsKoduAlfabesi) {
			continue
		}
		out[i] = IsKoduAlfabesi[int(tek[0])%len(IsKoduAlfabesi)]
		i++
	}
	return string(out), nil
}

// EslesmeKoduNormal: tire/boşluk atılır, büyük harfe çevrilir; uzunluk ve alfabe
// tutmazsa ok=false.
func EslesmeKoduNormal(s string) (string, bool) {
	if len(s) > 4*EslesmeKoduUzunlugu {
		return "", false
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '-' || r == ' ':
			continue
		case r >= 'a' && r <= 'z':
			r -= 'a' - 'A'
		}
		if r > 0x7f || !strings.ContainsRune(IsKoduAlfabesi, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	if b.Len() != EslesmeKoduUzunlugu {
		return "", false
	}
	return b.String(), true
}

// EslesmeKoduOzet: normalleştirilmiş kodun sha256'sı (64 küçük hex); geçersiz kod → ok=false.
func EslesmeKoduOzet(kod string) (string, bool) {
	n, ok := EslesmeKoduNormal(kod)
	if !ok {
		return "", false
	}
	h := sha256.Sum256([]byte(n))
	return hex.EncodeToString(h[:]), true
}

// EslesmeKoduGorunen: insan için 4'lü gruplar (ABCD-EFGH-JKLM-NPQR).
func EslesmeKoduGorunen(kod string) string {
	n, ok := EslesmeKoduNormal(kod)
	if !ok {
		return ""
	}
	return n[0:4] + "-" + n[4:8] + "-" + n[8:12] + "-" + n[12:16]
}
