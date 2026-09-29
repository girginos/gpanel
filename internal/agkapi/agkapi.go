// Package agkapi — panelin kullanıcı-kontrollü adreslere açtığı DIŞ bağlantılar
// için ortak SSRF kapısı ("çöz → denetle → çözülen IP'ye bağlan").
//
// 🔴 NEDEN: panel, adresi kullanıcının (müşteri/bayi/kök) yazdığı hedeflere
// bağlanıyor: domain sağlık probe'u (monitor), yedek hedefleri (S3/WebDAV/SMB/
// OAuth sağlayıcıları). Adres yalnız sözdizimsel doğrulanırsa (FQDN-regex) bir
// kullanıcı 127.0.0.1.nip.io / metadata.google.internal / A-kaydı iç IP'ye çözülen
// bir isim yazıp paneli iç servislere ya da bulut metadata ucuna (169.254.169.254
// → örnek kimlik bilgileri) istek atmaya zorlayabilir. Kapı dial ANINDA çözer,
// çözülen TÜM IP'leri denetler ve yalnız doğrulanan IP-literal'e bağlanır →
// çözümleme-anı DNS-rebinding'i de kapanır.
//
// Mantık ilk olarak internal/monitor/ssrf_guard.go'da yazıldı; yedek hedefleri de
// aynı kapıyı kullansın diye buraya taşındı. Monitor katı modda, yalnız
// KendiAdresIzni açık olarak kullanır ve davranışı değişmez.
//
// "İç ağa izin" (Secenek.IcAgIzni): kökün LAN'daki bir SMB/MinIO sunucusu
// meşru bir yedek hedefi olabilir. Bu seçenek YALNIZ özel (RFC 1918 / ULA) ve
// CGNAT aralıklarını açar; loopback, bağlantı-yerel ve bulut metadata adresleri
// (169.254.169.254, fd00:ec2::254, fd20:ce::254, 168.63.129.16 ...) HER
// DURUMDA reddedilir — kararda adresin herkese açık/dahili sınıflamasından
// ÖNCE ve hiçbir seçenekle açılmadan.
//
// Sunucunun KENDİ adresleri (tüm arayüz IP'leri, herkese açık olanlar dahil)
// loopback gibi reddedilir: yoksa bir kullanıcı sunucunun herkese açık IP'sini
// (ya da ona çözülen bir adı) yazarak panelin 0.0.0.0'a bağlı servislerine
// kendi içinden ulaşırdı. Yalnız Secenek.KendiAdresIzni (monitor) bunu açar.
package agkapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// ErrReddedildi — kapının reddettiği her bağlantı bununla eşleşir
// (errors.Is(err, agkapi.ErrReddedildi)).
var ErrReddedildi = errors.New("ssrf-guard: hedef reddedildi")

// redHatasi — ayrıntılı mesaj taşır ama ErrReddedildi ile eşleşir.
type redHatasi struct{ mesaj string }

func (e *redHatasi) Error() string       { return e.mesaj }
func (e *redHatasi) Is(hedef error) bool { return hedef == ErrReddedildi }

func reddet(format string, a ...any) error {
	return &redHatasi{mesaj: fmt.Sprintf(format, a...)}
}

func cidrler(araliklar ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range araliklar {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("agkapi: gecersiz CIDR " + c)
		}
		out = append(out, n)
	}
	return out
}

// ozelCIDRler — net.IP predikatlarının (IsPrivate/IsLoopback/IsLinkLocal*/
// IsMulticast/IsUnspecified) KAPSAMADIĞI ek dahili/rezerve aralıklar.
// Metadata (169.254.169.254) zaten IsLinkLocalUnicast'e takılır.
var ozelCIDRler = cidrler(
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
)

