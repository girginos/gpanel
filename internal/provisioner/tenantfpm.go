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
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	tenantUnitDir = "/etc/systemd/system"
	tenantCfgRoot = "/etc/php-fpm-tenant"
	tenantLogDir  = "/var/log/php-fpm"
)

func tenantUnitName(sk string) string     { return "php-fpm-" + sk + ".service" }
func tenantUnitPath(sk string) string     { return filepath.Join(tenantUnitDir, tenantUnitName(sk)) }
func tenantRunDir(sk string) string       { return "/run/php-fpm-" + sk }
func tenantSocket(sk string) string       { return filepath.Join(tenantRunDir(sk), sk+".sock") }
func tenantCfgDir(sk string) string       { return filepath.Join(tenantCfgRoot, sk) }
func tenantPerSkLogDir(sk string) string  { return "/var/log/php-fpm-" + sk }
func tenantPerSkLogFile(sk string) string { return filepath.Join(tenantPerSkLogDir(sk), "tenant.log") }

// postfixInstalled: MTA (sendmail wrapper) kurulu mu? PHP mail() fonksiyonunun namespace
// içinden çalışabilmesi için gerekli. Kurulu değilse cage için bind eklenmez (skip).
// 181-dev'de kurulu DEĞİL; prod 177'de kurulu olabilir — kosullu ekleniyor.
func postfixInstalled() bool {
	_, err := os.Stat("/usr/sbin/sendmail")
	return err == nil
}

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
	DisplayErrors  bool
	LogErrors      bool
	ErrorReporting string
	DebugMode      bool
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
//
// error_log yolu /var/log/php-fpm-<sk>/tenant.log — bu dizin sistemd LogsDirectory ile
// yaratılır ve mount-namespace izolasyonunda tenant için AÇIK olan TEK log dizinidir.
// Eski /var/log/php-fpm/tenant-<sk>.log yolu, artık namespace altında BOS tmpfs olduğu
// için yazılamaz (sibling log ad-sizintisini kapatan drop-in).
func renderTenantGlobalCfg(sk string) string {
	return fmt.Sprintf(`[global]
pid = %s/php-fpm.pid
error_log = %s
log_level = warning
daemonize = no
include=%s/pool.conf
`, tenantRunDir(sk), tenantPerSkLogFile(sk), tenantCfgDir(sk))
}

