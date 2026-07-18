// Package composer: per-domain PHP Composer çalıştırma (whitelist + domain kullanıcısı olarak).
package composer

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

const composerBin = "/usr/local/bin/composer"

var rePkg = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*)/[a-z0-9]([a-z0-9._-]*)(:[\^~<>=0-9.* |,-]+)?$`)

func (h *Handlers) load(r *http.Request) (id int64, sk string, demo bool, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).Scan(&sk, &isDemo); err != nil {
		return id, "", false, false
	}
	return id, sk, isDemo == 1, true
}

// GET /domains/{id}/composer — durum (composer kurulu mu, composer.json var mı)
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	_, sk, _, ok := h.load(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	var surum string
	kurulu := false
	vc := exec.Command(composerBin, "--version", "--no-ansi")
	vc.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"COMPOSER_HOME=/tmp",
	}
	if out, err := vc.Output(); err == nil { // stdout-only: stderr plugin-uyarısını dışla
		kurulu = true
		surum = strings.TrimSpace(string(out))
	}
	_, jErr := os.Stat("/home/" + sk + "/public_html/composer.json")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kurulu":        kurulu,
		"surum":         surum,
		"composer_json": jErr == nil,
		"kullanici":     sk,
		"dizin":         "/home/" + sk + "/public_html",
	})
}

// POST /domains/{id}/composer  body {"komut":"install|update|dump-autoload|validate|require|remove","paket":"vendor/pkg"}
func (h *Handlers) Calistir(w http.ResponseWriter, r *http.Request) {
	_, sk, demo, ok := h.load(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde composer çalıştırılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	if _, err := os.Stat(composerBin); err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "composer sunucuda kurulu değil")
		return
	}
	var req struct {
		Komut string `json:"komut"`
		Paket string `json:"paket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	allowed := map[string]bool{"install": true, "update": true, "dump-autoload": true, "validate": true, "require": true, "remove": true, "show": true}
	if !allowed[req.Komut] {
		httpx.WriteError(w, http.StatusBadRequest, "izin verilmeyen komut")
		return
	}
	dizin := "/home/" + sk + "/public_html"
	// argv EXPLICIT (shell yok → enjeksiyon yok)
	args := []string{"-u", sk, "--", composerBin, req.Komut, "--no-interaction", "--no-ansi", "-d", dizin}
	// Guvenlik: script/plugin calistirmayi engelle (rasgele kod + env-sizinti vektoru)
	switch req.Komut {
	case "install", "update", "require", "remove", "dump-autoload":
		args = append(args, "--no-scripts", "--no-plugins")
	}
	if req.Komut == "require" || req.Komut == "remove" {
		pkg := strings.TrimSpace(req.Paket)
		if !rePkg.MatchString(pkg) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz paket adı (vendor/paket[:sürüm] biçiminde olmalı)")
			return
		}
		args = append(args, pkg)
	}
	cmd := exec.Command("runuser", args...)
	// Guvenlik: panel sirlarini (PANEL_JWT_SECRET, PANEL_DB_DSN, ...) alt-surece VERME
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/home/" + sk,
		"COMPOSER_HOME=/home/" + sk + "/.composer",
		"COMPOSER_ALLOW_SUPERUSER=0",
	}
	out, err := cmd.CombinedOutput()
	cikti := string(out)
	if len(cikti) > 20000 {
		cikti = cikti[len(cikti)-20000:]
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    err == nil,
		"komut": req.Komut,
		"cikti": cikti,
	})
}
