package subdomain

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// ── Alt alan web-sunucu ayarları (domain nginx_settings + web_backend paritesi) ──

type subNginx struct {
	HdrXContentType    bool   `json:"hdr_x_content_type"`
	HdrXXSS            bool   `json:"hdr_x_xss"`
	HdrReferrer        bool   `json:"hdr_referrer"`
	HdrPermissions     bool   `json:"hdr_permissions"`
	HdrCSPUpgrade      bool   `json:"hdr_csp_upgrade"`
	HdrHSTS            bool   `json:"hdr_hsts"`
	HSTSMaxAge         int    `json:"hsts_max_age"`
	HSTSSubdomains     bool   `json:"hsts_subdomains"`
	HSTSPreload        bool   `json:"hsts_preload"`
	FastcgiCache       bool   `json:"fastcgi_cache"`
	FastcgiCacheDakika int    `json:"fastcgi_cache_dakika"`
	BrowserCache       bool   `json:"browser_cache"`
	BrowserCacheGun    int    `json:"browser_cache_gun"`
	ClientMaxBodyMB    int    `json:"client_max_body_mb"`
	EkDirektifler      string `json:"ek_direktifler"`
}

func subNginxDefaults() subNginx {
	return subNginx{HdrXContentType: true, HdrXXSS: true, HSTSMaxAge: 31536000,
		FastcgiCacheDakika: 60, BrowserCache: true, BrowserCacheGun: 30, ClientMaxBodyMB: 64}
}

func subNginxGet(ctx context.Context, db *sql.DB, sid int64) (subNginx, error) {
	n := subNginxDefaults()
	row := db.QueryRowContext(ctx, `SELECT hdr_x_content_type, hdr_x_xss, hdr_referrer, hdr_permissions,
		hdr_csp_upgrade, hdr_hsts, hsts_max_age, hsts_subdomains, hsts_preload,
		fastcgi_cache, fastcgi_cache_dakika, browser_cache, browser_cache_gun, client_max_body_mb, ek_direktifler
		FROM subdomain_nginx_settings WHERE subdomain_id=?`, sid)
	err := row.Scan(&n.HdrXContentType, &n.HdrXXSS, &n.HdrReferrer, &n.HdrPermissions,
		&n.HdrCSPUpgrade, &n.HdrHSTS, &n.HSTSMaxAge, &n.HSTSSubdomains, &n.HSTSPreload,
		&n.FastcgiCache, &n.FastcgiCacheDakika, &n.BrowserCache, &n.BrowserCacheGun, &n.ClientMaxBodyMB, &n.EkDirektifler)
	if err == sql.ErrNoRows {
		return n, nil
	}
	return n, err
}

func subNginxSave(ctx context.Context, db *sql.DB, sid int64, n subNginx) error {
	b := func(x bool) int {
		if x {
			return 1
		}
		return 0
	}
	_, err := db.ExecContext(ctx, `INSERT INTO subdomain_nginx_settings(subdomain_id,
		hdr_x_content_type, hdr_x_xss, hdr_referrer, hdr_permissions, hdr_csp_upgrade, hdr_hsts,
		hsts_max_age, hsts_subdomains, hsts_preload, fastcgi_cache, fastcgi_cache_dakika,
		browser_cache, browser_cache_gun, client_max_body_mb, ek_direktifler)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE hdr_x_content_type=VALUES(hdr_x_content_type), hdr_x_xss=VALUES(hdr_x_xss),
		hdr_referrer=VALUES(hdr_referrer), hdr_permissions=VALUES(hdr_permissions),
		hdr_csp_upgrade=VALUES(hdr_csp_upgrade), hdr_hsts=VALUES(hdr_hsts), hsts_max_age=VALUES(hsts_max_age),
		hsts_subdomains=VALUES(hsts_subdomains), hsts_preload=VALUES(hsts_preload),
		fastcgi_cache=VALUES(fastcgi_cache), fastcgi_cache_dakika=VALUES(fastcgi_cache_dakika),
		browser_cache=VALUES(browser_cache), browser_cache_gun=VALUES(browser_cache_gun),
		client_max_body_mb=VALUES(client_max_body_mb), ek_direktifler=VALUES(ek_direktifler)`,
		sid, b(n.HdrXContentType), b(n.HdrXXSS), b(n.HdrReferrer), b(n.HdrPermissions), b(n.HdrCSPUpgrade), b(n.HdrHSTS),
		n.HSTSMaxAge, b(n.HSTSSubdomains), b(n.HSTSPreload), b(n.FastcgiCache), n.FastcgiCacheDakika,
		b(n.BrowserCache), n.BrowserCacheGun, n.ClientMaxBodyMB, n.EkDirektifler)
	return err
}