// renderTenantUnit: per-tenant php-fpm systemd unit'i (slice + sandbox).
//
// CageFS-eşdeğeri mount-namespace sertleştirmesi (canli-dogrulandi 181-dev prototip):
//
//   - ProtectHome=tmpfs + BindPaths=/home/<sk>  → /home altında SADECE bu tenant görünür,
//     komşu tenant home'ları GORUNMEZ (open_basedir bypass'larına ek katman).
//   - TemporaryFileSystem=/etc/php-fpm-tenant:ro,mode=755 /var/log/php-fpm:ro,mode=755
//   - BindReadOnlyPaths=/etc/php-fpm-tenant/<sk>
//     → komşu tenant cfg/log dosya ADLARI dahi görünmez (bilgi ifsası kapatıldı).
//   - LogsDirectory=php-fpm-<sk>  → /var/log/php-fpm-<sk>/ RW (izole namespace altında
//     tek yazılabilir log dizini; sibling namespace'ler görmez).
//   - CapabilityBoundingSet=9-cap  → CAP_SYS_ADMIN/CAP_SYS_MODULE/CAP_NET_ADMIN vs.
//     tümü DUSTU (worker root→tenant-user setuid için CAP_SETUID/CAP_SETGID gerekli).
//   - PrivateDevices + PrivateIPC + RemoveIPC + ProcSubset=pid + ProtectKernelModules/
//     Logs/Clock/Hostname + LockPersonality + RestrictRealtime + KeyringMode=private
//     → kernel-yüzey ve IPC daraltma.
//   - RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK  → AF_PACKET raw
//     paket sniff, netlink dışı özel yuvalar reddedilir.
//
// ASLA EKLENMEZ (canli-cikan zararlar 181-dev'de dogrulandi):
//   - SystemCallFilter=* → php-fpm master @system-service dışı syscall çağrısı yapıyor,
//     SIGSYS ile crash (status=31/SYS). TAMAMEN yasak.
//   - PrivateNetwork=yes → outbound HTTP/DB/mail koparir.
//   - PrivateUsers=yes → master root→tenant user setuid map'i çöker, worker c_<sk>'e
//     inemiyor.
//   - MemoryDenyWriteExecute=yes → PHP 8 opcache.jit ile W^X ihlali; jit=off olsa
//     bile gelecek açıklık.
//   - UMask=0077 → nginx tenant grubunda değil; WP media 403 riski.
//   - BindReadOnlyPaths=/var/lib/mysql/mysql.sock → MariaDB restart'ta socket
//     unlink+recreate; EBUSY riski. ProtectSystem=strict altında RO fs üzerinden
//     unix CONNECT zaten çalışıyor (canli-dogrulandi: "Access denied" = socket
//     ulasildi).
//
// Kosullu (MTA gate): postfix/sendmail kuruluysa /usr/sbin/sendmail + postfix spool
// bind eklenir; 181-dev'de kurulu değil (skip); prod 177 gerekli olursa otomatik açılır.
func renderTenantUnit(sk, fpmBin string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `[Unit]
Description=GirginOSPanel per-tenant PHP-FPM — %s
After=network.target
Before=nginx.service

[Service]
Type=notify
NotifyAccess=all
Slice=girginos-%s.slice
ExecStart=%s --nodaemonize --fpm-config %s/php-fpm.conf
ExecReload=/bin/kill -USR2 $MAINPID

# ---- runtime + logs dir (systemd yaratir + temizler) ----
RuntimeDirectory=php-fpm-%s
RuntimeDirectoryMode=0755
RuntimeDirectoryPreserve=yes
LogsDirectory=php-fpm-%s
LogsDirectoryMode=0750

# ---- CageFS: mount-namespace tabani ----
ProtectHome=tmpfs
BindPaths=/home/%s
PrivateTmp=yes
ProtectSystem=strict
ProtectProc=invisible
ProcSubset=pid

# ---- cfg + log dizin sizintisini kapama (canli-dogrulandi 181) ----
TemporaryFileSystem=/etc/php-fpm-tenant:ro,mode=755 /var/log/php-fpm:ro,mode=755
BindReadOnlyPaths=/etc/php-fpm-tenant/%s

# ---- cihaz + IPC izolasyonu ----
PrivateDevices=yes
PrivateIPC=yes
RemoveIPC=yes

# ---- POSIX shared memory izolasyonu (KRITIK: cross-tenant /dev/shm sizintisi) ----
# PrivateIPC=yes SysV shm/sem/msg + POSIX mqueue'yu izole eder AMA /dev/shm'i ETMEZ:
# POSIX shm bir IPC-namespace nesnesi DEGIL, dosya-sistemi nesnesidir. PrivateDevices=yes
# private /dev kurarken host /dev/shm'i icine BIND eder (aksi halde POSIX shm tamamen
# kirilirdi) -> ucu de "0:25 ... master:3", yani birebir ayni host tmpfs superblock'u.
# 181-dev kaniti: tenant A (uid 1001) yazdi, tenant B (uid 1002) ve host TAM okudu.
# Bu satirdan sonra her tenant kendi tmpfs'ini alir (farkli major:minor, "master:" YOK).
#   mode=1777 ZORUNLU: systemd varsayilani 0755'tir; sticky+other-write olmadan tenant
#     uid'i /dev/shm'e yazamaz -> shm_open()/sem_open() EACCES.
#   size=64M ZORUNLU: size'siz tmpfs varsayilani RAM'in YARISI; per-tenant'a bolununce
#     N x 978M olur (bu makinede 1955MB RAM) -> OOM. Ayrica cgroup backstop UNIVERSAL
#     DEGIL (cagea/cageb icin .slice dosyasi yok -> MemoryMax=infinity), tek koruma bu.
#     Tavan REZERVASYON degil; kullanilmadigi surece 0 maliyet. opcache'i BOGMAZ:
#     opcache "mmap" handler'i MAP_ANONYMOUS|MAP_SHARED kullanir, bu mount'un size=
#     muhasebesine girmez (181'de opcache_enabled=true dogrulandi).
#   noexec: host /dev/shm'de YOK ama /dev/shm klasik web-shell "birak-calistir" hedefi.
#     PHP JIT kapali (php83 jit_buffer_size=0, php84 opcache.jit=disable) -> guvenli.
#     Gerekirse TEK BASINA geri alinabilsin diye ayri satirda tutuluyor.
# NOT: mevcut TemporaryFileSystem= satirina DOKUNULMAZ — systemd coklu atamayi
# BIRIKTIRIR. Bos atama (TemporaryFileSystem=) ASLA yazilmamali: onceki tum atamalari
# sifirlar ve /etc/php-fpm-tenant + /var/log/php-fpm sizinti fix'leri de kaybolur.
TemporaryFileSystem=/dev/shm:mode=1777,nosuid,nodev,noexec,size=64M

# ---- kernel-yuzey daraltma ----
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
LockPersonality=yes

# ---- cap dusur: 9-cap (canli-dogrulandi CapBnd=00000000010005eb) ----
CapabilityBoundingSet=CAP_CHOWN CAP_DAC_OVERRIDE CAP_FOWNER CAP_KILL CAP_SETUID CAP_SETGID CAP_SETPCAP CAP_SYS_RESOURCE CAP_NET_BIND_SERVICE
AmbientCapabilities=

# ---- ayricalik ----
NoNewPrivileges=yes
RestrictNamespaces=yes
RestrictSUIDSGID=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
KeyringMode=private
UMask=0022
`, sk, sk, fpmBin, tenantCfgDir(sk), sk, sk, sk, sk)

	// Kosullu MTA gate: sendmail/postfix kuruluysa PHP mail() cage icinden calisir.
	if postfixInstalled() {
		b.WriteString(`
# ---- MTA (postfix/sendmail kurulu — mail() cage icinden calisir) ----
BindReadOnlyPaths=/usr/sbin/sendmail
BindPaths=/var/spool/postfix/public /var/spool/postfix/maildrop
`)
	}

	b.WriteString(`
# ---- lifecycle ----
LimitCORE=0
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`)
	// 🔴 SURUM DAMGASI (META KRITIK — PROD KAPSAMA DELIGI): unit dosyasi kodda
	// SADECE EnableTenantFPM icinde yaziliyordu. Zaten per-tenant FPM'e gecmis bir
	// tenant'ta unit ASLA yeniden render edilmiyordu (HealTenantFPM "zaten-aktif"
	// dali yalniz limitleri re-assert eder; EnsureTenantFPMOnStartup yalniz pool.conf
	// drift'ine bakar). Sonuc: sablona giren HER guvenlik sertlestirmesi (or. /dev/shm
	// cross-tenant sizinti fix'i) MEVCUT musterilere HIC inmiyordu.
	// Damga = sablon surumu + govdenin sha256 on-eki. RepairTenantUnitDrift bunu
	// diskteki ile karsilastirip fark varsa yeniden render + kontrollu restart yapar.
	body := b.String()
	return tenantUnitStampLine(body) + body
}

// tenantUnitTemplateVersion: unit sablonunun elle yonetilen ana surumu. Sablonda
// anlamli bir davranis degisikligi yapildiginda artir (damga zaten sha ile otomatik
// degisir; surum insan-okunur teshis icin).
const tenantUnitTemplateVersion = "v3"

