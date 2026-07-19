// Package php: per-domain PHP ayarlari (Plesk benzeri).
// Versiyon listesi, pool conf rendering, settings CRUD.
package php

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"girginospanel/internal/httpx"
	"girginospanel/internal/phpsurum"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type Surum struct {
	Surum    string `json:"surum"`
	PoolDir  string `json:"pool_dir"`
	SockDir  string `json:"sock_dir"`
	Service  string `json:"service"`
	Aciklama string `json:"aciklama"`
}

var KurulSurumler = []Surum{
	{Surum: "8.3", PoolDir: "/etc/php-fpm.d", SockDir: "/run/php-fpm", Service: "php-fpm", Aciklama: "AppStream · OPcache"},
	{Surum: "8.2", PoolDir: "/etc/opt/remi/php82/php-fpm.d", SockDir: "/var/opt/remi/php82/run/php-fpm", Service: "php82-php-fpm", Aciklama: "Remi · Stable"},
	{Surum: "7.4", PoolDir: "/etc/opt/remi/php74/php-fpm.d", SockDir: "/var/opt/remi/php74/run/php-fpm", Service: "php74-php-fpm", Aciklama: "Remi · Legacy"},
}

func surumBilgi(surum string) (Surum, bool) {
	// Once sabit liste (geriye uyum)
	for _, s := range KurulSurumler {
		if s.Surum == surum {
			return s, true
		}
	}
	// Dinamik: phpsurum'dan discover et
	for _, ds := range phpsurum.TumSurumler() {
		if ds.Surum == surum && ds.Yuklu {
			return Surum{
				Surum:    ds.Surum,
				PoolDir:  ds.PoolDir,
				SockDir:  ds.SockDir,
				Service:  ds.Service,
				Aciklama: ds.Aciklama,
			}, true
		}
	}
	return Surum{}, false
}

