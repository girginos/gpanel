// Package kota: plan limitlerini check eder (yeni domain/DB/FTP ekleme oncesi)
package kota

import (
	"context"
	"database/sql"
	"fmt"
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

// CheckDBEklenebilir: domain'in customer plan.max_db
func CheckDBEklenebilir(ctx context.Context, db *sql.DB, domainID int64) error {
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
