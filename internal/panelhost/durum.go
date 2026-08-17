// Package panelhost — panel'in kendi hostname'i ve SSL sertifikası yönetimi.
//
// İki dış araca dayanır:
//   1. /usr/local/bin/girginospanel-panelhost — nginx server_name / catchall
//      yönetimi (kilitlenme koruması, DNS check, auto-rollback bash içinde)
//   2. /root/.acme.sh — Let's Encrypt cert alma (webroot modu)
//
// Panel için :80 mini vhost'u burada üretilir; yalnızca ACME challenge yolunu
// ve HTTPS'e 301 yönlendirmesini içerir — diğer :80 servislere dokunmaz.

package panelhost

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// Panel'in nginx vhost yolları — girginospanel-panelhost betiği ile aynı.
	PanelVhostPath = "/etc/nginx/conf.d/_panel.conf"
	// Panel için özel :80 vhost — ACME challenge + HTTPS 301
	AcmeVhostPath = "/etc/nginx/conf.d/_panel_acme.conf"
	// Webroot dizini (acme.sh --webroot)
	WebrootDir = "/var/www/gpanel-acme-challenge"
	// Panel SSL dosyaları — install-cert bunları değiştirecek
	CertPath = "/etc/ssl/girginospanel/panel.crt"
	KeyPath  = "/etc/ssl/girginospanel/panel.key"
	// acme.sh binary ve panel-özel config-home (müşteri acme'sinden AYRI)
	AcmeBin        = "/root/.acme.sh/acme.sh"
	AcmeConfigHome = "/opt/girginospanel/acme"
	// Betik yolu
	PanelHostBetik = "/usr/local/bin/girginospanel-panelhost"
)

// Durum — panel hostname/SSL'inin şu anki hali.
type Durum struct {
	// nginx _panel.conf'tan okunan server_name listesi (ilk özel isim,
	// localhost/IP hariç).
	Hostname    string   `json:"hostname"`
	Izinliler   []string `json:"izinliler"` // tüm server_name
	SunucuIP4   []string `json:"sunucu_ip4"`
	SunucuIP6   []string `json:"sunucu_ip6"`
	// SSL bilgileri
	SslKonu        string `json:"ssl_konu"`
	SslBitis       string `json:"ssl_bitis"`  // RFC3339
	SslKalanGun    int    `json:"ssl_kalan_gun"`
	SslLeSertifika bool   `json:"ssl_le"`     // Let's Encrypt CA'sı mı?
	SslHata        string `json:"ssl_hata"`    // cert okunamıyor/parse fail — UI göstersin
	// nginx'te bilinmeyen isimlerin yakalanma durumu
	CatchallKurulu bool `json:"catchall_kurulu"`
}

