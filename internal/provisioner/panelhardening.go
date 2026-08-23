package provisioner

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	edgeMapEnsure()
	tlsGlobalEnsure()
	logrotateEnsure()
	oomGuardEnsure() // OOM zinciri: userdbd tavani + MariaDB korumasi
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
	// Yedek kopyalar da ayni sirri tasir (_panel.conf.yedek.*) — 0600 root:root.
	if ys, err := filepath.Glob(panelVhostPath + ".yedek*"); err == nil {
		for _, y := range ys {
			_ = os.Chmod(y, 0o600)
			_ = exec.Command("chown", "root:root", y).Run()
		}
	}
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

// edgeMapEnsure: CDN/proxy (Cloudflare vb.) edge'de TLS'i sonlandirdiginda origin'e
// DUZ HTTP gelir. Vhost'un :80 blogu KOSULSUZ "301 https" verirse sonsuz dongu olusur
// (CF "Flexible" modu → ERR_TOO_MANY_REDIRECTS). Bu map, istegin ZINCIRIN BASINDA
// HTTPS olup olmadigini soyler: $gosp_force_https yalniz GERCEK duz-HTTP istemcide 1.
// 00- prefix → domain vhost'larindan ONCE yuklenir (map http-context'te olmali).
func edgeMapEnsure() {
	yol := "/etc/nginx/conf.d/00-gosp-edge.conf"
	icerik := `# GirginOSPanel — edge (CDN/proxy) sema tespiti (otomatik)
map $http_x_forwarded_proto $gosp_xfp_https { default 0; https 1; }
map $http_cf_visitor $gosp_cf_https { default 0; "~*scheme.{0,4}https" 1; }
map "$scheme$gosp_xfp_https$gosp_cf_https" $gosp_force_https {
    default  0;
    "http00" 1;
}
`
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), "gosp_force_https") {
		return
	}
	_ = os.WriteFile(yol, []byte(icerik), 0o644)
}

// tlsGlobalEnsure: TLS politikasini http-context'te TEK dosyada toplar.
// NEDEN: politika her vhost'a kopyalanmisti (23 dosya) ve tutarsizdi —
// _default443/_panel'de cipher hic yoktu, stapling hicbir yerde yoktu.
// 00- prefix → vhost'lardan ONCE yuklenir.
func tlsGlobalEnsure() {
	yol := "/etc/nginx/conf.d/00-gosp-tls.conf"
	icerik := `# GirginOSPanel — global TLS politikasi (otomatik). TEK KAYNAK: politika degisimi
# 23 vhost'u degil YALNIZ bu dosyayi degistirir (Plesk modeli: conf.d/ssl.conf).
# Vhost'lar yalniz ssl_certificate/key tasir.
ssl_protocols TLSv1.2 TLSv1.3;
# Mozilla "Intermediate" — 3DES ve CBC-SHA1 suite'leri DISARIDA (eski HIGH: bunlari aciyordu).
ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
# TLS1.3'te islevsiz, TLS1.2'de istemci tercihine birak (Mozilla onerisi).
ssl_prefer_server_ciphers off;
ssl_session_cache shared:GOSPSSL:50m;
ssl_session_timeout 1d;
# Ticket anahtari rotasyonu olmadan PFS zayiflar → kapali.
ssl_session_tickets off;
# OCSP stapling: el sikismayi hizlandirir, istemcinin CA'ya gitmesini onler.
ssl_stapling on;
ssl_stapling_verify on;
resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;
resolver_timeout 5s;
`
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), "GOSPSSL") {
		return
	}
	_ = os.WriteFile(yol, []byte(icerik), 0o644)
}

// logrotateEnsure: tenant PHP-FPM loglari icin donusum kurali.
// NEDEN: /var/log/php-fpm-c_*/tenant.log hicbir logrotate kuralina girmiyordu;
// tenant sayisi arttikca disk sessizce doluyordu.
func logrotateEnsure() {
	yol := "/etc/logrotate.d/girginospanel-tenant-fpm"
	icerik := `# GirginOSPanel — tenant PHP-FPM loglari (otomatik). Bunlar catch_workers_output=yes
# ile PHP warning'lerini de topladigi icin hizla buyur; kurali olmayan tek log ailesiydi.
/var/log/php-fpm-c_*/tenant.log {
    weekly
    rotate 4
    missingok
    notifempty
    compress
    delaycompress
    # copytruncate: FPM'e sinyal gondermeye gerek yok (reload/kesinti olmaz).
    copytruncate
    su root root
}
`
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), "php-fpm-c_") {
		return
	}
	_ = os.WriteFile(yol, []byte(icerik), 0o644)
}
