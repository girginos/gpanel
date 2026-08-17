package hostuyg

// Subdomain vhost modu — recipe.NginxProxy.Subdomain=true ise:
//   1. `<sub>.<panelhost>` için LE cert al (acme.sh HTTP-01 webroot)
//   2. `/etc/nginx/conf.d/gpanel-app-<kod>.conf` — ayrı server bloğu :8443 SSL
//   3. Kaldırma: vhost sil + cert dosya sil + acme.sh --remove
//
// Cert konumu: `/etc/ssl/gpanel-apps/<subdomain>/`
// ACME challenge webroot: `/var/www/gpanel-acme-challenge` (panelhost ile ortak)
// ACME config-home: `/opt/gpanel-apps/acme` (hostuyg-özel; panelhost'unkinden ayrı)
//
// ÖNKOŞUL: subdomain DNS A/AAAA panel sunucusuna işaret etmeli. Doğrulama:
// acme.sh --issue kendisi HTTP-01 challenge sırasında bunu test eder.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Port-match regex — `listen 8443 ssl` varyantlarına dayanıklı (çift boşluk,
// `http2`, `default_server`, `[::]:PORT` prefix, satır-içi `server { listen ... }`).
// `\b` word-boundary tercih edildi (`^\s*` satır-ortası match'i kaçırıyordu).
var (
	reListenV4Sub = regexp.MustCompile(`\b(listen\s+)(\d+)(\s+ssl\b)`)
	reListenV6Sub = regexp.MustCompile(`\b(listen\s+\[::\]:)(\d+)(\s+ssl\b)`)
)

const (
	AcmeBin           = "/root/.acme.sh/acme.sh"
	HostuygAcmeConfig = "/opt/gpanel-apps/acme"
	AcmeWebroot       = "/var/www/gpanel-acme-challenge"
	HostuygSSLKok     = "/etc/ssl/gpanel-apps"
	SubdomainVhostDir = "/etc/nginx/conf.d"
)

// PanelListenPortGetir — panelin dış portunu runtime'da oku (Feature 6 ile
// değişebilir). main.go set etmezse 8443 fallback.
// Circular dep engellemek için portyonetim import yerine fn ptr.
var PanelListenPortGetir func() int = func() int { return 8443 }

// AcmeVarMi — acme.sh binary yüklü mü?
func AcmeVarMi() bool {
	st, err := os.Stat(AcmeBin)
	return err == nil && !st.IsDir()
}

// SubdomainAdi — recipe.NginxProxy.SubdomainOn + panelhost'tan tam FQDN üret.
// "gitea" + "lab4.girginos.app" → "gitea.lab4.girginos.app"
func SubdomainAdi(subOn, panelhost string) string {
	if subOn == "" || panelhost == "" {
		return ""
	}
	return strings.ToLower(subOn) + "." + strings.ToLower(panelhost)
}

// SubdomainCertKur — LE cert al + install. Cert konumu döner (fullchain, key).
func SubdomainCertKur(ctx context.Context, subdomain string) (certPath, keyPath string, err error) {
	if !AcmeVarMi() {
		return "", "", fmt.Errorf("acme.sh yüklü değil (%s)", AcmeBin)
	}
	if err := os.MkdirAll(AcmeWebroot, 0755); err != nil {
		return "", "", fmt.Errorf("webroot: %w", err)
	}
	if err := os.MkdirAll(HostuygAcmeConfig, 0700); err != nil {
		return "", "", fmt.Errorf("config-home: %w", err)
	}
	sslDir := filepath.Join(HostuygSSLKok, subdomain)
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		return "", "", fmt.Errorf("ssl dir: %w", err)
	}
	certPath = filepath.Join(sslDir, "fullchain.pem")
	keyPath = filepath.Join(sslDir, "privkey.pem")

	// Geçici ACME vhost — challenge cevabı için :80'de bu subdomain'i handle et
	acmeVhostYol := filepath.Join(SubdomainVhostDir, "gpanel-app-"+subdomain+"-acme.conf")
	acmeVhostIcerik := fmt.Sprintf(`# GEÇİCİ — hostuyg subdomain cert challenge (sil sonra)
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    location /.well-known/acme-challenge/ {
        root %s;
        try_files $uri =404;
    }
    location / { return 404; }
}
`, subdomain, AcmeWebroot)
	if err := atomicYaz(acmeVhostYol, []byte(acmeVhostIcerik), 0644); err != nil {
		return "", "", fmt.Errorf("acme vhost yaz: %w", err)
	}
	defer func() {
		// Cert alındıktan sonra ACME vhost'una gerek yok — sil
		_ = os.Remove(acmeVhostYol)
		_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
	}()

	if out, e := exec.CommandContext(ctx, "nginx", "-t").CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("acme vhost nginx -t: %s", strings.TrimSpace(string(out)))
	}
	if _, e := exec.CommandContext(ctx, "nginx", "-s", "reload").CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("nginx reload: %w", e)
	}

	// acme.sh --issue
	issue := exec.CommandContext(ctx, AcmeBin,
		"--issue",
		"-d", subdomain,
		"--webroot", AcmeWebroot,
		"--server", "letsencrypt",
		"--keylength", "ec-256",
		"--config-home", HostuygAcmeConfig,
	)
	if out, e := issue.CombinedOutput(); e != nil {
		// Zaten var mı? "Domains not changed" gibi olabilir
		if !strings.Contains(string(out), "not changed") {
			return "", "", fmt.Errorf("acme --issue: %s", sonSatirlarSubdomain(string(out), 5))
		}
	}

	// install-cert
	install := exec.CommandContext(ctx, AcmeBin,
		"--install-cert",
		"-d", subdomain, "--ecc",
		"--fullchain-file", certPath,
		"--key-file", keyPath,
		"--config-home", HostuygAcmeConfig,
	)
	if out, e := install.CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("acme --install-cert: %s", sonSatirlarSubdomain(string(out), 5))
	}
	// Key izni 0600 zorla (acme.sh default 0644 world-readable → private key sızar)
	_ = os.Chmod(keyPath, 0600)
	_ = os.Chmod(certPath, 0644)

	// Cert doğrulama
	if raw, e := os.ReadFile(certPath); e == nil {
		if blok, _ := pem.Decode(raw); blok != nil {
			if crt, e2 := x509.ParseCertificate(blok.Bytes); e2 == nil {
				bulundu := false
				for _, n := range crt.DNSNames {
					if strings.EqualFold(n, subdomain) {
						bulundu = true
						break
					}
				}
				if !bulundu {
					return "", "", fmt.Errorf("cert SAN'ı subdomain içermiyor: %v", crt.DNSNames)
				}
			}
		}
	}
	return certPath, keyPath, nil
}