var gecerliSubBackend = map[string]bool{"php-fpm": true, "apache": true, "static": true}

func webBackendGet(ctx context.Context, db *sql.DB, sid int64) string {
	var b string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(web_backend,'php-fpm') FROM subdomanlar WHERE id=?`, sid).Scan(&b)
	if b == "" {
		b = "php-fpm"
	}
	return b
}

// ── nginx vhost render (parite: backend switch + cache + client_max_body + başlıklar) ──

func subApachePath(sk, altAd string) string {
	return "/etc/httpd/conf.d/sub_apache_" + sk + "_" + altAd + ".conf"
}

func subSecHeaders(n subNginx, ssl bool) string {
	var b strings.Builder
	if n.HdrXContentType {
		b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	}
	if n.HdrXXSS {
		b.WriteString("    add_header X-XSS-Protection \"1; mode=block\" always;\n")
	}
	if n.HdrReferrer {
		b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n")
	}
	if n.HdrPermissions {
		b.WriteString("    add_header Permissions-Policy \"geolocation=(), microphone=(), camera=()\" always;\n")
	}
	if n.HdrCSPUpgrade {
		b.WriteString("    add_header Content-Security-Policy \"upgrade-insecure-requests\" always;\n")
	}
	if ssl && n.HdrHSTS {
		v := fmt.Sprintf("max-age=%d", n.HSTSMaxAge)
		if n.HSTSSubdomains {
			v += "; includeSubDomains"
		}
		if n.HSTSPreload {
			v += "; preload"
		}
		b.WriteString("    add_header Strict-Transport-Security \"" + v + "\" always;\n")
	}
	return b.String()
}

// phpBlok: backend'e göre PHP/statik lokasyonu.
func phpBlok(o subVhostOpts) string {
	switch o.Backend {
	case "static":
		return "    location ~ \\.php$ { return 403; }\n"
	case "apache":
		return `    location ~ \.php$ {
        proxy_pass http://` + provisioner.ApacheUpstream + `;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
    }
`
	default: // php-fpm
		cache := ""
		if o.N.FastcgiCache {
			cache = fmt.Sprintf("        fastcgi_cache girgincache;\n        fastcgi_cache_valid 200 301 302 %dm;\n        fastcgi_cache_key $scheme$request_method$host$request_uri;\n", o.N.FastcgiCacheDakika)
		}
		https := ""
		if o.SSL {
			https = "        fastcgi_param HTTPS on;\n"
		}
		return `    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:` + o.Socket + `;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
` + https + cache + `        fastcgi_read_timeout 60s;
    }
`
	}
}

func browserCacheBlok(n subNginx) string {
	if !n.BrowserCache {
		return ""
	}
	return fmt.Sprintf("    location ~* \\.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|pdf|zip|gz)$ {\n        expires %dd;\n        access_log off;\n    }\n", n.BrowserCacheGun)
}

func serverGovde(o subVhostOpts) string {
	extras := ""
	if o.N.ClientMaxBodyMB > 0 {
		extras += fmt.Sprintf("    client_max_body_size %dm;\n", o.N.ClientMaxBodyMB)
	}
	extras += subSecHeaders(o.N, o.SSL)
	if strings.TrimSpace(o.N.EkDirektifler) != "" {
		extras += "    # Ek direktifler\n    " + strings.ReplaceAll(strings.TrimSpace(o.N.EkDirektifler), "\n", "\n    ") + "\n"
	}
	return `    root ` + o.DocRoot + `;
    index index.php index.html index.htm;

    access_log /var/log/nginx/` + o.TamAd + `.access.log;
    error_log  /var/log/nginx/` + o.TamAd + `.error.log warn;

` + extras + o.Koruma + `
    error_page 404 /_gosp_404.html;
    location = /_gosp_404.html { root /usr/share/girginospanel/errors; internal; access_log off; }
    location ^~ /_gosp/ { alias /usr/share/girginospanel/errors/; access_log off; expires 7d; }

    location / { try_files $uri $uri/ /index.php?$query_string; }
