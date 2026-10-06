package opsbetik

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// SELinux MODU kurulum/güncelleme tarafından ASLA değiştirilmez; yalnız raporlanır
// (kurallar/etiketler her modda yazılır). Bu test hem "mod bildirimi var" hem "mod
// değiştiren komut yok" değişmezini kilitler.
func TestSELinuxModuBildirilirDegistirilmez(t *testing.T) {
	kok := filepath.Join("..", "..")
	dosyalar := []string{
		filepath.Join(kok, "girginospanel-install.sh"),
		filepath.Join(kok, "assets", "ops", "girginospanel-update"),
	}
	degistirir := regexp.MustCompile(`(^|[;&|]\s*|\s)(setenforce\s+[01A-Za-z]|sed\s+-i[^\n]*SELINUX=|grubby[^\n]*selinux=0)`)
	for _, d := range dosyalar {
		b, err := os.ReadFile(d)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "getenforce") || !strings.Contains(s, "Permissive") {
			t.Errorf("%s: SELinux mod bildirimi (getenforce + Permissive dalı) yok", d)
		}
		for i, satir := range strings.Split(s, "\n") {
			ts := strings.TrimSpace(satir)
			if strings.HasPrefix(ts, "#") || strings.HasPrefix(ts, "echo") || strings.HasPrefix(ts, "ok ") ||
				strings.HasPrefix(ts, "warn ") || strings.Contains(ts, "echo -e") || strings.Contains(ts, `warn "`) || strings.Contains(ts, `ok "`) {
				continue // bildirim metni, komut değil
			}
			if degistirir.MatchString(satir) {
				t.Errorf("%s:%d SELinux modunu değiştiren komut: %s", d, i+1, ts)
			}
		}
	}
}