// tenantUnitStampLine: unit govdesinin basina konan surum damgasi satiri.
func tenantUnitStampLine(body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("# gpanel-unit-template: %s %x\n", tenantUnitTemplateVersion, sum[:6])
}

// unitStampOf: bir unit metninden damga satirini cikarir (teshis loglari icin).
func unitStampOf(unit string) string {
	line, _, _ := strings.Cut(unit, "\n")
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "# gpanel-unit-template:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "# gpanel-unit-template:"))
	}
	return "damgasiz(eski)"
}

// ---------------------------------------------------------------------------
// Post-start crash-loop dedektoru (META KRITIK #2 — 22dk 502 penceresi deligi)
// ---------------------------------------------------------------------------
//
// SORUN: `systemctl restart` rc=0 doner ve Type=notify ready bildirimi 6s icinde
// gelir → waitForSocket GECER. Ancak php-fpm master BUNDAN SONRA olurse (or. SIGSYS
// status=31/SYS) Restart=on-failure sonsuz crash-loop kurar ve RollbackToSharedFPM
// ASLA cagrilmaz. 181-dev'de c_test_com'da 22 dakikalik %41 duty-cycle 502 yasandi.
//
// 181-dev'de OLCULEN sinyaller (kill -SYS enjeksiyonu, crash@3s profili):
//   - NRestarts monotonik ve GUVENILIR tek sinyal: 0→1(~5.5s)→2(~10s)→3(~15s)...
//   - `systemctl restart` NRestarts'i SIFIRLAR; `start`/`reload`/`daemon-reload` SIFIRLAMAZ
//     → mutlak esik degil, DELTA kullanilir (base her cagride taze okunur).
//   - ActiveState HICBIR ZAMAN "failed" olmaz: crash-loop boyunca active↔activating.
//     is-active/is-failed tabanli dedektor CALISMAZ.
//   - SubState=auto-restart / auto-restart-queued yalniz ~2s'lik restart penceresinde
//     gorunur (edge-triggered) ama ILK crash'te ~3.1s'de yakalanir → hizli yol.
//   - StartLimitIntervalUSec=10s + Burst=5 + RestartSec=2 kombinasyonu 5.1s'lik
//     dongude ASLA tripe etmez → systemd'nin kendi guvenlik agi YOK.
//
// PENCERE SECIMI: 15s. Sadece NRestarts>=base+2 kurali ile crash@3s profili 10s'lik
// pencerede NRdelta=1'de kalip KACIRIYOR (delta=2'ye ulasma 10.4s). 15s + SubState
// hizli-yolu birlikte crash@0s/@3s/@9s profillerinin ucunu de yakaliyor.
//
// PENCERE DISI (seyrek/trafik-tetikli) crash'ler icin ayrica arka plan watchdog
// (StartFPMWatchdog) 60s tick + 10dk kayan pencere ile calisir — gercek c_test_com
// olayi ~78s'de bir crash idi, sinirli pencere bunu YAPISAL OLARAK yakalayamaz.

const (
	fpmPostStartWindow      = 15 * time.Second       // enable/refresh sonrasi senkron izleme
	fpmPostStartWindowBoot  = 8 * time.Second        // acilis yolu (boot'u uzatmamak icin kisa)
	fpmPostStartPoll        = 500 * time.Millisecond // ornekleme araligi
	fpmPostStartNRDelta     = 2                      // kesin dongu esigi
	fpmWatchdogTick         = 60 * time.Second       // arka plan tarama araligi
	fpmWatchdogWindow       = 10 * time.Minute       // kayan pencere
	fpmWatchdogNRDelta      = 3                      // pencere icinde restart esigi
	fpmWatchdogUnitNotFound = "not-found"
	fpmDeadConfirmTries     = 3                       // clean-exit dogrulama ornek sayisi
	fpmDeadConfirmWait      = 1500 * time.Millisecond // ornekler arasi bekleme
)

// fpmCrashLoopSubStates: systemd'nin crash→restart dongusunde gectigi ara durumlar.
// Bunlardan birini gormek = master en az bir kez basarisiz cikti.
var fpmCrashLoopSubStates = map[string]bool{
	"auto-restart":        true,
	"auto-restart-queued": true,
	"failed":              true,
}

// fpmLocks: sk basina rollback kilidi. K1 (senkron monitor) ve K2 (watchdog) ayni
// tenant icin es zamanli RollbackToSharedFPM cagirirsa os.Rename(.bak→pool) yarisir;
// ikinci cagri .bak bulamayip writePoolValidated fallback'ine duser ve tenant'in
// ozel pool ayarlari kaybolur. Bu kilit onu engeller.
var fpmLocks sync.Map // sk -> *sync.Mutex

