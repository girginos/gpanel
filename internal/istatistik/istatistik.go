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

// acc: birden fazla access.log'u tek özete toplayan biriktirici.
type acc struct {
	o                            *Ozet
	yollar, ipler, durum, gunler map[string]int
	son                          []string
	toplamBytes                  int64
	botSayi                      int
}

func newAcc(o *Ozet) *acc {
	return &acc{o: o, yollar: map[string]int{}, ipler: map[string]int{}, durum: map[string]int{}, gunler: map[string]int{}}
}

var botKeys = []string{"bot", "spider", "crawl", "slurp", "bingpreview", "facebookexternal", "curl", "wget", "python", "go-http"}

// parseFile: tek log dosyasını biriktiriciye ekler. Dosya yoksa false döner.
func (a *acc) parseFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	satir := 0
	for sc.Scan() {
		m := reLog.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		satir++
		if satir > maxSatir {
			break
		}
		ip, tarih, metot, yol, st, byt, ua := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
		a.o.ToplamIstek++
		a.ipler[ip]++
		if i := strings.IndexByte(yol, '?'); i >= 0 {
			yol = yol[:i]
		}
		if len(yol) > 80 {
			yol = yol[:80]
		}
		a.yollar[metot+" "+yol]++
		a.durum[st]++
		switch st[0] {
		case '2':
			a.o.DurumGrup["2xx"]++
		case '3':
			a.o.DurumGrup["3xx"]++
		case '4':
			a.o.DurumGrup["4xx"]++
		case '5':
			a.o.DurumGrup["5xx"]++
		}
		a.gunler[tarih]++
		if byt != "-" {
			if b, e := strconv.ParseInt(byt, 10, 64); e == nil {
				a.toplamBytes += b
			}
		}
		uaLow := strings.ToLower(ua)
		for _, bk := range botKeys {
			if strings.Contains(uaLow, bk) {
				a.botSayi++
				break
			}
		}
		if len(a.son) < 60 {
			a.son = append(a.son, st+" "+metot+" "+yol+" ("+ip+")")
		}
	}
	return true
}

// logYollari: {sid} varsa yalnız o subdomain'in logu; yoksa domain'in kendi logu
// + TÜM subdomain'lerinin logları (parent panelinde toplanır).
func (h *Handlers) logYollari(r *http.Request, id int64, alanAdi string) (host string, paths []string) {
	if sidStr := chi.URLParam(r, "sid"); sidStr != "" {
		sid, _ := strconv.ParseInt(sidStr, 10, 64)
		var tamAd string
		if err := h.DB.QueryRow(`SELECT tam_ad FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&tamAd); err == nil && tamAd != "" {
			return tamAd, []string{"/var/log/nginx/" + tamAd + ".access.log"}
		}
	}
	paths = []string{"/var/log/nginx/" + alanAdi + ".access.log"}
	rows, err := h.DB.Query(`SELECT tam_ad FROM subdomanlar WHERE domain_id=?`, id)
	if err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil && t != "" {
				paths = append(paths, "/var/log/nginx/"+t+".access.log")
			}
		}
		rows.Close()
	}
	return alanAdi, paths
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	host, paths := h.logYollari(r, id, alanAdi)
	o := Ozet{AlanAdi: host, DurumGrup: map[string]int{"2xx": 0, "3xx": 0, "4xx": 0, "5xx": 0}}
	a := newAcc(&o)
	for _, p := range paths {
		if a.parseFile(p) {
			o.LogVar = true
		}
	}
	o.TekilIP = len(a.ipler)
	o.ToplamBantMB = float64(a.toplamBytes) / (1024 * 1024)
	if o.ToplamIstek > 0 {
		o.BotOrani = a.botSayi * 100 / o.ToplamIstek
	}
	o.TopYollar = topN(a.yollar, 10)
	o.TopIP = topN(a.ipler, 10)
	o.TopDurum = topN(a.durum, 8)
	for i := len(a.son) - 1; i >= 0 && len(o.SonIstekler) < 20; i-- {
		o.SonIstekler = append(o.SonIstekler, a.son[i])
	}
	gk := make([]string, 0, len(a.gunler))
	for k := range a.gunler {
		gk = append(gk, k)
	}
	sort.Strings(gk)
	if len(gk) > 7 {
		gk = gk[len(gk)-7:]
	}
	for _, k := range gk {
		o.Gunluk = append(o.Gunluk, Gun{k, a.gunler[k]})
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}
