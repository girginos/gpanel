package provisioner

// Mail eklentisi SSL + WEBMAIL vhost entegrasyonu.
//
// İKİ İŞ BİR ARADA, çünkü birbirinden AYRILAMAZLAR:
//  1. mail.<d> / webmail.<d> için AYRI Let's Encrypt sertifikası (web SSL'e
//     sıfır regresyon riski — apex sertifikası hiç ellenmez).
//  2. O hostları KARŞILAYAN nginx vhost'u (Roundcube kökü + ACME challenge kökü).
//
// 🔴 SIRALAMA TUZAĞI (burada çözülür): sertifika, vhost OLMADAN alınamaz —
// HTTP-01 challenge 80/tcp'den doğrulanır ve mail./webmail. hostlarını
// karşılayan bir server bloğu yoksa istek default vhost'a düşer. Demo sunucuda
// ÖLÇÜLDÜ: `curl http://mail.versilo.net/.well-known/acme-challenge/x` → 500.
// Vhost da sertifika olmadan HTTPS dinleyemez. Uygulanan sıra:
//
//	HTTP-only vhost (+challenge kökü)  →  ad başına ön doğrulama  →  sertifika
//	→  HTTPS vhost'una geçiş
//
// 🔴 SAN TUZAĞI: Let's Encrypt siparişindeki BİR ad doğrulanamazsa TÜM sipariş
// düşer (bu projede "www DNS'te yoksa tüm LE siparişi düşer" diye kayıtlı bilinen
// tuzak). Bu yüzden her ad SAN'a girmeden ÖNCE ayrı ayrı sınanır:
//   - (a) DNS bu sunucuya çözülüyor mu,
//   - (b) challenge yolu GERÇEKTEN 200 ve BEKLENEN GÖVDEYİ dönüyor mu.
//
// Sınavı geçmeyen ad SAN'dan ÇIKARILIR; kalan adlarla sertifika yine alınır ve
// çıkarılan adların SEBEBİ çağırana döner (sessiz kayıp YOK).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const mailEklentiSoket = "/run/girginospanel/eklenti-mail.sock"

const (
	// 🔴 ORTAK ACME KÖKÜ: mail./webmail. hostları bir tenant'ın public_html'ine
	// DEĞİL, panelin kendi köküne yazar. Neden: bu hostları TEK bir paylaşımlı
	// nginx vhost'u karşılar (her domain için ayrı vhost üretmiyoruz), o vhost'un
	// kökü de tenant'a göre değişemez. Ayrıca tenant home'u kotalı/ACL'li olabilir
	// ve challenge dosyası sessizce yazılamaz hâle gelirdi.
	MailAcmeWebroot = "/var/lib/girginospanel/acme-webroot"

	// Paylaşımlı webmail vhost'u — TÜM domainlerin mail./webmail. hostlarını
	// karşılar (regex server_name). Domaine özel LE sertifikası alındığında
	// AYRI bir dosyaya tam adlı server bloğu yazılır; nginx'te tam ad regex'i
	// YENER, dolayısıyla domaine özel blok otomatik olarak öne geçer.
	webmailOrtakVhost = "/etc/nginx/conf.d/_gosp_webmail.conf"

	// Roundcube kurulum kökü (EPEL roundcubemail paketi) ve PHP-FPM soketi.
	// Kurucu (internal/lisans/kurulum_mail_webmail.go) aynı yolları kullanır.
	RoundcubeKok   = "/usr/share/roundcubemail"
	roundcubeSoket = "unix:/run/php-fpm/roundcube.sock"

	// Mail yığınının kendinden imzalı sertifikası — LE gelene kadar HTTPS'in
	// AYAKTA olması için kullanılır (tarayıcı uyarır ama sayfa açılır).
	mailSelfCrt = "/etc/pki/mail/mail.crt"
	mailSelfKey = "/etc/pki/mail/mail.key"

	mailCertKok = "/var/lib/girginospanel/mail-certs"
)

