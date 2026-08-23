// Package php — subpool.go: alt alan (subdomain) başına AYRI PHP-FPM havuzu.
// Domain'in per-tenant mount-ns unit'ine dokunmadan, alt alanın seçtiği PHP
// sürümünün paylaşılan master'ında `<sk>_s<subID>` isimli ayrı bir pool açar:
// kendi soketi, kendi pm.* ve php_admin_value sertleştirmesi. Kullanıcı=sk +
// open_basedir=/home/<sk>/ olduğundan uid izolasyonu korunur; alt alan böylece
// parent'tan bağımsız PHP sürümü VE bağımsız PHP ayarları alır.
package php

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// SubPoolName: alt alan havuz/pool adı. Pool bölümü adıdır (Linux kullanıcısı DEĞİL).
func SubPoolName(sk string, subID int64) string {
	return sk + "_s" + strconv.FormatInt(subID, 10)
}

// subPoolTmpl: named pool — bölüm adı poolName, ama user/group = sk (gerçek Linux
// kullanıcısı), listen = sockDir/poolName.sock. Sertleştirme poolTmpl ile birebir.
var subPoolTmpl = template.Must(template.New("subpool").Funcs(template.FuncMap{"onoff": onoff, "aclSatiri": aclSatiri}).Parse(`[{{.Pool}}]
user = {{.SK}}
group = {{.SK}}
listen = {{.SockDir}}/{{.Pool}}.sock
listen.owner = nginx
listen.group = nginx
listen.mode = 0660
{{aclSatiri}}

pm = {{.S.PMStrategy}}
pm.max_children = {{.S.PMMaxChildren}}
pm.max_requests = {{.S.PMMaxRequests}}
pm.start_servers = {{.S.PMStartServers}}
pm.min_spare_servers = {{.S.PMMinSpareServers}}
pm.max_spare_servers = {{.S.PMMaxSpareServers}}
pm.process_idle_timeout = 30s

; ---- Performance & Security ----
php_admin_value[memory_limit] = {{.S.MemoryLimit}}
php_admin_value[max_execution_time] = {{.S.MaxExecutionTime}}
php_admin_value[max_input_time] = {{.S.MaxInputTime}}
php_admin_value[post_max_size] = {{.S.PostMaxSize}}
php_admin_value[upload_max_filesize] = {{.S.UploadMaxFilesize}}
php_admin_value[max_input_vars] = 10000
php_admin_value[disable_functions] = {{.S.DisableFunctions}}

; ---- Common ----
php_admin_flag[display_errors] = {{onoff .S.DisplayErrors}}
php_admin_flag[log_errors] = {{onoff .S.LogErrors}}
php_admin_flag[allow_url_fopen] = {{onoff .S.AllowURLFopen}}
php_admin_flag[file_uploads] = {{onoff .S.FileUploads}}
php_admin_flag[short_open_tag] = {{onoff .S.ShortOpenTag}}
php_admin_value[error_reporting] = {{.S.ErrorReporting}}
php_admin_value[include_path] = {{.S.IncludePath}}
php_admin_value[open_basedir] = {{if .S.OpenBasedir}}{{.S.OpenBasedir}}{{else}}/home/{{.SK}}/:/tmp/{{end}}
{{if .S.MailForceExtraParameters}}php_admin_value[mail.force_extra_parameters] = {{.S.MailForceExtraParameters}}{{end}}
php_admin_value[session.save_path] = {{if .S.SessionSavePath}}{{.S.SessionSavePath}}{{else}}/home/{{.SK}}/tmp{{end}}
php_admin_value[upload_tmp_dir] = /home/{{.SK}}/tmp
php_admin_value[sys_temp_dir] = /home/{{.SK}}/tmp

catch_workers_output = yes

; ---- BEGIN_CUSTOM ----
{{.S.EkDirektifler}}
; ---- END_CUSTOM ----
`))

func renderSubPool(sk, poolName, sockDir string, s Settings) (string, error) {
	var buf bytes.Buffer
	err := subPoolTmpl.Execute(&buf, map[string]any{"SK": sk, "Pool": poolName, "SockDir": sockDir, "S": s})
	return buf.String(), err
}

