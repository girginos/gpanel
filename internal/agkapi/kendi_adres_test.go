package agkapi

// kendi_adres_test.go — sunucunun kendi adreslerinin reddi, dikişli
// (kendiAdresleriOku) ve belirlenimli. Paketteki testler paralel koşmaz;
// dikiş kendiOnbellek.mu altında değiştirilir ve t.Cleanup ile geri alınır.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func dikisDegistir(f func() ([]net.Addr, error)) {
	kendiOnbellek.mu.Lock()
	kendiAdresleriOku = f
	kendiOnbellek.kume, kendiOnbellek.zaman = nil, time.Time{}
	kendiOnbellek.mu.Unlock()
}

// kendiAdresleriniAyarla — testin süresince arayüz adresleri f'den okunur.
func kendiAdresleriniAyarla(t *testing.T, f func() ([]net.Addr, error)) {
	t.Helper()
	kendiOnbellek.mu.Lock()
	eski := kendiAdresleriOku
	kendiOnbellek.mu.Unlock()
	dikisDegistir(f)
	t.Cleanup(func() { dikisDegistir(eski) })
}

// SabitAdresler — arayüz adresleri döndüren dikiş; sayac okuma sayısını tutar.
// "a.b.c.d/n" v4 CIDR'ler Linux'taki net.InterfaceAddrs gibi 16 BAYTLIK IP
// (net.IPv4 → ::ffff:a.b.c.d) + 4 baytlık maskeyle üretilir: kendi-adres
// kümesindeki Unmap normalizasyonu gerçek arayüze bağlı olmadan sınanır.
// Öneksiz "a.b.c.d" 4 baytlık *net.IPAddr olarak üretilir (diğer dal).
func sabitAdresler(sayac *atomic.Int64, cidrler ...string) func() ([]net.Addr, error) {
	return func() ([]net.Addr, error) {
		if sayac != nil {
			sayac.Add(1)
		}
		out := make([]net.Addr, 0, len(cidrler))
		for _, c := range cidrler {
			if !strings.Contains(c, "/") {
				out = append(out, &net.IPAddr{IP: net.IP(netip.MustParseAddr(c).AsSlice())})
				continue
			}
			p := netip.MustParsePrefix(c)
			if p.Addr().Is4() {
				b := p.Addr().As4()
				out = append(out, &net.IPNet{IP: net.IPv4(b[0], b[1], b[2], b[3]), Mask: net.CIDRMask(p.Bits(), 32)})
				continue
			}
			out = append(out, &net.IPNet{IP: net.IP(p.Addr().AsSlice()), Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen())})
		}
		return out, nil
	}
}

// Sunucunun arayüzleri: loopback, bir herkese açık v4 (Linux biçimi, 16 bayt)
// + v6, bir özel LAN adresi ve 4 baytlık *net.IPAddr olarak ikinci bir herkese açık v4.
var ornekArayuzler = []string{"127.0.0.1/8", "::1/128", "95.216.10.20/26", "2a01:4f9:3a:1234::2/64", "10.20.30.40/24", "95.216.10.30"}

// Dikiş gerçekten Linux biçimini üretiyor (yoksa Unmap testi boşa döner).
func TestSabitAdresler_LinuxBicimi(t *testing.T) {
	adresler, _ := sabitAdresler(nil, "95.216.10.20/26", "95.216.10.30")()
	if n, ok := adresler[0].(*net.IPNet); !ok || len(n.IP) != net.IPv6len || len(n.Mask) != net.IPv4len {
		t.Fatalf("v4 CIDR 16 baytlık IP + 4 baytlık maske olmalı: %#v", adresler[0])
	}
	if a, ok := adresler[1].(*net.IPAddr); !ok || len(a.IP) != net.IPv4len {
		t.Fatalf("öneksiz v4 4 baytlık *net.IPAddr olmalı: %#v", adresler[1])
	}
}

