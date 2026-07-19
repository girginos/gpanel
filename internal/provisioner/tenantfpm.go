// Per-tenant PHP-FPM izolasyonu (Seçenek A — CageFS/LVE eşdeğeri).
//
// Her tenant için AYRI bir php-fpm master servisi:
//   - Slice=girginos-<sk>.slice  → gerçek cgroup limit (CPU/RAM/Tasks/IO) uygulanır.
//   - ProtectHome=tmpfs + BindPaths=/home/<sk> → tenant YALNIZ kendi home'unu görür (CageFS).
//   - PrivateTmp + ProtectSystem=strict + ProtectProc=invisible + NoNewPrivileges +
//     RestrictNamespaces → sistem/komşu izolasyonu.
//   - Worker'lar pool `user=<sk>` ile tenant kimliğinde çalışır (master root → socket'i
//     nginx'e chown edebilmek için root; NoNewPrivileges root→tenant setuid'i ENGELLEMEZ).
//
// nginx vhost fastcgi_pass per-tenant socket'e yönlendirilir (ApplyVhostForDomain +
// PHPSocketFor otomatik olarak per-tenant socket'i çözer).
//
// 🔴 FALLBACK: cutover'da paylaşılan pool `.bak` olarak saklanır. Per-tenant servis
// bozulursa RollbackToSharedFPM eski paylaşılan-master düzenine güvenle geri döner —
// site asla düşük kalmaz.
package provisioner

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	tenantUnitDir = "/etc/systemd/system"
	tenantCfgRoot = "/etc/php-fpm-tenant"
	tenantLogDir  = "/var/log/php-fpm"
)

func tenantUnitName(sk string) string { return "php-fpm-" + sk + ".service" }
func tenantUnitPath(sk string) string { return filepath.Join(tenantUnitDir, tenantUnitName(sk)) }
func tenantRunDir(sk string) string   { return "/run/php-fpm-" + sk }
func tenantSocket(sk string) string   { return filepath.Join(tenantRunDir(sk), sk+".sock") }
func tenantCfgDir(sk string) string   { return filepath.Join(tenantCfgRoot, sk) }

var (
	fcontextMu   sync.Mutex
	fcontextDone bool
)

// fpmSocketFcontextSpec: per-tenant socket dizinleri /run/php-fpm-<sk>/ için SELinux
// dosya-bağlamı regex'i. 🔴 Mevcut /run/php-fpm(/.*)? kuralı TİRELİ per-tenant yolu
// (php-fpm-<sk>) KAPSAMAZ → bu kural olmadan restorecon yanlış tip (tmpfs/var_run_t)
// etiketler ve nginx (httpd_t) socket'e bağlanamaz → Enforcing'de site 500. Doğru tip
// httpd_var_run_t (paylaşılan socket'ler bunu kullanır, nginx bağlanır). 181 Permissive'de
// görünmez; 177 Enforcing'de kritik. (create-default-on CANLI → taze kurulumda yeni
// domain 500 vermemeli.)
const fpmSocketFcontextSpec = "/run/php-fpm-[^/]+(/.*)?"

// ensureFPMSELinuxFcontext: yukarıdaki fcontext kuralını (httpd_var_run_t) semanage ile
// KALICI + idempotent kaydeder. Süreç başına en fazla bir kez (başarılı olunca) çalışır;
// semanage fcontext -l yavaş olduğu için tekrar tekrar çağrılmaz. SELinux Disabled /
// semanage yok ise sessiz atlar. Kuralın YALNIZ VARLIĞINI garanti eder — asıl etiketleme
// (restorecon) EnableTenantFPM içinde socket oluştuktan sonra AYRI yapılır.
// (Desen: girginospanel-repair ensure_context.)
func ensureFPMSELinuxFcontext() {
	fcontextMu.Lock()
	defer fcontextMu.Unlock()
	if fcontextDone {
		return
	}
	if !selinuxAktif() {
		fcontextDone = true // SELinux yok → tekrar deneme
		return
	}
	if _, err := exec.LookPath("semanage"); err != nil {
		fcontextDone = true // semanage yok → restorecon default'a bırakılır, tekrar deneme
		return
	}
	// Kural zaten var mı? (repair ile aynı: -l yakala, sonra ara.)
	out, _ := exec.Command("semanage", "fcontext", "-l").CombinedOutput()
	if strings.Contains(string(out), "/run/php-fpm-[") {
		fcontextDone = true
		return
	}
	if _, err := exec.Command("semanage", "fcontext", "-a", "-t", "httpd_var_run_t", fpmSocketFcontextSpec).CombinedOutput(); err == nil {
		fcontextDone = true
	}
	// hata → fcontextDone=false; sonraki EnableTenantFPM / panel boot yeniden dener.
}

