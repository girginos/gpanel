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
//
// Mantık internal/agkapi'ye taşındı (yedek hedefleri de aynı kapıyı kullanır).
// Probe KATI modda kalır: iç ağ izni YOK, 5 sn bağlantı zaman aşımı, keep-alive
// kapalı — taşımadan önceki ssrfDialer ile birebir aynı.
package monitor

import (
	"context"
	"net"
	"net/http"
	"time"

	"girginospanel/internal/agkapi"
)

// probeKapisi — probe Transport'unun TEK çıkış kapısı.
var probeKapisi = agkapi.Yeni(agkapi.Secenek{BaglantiZamanasimi: 5 * time.Second, KeepAlive: -1})

// ipDahiliMi — IP loopback/özel/link-local(=metadata)/multicast/rezerve mi?
func ipDahiliMi(ip net.IP) bool { return agkapi.IPDahiliMi(ip) }

// guvenliDialContext — "çöz → doğrula → aynı IP'ye bağlan" (bkz. agkapi.Kapi.DialContext).
func guvenliDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return probeKapisi.DialContext(ctx, network, addr)
}

// yonlendirmeGuard — CheckRedirect: 5-yönlendirme sınırı KORUNUR; ayrıca
// IP-literal dahili hedefe erken red (isim→dahili zaten guvenliDialContext'te).
func yonlendirmeGuard(req *http.Request, via []*http.Request) error {
	return probeKapisi.YonlendirmeGuard(req, via)
}
