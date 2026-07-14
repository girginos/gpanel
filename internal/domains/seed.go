package domains

import (
	"context"
	"database/sql"
)

// SeedIfEmpty: eskiden boş tabloya 4 demo domain eklerdi.
// Artık NO-OP — yeni kurulumda ZERO domain gelir (demo site yok).
// Domain silinip tablo boşalınca demo'ların geri türemesi bug'ı da bununla kapanır.
func SeedIfEmpty(ctx context.Context, db *sql.DB, ipv4 string) error {
	_ = ctx
	_ = db
	_ = ipv4
	return nil
}
