package auth

// deneme_siniri.go — OTURUMLU kimlik doğrulama uçlarında (mevcut parola / 2FA kodu
// isteyen) HESAP başına hatalı deneme sınırı.
//
// NEDEN: /auth/login IP başına GirisLimiti ile korunur, ama /me/parola (mevcut parola) ve
// /me/2fa/disable (TOTP kodu) o sınırın DIŞINDADIR. Çalınmış bir JWT ile bu uçlar sınırsız
// denenebiliyordu: /me/parola yöneticide root'un sistem parolasını, /me/2fa/disable ise
// 6 haneli TOTP kodunu (±1 pencere ≈ 3/10^6) çevrimiçi kaba kuvvetle çözmeye yarar.
//
// Politika: pencere içinde denemeMaks hatalı deneme → hesap o uç için denemeKilit boyunca
// kilitlenir (429). Başarılı deneme sayaçı SIFIRLAMAZ (TOTP'de olduğu gibi: sıfırlansaydı
// saldırgan kendi hesabından başarılı deneme serpiştirerek sınırı aşardı).

import (
	"strconv"
	"sync"
	"time"
)

const (
	denemePencere = 15 * time.Minute
	denemeMaks    = 5
	denemeKilit   = 15 * time.Minute
)

type denemeKayit struct {
	hatalar    []time.Time
	kilitBitis time.Time
}

var (
	denemeMu   sync.Mutex
	denemeMap  = map[string]*denemeKayit{}
	denemeSaat = time.Now // testler değiştirir
)

func denemeAnahtar(islem string, uid int64) string { return islem + ":" + strconv.FormatInt(uid, 10) }

// denemeIzin: hesap bu uçta bir deneme daha yapabilir mi? (izinli, kalan kilit süresi)
func denemeIzin(islem string, uid int64) (bool, time.Duration) {
	simdi := denemeSaat()
	denemeMu.Lock()
	defer denemeMu.Unlock()
	// Süresi dolmuş kayıtları buda (sınırsız bellek büyümesi olmasın).
	for k, v := range denemeMap {
		if simdi.After(v.kilitBitis) && (len(v.hatalar) == 0 || simdi.Sub(v.hatalar[len(v.hatalar)-1]) > denemePencere) {
			delete(denemeMap, k)
		}
	}
	k := denemeMap[denemeAnahtar(islem, uid)]
	if k != nil && simdi.Before(k.kilitBitis) {
		return false, k.kilitBitis.Sub(simdi)
	}
	return true, 0
}

// denemeHata: hatalı denemeyi kaydeder; eşik aşılırsa hesabı kilitler.
func denemeHata(islem string, uid int64) {
	simdi := denemeSaat()
	denemeMu.Lock()
	defer denemeMu.Unlock()
	ad := denemeAnahtar(islem, uid)
	k := denemeMap[ad]
	if k == nil {
		k = &denemeKayit{}
		denemeMap[ad] = k
	}
	var tut []time.Time
	for _, t := range k.hatalar {
		if simdi.Sub(t) < denemePencere {
			tut = append(tut, t)
		}
	}
	k.hatalar = append(tut, simdi)
	if len(k.hatalar) >= denemeMaks {
		k.kilitBitis = simdi.Add(denemeKilit)
		k.hatalar = nil
	}
}

// denemeSifirla: yalnız testler.
func denemeSifirla() {
	denemeMu.Lock()
	denemeMap = map[string]*denemeKayit{}
	denemeMu.Unlock()
}
