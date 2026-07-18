// Package provisioner: alan adı için Linux user + nginx vhost + multi-version PHP-FPM + SSL/TLS
package provisioner

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
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

// pkgDB: askıya-alma durumunu HER vhost render'ında kontrol edebilmek için
// paket düzeyinde tutulan DB handle'ı (main.go'da Init ile set edilir).
// Böylece SetPHP/SSL/backend gibi doğrudan renderAndReload çağıran yollar da
// askıdaki bir domain'i sessizce yeniden yayına almaz.
var pkgDB *sql.DB

// Init: paket DB handle'ını ayarlar (askıya-alma tutarlılığı için) ve açılışta
// (her boot) fastcgi_cache "girgincache" zone tanımını GARANTİ EDİP nginx'i
// reload eder. Böylece "kullanım var, tanım yok" durumundaki MEVCUT sunucular
// yalnızca güncelleme + panel restart ile ELLE müdahale olmadan kendiliğinden
// onarılır (heal-on-startup).
func Init(d *sql.DB) {
	pkgDB = d
	HealCacheZoneOnStartup()
	// Batch2 sertlestirme: panel vhost guvenlik header'lari + mevcut tenant
	// vhost/pool'larinin (retroaktif) guvenli yeniden-render'i. Her ikisi de
	// sentinel/rollback korumali → tekrar-guvenli ve kirilmaz.
	HealPanelVhostHeadersOnStartup()
	HealVhostsOnStartup()
	HealHomePerms()            // Batch3: mevcut tenant ev dizinlerine izolasyon izinleri (retroaktif)
	ensureFPMSELinuxFcontext() // Batch5A: /run/php-fpm-<sk>/ için SELinux fcontext (taze Enforcing kurulumda ilk domain 500 vermesin)
	EnsureTenantFPMOnStartup() // Batch5A: kurulu per-tenant FPM servislerini (Seçenek A) ayakta tut
}

// cacheZoneConf: panelin yönettiği TEK fastcgi_cache zone tanım dosyası.
// http-context'e (conf.d nginx.conf http{} içine include edilir) yazılır.
const cacheZoneConf = "/etc/nginx/conf.d/girgincache.conf"

// cacheZoneTempConf: elle eklenmiş GEÇİCİ mitigasyon dosyası (aynı zone'u tanımlar).
// Panel kendi yönetilen dosyasını yazmadan ÖNCE bunu kaldırır; aksi halde iki
// "keys_zone=girgincache" tanımı → nginx "duplicate zone" ile patlar.
const cacheZoneTempConf = "/etc/nginx/conf.d/00-girgincache-gecici.conf"

// cacheZoneName: vhost template'inde kullanılan zone adı ile AYNI olmalı.
const cacheZoneName = "girgincache"

// cacheZoneDir: fastcgi_cache_path disk dizini.
const cacheZoneDir = "/var/cache/nginx/girgincache"

// cacheZoneBody: girgincache.conf içeriği (idempotent — sabit yol + sabit içerik).
const cacheZoneBody = `# GirginOSPanel tarafından otomatik yönetilir — ELLE DÜZENLEMEYİN.
# vhost'lar "fastcgi_cache girgincache" kullanır; zone tanımı burada garanti edilir.
# Her vhost render'ında ve panel açılışında yeniden yazılır (idempotent).
fastcgi_cache_path ` + cacheZoneDir + ` levels=1:2 keys_zone=` + cacheZoneName + `:100m max_size=1g inactive=60m use_temp_path=off;
`

// zoneDefinedElsewhereRe: nginx.conf veya başka bir conf.d dosyasında elle
// tanımlanmış girgincache zone'unu tespit eder (çift tanım = nginx -t hatası).
var zoneDefinedElsewhereRe = regexp.MustCompile(`keys_zone\s*=\s*` + regexp.QuoteMeta(cacheZoneName) + `\s*:`)

// HealCacheZoneOnStartup: açılışta girgincache zone tanımını garanti eder ve
// (yalnızca bir değişiklik yapıldıysa ve config geçerliyse) nginx'i reload eder.
// Bu, güncelleme sonrası restart'ta "tanım yok" sunucuların canlı olarak onarılmasını
// sağlar. nginx -t hâlâ başarısızsa reload ATLANIR (çalışan nginx'i bozmayız).
func HealCacheZoneOnStartup() {
	changed, err := ensureCacheZone()
	if err != nil {
		log.Printf("girgincache heal: zone conf yazılamadı: %v", err)
		return
	}
	if !changed {
		return // config zaten tutarlı, gereksiz reload yok
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		log.Printf("girgincache heal: nginx -t hâlâ başarısız, reload atlandı: %s", strings.TrimSpace(string(out)))
		return
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		log.Printf("girgincache heal: nginx reload: %s", strings.TrimSpace(string(out)))
		return
	}
	log.Printf("girgincache heal: zone tanımı garanti edildi + nginx reload OK")
}

