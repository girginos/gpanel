package protokol

import (
	"errors"
	"fmt"
	"time"
)

// Surum: protokol sürümü. Zarftaki "surum" bundan farklıysa mesaj reddedilir.
const Surum = 1

// Merkez uçları (bot.girginos.io, Cloudflare arkasında, 443).
const (
	MerkezHost = "bot.girginos.io"

	// WSSYolu: ajanın kalıcı bağlantısı (wss://MerkezHost + WSSYolu).
	WSSYolu = "/v1/ajan/ws"
	// WSAltProtokol: Sec-WebSocket-Protocol değeri; uyuşmazsa merkez el sıkışmayı reddeder.
	WSAltProtokol = "girginos-destek.v1"

	// Uzun yoklama yedeği (WSS kurulamazsa). Gövde = tek Zarf (POST) ya da Zarf dizisi (al).
	YoklamaKayitYolu  = "/v1/ajan/kayit"  // POST kayit → meydan_okuma
	YoklamaYanitYolu  = "/v1/ajan/yanit"  // POST meydan_yanit → kayit_sonuc (oturum_belirteci)
	YoklamaAlYolu     = "/v1/ajan/al"     // POST (boş) → merkez→ajan zarfları; en çok YoklamaBekleme bekler
	YoklamaGonderYolu = "/v1/ajan/gonder" // POST ajan→merkez zarfı
	// YoklamaBelirtecBasligi: yoklama isteklerinde "Bearer <oturum_belirteci>".
	YoklamaBelirtecBasligi = "Authorization"

	// TelegramWebhookYolu: Telegram → merkez (X-Telegram-Bot-Api-Secret-Token zorunlu).
	TelegramWebhookYolu = "/v1/telegram/webhook"
)

// Boyut ve zaman sınırları.
const (
	CerceveSiniri     = 512 << 10 // tek zarfın en büyük JSON boyutu (bayt)
	ParamSiniri       = 8 << 10   // is_emri.param / tool_use girdisi (bayt)
	OzetSiniri        = 4000      // is_sonucu.ozet (bayt)
	SatirSiniri       = 1024      // is_sonucu.satirlar[i] (bayt)
	SatirSayisiSiniri = 300       // is_sonucu.satirlar uzunluğu
	AyrintiSiniri     = 500       // hata.ayrinti (bayt)
	LisansIspatSiniri = 3072      // kayit.lisans_ispati çözülmüş bayt
	EnCokHesap        = 500       // bir oturumdaki hesap sayısı
	varsayilanDizge   = 256       // EnUzun verilmemiş serbest dizge parametresi

	EnUzunGecerlilik = 60 * time.Second // imzalı mesajın son_gecerlilik − verilme üst sınırı
	SaatSapmasi      = 5 * time.Second  // doğrulayıcının saat sapması toleransı
	MeydanGecerlilik = 30 * time.Second // meydan_okuma ömrü
	YoklamaBekleme   = 25 * time.Second
	KalpAraligi      = 20 * time.Second
	KalpZamanAsimi   = 3 * KalpAraligi
)

// HataKodu: taraflar arası makinece okunur hata kodu (hata.kod, is_sonucu.hata_kodu, kayit_sonuc.hata_kodu).
type HataKodu string

const (
	HataSurum             HataKodu = "surum_desteklenmiyor"
	HataBicim             HataKodu = "bicim_hatali"
	HataBilinmeyenAlan    HataKodu = "bilinmeyen_alan"
	HataCerceveBuyuk      HataKodu = "cerceve_buyuk"
	HataTipBilinmiyor     HataKodu = "tip_bilinmiyor"
	HataYon               HataKodu = "yon_gecersiz"
	HataImza              HataKodu = "imza_gecersiz"
	HataAnahtarBilinmiyor HataKodu = "anahtar_bilinmiyor"
	HataZaman             HataKodu = "zaman_gecersiz"
	HataSuresiDoldu       HataKodu = "suresi_doldu"
	HataGecerlilikUzun    HataKodu = "gecerlilik_uzun"
	HataTekrar            HataKodu = "tekrar"
	HataLisans            HataKodu = "lisans_gecersiz"
	HataMeydan            HataKodu = "meydan_gecersiz"
	HataEslesmeKodu       HataKodu = "eslesme_kodu_gecersiz"
	HataBaglantiYok       HataKodu = "baglanti_yok"
	HataBaglantiIptal     HataKodu = "baglanti_iptal"
	HataOnayBekliyor      HataKodu = "panel_onayi_bekliyor"
	HataAracBilinmiyor    HataKodu = "arac_bilinmiyor"
	HataParam             HataKodu = "param_gecersiz"
	HataYetki             HataKodu = "yetki_yok"
	HataKapsam            HataKodu = "kapsam_disi"
	HataDuzey             HataKodu = "duzey_yetersiz"
	HataKota              HataKodu = "kota_asildi"
	HataHiz               HataKodu = "hiz_siniri"
	HataMesgul            HataKodu = "mesgul"
	HataZamanAsimi        HataKodu = "zaman_asimi"
	HataDevreDisi         HataKodu = "destek_kapali"
	HataIc                HataKodu = "ic_hata"
)

// HataKodlari: tanımlı kodların tamamı (sıra sabit).
var HataKodlari = []HataKodu{
	HataSurum, HataBicim, HataBilinmeyenAlan, HataCerceveBuyuk, HataTipBilinmiyor, HataYon,
	HataImza, HataAnahtarBilinmiyor, HataZaman, HataSuresiDoldu, HataGecerlilikUzun, HataTekrar,
	HataLisans, HataMeydan, HataEslesmeKodu, HataBaglantiYok, HataBaglantiIptal, HataOnayBekliyor,
	HataAracBilinmiyor, HataParam, HataYetki, HataKapsam, HataDuzey, HataKota, HataHiz, HataMesgul,
	HataZamanAsimi, HataDevreDisi, HataIc,
}

// Gecerli: kod tanımlı mı.
func (k HataKodu) Gecerli() bool {
	for _, h := range HataKodlari {
		if h == k {
			return true
		}
	}
	return false
}

// Hata: bu paketin döndürdüğü hata. errors.Is(err, &Hata{Kod: X}) koda göre eşleşir.
type Hata struct {
	Kod     HataKodu
	Ayrinti string
}

func (h *Hata) Error() string {
	if h.Ayrinti == "" {
		return string(h.Kod)
	}
	return string(h.Kod) + ": " + h.Ayrinti
}

// Is: aynı koda sahip *Hata ile eşleşir (ayrıntıya bakmaz).
func (h *Hata) Is(hedef error) bool {
	t, ok := hedef.(*Hata)
	return ok && t.Kod == h.Kod
}

func hataf(k HataKodu, bicim string, a ...any) error {
	return &Hata{Kod: k, Ayrinti: fmt.Sprintf(bicim, a...)}
}

// HataKoduAl: err içindeki protokol hata kodu; nil → "", yabancı hata → HataIc.
func HataKoduAl(err error) HataKodu {
	if err == nil {
		return ""
	}
	var h *Hata
	if errors.As(err, &h) {
		return h.Kod
	}
	return HataIc
}
