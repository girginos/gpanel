// Apache backend için per-domain vhost yönetimi.
// nginx önde TLS terminator + edge proxy, Apache 127.0.0.1:10080'de
// vhost'larını dinler, PHP'yi mevcut PHP-FPM socket'ine mod_proxy_fcgi ile köprülemek için.
package provisioner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
        # Yedek / dump / hassas dosya erisimini engelle (403)
        <FilesMatch "\.(sql|bak|old|orig|save|swp|dump|tar|tgz|gz|zip|rar|7z|log|inc)$">
            Require all denied
        </FilesMatch>
        <FilesMatch "\.php\.bak$">
            Require all denied
        </FilesMatch>
    </Directory>

    <FilesMatch "\.php$">
        SetHandler "proxy:unix:{{.PHPSocket}}|fcgi://localhost"
    </FilesMatch>

    DirectoryIndex index.php index.html index.htm

    # Gerçek istemci IP'sini nginx'ten al
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
