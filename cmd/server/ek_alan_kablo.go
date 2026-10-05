package main

import (
	"context"
	"database/sql"

	"girginospanel/internal/domains"
	"girginospanel/internal/subdomain"
)

// Ek (addon) alan adı ad-alanı denetimi: subdomain paketi domains'i import edemez
// (domains → subdomain), bu yüzden denetleyici burada bağlanır. Bağlanmazsa
// subdomain.EkOlustur ekleme isteğini REDDEDER (fail-closed); rota_ek_alan_test.go
// bağlantının varlığını ölçer.
func init() {
	subdomain.AdAlaniEkle = func(ctx context.Context, db *sql.DB, ad string, kiraci int64, ekle func() error) error {
		return domains.AdAlaniEkle(ctx, db, ad, kiraci, ekle)
	}
}