func TestKendiAdres_KatiVeIcAgdaRed(t *testing.T) {
	kendiAdresleriniAyarla(t, sabitAdresler(nil, ornekArayuzler...))
	red := func(k *Kapi, kip, s string) {
		t.Helper()
		if err := k.Denetle(net.ParseIP(s)); !errors.Is(err, ErrReddedildi) {
			t.Errorf("%s: kendi adres %s REDDEDİLMELİYDİ: %v", kip, s, err)
		}
	}
	acik := func(k *Kapi, kip, s string) {
		t.Helper()
		if err := k.Denetle(net.ParseIP(s)); err != nil {
			t.Errorf("%s: %s açık olmalıydı: %v", kip, s, err)
		}
	}
	kati := Yeni(Secenek{})
	for _, s := range []string{"95.216.10.20", "::ffff:95.216.10.20", "2a01:4f9:3a:1234::2", "127.0.0.1", "10.20.30.40", "95.216.10.30"} {
		red(kati, "katı", s)
	}
	// Yardımcı tek başına da (normalize edilmemiş 16 baytlık v4 ve IPv6 içine
	// gömülü biçimler dahil) doğru. 95.216.10.20 = 5fd8:0a14.
	gomuluKendi := []string{
		"::5fd8:a14", "::ffff:0:5fd8:a14", "64:ff9b::5fd8:a14", "64:ff9b:1::5fd8:a14",
		"64:ff9b:1:5fd8:a:1400::", "2002:5fd8:a14::1",
	}
	for _, s := range append([]string{"95.216.10.20", "::ffff:95.216.10.20", "2a01:4f9:3a:1234::2", "95.216.10.30"}, gomuluKendi...) {
		if !kendiAdresiMi(net.ParseIP(s)) {
			t.Errorf("kendiAdresiMi(%s) = false", s)
		}
	}
	for _, s := range []string{"95.216.10.21", "::5fd8:a15", "64:ff9b:1::5fd8:a15", "::"} {
		if kendiAdresiMi(net.ParseIP(s)) {
			t.Errorf("kendiAdresiMi(%s) = true", s)
		}
	}
	for _, s := range gomuluKendi {
		red(kati, "katı", s)
		red(Yeni(Secenek{IcAgIzni: true}), "iç ağ", s)
	}
	// Aynı alt ağdaki komşu kendi adres DEĞİLDİR.
	acik(kati, "katı", "95.216.10.21")
	acik(kati, "katı", "2a01:4f9:3a:1234::3")

	ic := Yeni(Secenek{IcAgIzni: true})
	for _, s := range []string{"95.216.10.20", "2a01:4f9:3a:1234::2", "10.20.30.40", "127.0.0.1"} {
		red(ic, "iç ağ", s)
	}
	acik(ic, "iç ağ", "10.20.30.41")

	// Tüm karar yolları: isimle çözüm, literal Coz, yönlendirme.
	kc := Yeni(Secenek{Cozucu: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("95.216.10.20")}}, nil
	}})
	if _, err := kc.Coz(context.Background(), "panelin-kendisi.ornek.test"); !errors.Is(err, ErrReddedildi) ||
		!strings.Contains(err.Error(), "panelin-kendisi.ornek.test") {
		t.Errorf("kendi adrese çözülen isim reddedilmeliydi: %v", err)
	}
	if _, err := kati.Coz(context.Background(), "2a01:4f9:3a:1234::2"); !errors.Is(err, ErrReddedildi) {
		t.Errorf("kendi adres literal Coz'dan geçti: %v", err)
	}
	r, _ := http.NewRequest(http.MethodGet, "http://95.216.10.20:8443/", nil)
	if err := kati.YonlendirmeGuard(r, nil); !errors.Is(err, ErrReddedildi) {
		t.Errorf("kendi adrese yönlendirme reddedilmeliydi: %v", err)
	}
	// DialContext: kendi adres literal'i bağlantı DENEMEDEN reddedilir.
	if c, err := kati.DialContext(context.Background(), "tcp", "95.216.10.20:22"); err == nil {
		c.Close()
		t.Error("DialContext kendi adrese bağlandı")
	} else if !errors.Is(err, ErrReddedildi) {
		t.Errorf("DialContext: ErrReddedildi bekleniyordu: %v", err)
	}
}

// KendiAdresIzni (monitor) yalnız kendi adres denetimini kaldırır: herkese açık
// kendi adres açılır; loopback ve özel kendi adres dahili kuralıyla yine kapalı.
// IzinliAglar açık önek olarak kendi adresi de açar (testlerin loopback'i).
func TestKendiAdres_IzinVeAcikOnek(t *testing.T) {
	kendiAdresleriniAyarla(t, sabitAdresler(nil, ornekArayuzler...))
	mon := Yeni(Secenek{KendiAdresIzni: true})
	for _, s := range []string{"95.216.10.20", "2a01:4f9:3a:1234::2"} {
		if err := mon.Denetle(net.ParseIP(s)); err != nil {
			t.Errorf("KendiAdresIzni: %s açık olmalıydı: %v", s, err)
		}
	}
	for _, s := range []string{"127.0.0.1", "::1", "10.20.30.40", "169.254.169.254", "168.63.129.16"} {
		if err := mon.Denetle(net.ParseIP(s)); err == nil {
			t.Errorf("KendiAdresIzni %s adresini AÇMAMALI", s)
		}
	}
	if err := Yeni(Secenek{KendiAdresIzni: true, IcAgIzni: true}).Denetle(net.ParseIP("10.20.30.40")); err != nil {
		t.Errorf("KendiAdresIzni+IcAgIzni özel kendi adresi açmalı: %v", err)
	}
	ko := Yeni(Secenek{IzinliAglar: []netip.Prefix{netip.MustParsePrefix("95.216.10.20/32")}})
	if err := ko.Denetle(net.ParseIP("95.216.10.20")); err != nil {
		t.Errorf("IzinliAglar açık öneki kendi adresi açmalı: %v", err)
	}
	if err := ko.Denetle(net.ParseIP("2a01:4f9:3a:1234::2")); err == nil {
		t.Error("IzinliAglar dışındaki kendi adres açıldı")
	}
}

