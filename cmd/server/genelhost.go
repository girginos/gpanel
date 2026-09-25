package main

// Sunucunun GENEL adresi (portyonetim.GenelHost): arayüz, tarayıcı SSH tünelinden
// (127.0.0.1) bağlıyken bayi/müşteri adreslerini ve "kullanıcılara yeni adresi bildirin"
// uyarısını bununla kurar.
//
// 🔴 NEDEN detectIPv4 DEĞİL: o, PANEL_PUBLIC_IPV4 yoksa ilk loopback olmayan arayüz
// IPv4'ünü döndürür. NAT arkasındaki bulut sunucusunda bu özel IP'dir (10.x/172.x) —
// dışarıdan erişilemeyen adres kullanıcılara "yeni adres" diye bildirilirdi. Güvenilir
// kaynak yoksa BOŞ döner; arayüz "genel sunucu adresi bilinmiyor" gösterir.

import (
	"net"
	"net/netip"
	"os"
	"strings"

	"girginospanel/internal/portyonetim"
)

// genelHostBul: çağrı anında türetilir (panel hostname'i çalışma anında değişebilir).
func genelHostBul() string {
	conf, _ := os.ReadFile(portyonetim.VhostYolu)
	adresler, _ := net.InterfaceAddrs()
	return genelHostSec(string(conf), os.Getenv("PANEL_PUBLIC_IPV4"), adresler)
}

// genelHostSec: öncelik sırası —
//  1. panel hostname'i (_panel.conf'un ilk server_name'indeki ilk alan adı; girginospanel-
//     panelhost DNS'in bu sunucuya çözüldüğünü doğrulayıp yazar). panelhost'un yakala-hepsi
//     bloğu bilinmeyen Host'u 444'ler: sunucu adında olmayan bir IP ile kurulan adres
//     (ör. NAT'ın dış IP'si) çalışmaz, hostname her zaman çalışır.
//  2. PANEL_PUBLIC_IPV4 — operatörün açıkça yazdığı adres (loopback/joker değilse).
//  3. arayüzlerdeki ilk GENEL IPv4 (özel, CGNAT, loopback, link-local, ayrılmış değil).
//
// Hiçbiri yoksa "".
func genelHostSec(panelConf, envIPv4 string, adresler []net.Addr) string {
	if ad := panelHostAdi(panelConf); ad != "" {
		return ad
	}
	if ip, err := netip.ParseAddr(strings.TrimSpace(envIPv4)); err == nil && ip.Is4() &&
		!ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() {
		return ip.String()
	}
	for _, a := range adresler {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(ipn.IP); ok && genelIPv4Mu(ip.Unmap()) {
			return ip.Unmap().String()
		}
	}
	return ""
}

// ozelIPv4Bloklari: dışarıdan yönlendirilmeyen, Go'nun IsPrivate'inin KAPSAMADIĞI bloklar.
var ozelIPv4Bloklari = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protokol atamaları (DS-Lite vb.)
	netip.MustParsePrefix("192.0.2.0/24"),    // belgeleme (RFC 5737)
	netip.MustParsePrefix("198.18.0.0/15"),   // kıyaslama (RFC 2544)
	netip.MustParsePrefix("198.51.100.0/24"), // belgeleme
	netip.MustParsePrefix("203.0.113.0/24"),  // belgeleme
	netip.MustParsePrefix("240.0.0.0/4"),     // ayrılmış (+ 255.255.255.255)
}

// genelIPv4Mu: internetten yönlendirilebilir bir IPv4 mü?
func genelIPv4Mu(ip netip.Addr) bool {
	if !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false // loopback, link-local, joker, çoklu yayın, RFC1918
	}
	for _, b := range ozelIPv4Bloklari {
		if b.Contains(ip) {
			return false
		}
	}
	return true
}

// panelHostAdi: _panel.conf'un İLK server_name satırındaki ilk alan adı (panelhost'un
// okuduğu satır). `_`, localhost ve IP'ler atlanır; alan adı biçiminde değilse "".
func panelHostAdi(conf string) string {
	for _, ln := range strings.Split(conf, "\n") {
		if i := strings.IndexByte(ln, '#'); i >= 0 {
			ln = ln[:i]
		}
		f := strings.Fields(strings.ReplaceAll(ln, ";", " "))
		if len(f) == 0 || f[0] != "server_name" {
			continue
		}
		for _, ad := range f[1:] {
			if alanAdiMi(ad) {
				return strings.ToLower(ad)
			}
		}
		return ""
	}
	return ""
}

// alanAdiMi: nokta içeren, harf/rakam/tire etiketlerinden oluşan ad (IP, joker `*.x`,
// düzenli ifade `~...`, `_` ve localhost DEĞİL).
func alanAdiMi(ad string) bool {
	if len(ad) > 253 || !strings.Contains(ad, ".") || net.ParseIP(ad) != nil {
		return false
	}
	if l := strings.ToLower(ad); l == "localhost" || strings.HasPrefix(l, "localhost.") {
		return false
	}
	for _, etiket := range strings.Split(ad, ".") {
		if etiket == "" || len(etiket) > 63 || etiket[0] == '-' || etiket[len(etiket)-1] == '-' {
			return false
		}
		for _, c := range etiket {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
