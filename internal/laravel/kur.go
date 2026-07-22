package laravel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"
)

// setupUnit: kurulum/deploy için transient unit adı
func setupUnit(id int64) string   { return fmt.Sprintf("girginos-lt-kur-%d", id) }
func setupLog(id int64) string    { return fmt.Sprintf("%s/kur-%d.log", logRootDizin, id) }
func setupScript(id int64) string { return fmt.Sprintf("/run/girginos-lt-setup-%d.sh", id) }

// mkdirTenant: dizini TENANT OLARAK oluştur (runuser mkdir). Root MkdirAll+chown
// symlink'li ebeveyni takip edip keyfi konumda dizin yaratıp tenant'a chown ederdi
// (denetim #4); tenant olarak yaratınca kendi izinleri dışına çıkamaz.
func mkdirTenant(sk, dir string) error {
	out, err := exec.Command("runuser", "-u", sk, "--", "/bin/mkdir", "-p", dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %v: %s", err, string(out))
	}
	return nil
}

// setDocroot: domains.web_root'u <altDizin>'e ayarla + vhost yeniden render.
// altDizin public_html'e göre relative ("public" veya "myapp/public"). Dizin var olmalı.
func (h *Handlers) setDocroot(ctx context.Context, id int64, sk, altDizin string) error {
	abs, err := provisioner.WebRootMutlak(sk, altDizin)
	if err != nil {
		return err
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE domains SET web_root=? WHERE id=?`, abs, id); err != nil {
		return err
	}
	return provisioner.RerenderVhost(h.DB, id)
}

// altDizinPublic: app_root ("public_html" | "public_html/x") → docroot alt dizini
// ("public" | "x/public")
func altDizinPublic(appRoot string) string {
	ar := strings.TrimPrefix(strings.Trim(appRoot, "/"), "public_html")
	ar = strings.Trim(ar, "/")
	if ar == "" {
		return "public"
	}
	return ar + "/public"
}

type kurReq struct {
	Mode    string `json:"mode"`     // yerel | uzak | iskele
	RepoURL string `json:"repo_url"` // uzak için
	Branch  string `json:"branch"`   // uzak için (varsayılan main)
	AppRoot string `json:"app_root"` // public_html | public_html/alt
}

// POST /domains/{id}/laravel/kur
func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde Laravel kurulamaz")
		return
	}
	defer kilitle(id)() // domain başına serileştir (yarış/çift-unit önlenir — denetim #9)
	var mevcutDurum string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(son_deploy_durum,'') FROM cp_laravel_apps WHERE domain_id=?`, id).Scan(&mevcutDurum)
	if mevcutDurum == "kuruluyor" || mevcutDurum == "calisiyor" {
		httpx.WriteError(w, http.StatusConflict, "bu domain için bir işlem (kurulum/dağıtım) zaten sürüyor")
		return
	}
	var req kurReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Mode != "yerel" && req.Mode != "uzak" && req.Mode != "iskele" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz mod")
		return
	}
	appDir, err := guvenliAppDir(sk, req.AppRoot)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	appRoot := strings.Trim(strings.TrimSpace(req.AppRoot), "/")
	if appRoot == "" {
		appRoot = "public_html"
	}
	if err := mkdirTenant(sk, appDir); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "dizin oluşturulamadı: "+err.Error())
		return
	}
	php := phpBin(phpSurum)
	logPath := setupLog(id)
	tmp := "/home/" + sk + "/.lt-skel-" + fmt.Sprint(id)

	// kaydı hemen oluştur (kuruluyor)
	_ = h.upsertTemel(r.Context(), id, appRoot, req.Mode, phpSurum, "")
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET son_deploy_durum='kuruluyor' WHERE domain_id=?`, id)

	switch req.Mode {
	case "yerel":
		// git init senkron (hızlı)
		out, gok := TenantExec(r.Context(), sk, appDir, "/usr/bin/git", "init")
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET son_deploy_durum=? WHERE domain_id=?`,
			map[bool]string{true: "hazir", false: "hata"}[gok], id)
		// public zaten varsa docroot ayarla (yoksa ilk deploy'da)
		if _, e := os.Stat(filepath.Join(appDir, "public")); e == nil {
			_ = h.setDocroot(r.Context(), id, sk, altDizinPublic(appRoot))
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": gok, "async": false, "cikti": out,
			"mesaj": "Boş git deposu oluşturuldu. Kodunuzu push edip Dağıtım sekmesinden yayınlayın."})
		return

	case "uzak":
		if !gecerliRepoURL(req.RepoURL) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz repo URL (https:// veya git@ olmalı)")
			return
		}
		branch := strings.TrimSpace(req.Branch)
		if branch == "" {
			branch = "main"
		}
		if !reArg.MatchString(branch) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz branch adı")
			return
		}
		script := kurScriptUzak(appDir, req.RepoURL, branch, php, tmp)
		if err := detachedKur(id, sk, appDir, logPath, script); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

	case "iskele":
		script := kurScriptIskele(appDir, php, tmp)
		if err := detachedKur(id, sk, appDir, logPath, script); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "async": true, "unit": setupUnit(id),
		"mesaj": "Kurulum başlatıldı; ilerleme için durum sorgulanıyor.",
	})
}

