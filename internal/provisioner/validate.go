package provisioner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ValidateNginxDirectives, plan/domain seviyesinde girilen serbest nginx
// direktiflerini CANLI yapılandırmayı bozmadan doğrular:
//   - direktifleri geçici bir server{} bloğuna gömer (/etc/nginx/conf.d altında),
//   - `nginx -t` çalıştırır (tüm konfigürasyonu parse+doğrular ama socket AÇMAZ),
//   - geçici dosyayı her durumda siler.
//
// Geçersizse nginx'in kendi hata çıktısını döndürür; çağıran bu hatayı kullanıcıya
// gösterip kaydı REDDEDER. Boş girdi geçerli sayılır.
//
// Not: direktifler, gerçek domain vhost'unda da server bloğuna enjekte edildiği
// için doğrulama server context'inde yapılır (per-domain ek_direktifler ile aynı).
func ValidateNginxDirectives(direktifler string) error {
	d := strings.TrimSpace(direktifler)
	if d == "" {
		return nil
	}

	tmp, err := os.CreateTemp("/etc/nginx/conf.d", "_planvalidate_*.conf.tmp")
	if err != nil {
		return fmt.Errorf("geçici doğrulama dosyası oluşturulamadı: %w", err)
	}
	tmpPath := tmp.Name()
	// nginx yalnızca *.conf dosyalarını okur; ".tmp" uzantısı doğrulamaya
	// katılmaz. Bu yüzden gerçek ".conf" adına taşıyoruz.
	finalPath := strings.TrimSuffix(tmpPath, ".tmp")

	block := fmt.Sprintf(`# GirginOSPanel geçici plan direktif doğrulaması — otomatik silinir
server {
    listen 127.0.0.1:65071;
    server_name _gosp_plan_validate;
    root /var/www/_default80;
    # ---- doğrulanan direktifler ----
%s
}
`, indentLines(d, "    "))

	if _, err := tmp.WriteString(block); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("geçici doğrulama dosyası yazılamadı: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("geçici doğrulama dosyası hazırlanamadı: %w", err)
	}
	defer os.Remove(finalPath)

	out, err := exec.Command("nginx", "-t").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// Kullanıcıya gösterilecek mesajdan geçici dosya yolunu sadeleştir.
		msg = strings.ReplaceAll(msg, finalPath, "(direktifler)")
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

var nginxYasakDirektif = map[string]bool{
	"alias": true, "root": true,
	"proxy_pass": true, "fastcgi_pass": true, "uwsgi_pass": true, "scgi_pass": true,
	"grpc_pass": true, "memcached_pass": true,
	"include": true, "load_module": true,
	"ssl_certificate": true, "ssl_certificate_key": true, "ssl_trusted_certificate": true,
	"error_log": true, "access_log": true, "fastcgi_param": true,
	"auth_basic_user_file": true, "secure_link_secret": true,
}

// TehlikeliNginxDirektifi: tenant ek_direktifler icinde yasak (LFD/SSRF/RCE) bir
// direktif varsa adini, yoksa "" doner. Direktif adi = ; { } sonrasi ilk token.
func TehlikeliNginxDirektifi(direktifler string) string {
	var nc strings.Builder
	for _, line := range strings.Split(direktifler, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		nc.WriteString(line)
		nc.WriteByte('\n')
	}
	repl := strings.NewReplacer("{", "\n", "}", "\n", ";", "\n")
	for _, stmt := range strings.Split(repl.Replace(nc.String()), "\n") {
		f := strings.Fields(stmt)
		if len(f) == 0 {
			continue
		}
		name := strings.ToLower(f[0])
		if nginxYasakDirektif[name] ||
			strings.Contains(name, "_by_lua") ||
			strings.HasPrefix(name, "lua_") ||
			strings.HasPrefix(name, "js_") ||
			strings.HasPrefix(name, "perl") {
			return name
		}
	}
	return ""
}

// indentLines her satırın başına prefix ekler (nginx bloğu okunabilirliği için).
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
