package dns

// mail_dkim_dns.go — mail eklentisi DKIM public key'ini panel DNS zone'una
// "mail._domainkey" TXT olarak yayınlar ve panel'in kendi ürettiği, artık
// kullanılmayan "default._domainkey" kaydını kaldırır.
//
// Neden: mail eklentisi aktifken imzalama opendkim'in "mail" selector key'iyle
// yapılır; panel DNS'in "default" key'i işe yaramaz (bkz. provisioner/mail_dkim.go).
// Bu fonksiyon DNS'i imzalayan gerçek key'e hizalar → alıcı DKIM'i doğrulayabilir.

import (
	"context"
	"database/sql"
	"strings"
)

// MailDKIMYayinla — verilen DKIM DNS TXT değerini (v=DKIM1; k=rsa; p=...) bu
// domain için "mail._domainkey" olarak yazar (idempotent upsert), eski
// "default._domainkey" kayıtlarını siler, zone'u yeniden yazıp reload eder.
func MailDKIMYayinla(ctx context.Context, db *sql.DB, domainID int64, dnsTxt string) error {
	dnsTxt = strings.TrimSpace(dnsTxt)
	if dnsTxt == "" {
		return nil // eklenti key vermedi — sessizce geç (mail yoksa DKIM de yok)
	}

	// 1) Uyumsuz "default._domainkey" (panel'in kendi ürettiği, imzalamada
	//    kullanılmayan) kaydı temizle — yoksa alıcıda iki farklı key kafa karıştırır.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM dns_records WHERE domain_id=? AND ad='default._domainkey' AND tip='TXT'`,
		domainID); err != nil {
		return err
	}

	// 2) mail._domainkey upsert: varsa değeri güncelle, yoksa ekle.
	var mevcutID int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM dns_records WHERE domain_id=? AND ad='mail._domainkey' AND tip='TXT' LIMIT 1`,
		domainID).Scan(&mevcutID)
	if err == nil {
		if _, e := db.ExecContext(ctx,
			`UPDATE dns_records SET deger=?, aktif=1 WHERE id=?`, dnsTxt, mevcutID); e != nil {
			return e
		}
	} else {
		if _, e := db.ExecContext(ctx,
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?, 'mail._domainkey', 'TXT', ?, 3600, 0, 1)`, domainID, dnsTxt); e != nil {
			return e
		}
	}

	// 3) Zone dosyasını yeniden yaz + nameserver reload (WriteZone içinde reload).
	return WriteZone(ctx, db, domainID)
}