// detachedKur: root-sahipli setup script'i yaz (/run, tenant değiştiremez) + tenant
// olarak detached çalıştır
func detachedKur(id int64, sk, appDir, logPath, script string) error {
	sp := setupScript(id)
	if err := os.WriteFile(sp, []byte(script), 0755); err != nil {
		return fmt.Errorf("setup script yazılamadı: %v", err)
	}
	// önceki aynı-isimli unit varsa temizle
	_ = exec.Command("systemctl", "reset-failed", setupUnit(id)+".service").Run()
	return systemdRunDetached(sk, appDir, setupUnit(id), logPath, "/bin/bash", sp)
}

// kurScriptIskele: composer create-project. create-project BOŞ dizin ister → dizin
// doluysa geçici dizine kurup içeriği birleştiriyoruz (mevcut dosyalar korunur).
// Tüm değerler server-hesaplı (enjeksiyon yok).
func kurScriptIskele(appDir, php, tmp string) string {
	cp := php + " " + composerBin + " create-project --no-interaction --prefer-dist"
	return "#!/bin/bash\nset -e\n" +
		"DEST=" + shq(appDir) + "\nTMP=" + shq(tmp) + "\n" +
		"if [ -z \"$(ls -A \"$DEST\" 2>/dev/null)\" ]; then\n" +
		"  " + cp + " laravel/laravel \"$DEST\" || " + cp + " laravel/laravel:^11 \"$DEST\"\n" +
		"else\n" +
		"  echo '(dizin dolu — geçici dizine kurup birleştiriliyor)'\n" +
		"  rm -rf \"$TMP\"\n" +
		"  " + cp + " laravel/laravel \"$TMP\" || " + cp + " laravel/laravel:^11 \"$TMP\"\n" +
		"  cp -a \"$TMP\"/. \"$DEST\"/\n" +
		"  rm -rf \"$TMP\"\n" +
		"fi\n" +
		"cd \"$DEST\"\n" +
		"[ -f .env ] || { [ -f .env.example ] && cp .env.example .env; }\n" +
		php + " artisan key:generate --force || true\n"
}

// kurScriptUzak: git clone (boş dizin ister → daima geçici dizine klonla + birleştir)
// + composer install + key:generate
func kurScriptUzak(appDir, repoURL, branch, php, tmp string) string {
	return "#!/bin/bash\nset -e\n" +
		"DEST=" + shq(appDir) + "\nTMP=" + shq(tmp) + "\n" +
		"rm -rf \"$TMP\"\n" +
		"/usr/bin/git clone --depth 1 --branch " + shq(branch) + " -- " + shq(repoURL) + " \"$TMP\"\n" +
		"cp -a \"$TMP\"/. \"$DEST\"/\n" +
		"rm -rf \"$TMP\"\n" +
		"cd \"$DEST\"\n" +
		"[ -f .env ] || { [ -f .env.example ] && cp .env.example .env; }\n" +
		php + " " + composerBin + " install --no-interaction --prefer-dist || true\n" +
		"[ -f artisan ] && " + php + " artisan key:generate --force || true\n"
}

// shq: tek-tırnak shell-quote (server-hesaplı değerler için ek güvenlik). İçerde
// tek tırnak zaten regex/validasyonla dışlanmış; yine de kaçış uygula.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// GET /domains/{id}/laravel/kur/durum
func (h *Handlers) KurDurum(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	unit := setupUnit(id) + ".service"
	st := unitDurum(unit)
	logTail := dosyaTail(setupLog(id), 8<<10)
	calisiyor := st == "activating" || st == "active" || st == "reloading"
	k := h.getKayit(r.Context(), id)
	appDir, _ := guvenliAppDir(sk, k.AppRoot)
	// bittiyse: docroot ayarla + durumu güncelle (bir kez)
	if !calisiyor && k.SonDeployDurum == "kuruluyor" {
		artisan, _ := laravelKurulu(appDir)
		durum := "hata"
		if artisan {
			durum = "hazir"
			if _, e := os.Stat(filepath.Join(appDir, "public")); e == nil {
				_ = h.setDocroot(r.Context(), id, sk, altDizinPublic(k.AppRoot))
			}
		}
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET son_deploy_durum=? WHERE domain_id=?`, durum, id)
		_ = exec.Command("systemctl", "reset-failed", unit).Run()
		_ = os.Remove(setupScript(id))
		k.SonDeployDurum = durum
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"calisiyor": calisiyor, "durum": k.SonDeployDurum, "log": logTail,
	})
}
