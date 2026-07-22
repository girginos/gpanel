package laravel

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"girginospanel/internal/httpx"
)

// ---- artisan ----

var artisanIzin = map[string]bool{
	"migrate": true, "migrate:status": true, "migrate:rollback": true, "migrate:fresh": true,
	"db:seed": true, "config:cache": true, "config:clear": true, "route:cache": true,
	"route:clear": true, "route:list": true, "view:cache": true, "view:clear": true,
	"cache:clear": true, "optimize": true, "optimize:clear": true, "queue:restart": true,
	"queue:failed": true, "queue:retry": true, "storage:link": true, "key:generate": true,
	"down": true, "up": true, "schedule:run": true, "schedule:list": true, "about": true,
	"event:list": true, "migrate:install": true,
}

var reArtisanArg = regexp.MustCompile(`^(--?[A-Za-z0-9][A-Za-z0-9=:_./-]*|[A-Za-z0-9][A-Za-z0-9=:_./-]*)$`)

// çalışılacak app dizinini çöz (kayıt app_root)
func (h *Handlers) appDizin(r *http.Request, id int64, sk string) (string, error) {
	k := h.getKayit(r.Context(), id)
	return guvenliAppDir(sk, k.AppRoot)
}

// POST /domains/{id}/laravel/artisan  {"komut":"migrate --force"}
func (h *Handlers) Artisan(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde artisan çalıştırılamaz")
		return
	}
	var req struct {
		Komut string `json:"komut"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	parts := strings.Fields(strings.TrimSpace(req.Komut))
	if len(parts) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "komut boş")
		return
	}
	alt := parts[0]
	if !artisanIzin[alt] {
		httpx.WriteError(w, http.StatusBadRequest, "izin verilmeyen artisan komutu: "+alt+" (tinker ve keyfi komutlar kapalı)")
		return
	}
	argv := []string{"artisan", alt, "--no-interaction"}
	for _, a := range parts[1:] {
		if !reArtisanArg.MatchString(a) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz argüman: "+a)
			return
		}
		argv = append(argv, a)
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, cok := TenantExec(r.Context(), sk, appDir, phpBin(phpSurum), argv...)
	if bakimAktif(appDir) != (alt == "down") && (alt == "down" || alt == "up") {
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET maintenance=? WHERE domain_id=?`,
			map[bool]int{true: 1, false: 0}[alt == "down"], id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": cok, "komut": "artisan " + strings.Join(parts, " "), "cikti": out})
}

// ---- composer (scriptli — operatör kararı: Laravel package:discover çalışsın) ----

var composerIzin = map[string]bool{
	"install": true, "update": true, "require": true, "remove": true,
	"dump-autoload": true, "validate": true, "show": true, "diagnose": true,
}

// POST /domains/{id}/laravel/composer  {"komut":"install","paket":"vendor/pkg"}
func (h *Handlers) Composer(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde composer çalıştırılamaz")
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
	if !composerIzin[req.Komut] {
		httpx.WriteError(w, http.StatusBadRequest, "izin verilmeyen composer komutu")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// scriptler AÇIK (operatör kararı) — Laravel package:discover çalışsın. root DEĞİL.
	argv := []string{composerBin, req.Komut, "--no-interaction", "--no-ansi", "-d", appDir}
	if req.Komut == "install" || req.Komut == "update" {
		argv = append(argv, "--prefer-dist")
	}
	if req.Komut == "require" || req.Komut == "remove" {
		pkg := strings.TrimSpace(req.Paket)
		if !reComposerPkg.MatchString(pkg) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz paket adı (vendor/paket[:sürüm])")
			return
		}
		argv = append(argv, pkg)
	}
	out, cok := TenantExec(r.Context(), sk, appDir, phpBin(phpSurum), argv...)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": cok, "komut": "composer " + req.Komut, "cikti": out})
}

// ---- node / npm ----

const nDizin = "/usr/local/n/versions/node"

// kuruluNodeSurumleri: n ile kurulu ana sürümler (örn ["20","22"]); yoksa sistem node
func kuruluNodeSurumleri() []string {
	majSet := map[string]bool{}
	if ents, err := os.ReadDir(nDizin); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				maj := strings.SplitN(e.Name(), ".", 2)[0]
				majSet[maj] = true
			}
		}
	}
	if len(majSet) == 0 {
		// sistem node (n kurulu değil)
		if _, err := os.Stat("/usr/bin/npm"); err == nil {
			return []string{"sistem"}
		}
		return []string{}
	}
	out := make([]string, 0, len(majSet))
	for m := range majSet {
		out = append(out, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// nodeBinDir: istenen ana sürüm için node/npm'in bulunduğu DİZİN. Bulunamazsa sistem.
func nodeBinDir(surum string) string {
	surum = strings.TrimSpace(surum)
	if surum != "" && surum != "sistem" {
		if ents, err := os.ReadDir(nDizin); err == nil {
			var eslesen []string
			for _, e := range ents {
				if e.IsDir() && (e.Name() == surum || strings.HasPrefix(e.Name(), surum+".")) {
					eslesen = append(eslesen, e.Name())
				}
			}
			if len(eslesen) > 0 {
				sort.Sort(sort.Reverse(sort.StringSlice(eslesen)))
				cand := filepath.Join(nDizin, eslesen[0], "bin")
				if _, err := os.Stat(filepath.Join(cand, "npm")); err == nil {
					return cand
				}
			}
		}
	}
	return "/usr/bin" // sistem
}

var npmIzin = map[string]bool{
	"install": true, "ci": true, "run": true, "prune": true, "ls": true,
	"outdated": true, "audit": true, "--version": true,
}
var reNpmScript = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_-]*$`) // baştaki tire yok (flag-injection sertleştirme — denetim #8)

// POST /domains/{id}/laravel/npm  {"komut":"ci","script":"build","node_surum":"22","ignore_scripts":true}
func (h *Handlers) Npm(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde npm çalıştırılamaz")
		return
	}
	var req struct {
		Komut         string `json:"komut"`
		Script        string `json:"script"`
		NodeSurum     string `json:"node_surum"`
		IgnoreScripts bool   `json:"ignore_scripts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if !npmIzin[req.Komut] {
		httpx.WriteError(w, http.StatusBadRequest, "izin verilmeyen npm komutu")
		return
	}
	if req.NodeSurum != "" && req.NodeSurum != "sistem" && !reNodeSurum.MatchString(req.NodeSurum) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz node sürümü")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	binDir := nodeBinDir(req.NodeSurum)
	npmBin := filepath.Join(binDir, "npm")
	if _, err := os.Stat(npmBin); err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "node/npm sunucuda kurulu değil")
		return
	}
	argv := []string{req.Komut, "--prefix", appDir, "--no-fund", "--no-audit"}
	if req.IgnoreScripts {
		argv = append(argv, "--ignore-scripts")
	}
	if req.Komut == "run" {
		s := strings.TrimSpace(req.Script)
		if !reNpmScript.MatchString(s) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz script adı")
			return
		}
		argv = []string{"run", s, "--prefix", appDir}
	}
	// PATH'e node bin dizinini başa ekle (npm bir node script'i)
	env := tenantEnv(sk)
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + binDir + ":" + sistemPATH
		}
	}
	out, cok := tenantExecEnv(r.Context(), sk, appDir, env, npmBin, argv...)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": cok, "komut": "npm " + req.Komut, "cikti": out, "node_dir": binDir})
}

// GET /domains/{id}/laravel/node — kurulu node sürümleri
func (h *Handlers) NodeSurumler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"surumler": kuruluNodeSurumleri()})
}
