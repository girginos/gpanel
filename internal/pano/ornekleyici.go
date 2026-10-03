package pano

// ornekleyici.go — pano arka plan döngüleri (Baslat, cmd/server açılışı):
//
//	15 dk  Tur:     disk ölçümü (toplu xfs_quota) → günlük anlık görüntü →
//	                kota/havuz eşikleri → (6 saatte bir) SSL bitiş taraması + eşik →
//	                Website Security yeni yüksek bulgu
//	 1 dk  IsTur:   biten taşıma / toplu işlem bildirimleri
//	24 sa  Temizle: bildirim saklama (180 gün), günlük görüntü (400 gün), yetim durumlar
//
// Her adım bağımsızdır: biri düşerse ötekiler koşar (hata günlüğe). Uçlar bu
// işlerin hiçbirini istek anında yapmaz.

import (
	"context"
	"database/sql"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/bildirim"
	"girginospanel/internal/domains"
	"girginospanel/internal/kaynaklimit"
)

// Dikişler (testler sahteyle değiştirir: ana makinenin xfs_quota'sına / sertifika
// dosyalarına dokunulmaz).
var (
	kotaOkuyucu   = kaynaklimit.KotaRaporuTumu
	sslTazeleyici = domains.SSLBitisTazele
)

const (
	gunlukSaklamaGun = 400
	isIsaretSaklama  = 30 // gün
	sslAraligi       = 6 * time.Hour
)

var (
	sslMu  sync.Mutex
	sslSon time.Time
)

// Baslat — döngüleri başlatır (açılıştan kısa süre sonra; göçler koşmuş olur).
func Baslat(db *sql.DB) {
	go periyodik("tur", 2*time.Minute, 15*time.Minute, 10*time.Minute, func(ctx context.Context) { Tur(ctx, db) })
	go periyodik("is", 90*time.Second, time.Minute, 50*time.Second, func(ctx context.Context) { IsTur(ctx, db) })
	go periyodik("temizlik", 10*time.Minute, 24*time.Hour, 30*time.Minute, func(ctx context.Context) { Temizlik(ctx, db) })
}

func periyodik(ad string, ilk, aralik, sure time.Duration, fn func(context.Context)) {
	time.Sleep(ilk)
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pano: %s döngüsü panikledi: %v", ad, r)
				}
			}()
			ctx, iptal := context.WithTimeout(context.Background(), sure)
			defer iptal()
			fn(ctx)
		}()
		time.Sleep(aralik)
	}
}

// Tur — 15 dakikalık örnekleme turu.
func Tur(ctx context.Context, db *sql.DB) {
	if err := diskOlc(ctx, db); err != nil {
		log.Printf("pano: disk ölçümü: %v", err)
	}
	if err := gunlukYaz(ctx, db); err != nil {
		log.Printf("pano: günlük anlık görüntü: %v", err)
	}
	if err := kotaEsikTara(ctx, db); err != nil {
		log.Printf("pano: kota eşikleri: %v", err)
	}
	if err := havuzEsikTara(ctx, db); err != nil {
		log.Printf("pano: bayi havuz eşikleri: %v", err)
	}
	sslMu.Lock()
	sslZamani := time.Since(sslSon) >= sslAraligi
	if sslZamani {
		sslSon = time.Now()
	}
	sslMu.Unlock()
	if sslZamani {
		sslTazeleyici(ctx, db)
		imlecYaz(ctx, db, "ssl_tarama")
	}
	if err := sslEsikTara(ctx, db); err != nil {
		log.Printf("pano: SSL eşikleri: %v", err)
	}
	if err := websecTara(ctx, db); err != nil {
		log.Printf("pano: websec bulguları: %v", err)
	}
}