// GetSub: subdomain_php_settings satırını okur (yoksa Defaults()).
func GetSub(ctx context.Context, db *sql.DB, subID int64) (Settings, error) {
	s := Defaults()
	row := db.QueryRowContext(ctx, `SELECT memory_limit, max_execution_time, max_input_time, post_max_size,
		upload_max_filesize, opcache_enable, disable_functions,
		display_errors, log_errors, allow_url_fopen, file_uploads, short_open_tag,
		error_reporting, include_path, open_basedir, session_save_path, mail_force_extra_parameters,
		pm_strategy, pm_max_children, pm_max_requests, pm_start_servers, pm_min_spare_servers, pm_max_spare_servers,
		ek_direktifler, debug_mode FROM subdomain_php_settings WHERE subdomain_id=?`, subID)
	err := row.Scan(&s.MemoryLimit, &s.MaxExecutionTime, &s.MaxInputTime, &s.PostMaxSize,
		&s.UploadMaxFilesize, &s.OpcacheEnable, &s.DisableFunctions,
		&s.DisplayErrors, &s.LogErrors, &s.AllowURLFopen, &s.FileUploads, &s.ShortOpenTag,
		&s.ErrorReporting, &s.IncludePath, &s.OpenBasedir, &s.SessionSavePath, &s.MailForceExtraParameters,
		&s.PMStrategy, &s.PMMaxChildren, &s.PMMaxRequests, &s.PMStartServers, &s.PMMinSpareServers, &s.PMMaxSpareServers,
		&s.EkDirektifler, &s.DebugMode)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	return s, err
}

// HasSub: alt alanın kendi PHP ayar satırı (ve dolayısıyla ayrı havuzu) var mı?
func HasSub(ctx context.Context, db *sql.DB, subID int64) bool {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subdomain_php_settings WHERE subdomain_id=?`, subID).Scan(&n)
	return n > 0
}

// SaveSub: subdomain_php_settings upsert.
func SaveSub(ctx context.Context, db *sql.DB, subID int64, s Settings) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO subdomain_php_settings(subdomain_id, memory_limit, max_execution_time, max_input_time, post_max_size,
			upload_max_filesize, opcache_enable, disable_functions,
			display_errors, log_errors, allow_url_fopen, file_uploads, short_open_tag,
			error_reporting, include_path, open_basedir, session_save_path, mail_force_extra_parameters,
			pm_strategy, pm_max_children, pm_max_requests, pm_start_servers, pm_min_spare_servers, pm_max_spare_servers,
			ek_direktifler, debug_mode)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
			memory_limit=VALUES(memory_limit), max_execution_time=VALUES(max_execution_time),
			max_input_time=VALUES(max_input_time), post_max_size=VALUES(post_max_size),
			upload_max_filesize=VALUES(upload_max_filesize), opcache_enable=VALUES(opcache_enable),
			disable_functions=VALUES(disable_functions), display_errors=VALUES(display_errors),
			log_errors=VALUES(log_errors), allow_url_fopen=VALUES(allow_url_fopen),
			file_uploads=VALUES(file_uploads), short_open_tag=VALUES(short_open_tag),
			error_reporting=VALUES(error_reporting), include_path=VALUES(include_path),
			open_basedir=VALUES(open_basedir), session_save_path=VALUES(session_save_path),
			mail_force_extra_parameters=VALUES(mail_force_extra_parameters),
			pm_strategy=VALUES(pm_strategy), pm_max_children=VALUES(pm_max_children),
			pm_max_requests=VALUES(pm_max_requests), pm_start_servers=VALUES(pm_start_servers),
			pm_min_spare_servers=VALUES(pm_min_spare_servers), pm_max_spare_servers=VALUES(pm_max_spare_servers),
			ek_direktifler=VALUES(ek_direktifler), debug_mode=VALUES(debug_mode)`,
		subID, s.MemoryLimit, s.MaxExecutionTime, s.MaxInputTime, s.PostMaxSize,
		s.UploadMaxFilesize, b2i(s.OpcacheEnable), s.DisableFunctions,
		b2i(s.DisplayErrors), b2i(s.LogErrors), b2i(s.AllowURLFopen), b2i(s.FileUploads), b2i(s.ShortOpenTag),
		s.ErrorReporting, s.IncludePath, s.OpenBasedir, s.SessionSavePath, s.MailForceExtraParameters,
		s.PMStrategy, s.PMMaxChildren, s.PMMaxRequests, s.PMStartServers, s.PMMinSpareServers, s.PMMaxSpareServers,
		s.EkDirektifler, b2i(s.DebugMode))
	return err
}

