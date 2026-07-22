package laravel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"girginospanel/internal/httpx"
)

func deployUnit(id int64) string    { return fmt.Sprintf("girginos-lt-deploy-%d", id) }
func deployLog(id int64) string     { return fmt.Sprintf("%s/deploy-%d.log", logRootDizin, id) }
func deployScriptP(id int64) string { return fmt.Sprintf("/run/girginos-lt-deploy-%d.sh", id) }

// deployScript: bakım-sarmalı yayın. set -e YOK (up her zaman çalışmalı); her adım ||true.
func deployScript(appDir, php, nodeDir string, migrate, npmBuild bool) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("export PATH=" + nodeDir + ":" + sistemPATH + "\n")
	b.WriteString("cd " + shq(appDir) + " || exit 1\n")
	b.WriteString("echo '== bakım moduna al =='\n")
	b.WriteString(php + " artisan down || true\n")
	b.WriteString("echo '== git pull =='\n")
	b.WriteString("if [ -d .git ]; then git pull --ff-only 2>&1 || git pull 2>&1 || true; else echo '(git deposu değil, atlandı)'; fi\n")
	b.WriteString("echo '== composer install (--no-dev) =='\n")
	b.WriteString(php + " " + composerBin + " install --no-interaction --prefer-dist --no-dev 2>&1 || true\n")
	if npmBuild {
		b.WriteString("echo '== npm ci + build =='\n")
		b.WriteString(nodeDir + "/npm ci --prefix " + shq(appDir) + " --no-fund --no-audit 2>&1 || " + nodeDir + "/npm install --prefix " + shq(appDir) + " 2>&1 || true\n")
		b.WriteString(nodeDir + "/npm run build --prefix " + shq(appDir) + " 2>&1 || true\n")
	}
	if migrate {
		b.WriteString("echo '== migrate --force =='\n")
		b.WriteString(php + " artisan migrate --force 2>&1 || true\n")
	}
	b.WriteString("echo '== cache =='\n")
	b.WriteString(php + " artisan config:cache 2>&1 || true\n")
	b.WriteString(php + " artisan route:cache 2>&1 || true\n")
	b.WriteString("echo '== bakım modundan çıkar =='\n")
	b.WriteString(php + " artisan up || true\n")
	b.WriteString("echo '== DEPLOY BİTTİ =='\n")
	return b.String()
}

// POST /domains/{id}/laravel/deploy  {"migrate":true,"npm_build":true,"node_surum":"22"}
func (h *Handlers) Deploy(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde deploy yapılamaz")
		return
	}
	defer kilitle(id)() // domain başına serileştir (yarış/çift-unit önlenir — denetim #9)
	var mevcutDurum string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(son_deploy_durum,'') FROM cp_laravel_apps WHERE domain_id=?`, id).Scan(&mevcutDurum)
	if mevcutDurum == "kuruluyor" || mevcutDurum == "calisiyor" {
		httpx.WriteError(w, http.StatusConflict, "bu domain için bir işlem (kurulum/dağıtım) zaten sürüyor")
		return
	}
	var req struct {
		Migrate   bool   `json:"migrate"`
		NpmBuild  bool   `json:"npm_build"`
		NodeSurum string `json:"node_surum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	nodeDir := nodeBinDir(req.NodeSurum)
	script := deployScript(appDir, phpBin(phpSurum), nodeDir, req.Migrate, req.NpmBuild)
	sp := deployScriptP(id)
	if err := os.WriteFile(sp, []byte(script), 0755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "deploy script yazılamadı")
		return
	}
	_ = exec.Command("systemctl", "reset-failed", deployUnit(id)+".service").Run()
	if err := systemdRunDetached(sk, appDir, deployUnit(id), deployLog(id), "/bin/bash", sp); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET son_deploy_durum='calisiyor', son_deploy_at=NOW() WHERE domain_id=?`, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": deployUnit(id)})
}

// GET /domains/{id}/laravel/deploy/durum
func (h *Handlers) DeployDurum(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	unit := deployUnit(id) + ".service"
	st := unitDurum(unit)
	calisiyor := st == "activating" || st == "active" || st == "reloading"
	logTail := dosyaTail(deployLog(id), 16<<10)
	k := h.getKayit(r.Context(), id)
	if !calisiyor && k.SonDeployDurum == "calisiyor" {
		durum := "basarili"
		if !strings.Contains(logTail, "DEPLOY BİTTİ") {
			durum = "hata"
		}
		appDir, _ := guvenliAppDir(sk, k.AppRoot)
		lastCommit := ""
		if out, gok := TenantExec(r.Context(), sk, appDir, "/usr/bin/git", "-C", appDir, "rev-parse", "--short", "HEAD"); gok {
			lastCommit = strings.TrimSpace(out)
		}
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE cp_laravel_apps SET son_deploy_durum=?, last_commit=? WHERE domain_id=?`, durum, lastCommit, id)
		_ = exec.Command("systemctl", "reset-failed", unit).Run()
		_ = os.Remove(deployScriptP(id))
		k.SonDeployDurum = durum
		k.LastCommit = lastCommit
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"calisiyor": calisiyor, "durum": k.SonDeployDurum, "last_commit": k.LastCommit, "log": logTail,
	})
}
