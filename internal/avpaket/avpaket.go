// Package avpaket — antivirüs kural setinin İMZALI + şifreli teslimat biçimi.
//
// 🔴 NEDEN VAR (security-auditor C.1): kural seti ve WP sağlamaları dış
// servisten DÜZ geliyordu; imza/pin yoktu. Sahte bir sağlama ya da kural seti
// enjekte eden bir MITM/mirror, saldırganın backdoor'unu "temiz" gösterebilir
// ya da tespiti kapatabilirdi. Bu paket o güven kökünü kapatır.
//
// 🔴 İMZA vs ŞİFRELEME — DÜRÜST AYRIM:
//
//	İMZA (Ed25519) ASIL GÜVENLİKTİR: kural setinin BİZİM ürettiğimizi kanıtlar.
//	  Ajan imzayı doğrulamadan HİÇBİR kuralı yüklemez → sahte kural enjeksiyonu
//	  imkânsız. Bu, güvenlik özelliğidir.
//	ŞİFRELEME (AES-256-GCM) yalnız CAYDIRICIDIR: rakibin kural setimizi
//	  kopyalamasını zorlaştırır. Anahtar ikiliye gömülü olduğu için root olan
//	  biri onu çıkarabilir — koruma DEĞİL, engel yükseltmedir. Bunu güvenlik
//	  temeli saymıyoruz (adversaryel dersi: "şifreleme koruma değil").
//
// BİÇİM (little-endian uzunluklar):
//
//	"GOSPAV01"        8 bayt sihir
//	u32               başlık JSON uzunluğu
//	başlık JSON       {surum, uretim, sha256(düz gövde)} — İMZALANAN
//	u32               imza uzunluğu
//	imza              Ed25519(başlık JSON ham baytları)
//	şifreli gövde     AES-256-GCM(gömülü anahtar, nonce, düz KuralSeti JSON)
//
// 🔴 İmza BAŞLIK üzerindedir, başlık gövdenin sha256'sını taşır → gövde
// değiştirilirse sha tutmaz, sha başlıkta olduğu için imza da tutmaz. Zincir:
// imza → başlık → sha → gövde. Tek Ed25519 doğrulaması tüm paketi kapsar.
package avpaket

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var sihir = []byte("GOSPAV01")

// 🔴 GÖMÜLÜ AES ANAHTARI — yalnız caydırıcı (yukarıdaki not). 32 bayt.
// Üretimde kur.sh/paketle bunu kendi anahtarıyla değiştirebilir; şimdilik
// sabit çünkü güvenlik imzada, şifrelemede değil.
var gomuluAESKey = mustHex("a7f3c1e95b4d8206f1a9c7e3d5b8046279e1c3a5f7092b4d6e8103a5c7f9b1d3")

// Baslik — imzalanan meta.
type Baslik struct {
	Surum    int    `json:"surum"`
	Uretim   string `json:"uretim"` // RFC3339 (paketle stamp'ler)
	GovdeSHA string `json:"govde_sha256"`
}

// Olustur — KuralSeti JSON'unu imzalar + şifreler, paket baytları döner.
// sk: Ed25519 özel anahtar. uretimZamani: çağıran verir (Date.now yok — Go
// projesinde de deterministik olsun diye dışarıdan).
func Olustur(setJSON []byte, sk ed25519.PrivateKey, surum int, uretimZamani string) ([]byte, error) {
	sha := sha256.Sum256(setJSON)
	baslik := Baslik{Surum: surum, Uretim: uretimZamani, GovdeSHA: hex.EncodeToString(sha[:])}
	baslikJSON, err := json.Marshal(baslik)
	if err != nil {
		return nil, err
	}
	imza := ed25519.Sign(sk, baslikJSON)

	sifreli, err := aesGCMSifrele(gomuluAESKey, setJSON)
	if err != nil {
		return nil, err
	}

	var out []byte
	out = append(out, sihir...)
	out = u32Ekle(out, uint32(len(baslikJSON)))
	out = append(out, baslikJSON...)
	out = u32Ekle(out, uint32(len(imza)))
	out = append(out, imza...)
	out = append(out, sifreli...)
	return out, nil
}

// Ac — paketi DOĞRULAR (imza) ve çözer. pk: gömülü Ed25519 açık anahtar.
// İmza tutmazsa HATA döner — çağıran ASLA doğrulanmamış kural yüklememeli.
func Ac(paket []byte, pk ed25519.PublicKey) (Baslik, []byte, error) {
	var b Baslik
	if len(paket) < len(sihir)+4 || !eq(paket[:len(sihir)], sihir) {
		return b, nil, errors.New("gecersiz av paket sihiri")
	}
	p := paket[len(sihir):]

	blen, p, err := u32Oku(p)
	if err != nil {
		return b, nil, err
	}
	if int(blen) > len(p) {
		return b, nil, errors.New("baslik uzunlugu tasiyor")
	}
	baslikJSON := p[:blen]
	p = p[blen:]

	ilen, p, err := u32Oku(p)
	if err != nil {
		return b, nil, err
	}
	if int(ilen) > len(p) {
		return b, nil, errors.New("imza uzunlugu tasiyor")
	}
	imza := p[:ilen]
	sifreli := p[ilen:]

	// 🔴 İMZA DOĞRULAMASI — güvenlik kapısı. Başlık ham baytları üzerinde.
	if !ed25519.Verify(pk, baslikJSON, imza) {
		return b, nil, errors.New("av kural imzasi DOGRULANAMADI — paket reddedildi")
	}
	if err := json.Unmarshal(baslikJSON, &b); err != nil {
		return b, nil, err
	}

	setJSON, err := aesGCMCoz(gomuluAESKey, sifreli)
	if err != nil {
		return b, nil, err
	}

	// 🔴 sha çapraz kontrol: başlık gövdenin sha'sını taşır. İmza başlığı
	// koruduğu için sha güvenilir; gövde değiştirilmişse sha tutmaz.
	sha := sha256.Sum256(setJSON)
	if hex.EncodeToString(sha[:]) != b.GovdeSHA {
		return b, nil, errors.New("govde sha256 basliktaki ile UYUSMUYOR")
	}
	return b, setJSON, nil
}

// ── AES-GCM ────────────────────────────────────────────────────────────────

func aesGCMSifrele(key, düz []byte) ([]byte, error) {
	blok, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blok)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, düz, nil), nil
}

func aesGCMCoz(key, sifreli []byte) ([]byte, error) {
	blok, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blok)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sifreli) < ns {
		return nil, errors.New("sifreli govde cok kisa")
	}
	return gcm.Open(nil, sifreli[:ns], sifreli[ns:], nil)
}

// ── yardımcılar ──────────────────────────────────────────────────────────────

func u32Ekle(b []byte, n uint32) []byte {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], n)
	return append(b, t[:]...)
}

func u32Oku(b []byte) (uint32, []byte, error) {
	if len(b) < 4 {
		return 0, b, errors.New("u32 icin yetersiz bayt")
	}
	return binary.LittleEndian.Uint32(b[:4]), b[4:], nil
}

func eq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("avpaket: gomulu anahtar hex hatasi: %v", err))
	}
	return b
}
