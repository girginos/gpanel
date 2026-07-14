// Package provisioner: alan adı için Linux user + nginx vhost + multi-version PHP-FPM + SSL/TLS
package provisioner

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

var (
	alanAdiRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,251}\.[a-z]{2,24}$`)
	slugSan   = regexp.MustCompile(`[^a-z0-9]+`)
)

type phpAyar struct {
	PoolDir string
	SockDir string
	Service string
}

var phpMap = map[string]phpAyar{
	"7.4": {PoolDir: "/etc/opt/remi/php74/php-fpm.d", SockDir: "/var/opt/remi/php74/run/php-fpm", Service: "php74-php-fpm"},
	"8.2": {PoolDir: "/etc/opt/remi/php82/php-fpm.d", SockDir: "/var/opt/remi/php82/run/php-fpm", Service: "php82-php-fpm"},
	"8.3": {PoolDir: "/etc/php-fpm.d", SockDir: "/run/php-fpm", Service: "php-fpm"},
	"8.4": {PoolDir: "/etc/opt/remi/php84/php-fpm.d", SockDir: "/var/opt/remi/php84/run/php-fpm", Service: "php84-php-fpm"},
	"8.5": {PoolDir: "/etc/opt/remi/php85/php-fpm.d", SockDir: "/var/opt/remi/php85/run/php-fpm", Service: "php85-php-fpm"},
}

func ValidateDomain(d string) error {
	d = strings.ToLower(strings.TrimSpace(d))
	if d == "" {
		return fmt.Errorf("alan adı boş")
	}
	if len(d) > 253 {
		return fmt.Errorf("alan adı çok uzun")
	}
	if !alanAdiRe.MatchString(d) {
		return fmt.Errorf("geçersiz alan adı biçimi (örnek: example.com)")
	}
	return nil
}

func SlugFromDomain(d string) string {
	s := strings.ToLower(strings.TrimSpace(d))
	s = slugSan.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 26 {
		s = s[:26]
	}
	return "c_" + s
}

func normalizePHP(v string) string {
	v = strings.TrimSpace(v)
	if _, ok := phpMap[v]; !ok {
		return "8.3"
	}
	return v
}

// vhost template — SSL var/yok her durumu kapsar
var vhostTmpl = template.Must(template.New("v").Parse(`{{- if .SSL -}}
# {{.AlanAdi}} — 80 üzerinde HTTP-01 challenge için açık; geri kalan trafik 443'e yönlendirilir
server {
    listen 80;
    listen [::]:80;
    server_name {{.AlanAdi}} www.{{.AlanAdi}};

    location /.well-known/acme-challenge/ {
        root /var/www/_acme;
        auth_basic off;
        try_files $uri =404;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.AlanAdi}} www.{{.AlanAdi}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

    # ---- Güvenlik header'ları (panel'den yönetilir) ----
{{if .HdrXContentType}}    add_header X-Content-Type-Options "nosniff" always;
{{end}}{{if .HdrXXSS}}    add_header X-XSS-Protection "1; mode=block" always;
{{end}}{{if .HdrReferrer}}    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
{{end}}{{if .HdrPermissions}}    add_header Permissions-Policy "geolocation=(), microphone=(), camera=(), interest-cohort=()" always;
{{end}}{{if .HdrCSPUpgrade}}    add_header Content-Security-Policy "upgrade-insecure-requests" always;
{{end}}{{if .HdrHSTS}}    add_header Strict-Transport-Security "max-age={{.HSTSMaxAge}}{{if .HSTSSubdomains}}; includeSubDomains{{end}}{{if .HSTSPreload}}; preload{{end}}" always;
{{end}}
    root {{.WebRoot}};
    index index.php index.html index.htm;

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

{{if eq .Backend "apache"}}    # ---- Backend: Apache (127.0.0.1:10080 proxy) ----
    location / {
        proxy_pass http://127.0.0.1:10080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Host $host;
        proxy_read_timeout 60s;
    }
{{else if eq .Backend "static"}}    # ---- Backend: Statik dosya (PHP yok) — PHP-EXFIL guard ----
    location ~* \.(php|phtml|php3|php4|php5|phps)(/|$) { return 404; }
    location / { try_files $uri $uri/ =404; }
{{else}}    # ---- Backend: nginx + PHP-FPM (default) ----
    location / { try_files $uri $uri/ /index.php?$query_string; }

{{if .FastCgiCache}}    set $skip_cache 0;
    if ($request_method = POST) { set $skip_cache 1; }
    if ($query_string != "") { set $skip_cache 1; }
    if ($request_uri ~* "/wp-admin/|/wp-login.php|/cart/|/checkout/|/my-account/|preview=true|sitemap.*\.xml") { set $skip_cache 1; }
    if ($http_cookie ~* "comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_no_cache|wordpress_logged_in") { set $skip_cache 1; }
{{end}}    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:{{.PHPSocket}};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param PATH_INFO $fastcgi_path_info;
        fastcgi_param HTTPS on;
        fastcgi_read_timeout 60s;
{{if .FastCgiCache}}        fastcgi_cache girgincache;
        fastcgi_cache_valid 200 301 302 {{.FastCgiCacheDakika}}m;
        fastcgi_cache_valid 404 1m;
        fastcgi_cache_bypass $skip_cache;
        fastcgi_no_cache $skip_cache;
        fastcgi_cache_use_stale error timeout invalid_header updating http_500 http_503;
        fastcgi_cache_background_update on;
        fastcgi_cache_lock on;
        add_header X-Cache-Status $upstream_cache_status always;
{{end}}    }
{{end}}
{{if .BrowserCache}}    # ---- Browser cache (statik dosyalar) ----
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|mp4|webm|pdf|zip|gz)$ {
        expires {{.BrowserCacheGun}}d;
        access_log off;
        add_header Cache-Control "public, immutable" always;
    }
{{end}}

    location ~ /\.(?!well-known) { deny all; }

