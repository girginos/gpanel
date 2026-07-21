package middleware

// Giriş (login) kaba-kuvvet koruması — IP başına kayan pencere + kilitleme.
//
// NEDEN: Panel girişi sunucunun ROOT parolasıdır ve :8443 internete açıktır.
// Hız sınırı olmadan çevrimiçi kaba-kuvvet ile doğrudan tam sunucu ele geçirilebilir.
//
// TASARIM:
//   - Yalnız BAŞARISIZ (401) denemeler sayılır.
//   - Başarıda sayaç SIFIRLANMAZ: 2FA akışında parola doğru olunca 200 + iki_fa_gerekli
//     dönüyor; sıfırlasaydık saldırgan "parola-only" isteğiyle sayacı sürekli sıfırlayıp
//     TOTP kodunu sınırsız deneyebilirdi. Sayaç pencere dolunca kendiliğinden düşer.
//   - Politika: 15 dk içinde 5 başarısız deneme → o IP 30 dakika banlanır.
//   - Kademeli gecikme: her başarısız denemeden sonra istek yavaşlatılır (üst sınırlı).
//   - Kayıtlar periyodik budanır (bellek şişmesi/DoS önlenir).
//
// NOT: IP anahtarı httpx.ClientIP'ten gelir; o fonksiyon proxy başlıklarına YALNIZCA
// yerel ters-vekilden (nginx) gelen isteklerde güvenir — aksi halde sahte
// X-Forwarded-For ile bu sınır atlanabilirdi.

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

const (
	girisPencere  = 15 * time.Minute // başarısız denemelerin sayıldığı pencere
	girisMaxHata  = 5                // pencere içinde izin verilen başarısız deneme
	girisKilit    = 30 * time.Minute // aşılınca kilit (ban) süresi
	girisMaxGecik = 2 * time.Second  // kademeli gecikme üst sınırı
)

type girisKayit struct {
	hatalar    []time.Time
	kilitBitis time.Time
}

var (
	girisMu  sync.Mutex
	girisMap = map[string]*girisKayit{}
)

func init() { go girisTemizleyici() }

// girisTemizleyici — eski kayıtları budar (sınırsız bellek büyümesini önler).
func girisTemizleyici() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		simdi := time.Now()
		esik := simdi.Add(-(girisPencere + girisKilit))
		girisMu.Lock()
		for ip, k := range girisMap {
			bosVeEski := len(k.hatalar) == 0 || k.hatalar[len(k.hatalar)-1].Before(esik)
			if k.kilitBitis.Before(simdi) && bosVeEski {
				delete(girisMap, ip)
			}
		}
		girisMu.Unlock()
	}
}

// girisDurum — pencere dışı hataları budar; (mevcut hata sayısı, kalan kilit süresi).
func girisDurum(ip string) (int, time.Duration) {
	simdi := time.Now()
	girisMu.Lock()
	defer girisMu.Unlock()
	k := girisMap[ip]
	if k == nil {
		return 0, 0
	}
	if simdi.Before(k.kilitBitis) {
		return girisMaxHata, k.kilitBitis.Sub(simdi)
	}
	kes := simdi.Add(-girisPencere)
	yeni := k.hatalar[:0]
	for _, t := range k.hatalar {
		if t.After(kes) {
			yeni = append(yeni, t)
		}
	}
	k.hatalar = yeni
	return len(k.hatalar), 0
}

func girisHataEkle(ip string) {
	simdi := time.Now()
	girisMu.Lock()
	defer girisMu.Unlock()
	k := girisMap[ip]
	if k == nil {
		k = &girisKayit{}
		girisMap[ip] = k
	}
	k.hatalar = append(k.hatalar, simdi)
	if len(k.hatalar) >= girisMaxHata {
		k.kilitBitis = simdi.Add(girisKilit)
		k.hatalar = nil
	}
}

// sureMetni — kalan süreyi insana okunur biçime çevirir (1800 sn yerine "30 dakika").
func sureMetni(sn int) string {
	if sn < 60 {
		return fmt.Sprintf("%d saniye", sn)
	}
	dk := (sn + 59) / 60
	return fmt.Sprintf("%d dakika", dk)
}

// durumYazici — handler'ın yazdığı HTTP durum kodunu yakalar.
type durumYazici struct {
	http.ResponseWriter
	kod int
}

func (d *durumYazici) WriteHeader(k int) {
	d.kod = k
	d.ResponseWriter.WriteHeader(k)
}

// GirisLimiti — kimlik doğrulama uçlarına kaba-kuvvet koruması (401 sayar).
func GirisLimiti(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r)
		adet, kalan := girisDurum(ip)
		if kalan > 0 {
			sn := int(kalan.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(sn))
			httpx.WriteError(w, http.StatusTooManyRequests,
				fmt.Sprintf("çok fazla başarısız giriş denemesi — %s sonra tekrar deneyin", sureMetni(sn)))
			return
		}
		if adet > 0 { // kademeli yavaşlatma
			g := time.Duration(adet) * 250 * time.Millisecond
			if g > girisMaxGecik {
				g = girisMaxGecik
			}
			time.Sleep(g)
		}
		dw := &durumYazici{ResponseWriter: w, kod: http.StatusOK}
		next.ServeHTTP(dw, r)
		if dw.kod == http.StatusUnauthorized {
			girisHataEkle(ip)
		}
	})
}