// metadataCIDRler — iç ağa izin verilse de reddedilen bulut metadata/platform
// DNS uçları. IPv4/IPv6 bağlantı-yerel aralıkların tamamı (IsLinkLocalUnicast)
// ayrıca reddedilir; buradakiler o aralıkların DIŞINDA kalan, sağlayıcının
// kendi belgesinde yayımlanmış uçlardır (resmî belgesi bulunamayan aday
// EKLENMEDİ). IPv6 uçları ULA'dır (fc00::/7): katı modda zaten dahili sayılır,
// ama IcAgIzni/IzinliAglar ULA'yı açtığı için burada ayrıca adıyla kapatılır.
// 168.63.129.16 herkese açık aralıktadır: izinli() metadata denetimini adres
// sınıflamasından ÖNCE yapmasının somut nedeni budur.
var metadataCIDRler = cidrler(
	// AWS IMDS (IPv6, Nitro) — docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html
	"fd00:ec2::254/128",
	// AWS Route 53 Resolver (VPC DNS, IPv6, Nitro) — docs.aws.amazon.com/whitepapers/latest/ipv6-on-aws/designing-dns-for-ipv6.html
	"fd00:ec2::253/128",
	// GCP metadata sunucusu (yalnız-IPv6 VM) — docs.cloud.google.com/compute/docs/metadata/overview
	"fd20:ce::254/128",
	// Azure platform uç noktası (WireServer, Azure DNS, DHCP, sağlık yoklaması;
	// herkese açık aralıkta sanal IP) — learn.microsoft.com/azure/virtual-network/what-is-ip-address-168-63-129-16
	"168.63.129.16/32",
	"100.100.100.200/32", // Alibaba Cloud metadata
	"192.0.0.192/32",     // Oracle Cloud (eski) metadata
)

// icAgCIDRler — IcAgIzni ile açılan aralıklar (IsPrivate'e EK olarak).
var icAgCIDRler = cidrler(
	"100.64.0.0/10", // CGNAT (ör. Tailscale/taşıyıcı içi LAN)
)

// normalize — IPv4-eşlemli IPv6 (::ffff:10.0.0.1) saf v4'e indirilir;
// predikatlar ve v4 CIDR'ler ancak böyle yakalar.
func normalize(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func icinde(ip net.IP, aglar []*net.IPNet) bool {
	for _, n := range aglar {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IPDahiliMi — IP loopback/özel/link-local(=metadata)/multicast/rezerve mi?
// nil (çözülemeyen) IP güvenli tarafta dahili sayılır.
func IPDahiliMi(ip net.IP) bool {
	if ip == nil {
		return true
	}
	ip = normalize(ip)
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	return icinde(ip, ozelCIDRler)
}

// gomuluV4 — NAT64 (64:ff9b::/96) ve 6to4 (2002::/16) adreslerindeki gömülü
// IPv4'ü döner (yoksa nil).
func gomuluV4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}
	if ip16[0] == 0x00 && ip16[1] == 0x64 && ip16[2] == 0xff && ip16[3] == 0x9b {
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15]).To4()
	}
	if ip16[0] == 0x20 && ip16[1] == 0x02 {
		return net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5]).To4()
	}
	return nil
}

// MetadataMi — adres bulut metadata ucu ya da bağlantı-yerel mi? Bu adresler
// hiçbir seçenekle (IcAgIzni, IzinliAglar) açılmaz. NAT64/6to4 içine gömülü
// metadata adresi de yakalanır.
func MetadataMi(ip net.IP) bool {
	if ip == nil {
		return true
	}
	ip = normalize(ip)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if icinde(ip, metadataCIDRler) {
		return true
	}
	if g := gomuluV4(ip); g != nil {
		return MetadataMi(g)
	}
	return false
}

// icAgMi — IcAgIzni ile açılabilen özel ağ adresi mi (loopback DEĞİL).
func icAgMi(ip net.IP) bool {
	ip = normalize(ip)
	return ip.IsPrivate() || icinde(ip, icAgCIDRler)
}

