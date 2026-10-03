package bildirim

// temizlik.go — bildirim SAKLAMA süresi. cp_bildirim eskiden sınırsız büyüyordu
// (DELETE hiçbir yerde yoktu). Okundu kayıtları FK ON DELETE CASCADE ile düşer.

import (
	"context"
	"database/sql"
	"log"
)

// SaklamaGun — bildirimlerin tutulduğu gün sayısı (plan kararı: 180 gün).
const SaklamaGun = 180

// temizlikParca — tek işlemde silinen en fazla satır (kilit süresi kısa kalsın).
const temizlikParca = 1000

// Temizle — SaklamaGun'den eski bildirimleri parça parça siler; silinen sayıyı döner.
// Her parça kendi kısa işlemidir; kilit çakışmasında KilitDenemeli ile yeniden denenir.
// Satırlar birincil anahtarla silinir (okunduYazTek ile aynı kilit ilkesi: önce
// kimlikler, sonra PRIMARY ile DELETE — eşzamanlı "tümünü okundu" ile kilitlenme yok).
func Temizle(ctx context.Context, db *sql.DB) int64 {
	var toplam int64
	for ctx.Err() == nil {
		idler, err := kimlikler(ctx, db,
			`SELECT id FROM cp_bildirim WHERE created_at < NOW() - INTERVAL ? DAY ORDER BY id LIMIT ?`,
			SaklamaGun, temizlikParca)
		if err != nil {
			log.Printf("bildirim: saklama taraması başarısız: %v", err)
			return toplam
		}
		if len(idler) == 0 {
			return toplam
		}
		var silinen int64
		err = KilitDenemeli(ctx, func() error {
			res, e := db.ExecContext(ctx, `DELETE FROM cp_bildirim WHERE id IN (`+yerTutucu(len(idler))+`)`, idAny(idler)...) //nolint:gosec // G202/G701: yalnız yer tutucular; kimlikler ? ile bağlanır.
			if e != nil {
				return e
			}
			silinen, _ = res.RowsAffected()
			return nil
		})
		if err != nil {
			log.Printf("bildirim: saklama silmesi başarısız: %v", err)
			return toplam
		}
		toplam += silinen
		if len(idler) < temizlikParca {
			return toplam
		}
	}
	return toplam
}

func yerTutucu(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
