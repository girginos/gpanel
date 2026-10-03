package apianahtar

// kimlik.go — Authorization başlığı biçimi: anahtar YALNIZ sahibinin kullanıcı
// adıyla birlikte geçerlidir (cPanel/WHM "whm <kullanıcı>:<jeton>" modeli; WHMCS
// sunucu formundaki "Username" + "API Token" alanlarına birebir).
//
//	Authorization: gosp <kullanıcı adı>:<gosp_önek_gizli>
//
// 🔴 Eski "Bearer gosp_…" biçimi KABUL EDİLMEZ (kullanıcı kararı "yalnız
// kullanıcı adıyla birlikte geçerli"; D4 yayınlanmadı → geçiş yükü yok). Eski
// biçimi kabul etmek kullanıcı adı bağını süse çevirirdi: saldırgan yalnız
// "Bearer" gönderip eşleşmeyi atlardı.

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// Sema: Authorization şeması (harf duyarsız).
const Sema = "gosp"

// KokKullaniciAdi: kök anahtarının kullanıcı adı. JWT'deki usr ile aynı ("root");
// users.username (ör. "admin") da kabul edilir.
const KokKullaniciAdi = "root"

// MaksKullaniciUzunluk: users.username VARCHAR(64).
const MaksKullaniciUzunluk = 64

// EskiBicimMesaji: "Bearer gosp_…" ile gelen isteğe dönen yönlendirici hata.
const EskiBicimMesaji = "API anahtarı 'Authorization: gosp <kullanıcı adı>:<anahtar>' biçimiyle gönderilmeli"

// SemaMi: başlık değeri "gosp <...>" şemasıyla mı başlıyor? Değeri (şemasız) döner.
func SemaMi(baslik string) (deger string, ok bool) {
	sema, kalan, var_ := strings.Cut(strings.TrimSpace(baslik), " ")
	if !var_ || !strings.EqualFold(sema, Sema) {
		return "", false
	}
	return strings.TrimSpace(kalan), true
}

// KimlikAyristir: "<kullanıcı>:<anahtar>" → parçalar. Anahtar ':' içermez
// (base32 + base64url + '_'), bu yüzden SON ':' ayraçtır. Biçim denetimi
// (Ayristir) çağıranındır; burada yalnız kabaca bölünür.
func KimlikAyristir(deger string) (kullanici, tam string, ok bool) {
	i := strings.LastIndexByte(deger, ':')
	if i <= 0 || i == len(deger)-1 {
		return "", "", false
	}
	kullanici, tam = deger[:i], deger[i+1:]
	if len(kullanici) > MaksKullaniciUzunluk || strings.ContainsAny(kullanici, " \t\r\n") {
		return "", "", false
	}
	return kullanici, tam, true
}

// KullaniciEsit: sabit zamanlı, harf duyarsız kullanıcı adı karşılaştırması
// (uzunluk da sızmasın diye özetler karşılaştırılır).
func KullaniciEsit(beklenen, verilen string) bool {
	a := sha256.Sum256([]byte(strings.ToLower(beklenen)))
	b := sha256.Sum256([]byte(strings.ToLower(verilen)))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1 && beklenen != ""
}
