package panelhost

// :80 mini vhost — panel hostname'i için. YALNIZ .well-known/acme-challenge
// yolunu webroot'a bağlar, kalan trafiği HTTPS panele 301 yönlendirir. Diğer
// :80 vhost'larıyla çakışmaz çünkü `server_name` özel bir isim; başka isimle
// gelen 80 istekleri buraya düşmez.

import (
	"fmt"
	"os"
	"regexp"
)

const acmeVhostSablon = `# OTOMATİK ÜRETİLDİ — panelhost.AcmeVhostYaz
#
# Panel'in kendi hostname'i için :80 mini vhost. YALNIZ Let's Encrypt HTTP-01
# challenge dosyalarını sunar (%s → dosya). Kalan yollar HTTPS panele 301.
#
# 🔴 Bu dosyayı elle düzenleme — hostname değişince panelhost.AcmeVhostYaz
# üzerine yazar. Cert alma sonrası da yerinde kalır (auto-renew için).
server {
    listen 80;
    listen [::]:80;
    server_name %s;

    # ACME HTTP-01 challenge — webroot mod
    location /.well-known/acme-challenge/ {
        root %s;
        try_files $uri =404;
        access_log off;
    }

    # Kalan HTTP trafiği HTTPS panele — kullanıcı sekmede http:// yazsa bile
    # doğru porta gitsin.
    # 🔴 $server_name (deterministik) — $host Host başlığından gelir ve open
    # redirect riski taşır. server_name eşleşmesi zaten hostname'i garanti eder.
    location / {
        return 301 https://$server_name:%s$request_uri;
    }
}
`

// AcmeVhostYaz — hostname için :80 vhost'unu üret. İdempotent (aynı içerikse
// dosya yazmaz).
func AcmeVhostYaz(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname boş")
	}
	yeni := fmt.Sprintf(acmeVhostSablon, WebrootDir, hostname, WebrootDir, PanelPort())
	// Aynıysa dokunma — nginx reload gereksiz olur
	if mev, err := os.ReadFile(AcmeVhostPath); err == nil && string(mev) == yeni {
		return nil
	}
	return os.WriteFile(AcmeVhostPath, []byte(yeni), 0644)
}

// AcmeVhostSil — yalnız rollback yolunda kullanılır (nginx -t başarısızsa).
func AcmeVhostSil() error {
	if _, err := os.Stat(AcmeVhostPath); err != nil {
		return nil
	}
	return os.Remove(AcmeVhostPath)
}

// PanelPort — panel vhost'unun dinledigi HTTPS portu (_panel.conf'tan okunur).
// 🔴 NEDEN: port sabit 8443 varsayiliyordu; kurulumda cakisma olursa panel baska
// porta (or. 7443) tasinir ve HTTP->HTTPS yonlendirmesi YANLIS porta gider
// (panel acilmaz). Okunamazsa 8443'e duser.
func PanelPort() string {
	b, err := os.ReadFile(PanelVhostPath)
	if err == nil {
		if m := rePanelListen.FindSubmatch(b); len(m) >= 2 {
			return string(m[1])
		}
	}
	return "8443"
}

var rePanelListen = regexp.MustCompile(`(?m)^\s*listen\s+(\d+)\s+ssl`)
