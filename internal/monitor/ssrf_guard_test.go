package monitor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"girginospanel/internal/agkapi"
)

func TestIpDahiliMi(t *testing.T) {
	dahili := []string{
		"127.0.0.1", "127.9.9.9", "::1", // loopback
		"169.254.169.254", "169.254.1.1", // link-local / bulut metadata
		"10.0.0.5", "172.16.0.1", "172.31.255.255", "192.168.1.1", // özel
		"100.64.0.1",         // CGNAT
		"0.0.0.0",            // unspecified
		"224.0.0.1",          // multicast
		"::ffff:10.0.0.1",    // v4-eşlemli özel
		"fc00::1", "fe80::1", // ULA / link-local v6
		"240.0.0.1", "255.255.255.255", // rezerve/broadcast
		"198.18.0.1", // benchmark
	}
	for _, s := range dahili {
		if ip := net.ParseIP(s); ip == nil || !agkapi.IPDahiliMi(ip) {
			t.Errorf("dahili SAYILMALI ama sayilmadi: %s", s)
		}
	}
	public := []string{
		"8.8.8.8", "1.1.1.1", "148.251.169.181", // gercek public
		"2606:4700:4700::1111", // public v6
		"93.184.216.34",        // example.com
	}
	for _, s := range public {
		if ip := net.ParseIP(s); ip == nil || agkapi.IPDahiliMi(ip) {
			t.Errorf("public SAYILMALI ama dahili sayildi: %s", s)
		}
	}
}

func TestGuvenliDialContext_IPLiteralDahiliRed(t *testing.T) {
	// IP-literal dahili hedef DNS'siz reddedilmeli (baglanti denemesi bile yok).
	for _, addr := range []string{"127.0.0.1:80", "169.254.169.254:80", "10.0.0.1:443", "[::1]:80"} {
		c, err := guvenliDialContext(context.Background(), "tcp", addr)
		if err == nil {
			c.Close()
			t.Errorf("dahili IP-literal reddedilmeliydi: %s", addr)
			continue
		}
		if !strings.Contains(err.Error(), "ssrf-guard") {
			t.Errorf("%s: ssrf-guard hatasi bekleniyordu, alindi: %v", addr, err)
		}
	}
}

func TestGuvenliDialContext_LocalhostIsimRed(t *testing.T) {
	// localhost -> 127.0.0.1 cozulur -> reddedilmeli (rebinding/hosts sinifi).
	if c, err := guvenliDialContext(context.Background(), "tcp", "localhost:80"); err == nil {
		c.Close()
		t.Error("localhost dahiliye cozulup reddedilmeliydi")
	}
}

// Probe, sunucunun KENDİ herkese açık adreslerini yoklayabilmeli (panelin
// barındırdığı alan adları oraya çözülür): KendiAdresIzni olmadan agkapi'nin
// katı kapısı aynı adresleri REDDEDER — seçenek gerçekten iş görüyor. Makinede
// herkese açık arayüz adresi yoksa atlanır (ortamdan bağımsız sürüm:
// TestProbeKapisi_KendiAdres_Dikisli).
func TestProbeKapisi_KendiAdresIzni(t *testing.T) {
	adresler, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("arayüz adresleri: %v", err)
	}
	var acik []net.IP
	for _, a := range adresler {
		if n, ok := a.(*net.IPNet); ok && !agkapi.IPDahiliMi(n.IP) {
			acik = append(acik, n.IP)
		}
	}
	if len(acik) == 0 {
		t.Skip("bu makinede herkese açık arayüz adresi yok")
	}
	kati := agkapi.Yeni(agkapi.Secenek{})
	for _, ip := range acik {
		if err := probeKapisi.kapi("443").Denetle(ip); err != nil {
			t.Errorf("probe kendi herkese açık adresini (%s) yoklayabilmeli: %v", ip, err)
		}
		if err := kati.Denetle(ip); err == nil {
			t.Errorf("varsayılan kapı kendi adresi (%s) reddetmeliydi", ip)
		}
	}
	// Kendi adres izni loopback'i AÇMAZ.
	if err := probeKapisi.kapi("443").Denetle(net.ParseIP("127.0.0.1")); err == nil {
		t.Error("probe kapısı loopback'i açtı")
	}
}

