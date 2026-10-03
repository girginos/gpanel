package ajan

import (
	"strconv"
	"sync"
	"time"

	p "girginospanel/internal/destek/protokol"
)

// Eşleştirme kodları (adina deseni): bellekte YALNIZ SHA-256 özetiyle, 10 dk,
// tek kullanımlık. Kodun kendisi yalnız onu üreten panel isteğinin yanıtında görünür;
// merkeze yalnız özeti gider. Süreç yeniden başlarsa bekleyen kodlar düşer (yeniden üretilir).
const (
	kodSiniri       = 10000 // bellekteki toplam bekleyen kod
	sahipKodSiniri  = 3     // bir sahibin aynı anda geçerli kod sayısı
	bekleyenSiniri  = 5     // bir sahibin onay bekleyen bağlantı sayısı
	bekleyenOmru    = 30 * time.Minute
	enUzunSureGun   = 30
	varsayilanSure  = 7
	aydinlatmaSurum = "2026-10-03.taslak-1"
)

// sahip: kodu üreten panel kimliği. Bağlantı bu kimliğin kapsamını alır.
type sahip struct {
	Rol         p.Rol
	PanelUserID int64 // kök/bayi users.id
	ResellerID  int64 // bayi: kendi id; müşteri: domainin bayisi
	DomainID    int64 // müşteri
	FTPHesapID  int64 // müşteri
}

func (s sahip) anahtar() string {
	switch s.Rol {
	case p.RolMusteri:
		return "m:" + strconv.FormatInt(s.DomainID, 10) + ":" + strconv.FormatInt(s.FTPHesapID, 10)
	default:
		return string(s.Rol) + ":" + strconv.FormatInt(s.PanelUserID, 10)
	}
}

type kodKaydi struct {
	sahip     sahip
	duzey     p.Duzey
	sureGun   int
	olusturan string
	bitis     time.Time
}

type kodDeposu struct {
	mu    sync.Mutex
	kayit map[string]kodKaydi // özet → kayıt
	saat  func() time.Time
}

func yeniKodDeposu(saat func() time.Time) *kodDeposu {
	return &kodDeposu{kayit: map[string]kodKaydi{}, saat: saat}
}

func (d *kodDeposu) temizle(simdi time.Time) {
	for o, k := range d.kayit {
		if !simdi.Before(k.bitis) {
			delete(d.kayit, o)
		}
	}
}

// sakla: sınırlar aşılmıyorsa ekler; false → çok fazla bekleyen kod.
func (d *kodDeposu) sakla(ozet string, k kodKaydi) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.temizle(d.saat())
	if len(d.kayit) >= kodSiniri {
		return false
	}
	n := 0
	for _, v := range d.kayit {
		if v.sahip.anahtar() == k.sahip.anahtar() {
			n++
		}
	}
	if n >= sahipKodSiniri {
		return false
	}
	d.kayit[ozet] = k
	return true
}

// tuket: TEK KULLANIMLIK (bulunsun bulunmasın siler); süresi dolmuşsa false.
func (d *kodDeposu) tuket(ozet string) (kodKaydi, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k, ok := d.kayit[ozet]
	delete(d.kayit, ozet)
	if !ok || !d.saat().Before(k.bitis) {
		return kodKaydi{}, false
	}
	return k, true
}

// gecerliler: merkeze yeniden bildirilecek (bağlantı yeniden kurulunca) özetler.
func (d *kodDeposu) gecerliler() map[string]time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.temizle(d.saat())
	out := make(map[string]time.Time, len(d.kayit))
	for o, k := range d.kayit {
		out[o] = k.bitis
	}
	return out
}
