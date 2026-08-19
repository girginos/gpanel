// Package zincir — FAZ 2 Attack Chain Engine.
//
// 🔴 NEDEN: Faz 0 (dosya) ve Faz 1 (süreç) TEKİL sinyaller üretir. Saldırı ise
// bir ZİNCİRDİR: shell.php yazıldı → php-fpm→bash → /tmp/x indirildi → cron.
// Tek tek "uyarı" yerine bunları KİRACI + ZAMAN penceresinde birleştirip
// "saldırı zinciri, güven %X" demek — ürünü antivirüsten EDR'a taşıyan katman.
//
// Detektörler `av_olay`'a ASAMA-sınıflı olay yazar (avajan). Bu motor server'da
// periyodik koşar: penceredeki olayları kiracıya göre gruplar, ≥2 DISTINCT aşama
// varsa zincir ilan eder (av_zincir + izole bildirim). Tek aşama zincir DEĞİL.
package zincir

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/bildirim"
)

const (
	pencereDk  = 15 // korelasyon penceresi (dakika)
	tickSaniye = 20 // tarama aralığı
	yenidenDk  = 30 // aynı zincir imzasını yeniden bildirmeden önceki süre
)

// Kill-chain aşama sırası + insan-okunur adları.
var asamaSira = map[string]int{"giris": 0, "dosya_yazma": 1, "calistirma": 2, "c2": 3, "persistence": 4}
var asamaAd = map[string]string{
	"giris": "Initial Access", "dosya_yazma": "File Write", "calistirma": "Execution",
	"c2": "C2", "persistence": "Persistence",
}

// Olay — korelasyon için sadeleştirilmiş av_olay satırı.
type Olay struct {
	Asama  string
	Seviye string
	Zaman  time.Time
}

// ── Saf çekirdek (test edilebilir) ──────────────────────────────────────────

// ZincirPuanla — bir kiracının penceredeki olaylarından güven skoru + sıralı
// aşama dizisi. KURAL: en az 2 DISTINCT aşama (tek sinyal zincir değildir →
// yanlış-pozitif önler). Güven: aşama çeşitliliği + kritik + kill-chain sırası.
func ZincirPuanla(olaylar []Olay) (guven int, asamalar []string, yeterli bool) {
	set := map[string]bool{}
	kritik := false
	for _, o := range olaylar {
		if asamaSira[o.Asama] == 0 && o.Asama != "giris" {
			continue // bilinmeyen aşama → yoksay
		}
		set[o.Asama] = true
		if o.Seviye == "kritik" {
			kritik = true
		}
	}
	if len(set) < 2 {
		return 0, nil, false
	}
	for a := range set {
		asamalar = append(asamalar, a)
	}
	sort.Slice(asamalar, func(i, j int) bool { return asamaSira[asamalar[i]] < asamaSira[asamalar[j]] })

	// 2 aşama → 60, 3 → 80, 4 → 95, 5 → 99. Kritik olay +5, kill-chain sırası
	// (dosya_yazma→calistirma bitişikliği) +5.
	guven = 40 + (len(set)-1)*20
	if kritik {
		guven += 5
	}
	if set["dosya_yazma"] && set["calistirma"] {
		guven += 5 // klasik "yaz + çalıştır" — en güçlü zincir çekirdeği
	}
	if guven > 99 {
		guven = 99
	}
	return guven, asamalar, true
}

// ZincirImza — dedup anahtarı: domain + sıralı aşama dizisi (aynı zincir
// tekrar tekrar bildirilmesin).
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

// Baslat — periyodik korelasyon goroutine'i (server başlangıcında `go` ile).
func Baslat(db *sql.DB) {
	if db == nil {
		return
	}
	t := time.NewTicker(tickSaniye * time.Second)
	defer t.Stop()
	for range t.C {
		if err := Calistir(db); err != nil {
			log.Printf("zincir korelasyon: %v", err)
		}
	}
}

// Calistir — bir korelasyon turu: penceredeki olayı olan her kiracı için zincir
// değerlendir, yeni/yeterli zincirleri av_zincir'e yaz + bildir.
func Calistir(db *sql.DB) error {
	rows, err := db.Query(`SELECT DISTINCT domain_id FROM av_olay
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
		guven, asamalar, ok := ZincirPuanla(olaylar)
		if !ok {
			continue
		}
		imza := ZincirImza(dom, asamalar)
		if zatenBildirildi(db, imza) {
			continue // aynı zincir son yenidenDk içinde bildirildi
		}
		zincirYaz(db, dom, asamalar, guven, len(olaylar), imza)
	}
	return nil
}

func domainOlaylari(db *sql.DB, domID int64) ([]Olay, error) {
	rows, err := db.Query(`SELECT asama, seviye, created_at FROM av_olay
		WHERE domain_id=? AND created_at >= (NOW() - INTERVAL ? MINUTE)
		ORDER BY created_at`, domID, pencereDk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Olay
	for rows.Next() {
		var o Olay
		var ts string
		if rows.Scan(&o.Asama, &o.Seviye, &ts) == nil {
			o.Zaman, _ = time.Parse("2006-01-02 15:04:05", ts)
			out = append(out, o)
		}
	}
	return out, nil
}

// zatenBildirildi — bu imzada bir zincir son yenidenDk içinde yazıldı mı.
func zatenBildirildi(db *sql.DB, imza string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM av_zincir
		WHERE imza=? AND created_at >= (NOW() - INTERVAL ? MINUTE)`, imza, yenidenDk).Scan(&n)
	return n > 0
}

func zincirYaz(db *sql.DB, domID int64, asamalar []string, guven, olaySayisi int, imza string) {
	asamaStr := strings.Join(asamalar, ">")
	// Sahip reseller_id — bildirim izolasyonu (Faz 0/1 deseni).
	var rid any
	var r int64
	if db.QueryRow(`SELECT reseller_id FROM domains WHERE id=?`, domID).Scan(&r) == nil && r > 0 {
		rid = r
	}
	res, err := db.Exec(`INSERT INTO av_zincir
		(domain_id, reseller_id, asamalar, guven, olay_sayisi, imza)
		VALUES (?,?,?,?,?,?)`, domID, rid, asamaStr, guven, olaySayisi, imza)
	if err != nil {
		log.Printf("zincir yazılamadı: %v", err)
		return
	}
	zid, _ := res.LastInsertId()

	// İzole bildirim (bildirim.Yaz reseller_id çözer). Aşama sırasına göre başlık.
	var alan string
	_ = db.QueryRow(`SELECT alan_adi FROM domains WHERE id=?`, domID).Scan(&alan)
	baslik := "Saldırı zinciri tespit edildi"
	if alan != "" {
		baslik = alan + " — saldırı zinciri tespit edildi"
	}
	mesaj := AsamaOzet(asamalar) + "  ·  güven %" + strconv.Itoa(guven)
	seviye := "uyari"
	if guven >= 80 {
		seviye = "kritik"
	}
	bildirim.Yaz(db, seviye, "zincir", baslik, mesaj, domID, "av_zincir", zid)
	log.Printf("ZİNCİR [%s güven=%d] domain=%d: %s", seviye, guven, domID, AsamaOzet(asamalar))
}
