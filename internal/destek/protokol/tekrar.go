package protokol

import (
	"sync"
	"time"
)

// TekrarOnbellegi: imzalı mesajların tek kullanımlığını sağlar.
// Isaretle anahtarı ilk kez görüyorsa bitis anına kadar saklar ve nil döner;
// görülmüşse HataTekrar; yer kalmadıysa HataMesgul (KAPALI BAŞARISIZ — mesaj yürütülmez).
// Paylaşımlı (çok süreçli) uygulama aynı sözleşmeye uymalıdır.
type TekrarOnbellegi interface {
	Isaretle(anahtar string, bitis, simdi time.Time) error
}

// BellekOnbellek: süreç içi TekrarOnbellegi.
type BellekOnbellek struct {
	mu       sync.Mutex
	kayit    map[string]time.Time
	kapasite int
}

// YeniBellekOnbellek: kapasite ≤ 0 → 100 000.
func YeniBellekOnbellek(kapasite int) *BellekOnbellek {
	if kapasite <= 0 {
		kapasite = 100_000
	}
	return &BellekOnbellek{kayit: map[string]time.Time{}, kapasite: kapasite}
}

func (b *BellekOnbellek) Isaretle(anahtar string, bitis, simdi time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, var_ := b.kayit[anahtar]; var_ {
		return hataf(HataTekrar, "nonce yeniden kullanıldı")
	}
	if len(b.kayit) >= b.kapasite {
		for k, t := range b.kayit {
			if !t.After(simdi) {
				delete(b.kayit, k)
			}
		}
		if len(b.kayit) >= b.kapasite {
			return hataf(HataMesgul, "tekrar önbelleği dolu")
		}
	}
	b.kayit[anahtar] = bitis
	return nil
}

// Uzunluk: saklanan kayıt sayısı (gözlem/test).
func (b *BellekOnbellek) Uzunluk() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.kayit)
}
