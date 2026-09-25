// trafik.go — per-domain AYLIK trafik sayacı.
// nginx access.log'undan ($body_bytes_sent) artımlı okuma yapar (ofset+boyut imleci ile
// log-rotasyonuna dayanıklı), her satırın tarihine göre aylık kovaya toplar ve
// içinde bulunulan ayın toplamını domains.trafik_kb alanına yazar (dashboard buradan okur).
// Kök neden (Bug 2): trafik_kb HİÇBİR YERDE yazılmıyordu → hep 0 görünüyordu.
package istatistik

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// StartTrafikAggregator: panel başlangıcında çağrılır, kendi goroutine'inde periyodik toplar.
func StartTrafikAggregator(db *sql.DB, every time.Duration) {
	go func() {
		// açılıştan 30 sn sonra ilk toplama (migration + servisler otursun)
		time.Sleep(30 * time.Second)
		AggregateAll(db)
		t := time.NewTicker(every)
		defer t.Stop()
		for range t.C {
			AggregateAll(db)
		}
	}()
}

// AggregateAll: tüm domainler için trafiği toplar, işlenen domain sayısını döner.
func AggregateAll(db *sql.DB) int {
	rows, err := db.Query(`SELECT id, alan_adi FROM domains`)
	if err != nil {
		log.Printf("trafik: domain listesi: %v", err)
		return 0
	}
	type dom struct {
		id      int64
		alanAdi string
	}
	var liste []dom
	for rows.Next() {
		var d dom
		if err := rows.Scan(&d.id, &d.alanAdi); err == nil {
			liste = append(liste, d)
		}
	}
	rows.Close()

	n := 0
	for _, d := range liste {
		if aggregateDomain(db, d.id, d.alanAdi) {
			n++
		}
	}
	return n
}

