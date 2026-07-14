// Package istatistik: per-domain nginx access.log trafik analizi (salt-okunur).
package istatistik

import (
	"bufio"
	"database/sql"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

type KV struct {
	Ad   string `json:"ad"`
	Sayi int    `json:"sayi"`
}
type Gun struct {
	Tarih string `json:"tarih"`
	Istek int    `json:"istek"`
}
type Ozet struct {
	AlanAdi      string         `json:"alan_adi"`
	LogVar       bool           `json:"log_var"`
	ToplamIstek  int            `json:"toplam_istek"`
	ToplamBantMB float64        `json:"toplam_bant_mb"`
	TekilIP      int            `json:"tekil_ip"`
	BotOrani     int            `json:"bot_orani"` // yüzde
	DurumGrup    map[string]int `json:"durum_grup"`
	TopYollar    []KV           `json:"top_yollar"`
	TopIP        []KV           `json:"top_ip"`
	TopDurum     []KV           `json:"top_durum"`
	Gunluk       []Gun          `json:"gunluk"`
	SonIstekler  []string       `json:"son_istekler"`
}

// combined log format: IP - - [date] "METHOD path proto" status bytes "ref" "ua"
var reLog = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^:]+):[^\]]+\] "(\S+) (\S+)[^"]*" (\d{3}) (\d+|-) "[^"]*" "([^"]*)"`)

const maxSatir = 200000

func topN(m map[string]int, n int) []KV {
	out := make([]KV, 0, len(m))
	for k, v := range m {
		out = append(out, KV{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sayi != out[j].Sayi {
			return out[i].Sayi > out[j].Sayi
		}
		return out[i].Ad < out[j].Ad
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	o := Ozet{AlanAdi: alanAdi, DurumGrup: map[string]int{"2xx": 0, "3xx": 0, "4xx": 0, "5xx": 0}}

	logPath := "/var/log/nginx/" + alanAdi + ".access.log"
	f, err := os.Open(logPath)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, o) // log yok → boş özet
		return
	}
	defer f.Close()
	o.LogVar = true

	yollar := map[string]int{}
	ipler := map[string]int{}
	durum := map[string]int{}
	gunler := map[string]int{}
	var son []string
	var toplamBytes int64
	botKeys := []string{"bot", "spider", "crawl", "slurp", "bingpreview", "facebookexternal", "curl", "wget", "python", "go-http"}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	satir := 0
	for sc.Scan() {
		line := sc.Text()
		m := reLog.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		satir++
		if satir > maxSatir {
			break
		}
		ip, tarih, metot, yol, st, byt, ua := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
		o.ToplamIstek++
		ipler[ip]++
		// yol normalizasyonu (query string at)
		if i := strings.IndexByte(yol, '?'); i >= 0 {
			yol = yol[:i]
		}
		if len(yol) > 80 {
			yol = yol[:80]
		}
		yollar[metot+" "+yol]++
		durum[st]++
		switch st[0] {
		case '2':
			o.DurumGrup["2xx"]++
		case '3':
			o.DurumGrup["3xx"]++
		case '4':
			o.DurumGrup["4xx"]++
		case '5':
			o.DurumGrup["5xx"]++
		}
		gunler[tarih]++
		if byt != "-" {
			if b, e := strconv.ParseInt(byt, 10, 64); e == nil {
				toplamBytes += b
			}
		}
		uaLow := strings.ToLower(ua)
		for _, bk := range botKeys {
			if strings.Contains(uaLow, bk) {
				o.BotOrani++ // önce sayı, sonra yüzdeye çevrilecek
				break
			}
		}
		if len(son) < 40 {
			son = append(son, st+" "+metot+" "+yol+" ("+ip+")")
		}
	}
	o.TekilIP = len(ipler)
	o.ToplamBantMB = float64(toplamBytes) / (1024 * 1024)
	if o.ToplamIstek > 0 {
		o.BotOrani = o.BotOrani * 100 / o.ToplamIstek
	}
	o.TopYollar = topN(yollar, 10)
	o.TopIP = topN(ipler, 10)
	o.TopDurum = topN(durum, 8)
	// son istekleri ters çevir (en yeni üstte) — son 40'ın son 20'si
	for i := len(son) - 1; i >= 0 && len(o.SonIstekler) < 20; i-- {
		o.SonIstekler = append(o.SonIstekler, son[i])
	}
	// günlük: gün-adı sıralı son 7
	gk := make([]string, 0, len(gunler))
	for k := range gunler {
		gk = append(gk, k)
	}
	sort.Strings(gk)
	if len(gk) > 7 {
		gk = gk[len(gk)-7:]
	}
	for _, k := range gk {
		o.Gunluk = append(o.Gunluk, Gun{k, gunler[k]})
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}
