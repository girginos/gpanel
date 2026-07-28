package provisioner

import (
	"log"
	"net"
)

// ── CDN/proxy arkasindaki domainler ─────────────────────────────────────────
// 🔴 NEDEN ONEMLI: Domain Cloudflare gibi bir proxy arkasindaysa ziyaretci
// origin'e degil CDN'e baglanir; CDN de origin'e HTTPS ile gider. Origin'de
// gecerli sertifika YOKSA CDN "525 SSL handshake failed" (Full-strict'te 526)
// dondurur → site TAMAMEN kapalidir; kullanici sertifika uyarisi bile goremez.
// Bu yuzden proxy arkasindaki domainlerde sertifika, HSTS TLD'lerde oldugu gibi
// KURULUM ANINDA otomatik denenir.
//
// HTTP-01 dogrulamasi proxy arkasinda CALISIR: CDN :80'i origin'e iletir,
// .well-known/acme-challenge origin'den servis edilir (181'de dogrulandi).

// Bilinen CDN/proxy IPv4 blogu (Cloudflare + yaygin birkac saglayici).
// Liste kaba amac icin yeterli: "bu domain bize dogrudan bakmiyor" sinyali.
var cdnBloklari = []string{
	// Cloudflare
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	// Fastly
	"151.101.0.0/16", "199.232.0.0/16",
	// Akamai (ornek genis bloklar)
	"23.32.0.0/11", "23.64.0.0/14", "104.64.0.0/10",
}

var cdnAglari []*net.IPNet

func init() {
	for _, c := range cdnBloklari {
		if _, n, err := net.ParseCIDR(c); err == nil {
			cdnAglari = append(cdnAglari, n)
		}
	}
}

// ProxyArkasindaMi: domainin A kayitlari bilinen bir CDN/proxy blogunda mi?
// Cozulemezse false (bilinmiyor → normal akis).
func ProxyArkasindaMi(alanAdi string) bool {
	adresler, err := net.LookupHost(alanAdi)
	if err != nil || len(adresler) == 0 {
		return false
	}
	for _, a := range adresler {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		for _, n := range cdnAglari {
			if n.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// SertifikaSartMi: sertifikasiz kalirsa sitenin TAMAMEN erisilemez olacagi
// durumlar — (a) tarayici-dayatmali TLD (.app/.dev...), (b) CDN/proxy arkasi.
// Ikisinde de kurulum aninda otomatik Let's Encrypt denenir.
func SertifikaSartMi(alanAdi string) (bool, string) {
	if HTTPSZorunluMu(alanAdi) {
		return true, "HTTPS-zorunlu uzanti"
	}
	if ProxyArkasindaMi(alanAdi) {
		return true, "CDN/proxy arkasinda (sertifikasiz origin'de CDN 525/526 verir)"
	}
	return false, ""
}

// OtoSSLDene: SertifikaSartMi ise Let's Encrypt'i dener. Gercek CA sertifikasi
// kurulduysa (cert, key, true) doner; aksi halde sessizce (—, —, false).
func OtoSSLDene(alanAdi, sk, phpSurum, backend string) (string, string, bool) {
	gerek, neden := SertifikaSartMi(alanAdi)
	if !gerek {
		return "", "", false
	}
	log.Printf("oto-ssl: %s — %s → Let's Encrypt deneniyor", alanAdi, neden)
	crt, key, err := EnableLetsEncrypt(alanAdi, sk, phpSurum, backend)
	if err != nil {
		log.Printf("oto-ssl: %s sertifika alinamadi: %v", alanAdi, err)
		return "", "", false
	}
	if _, _, gercekCA := enIyiCertBul(alanAdi, 1); !gercekCA {
		log.Printf("oto-ssl: %s icin GERCEK sertifika alinamadi (self-signed ile devam) — %s oldugu icin site acilmayabilir; DNS/A kaydini ve :80 erisimini kontrol edin", alanAdi, neden)
		return "", "", false
	}
	log.Printf("oto-ssl: %s Let's Encrypt sertifikasi kuruldu", alanAdi)
	return crt, key, true
}
