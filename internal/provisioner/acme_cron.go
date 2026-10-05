package provisioner

// acme.sh yenileme cron'u — TEK kaynak, kalıcı, idempotent.
//
// 🔴 Sorun (181'de ölçüldü): root crontab'ında `acme.sh --cron --config-home /opt/girginospanel/acme`
// vardı (panelhost `--install-cronjob`'unun yazdığı). Panelin alan/alt alan/ek alan/posta
// sertifika kayıtları ise /var/lib/girginospanel/acme altında; açık --config-home bayrağı
// account.conf'taki CONFIG_HOME'u (acmeCronHizala) geçersiz kıldığından bu kayıtlar HİÇ
// yenilenmiyordu (test1/test2 yenileme zamanı geçmişti).
//
// Çözüm: /etc/cron.d/girginospanel-acme → girginospanel-acme-yenile sarmalayıcısı; her config-home
// için ayrı `acme.sh --cron --config-home <dizin>` (biri patlarsa diğeri engellenmez), çıktı
// `logger -t girginospanel-acme`. Eski panel/acme.sh imzalı crontab satırı kaldırılır (çifte
// yenileme olmasın); kullanıcının başka satırlarına ve başka config-home'lu acme satırlarına
// DOKUNULMAZ. panelhost artık `--install-cronjob` ÇAĞIRMAZ (satırı geri yazardı) → AcmeCronKur.

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed acmecron/girginospanel-acme-yenile
var acmeYenileBetik []byte

const (
	acmeYenileYol = "/usr/local/bin/girginospanel-acme-yenile"
	acmeCronYol   = "/etc/cron.d/girginospanel-acme"
	// Panelin bildiği config-home'lar: crontab'taki satır bunlardan birine (ya da hiçbirine =
	// acme.sh varsayılanı) bağlıysa panel imzalıdır.
	acmePanelHome2 = "/opt/girginospanel/acme"
)

// Dikişler (testler geçici dizin + bellek içi crontab ile koşar).
var (
	acmeBetikYolu  = acmeYenileYol
	acmeCronDosya  = acmeCronYol
	acmeCrontabOku = func() (string, error) {
		out, err := exec.Command("crontab", "-l").CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "no crontab") {
				return "", nil
			}
			return "", fmt.Errorf("crontab -l: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	acmeCrontabYaz = func(icerik string) error {
		c := exec.Command("crontab", "-")
		c.Stdin = strings.NewReader(icerik)
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("crontab -: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	acmeEtiketle = func(yol string) { _, _ = exec.Command("restorecon", yol).CombinedOutput() }
)

// acmeCronIcerik: /etc/cron.d/girginospanel-acme içeriği (sabit; 6 saatte bir).
func acmeCronIcerik() string {
	return "# OTOMATİK — panel yönetir (provisioner.AcmeCronKur); elle değişiklik her panel açılışında geri alınır.\n" +
		"# acme.sh yenilemesi: her config-home için ayrı çağrı + günlük (journalctl -t girginospanel-acme).\n" +
		"SHELL=/bin/bash\nPATH=/usr/local/bin:/usr/bin:/bin\n" +
		"26 3,9,15,21 * * * root " + acmeBetikYolu + " >/dev/null 2>&1\n"
}

// acmePanelCronSatiri: crontab satırı panelin/acme.sh'ın kendi yenileme satırı mı?
// Yorum satırları ve başka config-home'a bağlı (kullanıcıya ait) satırlar HAYIR.
func acmePanelCronSatiri(satir string) bool {
	s := strings.TrimSpace(satir)
	if s == "" || strings.HasPrefix(s, "#") {
		return false
	}
	if !strings.Contains(s, "acme.sh") || !strings.Contains(s, "--cron") {
		return false
	}
	alanlar := strings.Fields(s)
	for i, a := range alanlar {
		if a == "--config-home" || a == "--config-home=" {
			if i+1 >= len(alanlar) {
				return false
			}
			d := strings.TrimRight(alanlar[i+1], "/")
			return d == acmeConfigHome || d == acmePanelHome2
		}
		if strings.HasPrefix(a, "--config-home=") {
			d := strings.TrimRight(strings.TrimPrefix(a, "--config-home="), "/")
			return d == acmeConfigHome || d == acmePanelHome2
		}
	}
	return true // --config-home yok → acme.sh varsayılan (kurulumun kendi) satırı
}

// acmeCrontabTemizle: panel imzalı acme satırlarını çıkarır, geri kalan her şeyi AYNEN korur.
func acmeCrontabTemizle(in string) (string, int) {
	if in == "" {
		return in, 0
	}
	var cikti []string
	silinen := 0
	for _, l := range strings.Split(in, "\n") {
		if acmePanelCronSatiri(l) {
			silinen++
			continue
		}
		cikti = append(cikti, l)
	}
	return strings.Join(cikti, "\n"), silinen
}

// acmeAtomikYaz: içerik farklıysa temp+rename ile yazar; değiştiyse true.
func acmeAtomikYaz(yol string, b []byte, mode os.FileMode) (bool, error) {
	if mev, err := os.ReadFile(yol); err == nil && bytes.Equal(mev, b) {
		if st, e := os.Stat(yol); e == nil && st.Mode().Perm() == mode {
			return false, nil
		}
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, yol); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// AcmeCronKur: sarmalayıcıyı + cron.d dosyasını garanti eder, eski panel/acme.sh crontab
// satırını kaldırır. Idempotent; sıra önemli: önce yeni yenileme yolu kurulur, SONRA eskisi
// kaldırılır (hiçbir an yenileme güvencesiz kalmaz). acme.sh kurulu değilse hiçbir şey yapmaz.
func AcmeCronKur() error {
	if _, err := os.Stat("/root/.acme.sh/acme.sh"); err != nil && acmeBetikYolu == acmeYenileYol {
		return nil // acme.sh kurulu değil
	}
	if err := os.MkdirAll(filepath.Dir(acmeBetikYolu), 0o755); err != nil {
		return err
	}
	d1, err := acmeAtomikYaz(acmeBetikYolu, acmeYenileBetik, 0o755)
	if err != nil {
		return fmt.Errorf("acme sarmalayıcı yazılamadı: %w", err)
	}
	d2, err := acmeAtomikYaz(acmeCronDosya, []byte(acmeCronIcerik()), 0o644)
	if err != nil {
		return fmt.Errorf("acme cron.d yazılamadı: %w", err)
	}
	if d1 || d2 {
		acmeEtiketle(acmeBetikYolu)
		acmeEtiketle(acmeCronDosya)
	}
	mev, err := acmeCrontabOku()
	if err != nil {
		return err
	}
	yeni, silinen := acmeCrontabTemizle(mev)
	if silinen > 0 {
		if err := acmeCrontabYaz(yeni); err != nil {
			return err
		}
	}
	if d1 || d2 || silinen > 0 {
		log.Printf("acme yenileme cron'u kuruldu: sarmalayıcı=%v cron.d=%v eski crontab satırı kaldırılan=%d", d1, d2, silinen)
	}
	return nil
}
