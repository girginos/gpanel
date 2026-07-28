package provisioner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// ── 443 catch-all ───────────────────────────────────────────────────────────
// 🔴 SORUN: SSL'i olmayan bir domain'e https:// ile gelindiğinde nginx 443'ü HİÇ
// dinlemiyordu (tek bir SSL'li domain yoksa hiçbir dosyada `listen 443` yok) →
// tarayıcı "siteye bağlanamadı" (ERR_CONNECTION_REFUSED) diyordu. Kullanıcı
// "sertifika uyarısını görüp devam edebilmeli" diyor: bunun için 443'ün DAİMA
// dinlenmesi ve bir sertifika sunulması gerekir.
//
// Çözüm: kendinden imzalı sertifikayla bir `default_server` bloğu. Eşleşmeyen
// (veya SSL'i olmayan) her host buraya düşer; tarayıcı uyarı gösterir, kullanıcı
// devam ederse "SSL kurulu değil" marka sayfası açılır (http bağlantısı verir).
// HSTS-preload TLD'lerde (.app, .dev) tarayıcı yine de bypass'a izin vermez —
// orada tek çözüm gerçek sertifikadır; sayfa bunu da açıkça söyler.

const (
	default443Conf  = "/etc/nginx/conf.d/_default443.conf"
	default443Dizin = "/etc/pki/girginospanel/_default"
	default443Kok   = "/var/www/_default443"
)

// EnsureDefault443: kendinden imzalı sertifika + 443 catch-all vhost + bilgi
// sayfasi (idempotent — icerik degismediyse dokunmaz, nginx reload etmez).
func EnsureDefault443() {
	crt := filepath.Join(default443Dizin, "default.crt")
	key := filepath.Join(default443Dizin, "default.key")

	degisti := false

	// 1) Sertifika (yoksa uret — 10 yillik, yalnizca "TLS cevap versin" amacli)
	if _, err := os.Stat(crt); os.IsNotExist(err) {
		_ = os.MkdirAll(default443Dizin, 0o750)
		host, _ := os.Hostname()
		if host == "" {
			host = "girginospanel"
		}
		out, e := exec.Command("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048",
			"-days", "3650", "-keyout", key, "-out", crt,
			"-subj", "/C=TR/O=GirginOSPanel/CN="+host).CombinedOutput()
		if e != nil {
			log.Printf("default443: sertifika uretilemedi: %v (%s)", e, out)
			return
		}
		_ = os.Chmod(key, 0o600)
		_ = os.Chmod(crt, 0o644)
		degisti = true
	}

	// 2) Bilgi sayfasi
	_ = os.MkdirAll(default443Kok, 0o755)
	sayfa := filepath.Join(default443Kok, "index.html")
	yeniSayfa := []byte(sslYokHTML())
	if mevcut, err := os.ReadFile(sayfa); err != nil || string(mevcut) != string(yeniSayfa) {
		_ = os.WriteFile(sayfa, yeniSayfa, 0o644)
		degisti = true
	}

	// 3) vhost
	blok := fmt.Sprintf(`# GirginOSPanel — 443 catch-all (SSL'siz/eşleşmeyen host'lar)
# Amac: 443 DAIMA dinlensin ki tarayici "baglanamadi" yerine sertifika uyarisi
# gostersin ve kullanici devam edip bilgilendirme sayfasini gorebilsin.
server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    server_name _;

    ssl_certificate     %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:GOSPDEF:1m;

    root %s;
    index index.html;

    # ACME dogrulamasi buraya dusmemeli (domain vhost'u varsa o yakalar), ama
    # catch-all'da da 404 yerine acik cevap verelim.
    location ^~ /_gosp/ {
        alias /usr/share/girginospanel/errors/;
        access_log off;
    }
    location / { try_files $uri /index.html; }

    access_log /var/log/nginx/default443.access.log;
    error_log  /var/log/nginx/default443.error.log warn;
}
`, crt, key, default443Kok)

	if mevcut, err := os.ReadFile(default443Conf); err != nil || string(mevcut) != blok {
		if err := os.WriteFile(default443Conf, []byte(blok), 0o644); err != nil {
			log.Printf("default443: vhost yazilamadi: %v", err)
			return
		}
		degisti = true
	}

	if !degisti {
		return
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		log.Printf("default443: nginx -t basarisiz, geri aliniyor: %s", out)
		_ = os.Remove(default443Conf)
		return
	}
	_, _ = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
	log.Printf("default443: 443 catch-all hazir (SSL'siz host'lar artik baglanti hatasi yerine uyari sayfasi gorur)")
}

// sslYokHTML: sertifika uyarisi asildiktan sonra gorunen bilgilendirme sayfasi.
func sslYokHTML() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="tr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Güvenli bağlantı hazır değil</title>
<style>%s</style>
</head>
<body>
<div class="sayfa">
  <aside class="gorsel">
%s
    <div class="dev">girginos</div>
  </aside>
  <main class="icerik">
    <span class="ust">SSL kurulu değil</span>
    <h1>Bu site için<br><span class="alan">güvenli bağlantı</span> hazır değil</h1>
    <p class="spot">Alan adı sunucumuzda yayında, ancak bu site için henüz bir SSL sertifikası kurulmamış. Siteye şimdilik <b>http://</b> ile erişebilirsiniz.</p>
    <ol class="adimlar">
      <li><span class="no">1</span><span><b>Site sahibiyseniz</b> — panelden <b>SSL</b> bölümünden ücretsiz Let's Encrypt sertifikası kurun; birkaç saniye sürer.</span></li>
      <li><span class="no">2</span><span><b>Ziyaretçiyseniz</b> — adresi <code>http://</code> ile yeniden deneyin.</span></li>
      <li><span class="no">3</span><span><b>.app / .dev</b> gibi alan adlarında tarayıcı yalnız HTTPS'e izin verir; bu durumda sertifika kurulması <b>zorunludur</b>.</span></li>
    </ol>
%s
  </main>
</div>
</body>
</html>`, markaStil, markaCizim, markaAlt)
}