{{if .EkDirektifler}}    # ---- Ek direktifler (kullanıcı) ----
    {{.EkDirektifler}}
{{end}}    # GirginOSPanel managed (SSL: {{.SSLKaynak}}) — {{.AlanAdi}}
}
{{- else -}}
server {
    listen 80;
    listen [::]:80;
    server_name {{.AlanAdi}} www.{{.AlanAdi}};

    root {{.WebRoot}};
    index index.php index.html index.htm;

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

    # ---- Güvenlik header'ları (panel'den yönetilir) ----
{{if .HdrXContentType}}    add_header X-Content-Type-Options "nosniff" always;
{{end}}{{if .HdrXXSS}}    add_header X-XSS-Protection "1; mode=block" always;
{{end}}{{if .HdrReferrer}}    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
{{end}}{{if .HdrPermissions}}    add_header Permissions-Policy "geolocation=(), microphone=(), camera=(), interest-cohort=()" always;
{{end}}{{if .HdrCSPUpgrade}}    add_header Content-Security-Policy "upgrade-insecure-requests" always;
{{end}}
    location /.well-known/acme-challenge/ {
        root /var/www/_acme;
        auth_basic off;
        try_files $uri =404;
    }

{{if eq .Backend "apache"}}    # ---- Backend: Apache (127.0.0.1:10080 proxy) ----
    location / {
        proxy_pass http://127.0.0.1:10080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto http;
        proxy_set_header X-Forwarded-Host $host;
        proxy_read_timeout 60s;
    }
{{else if eq .Backend "static"}}    # ---- Backend: Statik (PHP yok) — PHP-EXFIL guard ----
    location ~* \.(php|phtml|php3|php4|php5|phps)(/|$) { return 404; }
    location / { try_files $uri $uri/ =404; }
{{else}}    # ---- Backend: nginx + PHP-FPM (default) ----
    location / { try_files $uri $uri/ /index.php?$query_string; }

{{if .FastCgiCache}}    set $skip_cache 0;
    if ($request_method = POST) { set $skip_cache 1; }
    if ($query_string != "") { set $skip_cache 1; }
    if ($request_uri ~* "/wp-admin/|/wp-login.php|/cart/|/checkout/|/my-account/|preview=true|sitemap.*\.xml") { set $skip_cache 1; }
    if ($http_cookie ~* "comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_no_cache|wordpress_logged_in") { set $skip_cache 1; }
{{end}}    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:{{.PHPSocket}};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param PATH_INFO $fastcgi_path_info;
        fastcgi_read_timeout 60s;
{{if .FastCgiCache}}        fastcgi_cache girgincache;
        fastcgi_cache_valid 200 301 302 {{.FastCgiCacheDakika}}m;
        fastcgi_cache_valid 404 1m;
        fastcgi_cache_bypass $skip_cache;
        fastcgi_no_cache $skip_cache;
        fastcgi_cache_use_stale error timeout invalid_header updating http_500 http_503;
        fastcgi_cache_background_update on;
        fastcgi_cache_lock on;
        add_header X-Cache-Status $upstream_cache_status always;
{{end}}    }
{{end}}
{{if .BrowserCache}}    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|mp4|webm|pdf|zip|gz)$ {
        expires {{.BrowserCacheGun}}d;
        access_log off;
        add_header Cache-Control "public, immutable" always;
    }
{{end}}

    location ~ /\.(?!well-known) { deny all; }