func fpmSkLock(sk string) *sync.Mutex {
	v, _ := fpmLocks.LoadOrStore(sk, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// fpmInflight: EnableTenantFPM'in senkron monitoru calisirken watchdog'un ayni
// tenant'a dokunmasini engeller (cift rollback / yaris onleme).
var fpmInflight sync.Map // sk -> struct{}

// unitProps: `systemctl show` ciktisini KEY=VALUE olarak ayristirir.
//
// 🔴 --value KULLANILMAZ: coklu -p ile `systemctl show --value` cikti SIRASINI
// KORUMAZ (181'de dogrulandi: istenen NRestarts,ActiveState,SubState,Result,
// ExecMainCode,ExecMainStatus → donen Result,NRestarts,ExecMainCode,ExecMainStatus,
// ActiveState,SubState). Index-tabanli parse SESSIZCE yanlis deger okur ve dedektor
// hicbir zaman tetiklenmez — duzeltmeye calistigimiz deligin aynisi.
func unitProps(unit string, keys ...string) map[string]string {
	args := make([]string, 0, 2+2*len(keys))
	args = append(args, "show", unit)
	for _, k := range keys {
		args = append(args, "-p", k)
	}
	out, err := exec.Command("systemctl", args...).Output()
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(keys))
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k != "" {
			m[k] = v
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func unitUint(m map[string]string, key string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(m[key]), 10, 64)
	return n
}

// monitorPostStart: unit'i `window` boyunca izler; crash-loop tespit ederse
// (sebep, true) doner. Temizse ("", false).
//
// base NRestarts CAGRI ANINDA taze okunur → restart (sifirlar) ve start/reload
// (sifirlamaz) yollarinin ikisinde de delta dogru calisir.
func monitorPostStart(unit string, window time.Duration) (string, bool) {
	base := unitUint(unitProps(unit, "NRestarts"), "NRestarts")
	deadline := time.Now().Add(window)
	parseFail := 0
	for time.Now().Before(deadline) {
		time.Sleep(fpmPostStartPoll)
		p := unitProps(unit, "NRestarts", "ActiveState", "SubState", "Result", "ExecMainCode", "ExecMainStatus", "LoadState")
		if p == nil || p["LoadState"] == fpmWatchdogUnitNotFound {
			// Teardown yarisi: operator domain'i silmis olabilir (unit dosyasi yok).
			// "unit yok = bozuk" diye rollback edersek SILINMIS domain icin paylasilan
			// pool'u yeniden yazariz. Guard: 3 ust uste okunamazsa TEMIZ don.
			parseFail++
			if parseFail >= 3 {
				return "", false
			}
			continue
		}
		parseFail = 0
		nr := unitUint(p, "NRestarts")
		det := fmt.Sprintf("NRestarts=%d(base=%d) ActiveState=%s SubState=%s Result=%s ExecMainCode=%s ExecMainStatus=%s",
			nr, base, p["ActiveState"], p["SubState"], p["Result"], p["ExecMainCode"], p["ExecMainStatus"])
		if nr >= base+fpmPostStartNRDelta {
			return "nrestarts_delta " + det, true
		}
		if fpmCrashLoopSubStates[p["SubState"]] {
			return "substate " + det, true
		}
		if p["ActiveState"] == "failed" {
			return "active_failed " + det, true
		}
		// 🔴 CLEAN-EXIT KOR NOKTASI (META KRITIK #3): master TEMIZ olurse
		// (systemctl stop / operator pkill / SIGTERM/SIGQUIT) Restart=on-failure
		// DEVREYE GIRMEZ -> Result=success, NRestarts SABIT, ActiveState=inactive,
		// SubState=dead. Ustteki uc tetikleyicinin (nrestarts_delta / substate /
		// active_failed) HICBIRI eslesmez -> tenant KALICI 502 kalir ve hicbir
		// mekanizma yakalamaz. Unit dosyasi DISKTE VAR + servis OLU = OLUM.
		if unitDeadConfirmed(unit) {
			return "clean_exit " + det, true
		}
	}
	return "", false
}

// unitDeadConfirmed: "unit dosyasi var ama servis olu" durumunu YARIS-GUVENLI dogrular.
// Kontrollu restart penceresi (~1s) ve teardown/rollback yarisi yanlis pozitif
// uretmesin diye arka arkaya fpmDeadConfirmTries kez ayni sonucu ister; herhangi bir
// ornekte unit dosyasi kaybolmus ya da servis canlanmissa OLU DEGIL doner.
func unitDeadConfirmed(unit string) bool {
	path := filepath.Join(tenantUnitDir, unit)
	for i := 0; i < fpmDeadConfirmTries; i++ {
		if i > 0 {
			time.Sleep(fpmDeadConfirmWait)
		}
		// Unit dosyasi yoksa tenant zaten paylasilan duzende (rollback/teardown) → olum degil.
		if _, err := os.Stat(path); err != nil {
			return false
		}
		p := unitProps(unit, "ActiveState", "SubState", "LoadState")
		if p == nil || p["LoadState"] == fpmWatchdogUnitNotFound {
			return false
		}
		if !fpmDeadActiveStates[p["ActiveState"]] {
			return false
		}
	}
	return true
}

// fpmDeadActiveStates: unit dosyasi varken "olu" sayilan ActiveState degerleri.
// activating/deactivating/reloading GECICI → olum sayilmaz (yanlis pozitif olurdu).
var fpmDeadActiveStates = map[string]bool{
	"inactive": true,
	"failed":   true,
}

// guardPostStart: enable/start/reload sonrasi crash-loop dedektoru + otomatik
// RollbackToSharedFPM. nil doner = tenant saglikli. non-nil = crash-loop yakalandi
// ve tenant paylasilan FPM'e GERI ALINDI (site 200'e doner, izolasyon kaybedilir).
func guardPostStart(db *sql.DB, domainID int64, sk, surum string, window time.Duration) error {
	unit := tenantUnitName(sk)
	sebep, kirik := monitorPostStart(unit, window)
	if !kirik {
		return nil
	}
	log.Printf("🔴 post-start crash-loop: %s [%s] → paylasilan FPM'e geri aliniyor", sk, sebep)

	mu := fpmSkLock(sk)
	mu.Lock()
	defer mu.Unlock()
	// Kilidi beklerken baskasi (watchdog / paralel Enable) zaten rollback etmis olabilir.
	if _, statErr := os.Stat(tenantUnitPath(sk)); os.IsNotExist(statErr) {
		return fmt.Errorf("per-tenant FPM crash-loop (%s) — zaten paylasilan FPM'e alinmis", sebep)
	}
	rerr := RollbackToSharedFPM(db, domainID, sk, surum)
	// reset-failed olmazsa unit `systemctl --failed` listesinde asili kalir.
	_, _ = exec.Command("systemctl", "reset-failed", unit).CombinedOutput()
	if rerr != nil {
		return fmt.Errorf("crash-loop rollback BASARISIZ (%s): %w", sebep, rerr)
	}
	log.Printf("✅ %s paylasilan FPM'e geri alindi (crash-loop rollback tamam)", sk)
	return fmt.Errorf("per-tenant FPM crash-loop (%s) — paylasilan FPM'e geri alindi", sebep)
}

// ---- K2: arka plan watchdog (sinirli pencerenin kaciramayacagi seyrek crash'ler) ----

type fpmSample struct {
	t  time.Time
	nr uint64
}

var fpmWatchdogOnce sync.Once

// StartFPMWatchdog: panel omru boyunca calisan crash-loop gozcusu. Idempotent —
// birden fazla cagri tek goroutine baslatir.
//
// NEDEN GEREKLI: guardPostStart'in 15s'lik penceresi YAPISAL bir sinirdir. Gercek
// c_test_com olayinda crash'ler ~78s'de bir geliyordu (17 crash / 22dk); sinirli
// pencere bunu asla goremezdi. Watchdog 10dk kayan pencerede >=3 restart gorurse
// tenant'i paylasilan FPM'e indirir.
func StartFPMWatchdog() {
	fpmWatchdogOnce.Do(func() { go fpmWatchdogLoop() })
}

func fpmWatchdogLoop() {
	hist := map[string][]fpmSample{}
	tk := time.NewTicker(fpmWatchdogTick)
	defer tk.Stop()
	for range tk.C {
		// 🔴 recover TICK BASINA: dis dongude olsaydi tek bir panic goroutine'i
		// KALICI olarak oldururdu (watchdog sessizce olur). Ayrica recover'siz
		// panic TUM girginospanel surecini goturur.
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("fpmWatchdog tick panic (kurtarildi): %v", r)
				}
			}()
			fpmWatchdogScan(hist)
		}()
	}
}

