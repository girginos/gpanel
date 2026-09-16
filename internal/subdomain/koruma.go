package subdomain

import (
	"context"
	"database/sql"
)

// ReRenderKoruma: subdomain vhost'unu, o subdomain'in korumali_dizinler kayitlariyla
// (auth_basic bloklari) yeniden yazar. SSL-farkinda: sertifika varsa vhostSSL (HTTPS)
// korunur, yoksa duz HTTP vhost. nginx -t patlarsa yedekten geri doner (site bozulmaz).
// Sifre-koruma (sifrekoruma paketi) subdomain kapsaminda Ekle/Sil sonrasi bunu cagirir.
func ReRenderKoruma(db *sql.DB, subID int64) error {
	// 🔴 TEK RENDER YOLU. Burasi eskiden kendi (eksik) uretecini kullaniyordu:
	// erisim kisitlama blogunu ve musterinin nginx ayarlarini dusuruyordu.
	// Artik ana yolu cagiriyor -- ayni dosya, ayni kilit, ayni geri alma.
	var sk, altAd, tamAd, php string
	if err := db.QueryRow(`SELECT d.sistem_kullanici, s.alt_ad, s.tam_ad, COALESCE(s.php_surum,'8.3')
		FROM subdomanlar s JOIN domains d ON d.id = s.domain_id WHERE s.id=?`, subID).
		Scan(&sk, &altAd, &tamAd, &php); err != nil {
		return err
	}
	return rebuildVhostDB(context.Background(), db, subID, sk, altAd, tamAd, php)
}
