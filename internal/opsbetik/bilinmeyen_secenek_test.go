package opsbetik

// Yönetici betikleri bilinmeyen seçeneği REDDETMELİ (çıkış 2). Eskiden yok sayılıp iş
// yapılıyordu: "--dryrun" gibi bir yazım hatası gerçek değişiklik yapardı.
//
// GÜVENLİ KOŞU: GOSP_SECENEK_TEST=1 ile betik seçenek denetiminden HEMEN SONRA 0 ile
// çıkar; PATH'te yalnız cat. Koruma kaldırılırsa betik iş yapmadan 0 döner ve test düşer
// (ayrıcalık düşürmek bu test ortamında mümkün değil; yalnız boş PATH de yetmezdi:
// `echo … > /etc/…` kabuk iç komutudur).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpsBetik_BilinmeyenSecenekReddedilir(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash yok")
	}
	bos := t.TempDir()
	// Yalnız cat (yardım metni için; set -e'li betikte eksikliği 127 verir) — yan etkisiz.
	if c, err := exec.LookPath("cat"); err == nil {
		_ = os.Symlink(c, filepath.Join(bos, "cat"))
	}
	for _, d := range []struct {
		ad      string
		gecerli []string
	}{
		{"girginospanel-wp-onkosul", []string{"--dry-run", "--ayrinti"}},
		{"girginospanel-ioncube", []string{"--force", "--restart"}},
		{"girginospanel-redis-setup", nil},
		{"girginospanel-db-backup", nil},
	} {
		yol, _ := filepath.Abs(filepath.Join("../../assets/ops", d.ad))
		kos := func(arg ...string) (int, string) {
			cmd := exec.Command(bash, append([]string{yol}, arg...)...)
			cmd.Env = []string{"PATH=" + bos, "HOME=" + bos, "LC_ALL=C", "GOSP_SECENEK_TEST=1"}
			cmd.Dir = bos
			out, err := cmd.CombinedOutput()
			kod := 0
			if ee, ok := err.(*exec.ExitError); ok {
				kod = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("%s: %v", d.ad, err)
			}
			return kod, string(out)
		}
		for _, kotu := range [][]string{{"--bogus"}, {"--dryrun"}, append(append([]string{}, d.gecerli...), "--bogus")} {
			if kod, out := kos(kotu...); kod != 2 || !strings.Contains(out, "unknown option:") {
				t.Errorf("%s %v: çıkış %d, çıktı %q (beklenen 2 + 'unknown option')", d.ad, kotu, kod, out)
			}
		}
		if kod, _ := kos("--help"); kod != 0 {
			t.Errorf("%s --help: çıkış %d, beklenen 0", d.ad, kod)
		}
		if kod, out := kos(d.gecerli...); kod != 0 {
			t.Errorf("%s %v (geçerli seçenekler): çıkış %d %q", d.ad, d.gecerli, kod, out)
		}
	}
}