// hostCozuluyorMu — host, apex (domain) ile AYNI IP kümesine çözülüyor mu?
// (wwwSANUygun'un genelleştirilmişi — HTTP-01 için host bu sunucuya gelmeli.)
//
// 🔴 GEÇİCİ ÇÖZÜMLEME HATASI SAN'I DARALTMAMALI: demo sunucuda ölçüldü —
// mail.<d> A kaydı DOĞRUYKEN tek seferlik bir resolver takılması yüzünden ad
// "çözülmüyor" sayıldı ve sertifikadan DÜŞTÜ (bir sonraki çalıştırmada
// sorunsuz çözüldü). Geçici arıza ile kalıcı yokluk aynı şey değildir:
// çözümleme HATASINDA yeniden denenir. IP kümesi UYUŞMUYORSA (yani cevap
// alınmış ama başka sunucuyu gösteriyorsa) tekrar denemenin anlamı yok.
func hostCozuluyorMu(domain, host string) bool {
	cozumle := func(ad string) []string {
		for deneme := 0; deneme < 3; deneme++ {
			ip, err := net.LookupHost(ad)
			if err == nil && len(ip) > 0 {
				return ip
			}
			if deneme < 2 {
				time.Sleep(time.Second)
			}
		}
		return nil
	}
	h := cozumle(host)
	if len(h) == 0 {
		return false
	}
	// 🔴 KALICI FIX (mail-only / split-server): mail alt-alani BU sunucunun IP'sine
	// cozulyorsa YETER. Apex (web) baska sunucuda/CDN'de (Cloudflare vb.) olabilir;
	// mail icin apex ile AYNI IP olmasi GEREKMEZ. Eski apex-eslesme, web'i CDN'de
	// olan domainlerde mail SSL'i yanlislikla "cozulmuyor" sayiyordu.
	if sip := mailYerelSunucuIP(); sip != "" {
		for _, ip := range h {
			if ip == sip {
				return true
			}
		}
	}
	// Geriye uyum: apex = bu sunucu senaryosu (host, apex ile ayni IP kumesinde).
	apex := cozumle(domain)
	if len(apex) == 0 {
		return false
	}
	kume := make(map[string]bool, len(apex))
	for _, ip := range apex {
		kume[ip] = true
	}
	for _, ip := range h {
		if !kume[ip] {
			return false
		}
	}
	return true
}

// mailYerelSunucuIP — sunucunun giden (public) IPv4'unu dondurur (ssl_kapsam.go
// yerelSunucuIP ile ayni yontem; paketler-arasi bagimlilik olmasin diye kopya).
func mailYerelSunucuIP() string {
	c, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer c.Close()
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
			return a.IP.String()
		}
	}
	return ""
}

// ── webmail vhost ──────────────────────────────────────────────────────────

// roundcubeGovde — Roundcube'u sunan ortak location bloğu (hem paylaşımlı hem
// domaine özel vhost aynı gövdeyi kullanır → iki yerde ayrışamaz).
func roundcubeGovde() string {
	return `    root ` + RoundcubeKok + `;
    index index.php;
    client_max_body_size 64m;

    # Kaynak/kurulum dizinleri DIŞARIYA KAPALI: installer/ açık kalırsa
    # yapılandırma sihirbazı internetten erişilebilir olurdu.
    location ~ ^/(config|temp|logs|SQL|bin|installer|vendor)/ { deny all; }
    location ~ ^/(README|INSTALL|LICENSE|CHANGELOG|UPGRADING|SECURITY|composer)\.?[a-z]*$ { deny all; }
    location ~ /\. { deny all; }

    location / {
        try_files $uri $uri/ /index.php$is_args$args;
    }

    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_pass ` + roundcubeSoket + `;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTPS on;
        fastcgi_read_timeout 300;
    }

    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "same-origin" always;
`
}

// acmeBlok — HTTP-01 challenge yolu. 🔴 Bu blok HER ZAMAN yazılır; sertifika
// almanın ÖN KOŞULU budur ve sertifika yenilenirken de gerekir.
func acmeBlok() string {
	return `    location ^~ /.well-known/acme-challenge/ {
        root ` + MailAcmeWebroot + `;
        default_type "text/plain";
        try_files $uri =404;
        access_log off;
    }
`
}

// 🔴 smtp|imap|pop + autoconfig|autodiscover DAHİL: bu vhost ACME HTTP-01
// challenge'ını da karşılar; server_name eşleşmezse o hostun challenge'ı 404 olur
// ve ad SAN'dan düşer. Outlook/istemci gelenek adları (smtp./imap./pop.) ve
// otomatik-yapılandırma adları da bu regex ile karşılanmalı ki sertifikaya girsin.
const webmailRegexAd = `~^(?:webmail|mail|smtp|imap|pop|autoconfig|autodiscover)\.[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)+$`

