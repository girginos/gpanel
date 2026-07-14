package monitor

// Sistem yük (load average) geçmişi: periyodik örnekleme + dashboard grafiği endpoint'i.

import (
	"database/sql"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

// StartYukSampler: her `every` sürede /proc/loadavg + /proc/meminfo örnekler, sistem_yuk'e yazar.
// Retention: 7 gün (yaklaşık saatte bir budar). Panic-güvenli.
func StartYukSampler(db *sql.DB, every time.Duration) {
	go func() {
		defer func() { _ = recover() }()
		yukOrnekle(db) // açılışta hemen bir nokta
		t := time.NewTicker(every)
		defer t.Stop()
		var n int
		for range t.C {
			yukOrnekle(db)
			if n++; n%60 == 0 {
				_, _ = db.Exec(`DELETE FROM sistem_yuk WHERE ts < NOW() - INTERVAL 7 DAY`)
			}
		}
	}()
}

func yukOrnekle(db *sql.DB) {
	y1, y5, y15 := okuLoad()
	mem := okuBellekYuzde()
	_, _ = db.Exec(`INSERT INTO sistem_yuk (yuk1, yuk5, yuk15, bellek_yuzde) VALUES (?,?,?,?)`, y1, y5, y15, mem)
}

func okuLoad() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(f[0], 64)
	c, _ := strconv.ParseFloat(f[1], 64)
	d, _ := strconv.ParseFloat(f[2], 64)
	return a, c, d
}

func okuBellekYuzde() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, avail float64
	for _, line := range strings.Split(string(b), "\n") {
		ff := strings.Fields(line)
		if len(ff) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(ff[1], 64)
		switch ff[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total <= 0 {
		return 0
	}
	return math.Round(10000*(total-avail)/total) / 100
}

type YukNokta struct {
	Ts     string  `json:"ts"`
	Yuk1   float64 `json:"yuk1"`
	Yuk5   float64 `json:"yuk5"`
	Yuk15  float64 `json:"yuk15"`
	Bellek float64 `json:"bellek"`
}

// GET /system/load-history?saat=24  (1..168) — bucket'lanmış (≤ ~500 nokta) yük serisi
func (h *Handlers) YukGecmisi(w http.ResponseWriter, r *http.Request) {
	saat := 24
	if s := r.URL.Query().Get("saat"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= 168 {
			saat = v
		}
	}
	bucket := saat * 3600 / 500 // saniye — hedef ~500 nokta
	if bucket < 60 {
		bucket = 60
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT MIN(ts)          AS ts,
		       ROUND(AVG(yuk1),2),
		       ROUND(AVG(yuk5),2),
		       ROUND(AVG(yuk15),2),
		       ROUND(AVG(bellek_yuzde),1)
		  FROM sistem_yuk
		 WHERE ts >= NOW() - INTERVAL ? HOUR
		 GROUP BY FLOOR(UNIX_TIMESTAMP(ts) / ?)
		 ORDER BY ts`, saat, bucket)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yük geçmişi okunamadı")
		return
	}
	defer rows.Close()
	out := []YukNokta{}
	for rows.Next() {
		var p YukNokta
		if err := rows.Scan(&p.Ts, &p.Yuk1, &p.Yuk5, &p.Yuk15, &p.Bellek); err == nil {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yük geçmişi hatası")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"saat": saat, "cekirdek": cekirdekSayisi(), "noktalar": out})
}

func cekirdekSayisi() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "processor\t")
}
