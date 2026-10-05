package uzak

// ilerleme.go — ilerleme ucunun veritabanı yarısı: tur kapsamı + tek özet sorgusu.
//
// "Tur": art arda kuyruğa alınmış işlerin bütünü (ör. "Mevcut yedekleri de gönder" ile
// 63 iş). Tur, kuyruk boşken ilk iş geldiğinde başlar ve kuyruk boşalınca biter; bitişten
// sonra TamamlandiSuresi boyunca özet (tamamlandı) görünür. Başlangıç bellekte mandallanır
// (iş bitince toplam küçülmesin); panel yeniden başlarsa en eski bekleyen işten yeniden kurulur.

import (
	"context"
	"database/sql"
	"time"
)

const bekleyenDurumlar = `('bekliyor','sifreleniyor','yukleniyor','dogrulaniyor')`

// IlerlemeOku — canlı ilerleme: bellek (z) + iki küçük sorgu (≈ms). z nil ise işçi
// çalışmıyor sayılır. Yanıtta sır/anahtar/yerel yol YOKTUR.
func IlerlemeOku(ctx context.Context, db *sql.DB, z *Izleyici, ayarAktif bool) (Ilerleme, error) {
	if z == nil {
		z = &Izleyici{}
	}
	var bek int
	var min string
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(DATE_FORMAT(MIN(created_at) - INTERVAL 60 SECOND,'%Y-%m-%d %H:%i:%s'),'')
		 FROM backup_uzak_kopyalar WHERE kaynak='hedef' AND durum IN `+bekleyenDurumlar+` AND created_at >= NOW() - INTERVAL 3 DAY`).
		Scan(&bek, &min); err != nil {
		return Ilerleme{}, err
	}
	z.mu.Lock()
	now := z.simdi()
	tamamlandi := false
	if bek > 0 {
		if z.turBas == "" || !z.turBitis.IsZero() {
			z.turBas = min
		}
		z.turBitis = time.Time{}
	} else if z.turBas != "" {
		if z.turBitis.IsZero() {
			z.turBitis = now
		}
		if now.Sub(z.turBitis) > TamamlandiSuresi {
			z.turBas, z.turBitis = "", time.Time{}
		} else {
			tamamlandi = true
		}
	}
	tur := z.turBas
	if bek > 0 && tur == "" {
		tur = min
	}
	z.mu.Unlock()

	var s Sayim
	if tur != "" {
		var err error
		if s, err = sayimOku(ctx, db, tur); err != nil {
			return Ilerleme{}, err
		}
		if tamamlandi && s.ToplamIs == 0 {
			tamamlandi = false
		}
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hesapla(s, ayarAktif, tamamlandi), nil
}

func sayimOku(ctx context.Context, db *sql.DB, tur string) (Sayim, error) {
	const hazir = `(k.durum='bekliyor' AND (k.sonraki_deneme IS NULL OR k.sonraki_deneme<=NOW()))`
	const bitti = `k.durum IN ('tamam','silindi','siliniyor')`
	var s Sayim
	err := db.QueryRowContext(ctx,
		`SELECT CAST(COALESCE(SUM(k.durum<>'iptal'),0) AS SIGNED),
		        CAST(COALESCE(SUM(`+bitti+`),0) AS SIGNED),
		        CAST(COALESCE(SUM(k.durum='hata'),0) AS SIGNED),
		        CAST(COALESCE(SUM(`+hazir+`),0) AS SIGNED),
		        CAST(COALESCE(SUM(k.durum='bekliyor' AND k.sonraki_deneme>NOW()),0) AS SIGNED),
		        CAST(COALESCE(SUM(k.durum IN ('sifreleniyor','yukleniyor','dogrulaniyor')),0) AS SIGNED),
		        CAST(COALESCE(SUM(CASE WHEN k.durum NOT IN ('hata','iptal') THEN k.sz END),0) AS SIGNED),
		        CAST(COALESCE(SUM(CASE WHEN `+bitti+` THEN k.sz END),0) AS SIGNED),
		        CAST(COALESCE(SUM(CASE WHEN `+hazir+` THEN k.sz END),0) AS SIGNED)
		 FROM (SELECT x.durum, x.sonraki_deneme, COALESCE(NULLIF(x.boyut_b,0), NULLIF(x.boyut_duz,0), b.boyut_b, 0) AS sz
		       FROM backup_uzak_kopyalar x LEFT JOIN backups b ON b.id=x.backup_id
		       WHERE x.kaynak='hedef' AND x.created_at >= ?) k`, tur).
		Scan(&s.ToplamIs, &s.Tamam, &s.Hata, &s.Hazir, &s.Yeniden, &s.Kosan, &s.ToplamB, &s.TamamB, &s.HazirB)
	return s, err
}