// selinuxAktif: SELinux Enforcing/Permissive mi (Disabled değil ve getenforce mevcut).
func selinuxAktif() bool {
	out, err := exec.Command("getenforce").Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s == "Enforcing" || s == "Permissive"
}

var (
	httpdBoolMu   sync.Mutex
	httpdBoolDone bool
)

// ensureHTTPDHomeBooleans: SELinux httpd_enable_homedirs + httpd_read_user_content
// boolean'larını açar. 🔴 KAPALI iken nginx(httpd_t) tenant home içeriğini (public_html)
// OKUYAMAZ → try_files dosyayı "yok" sanar → 404. Taze AlmaLinux 10 Enforcing kurulumda
// varsayılan KAPALI → tüm siteler 404. İdempotent, süreç-başına-bir-kez; SELinux Disabled
// veya getsebool/setsebool yoksa sessiz atlar. (Desen: ensureFPMSELinuxFcontext.)
func ensureHTTPDHomeBooleans() {
	httpdBoolMu.Lock()
	defer httpdBoolMu.Unlock()
	if httpdBoolDone {
		return
	}
	if !selinuxAktif() {
		httpdBoolDone = true // SELinux yok → tekrar deneme
		return
	}
	if _, err := exec.LookPath("setsebool"); err != nil {
		httpdBoolDone = true
		return
	}
	gerekli := []string{"httpd_enable_homedirs", "httpd_read_user_content"}
	var kapali []string
	for _, b := range gerekli {
		out, err := exec.Command("getsebool", b).Output()
		if err != nil {
			continue // getsebool yok/hatalı → bu boolean'ı atla
		}
		if !strings.Contains(string(out), "--> on") {
			kapali = append(kapali, b)
		}
	}
	if len(kapali) == 0 {
		httpdBoolDone = true // zaten hepsi açık
		return
	}
	args := []string{"-P"} // -P = kalıcı
	for _, b := range kapali {
		args = append(args, b+"=on")
	}
	if out, err := exec.Command("setsebool", args...).CombinedOutput(); err == nil {
		httpdBoolDone = true
		log.Printf("SELinux: httpd home boolean'ları açıldı (%v) — home'dan site sunumu için", kapali)
	} else {
		log.Printf("SELinux setsebool httpd home: %s: %v", strings.TrimSpace(string(out)), err)
		// hata → httpdBoolDone=false; sonraki boot yeniden dener.
	}
}

// TenantFPMActive: bu tenant için per-tenant FPM servisi kurulu mu (unit dosyası var mı).
func TenantFPMActive(sk string) bool {
	if sk == "" {
		return false
	}
	_, err := os.Stat(tenantUnitPath(sk))
	return err == nil
}

// tenantSanitizeScalar: pool'a gömülecek tek-satır değere kaçış/enjeksiyon girmesini
// engeller (php_settings kaydederken zaten doğrulanır; burada ikinci savunma).
func tenantSanitizeScalar(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return def
	}
	return v
}

// tenantPMMaxChildren: domain'in planına göre pm.max_children'ı türetir.
// Plan.pm_max_children>0 ise onu; değilse plan.ram_mb'den max(4, ram_mb/64);
// plan yoksa 8. RAM tavanı (MemoryMax) ile tutarlı → OOM-kill önler.
func tenantPMMaxChildren(db *sql.DB, domainID int64) int {
	var pmc, ram int
	if db != nil && domainID > 0 {
		_ = db.QueryRow(`SELECT COALESCE(p.pm_max_children,0), COALESCE(p.ram_mb,0)
		                 FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
		                 WHERE d.id=?`, domainID).Scan(&pmc, &ram)
	}
	if pmc > 0 {
		return pmc
	}
	if ram > 0 {
		c := ram / 64
		if c < 4 {
			c = 4
		}
		return c
	}
	return 8
}

// tenantPoolSettings: pool'a yansıyacak (güvenli) php_settings alanları. Satır yoksa
// hardened default'lar kullanılır.
type tenantPoolSettings struct {
	MemoryLimit       string
	MaxExecutionTime  int
	MaxInputTime      int
	PostMaxSize       string
	UploadMaxFilesize string
	DisableFunctions  string
	PMStrategy        string
	PMMaxRequests     int
	// Loglama / Debug Modu (php_settings) — saglam fatal-gorunurluk icin.
	DisplayErrors     bool
	LogErrors         bool
	ErrorReporting    string
	DebugMode         bool
}

const hardenedDisableFns = "exec,passthru,shell_exec,system,proc_open,popen,proc_close,proc_get_status,proc_terminate,proc_nice,pcntl_exec,dl,symlink,link,posix_kill,posix_mkfifo,posix_setpgid,posix_setsid,posix_setuid,posix_setgid"

