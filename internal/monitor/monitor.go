// Package monitor: ek izleme endpoint'leri — top işlemler + domain HTTP sağlık probe'u
package monitor

import (
	"crypto/tls"
	"database/sql"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Process struct {
	PID   int     `json:"pid"`
	User  string  `json:"user"`
	CPU   float64 `json:"cpu_yuzde"`
	Mem   float64 `json:"mem_yuzde"`
	Komut string  `json:"komut"`
}

type Handlers struct {
	DB *sql.DB
}

// GET /system/processes?n=15&sirala=cpu|mem
func Processes(w http.ResponseWriter, r *http.Request) {
	n := 15
	if s := r.URL.Query().Get("n"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 100 {
			n = v
		}
	}
	sortBy := r.URL.Query().Get("sirala")
	sortFlag := "-pcpu"
	if sortBy == "mem" {
		sortFlag = "-pmem"
	}

	cmd := exec.Command("ps", "-eo", "pid,user:32,pcpu,pmem,args", "--no-headers", "--sort="+sortFlag)
	out, err := cmd.Output()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ps okunamadı: "+err.Error())
		return
	}
	lines := strings.Split(string(out), "\n")
	procs := make([]Process, 0, n)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		cpu, _ := strconv.ParseFloat(f[2], 64)
		mem, _ := strconv.ParseFloat(f[3], 64)
		komut := strings.Join(f[4:], " ")
		if len(komut) > 120 {
			komut = komut[:120] + "…"
		}
		procs = append(procs, Process{PID: pid, User: f[1], CPU: cpu, Mem: mem, Komut: komut})
		if len(procs) >= n {
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, procs)
}

type SSLBilgi struct {
	Gecerli      bool   `json:"gecerli"`
	BitisTarihi  string `json:"bitis_tarihi"`
	KalanGun     int    `json:"kalan_gun"`
	Cikaran      string `json:"cikaran,omitempty"`
	OznName      string `json:"ozne,omitempty"`
}

type DomainHealth struct {
	URL           string    `json:"url"`
	DurumKodu     int       `json:"durum_kodu"`
	YanitSuresiMs float64   `json:"yanit_suresi_ms"`
	Erisilebilir  bool      `json:"erisilebilir"`
	Hata          string    `json:"hata,omitempty"`
	Sema          string    `json:"sema"` // "http" | "https"
	SSL           *SSLBilgi `json:"ssl,omitempty"`
	Boyut         int64     `json:"boyut_byte"`
	Server        string    `json:"server,omitempty"`
}

// GET /domains/{id}/health
// HTTPS önce dener; bağlanamazsa HTTP'ye düşer. SSL bilgisi cert'ten okunur.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, ipv4 string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, ipv4 FROM domains WHERE id=?`, id).Scan(&alanAdi, &ipv4)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB hata: "+err.Error())
		return
	}

	res := probe("https://" + alanAdi)
	res.Sema = "https"
	if !res.Erisilebilir {
		// HTTPS başarısız → HTTP'ye düş
		alt := probe("http://" + alanAdi)
		if alt.Erisilebilir {
			alt.Sema = "http"
			httpx.WriteJSON(w, http.StatusOK, alt)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func probe(targetURL string) DomainHealth {
	res := DomainHealth{URL: targetURL}

	tlsCfg := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	tr := &http.Transport{
		TLSClientConfig:   tlsCfg,
		DisableKeepAlives: true,
		ResponseHeaderTimeout: 6 * time.Second,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   8 * time.Second,
		// 5 yönlendirmeye kadar takip et
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	start := time.Now()
	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", "GirginOSPanel-Monitor/1.0")
	req.Header.Set("Accept", "text/html,*/*")
	resp, err := client.Do(req)
	res.YanitSuresiMs = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		res.Hata = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Erisilebilir = true
	res.DurumKodu = resp.StatusCode
	res.Server = resp.Header.Get("Server")
	res.Boyut = resp.ContentLength

	// SSL cert bilgisi (TLS bağlantısı varsa)
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		c := resp.TLS.PeerCertificates[0]
		now := time.Now()
		kalan := int(c.NotAfter.Sub(now).Hours() / 24)
		res.SSL = &SSLBilgi{
			Gecerli:     now.Before(c.NotAfter) && now.After(c.NotBefore),
			BitisTarihi: c.NotAfter.Format("2006-01-02"),
			KalanGun:    kalan,
			Cikaran:     c.Issuer.CommonName,
			OznName:     c.Subject.CommonName,
		}
	}
	return res
}
