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
