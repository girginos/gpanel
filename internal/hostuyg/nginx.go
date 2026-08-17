package hostuyg

// Nginx reverse proxy — web app'lere panel hostname üzerinden erişim.
//
// Iki mod:
//   1. SubPath: https://panelhost:8443/gitea  → 127.0.0.1:{port_web}
//      (mevcut _panel.conf server bloğu içine location eklenir)
//   2. Subdomain: https://gitea.panelhost:8443  → 127.0.0.1:{port_web}
//      (ayrı vhost /etc/nginx/conf.d/gpanel-app-{kod}.conf)
//
// SubPath tercih edilir çünkü:
//   - Panel'in mevcut SSL cert'ini kullanır (yeni cert gerekmez)
//   - Panel hostname zaten operatör tarafından ayarlı
//   - Subdomain wildcard cert veya per-subdomain LE gerektirir (kompleks)
//
// UPGRADE: WebSocket destekli app'ler (Gitea, Uptime Kuma) için
// Upgrade+Connection header'ları eklenir.

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	PanelVhostYolu = "/etc/nginx/conf.d/_panel.conf"
	AppVhostDir    = "/etc/nginx/conf.d"
)

// NginxProxyKur — recipe.NginxProxy'ye göre nginx yapılandır.
// SubPath modunda _panel.conf'a location bloğu enjekte edilir (marker'lar
// arasına, idempotent).
// Subdomain modunda ayrı vhost yazılır.
func NginxProxyKur(kod string, tarif *Tarif, port int) error {
	if tarif.NginxProxy == nil {
		return nil
	}
	np := tarif.NginxProxy

	if np.Subdomain {
		return subdomainVhostYaz(kod, np, port)
	}
	return subpathLocationYaz(kod, np, port)
}

// NginxProxyKaldir — kurulum kaldırılırken çağrılır.
// Faz 3.4: cert silme SubdomainAd DB param'ından geliyor (kaldirYurut).
// Bu fn sadece vhost dosyalarını siler (SubPath marker + Subdomain vhost).
func NginxProxyKaldir(kod, subdomainAd string) error {
	// SubPath: _panel.conf'tan marker bloğunu sil
	if err := subpathLocationSil(kod); err != nil {
		return err
	}
	// Subdomain vhost dosyası
	v := filepath.Join(AppVhostDir, "gpanel-app-"+kod+".conf")
	_ = os.Remove(v)
	// Subdomain cert (DB'den geldi, katalog scan YOK — orphan cert borcu kapandı)
	if subdomainAd != "" {
		SubdomainCertSil(subdomainAd)
	}
	if out, err := exec.Command("nginx", "-s", "reload").CombinedOutput(); err != nil {
		log.Printf("hostuyg.NginxProxyKaldir: nginx reload uyarı: %s (%v)",
			strings.TrimSpace(string(out)), err)
	}
	return nil
}

/* ---------- SubPath ---------- */

func markerBasla(kod string) string { return "# >>> gpanel-app:" + kod + " >>>" }
func markerBitis(kod string) string { return "# <<< gpanel-app:" + kod + " <<<" }

func subpathLocationBlok(kod string, np *NginxProxyTarifi, port int) string {
	yol := np.SubPathOn
	if yol == "" {
		yol = "/" + kod
	}
	// Location root: trailing slash olmadan match, proxy_pass sonda slash
	upgrade := ""
	if np.UpgradeWS {
		upgrade = "        proxy_http_version 1.1;\n" +
			"        proxy_set_header Upgrade $http_upgrade;\n" +
			"        proxy_set_header Connection \"upgrade\";\n"
	}
	body := ""
	if np.MaxBodySize != "" {
		body = "        client_max_body_size " + np.MaxBodySize + ";\n"
	}
	extra := ""
	if np.ExtraDirektif != "" {
		extra = "        " + np.ExtraDirektif + "\n"
	}
	return fmt.Sprintf(`%s
    location %s/ {
        proxy_pass http://127.0.0.1:%d/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
%s%s%s    }
%s
`, markerBasla(kod), yol, port, upgrade, body, extra, markerBitis(kod))
}

