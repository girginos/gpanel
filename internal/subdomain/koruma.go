package subdomain

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"girginospanel/internal/provisioner"
)

// ReRenderKoruma: subdomain vhost'unu, o subdomain'in korumali_dizinler kayitlariyla
// (auth_basic bloklari) yeniden yazar. SSL-farkinda: sertifika varsa vhostSSL (HTTPS)
// korunur, yoksa duz HTTP vhost. nginx -t patlarsa yedekten geri doner (site bozulmaz).
// Sifre-koruma (sifrekoruma paketi) subdomain kapsaminda Ekle/Sil sonrasi bunu cagirir.
func ReRenderKoruma(db *sql.DB, subID int64) error {
	var sk, altAd, tamAd, php string
	if err := db.QueryRow(`SELECT d.sistem_kullanici, s.alt_ad, s.tam_ad, COALESCE(s.php_surum,'8.3')
		FROM subdomanlar s JOIN domains d ON d.id = s.domain_id WHERE s.id=?`, subID).
		Scan(&sk, &altAd, &tamAd, &php); err != nil {
		return err
	}
	socket, err := provisioner.PHPSocketFor(sk, php)
	if err != nil {
		return err
	}
	koruma := provisioner.ProtectedBlocksForSub(db, subID, socket)
	docroot := docrootOf(sk, tamAd)
	// SSL-farkinda: cert varsa HTTPS vhost'u koru (SSL'i ezme)
	crt, key := certYolu(sk, tamAd)
	var yeni string
	if dosyaVar(crt) && dosyaVar(key) {
		yeni = vhostSSL(tamAd, docroot, socket, crt, key, koruma)
	} else {
		yeni = vhost(tamAd, docroot, socket, koruma)
	}
	conf := confPath(sk, altAd)
	eski, _ := os.ReadFile(conf)
	if err := os.WriteFile(conf, []byte(yeni), 0o644); err != nil {
		return err
	}
	_ = exec.Command("restorecon", conf).Run()
	if out, e := exec.Command("nginx", "-t").CombinedOutput(); e != nil {
		if len(eski) > 0 {
			_ = os.WriteFile(conf, eski, 0o644)
			_ = exec.Command("nginx", "-t").Run()
		}
		return fmt.Errorf("nginx: %s", strings.TrimSpace(string(out)))
	}
	_ = exec.Command("systemctl", "reload", "nginx").Run()
	return nil
}
