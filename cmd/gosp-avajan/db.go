package main

// Ajan → veritabanı: bulguları av_taramalar/av_bulgular'a yazar, karantina
// yollarını saklar, bildirim üretir.
//
// 🔴 NEDEN: ajan bir zararlı bulunca panel GÖRMELİ. Eskiden ajan yalnız
// stdout/json veriyordu; zamanlı/gerçek-zamanlı tarama bir şey bulsa bile
// panelde iz yoktu ("başarısızlık güven olarak render" — operatör temiz sanır).

import (
	"database/sql"
	"path/filepath"
	"strings"
	"time"

	"girginospanel/internal/avmotor"
)

// taramaBasla — av_taramalar'a yeni bir tarama kaydı açar, id döner.
// domain_id=0 = tüm sunucu/host taraması (bulgular kendi domain_id'lerini taşır).
func taramaBasla(db *sql.DB, kaynak, kapsam string) int64 {
	if db == nil {
		return 0
	}
	res, err := db.Exec(
		`INSERT INTO av_taramalar (domain_id, durum, motor, kaynak, kapsam)
		 VALUES (0, 'calisiyor', 'gosp', ?, ?)`, kaynak, kapsam)
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// taramaBitir — tarama kaydını tamamlar.
func taramaBitir(db *sql.DB, taramaID int64, taranan, enfekte int) {
	if db == nil || taramaID == 0 {
		return
	}
	_, _ = db.Exec(
		`UPDATE av_taramalar SET bitis=NOW(), durum='tamam', taranan=?, enfekte=? WHERE id=?`,
		taranan, enfekte, taramaID)
}

// domainCache — /home/c_X → domain_id eşlemesini önbellekler (tarama başına
// binlerce dosya için tekrar sorgu israf).
type domainCache struct {
	db   *sql.DB
	skID map[string]int64 // sistem_kullanici → domain_id
}

func yeniDomainCache(db *sql.DB) *domainCache {
	return &domainCache{db: db, skID: map[string]int64{}}
}

func (c *domainCache) domainID(yol string) int64 {
	if c.db == nil {
		return 0
	}
	sk := skCikar(yol)
	if sk == "" {
		return 0
	}
	if id, ok := c.skID[sk]; ok {
		return id
	}
	var id int64
	_ = c.db.QueryRow(`SELECT id FROM domains WHERE sistem_kullanici=? LIMIT 1`, sk).Scan(&id)
	c.skID[sk] = id // 0 da önbelleklenir (tekrar sorgulamayı önler)
	return id
}

// skCikar — /home/c_X/... → c_X.
func skCikar(yol string) string {
	p := filepath.ToSlash(yol)
	if !strings.HasPrefix(p, "/home/") {
		return ""
	}
	parca := strings.SplitN(strings.TrimPrefix(p, "/home/"), "/", 2)
	if len(parca) >= 1 && strings.HasPrefix(parca[0], "c_") {
		return parca[0]
	}
	return ""
}

// bulguYaz — bir bulguyu av_bulgular'a yazar; karantina alındıysa yollarını da.
// Döner: eklenen bulgunun id'si (bildirim referansı için).
func bulguYaz(db *sql.DB, taramaID, domainID int64, b avmotor.Bulgu, karantinaYol string) int64 {
	if db == nil {
		return 0
	}
	kar := 0
	durum := "aktif"
	if karantinaYol != "" {
		kar = 1
		durum = "karantina"
	}
	res, err := db.Exec(
		`INSERT INTO av_bulgular
		   (tarama_id, domain_id, dosya, imza, motor, seviye, puan,
		    orijinal_yol, karantina_yol, karantina, durum)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		taramaID, domainID, b.Dosya, kisalt(b.Aciklama, 250), "gosp/"+string(b.Seviye),
		string(b.Seviye), b.Puan, b.Dosya, karantinaYol, kar, durum)
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// bildirimYaz — cp_bildirim'e bir kayıt. domainID=0 → panel-geneli (admin).
func bildirimYaz(db *sql.DB, seviye, baslik, mesaj string, domainID int64, refTur string, refID int64) {
	if db == nil {
		return
	}
	var dom any
	if domainID > 0 {
		dom = domainID
	} else {
		dom = nil
	}
	// Sahip reseller_id'yi coz -> bildirim gorunurlugu domain sahibine kilitlenir
	// (bkz. internal/bildirim.kapsam). domainID=0 / reseller_id=0 -> NULL (admin).
	var rid any
	if domainID > 0 {
		var r int64
		if db.QueryRow(`SELECT reseller_id FROM domains WHERE id=?`, domainID).Scan(&r) == nil && r > 0 {
			rid = r
		}
	}
	_, _ = db.Exec(
		`INSERT INTO cp_bildirim (seviye, kategori, baslik, mesaj, domain_id, reseller_id, ref_tur, ref_id)
		 VALUES (?, 'antivirus', ?, ?, ?, ?, ?, ?)`,
		seviye, kisalt(baslik, 190), mesaj, dom, rid, refTur, refID)
}

// domainAdi -- bildirim basligi icin alan adi (yoksa "").
func domainAdi(db *sql.DB, domainID int64) string {
	if db == nil || domainID <= 0 {
		return ""
	}
	var ad string
	_ = db.QueryRow(`SELECT alan_adi FROM domains WHERE id=?`, domainID).Scan(&ad)
	return ad
}

func kisalt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var _ = time.Now // (ileride tarih biçimleme için import korunur)
