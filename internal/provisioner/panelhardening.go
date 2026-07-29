package provisioner

import (
	"log"
	"os"
	"os/exec"
	"strings"

	"girginospanel/internal/httpx"
)

// okuProxyGizli: kalici gizliyi okur (>=32 hex), yoksa "".
func okuProxyGizli() string {
	b, err := os.ReadFile(httpx.ProxySecretPath)
	if err != nil {
		return ""
	}
	if t := strings.TrimSpace(string(b)); len(t) >= 32 {
		return t
	}
	return ""
}

// HealPanelProxyTrustOnStartup: panel vhost'una (yoksa sessiz gecer) su sertlestirmeleri ekler:
//  1. her `X-Real-IP $remote_addr` satirinin ardina paylasimli `X-Gosp-Proxy "<gizli>"`
//     → app :8080'e dogrudan ulasan kiracinin sahte X-Real-IP'sine guvenmez (brute-force kapanir),
//  2. `/api/v1/internal/pma-redeem` disari (nginx uzerinden) DENY — pma-signon dogrudan :8080'e gider,
//  3. slowloris: `client_body_timeout 3600s` → `60s`.
//
// nginx -t basarisizsa HER SEY geri alinir ve gizli dosyasi YAZILMAZ (fail-safe).
func HealPanelProxyTrustOnStartup() {
	orig, err := os.ReadFile(panelVhostPath)
	if err != nil {
		return // panel bu host'ta kurulu degil
	}
	s := string(orig)
	gizliVhostIzniSertlestir() // gizli sizmasin: _panel.conf 0640 root:nginx (kiraci okuyamaz)

	// Mevcut gizliyi koru (reboot'ta rotasyon olmasin); yoksa uret.
	cand := okuProxyGizli()
	if cand == "" {
		cand = httpx.NewProxySecret()
	}
	if cand == "" {
		log.Printf("panel proxy-trust heal: gizli uretilemedi, atlandi")
		return
	}
	istenen := "proxy_set_header X-Gosp-Proxy \"" + cand + "\";"

	// (1) X-Gosp-Proxy enjeksiyonu (idempotent + gizli-rotasyon guvenli).
	if !strings.Contains(s, istenen) {
		var out []string
		for _, ln := range strings.Split(s, "\n") {
			if strings.Contains(ln, "X-Gosp-Proxy") {
				continue // eski gizli satirini temizle
			}
			out = append(out, ln)
			if strings.Contains(ln, "proxy_set_header X-Real-IP $remote_addr;") {
				ind := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
				out = append(out, ind+istenen)
			}
		}
		s = strings.Join(out, "\n")
	}

	// (2) pma-redeem disari kapali.
	if !strings.Contains(s, "location = /api/v1/internal/pma-redeem") {
		deny := "    # ic-uc: yalniz yerel pma-signon (dogrudan :8080) erisir; disari kapali\n" +
			"    location = /api/v1/internal/pma-redeem { deny all; return 403; }\n\n"
		if i := strings.Index(s, "    location /api/ {"); i >= 0 {
			s = s[:i] + deny + s[i:]
		}
	}

	// (3) slowloris timeout.
	s = strings.Replace(s, "client_body_timeout 3600s;", "client_body_timeout 60s;", 1)

	// (4) limit_conn: per-IP eszamanli baglanti tavani (slowloris/baglanti-tukenme derinlik savunmasi).
	limitZoneEnsure()
	if !strings.Contains(s, "limit_conn gosppanel") {
		if i := strings.Index(s, "client_body_timeout 60s;"); i >= 0 {
			son := i + len("client_body_timeout 60s;")
			s = s[:son] + "\n    limit_conn gosppanel 50;  # per-IP eszamanli baglanti tavani (slowloris savunmasi)" + s[son:]
		}
	}

	nginxDegisti := s != string(orig)
	if !nginxDegisti && okuProxyGizli() == cand {
		return // her sey zaten yerinde
	}

	if nginxDegisti {
		if e := os.WriteFile(panelVhostPath, []byte(s), 0644); e != nil {
			log.Printf("panel proxy-trust heal: yazilamadi: %v", e)
			return
		}
		gizliVhostIzniSertlestir()
		if o, e := exec.Command("nginx", "-t").CombinedOutput(); e != nil {
			_ = os.WriteFile(panelVhostPath, orig, 0644) // GERI YUKLE
			log.Printf("panel proxy-trust heal: nginx -t basarisiz, geri alindi: %s", strings.TrimSpace(string(o)))
			return
		}
	}

	// FAIL-SAFE: X-Gosp-Proxy gerçekten enjekte edilmediyse (X-Real-IP çapası yoksa)
	// gizliyi YAZMA → ClientIP eski loopback-güven davranışında kalır, kimse kilitlenmez.
	if !strings.Contains(s, istenen) {
		log.Printf("panel proxy-trust heal: X-Real-IP capasi yoksa gizli yazma — atlandi (fail-safe)")
		return
	}
	// nginx OK (veya degismedi) → gizliyi kalici yaz (app artik guvenir).
	if e := os.WriteFile(httpx.ProxySecretPath, []byte(cand+"\n"), 0600); e != nil {
		log.Printf("panel proxy-trust heal: gizli yazilamadi: %v", e)
		if nginxDegisti {
			_ = os.WriteFile(panelVhostPath, orig, 0644)
			_ = exec.Command("systemctl", "reload", "nginx").Run()
		}
		return
	}
	_ = os.Chmod(httpx.ProxySecretPath, 0600)

	if nginxDegisti {
		if o, e := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); e != nil {
			log.Printf("panel proxy-trust heal: nginx reload: %s", strings.TrimSpace(string(o)))
			return
		}
	}
	log.Printf("panel proxy-trust heal: X-Gosp-Proxy + pma-redeem deny + slowloris timeout OK")
}

// gizliVhostIzniSertlestir: _panel.conf X-Gosp-Proxy gizlisini icerdiginden DUNYA-OKUNABILIR
// olmamali (kiraci okuyup forge eder). 0640 root:nginx — nginx master (root) okur, kiraci
// (other) okuyamaz. Her acilista kosulsuz uygulanir (idempotent).
func gizliVhostIzniSertlestir() {
	_ = exec.Command("chgrp", "nginx", panelVhostPath).Run()
	_ = os.Chmod(panelVhostPath, 0o640)
}

// limitZoneEnsure: limit_conn icin gereken http-context zone'unu ayri bir conf.d dosyasina
// yazar (00- prefix → _panel.conf'tan ONCE yuklenir). Zone tek basina gecerli nginx'tir.
func limitZoneEnsure() {
	yol := "/etc/nginx/conf.d/00-gosp-seclimits.conf"
	istenen := "limit_conn_zone $binary_remote_addr zone=gosppanel:10m;\n"
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), "zone=gosppanel") {
		return
	}
	_ = os.WriteFile(yol, []byte("# GirginOSPanel guvenlik limitleri (otomatik)\n"+istenen), 0o644)
}
