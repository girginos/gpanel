package laravel

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

// kayit: cp_laravel_apps satırı
type kayit struct {
	Mevcut          bool
	AppRoot         string
	DeployMode      string
	PhpSurum        string
	NodeSurum       string
	ScheduleEnabled bool
	QueueEnabled    bool
	QueueTimeout    int
	QueueMaxJobs    int
	QueueConnection string
	Maintenance     bool
	LastCommit      string
	SonDeployDurum  string
}

// lookup: domain temel bilgisi + demo/kullanıcı gate
func (h *Handlers) lookup(r *http.Request) (id int64, sk, phpSurum string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(php_surum,'8.3'), is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &phpSurum, &isDemo); err != nil {
		return id, "", "", false, false
	}
	return id, sk, phpSurum, isDemo == 1, true
}

// getKayit: cp_laravel_apps satırını oku (yoksa Mevcut=false)
func (h *Handlers) getKayit(ctx context.Context, id int64) kayit {
	var k kayit
	var sched, queue, maint int
	err := h.DB.QueryRowContext(ctx,
		`SELECT app_root, deploy_mode, php_surum, node_surum, schedule_enabled, queue_enabled,
		        queue_timeout, queue_max_jobs, queue_connection, maintenance, last_commit, son_deploy_durum
		 FROM cp_laravel_apps WHERE domain_id=?`, id).
		Scan(&k.AppRoot, &k.DeployMode, &k.PhpSurum, &k.NodeSurum, &sched, &queue,
			&k.QueueTimeout, &k.QueueMaxJobs, &k.QueueConnection, &maint, &k.LastCommit, &k.SonDeployDurum)
	if err != nil {
		return kayit{AppRoot: "public_html", QueueTimeout: 60, QueueMaxJobs: 1000, QueueConnection: "database"}
	}
	k.Mevcut = true
	k.ScheduleEnabled = sched == 1
	k.QueueEnabled = queue == 1
	k.Maintenance = maint == 1
	return k
}

// upsertKayit: cp_laravel_apps satırını oluştur/güncelle (yalnız verilen alanlar)
func (h *Handlers) upsertTemel(ctx context.Context, id int64, appRoot, deployMode, php, node string) error {
	_, err := h.DB.ExecContext(ctx,
		`INSERT INTO cp_laravel_apps(domain_id, app_root, deploy_mode, php_surum, node_surum)
		 VALUES(?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE app_root=VALUES(app_root), deploy_mode=VALUES(deploy_mode),
		   php_surum=VALUES(php_surum), node_surum=VALUES(node_surum)`,
		id, appRoot, deployMode, php, node)
	return err
}

// laravelKurulu: appDir'de artisan + composer.json var mı (Laravel tespiti)
func laravelKurulu(appDir string) (artisan, composerJSON bool) {
	if _, err := os.Stat(filepath.Join(appDir, "artisan")); err == nil {
		artisan = true
	}
	if _, err := os.Stat(filepath.Join(appDir, "composer.json")); err == nil {
		composerJSON = true
	}
	return
}

// bakimAktif: artisan down işareti (Laravel 8+ maintenance.php veya eski down)
func bakimAktif(appDir string) bool {
	for _, p := range []string{"storage/framework/maintenance.php", "storage/framework/down"} {
		if _, err := os.Stat(filepath.Join(appDir, p)); err == nil {
			return true
		}
	}
	return false
}

// GET /domains/{id}/laravel — keşif/durum
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	k := h.getKayit(r.Context(), id)
	appRoot := k.AppRoot
	if appRoot == "" {
		appRoot = "public_html"
	}
	appDir, err := guvenliAppDir(sk, appRoot)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	artisan, composerJSON := laravelKurulu(appDir)
	// php sürümü: kayıt > domain
	php := k.PhpSurum
	if php == "" {
		php = phpSurum
	}
	// git özeti
	gitVar := false
	lastCommit := ""
	if _, e := os.Stat(filepath.Join(appDir, ".git")); e == nil {
		gitVar = true
		if out, ok := TenantExec(r.Context(), sk, appDir, "/usr/bin/git", "-C", appDir, "rev-parse", "--short", "HEAD"); ok {
			lastCommit = strings.TrimSpace(out)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kurulu":           artisan,
		"kayit_var":        k.Mevcut,
		"app_root":         appRoot,
		"kullanici":        sk,
		"dizin":            appDir,
		"php_surum":        php,
		"node_surum":       k.NodeSurum,
		"composer_json":    composerJSON,
		"git_var":          gitVar,
		"last_commit":      lastCommit,
		"bakim":            bakimAktif(appDir),
		"schedule_enabled": k.ScheduleEnabled,
		"queue_enabled":    k.QueueEnabled,
		"queue_timeout":    k.QueueTimeout,
		"queue_max_jobs":   k.QueueMaxJobs,
		"queue_connection": k.QueueConnection,
		"son_deploy_durum": k.SonDeployDurum,
		"php_binary":       phpBin(php),
	})
}
