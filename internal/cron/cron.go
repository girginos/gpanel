// Package cron: domain user'in crontab'i icin CRUD
package cron

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const (
	maxGorev    = 100
	maxKomut    = 1024
	bannerLine  = "# girginospanel cron — bu dosya panel tarafindan yonetiliyor; elle duzenlemeyin"
)

type Gorev struct {
	Idx     int    `json:"idx"`
	Dakika  string `json:"dakika"`
	Saat    string `json:"saat"`
	Gun     string `json:"gun"`
	Ay      string `json:"ay"`
	Hafta   string `json:"hafta"`
	Komut   string `json:"komut"`
	Yorum   string `json:"yorum,omitempty"`
}

type Handlers struct {
	DB *sql.DB
}

var (
	errDemo = errors.New("demo aboneliğin cron'u yönetilemez")
	errBad  = errors.New("güvenlik: c_ prefix'siz user reddedildi")
)

func (h *Handlers) lookup(r *http.Request) (string, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if isDemo == 1 {
		return "", errDemo
	}
	if !strings.HasPrefix(sk, "c_") {
		return "", errBad
	}
	return sk, nil
}

// crontab path
func cronPath(sk string) string {
	return "/var/spool/cron/" + sk
}

func read(sk string) ([]Gorev, error) {
	p := cronPath(sk)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []Gorev{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := make([]Gorev, 0)
	sc := bufio.NewScanner(f)
	var lastYorum string
	idx := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			lastYorum = ""
			continue
		}
		if strings.HasPrefix(line, "#") {
			c := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			// kendi banner satirimizi atla
			if !strings.HasPrefix(c, "girginospanel") {
				lastYorum = c
			}
			continue
		}
		// 5 alan + komut
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		out = append(out, Gorev{
			Idx:    idx,
			Dakika: fields[0], Saat: fields[1], Gun: fields[2],
			Ay:     fields[3], Hafta: fields[4],
			Komut:  strings.Join(fields[5:], " "),
			Yorum:  lastYorum,
		})
		idx++
		lastYorum = ""
	}
	return out, sc.Err()
}

func write(sk string, list []Gorev) error {
	var buf bytes.Buffer
	buf.WriteString(bannerLine + "\n")
	buf.WriteString("# son güncelleme: " + sk + "\n\n")
	for _, g := range list {
		if g.Yorum != "" {
			fmt.Fprintf(&buf, "# %s\n", strings.ReplaceAll(g.Yorum, "\n", " "))
		}
		fmt.Fprintf(&buf, "%s %s %s %s %s %s\n",
			g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta, g.Komut)
	}
	p := cronPath(sk)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	_ = os.Chmod(p, 0600)
	// chown user:user
	if out, err := exec.Command("chown", sk+":"+sk, p).CombinedOutput(); err != nil {
		return fmt.Errorf("chown: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// SELinux context — system_cron_spool_t
	_, _ = exec.Command("restorecon", p).CombinedOutput()
	return nil
}

func validate(g Gorev) error {
	if g.Dakika == "" || g.Saat == "" || g.Gun == "" || g.Ay == "" || g.Hafta == "" {
		return fmt.Errorf("tüm zaman alanları zorunlu")
	}
	if g.Komut == "" {
		return fmt.Errorf("komut boş olamaz")
	}
	if len(g.Komut) > maxKomut {
		return fmt.Errorf("komut çok uzun (max %d)", maxKomut)
	}
	for _, f := range []string{g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta} {
		if strings.ContainsAny(f, ";|&`\n") {
			return fmt.Errorf("zaman alanlarında geçersiz karakter")
		}
	}
	if strings.ContainsAny(g.Komut, "\n\r") {
		return fmt.Errorf("komutta satır sonu olamaz")
	}
	return nil
}

func statusFromErr(err error) int {
	switch err {
	case os.ErrNotExist:
		return http.StatusNotFound
	case errDemo:
		return http.StatusForbidden
	case errBad:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sistem_kullanici": sk,
		"toplam":           len(list),
		"gorevler":         list,
	})
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var g Gorev
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if err := validate(g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(list) >= maxGorev {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("en fazla %d görev olabilir", maxGorev))
		return
	}
	list = append(list, g)
	if err := write(sk, list); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "idx": len(list) - 1})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "idx"))
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if idx < 0 || idx >= len(list) {
		httpx.WriteError(w, http.StatusNotFound, "index aralık dışında")
		return
	}
	silinen := list[idx]
	list = append(list[:idx], list[idx+1:]...)
	if err := write(sk, list); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": silinen})
}
