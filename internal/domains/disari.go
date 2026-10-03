// Dışa açma sarmalayıcıları — domains paketinin iç yardımcılarını başka paketlere
// (site taşıma) açar. Davranış iç yardımcıyla birebir aynıdır; mantık burada DEĞİL,
// sarılan fonksiyondadır.
package domains

import (
	"context"
	"database/sql"
)

// MailKutuLimitSenkron — domaine uygulanan planın posta limitlerini (kutu sayısı,
// saatlik gönderim, kutu başına kota) mail eklentisine iletir; best-effort (bkz.
// mailKutuLimitSenkron). Site taşıma kutu AÇMADAN önce çağırır: limit eklentide
// uygulanır, yeni mail domaininde ise henüz yoktur.
func MailKutuLimitSenkron(ctx context.Context, db *sql.DB, domainID int64, alanAdi string) {
	mailKutuLimitSenkron(ctx, db, domainID, alanAdi)
}
