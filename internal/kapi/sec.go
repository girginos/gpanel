package kapi

import (
	"crypto/rand"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"
)

// Kapı portu aralığı.
//
// 🔴 ÜST SINIR NEDEN 32767: Linux'un geçici (ephemeral) port aralığı varsayılan
// olarak 32768'de başlar (ip_local_port_range). Dinlenen bir port o aralıkta
// olsaydı, giden bağlantılara atanan kaynak portlarla çakışıp ya nginx'in bind'ini
// ya da o anki bir giden bağlantıyı rastgele düşürebilirdi. Çekirdek aralığı daha
// aşağı çekilmişse üst sınır onun da altına iner.
// Alt sınır 20000: yaygın servis portlarının büyük çoğunluğu bunun altındadır.
const (
	PortAltSinir = 20000
	PortUstSinir = 32767
)

// geciciPortBaslangici: çekirdeğin geçici port aralığının başı (okunamazsa 32768).
// Testlerde değiştirilebilir.
var geciciPortBaslangici = func() int {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		return 32768
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 32768
	}
	n, err := strconv.Atoi(f[0])
	if err != nil || n < 1024 {
		return 32768
	}
	return n
}

// RastgelePortSec: kapı aralığından kriptografik rastgele bir port seçer.
// uygun(p) false dönen (yasaklı/meşgul) ve haric'teki portlar atlanır.
//
// Önce tekdüze rastgele örnekleme yapılır (dağılım eğilimsiz, tahmin edilemez).
// Aralık neredeyse doluysa rastgele başlangıçtan tek geçişlik taramaya düşülür:
// her port en fazla bir kez denenir, aralık tamamen doluysa sonsuza dek dönmez.
//
// uygun, çağıran tarafından sağlanır (yasaklı liste + `ss` anlık görüntüsü):
// bu paket portyonetim'e bağımlı olamaz (portyonetim bu paketi kullanır).
func RastgelePortSec(uygun func(int) bool, haric map[int]bool) (int, error) {
	ust := PortUstSinir
	if e := geciciPortBaslangici() - 1; e < ust {
		ust = e
	}
	if ust < PortAltSinir {
		return 0, errors.New("uygun port aralığı yok (çekirdeğin geçici port aralığı çok düşük)")
	}
	aralik := ust - PortAltSinir + 1
	adayMi := func(p int) bool { return !haric[p] && (uygun == nil || uygun(p)) }

	for i := 0; i < 64; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(aralik)))
		if err != nil {
			return 0, err
		}
		if p := PortAltSinir + int(n.Int64()); adayMi(p) {
			return p, nil
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(aralik)))
	if err != nil {
		return 0, err
	}
	bas := int(n.Int64())
	for i := 0; i < aralik; i++ {
		if p := PortAltSinir + (bas+i)%aralik; adayMi(p) {
			return p, nil
		}
	}
	return 0, errors.New("kapı aralığında uygun boş port bulunamadı")
}

// RolPortlariSec: üç rol için birbirinden ve haric'ten farklı portlar seçer.
func RolPortlariSec(uygun func(int) bool, haric map[int]bool) (admin, bayi, musteri int, err error) {
	kullanilan := map[int]bool{}
	for p := range haric {
		kullanilan[p] = true
	}
	sec := func() (int, error) {
		p, e := RastgelePortSec(uygun, kullanilan)
		if e == nil {
			kullanilan[p] = true
		}
		return p, e
	}
	if admin, err = sec(); err != nil {
		return
	}
	if bayi, err = sec(); err != nil {
		return
	}
	musteri, err = sec()
	return
}
