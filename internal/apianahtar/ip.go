package apianahtar

import (
	"errors"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// IP izin listesi: tek IP ya da CIDR. Kökte ZORUNLU (en az 1), bayide isteğe bağlı
// (boş = her IP). Kullanıcı kararı 2026-10-03.

// MaksIP: bir anahtardaki en çok girdi.
const MaksIP = 32

var (
	ErrIPBos      = errors.New("kök API anahtarında IP izin listesi zorunludur")
	ErrIPGecersiz = errors.New("geçersiz IP ya da CIDR")
	ErrIPCokGenis = errors.New("çok geniş ağ aralığı")
	ErrIPCok      = errors.New("en çok 32 IP/CIDR girilebilir")
)

// IPListesiKanonik: girdileri doğrular, kanonik biçime getirir (maskelenmiş ağ,
// tekil, sıralı). zorunlu=true (kök) iken boş liste reddedilir.
//
// 🔴 "Her IP" anlamına gelen aralıklar (0.0.0.0/0, ::/0) HİÇ kabul edilmez; kökte
// ayrıca IPv4 /8'den, IPv6 /16'dan geniş aralıklar reddedilir — zorunlu liste
// tek satırla anlamsızlaştırılamasın.
func IPListesiKanonik(ham []string, zorunlu bool) ([]string, error) {
	tekil := map[string]bool{}
	for _, h := range ham {
		for _, parca := range strings.FieldsFunc(h, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == ';' }) {
			pfx, err := ipCoz(parca)
			if err != nil {
				return nil, err
			}
			if pfx.Bits() == 0 {
				return nil, ErrIPCokGenis
			}
			if zorunlu {
				if (pfx.Addr().Is4() && pfx.Bits() < 8) || (pfx.Addr().Is6() && pfx.Bits() < 16) {
					return nil, ErrIPCokGenis
				}
			}
			tekil[pfx.String()] = true
		}
	}
	if len(tekil) > MaksIP {
		return nil, ErrIPCok
	}
	if zorunlu && len(tekil) == 0 {
		return nil, ErrIPBos
	}
	out := make([]string, 0, len(tekil))
	for k := range tekil {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func ipCoz(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		pfx, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, ErrIPGecersiz
		}
		pfx = pfx.Masked()
		if pfx.Addr().Is4In6() {
			return netip.Prefix{}, ErrIPGecersiz
		}
		return pfx, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, ErrIPGecersiz
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// IPIzinli: istemci IP'si listede mi? Liste boşsa true (bayide "her IP").
// 🔴 ip boşsa (istemci IP'si KANITLANAMADI) ve liste doluysa false — kanıtsız
// istek listeyi geçemez.
func IPIzinli(liste []string, ip string) bool {
	if len(liste) == 0 {
		return true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, l := range liste {
		pfx, err := ipCoz(l)
		if err != nil {
			continue
		}
		if pfx.Contains(a) {
			return true
		}
	}
	return false
}

// SunucuIPleri: bu sunucunun kendi arayüz adresleri (loopback ve link-local hariç).
// Dikiş: testler değiştirir. Üretimde 60 sn önbellekli.
var SunucuIPleri = sunucuIPleriOnbellekli

var (
	sunucuIPMu    sync.Mutex
	sunucuIPLer   []string
	sunucuIPZaman time.Time
)

func sunucuIPleriOnbellekli() []string {
	sunucuIPMu.Lock()
	defer sunucuIPMu.Unlock()
	if time.Since(sunucuIPZaman) < time.Minute && sunucuIPLer != nil {
		return sunucuIPLer
	}
	out := []string{}
	if adresler, err := net.InterfaceAddrs(); err == nil {
		for _, a := range adresler {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() || n.IP.IsUnspecified() {
				continue
			}
			out = append(out, n.IP.String())
		}
	}
	sunucuIPLer, sunucuIPZaman = out, time.Now()
	return out
}

// IPIzinliYerel: IPIzinli + YEREL (loopback) istek, sunucunun kendi IP'lerinden biri
// listedeyse kabul edilir.
//
// NEDEN: panelle AYNI sunucudaki istemci (ör. WHMCS) panel adını yerelde çözer
// (/etc/hosts → ::1 / 127.0.0.1) ve istek loopback'ten gelir; kullanıcı ise listeye
// doğal olarak sunucunun IP'sini yazar. Ölçüldü (.180, 2026-10-12): liste
// "203.0.113.180/32", WHMCS isteği "::1" → 403 "bu IP'den erişim izni yok".
// Yeni açıklık DEĞİL: aynı sunucudaki süreç panele sunucunun genel IP'sinden
// bağlanınca kaynak adres zaten o IP olur ve listeyi bugün de geçerdi. Kanıtsız
// istek (ip "") ve loopback olmayan yabancı IP'ler yine reddedilir.
func IPIzinliYerel(liste []string, ip string) bool {
	if IPIzinli(liste, ip) {
		return true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !a.Unmap().IsLoopback() {
		return false
	}
	for _, s := range SunucuIPleri() {
		if IPIzinli(liste, s) {
			return true
		}
	}
	return false
}
