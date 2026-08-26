package domains

// ssl_kapsam.go — SSL/TLS sayfası için KAPSAM durumu: apex, www, mail, webmail
// (mail eklentisi aktifse) alt-alanlarının her biri için ayrı ayrı "sertifika
// bu adı kapsıyor mu + DNS bu sunucuya geliyor mu + bitiş".
//
// Neden ayrı endpoint: tek bir "ssl_aktif" bayrağı, kullanıcının hangi PARÇAYA
// sertifika kurulmadığını göstermiyordu. Outlook'un mail.<d> sertifikası eksik
// olduğu için şifre sorması tam olarak bu görünürlük eksikliğiydi.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"github.com/go-chi/chi/v5"
)

type kapsamDurumu struct {
	Etiket   string `json:"etiket"`              // "Alan adı" | "www" | "Mail" | "Webmail"
	Host     string `json:"host"`                // dnshosting.me, www.dnshosting.me, ...
	Durum    string `json:"durum"`               // "kurulu" | "eksik" | "dns_yok"
	Kaynak   string `json:"kaynak,omitempty"`    // letsencrypt | self-signed
	BitisISO string `json:"bitis_iso,omitempty"` // sertifikanın gerçek NotAfter'ı
	DNSVar   bool   `json:"dns_var"`             // host bu sunucunun IP'sine çözülüyor mu
	Grup     string `json:"grup"`                // "web" | "mail"
	Aciklama string `json:"aciklama,omitempty"`  // kısa neden (eksikse)
}

type sslKapsamResp struct {
	MailEklenti bool           `json:"mail_eklenti"`
	SunucuIP    string         `json:"sunucu_ip"`
	Kapsamlar   []kapsamDurumu `json:"kapsamlar"`
}

// SSLKapsam: GET /domains/{id}/ssl/kapsam
func (h *Handlers) SSLKapsam(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, certYol string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, COALESCE(cert_path,'') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &certYol); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}

	mailAktif := h.mailEklentiAktif(r.Context())
	sunucuIP := yerelSunucuIP()

	// Web sertifikasının SAN kümesi (apex + www buradan gelir).
	webSAN, webBitis, webKaynak := sertifikaOku(certYol)

	// Mail sertifikası — panel kopyası: /var/lib/girginospanel/mail-certs/<d>/fullchain.pem
	mailSAN, mailBitis, mailKaynak := sertifikaOku("/var/lib/girginospanel/mail-certs/" + alanAdi + "/fullchain.pem")

	// Gosterilecek satirlar (sertifika/DNS durumu asagida doldurulur).
	type satir struct{ etiket, host, grup string }
	satirlar := []satir{
		{"Alan adı", alanAdi, "web"},
		{"www", "www." + alanAdi, "web"},
	}
	if mailAktif {
		// 🔴 Her mail alt-alani AYRI satir/checkbox: kullanici hangilerine SSL
		// istedigini tek tek secer (or. SMTP-only -> mail; Outlook yok -> autodiscover atla).
		for _, m := range []struct{ etiket, prefix string }{
			{"Mail", "mail"}, {"Webmail", "webmail"},
			{"SMTP", "smtp"}, {"IMAP", "imap"}, {"POP", "pop"},
			{"Autoconfig", "autoconfig"}, {"Autodiscover", "autodiscover"},
		} {
			satirlar = append(satirlar, satir{m.etiket, m.prefix + "." + alanAdi, "mail"})
		}
	}

	// 🔴 DNS aramalari PARALEL. Eskiden her satir kendi icinde sirayla
	// LookupHost cagiriyordu: 9 host x 3sn timeout = ~24sn yanit. Sunucunun
	// resolv.conf'undaki ilk resolver takildiginda her arama tam timeout'a
	// gidiyor ve uc, istemci timeout'una (30sn) dayaniyordu -> SSL sayfasinda
	// kapsam karti SESSIZCE hic gorunmuyordu (catch -> setKapsam(null)).
	// Paralelde toplam sure en yavas TEK aramaya iner (<=3sn).
	dnsSonuc := make([]bool, len(satirlar))
	var wg sync.WaitGroup
	for i, st := range satirlar {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			dnsSonuc[i] = hostBuSunucuya(host, sunucuIP)
		}(i, st.host)
	}
	wg.Wait()

	kapsamlar := make([]kapsamDurumu, 0, len(satirlar))
	for i, st := range satirlar {
		san, bitis, kaynak := webSAN, webBitis, webKaynak
		if st.grup == "mail" {
			san, bitis, kaynak = mailSAN, mailBitis, mailKaynak
		}
		kapsamlar = append(kapsamlar,
			webKapsam(st.etiket, st.host, st.grup, san, bitis, kaynak, sunucuIP, dnsSonuc[i]))
	}

	httpx.WriteJSON(w, http.StatusOK, sslKapsamResp{
		MailEklenti: mailAktif,
		SunucuIP:    sunucuIP,
		Kapsamlar:   kapsamlar,
	})
}

