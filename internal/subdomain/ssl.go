package subdomain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// Subdomain SSL: self-signed veya Let's Encrypt. Parent domain ile AYNI mantık
// (openssl / acme.sh --webroot /var/www/_acme) ama subdomain vhost'una (sub_*.conf) uygulanır.

func sslDir(sk string) string { return "/home/" + sk + "/ssl" }
func sslSid(r *http.Request) int64 {
	v, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	return v
}

func certYolu(sk, tamAd string) (string, string) {
	d := sslDir(sk)
	return filepath.Join(d, tamAd+".crt"), filepath.Join(d, tamAd+".key")
}

// subInfo: sid + parent'tan alt_ad/tam_ad/php_surum çöz.
func (h *Handlers) subInfo(r *http.Request, id int64) (altAd, tamAd, phpSurum string, ok bool) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`,
		sid, id).Scan(&altAd, &tamAd, &phpSurum); err != nil {
		return "", "", "", false
	}
	return altAd, tamAd, phpSurum, true
}

// GET /domains/{id}/subdomain/{sid}/ssl — durum
func (h *Handlers) SSLDurum(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, tamAd, _, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	crt, key := certYolu(sk, tamAd)
	aktif := dosyaVar(crt) && dosyaVar(key)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"aktif": aktif})
}

// POST /domains/{id}/subdomain/{sid}/ssl  {tip:"self-signed"|"letsencrypt"}
func (h *Handlers) SSLKur(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, demo, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	altAd, tamAd, phpSurum, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	var req struct {
		Tip string `json:"tip"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	tip := strings.ToLower(strings.TrimSpace(req.Tip))
	if tip == "" {
		tip = "self-signed"
	}

	if _, err := provisioner.PHPSocketFor(sk, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü kurulu değil: "+phpSurum)
		return
	}
	docroot := docrootOf(sk, tamAd)
	crt, key := certYolu(sk, tamAd)
	_ = os.MkdirAll(sslDir(sk), 0o750)

	switch tip {
	case "letsencrypt", "le":
		// Challenge subdomainin KENDI docroot'unda; acme kaydi panel veri dizininde.
		_ = os.MkdirAll(filepath.Join(docroot, ".well-known", "acme-challenge"), 0o755)
		_, _ = exec.Command("restorecon", "-R", filepath.Join(docroot, ".well-known")).CombinedOutput()
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--issue", "--server", "letsencrypt",
			"--config-home", provisioner.AcmeConfigHome(), "--webroot", docroot,
			"-d", tamAd, "--keylength", "ec-256").CombinedOutput(); err != nil {
			// 🔴 acme.sh çıkış kodu 2 = RENEW_SKIP: geçerli cert ZATEN var, yenileme gerekmiyor.
			// Bu HATA DEĞİL — mevcut cert'i install-cert ile yerleştirmeye devam et. Aksi halde
			// (eski hata) ikinci kurulumda "Let's Encrypt alınamadı" yanılgısı verip panel SSL'i
			// hiç kurmuyordu. Yalnız DİĞER çıkış kodları (DNS/challenge hatası) gerçek başarısızlık.
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
				httpx.WriteError(w, http.StatusBadRequest,
					"Let's Encrypt alınamadı (subdomain DNS'i bu sunucuya A kaydıyla yönlendirilmeli): "+strings.TrimSpace(string(out)))
				return
			}
		}
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--install-cert", "--config-home", provisioner.AcmeConfigHome(), "-d", tamAd, "--ecc",
			"--key-file", key, "--fullchain-file", crt,
			"--reloadcmd", "systemctl reload nginx").CombinedOutput(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "cert yerleştirilemedi: "+strings.TrimSpace(string(out)))
			return
		}
	default: // self-signed
		if out, err := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
			"-days", "365", "-keyout", key, "-out", crt,
			"-subj", "/CN="+tamAd, "-addext", "subjectAltName=DNS:"+tamAd).CombinedOutput(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "openssl: "+strings.TrimSpace(string(out)))
			return
		}
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, sslDir(sk)).Run()
	_ = exec.Command("restorecon", "-R", sslDir(sk)).Run()

	// vhost'u yeniden yaz — cert artık var, rebuildVhost HTTPS bloğunu üretir;
	// alt alanın özel PHP havuzu + nginx/backend ayarları KORUNUR.
	if err := h.rebuildVhost(r.Context(), sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "tam_ad": tamAd, "tip": tip})
}

// DELETE /domains/{id}/subdomain/{sid}/ssl — SSL'i kaldır, HTTP'ye dön
func (h *Handlers) SSLKaldir(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, demo, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	altAd, tamAd, phpSurum, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	crt, key := certYolu(sk, tamAd)
	_ = os.Remove(crt)
	_ = os.Remove(key)
	// cert gitti → rebuildVhost HTTP bloğunu üretir; özel ayarlar KORUNUR.
	if err := h.rebuildVhost(r.Context(), sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func dosyaVar(p string) bool { _, err := os.Stat(p); return err == nil }

func vhostSSL(tamAd, docroot, socket, crt, key, koruma string) string {
	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %[1]s;
    location /.well-known/acme-challenge/ { root %[2]s; auth_basic off; try_files $uri =404; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name %[1]s;

    ssl_certificate     %[4]s;
    ssl_certificate_key %[5]s;
    ssl_protocols TLSv1.2 TLSv1.3;

    root %[2]s;
    index index.php index.html index.htm;

    access_log /var/log/nginx/%[1]s.access.log;
    error_log  /var/log/nginx/%[1]s.error.log warn;

    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

%[6]s
    error_page 404 /_gosp_404.html;
    location = /_gosp_404.html {
        root /usr/share/girginospanel/errors;
        internal;
        access_log off;
    }
    location ^~ /_gosp/ {
        alias /usr/share/girginospanel/errors/;
        access_log off;
        expires 7d;
        gzip on;
        gzip_types application/json application/javascript;
    }

    location / { try_files $uri $uri/ /index.php?$query_string; }

    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:%[3]s;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTPS on;
        fastcgi_read_timeout 60s;
    }

    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|pdf|zip|gz)$ {
        expires 30d;
        access_log off;
    }

    location ~ /\.(?!well-known) { deny all; }
}
`, tamAd, docroot, socket, crt, key, koruma)
}
