package provisioner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// ── 80 catch-all ────────────────────────────────────────────────────────────
// 🔴 SORUN (49.12.158.182'de ölçüldü, HER kurulumu etkiliyor): port 80
// catch-all vhost'u installer tarafından STATİK olarak kopyalanıyordu
// (assets/nginx/_default80.conf) ama belge kökünü — /var/www/_default80 —
// ve içindeki index.html'i HİÇ KİMSE oluşturmuyordu. Dizin yokken vhost'un
// `try_files $uri /index.html` kuralı var olmayan /index.html'e yönleniyor,
// bu da kendini tekrar tetikliyor:
//
//	[error] rewrite or internal redirection cycle while internally
//	        redirecting to "/index.html" ... server: _
//
// Sonuç: eşleşmeyen HER Host header'ı port 80'de **HTTP 500** alıyordu.
// 443 tarafı doğruydu çünkü onu bu paketteki Go provisioner (default443.go)
// her açılışta garanti ediyor — belge kökü + sayfa + vhost birlikte. 80
// tarafında bu simetri yoktu; asimetri tam da hatanın kaynağıydı.
//
// Bu dosya 80'i 443 ile SİMETRİK hale getirir: belge kökü, sayfa ve vhost
// tek yerde, her panel açılışında idempotent olarak garanti edilir. Böylece
// mevcut sunucular da güncelleme sonrası ilk açılışta KENDİNİ ONARIR —
// installer düzeltmesini beklemeleri gerekmez.
const (
	default80Conf = "/etc/nginx/conf.d/_default80.conf"
	default80Kok  = "/var/www/_default80"
)

// EnsureDefault80: port 80 catch-all belge kökü + bilgi sayfası + vhost.
// İdempotent: içerik değişmediyse dosyaya dokunmaz, nginx'i reload etmez.
func EnsureDefault80() {
	degisti := false

	// 1) Belge kökü + bilgi sayfası (EKSİK OLAN PARÇA BUYDU)
	if err := os.MkdirAll(default80Kok, 0o755); err != nil {
		log.Printf("default80: belge kökü oluşturulamadı: %v", err)
		return
	}
	sayfa := filepath.Join(default80Kok, "index.html")
	yeni := []byte(parkHTML())
	if mevcut, err := os.ReadFile(sayfa); err != nil || string(mevcut) != string(yeni) {
		if err := os.WriteFile(sayfa, yeni, 0o644); err != nil {
			log.Printf("default80: bilgi sayfası yazılamadı: %v", err)
			return
		}
		degisti = true
	}

	// 2) vhost
	blok := fmt.Sprintf(`# GirginOSPanel — 80 catch-all (eşleşmeyen Host header'lar)
# Belge kökünü de bu dosyayı yazan Go kodu garanti eder (bkz default80.go).
# Dizin yoksa try_files sonsuz iç yönlendirmeye girip 500 üretir — birlikte
# yönetilmelerinin sebebi budur.
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    root %s;
    index index.html;

    # ACME http-01: domain'in kendi vhost'u yoksa dogrulama buraya duser.
    location ^~ /.well-known/acme-challenge/ {
        root /var/www/acme;
        try_files $uri =404;
        access_log off;
    }

    location ^~ /_gosp/ {
        alias /usr/share/girginospanel/errors/;
        access_log off;
    }

    location / { try_files $uri /index.html; }

    access_log /var/log/nginx/default80.access.log;
    error_log  /var/log/nginx/default80.error.log warn;
}
`, default80Kok)

	if mevcut, err := os.ReadFile(default80Conf); err != nil || string(mevcut) != blok {
		// Geri alabilmek icin mevcut halini sakla (nginx -t duserse donecegiz).
		onceki, vardi := os.ReadFile(default80Conf)
		if err := os.WriteFile(default80Conf, []byte(blok), 0o644); err != nil {
			log.Printf("default80: vhost yazilamadi: %v", err)
			return
		}
		if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
			log.Printf("default80: nginx -t basarisiz, geri aliniyor: %s", out)
			if vardi == nil {
				_ = os.WriteFile(default80Conf, onceki, 0o644)
			} else {
				_ = os.Remove(default80Conf)
			}
			return
		}
		degisti = true
	}

	// ACME webroot'u da dursun ki yukaridaki location 404 yerine calissin.
	_ = os.MkdirAll("/var/www/acme/.well-known/acme-challenge", 0o755)

	if !degisti {
		return
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		log.Printf("default80: nginx -t basarisiz: %s", out)
		return
	}
	_, _ = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
	log.Printf("default80: 80 catch-all hazir (eslesmeyen host'lar artik 500 yerine bilgi sayfasi gorur)")
}

// parkHTML: bu sunucuda yayinda olmayan bir alan adiyla gelindiginde gorunur.
func parkHTML() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="tr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Site yayında değil</title>
<style>%s</style>
</head>
<body>
<div class="sayfa">
  <aside class="gorsel">
%s
    <div class="dev">girginos</div>
  </aside>
  <main class="icerik">
    <span class="ust">Yayında değil</span>
    <h1>Bu adres için<br><span class="alan">tanımlı bir site</span> bulunamadı</h1>
    <p class="spot">İstek sunucumuza ulaştı, ancak bu alan adı için burada yapılandırılmış bir site yok. Alan adı yeni yönlendirildiyse yayılmanın tamamlanması biraz sürebilir.</p>
    <ol class="adimlar">
      <li><span class="no">1</span><span><b>Site sahibiyseniz</b> — panelden alan adını <b>Alan Adları</b> bölümünden ekleyin.</span></li>
      <li><span class="no">2</span><span><b>Yeni eklediyseniz</b> — DNS yayılması için birkaç dakika bekleyip yeniden deneyin.</span></li>
      <li><span class="no">3</span><span><b>Ziyaretçiyseniz</b> — adresi kontrol edin veya site sahibine bildirin.</span></li>
    </ol>
%s
  </main>
</div>
</body>
</html>`, markaStil, markaCizim, markaAlt)
}
