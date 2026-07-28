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

	socket, err := provisioner.PHPSocketFor(sk, phpSurum)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü kurulu değil: "+phpSurum)
		return
	}
	docroot := docrootOf(sk, tamAd)
	crt, key := certYolu(sk, tamAd)
	_ = os.MkdirAll(sslDir(sk), 0o750)

	switch tip {
	case "letsencrypt", "le":
		_ = os.MkdirAll("/var/www/_acme", 0o755)
		_, _ = exec.Command("restorecon", "-R", "/var/www/_acme").CombinedOutput()
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--issue", "--webroot", "/var/www/_acme",
			"-d", tamAd, "--keylength", "ec-256").CombinedOutput(); err != nil {
			httpx.WriteError(w, http.StatusBadRequest,
				"Let's Encrypt alınamadı (subdomain DNS'i bu sunucuya A kaydıyla yönlendirilmeli): "+strings.TrimSpace(string(out)))
			return
		}
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--install-cert", "-d", tamAd, "--ecc",
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

	// vhost'u SSL-li yeniden yaz
	conf := confPath(sk, altAd)
	if err := os.WriteFile(conf, []byte(vhostSSL(tamAd, docroot, socket, crt, key, provisioner.ProtectedBlocksForSub(h.DB, sslSid(r), socket))), 0o644); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost yazılamadı")
		return
	}
	_ = exec.Command("restorecon", conf).Run()
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		// rollback: HTTP vhost'a dön
		_ = os.WriteFile(conf, []byte(vhost(tamAd, docroot, socket, provisioner.ProtectedBlocksForSub(h.DB, sslSid(r), socket))), 0o644)
		_ = exec.Command("systemctl", "reload", "nginx").Run()
		httpx.WriteError(w, http.StatusInternalServerError, "nginx doğrulanamadı: "+strings.TrimSpace(string(out)))
		return
	}
	_ = exec.Command("systemctl", "reload", "nginx").Run()
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
	socket, err := provisioner.PHPSocketFor(sk, phpSurum)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü kurulu değil")
		return
	}
	docroot := docrootOf(sk, tamAd)
	crt, key := certYolu(sk, tamAd)
	_ = os.Remove(crt)
	_ = os.Remove(key)
	conf := confPath(sk, altAd)
	_ = os.WriteFile(conf, []byte(vhost(tamAd, docroot, socket, provisioner.ProtectedBlocksForSub(h.DB, sslSid(r), socket))), 0o644)
	_ = exec.Command("restorecon", conf).Run()
	_ = exec.Command("systemctl", "reload", "nginx").Run()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func dosyaVar(p string) bool { _, err := os.Stat(p); return err == nil }

func vhostSSL(tamAd, docroot, socket, crt, key, koruma string) string {
	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %[1]s;
    location /.well-known/acme-challenge/ { root /var/www/_acme; try_files $uri =404; }
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
