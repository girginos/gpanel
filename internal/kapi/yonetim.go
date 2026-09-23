package kapi

import (
	"errors"
	"fmt"
)

// ErrEskiYok: açık eski kapı yok (geçiş tamamlanmış ya da hiç yapılmamış).
var ErrEskiYok = errors.New("açık eski kapı yok")

// AdminDogrulanmadiHatasi: admin yeni admin kapısından henüz hiç giriş yapmadı.
type AdminDogrulanmadiHatasi struct{ AdminPort int }

func (e *AdminDogrulanmadiHatasi) Error() string {
	return fmt.Sprintf("admin YENİ admin kapısından (:%d) henüz hiç giriş yapmadı. Sağlayıcı "+
		"firewall'u o portu kesiyorsa eski kapıyı kapatmak herkesi kilitler. Önce yeni adresten "+
		"giriş yapın ya da riski üstlenip zorla kapatın", e.AdminPort)
}

// EskiUzat: eski kapının açık kalma süresini gün kadar uzatır — şimdiden ya da
// mevcut bitişten, hangisi sonraysa (kısaltma yapmaz).
func EskiUzat(yol string, gun int, simdi int64) error {
	if gun < 1 {
		return errors.New("gün pozitif bir tam sayı olmalı")
	}
	return Guncelle(yol, func(a *Ayar) error {
		if len(a.EskiPortlar) == 0 {
			return ErrEskiYok
		}
		taban := simdi
		if a.EskiBitis > taban {
			taban = a.EskiBitis
		}
		a.EskiBitis = taban + int64(gun)*86400
		return nil
	})
}

// EskiKapat: eski kapıyı şimdi kapatır. Admin yeni kapıdan hiç girmediyse zorla
// olmadan REDDEDİLİR (*AdminDogrulanmadiHatasi). zorla: eski portlar ayardan düşer;
// Heal yine de admin kapısı dinlemeden hiçbir portu kaldırmaz.
func EskiKapat(yol string, zorla bool, simdi int64) error {
	return Guncelle(yol, func(a *Ayar) error {
		if len(a.EskiPortlar) == 0 {
			return ErrEskiYok
		}
		if a.AdminDogrulandi == 0 && !zorla {
			return &AdminDogrulanmadiHatasi{AdminPort: a.Admin}
		}
		if zorla {
			a.EskiPortlar, a.EskiBitis = nil, 0
			return nil
		}
		a.EskiBitis = simdi
		return nil
	})
}

// ErrAyniPort: istenen port rolün zaten kullandığı port.
var ErrAyniPort = errors.New("rol zaten bu portta — değişiklik yok")

// ErrEskiDevralma: admin yeni admin kapısını doğrulamadan açık bir eski port başka bir
// role devredilemez — devretmek geçiş kapısını (ya da onun bir portunu) kapatır.
var ErrEskiDevralma = errors.New("açık eski kapı portu, admin YENİ admin kapısından bir kez giriş " +
	"yapmadan bir role devredilemez: devretmek o adresteki geçiş kapısını kapatır ve sağlayıcı " +
	"firewall'u yeni admin portunu kesiyorsa admin kilitlenir. Önce yeni admin adresinden giriş yapın")

// RolPortuDegistir: bir rolün kapı portunu AYAR düzeyinde değiştirir; nginx'i Heal /
// provisioner.KapiSimdiUygula uygular. yeni'nin sistemde uygunluğunu (yasaklı, meşgul,
// firewall'da kapalı) ÇAĞIRAN denetler; burada yalnız ayar tutarlılığı sağlanır.
//
// Açık bir eski port rol portu yapılabilir (ör. müşteri kapısı bilinen 8443'te kalsın):
// port eski listesinden çıkar, o rolün kapısı olur — ama yalnız admin yeni admin
// kapısını doğruladıktan sonra (ErrEskiDevralma).
//
// 🔴 ADMIN portu değişince önceki admin portu "eski kapı" olur ve admin YENİ porttan
// girene dek açık kalır: sağlayıcının dış firewall'u yeni portu kesiyorsa önceki port
// da kapanırsa admin kilitlenirdi. Bayi/müşteri portu hemen taşınır (admin kendi
// kapısından ulaşmaya devam eder; kullanıcılara yeni adresi o bildirir). Değişen rolün
// kapı kanıtı sıfırlanır: yeni port henüz dışarıdan hiç kullanılmadı.
func RolPortuDegistir(yol, rol string, yeni int, simdi int64) (eski int, err error) {
	err = Guncelle(yol, func(a *Ayar) error {
		eski = a.RolPortu(rol)
		if eski == 0 {
			return fmt.Errorf("bilinmeyen rol: %q (admin|bayi|musteri)", rol)
		}
		if yeni == eski {
			return ErrAyniPort
		}
		kalan := a.EskiPortlar[:0:0]
		for _, p := range a.EskiPortlar {
			if p != yeni {
				kalan = append(kalan, p)
			}
		}
		if len(kalan) != len(a.EskiPortlar) && a.AdminDogrulandi == 0 {
			return ErrEskiDevralma
		}
		a.EskiPortlar = kalan
		switch rol {
		case Admin:
			a.Admin = yeni
			a.EskiPortlar = append(a.EskiPortlar, eski)
			if a.EskiBitis < simdi {
				a.EskiBitis = simdi
			}
			a.AdminDogrulandi = 0
		case Bayi:
			a.Bayi = yeni
			delete(a.IlkGiris, Bayi)
		case Musteri:
			a.Musteri = yeni
			delete(a.IlkGiris, Musteri)
		}
		if len(a.EskiPortlar) == 0 {
			a.EskiPortlar, a.EskiBitis = nil, 0
		}
		return nil
	})
	return eski, err
}