// webKapsam — bir host için durum kartı üretir: sertifika kapsıyor mu + DNS var mı.
func webKapsam(etiket, host, grup string, san map[string]bool, bitis, kaynak, sunucuIP string, dnsVar bool) kapsamDurumu {
	k := kapsamDurumu{Etiket: etiket, Host: host, Grup: grup}
	k.DNSVar = dnsVar

	if san[strings.ToLower(host)] {
		k.Durum = "kurulu"
		k.Kaynak = kaynak
		k.BitisISO = bitis
		return k
	}
	// Sertifika bu adı kapsamıyor.
	if !k.DNSVar {
		k.Durum = "dns_yok"
		if grup == "mail" {
			k.Aciklama = host + " A kaydı bu sunucuya (" + sunucuIP + ") gelmeli"
		} else {
			k.Aciklama = "DNS bu sunucuya gelmiyor — sertifika alınamaz"
		}
		return k
	}
	k.Durum = "eksik"
	if grup == "mail" {
		k.Aciklama = "Posta SSL kurulmadı"
	} else {
		k.Aciklama = "Sertifika bu adı kapsamıyor"
	}
	return k
}

// sertifikaOku — PEM sertifikayı okuyup küçük-harf SAN kümesi + NotAfter + kaynak
// (letsencrypt/self-signed) döner. Dosya yoksa boş küme.
func sertifikaOku(yol string) (san map[string]bool, bitisISO, kaynak string) {
	san = map[string]bool{}
	if yol == "" {
		return
	}
	b, err := os.ReadFile(yol)
	if err != nil {
		return
	}
	// fullchain olabilir — İLK (yaprak) sertifika bizimkidir.
	for len(b) > 0 {
		var blok *pem.Block
		blok, b = pem.Decode(b)
		if blok == nil {
			break
		}
		if blok.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blok.Bytes)
		if err != nil {
			continue
		}
		for _, d := range c.DNSNames {
			san[strings.ToLower(d)] = true
		}
		if len(c.Subject.CommonName) > 0 {
			san[strings.ToLower(c.Subject.CommonName)] = true
		}
		bitisISO = c.NotAfter.UTC().Format(time.RFC3339)
		// Kendi kendini imzalamış mı (issuer==subject) → self-signed.
		if c.Issuer.CommonName == c.Subject.CommonName {
			kaynak = "self-signed"
		} else {
			kaynak = "letsencrypt"
		}
		return // yalnız yaprak
	}
	return
}

// yerelSunucuIP — dışarı çıkışta kullanılan birincil IPv4.
func yerelSunucuIP() string {
	c, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer c.Close()
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
			return a.IP.String()
		}
	}
	return ""
}

// hostBuSunucuya — host bu sunucunun IP'sine çözülüyor mu (kısa timeout).
func hostBuSunucuya(host, sunucuIP string) bool {
	if sunucuIP == "" {
		return false
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip == sunucuIP {
			return true
		}
	}
	return false
}