func tenantReadPoolSettings(db *sql.DB, domainID int64) tenantPoolSettings {
	s := tenantPoolSettings{
		MemoryLimit:       "256M",
		MaxExecutionTime:  30,
		MaxInputTime:      60,
		PostMaxSize:       "64M",
		UploadMaxFilesize: "32M",
		DisableFunctions:  hardenedDisableFns,
		PMStrategy:        "ondemand",
		PMMaxRequests:     500,
		DisplayErrors:     false,
		LogErrors:         true,
		ErrorReporting:    "E_ALL & ~E_DEPRECATED & ~E_STRICT",
		DebugMode:         false,
	}
	if db == nil || domainID <= 0 {
		return s
	}
	var ml, pms, ums, df, strat string
	var met, mit, pmr int
	err := db.QueryRow(`SELECT memory_limit, max_execution_time, max_input_time,
	        post_max_size, upload_max_filesize, disable_functions, pm_strategy, pm_max_requests
	        FROM php_settings WHERE domain_id=?`, domainID).
		Scan(&ml, &met, &mit, &pms, &ums, &df, &strat, &pmr)
	if err != nil {
		return s // satır yok → hardened default
	}
	s.MemoryLimit = tenantSanitizeScalar(ml, s.MemoryLimit)
	s.PostMaxSize = tenantSanitizeScalar(pms, s.PostMaxSize)
	s.UploadMaxFilesize = tenantSanitizeScalar(ums, s.UploadMaxFilesize)
	s.DisableFunctions = tenantSanitizeScalar(df, s.DisableFunctions)
	s.PMStrategy = tenantSanitizeScalar(strat, s.PMStrategy)
	if met > 0 {
		s.MaxExecutionTime = met
	}
	if mit > 0 {
		s.MaxInputTime = mit
	}
	if pmr > 0 {
		s.PMMaxRequests = pmr
	}
	// pm_strategy yalnız bilinen değerlere kısıtla
	switch s.PMStrategy {
	case "static", "dynamic", "ondemand":
	default:
		s.PMStrategy = "ondemand"
	}
	// display_errors/log_errors/error_reporting/debug_mode AYRI okunur (geriye-uyumlu:
	// debug_mode kolonu yoksa bu sorgu hata verir -> default korunur, ana ayarlar
	// etkilenmez). Satir yoksa da (ana select zaten erken donerdi) default gecerli.
	var de, le, dm int
	var er string
	if derr := db.QueryRow(`SELECT COALESCE(display_errors,0), COALESCE(log_errors,1),
	        COALESCE(error_reporting,''), COALESCE(debug_mode,0)
	        FROM php_settings WHERE domain_id=?`, domainID).Scan(&de, &le, &er, &dm); derr == nil {
		s.DisplayErrors = de != 0
		s.LogErrors = le != 0
		s.DebugMode = dm != 0
		if strings.TrimSpace(er) != "" {
			s.ErrorReporting = tenantSanitizeScalar(er, s.ErrorReporting)
		}
	}
	return s
}