// WebmailVhostSagla — paylaşımlı mail./webmail. vhost'unu (idempotent) yazar.
//
// 80 bloğu KOŞULSUZ yazılır (challenge + HTTPS'e yönlendirme). 443 bloğu ancak
// GERÇEKTEN bir sertifika dosyası varsa yazılır — 🔴 olmayan sertifikaya işaret
// eden bir ssl_certificate satırı `nginx -t`'yi düşürür ve TÜM web sunucusunu
// yeniden yüklenemez hâle getirirdi (yalnız webmail değil, bütün siteler).
func WebmailVhostSagla() error {
	if err := os.MkdirAll(filepath.Join(MailAcmeWebroot, ".well-known", "acme-challenge"), 0o755); err != nil {
		return fmt.Errorf("acme kökü oluşturulamadı: %w", err)
	}
	_, _ = exec.Command("restorecon", "-R", MailAcmeWebroot).CombinedOutput()

	var b strings.Builder
	b.WriteString(`# GirginOSPanel — webmail (Roundcube) PAYLAŞIMLI vhost'u. OTOMATİK ÜRETİLDİ.
# TÜM domainlerin webmail.<d> / mail.<d> hostlarını tek blok karşılar.
# Domaine özel LE sertifikası alındığında /etc/nginx/conf.d/webmail_<d>.conf
# yazılır; nginx'te TAM AD regex'i yener, dolayısıyla o blok öne geçer.
server {
    listen 80;
    listen [::]:80;
    server_name ` + webmailRegexAd + `;

`)
	b.WriteString(acmeBlok())
	b.WriteString(`
    location / { return 301 https://$host$request_uri; }

    access_log /var/log/nginx/webmail.access.log;
    error_log  /var/log/nginx/webmail.error.log warn;
}
`)
	// 🔴 HTTPS bloğu yalnız sertifika DOSYASI varsa. Kendinden imzalı mail
	// sertifikası da iş görür: LE gelene kadar webmail AYAKTA kalır (tarayıcı
	// uyarısıyla), yoksa hiç açılmazdı.
	if dosyaVarPv(mailSelfCrt) && dosyaVarPv(mailSelfKey) {
		b.WriteString(`
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ` + webmailRegexAd + `;

    ssl_certificate     ` + mailSelfCrt + `;
    ssl_certificate_key ` + mailSelfKey + `;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

`)
		b.WriteString(acmeBlok())
		b.WriteString("\n")
		b.WriteString(roundcubeGovde())
		b.WriteString(`
    access_log /var/log/nginx/webmail.access.log;
    error_log  /var/log/nginx/webmail.error.log warn;
}
`)
	}
	return nginxDosyaYazDogrula(webmailOrtakVhost, b.String())
}

