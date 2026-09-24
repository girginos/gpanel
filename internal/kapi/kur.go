package kapi

import (
	"errors"
	"strings"
)

// ErrDisariyaKapali: panelin hiç genel listen'ı yok (erisim masaüstü modu — operatör
// paneli bilinçli olarak yalnız SSH tüneline açmış). Geçiş 3 yeni GENEL port açıp bu
// seçimi sessizce tersine çevireceği için yapılmaz.
var ErrDisariyaKapali = errors.New("panel dışarıya kapalı (erisim masaüstü modu) — kapı geçişi atlandı")

// ErrAdreseBagli: panel belirli bir adrese (ör. iç ağ/VPN IP'si) bağlı. Rol kapıları
// joker listen'la (tüm arayüzler) eklenir; geçiş operatörün bu ağ kısıtlamasını —
// yönetici yüzeyi dahil — sessizce kaldırırdı. Elle yapılandırılmalı.
var ErrAdreseBagli = errors.New("panel belirli bir adrese bağlı (iç ağ/VPN IP'si) — kapı geçişi atlandı: rol kapıları tüm arayüzlerde açılıp bu kısıtlamayı kaldırırdı")

// KurulumAyari: taze kurulum için rol portları. Eski kapı yoktur: şablonun 8443'ü
// Heal tarafından "beklenmeyen genel listen" olarak kaldırılır (kullanıcı yok).
func KurulumAyari(uygun func(int) bool, simdi int64) (*Ayar, error) {
	ad, by, mu, err := RolPortlariSec(uygun, nil)
	if err != nil {
		return nil, err
	}
	return &Ayar{Surum: 1, Admin: ad, Bayi: by, Musteri: mu, Olusturma: simdi}, nil
}

// GecisAyari: mevcut kurulumdan geçiş. Panelin BUGÜNKÜ tüm genel portları "eski" olur
// (tek tanesini seçip kalanları kesmek, onları kullananları kilitlerdi) ve en az gun
// gün + admin yeni kapıdan girene dek açık kalır.
func GecisAyari(panelConf string, uygun func(int) bool, simdi int64, gun int) (*Ayar, error) {
	eski := GenelPortlar(panelConf)
	if len(eski) == 0 {
		return nil, ErrDisariyaKapali
	}
	for _, d := range Dinlemeler(strings.Split(panelConf, "\n")) {
		if d.Genel() && d.Adresli {
			return nil, ErrAdreseBagli
		}
	}
	if gun < 1 {
		gun = VarsayilanGecisGun
	}
	// Rol portu, panel bloğunda İZİ olan HİÇBİR porta düşmemeli: genel (eski kapı),
	// yalnız-loopback (SSH tüneli) ve erisim'in yoruma aldığı satırlar. Uzlastir izi
	// olan portu "bilinçli yönetiliyor" sayıp listen EKLEMEZ → oraya düşen rol kapısı
	// hiç genel dinlenmezdi. `ss` bunu her zaman yakalamaz (nginx dosyayı henüz
	// yüklememiş / yorumlu satır). KapiRolPortuAta da YerelPortlar'ı aynı nedenle hariç tutar.
	haric := map[int]bool{}
	for _, d := range Dinlemeler(strings.Split(panelConf, "\n")) {
		if d.Port > 0 {
			haric[d.Port] = true
		}
	}
	ad, by, mu, err := RolPortlariSec(uygun, haric)
	if err != nil {
		return nil, err
	}
	return &Ayar{
		Surum: 1, Admin: ad, Bayi: by, Musteri: mu,
		EskiPortlar: eski,
		EskiBitis:   simdi + int64(gun)*86400,
		Olusturma:   simdi,
	}, nil
}