{{if .EkDirektifler}}    # ---- Ek direktifler (kullanıcı) ----
    {{.EkDirektifler}}
{{end}}    # GirginOSPanel managed — {{.AlanAdi}} (HTTP only, PHP {{.PHPSurum}})
}
{{- end -}}
`))

var phpPoolTmpl = template.Must(template.New("p").Parse(`[{{.User}}]
user = {{.User}}
group = {{.User}}
listen = {{.Socket}}
listen.owner = nginx
listen.group = nginx
listen.mode = 0660
pm = ondemand
pm.max_children = 8
pm.process_idle_timeout = 30s
pm.max_requests = 500
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen
php_admin_value[upload_tmp_dir] = /home/{{.User}}/tmp
php_admin_value[sys_temp_dir] = /home/{{.User}}/tmp
php_admin_value[session.save_path] = /home/{{.User}}/tmp
catch_workers_output = yes
`))

// VhostOpts: tek render fonksiyonu, SSL bilgisi opsiyonel
type VhostOpts struct {
	AlanAdi   string
	WebRoot   string
	PHPSocket string
	PHPSurum  string
	CertPath  string
	KeyPath   string
	SSLKaynak string // "self-signed" | "letsencrypt" | ""

	// nginx security header toggle'lari (default true)
	HdrXContentType bool
	HdrXXSS         bool
	HdrReferrer     bool
	HdrPermissions  bool
	HdrCSPUpgrade   bool
	HdrHSTS         bool
	HSTSMaxAge      int
	HSTSSubdomains  bool
	HSTSPreload     bool

	// Performans onbellegi
	FastCgiCache       bool
	FastCgiCacheDakika int
	BrowserCache       bool
	BrowserCacheGun    int

	// Kullanici ek direktifleri
	EkDirektifler string

	// Web sunucu backend: "php-fpm" (default) | "apache" | "static"
	Backend string
}

func (o VhostOpts) SSL() bool {
	return o.CertPath != "" && o.KeyPath != ""
}

type Result struct {
	SistemKullanici string
	WebRoot         string
	FTPHost         string
	PHPSurum        string
	PHPSocket       string
}

func phpPoolPath(sk, phpSurum string) (string, string, string) {
	v := normalizePHP(phpSurum)
	ay := phpMap[v]
	return filepath.Join(ay.PoolDir, sk+".conf"),
		filepath.Join(ay.SockDir, sk+".sock"),
		ay.Service
}

// renderAndReload: vhost'u yaz + nginx -t + reload (SSL var/yok aynı yol)
// Backend "apache" ise per-domain Apache vhost'unu da yazıp httpd'yi yeniden yükler.
// Backend değiştirildiyse eski Apache vhost dosyası temizlenir.
func renderAndReload(opts VhostOpts, sk string) error {
	// Default backend: php-fpm
	if opts.Backend == "" {
		opts.Backend = "php-fpm"
	}

	var buf bytes.Buffer
	if err := vhostTmpl.Execute(&buf, opts); err != nil {
		return fmt.Errorf("template render: %w", err)
	}
	cfgPath := "/etc/nginx/conf.d/dom_" + sk + ".conf"
	if err := os.WriteFile(cfgPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("vhost yaz: %w", err)
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t başarısız: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Apache backend yönetimi (idempotent — yoksa yaz, varsa sil)
	if opts.Backend == "apache" {
		if err := writeApacheVhost(opts, sk); err != nil {
			return err
		}
	} else {
		if err := deleteApacheVhostIfExists(sk); err != nil {
			return err
		}
	}
	return nil
}

func Provision(alanAdi, phpSurum string) (*Result, error) {
	if err := ValidateDomain(alanAdi); err != nil {
		return nil, err
	}
	phpSurum = normalizePHP(phpSurum)
	alanAdi = strings.ToLower(strings.TrimSpace(alanAdi))
	u := SlugFromDomain(alanAdi)
	home := "/home/" + u

	if !userExists(u) {
		out, err := exec.Command("useradd", "-m", "-d", home, "-s", "/usr/sbin/nologin", u).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "already exists") {
			return nil, fmt.Errorf("useradd: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}

	dirs := []string{"public_html", "logs", "tmp", "ssl", ".cron"}
	for _, d := range dirs {
		_ = os.MkdirAll(filepath.Join(home, d), 0755)
	}

	uid, gid, err := uidGid(u)
	if err == nil {
		_ = filepath.Walk(home, func(p string, _ os.FileInfo, _ error) error {
			_ = os.Chown(p, uid, gid)
			return nil
		})
	}

	_ = os.Chmod(home, 0711)
	_ = filepath.Walk(filepath.Join(home, "public_html"), func(p string, info os.FileInfo, _ error) error {
		if info == nil {
			return nil
		}
		if info.IsDir() {
			_ = os.Chmod(p, 0755)
		} else {
			_ = os.Chmod(p, 0644)
		}
		return nil
	})

	indexPath := filepath.Join(home, "public_html", "index.html")
	_ = os.WriteFile(indexPath, []byte(welcomeHTML(alanAdi)), 0644)
	if err == nil {
		_ = os.Chown(indexPath, uid, gid)
	}

	_, _ = exec.Command("restorecon", "-R", home).CombinedOutput()

	// PHP-FPM pool
	poolPath, socket, service := phpPoolPath(u, phpSurum)
	_ = os.MkdirAll(filepath.Dir(poolPath), 0755)
	_ = os.MkdirAll(filepath.Dir(socket), 0755)
	var poolBuf bytes.Buffer
	_ = phpPoolTmpl.Execute(&poolBuf, map[string]string{"User": u, "Socket": socket})
	if err := os.WriteFile(poolPath, poolBuf.Bytes(), 0644); err != nil {
		return nil, fmt.Errorf("php pool yaz: %w", err)
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", service).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("php-fpm (%s) reload: %s: %w", service, strings.TrimSpace(string(out)), err)
	}

	// vhost (SSL yok başlangıçta)
	if err := renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  phpSurum,
	}, u); err != nil {
		return nil, err
	}

	return &Result{
		SistemKullanici: u,
		WebRoot:         filepath.Join(home, "public_html"),
		FTPHost:         alanAdi, // not: ftp_host DB'ye handler'da h.IPv4 (sunucu IP) yazılır
		PHPSurum:        phpSurum,
		PHPSocket:       socket,
	}, nil
}

func Deprovision(alanAdi, sk string) error {
	cfgPath := "/etc/nginx/conf.d/dom_" + sk + ".conf"
	_ = os.Remove(cfgPath)
	for _, ay := range phpMap {
		p := filepath.Join(ay.PoolDir, sk+".conf")
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
			_, _ = exec.Command("systemctl", "reload-or-restart", ay.Service).CombinedOutput()
		}
	}
	_, _ = exec.Command("systemctl", "reload", "nginx").CombinedOutput()

	if !strings.HasPrefix(sk, "c_") {
		return fmt.Errorf("güvenlik: c_ prefix'li olmayan kullanıcı silinmez")
	}
	if userExists(sk) {
		_, _ = exec.Command("userdel", "-r", sk).CombinedOutput()
	}
	return nil
}

func SetPHPVersion(alanAdi, sk, yeniSurum, certPath, keyPath, sslKaynak, backend string) (string, error) {
	yeniSurum = normalizePHP(yeniSurum)
	for _, ay := range phpMap {
		p := filepath.Join(ay.PoolDir, sk+".conf")
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
			_, _ = exec.Command("systemctl", "reload-or-restart", ay.Service).CombinedOutput()
		}
	}

	poolPath, socket, service := phpPoolPath(sk, yeniSurum)
	_ = os.MkdirAll(filepath.Dir(poolPath), 0755)
	_ = os.MkdirAll(filepath.Dir(socket), 0755)
	var poolBuf bytes.Buffer
	_ = phpPoolTmpl.Execute(&poolBuf, map[string]string{"User": sk, "Socket": socket})
	if err := os.WriteFile(poolPath, poolBuf.Bytes(), 0644); err != nil {
		return "", fmt.Errorf("yeni pool yaz: %w", err)
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", service).CombinedOutput(); err != nil {
		return "", fmt.Errorf("php-fpm (%s) reload: %s: %w", service, strings.TrimSpace(string(out)), err)
	}

	home := "/home/" + sk
	if err := renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  yeniSurum,
		CertPath:  certPath,
		KeyPath:   keyPath,
		SSLKaynak: sslKaynak,
		Backend:   backend,
	}, sk); err != nil {
		return "", err
	}
	return socket, nil
}

// EnableSelfSigned: openssl ile self-signed cert üret, vhost SSL-li yeniden render
func EnableSelfSigned(alanAdi, sk, phpSurum, backend string) (certPath, keyPath string, err error) {
	phpSurum = normalizePHP(phpSurum)
	home := "/home/" + sk
	sslDir := filepath.Join(home, "ssl")
	_ = os.MkdirAll(sslDir, 0755)

	certPath = filepath.Join(sslDir, alanAdi+".crt")
	keyPath = filepath.Join(sslDir, alanAdi+".key")

	subj := fmt.Sprintf("/C=TR/ST=Local/L=GirginOSPanel/O=%s/CN=%s", alanAdi, alanAdi)
	args := []string{
		"req", "-x509", "-nodes",
		"-newkey", "rsa:2048",
		"-keyout", keyPath,
		"-out", certPath,
		"-days", "365",
		"-subj", subj,
		"-addext", "subjectAltName=DNS:" + alanAdi + ",DNS:www." + alanAdi,
	}
	if out, err := exec.Command("openssl", args...).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("openssl: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = os.Chmod(keyPath, 0640)

	uid, gid, _ := uidGid(sk)
	_ = os.Chown(certPath, uid, gid)
	_ = os.Chown(keyPath, uid, gid)
	_, _ = exec.Command("restorecon", "-R", sslDir).CombinedOutput()

	_, socket, _ := phpPoolPath(sk, phpSurum)
	if err := renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  phpSurum,
		CertPath:  certPath,
		KeyPath:   keyPath,
		SSLKaynak: "self-signed",
		Backend:   backend,
	}, sk); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

// EnableLetsEncrypt: acme.sh ile cert al, vhost SSL-li yeniden render
func EnableLetsEncrypt(alanAdi, sk, phpSurum, backend string) (certPath, keyPath string, err error) {
	phpSurum = normalizePHP(phpSurum)
	home := "/home/" + sk

	// acme webroot
	_ = os.MkdirAll("/var/www/_acme", 0755)
	_, _ = exec.Command("restorecon", "-R", "/var/www/_acme").CombinedOutput()

	sslDir := filepath.Join(home, "ssl")
	_ = os.MkdirAll(sslDir, 0755)
	certPath = filepath.Join(sslDir, alanAdi+".crt")
	keyPath = filepath.Join(sslDir, alanAdi+".key")

	// acme.sh issue (HTTP-01 webroot)
	args := []string{
		"--issue",
		"--webroot", "/var/www/_acme",
		"-d", alanAdi,
		"-d", "www." + alanAdi,
		"--keylength", "2048",
		"--force",
	}
	if out, err := exec.Command("/root/.acme.sh/acme.sh", args...).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("acme.sh issue: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// acme.sh install-cert ile target path'lere yerleştir
	insArgs := []string{
		"--install-cert",
		"-d", alanAdi,
		"--cert-file", certPath,
		"--key-file", keyPath,
		"--fullchain-file", certPath,
		"--reloadcmd", "systemctl reload nginx",
	}
	if out, err := exec.Command("/root/.acme.sh/acme.sh", insArgs...).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("acme.sh install-cert: %s: %w", strings.TrimSpace(string(out)), err)
	}

	uid, gid, _ := uidGid(sk)
	_ = os.Chown(certPath, uid, gid)
	_ = os.Chown(keyPath, uid, gid)
	_, _ = exec.Command("restorecon", "-R", sslDir).CombinedOutput()

	_, socket, _ := phpPoolPath(sk, phpSurum)
	if err := renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  phpSurum,
		CertPath:  certPath,
		KeyPath:   keyPath,
		SSLKaynak: "letsencrypt",
		Backend:   backend,
	}, sk); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

// DisableSSL: vhost'u SSL'siz hale döndür, cert dosyalarını silme (ileride yeniden açılabilir)
func DisableSSL(alanAdi, sk, phpSurum, backend string) error {
	phpSurum = normalizePHP(phpSurum)
	home := "/home/" + sk
	_, socket, _ := phpPoolPath(sk, phpSurum)
	return renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  phpSurum,
		Backend:   backend,
	}, sk)
}

func userExists(u string) bool {
	_, err := user.Lookup(u)
	return err == nil
}

func uidGid(u string) (int, int, error) {
	uu, err := user.Lookup(u)
	if err != nil {
		return 0, 0, err
	}
	uid, _ := strconv.Atoi(uu.Uid)
	gid, _ := strconv.Atoi(uu.Gid)
	return uid, gid, nil
}

func welcomeHTML(domain string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="tr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:Inter,system-ui,sans-serif;background:linear-gradient(135deg,#f8fafc,#fff7ed);min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px}
  .card{max-width:560px;background:#fff;border:1px solid #e2e8f0;border-radius:16px;padding:48px;text-align:center;box-shadow:0 10px 25px rgba(0,0,0,0.05)}
  .logo{width:48px;height:48px;background:#ea580c;border-radius:10px;margin:0 auto 20px;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:700}
  h1{font-size:24px;color:#0f172a;margin-bottom:8px}
  p{color:#64748b;line-height:1.6;margin-bottom:8px}
  .muted{font-size:13px;color:#94a3b8;margin-top:24px}
  code{background:#f1f5f9;padding:2px 6px;border-radius:4px;font-size:13px;color:#475569}
</style>
</head>
<body>
<div class="card">
  <div class="logo">G</div>
  <h1>%s</h1>
  <p>Web sitesi başarıyla oluşturuldu.</p>
  <p>İçerik yüklemek için FTP veya dosya yöneticisini kullanın.</p>
  <p class="muted">Web kökü: <code>public_html/</code> · PHP destekli · GirginOSPanel ile yönetiliyor</p>
</div>
</body>
</html>`, domain, domain)
}