// ValidateSettings: pool skalarlarında CRLF/NUL enjeksiyonunu reddeder (dışa açık).
func ValidateSettings(s Settings) error { return validatePoolScalars(s) }

// SanitizeEk: ek_direktifler alanını pool'a yazmadan önce güvenli hale getirir (dışa açık).
func SanitizeEk(raw string) (string, error) { return sanitizeEkDirektifler(raw) }

// ApplyNamedPool: verilen PHP sürümünün master'ında `poolName` havuzunu yazar,
// aynı poolName'in diğer sürümlerdeki eski conf'larını siler, master'ı reload eder.
// Döner: unix soket yolu. subID/sk sertleştirmesi renderSubPool içinde.
func ApplyNamedPool(sk, poolName, surum string, s Settings) (socket string, err error) {
	sb, ok := surumBilgi(surum)
	if !ok {
		return "", fmt.Errorf("desteklenmeyen PHP sürümü: %s", surum)
	}
	// diğer sürümlerdeki aynı isimli pool'u temizle (sürüm değişimi)
	for _, other := range KurulSurumler {
		if other.Surum == surum {
			continue
		}
		old := filepath.Join(other.PoolDir, poolName+".conf")
		if _, e := os.Stat(old); e == nil {
			_ = os.Remove(old)
			_, _ = exec.Command("systemctl", "reload-or-restart", other.Service).CombinedOutput()
		}
	}
	_ = os.MkdirAll(sb.PoolDir, 0755)
	_ = os.MkdirAll(sb.SockDir, 0755)
	body, err := renderSubPool(sk, poolName, sb.SockDir, s)
	if err != nil {
		return "", err
	}
	poolPath := filepath.Join(sb.PoolDir, poolName+".conf")
	if err := os.WriteFile(poolPath, []byte(body), 0644); err != nil {
		return "", err
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", sb.Service).CombinedOutput(); err != nil {
		return "", fmt.Errorf("php-fpm reload (%s): %s: %w", sb.Service, strings.TrimSpace(string(out)), err)
	}
	return filepath.Join(sb.SockDir, poolName+".sock"), nil
}

// RemoveNamedPool: poolName conf'unu TÜM sürümlerden siler + master'ları reload eder.
func RemoveNamedPool(poolName string) {
	seen := map[string]bool{}
	for _, sb := range KurulSurumler {
		p := filepath.Join(sb.PoolDir, poolName+".conf")
		if _, e := os.Stat(p); e == nil {
			_ = os.Remove(p)
			seen[sb.Service] = true
		}
	}
	for svc := range seen {
		_, _ = exec.Command("systemctl", "reload-or-restart", svc).CombinedOutput()
	}
}

// ApplyForSub: alt alanın kendi ayarı varsa AYRI havuzu (soket) render eder + döner
// (hasPool=true). Yoksa boş döner (çağıran parent socket'e düşer).
func ApplyForSub(ctx context.Context, db *sql.DB, sk string, subID int64, surum string) (socket string, hasPool bool, err error) {
	if !HasSub(ctx, db, subID) {
		return "", false, nil
	}
	s, e := GetSub(ctx, db, subID)
	if e != nil {
		return "", false, e
	}
	sock, e := ApplyNamedPool(sk, SubPoolName(sk, subID), surum, s)
	if e != nil {
		return "", false, e
	}
	return sock, true, nil
}