// aggregateDomain: tek domain'in access.log'unu artımlı okuyup aylık trafiği günceller.
func aggregateDomain(db *sql.DB, domainID int64, alanAdi string) bool {
	logPath := "/var/log/nginx/" + alanAdi + ".access.log"
	fi, err := os.Stat(logPath)
	if err != nil {
		// log yok → yine de içinde bulunulan ay kovasından trafik_kb tazele
		refreshTrafikKB(db, domainID)
		return false
	}
	size := fi.Size()

	var ofset, boyut int64
	_ = db.QueryRow(`SELECT ofset, boyut FROM domain_trafik_imlec WHERE domain_id=?`, domainID).Scan(&ofset, &boyut)

	start := ofset
	// rotasyon/truncate tespiti: dosya küçüldüyse baştan oku
	if size < ofset {
		start = 0
	}
	if start == size {
		refreshTrafikKB(db, domainID)
		return true
	}

	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			start = 0
			_, _ = f.Seek(0, 0)
		}
	}

	reader := bufio.NewReaderSize(f, 256*1024)
	aylik := map[string]int64{}
	consumed := start
	for {
		line, rerr := reader.ReadString('\n')
		if len(line) > 0 && strings.HasSuffix(line, "\n") {
			consumed += int64(len(line))
			if ay, byt, ok := parseTrafikSatir(line); ok {
				aylik[ay] += byt
			}
		}
		if rerr != nil {
			break // EOF veya kısmi son satır → ofset ilerletme
		}
	}

	// aylık kovaları upsert et
	for ay, byt := range aylik {
		if byt <= 0 {
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO domain_trafik(domain_id, yil_ay, bytes) VALUES(?,?,?)
			 ON DUPLICATE KEY UPDATE bytes=bytes+VALUES(bytes)`,
			domainID, ay, byt); err != nil {
			log.Printf("trafik upsert (domain=%d ay=%s): %v", domainID, ay, err)
		}
	}

	// imleci güncelle
	_, _ = db.Exec(
		`INSERT INTO domain_trafik_imlec(domain_id, ofset, boyut) VALUES(?,?,?)
		 ON DUPLICATE KEY UPDATE ofset=VALUES(ofset), boyut=VALUES(boyut)`,
		domainID, consumed, size)

	refreshTrafikKB(db, domainID)
	return true
}

// refreshTrafikKB: içinde bulunulan ayın toplamını domains.trafik_kb'ye yazar (KB).
func refreshTrafikKB(db *sql.DB, domainID int64) {
	ay := time.Now().UTC().Format("2006-01")
	var byt int64
	_ = db.QueryRow(`SELECT bytes FROM domain_trafik WHERE domain_id=? AND yil_ay=?`, domainID, ay).Scan(&byt)
	_, _ = db.Exec(`UPDATE domains SET trafik_kb=? WHERE id=?`, byt/1024, domainID)
}

// AyTrafik: domain_trafik'in tek aylık kovası.
type AyTrafik struct {
	Ay      string `json:"ay"` // "2026-09"
	GelenB  int64  `json:"gelen_b"`
	GidenB  int64  `json:"giden_b"`
	ToplamB int64  `json:"toplam_b"`
}

// TrafikGecmisi: GET /domains/{id}/trafik-gecmis yanıtı (eskiden yeniye).
type TrafikGecmisi struct {
	Kalemler []AyTrafik `json:"kalemler"`
	// GelenOlculmuyor: sayaç yalnız nginx $body_bytes_sent'i (sunucudan istemciye
	// giden yanıt gövdesi) toplar; gelen istek baytı hiçbir yerde ölçülmüyor. gelen_b
	// bu yüzden 0'dır ve ölçüm DEĞİLDİR (toplam_b = giden_b). Ön yüz gelen serisini çizmemeli.
	GelenOlculmuyor bool `json:"gelen_olculmuyor"`
}

// TrafikGecmis: son `ay` ayın (1..24, varsayılan 12) aylık trafiği; kovası olmayan
// ay 0. "Bu ay" refreshTrafikKB ile aynı (UTC). Yetki: rota MusteriScope arkasında.
func (h *Handlers) TrafikGecmis(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ay := 12
	if s := r.URL.Query().Get("ay"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 24 {
			httpx.WriteError(w, http.StatusBadRequest, "ay 1 ile 24 arasında olmalı")
			return
		}
		ay = n
	}
	ctx := r.Context()
	var bir int
	err := h.DB.QueryRowContext(ctx, `SELECT 1 FROM domains WHERE id=?`, id).Scan(&bir)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		log.Printf("trafik geçmişi: domain okunamadı (id=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		return
	}
	kalemler, err := aylikTrafik(ctx, h.DB, id, ay, time.Now().UTC())
	if err != nil {
		log.Printf("trafik geçmişi (domain=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "trafik geçmişi okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, TrafikGecmisi{Kalemler: kalemler, GelenOlculmuyor: true})
}

// aylikTrafik: simdi'nin ayı dahil geriye `ay` ay; eksik aylar 0, eskiden yeniye.
func aylikTrafik(ctx context.Context, db *sql.DB, domainID int64, ay int, simdi time.Time) ([]AyTrafik, error) {
	bas := time.Date(simdi.Year(), simdi.Month()-time.Month(ay-1), 1, 0, 0, 0, 0, time.UTC)
	kalemler := make([]AyTrafik, ay)
	sira := make(map[string]int, ay)
	for i := range kalemler {
		k := bas.AddDate(0, i, 0).Format("2006-01")
		kalemler[i].Ay = k
		sira[k] = i
	}
	rows, err := db.QueryContext(ctx,
		`SELECT yil_ay, bytes FROM domain_trafik WHERE domain_id=? AND yil_ay BETWEEN ? AND ?`,
		domainID, kalemler[0].Ay, kalemler[ay-1].Ay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var yilAy string
		var bayt int64
		if err := rows.Scan(&yilAy, &bayt); err != nil {
			return nil, err
		}
		if i, ok := sira[yilAy]; ok {
			kalemler[i].GidenB = bayt
			kalemler[i].ToplamB = bayt
		}
	}
	return kalemler, rows.Err()
}

// parseTrafikSatir: combined log satırından (ay 'YYYY-MM', bytes) çıkarır.
// Format: IP - - [17/Jul/2026:12:00:00 +0000] "GET / HTTP/1.1" 200 1234 "..." "..."
func parseTrafikSatir(line string) (string, int64, bool) {
	m := reLog.FindStringSubmatch(line)
	if m == nil {
		return "", 0, false
	}
	// m[2] = tarih kısmı (ör. 17/Jul/2026), m[6] = bytes (veya "-")
	t, err := time.Parse("02/Jan/2006", strings.TrimSpace(m[2]))
	if err != nil {
		return "", 0, false
	}
	byt := int64(0)
	if m[6] != "-" {
		byt, _ = strconv.ParseInt(m[6], 10, 64)
	}
	return t.Format("2006-01"), byt, true
}
