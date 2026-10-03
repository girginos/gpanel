// Package apianahtar — kök ve bayi API anahtarları (D4).
//
// 🔴 TASARIM ÖZETİ (ayrıntı: ADR taslağı tasarim-api.md + eleştiri #6/#10):
//   - Biçim: gosp_<önek:12>_<gizli:43>. Önek herkese açık arama kimliğidir
//     (base32 küçük harf, 60 bit), gizli 32 bayt crypto/rand (base64url).
//   - Saklama: YALNIZ SHA-256(tam anahtar) onaltılık. Gizli 256 bit rastgele →
//     kaba kuvvet imkânsız, yavaş özet (argon2/bcrypt) her istekte CPU yakar ve
//     güvenlik katmaz. Biber KULLANILMAZ (eleştiri #10): db.key / LoadCredential
//     sırrı kaybolur ya da `sir yenile` ile dönerse tüm anahtarlar sessizce
//     geçersiz olurdu. Anahtarlar JWT sırrından ve db.key'den BAĞIMSIZDIR.
//   - Karşılaştırma sabit zamanlı (subtle.ConstantTimeCompare); önek bulunamazsa
//     da özet hesaplanır (zamanlama eşitliği).
//   - Tam anahtar yalnız üretim yanıtında BİR KEZ döner; günlüğe/denetime yalnız
//     önek yazılır.
package apianahtar

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	// On: anahtarın tanınabilir öneki (gizli tarayıcı kalıbı + JWT'den ayrım: JWT "eyJ").
	On = "gosp_"
	// OnekUzunluk: herkese açık kimlik (base32, 12 karakter = 60 bit).
	OnekUzunluk = 12
	// GizliUzunluk: 32 bayt base64url (dolgusuz) = 43 karakter.
	GizliUzunluk = 43
	// TamUzunluk: gosp_ + önek + "_" + gizli.
	TamUzunluk = len(On) + OnekUzunluk + 1 + GizliUzunluk
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// randOku: dikiş (testler hata yolunu sınar).
var randOku = rand.Read

// ErrRastgele: crypto/rand okunamadı — anahtar ÜRETİLMEZ (zayıf anahtar yerine hata).
var ErrRastgele = errors.New("rastgele kaynak okunamadı")

// Uretilen: yeni anahtarın parçaları. Tam yalnız istemciye bir kez gösterilir.
type Uretilen struct {
	Tam  string // gosp_<önek>_<gizli> — SAKLANMAZ
	Onek string // arama kimliği (DB'de UNIQUE)
	Ozet string // SHA-256(Tam) onaltılık — DB'de saklanan TEK şey
	Son4 string // listede gösterim için gizlinin son 4 karakteri
}

// Uret: yeni rastgele anahtar.
func Uret() (Uretilen, error) {
	ob := make([]byte, 8) // 64 bit → base32 13 karakter; 12'ye kırpılır (60 bit)
	gb := make([]byte, 32)
	if _, err := randOku(ob); err != nil {
		return Uretilen{}, ErrRastgele
	}
	if _, err := randOku(gb); err != nil {
		return Uretilen{}, ErrRastgele
	}
	onek := strings.ToLower(b32.EncodeToString(ob))[:OnekUzunluk]
	gizli := base64.RawURLEncoding.EncodeToString(gb)
	tam := On + onek + "_" + gizli
	return Uretilen{Tam: tam, Onek: onek, Ozet: Ozet(tam), Son4: gizli[len(gizli)-4:]}, nil
}

// Ozet: tam anahtarın saklanan özeti (SHA-256, onaltılık 64 karakter).
func Ozet(tam string) string {
	s := sha256.Sum256([]byte(tam))
	return hex.EncodeToString(s[:])
}

// OzetEsit: sabit zamanlı karşılaştırma.
func OzetEsit(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// AnahtarMi: değer bir API anahtarı adayı mı (önekle başlıyor)? Doğrulamaz.
func AnahtarMi(deger string) bool { return strings.HasPrefix(deger, On) }

// Ayristir: biçimi denetler ve öneki döner. Biçim bozuksa ok=false.
// Önek alfabesi base32 küçük harf (a-z, 2-7); gizli base64url.
func Ayristir(tam string) (onek string, ok bool) {
	if len(tam) != TamUzunluk || !strings.HasPrefix(tam, On) {
		return "", false
	}
	govde := tam[len(On):]
	if govde[OnekUzunluk] != '_' {
		return "", false
	}
	onek, gizli := govde[:OnekUzunluk], govde[OnekUzunluk+1:]
	if !OnekGecerli(onek) {
		return "", false
	}
	for i := 0; i < len(gizli); i++ {
		c := gizli[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", false
		}
	}
	return onek, true
}

// OnekGecerli: 12 karakter, base32 küçük harf alfabesi.
func OnekGecerli(onek string) bool {
	if len(onek) != OnekUzunluk {
		return false
	}
	for i := 0; i < len(onek); i++ {
		c := onek[i]
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// Gosterim: listede/denetimde güvenli gösterim (gizlinin yalnız son 4 karakteri).
func Gosterim(onek, son4 string) string { return On + onek + "_…" + son4 }
