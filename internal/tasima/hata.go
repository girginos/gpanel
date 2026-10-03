package tasima

import (
	"errors"

	"girginospanel/internal/domains"
	"girginospanel/internal/plans"
)

// Kapsam — bir taşıma işinin YÜRÜTME kapsamı. İşin sahibi (sahip_rid) sütunundan
// türetilir; Ayarlar JSON'undan (istemci gövdesinden) ASLA okunmaz. Böylece bir
// bayi, gövdeye başka bir sahip/kapsam koyarak siteyi başka kiracının hesabına
// açtıramaz.
//
//   - RID == 0 → kök (panel sahibi / admin) işi: mevcut davranış.
//   - RID  > 0 → bayi işi: site YALNIZ o bayinin (users.id = RID) kendi hesabına
//     açılır; gövdedeki ResellerID/CustomerID/SahipEsle yok sayılır, orijinal DB
//     kimliği yolu kapatılır, üzerine yazmada hedefin sahibi bayinin kendisi olmalı,
//     bayiye dönen hata metni ayrıntısızdır.
type Kapsam struct {
	RID int64
}

// Bayi — bu iş bir bayi (reseller) kapsamında mı yürütülüyor?
func (kp Kapsam) Bayi() bool { return kp.RID > 0 }

// Yonetici — bu iş kök (admin) kapsamında mı? Hata metni ve ayrıntı görünürlüğü
// buna bağlıdır.
func (kp Kapsam) Yonetici() bool { return kp.RID == 0 }

// tasimaHataGenel — bayiye (yönetici olmayana) dönen, iç ayrıntı içermeyen metin.
// Ayrıntı (başka kiracının adı, sistem yolu, uzak/yerel araç çıktısı) yalnız
// yöneticiye gider ve bayi hatasında HER ZAMAN sunucu günlüğüne yazılır
// (HesapAktar) — nginxset.SunucuHatasi ile aynı ilke.
//
// nginxset.SunucuHatasiGenel yeniden KULLANILMADI: metni "sunucu yapılandırması
// uygulanamadı" der; taşıma hatalarının çoğu (dosya/DB aktarımı, sağlama, üzerine
// yazma reddi) yapılandırma hatası değildir → bayiyi yanlış yönlendirirdi.
const tasimaHataGenel = "taşıma sırasında bir hata oluştu; ayrıntı sunucu günlüğüne yazıldı, sorun sürerse yöneticinize başvurun"

// bayiyeAcikHatalar — bayiye metniyle AYNEN gösterilen ret nedenleri: sabit metin,
// başka kiracının adı/kimliği ya da sistem yolu içermez ve bayinin kendi
// düzeltebileceği (ya da yöneticisine soracağı) durumu anlatır. Sarılı gelse de
// (fmt.Errorf %w) yalnız SABİT hata döner — sarmalayıcının bağlamı sızmaz.
var bayiyeAcikHatalar = []error{
	errBayiSandboxYok,
	plans.ErrPlanKapsamDisi,
	plans.ErrOzelPlanBaskaHosting,
}

// hataMetni — bir taşıma hatasını KAPSAMA göre metne çevirir.
//
//   - Yönetici (kök): hatayı OLDUĞU GİBİ döndürür (tip ve zincir korunur; errors.As
//     ile AdAlaniHatasi hâlâ bulunur) → mevcut davranış birebir.
//   - Bayi: AdAlaniHatasi ise güvenli genel mesajını (Mesaj(false)); bayiyeAcikHatalar
//     ise o sabit hatayı; başka her hata için sabit genel metni döndürür — komşu
//     kiracının adı, sistem yolu ya da araç çıktısı bayiye SIZMAZ.
func hataMetni(yonetici bool, err error) error {
	if err == nil {
		return nil
	}
	if yonetici {
		return err
	}
	var ah *domains.AdAlaniHatasi
	if errors.As(err, &ah) {
		return errors.New(ah.Mesaj(false))
	}
	for _, acik := range bayiyeAcikHatalar {
		if errors.Is(err, acik) {
			return acik
		}
	}
	return errors.New(tasimaHataGenel)
}