func subpathLocationYaz(kod string, np *NginxProxyTarifi, port int) error {
	b, err := os.ReadFile(PanelVhostYolu)
	if err != nil {
		return fmt.Errorf("_panel.conf okunamadı: %w", err)
	}
	s := string(b)

	// Zaten var mı? Varsa değiştir
	blok := subpathLocationBlok(kod, np, port)
	if strings.Contains(s, markerBasla(kod)) {
		re := regexp.MustCompile("(?s)" + regexp.QuoteMeta(markerBasla(kod)) +
			`.*?` + regexp.QuoteMeta(markerBitis(kod)) + `\n`)
		s = re.ReplaceAllString(s, blok)
	} else {
		// I3 fix: brace sayarken # yorumları + "..." tırnak içi karakterleri
		// yut. `add_header X-Robots "noindex}"` gibi bir string içindeki `}`
		// yanlış sayıma sebep olmasın.
		serverStart := regexp.MustCompile(`(?m)^\s*server\s*\{`).FindStringIndex(s)
		if serverStart == nil {
			return fmt.Errorf("_panel.conf'ta server bloğu bulunamadı")
		}
		depth := 0
		kapama := -1
		inStr := byte(0) // 0 | '"' | '\''
		inCmt := false
		i := serverStart[1] - 1
	tarama:
		for ; i < len(s); i++ {
			c := s[i]
			// yorum sonu
			if inCmt {
				if c == '\n' {
					inCmt = false
				}
				continue
			}
			// tırnak içi
			if inStr != 0 {
				if c == '\\' && i+1 < len(s) {
					i++
					continue
				}
				if c == inStr {
					inStr = 0
				}
				continue
			}
			switch c {
			case '#':
				inCmt = true
			case '"', '\'':
				inStr = c
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					kapama = i
					break tarama
				}
			}
		}
		if kapama < 0 {
			return fmt.Errorf("_panel.conf server bloğu kapama `}` bulunamadı")
		}
		// SSL listen içeriyor mu kontrol (panel bloğu doğru mu)
		if !strings.Contains(s[serverStart[0]:kapama], "listen ") ||
			!strings.Contains(s[serverStart[0]:kapama], "ssl") {
			return fmt.Errorf("bulunan server bloğu SSL değil — beklenmedik vhost")
		}
		s = s[:kapama] + "\n" + blok + s[kapama:]
	}
	tmp := PanelVhostYolu + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, PanelVhostYolu); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// nginx -t → başarısızsa ATOMIC rollback (C4 fix: WriteFile atomic değil)
	geriAtomic := func() {
		rTmp := PanelVhostYolu + ".rollback.tmp"
		if err := os.WriteFile(rTmp, b, 0644); err != nil {
			return
		}
		_ = os.Rename(rTmp, PanelVhostYolu)
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		geriAtomic()
		return fmt.Errorf("nginx -t: %s (geri alındı)", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("nginx", "-s", "reload").CombinedOutput(); err != nil {
		geriAtomic()
		_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
		return fmt.Errorf("nginx reload: %s (geri alındı)", strings.TrimSpace(string(out)))
	}
	return nil
}

func subpathLocationSil(kod string) error {
	b, err := os.ReadFile(PanelVhostYolu)
	if err != nil {
		return err
	}
	s := string(b)
	if !strings.Contains(s, markerBasla(kod)) {
		return nil
	}
	re := regexp.MustCompile("(?s)" + regexp.QuoteMeta(markerBasla(kod)) +
		`.*?` + regexp.QuoteMeta(markerBitis(kod)) + `\n`)
	s = re.ReplaceAllString(s, "")
	tmp := PanelVhostYolu + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, PanelVhostYolu)
}

/* ---------- Subdomain — Faz 3.3 tam MVP ---------- */

// subdomainVhostYaz — subdomain FQDN üret + LE cert al + SSL vhost yaz.
// panelhost.go'dan `PanelHostVarsayilan` global'i kullanılır.
func subdomainVhostYaz(kod string, np *NginxProxyTarifi, port int) error {
	subdomain := SubdomainAdi(np.SubdomainOn, PanelHostVarsayilan)
	if subdomain == "" {
		return fmt.Errorf("subdomain adı üretilemedi (SubdomainOn=%q, panelhost=%q)",
			np.SubdomainOn, PanelHostVarsayilan)
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Minute) // 3dk LE için
	defer iptal()
	certPath, keyPath, err := SubdomainCertKur(ctx, subdomain)
	if err != nil {
		return fmt.Errorf("cert kur: %w", err)
	}
	return SubdomainVhostYaz(kod, subdomain, certPath, keyPath, port, np)
}

// SubdomainVhostKaldir — kaldırma pipeline'ında NginxProxyKaldir tarafından
// çağrılır. Vhost + cert + acme entry silinir.
func subdomainVhostKaldir(kod, panelhost string, np *NginxProxyTarifi) {
	SubdomainVhostSil(kod)
	if np != nil && np.Subdomain {
		if subdomain := SubdomainAdi(np.SubdomainOn, panelhost); subdomain != "" {
			SubdomainCertSil(subdomain)
		}
	}
}
