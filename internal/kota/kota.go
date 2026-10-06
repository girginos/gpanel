// Package kota: plan limitlerini check eder (yeni domain/DB/FTP ekleme oncesi)
package kota

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"girginospanel/internal/efektiflimit"
)

type LimitHatasi struct {
	Mesaj string
}

func (e *LimitHatasi) Error() string { return e.Mesaj }

// CheckDomainEklenebilir: customer_id varsa onun plan.max_domain'ine bak
func CheckDomainEklenebilir(ctx context.Context, db *sql.DB, customerID *int64) error {
	if customerID == nil {
		return nil // admin için sınır yok
	}
	var planID *int64
	if err := db.QueryRowContext(ctx, `SELECT plan_id FROM customers WHERE id=?`, *customerID).Scan(&planID); err != nil {
		return nil
	}
	if planID == nil {
		return nil
	}
	var maks int
	if err := db.QueryRowContext(ctx, `SELECT max_domain FROM service_plans WHERE id=?`, *planID).Scan(&maks); err != nil {
		return nil
	}
	if maks <= 0 {
		return nil // sınırsız
	}
	var mevcut int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE customer_id=?`, *customerID).Scan(&mevcut)
	if mevcut >= maks {
		return &LimitHatasi{Mesaj: fmt.Sprintf("plan limiti aşıldı: max %d domain", maks)}
	}
	return nil
}

// CheckDBEklenebilir: domain'e yeni veritabanı eklenebilir mi?
//
// 🔴 Kapı EFEKTİF limiti (hosting planı ⊕ hostinge özel üst yazım, efektiflimit —
// tek kaynak) bu hostingin kendi veritabanı sayısına karşı denetler; 0/NULL =
// sınırsız. Eskiden yalnız customers.plan_id → service_plans.max_db'ye bakılıyordu:
// canlıda customers tablosu BOŞ ve hosting planı domains.plan_id'de olduğundan kapı
// fiilen hiç uygulanmıyor, üst yazım (hosting_limit_ust.max_db) hiç okunmuyordu.
// Eski müşteri-modeli denetimi (hesap genelinde toplam) ek olarak korunur.
func CheckDBEklenebilir(ctx context.Context, db *sql.DB, domainID int64) error {
	var maks int
	err := db.QueryRowContext(ctx,
		`SELECT `+efektiflimit.IfadeVarsayilan("max_db", 0)+` FROM domains d`+efektiflimit.Join+`WHERE d.id=?`,
		domainID).Scan(&maks)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // domain yok: çağıran zaten 404/yan etkisiz yola düşer
	}
	if err != nil {
		return &LimitHatasi{Mesaj: "plan limitleri okunamadı"}
	}
	if maks > 0 {
		var mevcut int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=?`, domainID).Scan(&mevcut); err != nil {
			return &LimitHatasi{Mesaj: "veritabanı sayısı okunamadı"}
		}
		if mevcut >= maks {
			return &LimitHatasi{Mesaj: fmt.Sprintf("plan limiti aşıldı: max %d veritabanı", maks)}
		}
	}
	return musteriDBKotasi(ctx, db, domainID)
}

// musteriDBKotasi: eski müşteri modeli — customers.plan_id planının max_db'si, müşterinin
// TÜM hostinglerindeki toplam veritabanına karşı.
func musteriDBKotasi(ctx context.Context, db *sql.DB, domainID int64) error {
	var customerID *int64
	if err := db.QueryRowContext(ctx, `SELECT customer_id FROM domains WHERE id=?`, domainID).Scan(&customerID); err != nil {
		return nil
	}
	if customerID == nil {
		return nil
	}
	var planID *int64
	_ = db.QueryRowContext(ctx, `SELECT plan_id FROM customers WHERE id=?`, *customerID).Scan(&planID)
	if planID == nil {
		return nil
	}
	var maks int
	_ = db.QueryRowContext(ctx, `SELECT max_db FROM service_plans WHERE id=?`, *planID).Scan(&maks)
	if maks <= 0 {
		return nil
	}
	var mevcut int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM db_accounts a JOIN domains d ON d.id=a.domain_id WHERE d.customer_id=?`,
		*customerID).Scan(&mevcut)
	if mevcut >= maks {
		return &LimitHatasi{Mesaj: fmt.Sprintf("plan limiti aşıldı: max %d veritabanı", maks)}
	}
	return nil
}
