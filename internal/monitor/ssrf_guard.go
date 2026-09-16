// Package monitor — SSRF kapısı: domain sağlık probe'unun dış bağlantılarını
// yalnız herkese-açık (public) IP'lerle sınırlar. Bkz. monitor.go probe().
//
// 🔴 NEDEN: /domains/{id}/health, DB'deki müşteri-kontrollü alan_adi'ni
// https://<alanAdi> olarak probe eder. alan_adi yalnız FQDN-regex ile doğrulanır
// (IP-seviyesi guard YOK). Bir reseller alan_adi'ni 127.0.0.1.nip.io /
// metadata.google.internal / A-kaydı iç IP'ye çözülen bir isme ayarlayıp paneli
// iç servisleri probe etmeye zorlayabilir (SSRF). DomainHealth yanıtı durum/boyut/
// timing/SSL-issuer döndürdüğü için güçlü bir oracle. Guard dial ANINDA çözer,
// çözülen TÜM IP'leri denetler ve yalnız doğrulanan IP-literal'e bağlanır →
// çözümleme-anı DNS-rebinding'i de kapanır.
package monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

var ssrfDialer = &net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}

// ozelCIDRler — net.IP predikatlarının (IsPrivate/IsLoopback/IsLinkLocal*/
// IsMulticast/IsUnspecified) KAPSAMADIĞI ek dahili/rezerve aralıklar.
// Metadata (169.254.169.254) zaten IsLinkLocalUnicast'e takılır.
var ozelCIDRler = func() []*net.IPNet {
	araliklar := []string{
		"100.64.0.0/10",   // CGNAT (RFC 6598) — IsPrivate kapsamaz
		"192.0.0.0/24",    // IETF protokol tahsisi
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmark (RFC 2544)
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // rezerve (255.255.255.255 dahil)
		"64:ff9b::/96",    // NAT64 — gömülü v4
		"2002::/16",       // 6to4 — gömülü v4
		"100::/64",        // discard-only (RFC 6666)
	}
	var out []*net.IPNet
	for _, c := range araliklar {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// ipDahiliMi — IP loopback/özel/link-local(=metadata)/multicast/rezerve mi?
func ipDahiliMi(ip net.IP) bool {
	if ip == nil {
		return true // çözülemeyen → güvenli tarafta reddet
	}
	// IPv4-eşlemli IPv6 (::ffff:10.0.0.1) saf v4'e indirilir; predikatlar ve
	// v4 CIDR'ler ancak böyle yakalar.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	for _, n := range ozelCIDRler {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// guvenliDialContext — probe Transport'unun TEK çıkış noktası.
// "Çöz → doğrula → aynı IP'ye bağlan": host bir kez çözülür, çözülen TÜM IP'ler
// denetlenir; biri bile dahiliyse bağlantı reddedilir, değilse yalnız doğrulanan
// IP'ye (isme DEĞİL) bağlanılır. Bu, çözümleme-anı DNS-rebinding'ini kapatır.
// Yönlendirmeler de aynı Dial'dan geçtiği için otomatik korunur.
func guvenliDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	// IP-literal ise DNS yok — doğrudan denetle (regex bunu zaten eler; yine de
	// savunma-derinliği + tasima içe-aktarımı gibi diğer yazarlar için).
	if lit := net.ParseIP(host); lit != nil {
		if ipDahiliMi(lit) {
			return nil, fmt.Errorf("ssrf-guard: dahili/özel hedef reddedildi (%s)", lit)
		}
		return ssrfDialer.DialContext(ctx, network, net.JoinHostPort(lit.String(), port))
	}
	ipler, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ipler) == 0 {
		return nil, fmt.Errorf("ssrf-guard: %s çözümlenemedi", host)
	}
	// TEK dahili yanıt bile → tümünü reddet (karışık A kayıtları dahili gizlemesin).
	for _, ia := range ipler {
		if ipDahiliMi(ia.IP) {
			return nil, fmt.Errorf("ssrf-guard: %s dahili/özel adrese çözümlendi (%s)", host, ia.IP)
		}
	}
	// Hepsi public → ilk (denetlenen) IP-literal'e bağlan (yeniden çözme YOK).
	return ssrfDialer.DialContext(ctx, network, net.JoinHostPort(ipler[0].IP.String(), port))
}

// yonlendirmeGuard — CheckRedirect: 5-yönlendirme sınırı KORUNUR; ayrıca
// IP-literal dahili hedefe erken red (isim→dahili zaten guvenliDialContext'te).
func yonlendirmeGuard(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	if lit := net.ParseIP(req.URL.Hostname()); lit != nil && ipDahiliMi(lit) {
		return fmt.Errorf("ssrf-guard: yönlendirme dahili hedefe engellendi (%s)", lit)
	}
	return nil
}