// sahteProbe — probe kapılarını gerçek arayüzden bağımsız kurar: sunucunun
// kendi adresleri 95.216.10.20 (Linux biçimi, 16 bayt) ve 2a01:4f9:3a:1234::2;
// her isim cozum'e çözülür.
func sahteProbe(cozum string) *probeKapilari {
	return yeniProbeKapilari(func(s *agkapi.Secenek) {
		s.KendiAdresKaynagi = func() ([]net.Addr, error) {
			return []net.Addr{
				&net.IPNet{IP: net.IPv4(95, 216, 10, 20), Mask: net.CIDRMask(26, 32)},
				&net.IPNet{IP: net.ParseIP("2a01:4f9:3a:1234::2"), Mask: net.CIDRMask(64, 128)},
			}, nil
		}
		s.Cozucu = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP(cozum)}}, nil
		}
	})
}

// Üretim ayarlarıyla (yeniProbeKapilari) kurulan kapı, herkese açık IP'si
// olmayan makinede de: kendi adres 80/443'te AÇIK (KendiAdresIzni), başka
// portta KAPALI; loopback/özel/metadata her portta kapalı.
func TestProbeKapisi_KendiAdres_Dikisli(t *testing.T) {
	p := sahteProbe("95.216.10.20")
	kendi := []string{"95.216.10.20", "::ffff:95.216.10.20", "2a01:4f9:3a:1234::2"}
	for _, s := range kendi {
		for _, port := range []string{"80", "443"} {
			if err := p.kapi(port).Denetle(net.ParseIP(s)); err != nil {
				t.Errorf("probe kendi adresini (%s) %s portunda yoklayabilmeli: %v", s, port, err)
			}
		}
		for _, port := range []string{"8443", "22", "3306", "0", ""} {
			if err := p.kapi(port).Denetle(net.ParseIP(s)); !errors.Is(err, agkapi.ErrReddedildi) {
				t.Errorf("kendi adres (%s) %q portunda REDDEDİLMELİYDİ: %v", s, port, err)
			}
		}
	}
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "64:ff9b:1::a00:1"} {
		for _, port := range []string{"80", "443", "8443"} {
			if err := p.kapi(port).Denetle(net.ParseIP(s)); err == nil {
				t.Errorf("%s %s portunda açıldı", s, port)
			}
		}
	}
	// Yabancı herkese açık adres her portta açık (yalnız kendi adres port kısıtlı).
	for _, port := range []string{"443", "8443"} {
		if err := p.kapi(port).Denetle(net.ParseIP("93.184.216.34")); err != nil {
			t.Errorf("yabancı herkese açık adres %s portunda açık olmalı: %v", port, err)
		}
	}
}

// Yönlendirme ve dial: kendi IP'nin standart dışı portu (IP-literal ya da kendi
// IP'ye çözülen isim) bağlantı DENEMEDEN reddedilir — kör port taraması kapalı.
func TestProbeKapisi_KendiAdresPortu(t *testing.T) {
	p := sahteProbe("95.216.10.20")
	istek := func(u string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, u := range []string{"https://95.216.10.20/", "http://95.216.10.20/", "https://95.216.10.20:443/x", "http://[2a01:4f9:3a:1234::2]:80/", "https://site.ornek.test:8443/"} {
		if err := p.YonlendirmeGuard(istek(u), nil); err != nil {
			t.Errorf("yönlendirme %s açık olmalı: %v", u, err)
		}
	}
	for _, u := range []string{"https://95.216.10.20:8443/", "http://95.216.10.20:22/", "https://[2a01:4f9:3a:1234::2]:3306/", "https://[::ffff:95.216.10.20]:8080/"} {
		if err := p.YonlendirmeGuard(istek(u), nil); !errors.Is(err, agkapi.ErrReddedildi) {
			t.Errorf("kendi adresin standart dışı portuna yönlendirme %s REDDEDİLMELİYDİ: %v", u, err)
		}
	}
	if err := p.YonlendirmeGuard(istek("https://95.216.10.20/"), make([]*http.Request, 5)); err != http.ErrUseLastResponse {
		t.Errorf("5-yönlendirme sınırı korunmalı: %v", err)
	}
	for _, addr := range []string{"95.216.10.20:8443", "[2a01:4f9:3a:1234::2]:22", "site.ornek.test:8443", "site.ornek.test:3306"} {
		c, err := p.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			c.Close()
			t.Errorf("DialContext %s bağlandı", addr)
			continue
		}
		if !errors.Is(err, agkapi.ErrReddedildi) {
			t.Errorf("DialContext %s: ErrReddedildi bekleniyordu: %v", addr, err)
		}
	}
	if _, err := p.DialContext(context.Background(), "tcp", "portsuz"); err == nil {
		t.Error("portsuz adres hatasız geçti")
	}
}
