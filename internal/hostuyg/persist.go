package hostuyg

// cp_host_uygulamalar DB katmanı — CRUD + list.

import (
	"context"
	"database/sql"
	"time"
)

type UygulamaKayit struct {
	ID              int64     `json:"id"`
	Kod             string    `json:"kod"`
	OrnekAd         string    `json:"ornek_ad"`
	Surum           string    `json:"surum"`
	KurulumYolu     string    `json:"kurulum_yolu"`
	SistemKullanici string    `json:"sistem_kullanici"`
	SystemdUnit     string    `json:"systemd_unit"`
	SubdomainAd     string    `json:"subdomain_ad"` // Faz 3.4: cert cleanup için persist
	Durum           string    `json:"durum"`
	SonHata         string    `json:"son_hata"`
	MetaJSON        string    `json:"meta_json"`
	KuranUID        *int64    `json:"kuran_uid,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// Denormalize — sadece Liste'de doldurulur (JOIN yerine ayrı sorgu)
	Portlar    []PortKayit `json:"portlar,omitempty"`
	UnitDurumu string      `json:"unit_durumu,omitempty"` // "active" | "inactive" | ...
}

type PortKayit struct {
	Port         int    `json:"port"`
	Protokol     string `json:"protokol"`
	Aciklama     string `json:"aciklama"`
	FirewallAcik bool   `json:"firewall_acik"`
}

// UygulamaYaz — INSERT, id döner.
func UygulamaYaz(ctx context.Context, db *sql.DB, k *UygulamaKayit, kuranUID *int64) (int64, error) {
	res, err := db.ExecContext(ctx,
		`INSERT INTO cp_host_uygulamalar
		 (kod, ornek_ad, surum, kurulum_yolu, sistem_kullanici, systemd_unit, subdomain_ad,
		  durum, meta_json, kuran_uid)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.Kod, k.OrnekAd, k.Surum, k.KurulumYolu, k.SistemKullanici, k.SystemdUnit, k.SubdomainAd,
		k.Durum, k.MetaJSON, kuranUID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UygulamaDurumGuncelle — durum + son_hata alanlarını güncelle.
func UygulamaDurumGuncelle(ctx context.Context, db *sql.DB, id int64, durum, sonHata string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE cp_host_uygulamalar SET durum=?, son_hata=? WHERE id=?`,
		durum, sonHata, id)
	return err
}

// UygulamaGetir — id'e göre tek kayıt (+ port'lar).
func UygulamaGetir(ctx context.Context, db *sql.DB, id int64) (*UygulamaKayit, error) {
	k := &UygulamaKayit{}
	err := db.QueryRowContext(ctx,
		`SELECT id, kod, ornek_ad, surum, kurulum_yolu, sistem_kullanici, systemd_unit, subdomain_ad,
		        durum, son_hata, meta_json, kuran_uid, created_at, updated_at
		 FROM cp_host_uygulamalar WHERE id=?`, id).Scan(
		&k.ID, &k.Kod, &k.OrnekAd, &k.Surum, &k.KurulumYolu, &k.SistemKullanici, &k.SystemdUnit, &k.SubdomainAd,
		&k.Durum, &k.SonHata, &k.MetaJSON, &k.KuranUID, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	k.Portlar, _ = portlariYukle(ctx, db, id)
	return k, nil
}

// UygulamaListe — tümü (küçük N, bu admin panel).
func UygulamaListe(ctx context.Context, db *sql.DB) ([]UygulamaKayit, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, kod, ornek_ad, surum, kurulum_yolu, sistem_kullanici, systemd_unit, subdomain_ad,
		        durum, son_hata, meta_json, kuran_uid, created_at, updated_at
		 FROM cp_host_uygulamalar ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UygulamaKayit{}
	for rows.Next() {
		var k UygulamaKayit
		if err := rows.Scan(
			&k.ID, &k.Kod, &k.OrnekAd, &k.Surum, &k.KurulumYolu, &k.SistemKullanici, &k.SystemdUnit, &k.SubdomainAd,
			&k.Durum, &k.SonHata, &k.MetaJSON, &k.KuranUID, &k.CreatedAt, &k.UpdatedAt); err != nil {
			continue
		}
		k.Portlar, _ = portlariYukle(ctx, db, k.ID)
		k.UnitDurumu = UnitDurum(k.SystemdUnit)
		out = append(out, k)
	}
	return out, nil
}

// UygulamaSil — DB'den kaldır (portlar CASCADE ile gider).
func UygulamaSil(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM cp_host_uygulamalar WHERE id=?`, id)
	return err
}

func portlariYukle(ctx context.Context, db *sql.DB, uygulamaID int64) ([]PortKayit, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT port, protokol, aciklama, firewall_acik
		 FROM cp_host_uyg_portlari WHERE uygulama_id=? ORDER BY port`, uygulamaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortKayit{}
	for rows.Next() {
		var p PortKayit
		var fa int
		if err := rows.Scan(&p.Port, &p.Protokol, &p.Aciklama, &fa); err == nil {
			p.FirewallAcik = fa != 0
			out = append(out, p)
		}
	}
	return out, nil
}