func fpmWatchdogScan(hist map[string][]fpmSample) {
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

	canli := map[string]bool{}
	now := time.Now()
	for _, d := range list {
		if !TenantFPMActive(d.sk) {
			continue
		}
		canli[d.sk] = true
		if _, busy := fpmInflight.Load(d.sk); busy {
			continue // K1 senkron monitor calisiyor → karisma
		}
		p := unitProps(tenantUnitName(d.sk), "NRestarts", "LoadState", "ActiveState", "SubState")
		if p == nil || p["LoadState"] == fpmWatchdogUnitNotFound {
			continue
		}
		// 🔴 K2 CANLILIK YUKLEMI ARTIK GERCEK is-active (META KRITIK #3):
		// TenantFPMActive yalnizca os.Stat(unit) bakar → "unit dosyasi var" =
		// "tenant saglikli" SANILIYORDU. Master TEMIZ olurse (systemctl stop /
		// pkill SIGTERM) Restart=on-failure tetiklenmez, NRestarts ARTMAZ →
		// kayan-pencere kurali ASLA eslesmez → tenant KALICI 502 kalir.
		// Unit dosyasi VAR + ActiveState inactive/failed = OLU → paylasilana indir.
		if fpmDeadActiveStates[p["ActiveState"]] && unitDeadConfirmed(tenantUnitName(d.sk)) {
			fpmForceRollback(d.id, d.sk, d.php, fmt.Sprintf("temiz-cikis/olu servis (ActiveState=%s SubState=%s)",
				p["ActiveState"], p["SubState"]))
			delete(hist, d.sk)
			continue
		}
		nr := unitUint(p, "NRestarts")
		s := hist[d.sk]
		// Manuel restart NRestarts'i sifirlar → geriye gidiste gecmisi at.
		if len(s) > 0 && nr < s[len(s)-1].nr {
			s = nil
		}
		s = append(s, fpmSample{t: now, nr: nr})
		// kayan pencere disini bud (en az bir eski ornek kalsin)
		kes := 0
		for i, x := range s {
			if now.Sub(x.t) <= fpmWatchdogWindow {
				break
			}
			kes = i
		}
		s = s[kes:]
		hist[d.sk] = s
		if len(s) >= 2 && nr-s[0].nr >= fpmWatchdogNRDelta {
			fpmForceRollback(d.id, d.sk, d.php, fmt.Sprintf("son %s icinde %d restart (NRestarts %d→%d)",
				fpmWatchdogWindow, nr-s[0].nr, s[0].nr, nr))
			delete(hist, d.sk)
		}
	}
	// artik per-tenant olmayan sk'lerin gecmisini temizle (bellek sizintisi onleme)
	for sk := range hist {
		if !canli[sk] {
			delete(hist, sk)
		}
	}
}