// Arayüzler her bağlantıda taranmaz (önbellek); ömür dolunca yeniden okunur.
func TestKendiAdres_Onbellek(t *testing.T) {
	var okuma atomic.Int64
	kendiAdresleriniAyarla(t, sabitAdresler(&okuma, ornekArayuzler...))
	k := Yeni(Secenek{})
	for i := 0; i < 5; i++ {
		_ = k.Denetle(net.ParseIP("8.8.8.8"))
		_ = k.Denetle(net.ParseIP("95.216.10.20"))
	}
	if n := okuma.Load(); n != 1 {
		t.Fatalf("önbellek ömrü içinde arayüzler %d kez okundu (1 beklenirdi)", n)
	}
	kendiOnbellek.mu.Lock()
	kendiOnbellek.zaman = time.Now().Add(-kendiAdresOmru - time.Second)
	kendiOnbellek.mu.Unlock()
	_ = k.Denetle(net.ParseIP("8.8.8.8"))
	if n := okuma.Load(); n != 2 {
		t.Fatalf("ömür dolunca yeniden okunmadı (okuma=%d)", n)
	}
}

// Arayüzler hiç okunamazsa güvenli taraf: katı kapı herkese açık adresi de
// reddeder; önceden okunmuş küme varsa (bayat) o kullanılır.
func TestKendiAdres_OkumaHatasi(t *testing.T) {
	errArayuz := errors.New("netlink okunamadı")
	kendiAdresleriniAyarla(t, func() ([]net.Addr, error) { return nil, errArayuz })
	if err := Yeni(Secenek{}).Denetle(net.ParseIP("8.8.8.8")); !errors.Is(err, ErrReddedildi) {
		t.Errorf("arayüzler okunamazken katı kapı reddetmeliydi: %v", err)
	}
	if err := Yeni(Secenek{KendiAdresIzni: true}).Denetle(net.ParseIP("8.8.8.8")); err != nil {
		t.Errorf("KendiAdresIzni arayüz okumasına bağlı olmamalı: %v", err)
	}

	dikisDegistir(sabitAdresler(nil, ornekArayuzler...))
	k := Yeni(Secenek{})
	if err := k.Denetle(net.ParseIP("8.8.8.8")); err != nil {
		t.Fatalf("okuma düzelince açılmalıydı: %v", err)
	}
	kendiOnbellek.mu.Lock()
	kendiAdresleriOku = func() ([]net.Addr, error) { return nil, errArayuz }
	kendiOnbellek.zaman = time.Now().Add(-kendiAdresOmru - time.Second)
	kendiOnbellek.mu.Unlock()
	if err := k.Denetle(net.ParseIP("8.8.8.8")); err != nil {
		t.Errorf("bayat küme varken herkese açık adres açık kalmalı: %v", err)
	}
	if err := k.Denetle(net.ParseIP("95.216.10.20")); !errors.Is(err, ErrReddedildi) {
		t.Errorf("bayat kümedeki kendi adres reddedilmeli: %v", err)
	}
}

// Secenek.KendiAdresKaynagi: kapı başına dikiş (monitor testleri bununla
// gerçek arayüzden bağımsız kurar). Paylaşılan önbelleğe dokunmaz, her
// denetimde okunur; okuma hatası güvenli tarafta red.
func TestKendiAdres_KapiKaynagi(t *testing.T) {
	var okuma atomic.Int64
	kaynak := sabitAdresler(&okuma, "95.216.10.20/26", "2a01:4f9:3a:1234::2/64")
	k := Yeni(Secenek{KendiAdresKaynagi: kaynak})
	for _, s := range []string{"95.216.10.20", "::ffff:95.216.10.20", "2a01:4f9:3a:1234::2", "64:ff9b:1::5fd8:a14"} {
		if err := k.Denetle(net.ParseIP(s)); !errors.Is(err, ErrReddedildi) {
			t.Errorf("kaynaktaki kendi adres %s reddedilmeliydi: %v", s, err)
		}
	}
	if err := k.Denetle(net.ParseIP("95.216.10.21")); err != nil {
		t.Errorf("kaynakta olmayan adres açık olmalı: %v", err)
	}
	if n := okuma.Load(); n != 5 {
		t.Errorf("kaynak her denetimde okunmalı: %d okuma (5 beklenirdi)", n)
	}
	if err := Yeni(Secenek{KendiAdresKaynagi: kaynak, KendiAdresIzni: true}).Denetle(net.ParseIP("95.216.10.20")); err != nil {
		t.Errorf("KendiAdresIzni kaynaktaki adresi açmalı: %v", err)
	}
	hatali := Yeni(Secenek{KendiAdresKaynagi: func() ([]net.Addr, error) { return nil, errors.New("okunamadı") }})
	if err := hatali.Denetle(net.ParseIP("8.8.8.8")); !errors.Is(err, ErrReddedildi) {
		t.Errorf("kaynak okunamazken red bekleniyordu: %v", err)
	}
	// Kapı kaynağı paylaşılan önbelleği etkilemez.
	kendiAdresleriniAyarla(t, sabitAdresler(nil, "127.0.0.1/8"))
	if err := Yeni(Secenek{}).Denetle(net.ParseIP("95.216.10.20")); err != nil {
		t.Errorf("paylaşılan kümede olmayan adres açık olmalı: %v", err)
	}
}
