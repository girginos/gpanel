// Apache backend için per-domain vhost yönetimi.
// nginx önde TLS terminator + edge proxy, Apache 127.0.0.1:10080'de
// vhost'larını dinler, PHP'yi mevcut PHP-FPM socket'ine mod_proxy_fcgi ile köprülemek için.
package provisioner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"text/template"
)

const ApacheUpstream = "127.0.0.1:10080"

var apacheVhostTmpl = template.Must(template.New("a").Parse(`# {{.AlanAdi}} — GirginOSPanel Apache backend (nginx ön proxy)
# Guvenlik notu: yanit guvenlik header'lari EDGE'de (nginx) uygulanir (cift-header
# olmamasi icin). Apache katmani YURUTME/ERISIM politikasini uygular: CGI kapali,
# betik dosyalari + yedek/dump dosyalari reddedilir, symlink yalniz sahip-eslesirse.
<VirtualHost 127.0.0.1:10080>
    ServerName {{.AlanAdi}}
    ServerAlias www.{{.AlanAdi}}
    DocumentRoot {{.WebRoot}}

    <Directory {{.WebRoot}}>
        # CGI calistirma KAPALI; dizin listeleme KAPALI; symlink yalniz sahip-eslesirse.
        # .htaccess bu Options'lari GERI ACAMAZ (AllowOverride yalniz Indexes,MultiViews izin verir).
        Options -ExecCGI -Indexes -Includes -FollowSymLinks +SymLinksIfOwnerMatch
        AllowOverride AuthConfig FileInfo Indexes Limit Options=Indexes,MultiViews
        Require all granted

        # CGI / betik yorumlayici handler'larini kaldir + dogrudan erisimi engelle (403)
        RemoveHandler .cgi .pl .py .sh .rb .lua .fcgi .fpl
        <FilesMatch "\.(cgi|pl|py|sh|rb|lua|fcgi)$">
            Require all denied
        </FilesMatch>
        # Yedek / dump / hassas dosya erisimini engelle (403).
        # NOT: MESRU arsivler (zip/gz/tar/tgz/rar/7z) engellenMEZ; gzip'li SQL dump
        # (sql.gz) hassas oldugu icin HARIC tutulur (sitemap.xml.gz vb. serbest).
        <FilesMatch "\.(sql|sql\.gz|bak|old|orig|save|swp|swo|dump|inc|log)$">
            Require all denied
        </FilesMatch>
        <FilesMatch "\.(php\.bak|php~|php\.save)$">
            Require all denied
        </FilesMatch>
    </Directory>

    <FilesMatch "\.php$">
        SetHandler "proxy:unix:{{.PHPSocket}}|fcgi://localhost"
    </FilesMatch>

    DirectoryIndex index.php index.html index.htm

    # Gerçek istemci IP'sini nginx'ten al
    # 🔴 EDGE ŞEMASI: TLS nginx'te sonlanıyor; Apache'ye düz HTTP geliyor.
    # Bu satır olmadan PHP'de $_SERVER['HTTPS'] BOŞ kalır → WordPress/Laravel
    # kendini HTTP sanıp http:// URL üretir ve HTTPS'e sonsuz yönlendirme yapar
    # (Cloudflare arkasında ERR_TOO_MANY_REDIRECTS). FPM modunda karşılığı
    # fastcgi_param HTTPS on olarak zaten var; Apache modunda eksikti.
    SetEnvIf X-Forwarded-Proto "^https$" HTTPS=on
    # Kanonik URL'lerde ":10080" sızmasın (uygulama SERVER_PORT'a bakarsa).
    UseCanonicalName Off
    RemoteIPHeader X-Forwarded-For
    RemoteIPInternalProxy 127.0.0.1

    ErrorLog /var/log/httpd/{{.AlanAdi}}.error.log
    CustomLog /var/log/httpd/{{.AlanAdi}}.access.log combined
</VirtualHost>
`))

func apacheVhostPath(sk string) string {
	return "/etc/httpd/conf.d/dom_" + sk + ".conf"
}

func writeApacheVhost(opts VhostOpts, sk string) error {
	apacheDefaultEnsure() // eslesmeyen Host ilk siteye dusmesin
	var buf bytes.Buffer
	if err := apacheVhostTmpl.Execute(&buf, opts); err != nil {
		return fmt.Errorf("apache template: %w", err)
	}
	if err := os.WriteFile(apacheVhostPath(sk), buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("apache vhost yaz: %w", err)
	}
	return apacheTestReload()
}

func deleteApacheVhostIfExists(sk string) error {
	p := apacheVhostPath(sk)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("apache vhost sil: %w", err)
	}
	return apacheTestReload()
}

func apacheTestReload() error {
	if out, err := exec.Command("httpd", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("httpd -t başarısız: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", "httpd").CombinedOutput(); err != nil {
		return fmt.Errorf("httpd reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// apacheKullanicisiVar — sistemde "apache" kullanıcısı tanımlı mı (httpd kurulu mu).
// 🔴 NEDEN: FPM poola içinde VAR OLMAYAN bir kullanıcı yazılırsa

// apacheKullanicisiVar — sistemde "apache" kullanicisi tanimli mi (httpd kurulu mu).
// NEDEN: FPM pool'una `listen.acl_users` icinde VAR OLMAYAN kullanici yazilirsa
// php-fpm BASLAMAZ; setfacl de hata doner. Apache'ye erisim acan her yer once bunu sorar.
func apacheKullanicisiVar() bool {
	_, err := user.Lookup("apache")
	return err == nil
}

// aclSatiri — FPM pool sablonlarina konan listen.acl_users satiri (apache yoksa BOS).
func aclSatiri() string {
	if apacheKullanicisiVar() {
		return "listen.acl_users = nginx,apache"
	}
	return ""
}

// apacheDefaultEnsure: 10080 icin "her seyi reddet" DEFAULT vhost.
// Apache ilk yuklenen vhost'u default sayar; bu dosya olmadan eslesmeyen Host
// header'i ilk sitenin icerigine duser (cross-site sizinti).
func apacheDefaultEnsure() {
	_ = os.MkdirAll("/var/www/_gosp_apache_default", 0o755)
	yol := "/etc/httpd/conf.d/00-gosp-default.conf"
	icerik := `# GirginOSPanel — 10080 DEFAULT vhost (otomatik). 00- prefix: dom_*.conf'tan ONCE yuklenir.
# NEDEN: Apache'de ilk yuklenen vhost "default"tur; bu dosya olmadan eslesmeyen HER Host
# ilk sitenin icerigini gorur (cross-site sizinti). Burasi her seyi REDDEDER.
<VirtualHost 127.0.0.1:10080>
    ServerName gosp-default.invalid
    DocumentRoot /var/www/_gosp_apache_default
    <Directory />
        AllowOverride None
        Require all denied
    </Directory>
    ErrorLog /var/log/httpd/gosp-default.error.log
</VirtualHost>
`
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), "gosp-default.invalid") {
		return
	}
	_ = os.WriteFile(yol, []byte(icerik), 0o644)
}