// Settings: Plesk PHP Settings sayfasinin tum alanlari
type Settings struct {
	// Performance & Security
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	MaxInputTime      int    `json:"max_input_time"`
	PostMaxSize       string `json:"post_max_size"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	OpcacheEnable     bool   `json:"opcache_enable"`
	DisableFunctions  string `json:"disable_functions"`

	// Common
	DisplayErrors            bool   `json:"display_errors"`
	LogErrors                bool   `json:"log_errors"`
	AllowURLFopen            bool   `json:"allow_url_fopen"`
	FileUploads              bool   `json:"file_uploads"`
	ShortOpenTag             bool   `json:"short_open_tag"`
	ErrorReporting           string `json:"error_reporting"`
	IncludePath              string `json:"include_path"`
	OpenBasedir              string `json:"open_basedir"`
	SessionSavePath          string `json:"session_save_path"`
	MailForceExtraParameters string `json:"mail_force_extra_parameters"`

	// PHP-FPM
	PMStrategy        string `json:"pm_strategy"`
	PMMaxChildren     int    `json:"pm_max_children"`
	PMMaxRequests     int    `json:"pm_max_requests"`
	PMStartServers    int    `json:"pm_start_servers"`
	PMMinSpareServers int    `json:"pm_min_spare_servers"`
	PMMaxSpareServers int    `json:"pm_max_spare_servers"`

	// Additional
	EkDirektifler string `json:"ek_direktifler"`

	// Debug Modu — saglam fatal-gorunurluk (register_shutdown_function shim).
	DebugMode bool `json:"debug_mode"`
}

func Defaults() Settings {
	return Settings{
		MemoryLimit:       "256M",
		MaxExecutionTime:  30,
		MaxInputTime:      60,
		PostMaxSize:       "64M",
		UploadMaxFilesize: "32M",
		OpcacheEnable:     true,
		DisableFunctions:  "exec,passthru,shell_exec,system,proc_open,popen",
		DisplayErrors:     false,
		LogErrors:         true,
		AllowURLFopen:     true,
		FileUploads:       true,
		ShortOpenTag:      false,
		ErrorReporting:    "E_ALL & ~E_DEPRECATED & ~E_STRICT",
		IncludePath:       ".:/usr/share/php",
		OpenBasedir:       "",
		SessionSavePath:   "",
		PMStrategy:        "ondemand",
		PMMaxChildren:     8,
		PMMaxRequests:     500,
		PMStartServers:    2,
		PMMinSpareServers: 1,
		PMMaxSpareServers: 3,
		EkDirektifler:     "",
		DebugMode:         false,
	}
}

func Get(ctx context.Context, db *sql.DB, domainID int64) (Settings, error) {
	s := Defaults()
	row := db.QueryRowContext(ctx, `SELECT memory_limit, max_execution_time, max_input_time, post_max_size,
		upload_max_filesize, opcache_enable, disable_functions,
		display_errors, log_errors, allow_url_fopen, file_uploads, short_open_tag,
		error_reporting, include_path, open_basedir, session_save_path, mail_force_extra_parameters,
		pm_strategy, pm_max_children, pm_max_requests, pm_start_servers, pm_min_spare_servers, pm_max_spare_servers,
		ek_direktifler, debug_mode FROM php_settings WHERE domain_id=?`, domainID)
	err := row.Scan(&s.MemoryLimit, &s.MaxExecutionTime, &s.MaxInputTime, &s.PostMaxSize,
		&s.UploadMaxFilesize, &s.OpcacheEnable, &s.DisableFunctions,
		&s.DisplayErrors, &s.LogErrors, &s.AllowURLFopen, &s.FileUploads, &s.ShortOpenTag,
		&s.ErrorReporting, &s.IncludePath, &s.OpenBasedir, &s.SessionSavePath, &s.MailForceExtraParameters,
		&s.PMStrategy, &s.PMMaxChildren, &s.PMMaxRequests, &s.PMStartServers, &s.PMMinSpareServers, &s.PMMaxSpareServers,
		&s.EkDirektifler, &s.DebugMode)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil // default
	}
	return s, err
}

func Save(ctx context.Context, db *sql.DB, domainID int64, s Settings) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO php_settings(domain_id, memory_limit, max_execution_time, max_input_time, post_max_size,
			upload_max_filesize, opcache_enable, disable_functions,
			display_errors, log_errors, allow_url_fopen, file_uploads, short_open_tag,
			error_reporting, include_path, open_basedir, session_save_path, mail_force_extra_parameters,
			pm_strategy, pm_max_children, pm_max_requests, pm_start_servers, pm_min_spare_servers, pm_max_spare_servers,
			ek_direktifler, debug_mode)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
			memory_limit=VALUES(memory_limit),
			max_execution_time=VALUES(max_execution_time),
			max_input_time=VALUES(max_input_time),
			post_max_size=VALUES(post_max_size),
			upload_max_filesize=VALUES(upload_max_filesize),
			opcache_enable=VALUES(opcache_enable),
			disable_functions=VALUES(disable_functions),
			display_errors=VALUES(display_errors),
			log_errors=VALUES(log_errors),
			allow_url_fopen=VALUES(allow_url_fopen),
			file_uploads=VALUES(file_uploads),
			short_open_tag=VALUES(short_open_tag),
			error_reporting=VALUES(error_reporting),
			include_path=VALUES(include_path),
			open_basedir=VALUES(open_basedir),
			session_save_path=VALUES(session_save_path),
			mail_force_extra_parameters=VALUES(mail_force_extra_parameters),
			pm_strategy=VALUES(pm_strategy),
			pm_max_children=VALUES(pm_max_children),
			pm_max_requests=VALUES(pm_max_requests),
			pm_start_servers=VALUES(pm_start_servers),
			pm_min_spare_servers=VALUES(pm_min_spare_servers),
			pm_max_spare_servers=VALUES(pm_max_spare_servers),
			ek_direktifler=VALUES(ek_direktifler),
			debug_mode=VALUES(debug_mode)`,
		domainID, s.MemoryLimit, s.MaxExecutionTime, s.MaxInputTime, s.PostMaxSize,
		s.UploadMaxFilesize, b2i(s.OpcacheEnable), s.DisableFunctions,
		b2i(s.DisplayErrors), b2i(s.LogErrors), b2i(s.AllowURLFopen), b2i(s.FileUploads), b2i(s.ShortOpenTag),
		s.ErrorReporting, s.IncludePath, s.OpenBasedir, s.SessionSavePath, s.MailForceExtraParameters,
		s.PMStrategy, s.PMMaxChildren, s.PMMaxRequests, s.PMStartServers, s.PMMinSpareServers, s.PMMaxSpareServers,
		s.EkDirektifler, b2i(s.DebugMode))
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func onoff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// poolTmpl: PHP-FPM pool conf icerigi - tum ayarlari icerir
var poolTmpl = template.Must(template.New("pool").Funcs(template.FuncMap{"onoff": onoff}).Parse(`[{{.SK}}]
user = {{.SK}}
group = {{.SK}}
listen = {{.SockDir}}/{{.SK}}.sock
listen.owner = nginx
listen.group = nginx
listen.mode = 0660

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

// RenderPool: pool conf icerigini settings + sk + sockdir ile uretir.
func RenderPool(sk string, sockDir string, s Settings) (string, error) {
	var buf bytes.Buffer
	err := poolTmpl.Execute(&buf, map[string]any{"SK": sk, "SockDir": sockDir, "S": s})
	return buf.String(), err
}

// ApplyToFilesystem: pool conf'unu yaz, eski versiyon pool'larini sil, php-fpm reload, nginx reload.
func ApplyToFilesystem(sk, surum string, s Settings) (socket string, err error) {
	sb, ok := surumBilgi(surum)
	if !ok {
		return "", fmt.Errorf("desteklenmeyen PHP sürümü: %s", surum)
	}
	// eski sürümlerin pool'larini sil
	for _, other := range KurulSurumler {
		if other.Surum == surum {
			continue
		}
		old := filepath.Join(other.PoolDir, sk+".conf")
		if _, err := os.Stat(old); err == nil {
			_ = os.Remove(old)
			_, _ = exec.Command("systemctl", "reload-or-restart", other.Service).CombinedOutput()
		}
	}

	_ = os.MkdirAll(sb.PoolDir, 0755)
	_ = os.MkdirAll(sb.SockDir, 0755)
	body, err := RenderPool(sk, sb.SockDir, s)
	if err != nil {
		return "", err
	}
	poolPath := filepath.Join(sb.PoolDir, sk+".conf")
	if err := os.WriteFile(poolPath, []byte(body), 0644); err != nil {
		return "", err
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", sb.Service).CombinedOutput(); err != nil {
		return "", fmt.Errorf("php-fpm reload (%s): %s: %w", sb.Service, strings.TrimSpace(string(out)), err)
	}
	socket = filepath.Join(sb.SockDir, sk+".sock")
	return socket, nil
}

// ekDirektifSatirRe: ek_direktifler içinde YALNIZ 'php_value[anahtar]=' veya
// 'php_flag[anahtar]=' satırlarına izin verir. php_admin_*, pool direktifleri
// (user/group/listen/pm.*), [bölüm] başlıkları ve serbest metin reddedilir.
var ekDirektifSatirRe = regexp.MustCompile(`^php_(?:value|flag)\[([a-zA-Z0-9_.]+)\]\s*=`)

// ekDirektifYasakAnahtar: php_value ile bile ayarlanması tenant izolasyonunu
// zayıflatabilecek anahtarlar (open_basedir'i genişletme, tmp/session yolunu
// kaçırma, dosya/uzantı enjekte etme).
var ekDirektifYasakAnahtar = map[string]bool{
	"open_basedir": true, "disable_functions": true, "disable_classes": true,
	"extension": true, "zend_extension": true,
	"auto_prepend_file": true, "auto_append_file": true,
	"error_log": true, "sys_temp_dir": true, "upload_tmp_dir": true,
	"session.save_path": true, "mail.force_extra_parameters": true,
	"curl.cainfo": true, "openssl.capath": true, "include_path": true,
}

// sanitizeEkDirektifler: kullanıcının serbest "ek direktifler" alanını pool'a
// verbatim yazmadan önce satır-satır doğrular. Boş/yorum (;) satırlar korunur;
// diğer her satır güvenli bir php_value/php_flag olmalı ve yasak anahtar
// içermemeli. Böylece BEGIN_CUSTOM bloğu önceki php_admin_value sertleştirmesini
// (open_basedir, disable_functions, user/group) EZEMEZ.
func sanitizeEkDirektifler(raw string) (string, error) {
	if strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("ek direktiflerde geçersiz karakter (NUL)")
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	var temiz []string
	for i, ln := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, ";") {
			temiz = append(temiz, t)
			continue
		}
		m := ekDirektifSatirRe.FindStringSubmatch(t)
		if m == nil {
			return "", fmt.Errorf("ek direktif satır %d: yalnızca 'php_value[...]=' / 'php_flag[...]=' izinli (php_admin_*, user/group, [bölüm] reddedildi)", i+1)
		}
		if ekDirektifYasakAnahtar[strings.ToLower(m[1])] {
			return "", fmt.Errorf("ek direktif satır %d: '%s' anahtarı güvenlik nedeniyle yasak", i+1, m[1])
		}
		temiz = append(temiz, t)
	}
	return strings.Join(temiz, "\n"), nil
}

// validatePoolScalars: tek-satırlık string ayarlarına gömülü \n/\r ile pool'a yeni
// (php_admin_value / user=...) satır enjekte edilmesini engeller — ek_direktifler
// dışındaki alanlar da bir enjeksiyon yüzeyidir.
func validatePoolScalars(s Settings) error {
	alanlar := map[string]string{
		"memory_limit":                s.MemoryLimit,
		"post_max_size":               s.PostMaxSize,
		"upload_max_filesize":         s.UploadMaxFilesize,
		"disable_functions":           s.DisableFunctions,
		"error_reporting":             s.ErrorReporting,
		"include_path":                s.IncludePath,
		"open_basedir":                s.OpenBasedir,
		"session_save_path":           s.SessionSavePath,
		"mail_force_extra_parameters": s.MailForceExtraParameters,
		"pm_strategy":                 s.PMStrategy,
	}
	for k, v := range alanlar {
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("ayar '%s' satır sonu veya kontrol karakteri içeremez", k)
		}
	}
	return nil
}

// ----- HTTP handlers -----

type Handlers struct {
	DB *sql.DB
}

// Versions: kurulu sürümler — dinamik discover
func (h *Handlers) Versions(w http.ResponseWriter, r *http.Request) {
	all := phpsurum.TumSurumler()
	yuklu := []Surum{}
	gorulen := map[string]bool{}
	for _, s := range all {
		if !s.Yuklu {
			continue
		}
		if gorulen[s.Surum] {
			continue
		}
		gorulen[s.Surum] = true
		aciklama := "Remi · " + s.Aciklama
		if s.Kaynak == "appstream" {
			aciklama = "AppStream · OPcache"
		}
		yuklu = append(yuklu, Surum{
			Surum:    s.Surum,
			PoolDir:  s.PoolDir,
			SockDir:  s.SockDir,
			Service:  s.Service,
			Aciklama: aciklama,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, yuklu)
}

// GetAyarlar: domain'in PHP ayarlari + mevcut sürüm
func (h *Handlers) GetAyarlar(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, sk, surum string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &surum); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	s, err := Get(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// O domain'in PHP sürümü icin kurulu modülleri (info amaçlı)
	moduller := surumModulleri(surum)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alan_adi":  alanAdi,
		"sk":        sk,
		"php_surum": surum,
		"ayarlar":   s,
		"moduller":  moduller,
		"surumler": func() []Surum {
			all := phpsurum.TumSurumler()
			yuklu := []Surum{}
			gorulen := map[string]bool{}
			for _, ds := range all {
				if !ds.Yuklu || gorulen[ds.Surum] {
					continue
				}
				gorulen[ds.Surum] = true
				ac := "Remi"
				if ds.Kaynak == "appstream" {
					ac = "AppStream"
				}
				yuklu = append(yuklu, Surum{Surum: ds.Surum, PoolDir: ds.PoolDir, SockDir: ds.SockDir, Service: ds.Service, Aciklama: ac})
			}
			return yuklu
		}(),
	})
}

// PutAyarlar: ayarlari (+ opsiyonel versiyon) tek seferde kaydet ve pool conf'u yeniden yaz.
func (h *Handlers) PutAyarlar(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		PHPSurum string   `json:"php_surum,omitempty"` // opsiyonel; verilirse versiyon değişir
		Ayarlar  Settings `json:"ayarlar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde: "+err.Error())
		return
	}

	// Güvenlik: pool enjeksiyonunu engelle. Skaler alanlarda satır sonu reddedilir,
	// ek_direktifler allowlist'ten geçirilir (php_admin_*/user/group/open_basedir → 400).
	if err := validatePoolScalars(req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	temizEk, ekErr := sanitizeEkDirektifler(req.Ayarlar.EkDirektifler)
	if ekErr != nil {
		httpx.WriteError(w, http.StatusBadRequest, ekErr.Error())
		return
	}
	req.Ayarlar.EkDirektifler = temizEk

	var sk, surum string
	var demo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, php_surum, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &surum, &demo); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin PHP ayarları sabittir")
		return
	}
	if req.PHPSurum != "" && req.PHPSurum != surum {
		if _, ok := surumBilgi(req.PHPSurum); !ok {
			httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen PHP sürümü")
			return
		}
		surum = req.PHPSurum
	}

	if err := Save(r.Context(), h.DB, id, req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB kaydet: "+err.Error())
		return
	}

	// 🔴 Per-tenant FPM (Seçenek A) aktifse ayarlar per-tenant pool'a uygulanır
	// (paylaşılan /etc/php-fpm.d/<sk>.conf DEĞİL). EnableTenantFPM idempotenttir:
	// pool'u DB ayarlarından yeniden render eder, servisi restart eder, nginx'i
	// per-tenant socket'e re-render eder. Başarısızlıkta paylaşılan düzene rollback.
	var socket string
	if provisioner.TenantFPMActive(sk) {
		s, err := provisioner.EnableTenantFPM(h.DB, id, sk, surum)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "per-tenant fpm: "+err.Error())
			return
		}
		socket = s
	} else {
		s, err := ApplyToFilesystem(sk, surum, req.Ayarlar)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "pool yaz: "+err.Error())
			return
		}
		socket = s
		// nginx vhost'u yeni socket'le yeniden render et + reload (kritik!)
		if err := provisioner.ApplyVhostForDomain(h.DB, id, socket, surum); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "nginx vhost: "+err.Error())
			return
		}
	}

	if req.PHPSurum != "" {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE domains SET php_surum=? WHERE id=?`, surum, id)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"php_surum": surum,
		"socket":    socket,
	})
}

// debugLogPath: sk icin SABIT debug log yolu (path traversal yok — sk domain
// kaydindan gelir + c_ prefix dogrulanir; disaridan gelen deger pathe girmez).
func debugLogPath(sk string) (string, error) {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return "", fmt.Errorf("geçersiz sistem kullanıcısı")
	}
	return "/home/" + sk + "/.gpanel/php_debug.log", nil
}

// GetDebugLog: GET /domains/{id}/php/debug-log — per-domain PHP debug log'un son 200 satiri.
// DebugMode aciksa auto_prepend shim FATAL'lari bu dosyaya yazar (renderTenantPool).
func (h *Handlers) GetDebugLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	p, perr := debugLogPath(sk)
	if perr != nil {
		httpx.WriteError(w, http.StatusBadRequest, perr.Error())
		return
	}
	f, ferr := os.Open(p)
	if ferr != nil {
		// dosya yok / okunamiyor → bos liste (debug hic tetiklenmemis olabilir)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"satirlar": []string{}})
		return
	}
	defer f.Close()
	// DoS-guvenli: tum dosyayi belleğe okuma; sadece dosya SONUNDAN son ~64KB seek-oku.
	const tailBytes = 64 * 1024
	var data []byte
	if st, serr := f.Stat(); serr == nil && st.Size() > tailBytes {
		buf := make([]byte, tailBytes)
		if _, e := f.ReadAt(buf, st.Size()-tailBytes); e == nil || e == io.EOF {
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				buf = buf[i+1:] // ilk kismi satiri at
			}
			data = buf
		}
	} else {
		data, _ = io.ReadAll(f)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	const maxLines = 200
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"satirlar": lines})
}

// ClearDebugLog: DELETE /domains/{id}/php/debug-log — debug log'u truncate eder.
func (h *Handlers) ClearDebugLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	p, perr := debugLogPath(sk)
	if perr != nil {
		httpx.WriteError(w, http.StatusBadRequest, perr.Error())
		return
	}
	// truncate (dosya yoksa sessiz — zaten bos)
	if f, err := os.OpenFile(p, os.O_TRUNC|os.O_WRONLY, 0644); err == nil {
		_ = f.Close()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// surumModulleri: verilen sürüm icin php-fpm tarafindan yüklenen modülleri listele
func surumModulleri(surum string) []string {
	sb, ok := surumBilgi(surum)
	if !ok {
		return nil
	}
	// PHP binary'i bul
	phpBin := "/usr/bin/php"
	if sb.Service != "php-fpm" {
		// "php82-php-fpm" -> "/opt/remi/php82/root/usr/bin/php"
		// Service prefix'i ayikla
		// daha temizi: phpsurum'a sor
		for _, ds := range phpsurum.TumSurumler() {
			if ds.Surum == surum && ds.Yuklu {
				phpBin = ds.PHPBin
				break
			}
		}
	}
	out, err := exec.Command(phpBin, "-m").Output()
	if err != nil {
		return nil
	}
	moduller := []string{}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "[") {
			continue
		}
		moduller = append(moduller, ln)
	}
	return moduller
}