// ensureCacheZone: "girgincache" fastcgi_cache zone tanımının nginx http-context'te
// TAM OLARAK BİR KEZ mevcut olmasını garanti eder. Kendi yönettiğimiz conf dosyasını
// idempotent yazar. Duplicate zone'u (nginx -t "zone is already defined") önlemek için:
//   - Bilinen geçici mitigasyon dosyasını (00-girgincache-gecici.conf) kaldırır.
//   - Zone BAŞKA bir dosyada (nginx.conf / elle eklenmiş conf) zaten tanımlıysa kendi
//     dosyasını yazmaz/kaldırır (dış tanıma saygı gösterir).
//
// Bu fonksiyon her nginx -t ÖNCESİ çağrılır → self-heal / fail-safe.
// Dönüş: config'te bir değişiklik yapıldıysa changed=true.
func ensureCacheZone() (changed bool, err error) {
	// cache disk dizinini garanti et (nginx worker yazabilsin diye nginx sahipliği).
	_ = os.MkdirAll(cacheZoneDir, 0700)
	if uid, gid, e := uidGid("nginx"); e == nil {
		_ = os.Chown(cacheZoneDir, uid, gid)
	}
	_, _ = exec.Command("restorecon", "-R", cacheZoneDir).CombinedOutput()

	// Geçici mitigasyon dosyasını (varsa) kaldır — DUPLICATE zone riskini yok et.
	// Panel tek yönetilen dosyayı (girgincache.conf) kullanır.
	if _, e := os.Stat(cacheZoneTempConf); e == nil {
		if os.Remove(cacheZoneTempConf) == nil {
			changed = true
		}
	}

	// Zone başka bir yerde (bizim dosyamız hariç) tanımlı mı?
	if zoneDefinedElsewhere() {
		// Dışarıda tanım var → bizim dosyamız DUP yaratmasın; varsa kaldır.
		if _, e := os.Stat(cacheZoneConf); e == nil {
			if os.Remove(cacheZoneConf) == nil {
				changed = true
			}
		}
		return changed, nil
	}

	// İdempotent yaz: içerik zaten aynıysa dokunma.
	if cur, e := os.ReadFile(cacheZoneConf); e == nil && string(cur) == cacheZoneBody {
		return changed, nil
	}
	if e := os.WriteFile(cacheZoneConf, []byte(cacheZoneBody), 0644); e != nil {
		return changed, fmt.Errorf("girgincache zone conf yaz: %w", e)
	}
	_, _ = exec.Command("restorecon", cacheZoneConf).CombinedOutput()
	return true, nil
}

// zoneDefinedElsewhere: girgincache zone'unun bizim yönettiğimiz dosya DIŞINDA
// (nginx.conf veya başka bir conf.d/*.conf) tanımlı olup olmadığını döner.
func zoneDefinedElsewhere() bool {
	files := []string{"/etc/nginx/nginx.conf"}
	if extra, err := filepath.Glob("/etc/nginx/conf.d/*.conf"); err == nil {
		files = append(files, extra...)
	}
	for _, f := range files {
		if f == cacheZoneConf {
			continue // kendi dosyamızı sayma
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if zoneDefinedElsewhereRe.Match(b) {
			return true
		}
	}
	return false
}

type phpAyar struct {
	PoolDir string
	SockDir string
	Service string
	FpmBin  string // "php-fpm -t" ile pool config'ini reload ONCESI dogrulamak icin
}

var phpMap = map[string]phpAyar{
	"7.4": {PoolDir: "/etc/opt/remi/php74/php-fpm.d", SockDir: "/var/opt/remi/php74/run/php-fpm", Service: "php74-php-fpm", FpmBin: "/opt/remi/php74/root/usr/sbin/php-fpm"},
	"8.2": {PoolDir: "/etc/opt/remi/php82/php-fpm.d", SockDir: "/var/opt/remi/php82/run/php-fpm", Service: "php82-php-fpm", FpmBin: "/opt/remi/php82/root/usr/sbin/php-fpm"},
	"8.3": {PoolDir: "/etc/php-fpm.d", SockDir: "/run/php-fpm", Service: "php-fpm", FpmBin: "/usr/sbin/php-fpm"},
	"8.4": {PoolDir: "/etc/opt/remi/php84/php-fpm.d", SockDir: "/var/opt/remi/php84/run/php-fpm", Service: "php84-php-fpm", FpmBin: "/opt/remi/php84/root/usr/sbin/php-fpm"},
	"8.5": {PoolDir: "/etc/opt/remi/php85/php-fpm.d", SockDir: "/var/opt/remi/php85/run/php-fpm", Service: "php85-php-fpm", FpmBin: "/opt/remi/php85/root/usr/sbin/php-fpm"},
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

// vhost template — SSL var/yok her durumu kapsar.
//
// 🔴 GUVENLIK HEADER MIRAS SORUNU: nginx'te bir location KENDI add_header'ini
// tanimlarsa ust (server) seviyesindeki TUM add_header'lari DUSURUR (miras almaz).
// Bu yuzden guvenlik header blogu ({{.SecHeaders}}) server seviyesine EK OLARAK
// kendi add_header'i olan HER location'a (php, browser-cache) tekrar enjekte edilir.
// {{.DenyBlocks}} = CGI/betik yorumlayici + yedek/dump/hassas dosya erisim engeli.
// Her ikisi de renderAndReload icinde hesaplanip opts'a yazilir (DB'de tutulmaz).
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

    root {{.WebRoot}};
    index index.php index.html index.htm;
    # Sembolik baglanti saldirisi engeli: dosya, sahibi-farkli bir symlink uzerinden sunulmaz.
    disable_symlinks if_not_owner;

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

    # ---- Güvenlik header'ları (panel'den yönetilir; server seviyesi) ----
{{.SecHeaders}}
{{.DenyBlocks}}
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
        # Guvenlik header'lari — location kendi add_header'ini tanimladigi icin tekrar
{{.SecHeaders}}{{if .FastCgiCache}}        fastcgi_cache girgincache;
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
{{if .BrowserCache}}    # ---- Browser cache (statik + MESRU arsiv indirmeleri) ----
    # NOT: zip/gz MESRU; hassas .sql.gz deny blogunda (bu location'dan ONCE) yakalanir.
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|mp4|webm|pdf|zip|gz)$ {
        expires {{.BrowserCacheGun}}d;
        access_log off;
        add_header Cache-Control "public, immutable" always;
        # Guvenlik header'lari — location kendi add_header'ini tanimladigi icin tekrar
{{.SecHeaders}}    }
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
    # Sembolik baglanti saldirisi engeli: dosya, sahibi-farkli bir symlink uzerinden sunulmaz.
    disable_symlinks if_not_owner;

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

    # ---- Güvenlik header'ları (panel'den yönetilir; server seviyesi) ----
{{.SecHeaders}}
    location /.well-known/acme-challenge/ {
        root /var/www/_acme;
        auth_basic off;
        try_files $uri =404;
    }

{{.DenyBlocks}}
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
        # Guvenlik header'lari — location kendi add_header'ini tanimladigi icin tekrar
{{.SecHeaders}}{{if .FastCgiCache}}        fastcgi_cache girgincache;
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
{{if .BrowserCache}}    # ---- Browser cache (statik + MESRU arsiv indirmeleri) ----
    # NOT: zip/gz MESRU; hassas .sql.gz deny blogunda (bu location'dan ONCE) yakalanir.
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|mp4|webm|pdf|zip|gz)$ {
        expires {{.BrowserCacheGun}}d;
        access_log off;
        add_header Cache-Control "public, immutable" always;
        # Guvenlik header'lari — location kendi add_header'ini tanimladigi icin tekrar
{{.SecHeaders}}    }
{{end}}

    location ~ /\.(?!well-known) { deny all; }

