// Package zincir — FAZ 2 Attack Chain Engine. 3-AJAN DOĞRULAMA SONRASI SERTLEŞTİRME.
//
// 🔴 NEDEN: Faz 0 (dosya) ve Faz 1 (süreç) TEKİL sinyaller üretir. Saldırı bir
// ZİNCİRDİR: shell.php yazıldı → php-fpm→bash → /tmp/x indirildi → cron.
//
// 🔴🔴 FP-LAUNDERING KAPISI: salt ZAMANSAL korelasyon (yalnız kiracı+pencere) iki
// BAĞIMSIZ yanlış-pozitifi tek "kritik saldırı zinciri"ne yıkardı. Bu yüzden
// NEDENSELLİK (aynı dosya yolu / aynı süreç pid'i / aynı dizin) ve ZAMAN-SIRASI
// güvene ağırlık verir; nedensel bağ YOKSA zincir "kritik"e ESKALE OLMAZ (uyarı
// kalır). Ölçme > varsayım: sabit "kritik" bonusu kaldırıldı (dosya olayı hep
// kritik yazıldığından o bir ofsetti, sinyal değil).
package zincir

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/bildirim"
)

const (
	pencereDk     = 15   // korelasyon penceresi (dakika)
	tickSaniye    = 20   // tarama aralığı
	yenidenDk     = 30   // aynı imzayı yeniden bildirmeden önce
	domainDk      = 30   // domain başına bildirim cooldown (imzadan BAĞIMSIZ)
	domainMaxBild = 3    // cooldown penceresinde domain başına en çok zincir bildirimi
	olayLimit     = 500  // domain başına tek turda çekilecek en çok olay (bellek sınırı)
	saklamaGun    = 7    // av_olay/av_zincir retention (gün)
	temizlikDk    = 60   // retention job aralığı (dakika)
	sorguTimeout  = 5 * time.Second
)

// Kill-chain aşamaları.
var asamaSira = map[string]int{"giris": 0, "dosya_yazma": 1, "calistirma": 2, "c2": 3, "persistence": 4}
var asamaAd = map[string]string{
	"giris": "Initial Access", "dosya_yazma": "File Write", "calistirma": "Execution",
	"c2": "C2", "persistence": "Persistence",
}

// Olay — korelasyon için av_olay satırı (yol/pid nedensellik için).
type Olay struct {
	Asama  string
	Seviye string
	Yol    string
	Pid    int
	Zaman  time.Time
}

// Sonuc — ZincirPuanla çıktısı.
type Sonuc struct {
	Guven    int
	Asamalar []string
	Seviye   string // uyari | kritik
	Nedensel bool
	Yeterli  bool
}

// ── Saf çekirdek (test edilebilir) ──────────────────────────────────────────

// ZincirPuanla — olaylardan güven + aşama dizisi + severity.
// KURAL: ≥2 DISTINCT aşama (tek sinyal zincir değil). Güven: taban + NEDENSELLİK
// (yol/pid örtüşmesi) + ZAMAN-SIRASI. Severity KRİTİK ancak nedensel bağ VEYA
// 3+ sıralı aşama olduğunda (iki bağımsız düşük sinyal kritik ÜRETMEZ).
func ZincirPuanla(olaylar []Olay) Sonuc {
	set := map[string]bool{}
	for _, o := range olaylar {
		if _, ok := asamaSira[o.Asama]; !ok {
			continue // bilinmeyen aşama → yoksay (magic-sentinel değil, existence check)
		}
		set[o.Asama] = true
	}
	if len(set) < 2 {
		return Sonuc{}
	}
	var asamalar []string
	for a := range set {
		asamalar = append(asamalar, a)
	}
	sort.Slice(asamalar, func(i, j int) bool { return asamaSira[asamalar[i]] < asamaSira[asamalar[j]] })

	distinct := len(set)
	nedensel := nedenselBag(olaylar)
	sirali := siraliMi(olaylar, asamalar)

	// Taban: 2→55, 3→70, 4→85, 5→100(cap). Bonuslar: nedensel +25, sırali +5.
	guven := 40 + (distinct-1)*15
	if nedensel {
		guven += 25
	}
	if sirali {
		guven += 5
	}
	if guven > 99 {
		guven = 99
	}
	// KRİTİK yalnız nedensel bağ VEYA 3+ SIRALI aşama. Aksi (salt zamansal 2 sinyal)
	// → uyari (FP-laundering'i keser).
	seviye := "uyari"
	if nedensel || (distinct >= 3 && sirali) {
		seviye = "kritik"
	}
	return Sonuc{Guven: guven, Asamalar: asamalar, Seviye: seviye, Nedensel: nedensel, Yeterli: true}
}

