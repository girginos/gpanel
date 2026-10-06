// Package gizli: panel veritabaninda saklanan hassas degerlerin (MySQL kullanici
// parolalari) AT-REST sifrelenmesi.
//
// 🔴 NEDEN: db_accounts.db_pass_plain kiraci MySQL parolalarini DUZ METIN
// tutuyordu. Panel DB'sinin bir yedegi/dump'i ya da sinirli bir okuma erisimi
// TUM kiracilarin veritabani parolalarini ifsa ediyordu. phpMyAdmin SSO ve
// "parolami goster" akislari geri-donusturulebilir deger gerektirdigi icin
// hash kullanilamaz → sunucuya ozel anahtarla AES-256-GCM.
//
// Anahtar: /etc/girginospanel/db.key (0600, root). Yoksa ilk calistirmada
// uretilir. Anahtar yedeklenmeden DB tasinirsa parolalar COZULEMEZ — bu yuzden
// Coz() cozemedigi degeri OLDUGU GIBI dondurur (eski duz-metin kayitlarla
// geriye donuk uyum + anahtar kaybinda panelin calismaya devam etmesi).
package gizli

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"girginospanel/internal/sirdosya"
)

const onEk = "gos1:" // sifreli deger isareti (duz metinden ayirmak icin)

var (
	anahtarYol = "/etc/girginospanel/db.key" // testte gecici dosyaya cevrilir
	birKez     sync.Once
	anahtar    []byte
)

func anahtariYukle() {
	birKez.Do(func() {
		if b, err := os.ReadFile(anahtarYol); err == nil && len(b) >= 32 {
			anahtar = b[:32]
			return
		}
		yeni := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, yeni); err != nil {
			return // anahtar yok → Sakla() duz metne duser (islev bozulmaz)
		}
		if err := os.MkdirAll(filepath.Dir(anahtarYol), 0o700); err != nil {
			return
		}
		// sirdosya: atomik + izin TAM 0600 (önceden kalmış geniş izinli dosya üzerine
		// yazılsa bile) + symlink takip edilmez.
		if err := sirdosya.YazKok(anahtarYol, yeni, 0o600); err != nil {
			return
		}
		anahtar = yeni
	})
}

func aead() (cipher.AEAD, bool) {
	anahtariYukle()
	if len(anahtar) != 32 {
		return nil, false
	}
	blok, err := aes.NewCipher(anahtar)
	if err != nil {
		return nil, false
	}
	g, err := cipher.NewGCM(blok)
	if err != nil {
		return nil, false
	}
	return g, true
}

// Sakla: duz metni sifreler. Anahtar yoksa degeri OLDUGU GIBI dondurur —
// sifreleme kurulamadi diye parola kaydi kaybolmasin.
//
// 🔴 "Zaten sifreli gorunuyorsa dokunma" KISAYOLU YOK: girdi kullanici-kontrollu
// olabilir; onek atlamasi, baskasinin ciphertext'ini yazip okuyarak duz metne
// ulasmayi (sifre-cozme oracle'i) mumkun kiliyordu. Idempotency gereken TEK yer
// gecis migration'i → SaklaGecis.
func Sakla(duz string) string { return SaklaBagli(duz, "") }

// SaklaBagli: baglam (ornegin DB kullanici adi) AEAD'nin ek-verisine baglanir.
// Boylece bir satirin ciphertext'i BASKA bir satira tasinip cozulemez.
func SaklaBagli(duz, baglam string) string {
	if duz == "" {
		return duz
	}
	g, ok := aead()
	if !ok {
		return duz
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return duz
	}
	kapali := g.Seal(nonce, nonce, []byte(duz), []byte(baglam))
	return onEk + base64.RawStdEncoding.EncodeToString(kapali)
}

// SaklaGecis: YALNIZ gecis migration'i icin — DB'den okunan, zaten sifreli
// olabilecek degeri tekrar sifrelemez. Kullanici girdisinde ASLA kullanma.
func SaklaGecis(deger, baglam string) string {
	if SifreliMi(deger) {
		return deger
	}
	return SaklaBagli(deger, baglam)
}

// Coz: baglamsiz cozme (geriye donuk uyum).
func Coz(deger string) string { return CozBagli(deger, "") }

// CozBagli: baglama bagli cozme. Once verilen baglamla, olmazsa baglamsiz dener
// (baglam eklenmeden ONCE yazilmis eski kayitlar icin). Ikisi de olmazsa girdiyi
// oldugu gibi dondurur — yani BASKA bir satirin ciphertext'i asla cozulmez.
func CozBagli(deger, baglam string) string {
	if !strings.HasPrefix(deger, onEk) {
		return deger
	}
	g, ok := aead()
	if !ok {
		return deger
	}
	ham, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(deger, onEk))
	if err != nil || len(ham) < g.NonceSize() {
		return deger
	}
	nonce, govde := ham[:g.NonceSize()], ham[g.NonceSize():]
	if baglam != "" {
		if acik, err := g.Open(nil, nonce, govde, []byte(baglam)); err == nil {
			return string(acik)
		}
	}
	acik, err := g.Open(nil, nonce, govde, nil) // baglamsiz eski kayit
	if err != nil {
		return deger
	}
	return string(acik)
}

// SifreliMi: deger at-rest sifreli mi (gecis/denetim icin).
func SifreliMi(deger string) bool { return strings.HasPrefix(deger, onEk) }

// Sha256Hex: deterministik arama anahtari. AEAD non-deterministik oldugu icin
// WHERE ile aranamayan sirlar (orn git webhook_secret) icin hash sutunu uretir.
func Sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// AnahtarUret: db.key YOKSA üretir (panel açılışında bir kez çağrılır). Temiz kurulumda hiçbir
// sır henüz saklanmadığı için anahtar tembelce hiç üretilmezdi; anahtarı ASLA üretmeyen
// girginospanel-destek-ajan (AnahtarHazir) ise db.key yokken başlamayıp her birkaç saniyede
// çöküp yeniden başlıyordu. Var olan geçerli anahtara dokunmaz.
func AnahtarUret() error {
	anahtariYukle()
	return AnahtarHazir()
}

// AnahtarHazir: db.key okunabilir ve geçerli mi — ASLA ÜRETMEZ/YAZMAZ.
//
// 🔴 Anahtarı üretmemesi gereken ikincil süreçler (girginospanel-destek-ajan: cihaz kimliği
// db.key'e bağlı mühürlüdür) açılışta bunu çağırır. aead() anahtar OKUNAMAZSA yenisini
// üretip db.key'in üstüne YAZAR; ikincil süreçte bu, ortam sorunu (izin, bağlama) yüzünden
// tüm panelin sırlarını çözülemez kılar ve cihaz kimliğini sessizce değiştirirdi.
func AnahtarHazir() error {
	b, err := os.ReadFile(anahtarYol)
	if err != nil {
		return err
	}
	if len(b) < 32 {
		return errors.New("db.key kısa/bozuk")
	}
	return nil
}
