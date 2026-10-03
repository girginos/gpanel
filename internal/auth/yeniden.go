package auth

import (
	"database/sql"
	"log"
	"strings"

	"girginospanel/internal/gizli"

	"golang.org/x/crypto/bcrypt"
)

// YDSonuc: yeniden doğrulama sonucu.
type YDSonuc int

const (
	YDTamam        YDSonuc = iota
	YDParolaHatali         // parola yanlış
	YDKodGerekli           // 2FA açık, kod gönderilmedi
	YDKodHatali            // TOTP yanlış ya da tekrar kullanıldı
	YDHata                 // durum okunamadı (fail-closed)
)

// YenidenDogrula: hassas işlem (API anahtarı üretme/döndürme) öncesi OTURUM SAHİBİNİN
// parolasını ve 2FA açıksa TOTP kodunu yeniden doğrular. Çalınmış bir oturum jetonu
// (8 saat) tek başına KALICI bir anahtara dönüştürülemesin.
//
// Giriş akışıyla AYNI doğrulayıcılar (rootParolaKontrol / bcrypt / TOTPVerifyAdim +
// totp_last_step tekrar koruması) kullanılır; Login gövdesine dokunulmaz.
// FAIL-CLOSED: 2FA durumu okunamazsa YDHata.
func YenidenDogrula(db *sql.DB, uid int64, rol, parola, kod string) YDSonuc {
	if db == nil || uid <= 0 || parola == "" {
		return YDParolaHatali
	}
	switch rol {
	case "admin":
		if uid != 1 || !rootParolaKontrol(parola) {
			return YDParolaHatali
		}
	case "reseller":
		var hash string
		err := db.QueryRow(`SELECT password_hash FROM users WHERE id=? AND role='reseller'`, uid).Scan(&hash)
		if err != nil || hash == "" {
			_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(parola)) // sabit-zaman
			return YDParolaHatali
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(parola)) != nil {
			return YDParolaHatali
		}
	default:
		return YDParolaHatali
	}
	var en int
	var sec string
	var sonAdim int64
	if err := db.QueryRow(`SELECT totp_enabled, COALESCE(totp_secret,''), COALESCE(totp_last_step,0) FROM users WHERE id=?`, uid).Scan(&en, &sec, &sonAdim); err != nil {
		return YDHata
	}
	if en != 1 {
		return YDTamam
	}
	sec = gizli.CozBagli(sec, "totp")
	if strings.TrimSpace(sec) == "" {
		return YDHata
	}
	if strings.TrimSpace(kod) == "" {
		return YDKodGerekli
	}
	adim, ok := TOTPVerifyAdim(sec, kod, sonAdim)
	if !ok {
		return YDKodHatali
	}
	if _, err := db.Exec(`UPDATE users SET totp_last_step=? WHERE id=?`, adim, uid); err != nil {
		log.Printf("GÜVENLİK: totp_last_step yazılamadı (uid=%d): %v — TOTP kodu tekrar kullanılabilir", uid, err)
	}
	return YDTamam
}