` + phpBlok(o) + browserCacheBlok(o.N) + `    location ~ /\.(?!well-known) { deny all; }
`
}

func renderSubVhost(o subVhostOpts) string {
	if o.SSL {
		return `server {
    listen 80;
    listen [::]:80;
    server_name ` + o.TamAd + `;
    location /.well-known/acme-challenge/ { root ` + o.DocRoot + `; auth_basic off; try_files $uri =404; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ` + o.TamAd + `;

    ssl_certificate     ` + o.Crt + `;
    ssl_certificate_key ` + o.Key + `;
    ssl_protocols TLSv1.2 TLSv1.3;

` + serverGovde(o) + `}
`
	}
	return `server {
    listen 80;
    listen [::]:80;
    server_name ` + o.TamAd + `;

    location /.well-known/acme-challenge/ { auth_basic off; root ` + o.DocRoot + `; try_files $uri =404; }

` + serverGovde(o) + `}
`
}

type subVhostOpts struct {
	TamAd, DocRoot, Socket, Backend string
	SSL                             bool
	Crt, Key, Koruma                string
	N                               subNginx
}

// ── Apache backend (alt alana özel httpd vhost) ──

func subApacheYaz(sk, altAd, tamAd, docroot, socket string) error {
	body := "# " + tamAd + " — GirginOSPanel Apache backend (alt alan)\n" +
		"<VirtualHost 127.0.0.1:10080>\n" +
		"    ServerName " + tamAd + "\n" +
		"    DocumentRoot " + docroot + "\n" +
		"    <Directory " + docroot + ">\n" +
		"        AllowOverride All\n        Require all granted\n    </Directory>\n" +
		"    <FilesMatch \\.php$>\n" +
		"        SetHandler \"proxy:unix:" + socket + "|fcgi://localhost\"\n" +
		"    </FilesMatch>\n" +
		"    ErrorLog /var/log/httpd/" + tamAd + "_error.log\n" +
		"</VirtualHost>\n"
	if err := os.WriteFile(subApachePath(sk, altAd), []byte(body), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("httpd", "-t").CombinedOutput(); err != nil {
		_ = os.Remove(subApachePath(sk, altAd))
		return fmt.Errorf("httpd doğrulanamadı: %s", strings.TrimSpace(string(out)))
	}
	_ = exec.Command("systemctl", "reload", "httpd").Run()
	return nil
}

func subApacheSil(sk, altAd string) {
	p := subApachePath(sk, altAd)
	if _, err := os.Stat(p); err == nil {
		_ = os.Remove(p)
		_ = exec.Command("systemctl", "reload", "httpd").Run()
	}
}

// ── Handler'lar ──

// GET /domains/{id}/subdomain/{sid}/web-sunucu
func (h *Handlers) WebGet(w http.ResponseWriter, r *http.Request) {
	id, _, _, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&n)
	if n == 0 {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	ng, _ := subNginxGet(r.Context(), h.DB, sid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"backend":   webBackendGet(r.Context(), h.DB, sid),
		"mevcutlar": []string{"php-fpm", "apache", "static"},
		"nginx":     ng,
	})
}

// PUT /domains/{id}/subdomain/{sid}/web-sunucu {backend?, nginx?}
func (h *Handlers) WebPut(w http.ResponseWriter, r *http.Request) {
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
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var altAd, tamAd, php string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).
		Scan(&altAd, &tamAd, &php); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	var req struct {
		Backend string   `json:"backend"`
		Nginx   subNginx `json:"nginx"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// ek_direktifler güvenlik doğrulaması (domain ile aynı)
	if kotu := provisioner.TehlikeliNginxDirektifi(req.Nginx.EkDirektifler); kotu != "" {
		httpx.WriteError(w, http.StatusBadRequest, "izin verilmeyen nginx direktifi: "+kotu)
		return
	}
	if err := provisioner.ValidateNginxDirectives(req.Nginx.EkDirektifler); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "nginx direktif hatası: "+err.Error())
		return
	}
	backend := strings.TrimSpace(req.Backend)
	if backend == "" {
		backend = webBackendGet(r.Context(), h.DB, sid)
	}
	if !gecerliSubBackend[backend] {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz web sunucu tipi")
		return
	}
	if err := subNginxSave(r.Context(), h.DB, sid, req.Nginx); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "nginx ayarları kaydedilemedi")
		return
	}
	if _, err := h.DB.Exec(`UPDATE subdomanlar SET web_backend=? WHERE id=? AND domain_id=?`, backend, sid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "backend kaydedilemedi")
		return
	}
	if err := h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "backend": backend})
}
