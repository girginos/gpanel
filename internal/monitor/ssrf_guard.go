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
//
// KendiAdresIzni: agkapi sunucunun KENDİ arayüz adreslerini varsayılan olarak
// loopback gibi reddeder (yedek hedefi gibi kullanıcı adresleri panelin kendi
// servislerine yönelemesin). Probe BİLEREK açar — eski davranış korunur:
// probe'un hedefi bu panelin barındırdığı alan adıdır ve o ad olağan durumda
// sunucunun kendi herkese açık IP'sine çözülür; red, her yerel sitenin sağlık
// raporunu "erişilemez" yapardı. Sınır: URL sabit https://<alan_adi> ya da
// http://<alan_adi> (FQDN-regex, port/yol yok), salt GET, kimlik bilgisi yok;
// loopback/özel/metadata yine reddedilir.
//
// Kendi adres YALNIZ 80/443'te açıktır: bağlantı (ilk istek ya da yönlendirme)
// başka bir porta gidiyorsa kendi adres izni OLMAYAN kapı kullanılır. Yoksa
// barındırılan bir site kendi IP'sinin başka bir portuna (ör. :8443 panel ya
// da yalnız 0.0.0.0'a bağlı iç servis) yönlendirip probe'u kör port taramasına
// çevirebilirdi. Karar dial ANINDA, çözülen IP üzerinden verilir: isimle
// verilen hedef de (kendi IP'ye çözülen ad) kapsanır. Kendi-adres kümesinin
// sınırı (yalnız arayüz adresleri; 1:1 NAT/floating IP ve local route
// önekleri kapsanmaz) için bkz. agkapi paket belgesi.
package monitor

import (
	"context"
	"net"
	"net/http"
	"time"

	"girginospanel/internal/agkapi"
)

// probeKapilari — probe'un iki kapısı: standart portlar (80/443) için kendi
// adres izinli, diğer portlar için izinsiz. Başka her ayar aynı (katı mod,
// 5 sn bağlantı zaman aşımı, keep-alive kapalı).
type probeKapilari struct {
	standart *agkapi.Kapi
	diger    *agkapi.Kapi
}

// yeniProbeKapilari — ayar nil değilse ortak Secenek'i değiştirir (testler
// Cozucu/KendiAdresKaynagi dikişi verir); KendiAdresIzni burada belirlenir.
func yeniProbeKapilari(ayar func(*agkapi.Secenek)) *probeKapilari {
	sec := agkapi.Secenek{BaglantiZamanasimi: 5 * time.Second, KeepAlive: -1}
	if ayar != nil {
		ayar(&sec)
	}
	std, dgr := sec, sec
	std.KendiAdresIzni, dgr.KendiAdresIzni = true, false
	return &probeKapilari{standart: agkapi.Yeni(std), diger: agkapi.Yeni(dgr)}
}

// kapi — porta göre kapı: yalnız 80 ve 443 kendi adrese izinli.
func (p *probeKapilari) kapi(port string) *agkapi.Kapi {
	if port == "80" || port == "443" {
		return p.standart
	}
	return p.diger
}

// DialContext — "çöz → doğrula → aynı IP'ye bağlan" (bkz. agkapi.Kapi.DialContext).
func (p *probeKapilari) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	return p.kapi(port).DialContext(ctx, network, addr)
}

// YonlendirmeGuard — 5-yönlendirme sınırı + IP-literal izinsiz hedefe erken
// red; port URL'de yoksa şemanın varsayılanı (isim→kendi adres:port zaten
// DialContext'te yakalanır).
func (p *probeKapilari) YonlendirmeGuard(req *http.Request, via []*http.Request) error {
	port := req.URL.Port()
	if port == "" {
		switch req.URL.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return p.kapi(port).YonlendirmeGuard(req, via)
}

// probeKapisi — probe Transport'unun TEK çıkış kapısı.
var probeKapisi = yeniProbeKapilari(nil)

// guvenliDialContext — probe Transport'unun DialContext'i.
func guvenliDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return probeKapisi.DialContext(ctx, network, addr)
}

// yonlendirmeGuard — CheckRedirect: 5-yönlendirme sınırı KORUNUR; ayrıca
// IP-literal dahili hedefe ve kendi adresin standart dışı portuna erken red.
func yonlendirmeGuard(req *http.Request, via []*http.Request) error {
	return probeKapisi.YonlendirmeGuard(req, via)
}
