package agkapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testAglari = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}

func TestIPDahiliMi(t *testing.T) {
	dahili := []string{
		"127.0.0.1", "127.9.9.9", "::1", // loopback
		"169.254.169.254", "169.254.1.1", // link-local / bulut metadata
		"10.0.0.5", "172.16.0.1", "172.31.255.255", "192.168.1.1", // özel
		"100.64.0.1",    // CGNAT
		"0.0.0.0", "::", // unspecified
		"224.0.0.1",          // multicast
		"::ffff:10.0.0.1",    // v4-eşlemli özel
		"fc00::1", "fe80::1", // ULA / link-local v6
		"240.0.0.1", "255.255.255.255", // rezerve/broadcast
		"198.18.0.1",                              // benchmark
		"64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1", // NAT64 / 6to4 gömülü
	}
	for _, s := range dahili {
		if ip := net.ParseIP(s); ip == nil || !IPDahiliMi(ip) {
			t.Errorf("dahili SAYILMALI ama sayilmadi: %s", s)
		}
	}
	if !IPDahiliMi(nil) {
		t.Error("nil IP guvenli tarafta dahili sayilmali")
	}
	public := []string{"8.8.8.8", "1.1.1.1", "148.251.169.181", "2606:4700:4700::1111", "93.184.216.34"}
	for _, s := range public {
		if ip := net.ParseIP(s); ip == nil || IPDahiliMi(ip) {
			t.Errorf("public SAYILMALI ama dahili sayildi: %s", s)
		}
	}
}

func TestDenetle_KatiMod(t *testing.T) {
	k := Yeni(Secenek{})
	for _, s := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if err := k.Denetle(net.ParseIP(s)); err != nil {
			t.Errorf("public %s reddedildi: %v", s, err)
		}
	}
	for _, s := range []string{"10.0.0.5", "192.168.1.1", "127.0.0.1", "169.254.169.254", "fd12::1", "100.64.0.1"} {
		err := k.Denetle(net.ParseIP(s))
		if err == nil {
			t.Errorf("katı modda %s kabul edildi", s)
			continue
		}
		if !errors.Is(err, ErrReddedildi) || !strings.Contains(err.Error(), "ssrf-guard") {
			t.Errorf("%s: ErrReddedildi/ssrf-guard bekleniyordu: %v", s, err)
		}
	}
	if err := k.Denetle(nil); err == nil {
		t.Error("nil IP kabul edildi")
	}
}

// İç ağa izin YALNIZ özel/CGNAT'ı açar; metadata, bağlantı-yerel, loopback,
// belirtilmemiş, multicast ve gömülü metadata HER DURUMDA red.
func TestDenetle_IcAgIzni(t *testing.T) {
	k := Yeni(Secenek{IcAgIzni: true})
	acik := []string{"10.0.0.5", "192.168.1.10", "172.16.0.1", "fd12:3456::1", "100.64.0.1", "::ffff:192.168.1.1", "8.8.8.8"}
	for _, s := range acik {
		if err := k.Denetle(net.ParseIP(s)); err != nil {
			t.Errorf("iç ağ izniyle %s açılmalıydı: %v", s, err)
		}
	}
	kapali := []string{
		"169.254.169.254", "169.254.170.2", "fe80::1", "fd00:ec2::254", "100.100.100.200", "192.0.0.192",
		"::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1",
		"127.0.0.1", "::1", "0.0.0.0", "::", "224.0.0.1", "ff02::1", "255.255.255.255",
	}
	for _, s := range kapali {
		if err := k.Denetle(net.ParseIP(s)); err == nil {
			t.Errorf("iç ağ izniyle bile %s REDDEDİLMELİYDİ", s)
		}
	}
}

