package tasima

import (
	"errors"

	"girginospanel/internal/domains"
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
// yöneticiye ve sunucu günlüğüne gider (nginxset.SunucuHatasiGenel ile aynı ilke).
const tasimaHataGenel = "taşıma sırasında bir hata oluştu; ayrıntı sunucu günlüğüne yazıldı, sorun sürerse yöneticinize başvurun"

// hataMetni — bir taşıma hatasını KAPSAMA göre metne çevirir.
//
//   - Yönetici (kök): hatayı OLDUĞU GİBİ döndürür (tip ve zincir korunur; errors.As
//     ile AdAlaniHatasi hâlâ bulunur) → mevcut davranış birebir.
//   - Bayi: AdAlaniHatasi ise güvenli genel mesajını (Mesaj(false)); başka her hata
//     için sabit genel metni döndürür — komşu kiracının adı, sistem yolu ya da araç
//     çıktısı bayiye SIZMAZ.
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
	return errors.New(tasimaHataGenel)
}
