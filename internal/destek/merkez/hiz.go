package merkez

import (
	"sync"
	"time"
)

// kayanPencere: anahtar başına "pencere içinde en çok sinir olay".
type kayanPencere struct {
	mu      sync.Mutex
	sinir   int
	pencere time.Duration
	olaylar map[string][]time.Time
}

func yeniKayanPencere(sinir int, pencere time.Duration) *kayanPencere {
	return &kayanPencere{sinir: sinir, pencere: pencere, olaylar: map[string][]time.Time{}}
}

// Izin: olay kaydedilir ve izin verilirse true. Reddedilen olay SAYILMAZ.
// ilkRed: bu pencerede ilk ret mi (kullanıcıya tek uyarı için).
func (k *kayanPencere) Izin(anahtar string, simdi time.Time) (izin bool, ilkRed bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	esik := simdi.Add(-k.pencere)
	o := k.olaylar[anahtar]
	i := 0
	for i < len(o) && !o[i].After(esik) {
		i++
	}
	o = o[i:]
	if len(o) >= k.sinir {
		k.olaylar[anahtar] = o
		return false, !k.uyarildi(anahtar, simdi)
	}
	k.olaylar[anahtar] = append(o, simdi)
	if len(k.olaylar) > 50000 { // bellek sınırı: boşalan anahtarları at
		for a, v := range k.olaylar {
			if len(v) == 0 || !v[len(v)-1].After(esik) {
				delete(k.olaylar, a)
			}
		}
	}
	return true, false
}

// uyarildi: ret uyarısı bu pencerede verildi mi; "~" önekli işaret anahtarı kullanır.
func (k *kayanPencere) uyarildi(anahtar string, simdi time.Time) bool {
	ua := "~" + anahtar
	if t := k.olaylar[ua]; len(t) > 0 && simdi.Sub(t[0]) < k.pencere {
		return true
	}
	k.olaylar[ua] = []time.Time{simdi}
	return false
}

// Sil: anahtarın olaylarını ve ret-uyarı işaretini siler (/unutbeni).
func (k *kayanPencere) Sil(anahtar string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.olaylar, anahtar)
	delete(k.olaylar, "~"+anahtar)
}