{{if .EkDirektifler}}    # ---- Ek direktifler (kullanıcı) ----
    {{.EkDirektifler}}
{{end}}    # GirginOSPanel managed — {{.AlanAdi}} (HTTP only, PHP {{.PHPSurum}})
}
{{- end -}}
`))

// denyBlocksNginx: backend'den bagimsiz erisim engelleri. Regex location'lar
// tanim SIRASIYLA denenir; bu blok browser-cache/php location'larindan ONCE
// yerlestirilir (arsiv uzantilari onceligi burada kazanir).
const denyBlocksNginx = `    # ---- Yurutme engeli: CGI / betik yorumlayicilari ----
    location ~* \.(cgi|pl|py|sh|rb|lua|fcgi)$ { deny all; }
    # ---- Yedek / dump / hassas dosya engeli ----
    # NOT: MESRU arsivler (zip/gz/tar/tgz/tar.gz/rar/7z) + gzip'li sitemap (xml.gz)
    # engellenMEZ. Sadece hassas dosyalar: gzip'li SQL dump (sql.gz) HARIC tutulur.
    location ~* \.(sql|sql\.gz|bak|old|orig|save|swp|swo|dump|inc|log|php\.bak|php~|php\.save)$ { deny all; }
`

// buildSecurityHeaders: opts toggle'larina + SSL durumuna gore guvenlik add_header
// bloklarini uretir (her satir "always"). X-Frame-Options ve CSP-Report-Only DAIMA
// eklenir (yeni koruma, DB kolonu yok). HSTS + CSP-upgrade YALNIZ HTTPS'te uygulanir.
func buildSecurityHeaders(o VhostOpts) string {
	var b strings.Builder
	if o.HdrXContentType {
		b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	}
	// Clickjacking korumasi — daima (SAMEORIGIN: ayni-kaynak cerceveleme serbest).
	b.WriteString("    add_header X-Frame-Options \"SAMEORIGIN\" always;\n")
	if o.HdrXXSS {
		b.WriteString("    add_header X-XSS-Protection \"1; mode=block\" always;\n")
	}
	if o.HdrReferrer {
		b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n")
	}
	if o.HdrPermissions {
		b.WriteString("    add_header Permissions-Policy \"geolocation=(), microphone=(), camera=(), interest-cohort=()\" always;\n")
	}
	// Tenant CSP: GEVSEK Report-Only — musteri sitesini KIRMAZ (yalniz raporlar), her
	// kaynaga izin verir; sadece frame-ancestors 'self' ile gozlem/sinyal saglar.
	b.WriteString("    add_header Content-Security-Policy-Report-Only \"default-src 'self' https: http: data: blob: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'self';\" always;\n")
	if o.SSL() && o.HdrCSPUpgrade {
		b.WriteString("    add_header Content-Security-Policy \"upgrade-insecure-requests\" always;\n")
	}
	if o.SSL() && o.HdrHSTS {
		sd := ""
		if o.HSTSSubdomains {
			sd = "; includeSubDomains"
		}
		pl := ""
		if o.HSTSPreload {
			pl = "; preload"
		}
		fmt.Fprintf(&b, "    add_header Strict-Transport-Security \"max-age=%d%s%s\" always;\n", o.HSTSMaxAge, sd, pl)
	}
	return b.String()
}

// suspendedVhostTmpl — "Hesabı Askıya Al" için: acme-challenge hariç TÜM istekler 503.
// SSL sertifikası varsa 443'te de servis edilir (böylece askıdayken bile cert yenilenebilir).
var suspendedVhostTmpl = template.Must(template.New("s").Parse(`# {{.AlanAdi}} — GirginOSPanel tarafından ASKIYA ALINDI
server {
    listen 80;
    listen [::]:80;
    server_name {{.AlanAdi}} www.{{.AlanAdi}};

    location /.well-known/acme-challenge/ {
        root /var/www/_acme;
        auth_basic off;
        try_files $uri =404;
    }

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

    location / {
        return 503;
    }
    error_page 503 /_askida.html;
    location = /_askida.html {
        internal;
        default_type text/html;
        return 503 '<!doctype html><html lang="tr"><head><meta charset="utf-8"><title>Hesap Askıya Alındı</title><style>body{font-family:system-ui,sans-serif;background:#f8fafc;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}.k{max-width:520px;background:#fff;border:1px solid #e2e8f0;border-radius:16px;padding:48px;text-align:center}.l{width:48px;height:48px;background:#ea580c;border-radius:10px;margin:0 auto 20px;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:700;font-size:22px}h1{font-size:22px;color:#0f172a;margin:0 0 8px}p{color:#64748b;line-height:1.6}</style></head><body><div class="k"><div class="l">!</div><h1>Hesap Askıya Alındı</h1><p>Bu web sitesi geçici olarak askıya alınmıştır. Lütfen hizmet sağlayıcınız ile iletişime geçin.</p></div></body></html>';
    }
}
{{if .SSL}}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.AlanAdi}} www.{{.AlanAdi}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    access_log /var/log/nginx/{{.AlanAdi}}.access.log;
    error_log  /var/log/nginx/{{.AlanAdi}}.error.log warn;

    location / {
        return 503;
    }
    error_page 503 /_askida.html;
    location = /_askida.html {
        internal;
        default_type text/html;
        return 503 '<!doctype html><html lang="tr"><head><meta charset="utf-8"><title>Hesap Askıya Alındı</title><style>body{font-family:system-ui,sans-serif;background:#f8fafc;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}.k{max-width:520px;background:#fff;border:1px solid #e2e8f0;border-radius:16px;padding:48px;text-align:center}.l{width:48px;height:48px;background:#ea580c;border-radius:10px;margin:0 auto 20px;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:700;font-size:22px}h1{font-size:22px;color:#0f172a;margin:0 0 8px}p{color:#64748b;line-height:1.6}</style></head><body><div class="k"><div class="l">!</div><h1>Hesap Askıya Alındı</h1><p>Bu web sitesi geçici olarak askıya alınmıştır. Lütfen hizmet sağlayıcınız ile iletişime geçin.</p></div></body></html>';
    }
}
{{end}}`))

// phpPoolTmpl: per-tenant php-fpm pool. Guvenlik degerleri php_admin_value ile
// verilir → kullanici PHP kodu ini_set/php_value ile EZEMEZ. open_basedir dosya
// erisimini tenant home + /tmp ile sinirlar (baska tenant/sistem dosyalarini okuyamaz).
// disable_functions komut yurutme + surec kontrol + symlink fonksiyonlarini kapatir
// (mail acik birakilir; wp_mail vb. bozulmaz).
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
; ---- Guvenlik sertlestirmesi (php_admin_* → kullanici ini_set ile EZEMEZ) ----
php_admin_value[open_basedir] = /home/{{.User}}/:/tmp/
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,proc_close,proc_get_status,proc_terminate,proc_nice,pcntl_exec,dl,symlink,link,posix_kill,posix_mkfifo,posix_setpgid,posix_setsid,posix_setuid,posix_setgid
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

	// Askida true ise normal vhost yerine 503 "askıya alındı" vhost'u render edilir.
	Askida bool

	// Render-time hesaplanan alanlar (DB'de TUTULMAZ). renderAndReload icinde set edilir.
	SecHeaders string // guvenlik add_header blogu (her location'a enjekte edilir)
	DenyBlocks string // CGI/betik + yedek/dump dosya deny location'lari
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

// writePoolValidated: tenant php-fpm pool dosyasini (sertlestirilmis template ile)
// yazar, reload ONCESI `php-fpm -t` ile dogrular; gecersizse eski icerige GERI DONER
// (nginx/DNS'teki backup-rollback deseni). Basariliysa ilgili php-fpm servisini reload eder.
// Doner: aktif socket yolu + servis adi.
func writePoolValidated(sk, phpSurum string) (socket, service string, err error) {
	v := normalizePHP(phpSurum)
	ay := phpMap[v]
	poolPath, sock, svc := phpPoolPath(sk, v)

	_ = os.MkdirAll(filepath.Dir(poolPath), 0755)
	_ = os.MkdirAll(filepath.Dir(sock), 0755)

	var poolBuf bytes.Buffer
	if e := phpPoolTmpl.Execute(&poolBuf, map[string]string{"User": sk, "Socket": sock}); e != nil {
		return "", "", fmt.Errorf("pool template: %w", e)
	}

	// Fail-safe: eski icerigi yedekle; php-fpm -t patlarsa geri yukle (bu surumun
	// TUM pool'larini bozmayalim → diger tenant'lar da etkilenirdi).
	yedek, rerr := os.ReadFile(poolPath)
	yedekVar := rerr == nil
	if e := os.WriteFile(poolPath, poolBuf.Bytes(), 0644); e != nil {
		return "", "", fmt.Errorf("php pool yaz: %w", e)
	}
	if ay.FpmBin != "" {
		if out, e := exec.Command(ay.FpmBin, "-t").CombinedOutput(); e != nil {
			if yedekVar {
				_ = os.WriteFile(poolPath, yedek, 0644)
			} else {
				_ = os.Remove(poolPath)
			}
			return "", "", fmt.Errorf("php-fpm -t (%s) başarısız, pool geri alındı: %s: %w", v, strings.TrimSpace(string(out)), e)
		}
	}
	if out, e := exec.Command("systemctl", "reload-or-restart", svc).CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("php-fpm (%s) reload: %s: %w", svc, strings.TrimSpace(string(out)), e)
	}
	return sock, svc, nil
}

// renderAndReload: vhost'u yaz + nginx -t + reload (SSL var/yok aynı yol)
// Backend "apache" ise per-domain Apache vhost'unu da yazıp httpd'yi yeniden yükler.
// Backend değiştirildiyse eski Apache vhost dosyası temizlenir.
func renderAndReload(opts VhostOpts, sk string) error {
	// Default backend: php-fpm
	if opts.Backend == "" {
		opts.Backend = "php-fpm"
	}

	// Askıya-alma tutarlılığı: opts açıkça askıda demese bile DB'de bu kullanıcının
	// domaini askıdaysa, HER render'ı 503 vhost'u olarak zorla. Bu sayede SetPHP/SSL/
	// backend değişikliği gibi işlemler askıyı EZMEZ (Bug 3 kalıcılık garantisi).
	if !opts.Askida && pkgDB != nil {
		var ak int
		_ = pkgDB.QueryRow(`SELECT COALESCE(askida,0) FROM domains WHERE sistem_kullanici=?`, sk).Scan(&ak)
		if ak == 1 {
			opts.Askida = true
		}
	}

	// Guvenlik header + deny bloklarini her render'da hesapla (opts toggle'larina gore).
	opts.SecHeaders = buildSecurityHeaders(opts)
	opts.DenyBlocks = denyBlocksNginx

	var buf bytes.Buffer
	tmpl := vhostTmpl
	if opts.Askida {
		tmpl = suspendedVhostTmpl // askıdayken 503 vhost'u
	}
	if err := tmpl.Execute(&buf, opts); err != nil {
		return fmt.Errorf("template render: %w", err)
	}
	cfgPath := "/etc/nginx/conf.d/dom_" + sk + ".conf"
	// Fail-safe: bozuk config diske kalirsa sonraki nginx -t/reload TUM nginx'i dusurur.
	// Eski icerigi yedekle; nginx -t patlarsa geri yukle.
	yedek, rerr := os.ReadFile(cfgPath)
	yedekVar := rerr == nil
	if err := os.WriteFile(cfgPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("vhost yaz: %w", err)
	}
	// Fail-safe: vhost "fastcgi_cache girgincache" kullanabilir; zone tanımının
	// http-context'te mevcut olduğunu nginx -t ÖNCESİ garanti et. Aksi halde
	// "zone girgincache is unknown" → nginx -t patlar → suspend/unsuspend takılır.
	// (Sadece bu domain değil, cache etkin BAŞKA bir domain'in vhost'u da global
	// nginx -t'yi kırabildiği için her render'da garanti ediyoruz.)
	if _, err := ensureCacheZone(); err != nil {
		return err
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		if yedekVar {
			_ = os.WriteFile(cfgPath, yedek, 0644)
		} else {
			_ = os.Remove(cfgPath)
		}
		return fmt.Errorf("nginx -t başarısız: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Apache backend yönetimi (idempotent — yoksa yaz, varsa sil)
	// Askıdayken tüm istekler nginx'te 503 döner; Apache vhost'unu temizle.
	if opts.Backend == "apache" && !opts.Askida {
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
		_ = os.MkdirAll(filepath.Join(home, d), 0750)
	}

	uid, gid, err := uidGid(u)
	if err == nil {
		_ = filepath.Walk(home, func(p string, _ os.FileInfo, _ error) error {
			_ = os.Chown(p, uid, gid)
			return nil
		})
	}

	// public_html içeriği: dizinler 0750, dosyalar 0644 (nginx/php-fpm servis eder;
	// komşu tenant ev dizini seviyesinde zaten engellidir).
	_ = filepath.Walk(filepath.Join(home, "public_html"), func(p string, info os.FileInfo, _ error) error {
		if info == nil {
			return nil
		}
		if info.IsDir() {
			_ = os.Chmod(p, 0750)
		} else {
			_ = os.Chmod(p, 0644)
		}
		return nil
	})

	// 🔴 TENANT İZOLASYONU: komşu tenant'ın ev dizinini OKUYAMAMASI için "other"
	// bitleri kaldırılır; nginx/php-fpm'in servis edebilmesi için ev dizini + public_html
	// grubu 'nginx' yapılır. (open_basedir'e ek olarak OS seviyesinde çapraz-okuma engeli.)
	hardenHomePerms(home, uid)

	indexPath := filepath.Join(home, "public_html", "index.html")
	_ = os.WriteFile(indexPath, []byte(welcomeHTML(alanAdi)), 0644)
	if err == nil {
		_ = os.Chown(indexPath, uid, gid)
	}

	_, _ = exec.Command("restorecon", "-R", home).CombinedOutput()

	// PHP-FPM pool (php-fpm -t dogrulama + rollback ile)
	socket, _, perr := writePoolValidated(u, phpSurum)
	if perr != nil {
		return nil, perr
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
	// Subdomain vhost'ları (sub_<sk>_*.conf) da temizle — domain silinince bunlar
	// orphan kalıyordu; SSL'li bir subdomain vhost'u silinmiş cert'e referansla
	// nginx -t'yi GLOBAL kırıp yeni domain oluşturmayı bile engelliyordu.
	if subs, _ := filepath.Glob("/etc/nginx/conf.d/sub_" + sk + "_*.conf"); len(subs) > 0 {
		for _, s := range subs {
			_ = os.Remove(s)
		}
	}
	// Per-tenant FPM (Seçenek A) izlerini kaldır (servis + unit + config + run dizini + .bak).
	TeardownTenantFPM(sk)
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

	socket, _, perr := writePoolValidated(sk, yeniSurum)
	if perr != nil {
		return "", perr
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

// hardenHomePerms: tenant ev dizini + public_html için izolasyon izinlerini uygular.
//
//	home        = c_X:nginx 0710  → sahip tam; nginx grubu SADECE geçebilir (içerik
//	                                listeleyemez); other (komşu tenant) = hiçbir şey.
//	public_html = c_X:nginx 0750  → nginx okuyup servis eder; other = hiçbir şey.
//
// nginx grubu çözülemezse (kurulum farkı) fail-safe olarak eski davranışa (0711/0755,
// izole ama nginx erişimli) döner — servisi asla kırmaz. İçeriğe DOKUNMAZ → O(1), idempotent.
func hardenHomePerms(home string, uid int) {
	ph := filepath.Join(home, "public_html")
	if _, nginxGid, err := uidGid("nginx"); err == nil {
		_ = os.Chown(home, uid, nginxGid)
		_ = os.Chmod(home, 0o710)
		_ = os.Chown(ph, uid, nginxGid)
		_ = os.Chmod(ph, 0o750)
		return
	}
	log.Printf("hardenHomePerms: 'nginx' grubu bulunamadı, fail-safe 0711/0755 uygulanıyor (%s)", home)
	_ = os.Chmod(home, 0o711)
	_ = os.Chmod(ph, 0o755)
}

// HealHomePerms: MEVCUT tüm tenant ev dizinlerine (retroaktif) izolasyon izinlerini
// uygular. Yalnız iki dizinin sahip/mod'unu ayarlar (içeriği taramaz) → hızlı ve
// idempotent; her boot'ta güvenle çalışır. Init içinde çağrılır.
func HealHomePerms() {
	if pkgDB == nil {
		return
	}
	rows, err := pkgDB.Query(`SELECT DISTINCT sistem_kullanici FROM domains`)
	if err != nil {
		log.Printf("home-perms heal: sorgu: %v", err)
		return
	}
	var sks []string
	for rows.Next() {
		var sk string
		if rows.Scan(&sk) == nil && strings.HasPrefix(sk, "c_") {
			sks = append(sks, sk)
		}
	}
	_ = rows.Close()

	n := 0
	for _, sk := range sks {
		home := "/home/" + sk
		if fi, e := os.Stat(home); e != nil || !fi.IsDir() {
			continue
		}
		uid, _, e := uidGid(sk)
		if e != nil {
			continue
		}
		hardenHomePerms(home, uid)
		n++
	}
	if n > 0 {
		log.Printf("home-perms heal: %d tenant ev dizini izolasyon izinleriyle güncellendi (home 0710 / public_html 0750, grp nginx)", n)
	}
}

// SuspendUserRuntime: askıya alınan tenant'ın çalışan süreçlerini ve crontab'ını yönetir.
//
//	suspend=true  → crontab'ı cron spool'undan çıkar (crond çalıştırmaz) + tüm tenant
//	                süreçlerini öldür (php-fpm worker, cron-spawn script).
//	suspend=false → crontab'ı geri getir.
//
// c_ prefix ZORUNLU (sistem/root user'a asla dokunma). Best-effort.
func SuspendUserRuntime(sk string, suspend bool) {
	if !strings.HasPrefix(sk, "c_") {
		return // güvenlik: yalnız tenant user
	}
	const suspendStore = "/var/lib/girginospanel/cron-suspended"
	cronSpool := "/var/spool/cron/" + sk
	stored := filepath.Join(suspendStore, sk)

	if suspend {
		if _, err := os.Stat(cronSpool); err == nil {
			_ = os.MkdirAll(suspendStore, 0o700)
			if err := os.Rename(cronSpool, stored); err != nil {
				log.Printf("suspend runtime: crontab taşıma (%s): %v", sk, err)
			}
		}
		// Tenant'ın çalışan tüm süreçlerini öldür (yalnız bu uid). Eşleşme yoksa
		// pkill exit!=0 döner → yok say.
		_, _ = exec.Command("pkill", "-KILL", "-u", sk).CombinedOutput()
		return
	}
	if _, err := os.Stat(stored); err == nil {
		_ = os.MkdirAll("/var/spool/cron", 0o700)
		if err := os.Rename(stored, cronSpool); err != nil {
			log.Printf("suspend runtime: crontab geri getirme (%s): %v", sk, err)
			return
		}
		_ = os.Chmod(cronSpool, 0o600)
		_, _ = exec.Command("restorecon", cronSpool).CombinedOutput()
	}
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
	var askida int
	if err := db.QueryRow(
		`SELECT alan_adi, sistem_kullanici, COALESCE(cert_path,''), COALESCE(key_path,''), COALESCE(ssl_kaynak,''), COALESCE(web_backend,'php-fpm'), COALESCE(askida,0)
		 FROM domains WHERE id=?`, domainID).
		Scan(&alanAdi, &sk, &certPath, &keyPath, &sslKaynak, &backend, &askida); err != nil {
		return fmt.Errorf("domain bilgi cek: %w", err)
	}
	// 🔴 Per-tenant FPM (Seçenek A) aktifse socket'i DAİMA per-tenant socket'e zorla —
	// çağıran hangi socket'i geçerse geçsin (paylaşılan/heal socket'i) nginx doğru
	// FPM'e bağlanır. Böylece SetPHP/SSL/suspend/korumalı-dizin render'ları per-tenant
	// tenant'ta 502 üretmez.
	if TenantFPMActive(sk) {
		socket = tenantSocket(sk)
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
		Askida:          askida == 1, // askıdaysa her render'da 503 vhost'u tekrar uygulanır
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

// RerenderVhost: domainID için socket'i çözerek vhost'u yeniden render eder.
// Askıya al / askıdan al sonrası çağrılır; askıda durumu DB'den okunur.
func RerenderVhost(db *sql.DB, domainID int64) error {
	var sk, php string
	if err := db.QueryRow(`SELECT sistem_kullanici, php_surum FROM domains WHERE id=?`, domainID).Scan(&sk, &php); err != nil {
		return err
	}
	socket, err := PHPSocketFor(sk, php)
	if err != nil {
		// askıda vhost'u PHP gerektirmez; socket çözülemese bile 503 vhost'u yazılabilir
		socket = "/run/php-fpm/" + sk + ".sock"
	}
	return ApplyVhostForDomain(db, domainID, socket, php)
}

// PHPSocketFor: nginx vhost'un fastcgi_pass etmesi gereken socket. Per-tenant FPM
// (Seçenek A) aktifse onun izole socket'i; değilse paylaşılan master socket'i.
// Böylece TÜM çağıranlar (subdomain/sifrekoruma/nginxset/handlers) otomatik olarak
// doğru socket'i alır — cutover sonrası ayrı edit gerekmez.
func PHPSocketFor(sk, surum string) (string, error) {
	if TenantFPMActive(sk) {
		return tenantSocket(sk), nil
	}
	return sharedSocketPath(sk, surum)
}

// sharedSocketPath: paylaşılan php-fpm master socket yolu (per-tenant'tan bağımsız).
// EnableTenantFPM/RollbackToSharedFPM içinde doğrudan kullanılır (recursion olmasın).
func sharedSocketPath(sk, surum string) (string, error) {
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

// ---------------------------------------------------------------------------
// Batch2 sertlestirme heal'leri (retroaktif) — Init'ten cagrilir.
// ---------------------------------------------------------------------------

// vhostHardenSentinel: retroaktif vhost/pool re-render'inin YALNIZ BIR KEZ
// calismasini garanti eden isaret dosyasi. Silinirse bir sonraki panel restart'inda
// re-render tekrar calisir (elle yeniden tetikleme).
const vhostHardenSentinel = "/var/lib/girginospanel/.vhost_hardening_v2_done"

// HealVhostsOnStartup: MEVCUT tum domainlerin php-fpm pool + nginx vhost'unu YENI
// (sertlestirilmis) template ile bir kez yeniden render eder. Template degisikligi
// yalniz yeni-render'i etkiledigi icin, eski domainler bu heal olmadan sertlesmezdi.
//
// Guvenlik/kirilmazlik:
//   - Her pool writePoolValidated ile (php-fpm -t + rollback) yazilir.
//   - Her vhost ApplyVhostForDomain → renderAndReload ile (nginx -t + rollback) yazilir.
//   - Tek bir domain hata verse bile digerlerine DEVAM edilir; hatali domain ESKI
//     (calisan) haline birakilir → tum nginx asla kirilmaz.
//   - Sentinel ile idempotent (tekrar tekrar tum filoyu render etmez).
func HealVhostsOnStartup() {
	if pkgDB == nil {
		return
	}
	if _, err := os.Stat(vhostHardenSentinel); err == nil {
		return // zaten calisti
	}

	rows, err := pkgDB.Query(`SELECT id, sistem_kullanici, php_surum FROM domains`)
	if err != nil {
		log.Printf("vhost heal: domain listesi okunamadi (atlandi): %v", err)
		return
	}
	type dom struct {
		id  int64
		sk  string
		php string
	}
	var list []dom
	for rows.Next() {
		var d dom
		if scanErr := rows.Scan(&d.id, &d.sk, &d.php); scanErr == nil {
			list = append(list, d)
		}
	}
	rows.Close()

	var ok, fail int
	for _, d := range list {
		var socket string
		if TenantFPMActive(d.sk) {
			// Per-tenant FPM (Seçenek A) aktif: paylaşılan pool'a DOKUNMA (yoksa .bak'a
			// aldığımız pool'u geri yaratıp çakışan ikinci bir master kurardık). Socket
			// per-tenant socket'tir; servis EnsureTenantFPMOnStartup ile ayakta tutulur.
			socket = tenantSocket(d.sk)
		} else {
			// 1) pool'u yeni template ile yeniden yaz (php-fpm -t + rollback iceride).
			s, _, perr := writePoolValidated(d.sk, d.php)
			if perr != nil {
				log.Printf("vhost heal: %s pool yeniden yazilamadi (vhost yine denenecek): %v", d.sk, perr)
				if ps, e := sharedSocketPath(d.sk, d.php); e == nil {
					s = ps
				}
			}
			socket = s
			if socket == "" {
				socket = "/run/php-fpm/" + d.sk + ".sock"
			}
		}
		// 2) vhost'u yeni template ile yeniden render et (nginx -t + rollback iceride).
		if aerr := ApplyVhostForDomain(pkgDB, d.id, socket, d.php); aerr != nil {
			log.Printf("vhost heal: %s vhost yeniden render HATA (eski hali korundu): %v", d.sk, aerr)
			fail++
			continue
		}
		ok++
	}
	log.Printf("vhost heal: retroaktif sertlestirme tamam — %d ok / %d hata (toplam %d domain)", ok, fail, len(list))

	_ = os.MkdirAll(filepath.Dir(vhostHardenSentinel), 0755)
	if e := os.WriteFile(vhostHardenSentinel, []byte("done\n"), 0644); e != nil {
		log.Printf("vhost heal: sentinel yazilamadi (bir sonraki boot'ta tekrar calisir): %v", e)
	}
}

// panelVhostPath: kurulu panel nginx vhost'u (installer tarafindan yazilir).
const panelVhostPath = "/etc/nginx/conf.d/_panel.conf"

// panelSecSentinel: panel vhost'una guvenlik header'lari enjekte edildiginde eklenen
// isaret satiri. Idempotency icin (iki kez eklemeyi onler).
const panelSecSentinel = "# GOSP-PANEL-SEC v2"

// HealPanelVhostHeadersOnStartup: kurulu panel vhost'una (yoksa sessiz gecer)
// guvenlik header'larini SERVER seviyesinde ekler. Panel React SPA (location /) ve
// phpMyAdmin PHP location'lari kendi add_header'i olmadigi icin bu server-seviyesi
// header'lari MIRAS ALIR. Enjeksiyon SADECE add_header satirlaridir (istek yonlendirmesini
// degistirmez) ve nginx -t + rollback ile korunur → admin kilitlenmesi riski minimum.
func HealPanelVhostHeadersOnStartup() {
	orig, err := os.ReadFile(panelVhostPath)
	if err != nil {
		return // panel vhost yok (bu host'ta panel kurulu degil) — sessiz gec
	}
	s := string(orig)
	if strings.Contains(s, panelSecSentinel) {
		return // zaten sertlestirilmis
	}
	anchor := "server_name _;"
	idx := strings.Index(s, anchor)
	if idx < 0 {
		log.Printf("panel sec heal: '%s' capasi bulunamadi, atlandi", anchor)
		return
	}
	// Panel CSP: SIKI ama kendini-barindiran SPA + phpMyAdmin icin uyumlu
	// (script/style 'unsafe-inline'/'unsafe-eval' — pma satir-ici script kullanir).
	hdrs := "\n    " + panelSecSentinel + "\n" +
		"    add_header X-Content-Type-Options \"nosniff\" always;\n" +
		"    add_header X-Frame-Options \"SAMEORIGIN\" always;\n" +
		"    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n" +
		"    add_header Permissions-Policy \"geolocation=(), microphone=(), camera=(), interest-cohort=()\" always;\n" +
		"    add_header Content-Security-Policy \"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; img-src 'self' data: blob:; font-src 'self' data: https://fonts.gstatic.com; connect-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'\" always;\n" +
		"    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;\n"

	insertAt := idx + len(anchor)
	newS := s[:insertAt] + hdrs + s[insertAt:]

	if e := os.WriteFile(panelVhostPath, []byte(newS), 0644); e != nil {
		log.Printf("panel sec heal: yazilamadi: %v", e)
		return
	}
	if out, e := exec.Command("nginx", "-t").CombinedOutput(); e != nil {
		_ = os.WriteFile(panelVhostPath, orig, 0644) // GERI YUKLE
		log.Printf("panel sec heal: nginx -t basarisiz, geri alindi: %s", strings.TrimSpace(string(out)))
		return
	}
	if out, e := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); e != nil {
		log.Printf("panel sec heal: nginx reload: %s", strings.TrimSpace(string(out)))
		return
	}
	log.Printf("panel sec heal: guvenlik header'lari eklendi + nginx reload OK")
}