// SubdomainCertSil — cert dosyaları + acme.sh --remove.
func SubdomainCertSil(subdomain string) {
	_, _ = exec.Command(AcmeBin, "--remove", "-d", subdomain, "--ecc",
		"--config-home", HostuygAcmeConfig).CombinedOutput()
	_ = os.RemoveAll(filepath.Join(HostuygSSLKok, subdomain))
}

// SubdomainVhostYaz — ayrı server bloğu :8443 SSL. cert + key path caller'dan.
// UpgradeWS + MaxBodySize + ExtraDirektif de destekler.
func SubdomainVhostYaz(kod, subdomain, certPath, keyPath string, port int, np *NginxProxyTarifi) error {
	upgrade := ""
	if np.UpgradeWS {
		upgrade = "        proxy_http_version 1.1;\n" +
			"        proxy_set_header Upgrade $http_upgrade;\n" +
			"        proxy_set_header Connection \"upgrade\";\n"
	}
	body := ""
	if np.MaxBodySize != "" {
		body = "    client_max_body_size " + np.MaxBodySize + ";\n"
	}
	extra := ""
	if np.ExtraDirektif != "" {
		extra = "        " + np.ExtraDirektif + "\n"
	}

	icerik := fmt.Sprintf(`# OTOMATİK — hostuyg subdomain vhost: %s
server {
    listen %d ssl;
    listen [::]:%d ssl;
    server_name %s;

    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;
    http2 on;

%s    add_header X-Content-Type-Options "nosniff" always;
    add_header Strict-Transport-Security "max-age=31536000" always;

    location / {
        proxy_pass http://127.0.0.1:%d/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
%s%s    }
}
`, kod, PanelListenPortGetir(), PanelListenPortGetir(), subdomain, certPath, keyPath, body, port, upgrade, extra)

	yol := filepath.Join(SubdomainVhostDir, "gpanel-app-"+kod+".conf")
	// LATENT-fix: update senaryosunda mevcut içeriği YEDEKLE ki nginx -t fail
	// durumunda `os.Remove` DEĞİL, yedekten geri yükleme yapılsın.
	var onceki []byte
	varOnceden := false
	if b, err := os.ReadFile(yol); err == nil {
		onceki = b
		varOnceden = true
	}
	geriYukle := func() {
		if varOnceden {
			_ = os.WriteFile(yol, onceki, 0644)
		} else {
			_ = os.Remove(yol)
		}
	}
	if err := atomicYaz(yol, []byte(icerik), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		geriYukle()
		return fmt.Errorf("nginx -t: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("nginx", "-s", "reload").CombinedOutput(); err != nil {
		geriYukle()
		_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
		return fmt.Errorf("nginx reload: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// SubdomainVhostSil — vhost dosyasını sil + reload.
func SubdomainVhostSil(kod string) {
	yol := filepath.Join(SubdomainVhostDir, "gpanel-app-"+kod+".conf")
	_ = os.Remove(yol)
	_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
}

// SubdomainVhostlariPortGuncelle — Feature 6 (Port Değiştirme) callback'i.
// Panel dış portu değişince tüm gpanel-app-*.conf içindeki `listen X ssl`
// (v4+v6) satırları eskiPort → yeniPort dönüştürülür + nginx reload.
// Fail durumunda **tek başına** rollback yapar (silme tehlikeli).
func SubdomainVhostlariPortGuncelle(eskiPort, yeniPort int) error {
	if eskiPort == yeniPort {
		return nil
	}
	entries, err := os.ReadDir(SubdomainVhostDir)
	if err != nil {
		return fmt.Errorf("vhost dir okunamadı: %w", err)
	}
	type yedek struct {
		yol    string
		icerik []byte
	}
	var yedekler []yedek

	guncellenen := 0
	eskiStr := fmt.Sprintf("%d", eskiPort)
	yeniStr := fmt.Sprintf("%d", yeniPort)
	for _, e := range entries {
		ad := e.Name()
		if !strings.HasPrefix(ad, "gpanel-app-") || !strings.HasSuffix(ad, ".conf") {
			continue
		}
		if strings.HasSuffix(ad, "-acme.conf") {
			continue
		}
		yol := filepath.Join(SubdomainVhostDir, ad)
		b, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		s := string(b)
		// Regex-based replace — sadece eskiPort match'lerini değiştir.
		// Suggestion (yedek optimize): değişmeyen dosyaları backup listesine ekleme.
		yeniS := reListenV4Sub.ReplaceAllStringFunc(s, func(m string) string {
			p := reListenV4Sub.FindStringSubmatch(m)
			if len(p) >= 4 && p[2] == eskiStr {
				return p[1] + yeniStr + p[3]
			}
			return m
		})
		yeniS = reListenV6Sub.ReplaceAllStringFunc(yeniS, func(m string) string {
			p := reListenV6Sub.FindStringSubmatch(m)
			if len(p) >= 4 && p[2] == eskiStr {
				return p[1] + yeniStr + p[3]
			}
			return m
		})
		if yeniS == s {
			continue // bu vhost eskiPort'u kullanmıyor — yedek dahi eklenmez
		}
		yedekler = append(yedekler, yedek{yol, b})
		if err := atomicYaz(yol, []byte(yeniS), 0644); err != nil {
			for _, y := range yedekler {
				_ = os.WriteFile(y.yol, y.icerik, 0644)
			}
			return fmt.Errorf("vhost yaz fail (%s): %w — geri alındı", yol, err)
		}
		guncellenen++
	}
	if guncellenen == 0 {
		return nil // hiç subdomain vhost yok
	}
	// nginx -t başarısızsa TAM rollback
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		for _, y := range yedekler {
			_ = os.WriteFile(y.yol, y.icerik, 0644)
		}
		_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
		return fmt.Errorf("nginx -t fail: %s — geri alındı", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("nginx", "-s", "reload").CombinedOutput(); err != nil {
		for _, y := range yedekler {
			_ = os.WriteFile(y.yol, y.icerik, 0644)
		}
		_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
		return fmt.Errorf("nginx reload fail: %s — geri alındı", strings.TrimSpace(string(out)))
	}
	return nil
}

func atomicYaz(yol string, b []byte, mode os.FileMode) error {
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, yol)
}

func sonSatirlarSubdomain(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// CronDosyaYolu — sistem cron.d entry'si (subdomain cert auto-renew için).
const CronDosyaYolu = "/etc/cron.d/gpanel-apps-acme"

// SubdomainCronKontrol — startup'ta çağır (main.go). Eksikse cron entry
// oluşturur. Panelhost cron'undan AYRI (farklı --config-home).
// PROD publish öncesi zorunlu; DEV'de log-only.
func SubdomainCronKontrol() {
	istenen := `# OTOMATİK — hostuyg subdomain cert auto-renew (Faz 3.3)
# panelhost cron'undan AYRI: farklı --config-home
14 4,10,16,22 * * * root ` + AcmeBin + ` --cron --home /root/.acme.sh --config-home ` + HostuygAcmeConfig + ` > /dev/null 2>&1
`
	mev, err := os.ReadFile(CronDosyaYolu)
	if err == nil && string(mev) == istenen {
		return // aynı içerik
	}
	// Atomic yaz (0644 cron.d izni)
	tmp := CronDosyaYolu + ".tmp"
	if err := os.WriteFile(tmp, []byte(istenen), 0644); err != nil {
		log.Printf("hostuyg: cron dosya yazılamadı: %v\n", err)
		return
	}
	if err := os.Rename(tmp, CronDosyaYolu); err != nil {
		log.Printf("hostuyg: cron rename fail: %v\n", err)
		_ = os.Remove(tmp)
		return
	}
	log.Printf("hostuyg: cron entry oluşturuldu → %s\n", CronDosyaYolu)
}
