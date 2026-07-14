// Package logs: per-domain nginx log dosyalari + SSE canli tail
package logs

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

type LogDosya struct {
	Anahtar  string `json:"anahtar"`  // "access" | "error"
	Etiket   string `json:"etiket"`
	Yol      string `json:"yol"`
	BoyutB   int64  `json:"boyut_b"`
	Degisme  string `json:"degisme"`
	Mevcut   bool   `json:"mevcut"`
}

func (h *Handlers) lookup(r *http.Request) (string, string, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", os.ErrNotExist
	}
	if err != nil {
		return "", "", err
	}
	if isDemo == 1 {
		return "", "", errors.New("demo aboneliğin logları yönetilemez")
	}
	return alanAdi, sk, nil
}

func dosyaYolu(alanAdi, anahtar string) string {
	switch anahtar {
	case "access":
		return "/var/log/nginx/" + alanAdi + ".access.log"
	case "error":
		return "/var/log/nginx/" + alanAdi + ".error.log"
	}
	return ""
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	alanAdi, _, err := h.lookup(r)
	if err != nil {
		st := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			st = http.StatusNotFound
		} else if strings.Contains(err.Error(), "demo") {
			st = http.StatusForbidden
		}
		httpx.WriteError(w, st, err.Error())
		return
	}
	out := []LogDosya{}
	for _, k := range []struct{ Anahtar, Etiket string }{
		{"access", "Erişim (access)"},
		{"error", "Hata (error)"},
	} {
		p := dosyaYolu(alanAdi, k.Anahtar)
		entry := LogDosya{Anahtar: k.Anahtar, Etiket: k.Etiket, Yol: p}
		if st, err := os.Stat(p); err == nil {
			entry.Mevcut = true
			entry.BoyutB = st.Size()
			entry.Degisme = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, entry)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// Son N satiri okur (default 200, max 2000)
func (h *Handlers) Read(w http.ResponseWriter, r *http.Request) {
	alanAdi, _, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anahtar := r.URL.Query().Get("dosya")
	if anahtar == "" {
		anahtar = "access"
	}
	p := dosyaYolu(alanAdi, anahtar)
	if p == "" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz dosya anahtarı")
		return
	}
	son, _ := strconv.Atoi(r.URL.Query().Get("son"))
	if son <= 0 {
		son = 200
	}
	if son > 2000 {
		son = 2000
	}

	satirlar, err := sonNSatir(p, son)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"dosya": anahtar, "yol": p, "satirlar": []string{}, "mevcut": false,
			})
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"dosya": anahtar, "yol": p, "satirlar": satirlar, "mevcut": true,
	})
}

// SSE canli tail: log dosyasinin sonuna seek, yeni satirlari "data: ..." ile pushlar
func (h *Handlers) Tail(w http.ResponseWriter, r *http.Request) {
	alanAdi, _, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anahtar := r.URL.Query().Get("dosya")
	if anahtar == "" {
		anahtar = "access"
	}
	p := dosyaYolu(alanAdi, anahtar)
	if p == "" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz dosya anahtarı")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, "stream desteklenmiyor")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, ": tail %s baslatildi\n\n", anahtar)
	flusher.Flush()

	f, err := os.Open(p)
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: hata\ndata: dosya açılamadı: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	defer f.Close()
	// Önce son ~200 satırı gönder
	if mevcut, err := sonNSatir(p, 200); err == nil {
		for _, ln := range mevcut {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(ln, "\n", " "))
		}
		flusher.Flush()
	}
	// Sona atla
	_, _ = f.Seek(0, io.SeekEnd)

	reader := bufio.NewReader(f)
	ctx := r.Context()
	tick := time.NewTicker(15 * time.Second) // keepalive
	defer tick.Stop()

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return
		}
		if line != "" {
			ln := strings.TrimRight(line, "\n\r")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(ln, "\n", " "))
			flusher.Flush()
		}
		if err == io.EOF {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_, _ = fmt.Fprintln(w, ": keepalive")
				flusher.Flush()
			case <-time.After(500 * time.Millisecond):
				// dosya rotate vs. olabilir; size sıfırlandıysa yeniden aç
				if st, err := os.Stat(p); err == nil {
					if cur, _ := f.Seek(0, io.SeekCurrent); cur > st.Size() {
						f.Close()
						f, err = os.Open(p)
						if err != nil {
							return
						}
						reader = bufio.NewReader(f)
					}
				}
			}
		}
	}
}

// sonNSatir: dosyanın sonundan N satır oku
func sonNSatir(yol string, n int) ([]string, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	const blok = 8192
	var buf []byte
	var read int64
	// Dosyanın sonundan 8K parçalar halinde ileri-geri oku
	for read < size && countLines(buf) < n+1 {
		read += blok
		if read > size {
			read = size
		}
		_, _ = f.Seek(-read, io.SeekEnd)
		tmp := make([]byte, read)
		_, _ = f.ReadAt(tmp, size-read)
		buf = tmp
	}
	// Satırlara böl
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// helper for filepath import order
var _ = filepath.Base