// mailOtoYapilandirmaLokasyon — Outlook (autodiscover) + Thunderbird (autoconfig)
// XML'ini DOĞRUDAN döndüren nginx location blokları.
//
// 🔴 Roundcube'dan ÖNCE gelmeli: yoksa autodiscover isteği webmail HTML login
// sayfasına düşer, istemci oto-kurulumu ayrıştıramaz ve Outlook "sürekli parola
// sorar" (canlı gözlendi). Ayarlar: mail.<d> IMAP993/POP995/SMTP587, kullanıcı =
// tam e-posta. XML gövdesi girginospanel-autoconfig servisiyle BİREBİR
// (Outlook + Thunderbird ile test edilmiş şema). XML'de '$' YOK → nginx değişken
// genişletmesi riski yok; tek tırnak gövde, XML'de tek tırnak da yok.
func mailOtoYapilandirmaLokasyon(alanAdi string) string {
	tb := `<?xml version="1.0" encoding="UTF-8"?>
<clientConfig version="1.1">
  <emailProvider id="` + alanAdi + `">
    <domain>` + alanAdi + `</domain>
    <displayName>` + alanAdi + ` Mail</displayName>
    <displayShortName>` + alanAdi + `</displayShortName>
    <incomingServer type="imap">
      <hostname>mail.` + alanAdi + `</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
      <authentication>password-cleartext</authentication>
      <username>%EMAILADDRESS%</username>
    </incomingServer>
    <incomingServer type="pop3">
      <hostname>mail.` + alanAdi + `</hostname>
      <port>995</port>
      <socketType>SSL</socketType>
      <authentication>password-cleartext</authentication>
      <username>%EMAILADDRESS%</username>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>mail.` + alanAdi + `</hostname>
      <port>587</port>
      <socketType>STARTTLS</socketType>
      <authentication>password-cleartext</authentication>
      <username>%EMAILADDRESS%</username>
    </outgoingServer>
  </emailProvider>
</clientConfig>`
	ol := `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">
  <Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">
    <Account>
      <AccountType>email</AccountType>
      <Action>settings</Action>
      <Protocol>
        <Type>IMAP</Type>
        <Server>mail.` + alanAdi + `</Server>
        <Port>993</Port>
        <SSL>on</SSL>
        <SPA>off</SPA>
        <AuthRequired>on</AuthRequired>
      </Protocol>
      <Protocol>
        <Type>SMTP</Type>
        <Server>mail.` + alanAdi + `</Server>
        <Port>587</Port>
        <SSL>on</SSL>
        <SPA>off</SPA>
        <AuthRequired>on</AuthRequired>
      </Protocol>
    </Account>
  </Response>
</Autodiscover>`
	return `    # Outlook/Thunderbird otomatik yapılandırma — XML DOĞRUDAN (Roundcube'dan ÖNCE).
    location = /mail/config-v1.1.xml { default_type application/xml; return 200 '` + tb + `'; }
    location = /.well-known/autoconfig/mail/config-v1.1.xml { default_type application/xml; return 200 '` + tb + `'; }
    location ~* ^/autodiscover/autodiscover\.xml$ { default_type application/xml; return 200 '` + ol + `'; }
`
}

// WebmailVhostDomainYaz — domaine özel (GERÇEK sertifikalı) webmail vhost'u.
// Paylaşımlı regex bloğunu tam adla EZER; böylece webmail.<d> tarayıcı uyarısı
// olmadan açılır. autoconfig.<d>/autodiscover.<d> de bu bloktadır → LE cert o
// adları kapsar ve XML doğrudan döner (Outlook oto-kurulumu çalışır).
func WebmailVhostDomainYaz(alanAdi, certYol, keyYol string) error {
	if err := ValidateDomain(alanAdi); err != nil {
		return err
	}
	// 🔴 Sertifika dosyaları GERÇEKTEN yerinde mi? Yoksa `nginx -t` düşer ve
	// reload edilemeyen nginx TÜM siteleri eski yapılandırmada dondururdu.
	if !dosyaVarPv(certYol) || !dosyaVarPv(keyYol) {
		return fmt.Errorf("sertifika dosyaları yok (%s / %s) — vhost yazılmadı", certYol, keyYol)
	}
	yol := "/etc/nginx/conf.d/webmail_" + strings.ReplaceAll(alanAdi, ".", "_") + ".conf"
	govde := `# GirginOSPanel — webmail.` + alanAdi + ` (GERÇEK sertifika). OTOMATİK ÜRETİLDİ.
server {
    listen 80;
    listen [::]:80;
    server_name webmail.` + alanAdi + ` mail.` + alanAdi + ` autoconfig.` + alanAdi + ` autodiscover.` + alanAdi + `;

` + acmeBlok() + `
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name webmail.` + alanAdi + ` mail.` + alanAdi + ` autoconfig.` + alanAdi + ` autodiscover.` + alanAdi + `;

    ssl_certificate     ` + certYol + `;
    ssl_certificate_key ` + keyYol + `;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

` + acmeBlok() + `
` + mailOtoYapilandirmaLokasyon(alanAdi) + roundcubeGovde() + `
    access_log /var/log/nginx/webmail.` + alanAdi + `.access.log;
    error_log  /var/log/nginx/webmail.` + alanAdi + `.error.log warn;
}
`
	return nginxDosyaYazDogrula(yol, govde)
}

