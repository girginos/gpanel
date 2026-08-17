package panelhost

// LE cert alma için basit in-memory rate limit.
//
// 🔴 Neden: LE'nin haftalık 5 fail limiti var; kullanıcı DNS henüz propagate
// olmadıysa 5 kez üst üste "Kur" bassa hostname 168 saatlik ban yer. Backend
// kendini koru:
//   - Son 3 fail'i 6 saatlik pencere içinde takip et
//   - 3'e ulaşınca 4. deneme reddet
//   - 6 saat sonra sayaç sıfırlanır
//
// Ban penceresi LE spec'ine göre haftalık ama biz 6 saat kullanıyoruz — sık
// tekrar denemeyi caydırır ama gerçekten yenilenmiş DNS için makul bir bekleme.

import (
	"sync"
	"time"
)

const (
	rateLimitFailSayisi = 3
	rateLimitPencere    = 6 * time.Hour
)

type rateFail struct {
	Zaman time.Time
}

var (
	rateMu   sync.Mutex
	rateFails = map[string][]rateFail{} // hostname → fail listesi
)

// RateLimitIzinli — bu hostname için LE denemesi yapılabilir mi?
func RateLimitIzinli(hostname string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	failler := rateFails[hostname]
	// Eski fail'leri at
	esik := time.Now().Add(-rateLimitPencere)
	yeni := failler[:0]
	for _, f := range failler {
		if f.Zaman.After(esik) {
			yeni = append(yeni, f)
		}
	}
	rateFails[hostname] = yeni
	return len(yeni) < rateLimitFailSayisi
}

// RateLimitFail — bir SSL kurma denemesi başarısız oldu, sayacı artır.
func RateLimitFail(hostname string) {
	rateMu.Lock()
	defer rateMu.Unlock()
	rateFails[hostname] = append(rateFails[hostname], rateFail{Zaman: time.Now()})
}

// RateLimitBasari — başarı olunca sayacı sıfırla.
func RateLimitBasari(hostname string) {
	rateMu.Lock()
	delete(rateFails, hostname)
	rateMu.Unlock()
}

// RateLimitBilgi — kullanıcıya kaç deneme kaldığı bilgisi.
func RateLimitBilgi(hostname string) (kalan int, bekleyen time.Duration) {
	rateMu.Lock()
	defer rateMu.Unlock()
	failler := rateFails[hostname]
	esik := time.Now().Add(-rateLimitPencere)
	sayi := 0
	var enEskiAktif time.Time
	for _, f := range failler {
		if f.Zaman.After(esik) {
			sayi++
			if enEskiAktif.IsZero() || f.Zaman.Before(enEskiAktif) {
				enEskiAktif = f.Zaman
			}
		}
	}
	kalan = rateLimitFailSayisi - sayi
	if kalan < 0 { kalan = 0 }
	if kalan == 0 && !enEskiAktif.IsZero() {
		bekleyen = time.Until(enEskiAktif.Add(rateLimitPencere))
	}
	return
}
