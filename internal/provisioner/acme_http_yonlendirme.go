package provisioner

// :80 bloğundaki koşulsuz "if ($gosp_force_https) { return 301 }" ACME challenge'ı da 443'e atıyordu
// (rewrite aşaması location seçiminden önce çalışır) → HTTP-01 doğrulaması uygulama/docroot'un kendi
// sayfasını görüp "reader size limit exceeded" ile düşüyor, sertifika sessizce yenilenemiyordu.
// Şablon (vhostTmpl) düzeltildi; bu heal ise DAHA ÖNCE yazılmış vhost'ları açılışta yamalar —
// şablon değişikliği yalnız yeni render'ları etkilediği için eski domainler bu olmadan kırık kalırdı.

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const acmeHTTPEskiBlok = `    if ($gosp_force_https) {
        return 301 https://$host$request_uri;
    }
`

const acmeHTTPYeniBlok = `    set $gosp_https_yonlendir $gosp_force_https;
    if ($uri ~ ^/\.well-known/acme-challenge/) {
        set $gosp_https_yonlendir 0;
    }
    if ($gosp_https_yonlendir) {
        return 301 https://$host$request_uri;
    }
`

// HealACMEHTTPYonlendirmeOnStartup: eski bloğu içeren vhost'ları yamalar. Idempotent (yeni blokta eski
// kalıp yok → ikinci çalıştırma no-op). Tek `nginx -t`; düşerse TÜM yamalar geri alınır.
func HealACMEHTTPYonlendirmeOnStartup() {
	var dosyalar []string
	for _, desen := range []string{"dom_*.conf", "sub_*.conf", "ek_*.conf"} {
		m, _ := filepath.Glob(filepath.Join("/etc/nginx/conf.d", desen))
		dosyalar = append(dosyalar, m...)
	}
	eskiler := map[string][]byte{}
	for _, f := range dosyalar {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), acmeHTTPEskiBlok) {
			continue
		}
		yeni := strings.ReplaceAll(string(b), acmeHTTPEskiBlok, acmeHTTPYeniBlok)
		if err := os.WriteFile(f, []byte(yeni), 0o644); err != nil {
			log.Printf("acme http heal: %s yazılamadı: %v", f, err)
			continue
		}
		eskiler[f] = b
	}
	if len(eskiler) == 0 {
		return
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		for f, b := range eskiler {
			_ = os.WriteFile(f, b, 0o644)
		}
		log.Printf("acme http heal: nginx -t düştü, %d vhost GERİ ALINDI: %s", len(eskiler), strings.TrimSpace(string(out)))
		return
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		log.Printf("acme http heal: nginx reload: %s", strings.TrimSpace(string(out)))
		return
	}
	log.Printf("acme http heal: %d vhost'ta ACME challenge http→https yönlendirmesinden muaf tutuldu", len(eskiler))
}