// fpmForceRollback: watchdog (K2) tespitinden sonra tenant'i paylasilan FPM'e indirir.
// Kilidi beklerken baskasi (K1 / paralel Enable) zaten rollback etmis olabilir →
// unit dosyasi yoksa sessizce cikar.
func fpmForceRollback(id int64, sk, php, sebep string) {
	log.Printf("🔴 fpmWatchdog: %s %s → paylasilan FPM'e geri aliniyor", sk, sebep)
	mu := fpmSkLock(sk)
	mu.Lock()
	defer mu.Unlock()
	if _, statErr := os.Stat(tenantUnitPath(sk)); statErr != nil {
		return
	}
	if rerr := RollbackToSharedFPM(pkgDB, id, sk, php); rerr != nil {
		log.Printf("fpmWatchdog rollback BASARISIZ %s: %v", sk, rerr)
		return
	}
	_, _ = exec.Command("systemctl", "reset-failed", tenantUnitName(sk)).CombinedOutput()
	log.Printf("✅ fpmWatchdog: %s paylasilan FPM'e geri alindi", sk)
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

	// Watchdog (K2) bu tenant'a dokunmasin: senkron monitor (K1) sahibi.
	fpmInflight.Store(sk, struct{}{})
	defer fpmInflight.Delete(sk)

	ilkKurulum := !TenantFPMActive(sk)
	cfgDir := tenantCfgDir(sk)
	_ = os.MkdirAll(cfgDir, 0755)
	_ = os.MkdirAll(tenantLogDir, 0755)
	// Per-sk log dizini (LogsDirectory ile hizali; systemd de yaratir ama sertlestirilmis
	// unit'ten ONCE global cfg'nin error_log path'i icin bu path'in var olmasi gerek).
	_ = os.MkdirAll(tenantPerSkLogDir(sk), 0750)

	// 0) Eski log dosyasini yeni yola tasi (bir defalik migration; namespace altinda
	// eski path artik BOS tmpfs — yazilamaz. Idempotent: yeni dosya varsa atla.)
	{
		eski := filepath.Join(tenantLogDir, "tenant-"+sk+".log")
		yeni := tenantPerSkLogFile(sk)
		if _, err := os.Stat(yeni); os.IsNotExist(err) {
			if _, err := os.Stat(eski); err == nil {
				if err := os.Rename(eski, yeni); err != nil {
					// Rename fail-safe: kopyayla dene (cross-fs vs.); yeni dosya yoksa
					// systemd/php-fpm zaten yaratacak — eski path'i silmiyoruz ki
					// rollback sirasinda geri okunabilsin.
					if data, rerr := os.ReadFile(eski); rerr == nil {
						_ = os.WriteFile(yeni, data, 0640)
					}
				}
			}
		}
	}

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
	//
	// 🔴 CUTOVER ONCE, IZLEME SONRA (META KRITIK #2 duzeltmesi): guardPostStart eskiden
	// BU ADIMDAN ONCE calisiyordu. Yorumdaki "ilk kurulumda vhost hala paylasilan
	// socket'i gosterir, tenant ayakta" varsayimi YANLISTI: adim 5 paylasilan pool'u
	// ZATEN .bak'a tasiyip shared master'i reload etmisti → paylasilan socket ARTIK YOK.
	// Yani 15s'lik senkron izleme boyunca vhost OLU bir socket'e isaret ediyordu ve site
	// ~17s boyunca 502 veriyordu (181-dev'de duyarli PHP probu ile olculdu: 17.26s).
	// Cutover'i once yapmak kesintiyi ~2s'ye indirir; KORUMA AYNEN KALIR cunku
	// guardPostStart crash-loop yakalarsa RollbackToSharedFPM vhost'u paylasilana
	// geri render eder (RollbackToSharedFPM adim 4).
	if db != nil && domainID > 0 {
		if err := ApplyVhostForDomain(db, domainID, socket, surum); err != nil {
			_ = RollbackToSharedFPM(db, domainID, sk, surum)
			return "", fmt.Errorf("nginx per-tenant re-render: %w", err)
		}
	}

	// 8) 🔴 POST-START CRASH-LOOP GUARD (META KRITIK #2): ready-notify + socket GECER
	// ama master BUNDAN SONRA olurse (SIGSYS vb.) Restart=on-failure sonsuz dongu
	// kurar ve buraya kadar hicbir kontrol yakalamaz. 15s senkron izleme; tetiklenirse
	// RollbackToSharedFPM ZATEN icerde calisti → cagirana hata donuyoruz.
	if gerr := guardPostStart(db, domainID, sk, surum, fpmPostStartWindow); gerr != nil {
		return "", gerr
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
	// K2: panel omru boyunca crash-loop gozcusu (idempotent — tek goroutine).
	StartFPMWatchdog()
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
		// 🔴 KAPSAMA (META KRITIK #1): unit sablonu drift onarimi. Bu cagri OLMADAN
		// zaten-aktif tenant'in unit dosyasi HIC yeniden render edilmiyordu → sablona
		// giren guvenlik sertlestirmeleri MEVCUT musterilere inmiyordu. Damga uyusmuyorsa
		// yeniden render + daemon-reload + kontrollu restart + guardPostStart yapar.
		RepairTenantUnitDrift(d.id, d.sk, d.php)
		if out, _ := exec.Command("systemctl", "is-active", tenantUnitName(d.sk)).CombinedOutput(); strings.TrimSpace(string(out)) == "active" {
			continue
		}
		if out, err := exec.Command("systemctl", "start", tenantUnitName(d.sk)).CombinedOutput(); err != nil {
			// başlatılamadı → paylaşılan düzene güvenli indir
			_ = RollbackToSharedFPM(pkgDB, d.id, d.sk, d.php)
			_ = out
			continue
		}
		// start rc=0 dedi diye guvenme: post-start crash-loop ayni delige duser.
		// Acilis yolunda kisa pencere (boot'u uzatmamak icin); geri kalanini
		// StartFPMWatchdog (10dk kayan pencere) kapatir.
		if gerr := guardPostStart(pkgDB, d.id, d.sk, d.php, fpmPostStartWindowBoot); gerr != nil {
			log.Printf("EnsureTenantFPMOnStartup: %v", gerr)
		}
	}
}