// renderTenantPool: per-tenant pool.conf içeriği. Güvenlik değerleri php_admin_value
// ile verilir (kullanıcı ini_set ile EZEMEZ). open_basedir tenant home + /tmp ile
// sınırlıdır. pm.max_children plandan türetilir.
func renderTenantPool(db *sql.DB, sk string, domainID int64) string {
	ps := tenantReadPoolSettings(db, domainID)
	maxCh := tenantPMMaxChildren(db, domainID)
	startServers := maxCh / 4
	if startServers < 1 {
		startServers = 1
	}
	minSpare := 1
	maxSpare := maxCh / 2
	if maxSpare < minSpare {
		maxSpare = minSpare
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", sk)
	fmt.Fprintf(&b, "user = %s\n", sk)
	fmt.Fprintf(&b, "group = %s\n", sk)
	fmt.Fprintf(&b, "listen = %s\n", tenantSocket(sk))
	fmt.Fprintf(&b, "listen.owner = nginx\n")
	fmt.Fprintf(&b, "listen.group = nginx\n")
	fmt.Fprintf(&b, "listen.mode = 0660\n")
	fmt.Fprintf(&b, "pm = %s\n", ps.PMStrategy)
	fmt.Fprintf(&b, "pm.max_children = %d\n", maxCh)
	if ps.PMStrategy == "dynamic" {
		fmt.Fprintf(&b, "pm.start_servers = %d\n", startServers)
		fmt.Fprintf(&b, "pm.min_spare_servers = %d\n", minSpare)
		fmt.Fprintf(&b, "pm.max_spare_servers = %d\n", maxSpare)
	} else if ps.PMStrategy == "ondemand" {
		fmt.Fprintf(&b, "pm.process_idle_timeout = 30s\n")
	}
	fmt.Fprintf(&b, "pm.max_requests = %d\n", ps.PMMaxRequests)
	b.WriteString("; ---- Güvenlik sertleştirmesi (php_admin_* → kullanıcı ini_set ile EZEMEZ) ----\n")
	fmt.Fprintf(&b, "php_admin_value[open_basedir] = /home/%s/:/tmp/\n", sk)
	fmt.Fprintf(&b, "php_admin_value[disable_functions] = %s\n", ps.DisableFunctions)
	fmt.Fprintf(&b, "php_admin_value[upload_tmp_dir] = /home/%s/tmp\n", sk)
	fmt.Fprintf(&b, "php_admin_value[sys_temp_dir] = /home/%s/tmp\n", sk)
	fmt.Fprintf(&b, "php_admin_value[session.save_path] = /home/%s/tmp\n", sk)
	fmt.Fprintf(&b, "php_admin_value[memory_limit] = %s\n", ps.MemoryLimit)
	fmt.Fprintf(&b, "php_admin_value[max_execution_time] = %d\n", ps.MaxExecutionTime)
	fmt.Fprintf(&b, "php_admin_value[max_input_time] = %d\n", ps.MaxInputTime)
	fmt.Fprintf(&b, "php_admin_value[post_max_size] = %s\n", ps.PostMaxSize)
	fmt.Fprintf(&b, "php_admin_value[upload_max_filesize] = %s\n", ps.UploadMaxFilesize)
	// ---- Loglama (per-tenant, saglam): PHP fatal'lari sessizce kaybolmasin ----
	// log_errors ACIK + display_errors KAPALI (prod). error_log BILEREK verilmez ->
	// PHP hatalari stderr'e gider, catch_workers_output ile master bunlari per-tenant
	// error_log'a (tenant-<sk>.log) yazar. php_admin_value[error_log]=<yazilamaz/paylasimli
	// yol> ANTI-PATTERN'i (fatal'lari sessizce yutar) bu sablonda ASLA uretilmez.
	// log_errors (varsayilan on) — fatal'lar per-tenant error_log'a gitsin.
	logFlag := "off"
	if ps.LogErrors {
		logFlag = "on"
	}
	fmt.Fprintf(&b, "php_admin_flag[log_errors] = %s\n", logFlag)
	if ps.DebugMode {
		// 🔴 SAGLAM DEBUG: app runtime'da error_reporting(0) cagirirsa pool'daki
		// display_errors/error_reporting BUNU EZMEZ. Fatal'i gorunur kilmanin TEK
		// guvenilir yolu error_get_last() kullanan register_shutdown_function
		// (auto_prepend ile). Shim /home/<sk>/.gpanel/debug_prepend.php'ye yazilir.
		b.WriteString("php_admin_flag[display_errors] = on\n")
		b.WriteString("php_admin_value[error_reporting] = E_ALL\n")
		fmt.Fprintf(&b, "php_admin_value[auto_prepend_file] = %s\n", tenantDebugPrependPath(sk))
		writeDebugShim(db, sk, domainID)
	} else {
		// prod: display_errors kullanici ayarina gore; auto_prepend override YAZILMAZ
		// (shim dosyasi kalsa da etkisiz). error_reporting sanitize edilir.
		deFlag := "off"
		if ps.DisplayErrors {
			deFlag = "on"
		}
		fmt.Fprintf(&b, "php_admin_flag[display_errors] = %s\n", deFlag)
		fmt.Fprintf(&b, "php_admin_value[error_reporting] = %s\n", sanitizeErrorReporting(ps.ErrorReporting))
	}
	b.WriteString("catch_workers_output = yes\n")
	return b.String()
}

// renderTenantGlobalCfg: per-tenant php-fpm master global config'i (yalnız bu tenant'ın
// pool'unu include eder).
func renderTenantGlobalCfg(sk string) string {
	return fmt.Sprintf(`[global]
pid = %s/php-fpm.pid
error_log = %s/tenant-%s.log
log_level = warning
daemonize = no
include=%s/pool.conf
`, tenantRunDir(sk), tenantLogDir, sk, tenantCfgDir(sk))
}

// renderTenantUnit: per-tenant php-fpm systemd unit'i (slice + sandbox).
func renderTenantUnit(sk, fpmBin string) string {
	return fmt.Sprintf(`[Unit]
Description=GirginOSPanel per-tenant PHP-FPM — %s
After=network.target
Before=nginx.service

[Service]
Type=notify
NotifyAccess=all
Slice=girginos-%s.slice
ExecStart=%s --nodaemonize --fpm-config %s/php-fpm.conf
ExecReload=/bin/kill -USR2 $MAINPID
RuntimeDirectory=php-fpm-%s
RuntimeDirectoryMode=0755
RuntimeDirectoryPreserve=yes
# ---- CageFS eşdeğeri dosya sistemi izolasyonu ----
ProtectHome=tmpfs
BindPaths=/home/%s
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=%s
ProtectProc=invisible
NoNewPrivileges=yes
RestrictNamespaces=yes
RestrictSUIDSGID=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
LimitCORE=0
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, sk, sk, fpmBin, tenantCfgDir(sk), sk, sk, tenantLogDir)
}

// waitForSocket: socket dosyası oluşana kadar (timeout) bekler.
func waitForSocket(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// EnableTenantFPM: bir tenant'ı Seçenek-A per-tenant php-fpm servisine geçirir (idempotent).
// İlk çağrıda paylaşılan pool'u .bak'a taşır; sonraki çağrılarda pool/unit'i tazeleyip
// servisi yeniden başlatır (plan/ayar değişikliği). Herhangi bir adımda başarısız olursa
// otomatik RollbackToSharedFPM ile paylaşılan düzene döner (site düşmez).
// Döner: aktif per-tenant socket yolu.
func EnableTenantFPM(db *sql.DB, domainID int64, sk, surum string) (string, error) {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return "", fmt.Errorf("geçersiz sistem kullanıcısı: %q", sk)
	}
	surum = normalizePHP(surum)
	ay := phpMap[surum]
	fpmBin := ay.FpmBin
	if fpmBin == "" {
		return "", fmt.Errorf("php-fpm binary tanımsız (%s)", surum)
	}
	if _, err := os.Stat(fpmBin); err != nil {
		return "", fmt.Errorf("php-fpm binary yok (%s): %s", surum, fpmBin)
	}
	if _, err := os.Stat("/home/" + sk); err != nil {
		return "", fmt.Errorf("tenant home yok: /home/%s", sk)
	}

	ilkKurulum := !TenantFPMActive(sk)
	cfgDir := tenantCfgDir(sk)
	_ = os.MkdirAll(cfgDir, 0755)
	_ = os.MkdirAll(tenantLogDir, 0755)

	// 1) pool.conf (yedekle → rollback için)
	poolPath := filepath.Join(cfgDir, "pool.conf")
	poolYedek, poolYedekVar := os.ReadFile(poolPath)
	if err := os.WriteFile(poolPath, []byte(renderTenantPool(db, sk, domainID)), 0644); err != nil {
		return "", fmt.Errorf("tenant pool yaz: %w", err)
	}
	// 2) global php-fpm.conf
	if err := os.WriteFile(filepath.Join(cfgDir, "php-fpm.conf"), []byte(renderTenantGlobalCfg(sk)), 0644); err != nil {
		return "", fmt.Errorf("tenant global cfg yaz: %w", err)
	}
	// 3) config'i php-fpm -t ile doğrula (bozuksa pool'u geri al)
	if out, err := exec.Command(fpmBin, "-t", "-y", filepath.Join(cfgDir, "php-fpm.conf")).CombinedOutput(); err != nil {
		if poolYedekVar == nil {
			_ = os.WriteFile(poolPath, poolYedek, 0644)
		}
		return "", fmt.Errorf("php-fpm -t (tenant %s) başarısız: %s: %w", sk, strings.TrimSpace(string(out)), err)
	}
	// 4) unit dosyası + daemon-reload
	if err := os.WriteFile(tenantUnitPath(sk), []byte(renderTenantUnit(sk, fpmBin)), 0644); err != nil {
		return "", fmt.Errorf("tenant unit yaz: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return "", fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// 5) İlk kurulumda paylaşılan pool'u .bak'a taşı + shared master reload (fallback saklanır)
	if ilkKurulum {
		sharedPool := filepath.Join(ay.PoolDir, sk+".conf")
		if _, err := os.Stat(sharedPool); err == nil {
			_ = os.Rename(sharedPool, sharedPool+".bak")
			_, _ = exec.Command("systemctl", "reload-or-restart", ay.Service).CombinedOutput()
		}
	}

	// 6) servisi enable + (re)start
	if out, err := exec.Command("systemctl", "enable", tenantUnitName(sk)).CombinedOutput(); err != nil {
		_ = RollbackToSharedFPM(db, domainID, sk, surum)
		return "", fmt.Errorf("tenant fpm enable: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("systemctl", "restart", tenantUnitName(sk)).CombinedOutput(); err != nil {
		_ = RollbackToSharedFPM(db, domainID, sk, surum)
		return "", fmt.Errorf("tenant fpm restart: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// SELinux: ÖNCE per-tenant socket yolu için fcontext kuralını garanti et (idempotent),
	// SONRA restorecon ile etiketle. Kural olmadan restorecon yanlış tip verir → Enforcing'de
	// nginx→FPM Permission denied (site 500). 181 Permissive'de no-op.
	ensureFPMSELinuxFcontext()
	_, _ = exec.Command("restorecon", "-R", tenantRunDir(sk)).CombinedOutput()
	_, _ = exec.Command("restorecon", "-R", cfgDir).CombinedOutput()

	socket := tenantSocket(sk)
	if !waitForSocket(socket, 6*time.Second) {
		_ = RollbackToSharedFPM(db, domainID, sk, surum)
		return "", fmt.Errorf("tenant fpm socket oluşmadı: %s", socket)
	}
	// restorecon socket oluştuktan sonra bir kez daha (socket'in kendi bağlamı için)
	_, _ = exec.Command("restorecon", socket).CombinedOutput()

	// 7) nginx vhost'u per-tenant socket'e re-render (unit zaten var → ApplyVhostForDomain
	//    guard'ı socket'i tenantSocket olarak çözecek; yine de açıkça geçiyoruz).
	if db != nil && domainID > 0 {
		if err := ApplyVhostForDomain(db, domainID, socket, surum); err != nil {
			_ = RollbackToSharedFPM(db, domainID, sk, surum)
			return "", fmt.Errorf("nginx per-tenant re-render: %w", err)
		}
	}
	return socket, nil
}

// RollbackToSharedFPM: per-tenant FPM servisini kaldırıp paylaşılan-master düzenine
// güvenle döner. Site 500/blank olduğunda çağrılır (EnableTenantFPM içinde otomatik).
//  1. servisi durdur + unit'i kaldır + daemon-reload
//  2. paylaşılan pool'u .bak'tan geri getir (yoksa hardened pool'u yeniden yaz) + reload
//  3. per-tenant config artıklarını temizle
//  4. nginx vhost'u paylaşılan socket'e re-render
func RollbackToSharedFPM(db *sql.DB, domainID int64, sk, surum string) error {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return fmt.Errorf("geçersiz sistem kullanıcısı: %q", sk)
	}
	surum = normalizePHP(surum)
	ay := phpMap[surum]

	// 1) servisi durdur + kaldır (TenantFPMActive artık false döner → sonraki render shared)
	_, _ = exec.Command("systemctl", "disable", "--now", tenantUnitName(sk)).CombinedOutput()
	_ = os.Remove(tenantUnitPath(sk))
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()

	// 2) paylaşılan pool'u geri getir
	sharedPool := filepath.Join(ay.PoolDir, sk+".conf")
	bak := sharedPool + ".bak"
	var socket string
	if _, err := os.Stat(bak); err == nil {
		_ = os.Rename(bak, sharedPool)
		_, _ = exec.Command("systemctl", "reload-or-restart", ay.Service).CombinedOutput()
		socket = filepath.Join(ay.SockDir, sk+".sock")
	} else {
		// .bak yok → hardened paylaşılan pool'u yeniden yaz (php-fpm -t + rollback iceride)
		s, _, werr := writePoolValidated(sk, surum)
		if werr != nil {
			return fmt.Errorf("shared pool geri yazılamadı: %w", werr)
		}
		socket = s
	}

	// 3) per-tenant artıkları temizle
	_ = os.RemoveAll(tenantCfgDir(sk))
	_ = os.RemoveAll(tenantRunDir(sk))

	// 4) nginx vhost'u paylaşılan socket'e re-render (unit silindi → guard shared'e çözer)
	if db != nil && domainID > 0 {
		if err := ApplyVhostForDomain(db, domainID, socket, surum); err != nil {
			return fmt.Errorf("nginx shared re-render: %w", err)
		}
	}
	return nil
}

// TeardownTenantFPM: domain silinirken per-tenant FPM izlerini kaldırır (DB/nginx render
// YOK — Deprovision zaten vhost'u siler). Slice ayrı olarak kaynaklimit.SystemdSliceSil
// ile domain handler'ında silinir.
func TeardownTenantFPM(sk string) {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return
	}
	_, _ = exec.Command("systemctl", "disable", "--now", tenantUnitName(sk)).CombinedOutput()
	_ = os.Remove(tenantUnitPath(sk))
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	_ = os.RemoveAll(tenantCfgDir(sk))
	_ = os.RemoveAll(tenantRunDir(sk))
	// paylaşılan .bak pool artığını da temizle
	for _, ay := range phpMap {
		_ = os.Remove(filepath.Join(ay.PoolDir, sk+".conf.bak"))
	}
}

// EnsureTenantFPMOnStartup: açılışta kurulu tüm per-tenant FPM servislerinin ayakta
// olduğunu garanti eder (unit dosyası var ama servis inaktifse başlatır). Başlatılamayan
// tenant güvenli şekilde paylaşılan düzene indirilir.
func EnsureTenantFPMOnStartup() {
	if pkgDB == nil {
		return
	}
	rows, err := pkgDB.Query(`SELECT id, sistem_kullanici, php_surum FROM domains`)
	if err != nil {
		return
	}
	type dom struct {
		id  int64
		sk  string
		php string
	}
	var list []dom
	for rows.Next() {
		var d dom
		if scanErr := rows.Scan(&d.id, &d.sk, &d.php); scanErr == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		if !TenantFPMActive(d.sk) {
			continue
		}
		// aktif mi?
		// config-drift onarimi: eski provizyonlardan kalan hatali pool ayarlarini
		// (or. yazilamaz www-error.log error_log override'i -> fatal'lari yutuyordu)
		// guvenle duzeltir. php-fpm -t dogrular; bozuksa geri alir; graceful reload.
		repairTenantPoolDrift(d.id, d.sk, d.php)
		if out, _ := exec.Command("systemctl", "is-active", tenantUnitName(d.sk)).CombinedOutput(); strings.TrimSpace(string(out)) == "active" {
			continue
		}
		if out, err := exec.Command("systemctl", "start", tenantUnitName(d.sk)).CombinedOutput(); err != nil {
			// başlatılamadı → paylaşılan düzene güvenli indir
			_ = RollbackToSharedFPM(pkgDB, d.id, d.sk, d.php)
			_ = out
		}
	}
}

// repairTenantPoolDrift: mevcut per-tenant pool.conf guncel sablondan (renderTenantPool)
// sapmissa GUVENLE yeniden yazar. Amac: eski provizyonlardan kalan hatali ayarlari
// (ozellikle php_admin_value[error_log] = /var/log/php-fpm/www-error.log — yazilamaz/paylasimli
// hedef, PHP fatal'larini SESSIZCE yutuyordu) duzeltmek + loglama sertlestirmesini geriye
// donuk uygulamak. Idempotent: drift yoksa hicbir sey yapmaz (reload YOK). php-fpm -t ile
// dogrular; bozuksa eski config'i geri alir. Graceful reload (USR2) → site kesintiye ugramaz.
func repairTenantPoolDrift(domainID int64, sk, surum string) {
	if pkgDB == nil || sk == "" || !strings.HasPrefix(sk, "c_") {
		return
	}
	cfgDir := tenantCfgDir(sk)
	poolPath := filepath.Join(cfgDir, "pool.conf")
	cur, err := os.ReadFile(poolPath)
	if err != nil {
		return // pool.conf yok → dokunma (EnableTenantFPM ilgilenir)
	}
	want := renderTenantPool(pkgDB, sk, domainID)
	if string(cur) == want {
		return // drift yok → no-op
	}
	ay := phpMap[normalizePHP(surum)]
	if ay.FpmBin == "" {
		return
	}
	// yeni pool.conf'u yaz → php-fpm -t → basarisizsa eski haline geri al
	if err := os.WriteFile(poolPath, []byte(want), 0644); err != nil {
		return
	}
	if out, terr := exec.Command(ay.FpmBin, "-t", "-y", filepath.Join(cfgDir, "php-fpm.conf")).CombinedOutput(); terr != nil {
		_ = os.WriteFile(poolPath, cur, 0644) // rollback
		log.Printf("repairTenantPoolDrift: %s php-fpm -t basarisiz, geri alindi: %s", sk, strings.TrimSpace(string(out)))
		return
	}
	// graceful reload (ExecReload=USR2) — calisan istekleri dusurmez
	if out, rerr := exec.Command("systemctl", "reload", tenantUnitName(sk)).CombinedOutput(); rerr != nil {
		log.Printf("repairTenantPoolDrift: %s reload uyarisi: %s", sk, strings.TrimSpace(string(out)))
	}
	log.Printf("repairTenantPoolDrift: %s pool.conf guncellendi (loglama sertlestirmesi + drift onarimi)", sk)
}


// ---- PHP Debug Modu (saglam fatal-gorunurluk) ----

// tenantGpanelDir: tenant'in panel-yonetimli .gpanel dizini (root:root 0755).
func tenantGpanelDir(sk string) string { return filepath.Join("/home", sk, ".gpanel") }

// tenantDebugLogPath: per-domain debug log (tenant:tenant 0644 — worker append eder).
func tenantDebugLogPath(sk string) string { return filepath.Join(tenantGpanelDir(sk), "php_debug.log") }

// tenantDebugPrependPath: auto_prepend shim (root:root 0644 — tenant degistiremez).
func tenantDebugPrependPath(sk string) string {
	return filepath.Join(tenantGpanelDir(sk), "debug_prepend.php")
}

// errReportingRe: error_reporting degeri icin izinli karakter kumesi (E_* token + operator).
var errReportingRe = regexp.MustCompile(`^[A-Za-z0-9_ &|~()]+$`)

// sanitizeErrorReporting: yalniz [A-Za-z0-9_ &|~()] / E_* token'larina izin verir; aksi
// halde guvenli varsayilan E_ALL. Pool'a satir/direktif enjeksiyonunu engeller.
func sanitizeErrorReporting(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !errReportingRe.MatchString(v) {
		return "E_ALL"
	}
	return v
}

// tenantDocRoot: domain belge kokunu DB web_root'tan cozer; bos ise /home/<sk>/public_html.
func tenantDocRoot(db *sql.DB, sk string, domainID int64) string {
	if db != nil && domainID > 0 {
		var wr string
		if err := db.QueryRow(`SELECT COALESCE(web_root,'') FROM domains WHERE id=?`, domainID).Scan(&wr); err == nil {
			if wr = strings.TrimSpace(wr); wr != "" {
				return wr
			}
		}
	}
	return filepath.Join("/home", sk, "public_html")
}

// readUserIniAutoPrepend: docroot/.user.ini icindeki auto_prepend_file degerini okur (yoksa "").
// Debug modunda pool'daki php_admin_value[auto_prepend_file] app'in .user.ini prepend'ini
// EZER; shim icinde geri zincirlemek icin bu deger okunur.
func readUserIniAutoPrepend(docroot string) string {
	b, err := os.ReadFile(filepath.Join(docroot, ".user.ini"))
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, ";") {
			continue
		}
		i := strings.IndexByte(t, '=')
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(t[:i]), "auto_prepend_file") {
			return strings.Trim(strings.TrimSpace(t[i+1:]), "\"'")
		}
	}
	return ""
}

// renderDebugPrependPHP: auto_prepend shim icerigi. register_shutdown_function +
// error_get_last() FATAL'lari yakalar (app error_reporting(0) yapsa bile), per-domain
// debug log'a yazar + display_errors aciksa ekrana basar. orig (app'in kendi .user.ini
// auto_prepend'i) render aninda gomulur → app'in kendi prepend'i BOZULMAZ (varsa require).
func renderDebugPrependPHP(sk, orig string) string {
	logPath := tenantDebugLogPath(sk)
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("// GirginOSPanel PHP Debug Modu — otomatik uretildi, ELLE DUZENLEMEYIN.\n")
	b.WriteString("register_shutdown_function(function(){\n")
	b.WriteString("  $e=error_get_last();\n")
	b.WriteString("  if($e && in_array($e['type'],[E_ERROR,E_PARSE,E_CORE_ERROR,E_COMPILE_ERROR,E_RECOVERABLE_ERROR],true)){\n")
	fmt.Fprintf(&b, "    @file_put_contents('%s',\n", logPath)
	b.WriteString("      date('c').' ['.($_SERVER['REQUEST_URI']??'?').'] '.$e['message'].' @ '.$e['file'].':'.$e['line'].\"\\n\",\n")
	b.WriteString("      FILE_APPEND|LOCK_EX);\n")
	b.WriteString("    if(ini_get('display_errors')) echo \"\\n<pre style='background:#111;color:#f66;padding:8px'>PHP Fatal: \".htmlspecialchars($e['message']).\" @ \".$e['file'].':'.$e['line'].\"</pre>\";\n")
	b.WriteString("  }\n")
	b.WriteString("});\n")
	if orig != "" {
		esc := strings.ReplaceAll(orig, "\\", "\\\\")
		esc = strings.ReplaceAll(esc, "'", "\\'")
		fmt.Fprintf(&b, "@require_once '%s';\n", esc)
	}
	return b.String()
}

// writeDebugShim: DebugMode==true iken idempotent olarak .gpanel dizinini + debug log'u +
// auto_prepend shim'ini olusturur.
//   - /home/<sk>/.gpanel        root:root 0755 (tenant yazamaz → shim'i degistiremez)
//   - .../php_debug.log         tenant:tenant 0644 (worker=tenant-uid append eder)
//   - .../debug_prepend.php     root:root 0644 (tenant okur, root yazar)
// Hepsi restorecon ile etiketlenir (Enforcing'de tenant home altinda dogru baglam).
func writeDebugShim(db *sql.DB, sk string, domainID int64) {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return
	}
	if _, err := os.Stat(filepath.Join("/home", sk)); err != nil {
		return // tenant home yok
	}
	dir := tenantGpanelDir(sk)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	_ = os.Chown(dir, 0, 0)
	_ = os.Chmod(dir, 0755)
	_, _ = exec.Command("restorecon", "-R", dir).CombinedOutput()

	// debug log: tenant:tenant 0644 (worker tenant-uid ile append eder)
	logPath := tenantDebugLogPath(sk)
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		_ = f.Close()
	}
	if uid, gid, err := uidGid(sk); err == nil {
		_ = os.Chown(logPath, uid, gid)
	}
	_ = os.Chmod(logPath, 0644)
	_, _ = exec.Command("restorecon", logPath).CombinedOutput()

	// auto_prepend shim: app'in kendi .user.ini auto_prepend'ini zincirle (bozmadan)
	orig := readUserIniAutoPrepend(tenantDocRoot(db, sk, domainID))
	prePath := tenantDebugPrependPath(sk)
	if orig == prePath {
		orig = "" // kendini require etme
	}
	if err := os.WriteFile(prePath, []byte(renderDebugPrependPHP(sk, orig)), 0644); err == nil {
		_ = os.Chown(prePath, 0, 0)
		_ = os.Chmod(prePath, 0644)
		_, _ = exec.Command("restorecon", prePath).CombinedOutput()
	}
}
