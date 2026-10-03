package domains

// Site taşıma için dışa açma (bayi kota denetimi). Mantık resellerKotaKontrol'dedir;
// davranış birebir aynıdır (max_domain, fiili disk, taahhüt disk/trafik, askı).

import (
	"context"
	"database/sql"
)

// TasimaKotaKontrol — bayi (rid) yeni bir hosting açabilir mi? planID nil olabilir.
// Çağıran kilit.Bayi(rid) kilidini kota okuması ile domains INSERT'i arasında
// TUTMALIDIR (domains.Create ile aynı TOCTOU kuralı).
func TasimaKotaKontrol(ctx context.Context, db *sql.DB, rid int64, planID *int64) error {
	return (&Handlers{DB: db}).resellerKotaKontrol(ctx, rid, planID)
}