// Secenek — kapı ayarları. Sıfır değer = katı mod (yalnız herkese açık IP).
type Secenek struct {
	// IcAgIzni — özel (RFC 1918 / ULA) ve CGNAT adreslerine izin. Loopback,
	// bağlantı-yerel ve metadata adresleri yine reddedilir. Yalnız kökün AÇIK
	// onayıyla, hedef başına açılır.
	IcAgIzni bool
	// IzinliAglar — ek olarak izin verilen önekler (ör. testte 127.0.0.0/8).
	// Metadata/bağlantı-yerel, belirtilmemiş ve multicast adresler bu listeyle
	// de AÇILMAZ.
	IzinliAglar []netip.Prefix
	// BaglantiZamanasimi — TCP bağlantı zaman aşımı (0 → 10 sn).
	BaglantiZamanasimi time.Duration
	// KeepAlive — net.Dialer.KeepAlive (0 → Go varsayılanı, <0 → kapalı).
	KeepAlive time.Duration
	// Cozucu — isim çözümleyici (nil → net.DefaultResolver.LookupIPAddr).
	// Testlerde sahte DNS yanıtı vermek için.
	Cozucu func(ctx context.Context, host string) ([]net.IPAddr, error)
	// KendiAdresIzni — sunucunun kendi arayüz adreslerine bağlanmaya izin.
	// Varsayılan (false): kendi adresler loopback gibi reddedilir (katı ve iç
	// ağ modunda). YALNIZ domain sağlık probe'u (monitor) açar: panelin
	// barındırdığı alan adları olağan durumda sunucunun kendi herkese açık
	// IP'sine çözülür ve probe'un işi tam da onları yoklamaktır. Seçenek yalnız
	// kendi adres denetimini kaldırır; kendi adres loopback/özel ise dahili
	// kuralı yine uygulanır (katı modda red) — taşımadan önceki davranış.
	KendiAdresIzni bool
}

// Kapi — yapılandırılmış SSRF kapısı. Eşzamanlı kullanım güvenlidir.
type Kapi struct {
	sec    Secenek
	dialer *net.Dialer
}

// Yeni — kapı kurar.
func Yeni(sec Secenek) *Kapi {
	zaman := sec.BaglantiZamanasimi
	if zaman <= 0 {
		zaman = 10 * time.Second
	}
	return &Kapi{sec: sec, dialer: &net.Dialer{Timeout: zaman, KeepAlive: sec.KeepAlive}}
}