// IzinliAglar metadata/bağlantı-yerel adresleri açamaz — tüm uzay izinli olsa bile.
func TestDenetle_IzinliAglarMetadataAcmaz(t *testing.T) {
	k := Yeni(Secenek{IzinliAglar: []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0"), netip.MustParsePrefix("169.254.0.0/16"),
	}})
	for _, s := range []string{"169.254.169.254", "fd00:ec2::254", "100.100.100.200", "fe80::1", "0.0.0.0", "224.0.0.1"} {
		if err := k.Denetle(net.ParseIP(s)); err == nil {
			t.Errorf("IzinliAglar %s adresini AÇMAMALIYDI", s)
		}
	}
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "::1"} {
		if err := k.Denetle(net.ParseIP(s)); err != nil {
			t.Errorf("IzinliAglar ile %s açılmalıydı: %v", s, err)
		}
	}
	kl := Yeni(Secenek{IzinliAglar: testAglari})
	if err := kl.Denetle(net.ParseIP("10.1.2.3")); err == nil {
		t.Error("yalnız loopback izinliyken 10.1.2.3 açıldı")
	}
}

type sayacDinleyici struct {
	net.Listener
	kabul atomic.Int64
}

func (s *sayacDinleyici) Accept() (net.Conn, error) {
	c, err := s.Listener.Accept()
	if err == nil {
		s.kabul.Add(1)
	}
	return c, err
}

func sayacliSunucu(t *testing.T) (*httptest.Server, *sayacDinleyici) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sd := &sayacDinleyici{Listener: ln}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "host="+r.Host)
	}))
	srv.Listener = sd
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, sd
}

func TestDialContext_KatiModLoopbackRed(t *testing.T) {
	srv, sd := sayacliSunucu(t)
	u, _ := url.Parse(srv.URL)
	k := Yeni(Secenek{})
	for _, addr := range []string{u.Host, "169.254.169.254:80", "10.0.0.1:443", "[::1]:80"} {
		c, err := k.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			c.Close()
			t.Errorf("katı modda %s reddedilmeliydi", addr)
			continue
		}
		if !errors.Is(err, ErrReddedildi) {
			t.Errorf("%s: ErrReddedildi bekleniyordu: %v", addr, err)
		}
	}
	if n := sd.kabul.Load(); n != 0 {
		t.Errorf("reddedilen dial sunucuya ULAŞTI (%d bağlantı)", n)
	}
}

func TestDialContext_IzinliAgIleBaglanir(t *testing.T) {
	srv, sd := sayacliSunucu(t)
	k := Yeni(Secenek{IzinliAglar: testAglari})
	cl := &http.Client{Transport: k.HTTPTransport(), CheckRedirect: k.YonlendirmeGuard, Timeout: 10 * time.Second}
	resp, err := cl.Get(srv.URL)
	if err != nil {
		t.Fatalf("izinli ağda istek başarısız: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || sd.kabul.Load() != 1 {
		t.Fatalf("durum=%d kabul=%d", resp.StatusCode, sd.kabul.Load())
	}
}

// İsim bir kez çözülür, doğrulanan IP'ye bağlanılır; Host başlığı isim olarak kalır.
func TestDialContext_IsimCozulenIPyeBaglanir(t *testing.T) {
	srv, _ := sayacliSunucu(t)
	u, _ := url.Parse(srv.URL)
	var cozum atomic.Int64
	k := Yeni(Secenek{IzinliAglar: testAglari, Cozucu: func(_ context.Context, host string) ([]net.IPAddr, error) {
		cozum.Add(1)
		if host != "depo.ornek.test" {
			t.Errorf("beklenmeyen çözüm: %s", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}})
	cl := &http.Client{Transport: k.HTTPTransport(), Timeout: 10 * time.Second}
	resp, err := cl.Get("http://depo.ornek.test:" + u.Port() + "/")
	if err != nil {
		t.Fatalf("istek: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "host=depo.ornek.test:") {
		t.Errorf("Host başlığı isim olarak kalmalıydı: %s", b)
	}
	if cozum.Load() != 1 {
		t.Errorf("isim tam bir kez çözülmeliydi, %d", cozum.Load())
	}
}

// Karışık A kayıtları: tek bir dahili yanıt bile tümünü reddettirir.
func TestDialContext_KarisikCozumRed(t *testing.T) {
	for _, kip := range []Secenek{{}, {IcAgIzni: true}} {
		kip.Cozucu = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("169.254.169.254")}}, nil
		}
		k := Yeni(kip)
		_, err := k.DialContext(context.Background(), "tcp", "karisik.ornek.test:443")
		if err == nil || !errors.Is(err, ErrReddedildi) || !strings.Contains(err.Error(), "karisik.ornek.test") {
			t.Errorf("IcAgIzni=%v: karışık çözüm reddedilmeliydi: %v", kip.IcAgIzni, err)
		}
		if _, err := k.Coz(context.Background(), "karisik.ornek.test"); err == nil {
			t.Errorf("IcAgIzni=%v: Coz karışık çözümü kabul etti", kip.IcAgIzni)
		}
	}
	bos := Yeni(Secenek{Cozucu: func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }})
	if _, err := bos.DialContext(context.Background(), "tcp", "bos.ornek.test:80"); err == nil || !strings.Contains(err.Error(), "çözümlenemedi") {
		t.Errorf("boş çözüm hatası bekleniyordu: %v", err)
	}
}