// nginxDosyaYazDogrula — yaz → `nginx -t` → reload; test düşerse ESKİ İÇERİĞE
// GERİ AL. 🔴 Bozuk bir conf.d dosyası nginx'i reload edilemez hâle getirir ve
// bu, webmail'i değil SUNUCUDAKİ TÜM SİTELERİ etkiler.
func nginxDosyaYazDogrula(yol, govde string) error {
	eski, eskiVar := os.ReadFile(yol)
	if len(eski) > 0 && eskiVar == nil && string(eski) == govde {
		// İçerik aynı — nginx'i boşuna reload etme (idempotent çağrı).
		return nil
	}
	if err := os.WriteFile(yol, []byte(govde), 0o644); err != nil {
		return fmt.Errorf("%s yazılamadı: %w", yol, err)
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		if eskiVar == nil {
			_ = os.WriteFile(yol, eski, 0o644)
		} else {
			_ = os.Remove(yol)
		}
		return fmt.Errorf("`nginx -t` başarısız, %s GERİ ALINDI: %s", yol, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func dosyaVarPv(y string) bool {
	st, err := os.Stat(y)
	return err == nil && !st.IsDir()
}

// acmeYoluCanli — challenge yolunun GERÇEKTEN çalıştığını ölçer.
//
// 🔴 "vhost yazdım" KANIT DEĞİLDİR: DNS başka sunucuya bakıyor, araya CDN
// girmiş, 80 kapalı ya da başka bir vhost adı kapmış olabilir. Rastgele bir
// jeton yazıp HTTP ile GERİ OKURUZ. Bu sınavı geçmeyen ad SAN'a KONMAZ —
// konsaydı tüm LE siparişi düşer, mail.<d> yüzünden webmail.<d> de sertifikasız
// kalırdı.
func acmeYoluCanli(host string) error {
	ham := make([]byte, 16)
	if _, err := rand.Read(ham); err != nil {
		return fmt.Errorf("jeton üretilemedi: %w", err)
	}
	jeton := "gosp-onsinav-" + hex.EncodeToString(ham)
	govde := jeton + "." + hex.EncodeToString(ham)
	diz := filepath.Join(MailAcmeWebroot, ".well-known", "acme-challenge")
	if err := os.MkdirAll(diz, 0o755); err != nil {
		return fmt.Errorf("challenge dizini: %w", err)
	}
	yol := filepath.Join(diz, jeton)
	if err := os.WriteFile(yol, []byte(govde), 0o644); err != nil {
		return fmt.Errorf("challenge jetonu yazılamadı: %w", err)
	}
	defer os.Remove(yol)
	_, _ = exec.Command("restorecon", "-R", MailAcmeWebroot).CombinedOutput()

	cl := &http.Client{
		Timeout: 15 * time.Second,
		// 🔴 Yönlendirme İZLENMEZ: ACME sunucusu da izlemez sayılmaz —
		// challenge'ı 200 ile doğrudan almalıyız.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	url := "http://" + host + "/.well-known/acme-challenge/" + jeton
	resp, err := cl.Get(url)
	if err != nil {
		return fmt.Errorf("challenge yolu okunamadı: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("challenge yolu HTTP %d döndü (200 bekleniyordu)", resp.StatusCode)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if strings.TrimSpace(string(b)) != govde {
		return fmt.Errorf("challenge yolu BAŞKA içerik döndü — bu hostu başka bir vhost karşılıyor")
	}
	return nil
}

// MailSertifikaAl — mail.<d> + webmail.<d> için Let's Encrypt sertifikası alır.
//
// Dönüş: cert/key yolu, SAN'a GİREN adlar, SAN'dan ÇIKARILAN adlar (sebebiyle).
// sk parametresi geriye uyumluluk için durur; ACME kökü artık tenant home'u
// değil, paylaşımlı MailAcmeWebroot'tur (bkz. dosya başı).
func MailSertifikaAl(alanAdi, sk string) (certPath, keyPath string, kapsam []string, atlanan []string, err error) {
	if verr := ValidateDomain(alanAdi); verr != nil {
		return "", "", nil, nil, verr
	}
	// 1) ÖNCE vhost — sertifika bu olmadan ALINAMAZ.
	if e := WebmailVhostSagla(); e != nil {
		return "", "", nil, nil, fmt.Errorf("webmail/ACME vhost'u hazırlanamadı: %w", e)
	}
	// 2) Ad başına ÖN SINAV (SAN tuzağı). mail./webmail. + Outlook/istemci
	// gelenek adları smtp./imap./pop. + otomatik-yapılandırma adları
	// autoconfig./autodiscover. — çözülmeyen ad SAN'dan çıkar (atlanan).
	// 🔴 autoconfig./autodiscover. cert'e GİRMELİ: Outlook oto-kurulumda
	// https://autodiscover.<d>'ye gider; cert o adı kapsamazsa tarayıcı/istemci
	// GÜVENMEZ ve oto-kurulum başarısız olur (istemci "sürekli parola sorar").
	for _, h := range []string{
		"mail." + alanAdi, "webmail." + alanAdi,
		"smtp." + alanAdi, "imap." + alanAdi, "pop." + alanAdi,
		"autoconfig." + alanAdi, "autodiscover." + alanAdi,
	} {
		if !hostCozuluyorMu(alanAdi, h) {
			atlanan = append(atlanan, h+": DNS bu sunucuya çözülmüyor (A kaydı gerekli)")
			continue
		}
		if e := acmeYoluCanli(h); e != nil {
			atlanan = append(atlanan, h+": "+e.Error())
			continue
		}
		kapsam = append(kapsam, h)
	}
	if len(kapsam) == 0 {
		return "", "", nil, atlanan, fmt.Errorf(
			"hiçbir posta adı doğrulanamadı, sertifika İSTENMEDİ (LE oran sınırını boşa harcamamak için): %s",
			strings.Join(atlanan, " | "))
	}

	_ = os.MkdirAll(acmeConfigHome, 0o700)
	_ = os.Chmod(acmeConfigHome, 0o700)
	dir := filepath.Join(mailCertKok, alanAdi)
	if e := os.MkdirAll(dir, 0o750); e != nil {
		return "", "", kapsam, atlanan, e
	}
	certPath = filepath.Join(dir, "fullchain.pem")
	keyPath = filepath.Join(dir, "key.pem")

	args := []string{"--issue", "--server", "letsencrypt", "--config-home", acmeConfigHome, "--webroot", MailAcmeWebroot}
	for _, h := range kapsam {
		args = append(args, "-d", h)
	}
	args = append(args, "--keylength", "2048")
	if out, e := exec.Command("/root/.acme.sh/acme.sh", args...).CombinedOutput(); e != nil {
		// acme.sh "Domains not changed / skip" durumunda da 0 dışı dönebilir;
		// sertifika dosyası GERÇEKTEN yerindeyse devam et, yoksa hata ver.
		if !dosyaVarPv(filepath.Join(acmeConfigHome, kapsam[0], "fullchain.cer")) &&
			!dosyaVarPv(filepath.Join(acmeConfigHome, kapsam[0]+"_ecc", "fullchain.cer")) {
			return "", "", kapsam, atlanan, fmt.Errorf("acme mail issue: %s", strings.TrimSpace(string(out)))
		}
	}
	ins := []string{"--install-cert", "--config-home", acmeConfigHome, "-d", kapsam[0],
		"--fullchain-file", certPath, "--key-file", keyPath}
	if out, e := exec.Command("/root/.acme.sh/acme.sh", ins...).CombinedOutput(); e != nil {
		return "", "", kapsam, atlanan, fmt.Errorf("acme mail install: %s", strings.TrimSpace(string(out)))
	}
	// 🔴 "acme.sh hata vermedi" KANIT DEĞİLDİR — dosya gerçekten oluştu mu?
	if !dosyaVarPv(certPath) || !dosyaVarPv(keyPath) {
		return "", "", kapsam, atlanan, fmt.Errorf(
			"acme.sh hata vermedi ama sertifika dosyaları oluşmadı (%s / %s)", certPath, keyPath)
	}
	return certPath, keyPath, kapsam, atlanan, nil
}

// MailSertifikaGonder — cert+key'i mail eklentisinin /sertifika soketine POST'lar.
// Eklenti aynı kutuda çalışır (out-of-process, yerel UNIX soket). Ulaşılamazsa hata.
func MailSertifikaGonder(alanAdi, certPath, keyPath string) error {
	cert, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	govde, _ := json.Marshal(map[string]string{"domain": alanAdi, "cert_pem": string(cert), "key_pem": string(key)})
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	req, _ := http.NewRequest("POST", "http://mail/sertifika", bytes.NewReader(govde))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gosp-Rol", "admin") // core→eklenti güvenilir kanal (yerel soket)
	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("mail eklentisine ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mail eklenti sertifika reddetti (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