// nedenselBag — FARKLI aşamalardaki iki olay nedensel bağlı mı: aynı tam yol
// (düşen dosya çalıştırıldı), aynı dizin, ya da aynı pid (aynı süreç).
func nedenselBag(olaylar []Olay) bool {
	for i := range olaylar {
		for j := i + 1; j < len(olaylar); j++ {
			if olaylar[i].Asama == olaylar[j].Asama {
				continue
			}
			if olaylar[i].Pid > 0 && olaylar[i].Pid == olaylar[j].Pid {
				return true
			}
			if yolBagli(olaylar[i].Yol, olaylar[j].Yol) {
				return true
			}
		}
	}
	return false
}

// yolBagli — YALNIZ aynı tam yol nedensel sayılır (düşen dosya çalıştırıldı).
// "Aynı dizin" KASITLI DEĞİL: kiracı webroot'u (public_html) doğal olarak çok
// dosya barındırır → iki İLİŞKİSİZ tespit dizini paylaşır → FP-laundering geri
// açılırdı. İlişkisiz olaylar salt-zamansal kalır (uyarı), kritik değil.
func yolBagli(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.ToSlash(a) == filepath.ToSlash(b)
}

// siraliMi — aşamaların İLK görülme zamanları kill-chain sırasında (artan) mı.
// Geçersiz/zero zaman varsa sıralı sayılmaz (bonus verilmez).
func siraliMi(olaylar []Olay, asamalar []string) bool {
	ilk := map[string]time.Time{}
	for _, o := range olaylar {
		if o.Zaman.IsZero() {
			return false
		}
		if t, ok := ilk[o.Asama]; !ok || o.Zaman.Before(t) {
			ilk[o.Asama] = o.Zaman
		}
	}
	for i := 1; i < len(asamalar); i++ {
		if ilk[asamalar[i]].Before(ilk[asamalar[i-1]]) {
			return false
		}
	}
	return true
}

// ZincirImza — dedup: domain + sıralı aşama kümesi.
func ZincirImza(domID int64, asamalar []string) string {
	h := sha256.Sum256([]byte(strconv.FormatInt(domID, 10) + "|" + strings.Join(asamalar, ">")))
	return hex.EncodeToString(h[:])[:32]
}

func AsamaOzet(asamalar []string) string {
	var ad []string
	for _, a := range asamalar {
		if n := asamaAd[a]; n != "" {
			ad = append(ad, n)
		}
	}
	return strings.Join(ad, " → ")
}

// ── DB güdümlü korelasyon ───────────────────────────────────────────────────

// Baslat — periyodik korelasyon + retention. TEK örnek olarak çağrılmalı (tek
// goroutine → dedup check-then-insert yarışı yok).
func Baslat(db *sql.DB) {
	if db == nil {
		return
	}
	t := time.NewTicker(tickSaniye * time.Second)
	defer t.Stop()
	sonTemizlik := time.Now()
	for range t.C {
		// 🔴 panic-recover: bir korelasyon hatası TÜM paneli çökertmesin.
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("zincir korelasyon panic (kurtarıldı): %v", r)
				}
			}()
			if err := Calistir(db); err != nil {
				log.Printf("zincir korelasyon: %v", err)
			}
		}()
		if time.Since(sonTemizlik) > temizlikDk*time.Minute {
			temizle(db)
			sonTemizlik = time.Now()
		}
	}
}

// temizle — retention: eski av_olay/av_zincir satırlarını sil (sınırsız büyüme =
// disk/DB DoS'unu keser). Toplu-limitli.
func temizle(db *sql.DB) {
	ctx, iptal := context.WithTimeout(context.Background(), 30*time.Second)
	defer iptal()
	for _, tbl := range []string{"av_olay", "av_zincir"} {
		for i := 0; i < 20; i++ { // en çok 20×5000 satır/tur
			r, err := db.ExecContext(ctx,
				"DELETE FROM "+tbl+" WHERE created_at < (NOW() - INTERVAL ? DAY) LIMIT 5000", saklamaGun)
			if err != nil {
				log.Printf("zincir temizlik %s: %v", tbl, err)
				break
			}
			if n, _ := r.RowsAffected(); n < 5000 {
				break
			}
		}
	}
}

