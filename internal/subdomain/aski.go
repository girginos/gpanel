package subdomain

// Hesap askısı — alt alan adı tarafı. Askı HESAP düzeyindedir: hesabın bütün
// alt alan adları ana alan adıyla birlikte kapanır / açılır (hesapaski.Uygula).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"girginospanel/internal/php"
)

// altAlan — hesabın bir alt alan adı kaydı.
type altAlan struct {
	id                    int64
	altAd, tamAd, php, sk string
}

// altAlanlar — hesabın (domainID) alt alan adları.
func altAlanlar(ctx context.Context, db *sql.DB, domainID int64) ([]altAlan, error) {
	rows, err := db.QueryContext(ctx, `SELECT s.id, s.alt_ad, s.tam_ad, COALESCE(s.php_surum,'8.3'), d.sistem_kullanici
		FROM subdomanlar s JOIN domains d ON d.id = s.domain_id WHERE s.domain_id=? ORDER BY s.id`, domainID)
	if err != nil {
		return nil, fmt.Errorf("alt alan adları okunamadı: %w", err)
	}
	defer rows.Close()
	var liste []altAlan
	for rows.Next() {
		var a altAlan
		if err := rows.Scan(&a.id, &a.altAd, &a.tamAd, &a.php, &a.sk); err != nil {
			return nil, fmt.Errorf("alt alan adları okunamadı: %w", err)
		}
		liste = append(liste, a)
	}
	if err := rows.Err(); err != nil {
		// Yarım liste "hepsi kapandı" yanılgısı üretir.
		return nil, fmt.Errorf("alt alan adları eksik okundu: %w", err)
	}
	return liste, nil
}

// AltAlanVhostlariniYenidenKur — hesabın TÜM alt alan adı vhost'larını tek
// üreticiden (rebuildVhostDB) yeniden kurar. Askı durumu üreticinin içinde tek
// kaynaktan okunur: hesap askıdaysa hepsi 503 olur; askıdan alınca her biri
// kendi ayarlarıyla (PHP havuzu, SSL, web sunucu, erişim/koruma) aynen döner.
// Bir alt alan düşse bile ötekiler denenir; hatalar birlikte döner.
func AltAlanVhostlariniYenidenKur(ctx context.Context, db *sql.DB, domainID int64) error {
	liste, err := altAlanlar(ctx, db, domainID)
	if err != nil {
		return err
	}
	var hatalar []error
	for _, a := range liste {
		if err := vhostYenidenKur(ctx, db, a.id, a.sk, a.altAd, a.tamAd, a.php); err != nil {
			hatalar = append(hatalar, fmt.Errorf("alt alan adı %s: %w", a.tamAd, err))
		}
	}
	return errors.Join(hatalar...)
}

// AltAlanHavuzAdlari — hesabın alt alan adı PHP havuzlarının adları (php.SubPoolName).
// Havuzu olmayan alt alanın adı da döner; askı yalnız VAR olan havuz dosyasına dokunur.
func AltAlanHavuzAdlari(ctx context.Context, db *sql.DB, domainID int64, sk string) ([]string, error) {
	liste, err := altAlanlar(ctx, db, domainID)
	if err != nil {
		return nil, err
	}
	adlar := make([]string, 0, len(liste))
	for _, a := range liste {
		adlar = append(adlar, php.SubPoolName(sk, a.id))
	}
	return adlar, nil
}