func TestDialContext_LocalhostIsimRed(t *testing.T) {
	if c, err := Yeni(Secenek{}).DialContext(context.Background(), "tcp", "localhost:80"); err == nil {
		c.Close()
		t.Error("localhost dahiliye cozulup reddedilmeliydi")
	}
	if c, err := Yeni(Secenek{IcAgIzni: true}).DialContext(context.Background(), "tcp", "localhost:80"); err == nil {
		c.Close()
		t.Error("iç ağ izni loopback'i AÇMAMALI")
	}
}

func TestCoz_IPLiteral(t *testing.T) {
	k := Yeni(Secenek{IcAgIzni: true})
	ipler, err := k.Coz(context.Background(), "192.168.5.5")
	if err != nil || len(ipler) != 1 || !ipler[0].Equal(net.ParseIP("192.168.5.5")) {
		t.Errorf("iç ağ literal: %v %v", ipler, err)
	}
	if _, err := k.Coz(context.Background(), "169.254.169.254"); err == nil {
		t.Error("metadata literal Coz'dan geçti")
	}
}

func TestYonlendirmeGuard(t *testing.T) {
	istek := func(ham string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, ham, nil)
		return r
	}
	kati := Yeni(Secenek{})
	if err := kati.YonlendirmeGuard(istek("https://example.com/"), make([]*http.Request, 5)); err != http.ErrUseLastResponse {
		t.Errorf("5 yönlendirmede ErrUseLastResponse bekleniyordu: %v", err)
	}
	if err := kati.YonlendirmeGuard(istek("http://10.0.0.1/"), nil); !errors.Is(err, ErrReddedildi) {
		t.Errorf("dahili literal yönlendirme reddedilmeliydi: %v", err)
	}
	if err := kati.YonlendirmeGuard(istek("https://example.com/"), nil); err != nil {
		t.Errorf("public yönlendirme: %v", err)
	}
	ic := Yeni(Secenek{IcAgIzni: true})
	if err := ic.YonlendirmeGuard(istek("http://10.0.0.1/"), nil); err != nil {
		t.Errorf("iç ağ izniyle özel literal yönlendirme açık olmalı: %v", err)
	}
	if err := ic.YonlendirmeGuard(istek("http://169.254.169.254/latest/meta-data/"), nil); err == nil {
		t.Error("iç ağ izniyle bile metadata yönlendirmesi reddedilmeli")
	}
}

// Ortamdaki vekil ayarı kapıyı DELMEMELİ.
func TestHTTPTransport_VekilYok(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	tr := Yeni(Secenek{}).HTTPTransport()
	if tr.Proxy != nil {
		t.Fatal("HTTPTransport Proxy nil olmalı")
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext kapıya bağlı olmalı")
	}
}