func Calistir(db *sql.DB) error {
	ctx, iptal := context.WithTimeout(context.Background(), sorguTimeout)
	defer iptal()
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT domain_id FROM av_olay
		WHERE domain_id IS NOT NULL AND created_at >= (NOW() - INTERVAL ? MINUTE)`, pencereDk)
	if err != nil {
		return err
	}
	var domlar []int64
	for rows.Next() {
		var d int64
		if rows.Scan(&d) == nil && d > 0 {
			domlar = append(domlar, d)
		}
	}
	rows.Close()

	for _, dom := range domlar {
		olaylar, err := domainOlaylari(db, dom)
		if err != nil || len(olaylar) < 2 {
			continue
		}
		s := ZincirPuanla(olaylar)
		if !s.Yeterli {
			continue
		}
		imza := ZincirImza(dom, s.Asamalar)
		if zatenBildirildi(db, imza) || domainDoygun(db, dom) {
			continue
		}
		zincirYaz(db, dom, s, len(olaylar), imza)
	}
	return nil
}

func domainOlaylari(db *sql.DB, domID int64) ([]Olay, error) {
	ctx, iptal := context.WithTimeout(context.Background(), sorguTimeout)
	defer iptal()
	rows, err := db.QueryContext(ctx, `SELECT asama, seviye, yol, pid, created_at FROM av_olay
		WHERE domain_id=? AND created_at >= (NOW() - INTERVAL ? MINUTE)
		ORDER BY created_at LIMIT ?`, domID, pencereDk, olayLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Olay
	for rows.Next() {
		var o Olay
		var yol sql.NullString
		var pid sql.NullInt64
		var ts sql.NullTime // 🔴 DSN parseTime=true → TIMESTAMP time.Time gelir (string DEĞİL)
		if rows.Scan(&o.Asama, &o.Seviye, &yol, &pid, &ts) == nil {
			o.Yol = yol.String
			o.Pid = int(pid.Int64)
			o.Zaman = ts.Time
			out = append(out, o)
		}
	}
	return out, nil
}

// zatenBildirildi — bu imza son yenidenDk içinde bildirildi mi. DB hatasında
// FAIL-CLOSED (true) → geçici hatada yinelenen bildirim yerine atla.
func zatenBildirildi(db *sql.DB, imza string) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM av_zincir
		WHERE imza=? AND created_at >= (NOW() - INTERVAL ? MINUTE)`, imza, yenidenDk).Scan(&n); err != nil {
		log.Printf("zincir dedup sorgusu: %v", err)
		return true
	}
	return n > 0
}

// domainDoygun — domain cooldown: imzadan BAĞIMSIZ, domain başına domainDk
// penceresinde en çok domainMaxBild zincir (aşama-alt-küme spam'ini keser).
func domainDoygun(db *sql.DB, domID int64) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM av_zincir
		WHERE domain_id=? AND created_at >= (NOW() - INTERVAL ? MINUTE)`, domID, domainDk).Scan(&n); err != nil {
		return true // fail-closed
	}
	return n >= domainMaxBild
}

func zincirYaz(db *sql.DB, domID int64, s Sonuc, olaySayisi int, imza string) {
	asamaStr := strings.Join(s.Asamalar, ">")
	// Tek sorgu: sahip reseller_id + alan adı (silinen domain edge-case + verimli).
	var rid any
	var r sql.NullInt64
	var alan string
	_ = db.QueryRow(`SELECT reseller_id, alan_adi FROM domains WHERE id=?`, domID).Scan(&r, &alan)
	if r.Valid && r.Int64 > 0 {
		rid = r.Int64
	}
	res, err := db.Exec(`INSERT INTO av_zincir
		(domain_id, reseller_id, asamalar, guven, olay_sayisi, imza)
		VALUES (?,?,?,?,?,?)`, domID, rid, asamaStr, s.Guven, olaySayisi, imza)
	if err != nil {
		log.Printf("zincir yazılamadı: %v", err)
		return
	}
	zid, _ := res.LastInsertId()

	baslik := "Saldırı zinciri tespit edildi"
	if alan != "" {
		baslik = alan + " — saldırı zinciri tespit edildi"
	}
	bag := "zamansal korelasyon"
	if s.Nedensel {
		bag = "nedensel bağ"
	}
	mesaj := AsamaOzet(s.Asamalar) + "  ·  güven %" + strconv.Itoa(s.Guven) + " (" + bag + ")"
	bildirim.Yaz(db, s.Seviye, "zincir", baslik, mesaj, domID, "av_zincir", zid)
	log.Printf("ZİNCİR [%s güven=%d nedensel=%v] domain=%d: %s", s.Seviye, s.Guven, s.Nedensel, domID, AsamaOzet(s.Asamalar))
}
