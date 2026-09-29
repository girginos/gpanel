package hedef

// gecici.go — yarım (henüz nihai ada taşınmamış) UZAK yüklemelerin ortak adı.
//
// 🔴 NEDEN ORTAK: her tür kendi geçici ad biçimini seçseydi orkestratörün
// "24 saatten eski yarım dosyaları sil" temizliği ve sahipsiz dosya raporu
// türden bağımsız yazılamazdı. Sözleşme:
//   - Geçici dosya gerekiyorsa adı UzakGeciciAd ile üretilir ve nihai adla AYNI
//     dizindedir (Listele(dizin/) onu görür, yeniden adlandırma dizin içidir).
//   - Yukle hata/iptal ile biterse geçici dosyayı ayrı, kısa ömürlü bir bağlamla
//     (context.WithoutCancel + zaman aşımı) silmeyi dener.
//   - Silinemeyen (bağlantı koptu, süreç öldü) geçici dosya GeciciMi ile tanınır;
//     orkestratör GeciciZamani (ya da Nesne.Degisim) 24 saati geçince siler.
//   - Sağlayıcının kendi atomik yüklemesi (S3 çok parçalı) görünür dosya
//     bırakmaz; o türde geçici ad hiç kullanılmayabilir.
// Nihai adlar UzakGeciciOnek ile BAŞLAMAMALI (temizlik onları yarım sanar).

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// UzakGeciciOnek — yarım uzak yüklemelerin DOSYA adı öneki (dizin bileşeni değil).
const UzakGeciciOnek = ".gosp-yarim-"

// UzakGeciciAd — ad için AYNI dizinde benzersiz geçici ad üretir:
//
//	<dizin>/.gosp-yarim-<unix saniye>-<12 hex>-<dosya adı>
//
// Dosya adı yalnız okunurluk içindir; toplam AzamiAdUzunlugu baytı aşacaksa
// (UTF-8 bütünlüğü korunarak) kısaltılır. Geçersiz ad, zaten geçici olan ad ya
// da sığmayacak kadar uzun dizin → ErrGecersizAd.
func UzakGeciciAd(ad string) (string, error) { return uzakGeciciAd(ad, time.Now()) }

func uzakGeciciAd(ad string, simdi time.Time) (string, error) {
	if err := AdDogrula(ad); err != nil {
		return "", err
	}
	if GeciciMi(ad) {
		return "", ErrGecersizAd
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	dizin, dosya := AdBol(ad)
	bas := UzakGeciciOnek + strconv.FormatInt(simdi.Unix(), 10) + "-" + hex.EncodeToString(rnd[:]) + "-"
	if dizin != "" {
		bas = dizin + "/" + bas
	}
	kalan := AzamiAdUzunlugu - len(bas)
	if kalan < 1 {
		return "", ErrGecersizAd
	}
	if len(dosya) > kalan {
		for len(dosya) > kalan {
			_, boy := utf8.DecodeLastRuneInString(dosya)
			dosya = dosya[:len(dosya)-boy]
		}
		// Sonda boşluk/nokta kalmasın (Windows SMB sunucuları bunları atar).
		dosya = strings.TrimRight(dosya, " .")
	}
	g := bas + dosya
	if err := AdDogrula(g); err != nil {
		return "", err
	}
	return g, nil
}

// GeciciMi — adın DOSYA bileşeni UzakGeciciOnek ile mi başlıyor?
func GeciciMi(ad string) bool {
	_, dosya := AdBol(ad)
	return strings.HasPrefix(dosya, UzakGeciciOnek)
}

// GeciciZamani — UzakGeciciAd'ın adına yazdığı oluşturma zamanı. Sağlayıcı
// değişim zamanı bildirmiyorsa (Nesne.Degisim sıfır) 24 saat temizliği bunu
// kullanır. Geçici ad değilse ya da zaman okunamıyorsa ok=false.
func GeciciZamani(ad string) (time.Time, bool) {
	if !GeciciMi(ad) {
		return time.Time{}, false
	}
	_, dosya := AdBol(ad)
	govde := strings.TrimPrefix(dosya, UzakGeciciOnek)
	i := strings.IndexByte(govde, '-')
	if i <= 0 {
		return time.Time{}, false
	}
	sn, err := strconv.ParseInt(govde[:i], 10, 64)
	if err != nil || sn <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sn, 0), true
}