// RepairTenantUnitDrift: mevcut per-tenant systemd unit dosyasi guncel sablondan
// (renderTenantUnit) sapmissa GUVENLE yeniden yazar + kontrollu restart eder.
// true doner = unit degisti (restart yapildi veya rollback tetiklendi).
//
// 🔴🔴 NEDEN VAR (META KRITIK — PROD KAPSAMA DELIGI):
// Unit dosyasi kodda TEK yerde yaziliyordu: EnableTenantFPM adim 4. ZATEN per-tenant
// FPM'e gecmis bir tenant'ta bu fonksiyon bir daha CAGRILMIYORDU:
//   - HealTenantFPM "zaten-aktif" dali → yalniz LimitleriReAssert + continue,
//   - EnsureTenantFPMOnStartup → yalniz repairTenantPoolDrift (pool.conf), sonra
//     is-active gorunce continue.
//
// 181-dev kaniti: unit'e "# COVERAGE_CANARY" yazilip panel restart edildi → canary
// HAYATTA kaldi, md5 DEGISMEDI. Yani /dev/shm cross-tenant sizinti fix'i de dahil
// sablondaki her sertlestirme mevcut musterilere HIC inmeyecekti.
//
// NEDEN reload DEGIL restart: mount-namespace / TemporaryFileSystem / cap-bounding
// gibi unit-seviyesi ayarlar sadece SUREC BASLARKEN uygulanir; USR2 reload worker
// tazeler ama master'in namespace'ini DEGISTIRMEZ. Operator yuvarlanan kesintiyi
// (birkac saniye 503) ACIKCA KABUL ETTI → kontrollu restart serbest.
//
// GUVENLIK: restart basarisiz / socket olusmaz ise RollbackToSharedFPM; sonrasinda
// guardPostStart (crash-loop dedektoru) calisir. Drift yoksa TAM no-op — restart YOK.
func RepairTenantUnitDrift(domainID int64, sk, surum string) bool {
	if pkgDB == nil || sk == "" || !strings.HasPrefix(sk, "c_") {
		return false
	}
	unitPath := tenantUnitPath(sk)
	cur, err := os.ReadFile(unitPath)
	if err != nil {
		return false // per-tenant FPM kurulu degil → dokunma (EnableTenantFPM ilgilenir)
	}
	ay := phpMap[normalizePHP(surum)]
	if ay.FpmBin == "" {
		return false
	}
	want := renderTenantUnit(sk, ay.FpmBin)
	if string(cur) == want {
		return false // drift yok → no-op (restart YOK, kesinti YOK)
	}
	log.Printf("repairTenantUnitDrift: %s unit sablonu eski (disk=%s istenen=%s) → yeniden render + kontrollu restart",
		sk, unitStampOf(string(cur)), unitStampOf(want))

	// K2 watchdog bu tenant'a dokunmasin: kisa restart penceresinde "olu" sanip
	// gereksiz rollback etmesin (K1 sahibi biziz).
	fpmInflight.Store(sk, struct{}{})
	defer fpmInflight.Delete(sk)

	// Kilitli bolum AYRI closure: guardPostStart kendi icinde fpmSkLock alir,
	// sync.Mutex reentrant DEGIL → kilidi tutarken cagirmak deadlock olurdu.
	devam := func() bool {
		mu := fpmSkLock(sk)
		mu.Lock()
		defer mu.Unlock()
		if werr := os.WriteFile(unitPath, []byte(want), 0644); werr != nil {
			log.Printf("repairTenantUnitDrift: %s unit yazilamadi: %v", sk, werr)
			return false
		}
		if out, derr := exec.Command("systemctl", "daemon-reload").CombinedOutput(); derr != nil {
			_ = os.WriteFile(unitPath, cur, 0644) // eski unit'i geri koy
			_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
			log.Printf("repairTenantUnitDrift: %s daemon-reload basarisiz, geri alindi: %s",
				sk, strings.TrimSpace(string(out)))
			return false
		}
		if out, rerr := exec.Command("systemctl", "restart", tenantUnitName(sk)).CombinedOutput(); rerr != nil {
			log.Printf("repairTenantUnitDrift: %s restart basarisiz (%s) → paylasilan FPM'e geri aliniyor",
				sk, strings.TrimSpace(string(out)))
			_ = RollbackToSharedFPM(pkgDB, domainID, sk, surum)
			return false
		}
		if !waitForSocket(tenantSocket(sk), 6*time.Second) {
			log.Printf("repairTenantUnitDrift: %s socket olusmadi → paylasilan FPM'e geri aliniyor", sk)
			_ = RollbackToSharedFPM(pkgDB, domainID, sk, surum)
			return false
		}
		// Yeni namespace/runtime dizini → SELinux etiketlerini tazele (Permissive'de no-op).
		ensureFPMSELinuxFcontext()
		_, _ = exec.Command("restorecon", "-R", tenantRunDir(sk)).CombinedOutput()
		_, _ = exec.Command("restorecon", tenantSocket(sk)).CombinedOutput()
		log.Printf("repairTenantUnitDrift: %s unit guncellendi + restart edildi (damga=%s)", sk, unitStampOf(want))
		return true
	}()
	if !devam {
		return true
	}
	// restart rc=0 + socket var demek YETMEZ: master bundan SONRA olebilir (crash-loop).
	if gerr := guardPostStart(pkgDB, domainID, sk, surum, fpmPostStartWindowBoot); gerr != nil {
		log.Printf("repairTenantUnitDrift: %v", gerr)
	}
	return true
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
	// Migration akisi da ayni delige acik: USR2 sonrasi master olurse crash-loop.
	// reload NRestarts'i SIFIRLAMAZ → base cagri aninda okundugu icin delta dogru.
	if gerr := guardPostStart(pkgDB, domainID, sk, surum, fpmPostStartWindowBoot); gerr != nil {
		log.Printf("repairTenantPoolDrift: %v", gerr)
	}
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
	// DoS-guvenli log rotasyon: yazmadan ONCE dosya >2MB ise son ~1MB'i koru (basi kirp).
	fmt.Fprintf(&b, "    $lf='%s';\n", logPath)
	b.WriteString("    if(@filesize($lf)>2097152){$fp=@fopen($lf,'r');if($fp){@fseek($fp,-1048576,SEEK_END);$tl=@fread($fp,1048576);@fclose($fp);if($tl!==false){$nl=strpos($tl,\"\\n\");if($nl!==false)$tl=substr($tl,$nl+1);@file_put_contents($lf,$tl,LOCK_EX);}}}\n")
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
//
// Hepsi restorecon ile etiketlenir (Enforcing'de tenant home altinda dogru baglam).
func writeDebugShim(db *sql.DB, sk string, domainID int64) {
	if sk == "" || !strings.HasPrefix(sk, "c_") {
		return
	}
	home := filepath.Join("/home", sk)
	if _, err := os.Stat(home); err != nil {
		return // tenant home yok
	}
	orig := readUserIniAutoPrepend(tenantDocRoot(db, sk, domainID))
	if orig == tenantDebugPrependPath(sk) {
		orig = "" // kendini require etme
	}
	installDebugShim(home, sk, []byte(renderDebugPrependPHP(sk, orig)))
}

// installDebugShim: writeDebugShim'in FS-yazan cekirdegi (home test-icin enjekte edilebilir).
// SYMLINK/TOCTOU-guvenli: /home/<sk> tenant-sahipli (0710) guvenilmez ust-dizin oldugundan
// TUM islemler dir-fd + *at-syscall + O_NOFOLLOW ile yapilir. .gpanel ROOT:ROOT 0755 GERCEK
// dizin olarak dogrulanir/olusturulur (symlink/dosya/tenant-sahipli ise symlink-guvenli
// temizlenip yeniden yaratilir) -> cross-tenant chown DoS + keyfi root-yaz kapatilir.
func installDebugShim(home, sk string, content []byte) {
	homeFd, err := unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(homeFd)

	gpFd, ok := ensureRootDirAt(homeFd, ".gpanel")
	if !ok {
		return
	}
	defer unix.Close(gpFd)
	restoreconFdPath(gpFd) // SELinux: pinlenmis fd-yolu uzerinden relabel (symlink -R yok)

	// debug log: tenant:tenant 0644. O_NOFOLLOW + fd-uzerinden Fchown/Fchmod.
	if lf, e := unix.Openat(gpFd, "php_debug.log",
		unix.O_WRONLY|unix.O_CREAT|unix.O_APPEND|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644); e == nil {
		if uid, gid, ue := uidGid(sk); ue == nil {
			_ = unix.Fchown(lf, uid, gid)
		}
		_ = unix.Fchmod(lf, 0644)
		restoreconFdPath(lf)
		unix.Close(lf)
	}

	// auto_prepend shim: root:root — tenant okur, degistiremez.
	if pf, e := unix.Openat(gpFd, "debug_prepend.php",
		unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644); e == nil {
		_, _ = unix.Write(pf, content)
		_ = unix.Fchown(pf, 0, 0) // root:root — tenant okur, degistiremez
		_ = unix.Fchmod(pf, 0644)
		restoreconFdPath(pf)
		unix.Close(pf)
	}
}

// ensureRootDirAt: parentFd altinda `name`i ROOT:ROOT 0755 GERCEK dizin olarak GARANTI eder.
// Girdi symlink / dosya / tenant-sahipli-dizin ise guvensiz kabul edilir → symlink-guvenli
// ozyinelemeli silinip yeniden yaratilir. Basarida dizinin O_NOFOLLOW dir-fd'sini doner
// (cagiran Close etmeli). O_NOFOLLOW open + fd-uzerinden Fstat dogrulamasi TOCTOU-son-adim
// yarisini kapatir (yaris = ELOOP/yanlis-sahip → temizle+retry). Idempotent.
func ensureRootDirAt(parentFd int, name string) (int, bool) {
	for attempt := 0; attempt < 3; attempt++ {
		var st unix.Stat_t
		serr := unix.Fstatat(parentFd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if serr == nil {
			if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 {
				// symlink / dosya / yanlis-sahip → guvensiz, kaldir
				if removeAtRecursive(parentFd, name) != nil {
					return -1, false
				}
				serr = unix.ENOENT
			}
		}
		if serr == unix.ENOENT {
			if e := unix.Mkdirat(parentFd, name, 0755); e != nil && e != unix.EEXIST {
				return -1, false
			}
		} else if serr != nil {
			return -1, false
		}
		fd, e := unix.Openat(parentFd, name,
			unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if e != nil {
			continue // symlink-swap yarisi vb → retry
		}
		var fst unix.Stat_t
		if unix.Fstat(fd, &fst) != nil ||
			fst.Mode&unix.S_IFMT != unix.S_IFDIR || fst.Uid != 0 || fst.Gid != 0 {
			unix.Close(fd)
			_ = removeAtRecursive(parentFd, name) // guvensizi temizle, retry
			continue
		}
		_ = unix.Fchmod(fd, 0755)
		return fd, true
	}
	return -1, false
}

// removeAtRecursive: dirfd'ye-goreli name'i symlink-guvenli sil. Once unlinkat(flag 0)
// (dosya/symlink'i TAKIP ETMEDEN kaldirir); dizinse O_NOFOLLOW ile acip icini fd-ozyinelemeli
// bosaltir, sonra AT_REMOVEDIR. Hicbir adimda symlink takip edilmez → jail-disi silme imkansiz.
func removeAtRecursive(dirfd int, name string) error {
	if err := unix.Unlinkat(dirfd, name, 0); err == nil {
		return nil
	}
	fd, err := unix.Openat(dirfd, name,
		unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		// dizin degil (symlink zaten unlink denendi) → son care
		return unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
	}
	if names, e := readdirnamesRawFd(fd); e == nil {
		for _, n := range names {
			_ = removeAtRecursive(fd, n)
		}
	}
	unix.Close(fd)
	return unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
}

// readdirnamesRawFd: raw dir fd'yi (sahipligini ALMADAN) listeler — dup+os.File ile okur,
// dup'i kapatir; asil fd cagirana kalir.
func readdirnamesRawFd(dirfd int) ([]string, error) {
	dup, err := unix.Dup(dirfd)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(dup), ".")
	defer f.Close()
	return f.Readdirnames(-1)
}

// restoreconFdPath: fd'nin PINLENMIS gercek yolunu (/proc/self/fd/N → kernel cozer, saldirgan
// symlink'ine bagisik) alip restorecon calistirir. Enforcing SELinux'ta root'un olusturdugu
// dosya dogru context almazsa nginx/php-fpm erisemez; bu yuzden SART. Pinlenmis-yol → symlink
// -R relabel riski yok.
func restoreconFdPath(fd int) {
	real, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return
	}
	_, _ = exec.Command("restorecon", real).CombinedOutput()
}