// ApplyVhostForDomain: domainID'ye gore nginx vhost'unu yeniden render eder.
// PHP versiyonu/socket degisikliklerinden sonra cagrilir; SSL bilgilerini DB'den okur.
func ApplyVhostForDomain(db *sql.DB, domainID int64, socket, surum string) error {
	var alanAdi, sk, certPath, keyPath, sslKaynak, backend string
	if err := db.QueryRow(
		`SELECT alan_adi, sistem_kullanici, COALESCE(cert_path,''), COALESCE(key_path,''), COALESCE(ssl_kaynak,''), COALESCE(web_backend,'php-fpm')
		 FROM domains WHERE id=?`, domainID).
		Scan(&alanAdi, &sk, &certPath, &keyPath, &sslKaynak, &backend); err != nil {
		return fmt.Errorf("domain bilgi cek: %w", err)
	}
	home := "/home/" + sk

	// nginx_settings (yoksa default true)
	opts := VhostOpts{
		AlanAdi:         alanAdi,
		WebRoot:         filepath.Join(home, "public_html"),
		PHPSocket:       socket,
		PHPSurum:        surum,
		CertPath:        certPath,
		KeyPath:         keyPath,
		SSLKaynak:       sslKaynak,
		Backend:         backend,
		HdrXContentType: true, HdrXXSS: true, HdrReferrer: true,
		HdrPermissions: true, HdrCSPUpgrade: true, HdrHSTS: true,
		HSTSMaxAge: 31536000, HSTSSubdomains: true, HSTSPreload: false,
	}
	// Default cache: kapali fastcgi, acik browser cache 30 gun
	opts.FastCgiCache = false
	opts.FastCgiCacheDakika = 60
	opts.BrowserCache = true
	opts.BrowserCacheGun = 30

	var b1, b2, b3, b4, b5, b6, b7, b8, bFC, bBC int
	var maxAge, fcDk, bcGun int
	var ek string
	err := db.QueryRow(
		`SELECT hdr_x_content_type, hdr_x_xss, hdr_referrer, hdr_permissions,
		        hdr_csp_upgrade, hdr_hsts, hsts_max_age, hsts_subdomains, hsts_preload, ek_direktifler,
		        fastcgi_cache, fastcgi_cache_dakika, browser_cache, browser_cache_gun
		 FROM nginx_settings WHERE domain_id=?`, domainID).
		Scan(&b1, &b2, &b3, &b4, &b5, &b6, &maxAge, &b7, &b8, &ek,
			&bFC, &fcDk, &bBC, &bcGun)
	if err == nil {
		opts.HdrXContentType = b1 == 1
		opts.HdrXXSS = b2 == 1
		opts.HdrReferrer = b3 == 1
		opts.HdrPermissions = b4 == 1
		opts.HdrCSPUpgrade = b5 == 1
		opts.HdrHSTS = b6 == 1
		opts.HSTSMaxAge = maxAge
		opts.HSTSSubdomains = b7 == 1
		opts.HSTSPreload = b8 == 1
		opts.EkDirektifler = ek
		opts.FastCgiCache = bFC == 1
		opts.FastCgiCacheDakika = fcDk
		opts.BrowserCache = bBC == 1
		opts.BrowserCacheGun = bcGun
	}
	// Korumali dizin (.htpasswd) bloklari — nginx_settings satiri olsun olmasin eklenir
	if pb := buildProtectedBlocks(db, domainID, socket); pb != "" {
		if opts.EkDirektifler != "" {
			opts.EkDirektifler += "\n"
		}
		opts.EkDirektifler += pb
	}
	return renderAndReload(opts, sk)
}

