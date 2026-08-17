package panelhost

// İş kaydı persist — panel restart'ından sağ kal + audit log.
//
// Tasarım:
//   - Package-level DB reference (main.go set eder: panelhost.DB = d)
//   - Nil kontrolü: DB set edilmediyse persist NO-OP → panel yine çalışır
//   - IsBaslat → INSERT (durum=kosuyor)
//   - Bitir → UPDATE (durum + adimlar_json + bitis + hata)
//   - Startup → "kosuyor" olarak kalmış işleri "panel restart" olarak temizle

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// DB — main.go tarafından set edilir. Nil ise persist NO-OP.
var DB *sql.DB

// IsPersistYaz — INSERT (yeni iş başlatıldığında).
func IsPersistYaz(is *Is) {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	adimJson, _ := json.Marshal(is.Adimlar)
	_, err := DB.ExecContext(ctx,
		`INSERT INTO cp_panelhost_gecmisi (is_id, tip, hostname, durum, basla, adimlar_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		is.ID, is.Tip, is.Hostname, is.Durum, is.Basla, string(adimJson))
	if err != nil {
		log.Printf("panelhost.IsPersistYaz fail: %v", err)
	}
}

// IsPersistGuncelle — UPDATE (iş bittiğinde ya da adım eklendiğinde).
func IsPersistGuncelle(is *Is) {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	adimJson, _ := json.Marshal(is.Adimlar)
	var bitis interface{}
	if !is.Bitis.IsZero() {
		bitis = is.Bitis
	}
	_, err := DB.ExecContext(ctx,
		`UPDATE cp_panelhost_gecmisi
		 SET durum=?, bitis=?, hata=?, adimlar_json=?
		 WHERE is_id=?`,
		is.Durum, bitis, is.Hata, string(adimJson), is.ID)
	if err != nil {
		log.Printf("panelhost.IsPersistGuncelle fail: %v", err)
	}
}

// IsRestartTemizle — startup'ta çağrılır. Panel restart olduğu için yarım kalan
// "kosuyor" işleri "hata: panel restart" olarak işaretle.
func IsRestartTemizle() {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer iptal()
	res, err := DB.ExecContext(ctx,
		`UPDATE cp_panelhost_gecmisi
		 SET durum='hata', bitis=NOW(),
		     hata='panel restart oldu, iş yarım kaldı'
		 WHERE durum='kosuyor'`)
	if err != nil {
		log.Printf("panelhost.IsRestartTemizle fail: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("panelhost: %d yarım kalmış iş 'panel restart' olarak işaretlendi", n)
	}
}

// GecmisSatir — API için.
type GecmisSatir struct {
	ID       int64     `json:"id"`
	IsID     string    `json:"is_id"`
	Tip      string    `json:"tip"`
	Hostname string    `json:"hostname"`
	Durum    string    `json:"durum"`
	Basla    time.Time `json:"basla"`
	Bitis    *time.Time `json:"bitis,omitempty"`
	Hata     string    `json:"hata"`
	Adimlar  []Adim    `json:"adimlar"`
	AktorUID *int64    `json:"aktor_uid,omitempty"`
}

// IsGecmis — son N iş.
func IsGecmis(limit int) ([]GecmisSatir, error) {
	if DB == nil {
		return []GecmisSatir{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer iptal()
	rows, err := DB.QueryContext(ctx,
		`SELECT id, is_id, tip, hostname, durum, basla, bitis, hata, adimlar_json, aktor_uid
		 FROM cp_panelhost_gecmisi ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GecmisSatir{}
	for rows.Next() {
		var s GecmisSatir
		var bitis sql.NullTime
		var adimJson string
		if err := rows.Scan(&s.ID, &s.IsID, &s.Tip, &s.Hostname,
			&s.Durum, &s.Basla, &bitis, &s.Hata, &adimJson, &s.AktorUID); err != nil {
			continue
		}
		if bitis.Valid {
			s.Bitis = &bitis.Time
		}
		_ = json.Unmarshal([]byte(adimJson), &s.Adimlar)
		out = append(out, s)
	}
	return out, nil
}
