package apianahtar

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
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