// PHPSocketFor: kullanıcı + sürüm verildiğinde aktif socket yolunu döner
func PHPSocketFor(sk, surum string) (string, error) {
	surum = normalizePHP(surum)
	// AppStream 8.3
	if surum == "8.3" {
		return "/run/php-fpm/" + sk + ".sock", nil
	}
	// Remi pattern: 5.6 -> 56, 7.4 -> 74, 8.2 -> 82
	kod := strings.Replace(surum, ".", "", 1)
	if len(kod) >= 2 {
		socketDir := "/var/opt/remi/php" + kod + "/run/php-fpm"
		// Servisin gerçekten var olduğunu kontrol et
		if _, err := os.Stat("/opt/remi/php" + kod + "/root/usr/sbin/php-fpm"); err == nil {
			return socketDir + "/" + sk + ".sock", nil
		}
	}
	return "", fmt.Errorf("desteklenmeyen veya kurulmamış sürüm: %s", surum)
}

// buildProtectedBlocks: korumali_dizinler tablosundan nginx auth_basic location bloklari uretir.
// Her korunan yol icin outer prefix location + PHP kaynak sizmasini engelleyen nested .php location.
func buildProtectedBlocks(db *sql.DB, domainID int64, socket string) string {
	rows, err := db.Query(`SELECT DISTINCT yol, htpasswd_dosya FROM korumali_dizinler WHERE domain_id=? ORDER BY yol`, domainID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var yol, dosya string
		if err := rows.Scan(&yol, &dosya); err != nil {
			continue
		}
		if yol == "/" {
			// Kök dizin: ayrı "location /" oluşturulamaz — mevcut zorunlu "location /"
			// ile aynı prefix olur ve nginx "duplicate location" verir. Bunun yerine
			// auth_basic'i SERVER seviyesinde tanımlarız; tüm location'lar (PHP dahil)
			// bunu miras alır. acme-challenge location'ı "auth_basic off" ile muaf
			// tutulduğu için Let's Encrypt sertifika alımı/yenilemesi etkilenmez.
			fmt.Fprintf(&b, `    auth_basic "Kimlik Dogrulamasi Gerekli";
    auth_basic_user_file %s;
`, dosya)
			continue
		}
		fmt.Fprintf(&b, `    location ^~ %s {
        auth_basic "Kimlik Dogrulamasi Gerekli";
        auth_basic_user_file %s;
        location ~ \.php$ {
            auth_basic "Kimlik Dogrulamasi Gerekli";
            auth_basic_user_file %s;
            try_files $uri =404;
            fastcgi_split_path_info ^(.+\.php)(/.+)$;
            fastcgi_pass unix:%s;
            fastcgi_index index.php;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
            fastcgi_param HTTPS on;
        }
    }
`, yol, dosya, dosya, socket)
	}
	_ = rows.Err()
	return b.String()
}