// diskOlc — domains.boyut_kb'yi toplu XFS kota raporundan tazeler. Kota
// accounting kapalıysa DOKUNMAZ (sıfır yazmak "boş disk" yalanı olur; değer
// /kaynak açılışındaki du ölçümüyle kalır) ve imleç yazılmaz.
func diskOlc(ctx context.Context, db *sql.DB) error {
	m, ok := kotaOkuyucu()
	if !ok {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, sistem_kullanici, COALESCE(boyut_kb,0) FROM domains WHERE sistem_kullanici LIKE 'c\_%'`)
	if err != nil {
		return err
	}
	type s struct {
		id int64
		kb int64
	}
	var guncel []s
	for rows.Next() {
		var id, eski int64
		var sk string
		if rows.Scan(&id, &sk, &eski) != nil {
			continue
		}
		if k, bulundu := m[sk]; bulundu && k.KullanilanKB != eski {
			guncel = append(guncel, s{id, k.KullanilanKB})
		}
	}
	rows.Close()
	for _, g := range guncel {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := db.ExecContext(ctx, `UPDATE domains SET boyut_kb=? WHERE id=?`, g.kb, g.id); err != nil {
			return err
		}
	}
	imlecYaz(ctx, db, "disk_olcum")
	return nil
}

// gunlukYaz — BUGÜNÜN satırları: tüm sunucu (0) + her bayi (domainsiz bayi de
// sıfırlarla yazılır — eğilimde boşluk "veri yok" sanılmasın). Sayımlar
// /reseller/ozet ile aynı tanım (demo dahil COUNT(*)), KPI ile eğilim tutarlı.
func gunlukYaz(ctx context.Context, db *sql.DB) error {
	const guncelle = ` ON DUPLICATE KEY UPDATE hesap=VALUES(hesap), askida=VALUES(askida), yeni=VALUES(yeni),
		disk_kb=VALUES(disk_kb), trafik_kb=VALUES(trafik_kb)`
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pano_gunluk (tarih, kapsam_id, hesap, askida, yeni, disk_kb, trafik_kb)
		 SELECT CURDATE(), 0, COUNT(*), COALESCE(SUM(COALESCE(askida,0)),0),
		        COALESCE(SUM(DATE(olusturulma)=CURDATE()),0), COALESCE(SUM(boyut_kb),0), COALESCE(SUM(trafik_kb),0)
		   FROM domains`+guncelle); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO pano_gunluk (tarih, kapsam_id, hesap, askida, yeni, disk_kb, trafik_kb)
		 SELECT CURDATE(), u.id, COUNT(d.id), COALESCE(SUM(COALESCE(d.askida,0)),0),
		        COALESCE(SUM(DATE(d.olusturulma)=CURDATE()),0), COALESCE(SUM(d.boyut_kb),0), COALESCE(SUM(d.trafik_kb),0)
		   FROM users u LEFT JOIN domains d ON d.reseller_id=u.id
		  WHERE u.role='reseller' GROUP BY u.id`+guncelle)
	return err
}

// Temizlik — saklama süreleri.
func Temizlik(ctx context.Context, db *sql.DB) {
	if n := bildirim.Temizle(ctx, db); n > 0 {
		log.Printf("pano: %d eski bildirim silindi (%d günden eski)", n, bildirim.SaklamaGun)
	}
	sorgular := []string{
		`DELETE FROM pano_gunluk WHERE tarih < CURDATE() - INTERVAL ` + strconv.Itoa(gunlukSaklamaGun) + ` DAY`,
		`DELETE e FROM cp_esik_durum e LEFT JOIN domains d ON d.id=e.konu_id WHERE e.konu_tur='domain' AND d.id IS NULL`,
		`DELETE e FROM cp_esik_durum e LEFT JOIN users u ON u.id=e.konu_id AND u.role='reseller' WHERE e.konu_tur='bayi' AND u.id IS NULL`,
		`DELETE FROM cp_esik_durum WHERE konu_tur='is' AND olcum_at < NOW() - INTERVAL ` + strconv.Itoa(isIsaretSaklama) + ` DAY`,
	}
	for _, q := range sorgular {
		if _, err := db.ExecContext(ctx, q); err != nil {
			log.Printf("pano: temizlik (%s…): %v", strings.Fields(q)[0], err)
		}
	}
}