func (k *Kapi) izinliAgda(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, p := range k.sec.IzinliAglar {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// izinli — IP'ye bağlanılabilir mi? Denetle, Coz, DialContext ve
// YonlendirmeGuard'ın TEK karar noktası. Sıra önemlidir:
//  1. metadata/bağlantı-yerel, belirtilmemiş, multicast: HER DURUMDA red,
//     adresin herkese açık/dahili sınıflamasından ÖNCE (herkese açık aralıktaki
//     168.63.129.16 "herkese açık → izinli" kısa yoluna düşmesin); hiçbir
//     seçenek açmaz.
//  2. IzinliAglar: açık önek listesi (testte loopback) — kendi adresleri de açar.
//  3. sunucunun kendi adresleri: KendiAdresIzni yoksa red (loopback gibi).
//  4. herkese açık → izin; dahili → yalnız IcAgIzni ile özel/CGNAT.
func (k *Kapi) izinli(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = normalize(ip)
	if MetadataMi(ip) || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if k.izinliAgda(ip) {
		return true
	}
	if !k.sec.KendiAdresIzni && kendiAdresiMi(ip) {
		return false
	}
	if !IPDahiliMi(ip) {
		return true
	}
	return k.sec.IcAgIzni && icAgMi(ip)
}

// kendiAdresleriOku — sunucunun arayüz adresleri. Testler dikiş olarak
// değiştirir (kendiOnbellek.mu altında).
var kendiAdresleriOku = net.InterfaceAddrs

// kendiAdresOmru — önbellek ömrü: arayüze eklenen yeni IP en geç bu süre
// sonra kapıya yansır (her bağlantıda arayüz taraması yapılmaz).
const kendiAdresOmru = 30 * time.Second

var kendiOnbellek struct {
	mu    sync.Mutex
	kume  map[netip.Addr]struct{}
	zaman time.Time
}

// kendiAdresKumesi — önbellekli arayüz adresleri. Okuma hatasında son bilinen
// küme kullanılır; hiç okunamamışsa ok=false (çağıran güvenli tarafta reddeder).
func kendiAdresKumesi() (map[netip.Addr]struct{}, bool) {
	o := &kendiOnbellek
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.kume != nil && time.Since(o.zaman) < kendiAdresOmru {
		return o.kume, true
	}
	adresler, err := kendiAdresleriOku()
	if err != nil {
		return o.kume, o.kume != nil
	}
	kume := make(map[netip.Addr]struct{}, len(adresler))
	for _, a := range adresler {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if na, ok := netip.AddrFromSlice(ip); ok {
			kume[na.Unmap()] = struct{}{}
		}
	}
	o.kume, o.zaman = kume, time.Now()
	return kume, true
}

// kendiAdresiMi — IP sunucunun kendi arayüz adreslerinden biri mi? Arayüzler
// hiç okunamadıysa güvenli tarafta true (red).
func kendiAdresiMi(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	kume, ok := kendiAdresKumesi()
	if !ok {
		return true
	}
	_, kendi := kume[a.Unmap()]
	return kendi
}

// Denetle — IP'ye bağlanmaya izin var mı? Red → ErrReddedildi ile eşleşen hata.
func (k *Kapi) Denetle(ip net.IP) error {
	if !k.izinli(ip) {
		return reddet("ssrf-guard: dahili/özel hedef reddedildi (%s)", ip)
	}
	return nil
}

func (k *Kapi) coz(ctx context.Context, host string) ([]net.IPAddr, error) {
	if k.sec.Cozucu != nil {
		return k.sec.Cozucu(ctx, host)
	}
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// Coz — host'u çözer ve çözülen TÜM IP'leri denetler; tek bir izinsiz yanıt
// bile tümünü reddettirir (karışık A kayıtları dahiliyi gizlemesin). Alt süreç
// kullanan istemciler (lftp) isim yerine buradan dönen İLK IP'yi vermelidir.
func (k *Kapi) Coz(ctx context.Context, host string) ([]net.IP, error) {
	if lit := net.ParseIP(host); lit != nil {
		if err := k.Denetle(lit); err != nil {
			return nil, err
		}
		return []net.IP{lit}, nil
	}
	ipler, err := k.coz(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ipler) == 0 {
		return nil, fmt.Errorf("ssrf-guard: %s çözümlenemedi", host)
	}
	out := make([]net.IP, 0, len(ipler))
	for _, ia := range ipler {
		if !k.izinli(ia.IP) {
			return nil, reddet("ssrf-guard: %s dahili/özel adrese çözümlendi (%s)", host, ia.IP)
		}
		out = append(out, ia.IP)
	}
	return out, nil
}

// DialContext — http.Transport/SMB/diğer istemcilerin TEK çıkış noktası.
// "Çöz → doğrula → aynı IP'ye bağlan": host bir kez çözülür, çözülen TÜM IP'ler
// denetlenir; biri bile izinsizse bağlantı reddedilir, değilse yalnız doğrulanan
// ilk IP'ye (isme DEĞİL) bağlanılır. Bu, çözümleme-anı DNS-rebinding'ini kapatır.
// Yönlendirmeler de aynı Dial'dan geçtiği için otomatik korunur.
func (k *Kapi) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	// IP-literal ise DNS yok — doğrudan denetle.
	if lit := net.ParseIP(host); lit != nil {
		if !k.izinli(lit) {
			return nil, reddet("ssrf-guard: dahili/özel hedef reddedildi (%s)", lit)
		}
		return k.dialer.DialContext(ctx, network, net.JoinHostPort(lit.String(), port))
	}
	ipler, err := k.Coz(ctx, host)
	if err != nil {
		return nil, err
	}
	// Hepsi izinli → ilk (denetlenen) IP-literal'e bağlan (yeniden çözme YOK).
	return k.dialer.DialContext(ctx, network, net.JoinHostPort(ipler[0].String(), port))
}

// YonlendirmeGuard — http.Client.CheckRedirect: 5-yönlendirme sınırı + IP-literal
// izinsiz hedefe erken red (isim→dahili zaten DialContext'te yakalanır).
func (k *Kapi) YonlendirmeGuard(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	if lit := net.ParseIP(req.URL.Hostname()); lit != nil && !k.izinli(lit) {
		return reddet("ssrf-guard: yönlendirme dahili hedefe engellendi (%s)", lit)
	}
	return nil
}

// HTTPTransport — kapıdan geçen bir http.Transport. 🔴 Proxy BİLEREK nil:
// ortamdaki HTTPS_PROXY/HTTP_PROXY kullanılsaydı bağlantı hedefe değil vekile
// açılır, kapı yalnız vekili denetler ve hedef adres hiç denetlenmezdi.
func (k *Kapi) HTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           k.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
	}
}
