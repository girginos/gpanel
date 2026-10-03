package hedef

// maske.go — sağlayıcıdan/alt süreçten gelen HAM hata metninin günlüğe
// yazılmadan önce maskelenmesi.
//
// 🔴 NEDEN: uzak depo istemcileri hata metnine istek URL'sini (imzalı S3 URL'si,
// ?access_token=, kullanıcı:parola@host), başlıkları (Authorization) ya da
// sunucunun geri yansıttığı gövdeyi koyabilir. Bu metin son_hata/cp_bildirim/
// log/HTTP yanıtına ulaşırsa kimlik bilgisi sızar. İki katman:
//  1. bilinen sırlar (çağıranın verdiği parola/anahtar/jeton) ve kodlanmış
//     biçimleri birebir değiştirilir;
//  2. sır biçimli desenler (URL kullanıcı bilgisi, Authorization, anahtar=değer,
//     özel anahtar blokları, sağlayıcı jeton önekleri…) kalıpla maskelenir.
// Maskeleme KESMEDEN önce yapılır: kesme bir sırrı ikiye bölüp birebir
// eşleşmeyi kaçırtamaz.
//
// Desenler ve sır biçimleri internal/maske'dedir (tek kaynak); burada yalnız
// bu katmanın sözleşmesi durur: her uzunluktaki bilinen sır, tek satır, 512 rune.

import "girginospanel/internal/maske"

// MaskeIsareti — maskelenen değerin yerine yazılan işaret.
const MaskeIsareti = maske.Isaret

// AzamiAyrinti — maskelenmiş ayrıntının en çok rune sayısı.
const AzamiAyrinti = 512

// Maskele — ham metni günlüğe yazılabilir hale getirir: verilen sırlar ve
// kodlanmış biçimleri, sır biçimli desenler maskelenir; kontrol karakterleri
// boşluğa çevrilir; en çok AzamiAyrinti rune'a kesilir.
func Maskele(ham string, sirlar ...string) string {
	if ham == "" {
		return ""
	}
	out := maske.SirlariDegistir(ham, sirlar, 1)
	out = maske.DesenleMaskele(out)
	return maske.KesRune(maske.TekSatir(out), AzamiAyrinti)
}

// MaskeleHata — err.Error()'ı maskeler (nil → "").
func MaskeleHata(err error, sirlar ...string) string {
	if err == nil {
		return ""
	}
	return Maskele(err.Error(), sirlar...)
}