// DurumOku — yerel dosyalardan durumu topla. Hiçbir yazma yapmaz.
func DurumOku() Durum {
	d := Durum{Izinliler: []string{}, SunucuIP4: []string{}, SunucuIP6: []string{}}
	// nginx vhost'tan server_name — girginospanel-panelhost sıfırlarda
	// "148.251.169.181 localhost 127.0.0.1" gibi bir liste yazar.
	if raw, err := os.ReadFile(PanelVhostPath); err == nil {
		s := string(raw)
		for _, ln := range strings.Split(s, "\n") {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "server_name ") {
				t = strings.TrimSuffix(strings.TrimPrefix(t, "server_name "), ";")
				for _, ad := range strings.Fields(t) {
					d.Izinliler = append(d.Izinliler, ad)
				}
				break
			}
		}
		// İlk hostname (localhost/IP olmayan)
		for _, ad := range d.Izinliler {
			if ad == "localhost" || ad == "127.0.0.1" {
				continue
			}
			if net.ParseIP(ad) != nil {
				continue
			}
			d.Hostname = ad
			break
		}
	}
	// Sunucu IP'leri — YALNIZ dış erişilebilir (public). 🔴 Docker/libvirt/br-*
	// arayüzlerinin private IP'leri (172.17.x, 192.168.x, 10.x) DAHIL EDİLİRSE
	// hostname yanlışlıkla o IP'ye çözülünce DNSCoz true döner, LE HTTP-01
	// tarayıcı erişemez, LE haftada 5 fail = 168 saat ban. Bunu net şekilde
	// kes: private, ULA, docker bridge subnet'leri hariç.
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			// Sanal bridge arayüzlerini de isim bazlı at (docker0, br-*, virbr*, veth*)
			ad := iface.Name
			if ad == "docker0" ||
				strings.HasPrefix(ad, "br-") ||
				strings.HasPrefix(ad, "virbr") ||
				strings.HasPrefix(ad, "veth") ||
				strings.HasPrefix(ad, "tun") ||
				strings.HasPrefix(ad, "tap") {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				ip, _, err := net.ParseCIDR(a.String())
				if err != nil || ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					continue
				}
				if ip.IsPrivate() || ip.IsUnspecified() {
					continue // 10./172.16./192.168./ULA vs. atla
				}
				if ip.To4() != nil {
					d.SunucuIP4 = append(d.SunucuIP4, ip.String())
				} else {
					d.SunucuIP6 = append(d.SunucuIP6, ip.String())
				}
			}
		}
	}
	// SSL sertifika bilgileri
	if raw, err := os.ReadFile(CertPath); err != nil {
		d.SslHata = "cert okunamadı: " + err.Error()
	} else if blok, _ := pem.Decode(raw); blok == nil {
		d.SslHata = "cert PEM parse edilemedi (dosya bozuk?)"
	} else if crt, e := x509.ParseCertificate(blok.Bytes); e != nil {
		d.SslHata = "cert x509 parse: " + e.Error()
	} else {
		d.SslKonu = crt.Subject.CommonName
		d.SslBitis = crt.NotAfter.UTC().Format(time.RFC3339)
		d.SslKalanGun = int(time.Until(crt.NotAfter).Hours() / 24)
		for _, org := range crt.Issuer.Organization {
			if strings.Contains(strings.ToLower(org), "let's encrypt") {
				d.SslLeSertifika = true
				break
			}
		}
	}
	// Catchall bloğu var mı
	if _, err := os.Stat("/etc/nginx/conf.d/_panel_catchall.conf"); err == nil {
		d.CatchallKurulu = true
	}
	return d
}

// BetikVarMi — girginospanel-panelhost betiği yüklü mü?
func BetikVarMi() bool {
	if fi, err := os.Stat(PanelHostBetik); err == nil && !fi.IsDir() {
		return true
	}
	return false
}

// AcmeVarMi — acme.sh yüklü mü?
func AcmeVarMi() bool {
	if _, err := exec.LookPath(AcmeBin); err == nil {
		return true
	}
	if fi, err := os.Stat(AcmeBin); err == nil && !fi.IsDir() {
		return true
	}
	return false
}

// DNSCoz — hostname bu sunucunun IP'lerinden birine mi çözülüyor?
// (girginospanel-panelhost betiğinin `ayarla` içindeki mantığın Go tarafı,
// dry-run için ayrıca yararlı.)
func DNSCoz(hostname string, sunucuIP4 []string, sunucuIP6 []string) (cozulen []string, eslesme bool) {
	ipler, err := net.LookupHost(hostname)
	if err != nil {
		return nil, false
	}
	sunucuSet := make(map[string]struct{}, len(sunucuIP4)+len(sunucuIP6))
	for _, ip := range sunucuIP4 { sunucuSet[ip] = struct{}{} }
	for _, ip := range sunucuIP6 { sunucuSet[ip] = struct{}{} }
	for _, ip := range ipler {
		cozulen = append(cozulen, ip)
		if _, ok := sunucuSet[ip]; ok {
			eslesme = true
		}
	}
	return
}
