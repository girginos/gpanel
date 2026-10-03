package pano

// durum.go — cp_esik_durum (göç 0126) erişimi: kenar-tetikli eşik durumu,
// iş "bildirildi" işaretleri ve örnekleyici imleçleri.

import (
	"context"
	"database/sql"
)

type anahtar struct {
	id     int64
	metrik string
}

type durum struct {
	seviye int64
	sahip  int64
}

// durumlariYukle — bir konu türünün tüm durum satırları.
func durumlariYukle(ctx context.Context, db *sql.DB, konuTur string) (map[anahtar]durum, error) {
	rows, err := db.QueryContext(ctx, `SELECT konu_id, metrik, seviye, sahip_id FROM cp_esik_durum WHERE konu_tur=?`, konuTur)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[anahtar]durum{}
	for rows.Next() {
		var a anahtar
		var d durum
		if err := rows.Scan(&a.id, &a.metrik, &d.seviye, &d.sahip); err != nil {
			return nil, err
		}
		m[a] = d
	}
	return m, rows.Err()
}

// durumYaz — durum satırını yazar/günceller. degisim_at yalnız seviye değişince
// tazelenir (atama sırası önemli: seviye güncellenmeden ÖNCE karşılaştırılır).
func durumYaz(ctx context.Context, db *sql.DB, konuTur string, id int64, metrik string, seviye int64, deger float64, sahip int64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO cp_esik_durum (konu_tur, konu_id, metrik, seviye, deger, sahip_id, olcum_at, degisim_at)
		 VALUES (?,?,?,?,?,?,NOW(),NOW())
		 ON DUPLICATE KEY UPDATE degisim_at = IF(seviye <> VALUES(seviye), NOW(), degisim_at),
		    seviye = VALUES(seviye), deger = VALUES(deger), sahip_id = VALUES(sahip_id), olcum_at = NOW()`,
		konuTur, id, metrik, seviye, deger, sahip)
	return err
}

// imlecYaz — örnekleyicinin bir işi yaptığı an (widget'larda "son ölçüm").
func imlecYaz(ctx context.Context, db *sql.DB, metrik string) {
	_ = durumYaz(ctx, db, "imlec", 0, metrik, 1, 0, 0)
}

// imlecVar — imleç hiç yazıldı mı (ilk tur tespiti: taban çizgisi).
func imlecVar(ctx context.Context, db *sql.DB, metrik string) bool {
	var n int
	return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cp_esik_durum WHERE konu_tur='imlec' AND konu_id=0 AND metrik=?`, metrik).Scan(&n) == nil && n > 0
}
