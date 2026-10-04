package ajan

import (
	"context"
	"database/sql"
	"errors"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
)

// duzeySira: düzeylerin sırası (min kuralı için).
var duzeySira = map[p.Duzey]int{p.DuzeyKapali: 0, p.DuzeyOkuma: 1, p.DuzeyOnarim: 2, p.DuzeyTam: 3}

// minDuzey: en düşük düzey; geçersiz/bilinmeyen değer KAPALI sayılır (kapalı başarısız).
func minDuzey(ds ...p.Duzey) p.Duzey {
	en := p.DuzeyTam
	for _, d := range ds {
		s, ok := duzeySira[d]
		if !ok {
			return p.DuzeyKapali
		}
		if s < duzeySira[en] {
			en = d
		}
	}
	return en
}

// Ayar: destek_ayar satırı. Kök satırı (reseller_id=0) modül şalteri + üst sınırlar;
// bayi satırı yalnız MusteriDuzey ("müşterilerim kullanabilir").
type Ayar struct {
	ModulDuzey   p.Duzey `json:"modul_duzey"`
	BayiDuzey    p.Duzey `json:"bayi_duzey"`
	MusteriDuzey p.Duzey `json:"musteri_duzey"`
}

var kapaliAyar = Ayar{ModulDuzey: p.DuzeyKapali, BayiDuzey: p.DuzeyKapali, MusteriDuzey: p.DuzeyKapali}

// ayarOku: satır yoksa her şey kapalı; tablo yoksa da kapalı (göç koşmadı).
func ayarOku(ctx context.Context, db *sql.DB, rid int64) (Ayar, error) {
	var a Ayar
	var m, b, mu string
	err := db.QueryRowContext(ctx, `SELECT modul_duzey, bayi_duzey, musteri_duzey FROM destek_ayar WHERE reseller_id=?`, rid).Scan(&m, &b, &mu)
	if errors.Is(err, sql.ErrNoRows) || iptal.TabloYok(err) {
		return kapaliAyar, nil
	}
	if err != nil {
		return kapaliAyar, err
	}
	a.ModulDuzey, a.BayiDuzey, a.MusteriDuzey = p.Duzey(m), p.Duzey(b), p.Duzey(mu)
	return a, nil
}

// Tavan: bir sahibin (rol + bayi) bağlantılarına uygulanacak üst sınır — bağlantı
// düzeyi hariç min kuralı:
//
//	kök     = modül
//	bayi    = min(modül, kök.bayi)
//	müşteri = min(modül, kök.müşteri, [bayi.müşteri — domain bir bayinin ise])
//
// DB hatası → KAPALI (kapalı başarısız).
func Tavan(ctx context.Context, db *sql.DB, rol p.Rol, resellerID int64) p.Duzey {
	kok, err := ayarOku(ctx, db, 0)
	if err != nil {
		return p.DuzeyKapali
	}
	switch rol {
	case p.RolKok:
		return minDuzey(kok.ModulDuzey)
	case p.RolBayi:
		return minDuzey(kok.ModulDuzey, kok.BayiDuzey)
	case p.RolMusteri:
		if resellerID <= 0 {
			return minDuzey(kok.ModulDuzey, kok.MusteriDuzey)
		}
		bayi, err := ayarOku(ctx, db, resellerID)
		if err != nil {
			return p.DuzeyKapali
		}
		return minDuzey(kok.ModulDuzey, kok.MusteriDuzey, bayi.MusteriDuzey)
	}
	return p.DuzeyKapali
}

// EtkinDuzey = min(tavan, bağlantı düzeyi).
func EtkinDuzey(ctx context.Context, db *sql.DB, rol p.Rol, resellerID int64, baglantiDuzey p.Duzey) p.Duzey {
	return minDuzey(Tavan(ctx, db, rol, resellerID), baglantiDuzey)
}

// modulAcik: kök şalteri kapalı değil mi.
func modulAcik(ctx context.Context, db *sql.DB) bool {
	a, err := ayarOku(ctx, db, 0)
	return err == nil && minDuzey(a.ModulDuzey) != p.DuzeyKapali
}

// ayarYaz: ayar satırını yazar. surum her yazımda artar (aynı değerler yazılsa bile): ajan
// süreci DB yoklamasıyla değişikliği buradan fark eder (izleme.go ayarYokla). Göç 0130
// uygulanmamışsa (sütun yok) eski ifadeye düşer.
func ayarYaz(ctx context.Context, db *sql.DB, rid int64, a Ayar, guncelleyen string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO destek_ayar (reseller_id, modul_duzey, bayi_duzey, musteri_duzey, guncelleyen, surum)
		VALUES (?,?,?,?,?,1) ON DUPLICATE KEY UPDATE modul_duzey=VALUES(modul_duzey), bayi_duzey=VALUES(bayi_duzey),
		musteri_duzey=VALUES(musteri_duzey), guncelleyen=VALUES(guncelleyen), surum=surum+1`,
		rid, string(a.ModulDuzey), string(a.BayiDuzey), string(a.MusteriDuzey), kesRune(guncelleyen, 64))
	if kolonYok(err) {
		_, err = db.ExecContext(ctx, `INSERT INTO destek_ayar (reseller_id, modul_duzey, bayi_duzey, musteri_duzey, guncelleyen)
		VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE modul_duzey=VALUES(modul_duzey), bayi_duzey=VALUES(bayi_duzey),
		musteri_duzey=VALUES(musteri_duzey), guncelleyen=VALUES(guncelleyen)`,
			rid, string(a.ModulDuzey), string(a.BayiDuzey), string(a.MusteriDuzey), kesRune(guncelleyen, 64))
	}
	return err
}
