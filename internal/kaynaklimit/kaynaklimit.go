// Package kaynaklimit: domain başına cgroup + xfs_quota + MariaDB limitleri.
// Plan → domain eşleşmesinden alınan limitleri sistem seviyesinde uygular.
package kaynaklimit

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/provisioner"
)

// Limitler: plan tablosundan okunan aktif değerler.
type Limitler struct {
	CPUYuzde         int
	RAMMB            int
	MaxProcess       int
	InodeKota        int
	IOAgirlik        int
	MySQLMaxBaglanti int
	DiskKotaMB       int
	PMMaxChildren    int // 0 = otomatik türet (max(4, ram_mb/64))
	IOReadMBps       int // mutlak disk okuma bant genişliği MB/s; 0 = sınırsız
	IOWriteMBps      int // mutlak disk yazma bant genişliği MB/s; 0 = sınırsız
	IOReadIOPS       int // mutlak disk okuma IOPS; 0 = sınırsız
	IOWriteIOPS      int // mutlak disk yazma IOPS; 0 = sınırsız
	// MySQL Governor (native MariaDB kaynak limitleri; 0 = sınırsız)
	DBMaxQueriesPerHr int // MAX_QUERIES_PER_HOUR
	DBMaxUpdatesPerHr int // MAX_UPDATES_PER_HOUR
	DBMaxQuerySeconds int // yavaş-sorgu watchdog KILL eşiği (sn); 0 = öldürme yok
}

// ioDevicePath: mutlak disk G/Ç limitlerinin uygulanacağı yol. systemd bunu otomatik
// olarak tenant home'unu barındıran blok cihazına (major:minor) çözer (181/177: /home → /dev/vdaN).
const ioDevicePath = "/home"

// planLimitleriGetir: domain'in bağlı olduğu plan'ın limitlerini döner.
// Plan atanmamışsa boş Limitler{0,...} — uygulama kaldırılır.
func PlanLimitleriGetir(ctx context.Context, db *sql.DB, domainID int64) (Limitler, error) {
	var l Limitler
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(p.cpu_yuzde,0), COALESCE(p.ram_mb,0),
		       COALESCE(p.max_process,0), COALESCE(p.inode_kota,0),
		       COALESCE(p.io_agirlik,100), COALESCE(p.mysql_max_baglanti,0),
		       COALESCE(p.disk_kota_mb,0), COALESCE(p.pm_max_children,0),
		       COALESCE(p.io_read_mbps,0), COALESCE(p.io_write_mbps,0),
		       COALESCE(p.io_read_iops,0), COALESCE(p.io_write_iops,0),
		       COALESCE(p.db_max_queries_per_hour,0), COALESCE(p.db_max_updates_per_hour,0),
		       COALESCE(p.db_max_query_seconds,0)
		FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
		WHERE d.id=?`, domainID).
		Scan(&l.CPUYuzde, &l.RAMMB, &l.MaxProcess, &l.InodeKota,
			&l.IOAgirlik, &l.MySQLMaxBaglanti, &l.DiskKotaMB, &l.PMMaxChildren,
			&l.IOReadMBps, &l.IOWriteMBps, &l.IOReadIOPS, &l.IOWriteIOPS,
			&l.DBMaxQueriesPerHr, &l.DBMaxUpdatesPerHr, &l.DBMaxQuerySeconds)
	return l, err
}

const sliceDir = "/etc/systemd/system"

func sliceName(sk string) string {
	// systemd slice — girginos-c_reg_kalici_test_local.slice
	return "girginos-" + sk + ".slice"
}

func slicePath(sk string) string {
	return filepath.Join(sliceDir, sliceName(sk))
}

// SystemdSliceYaz: /etc/systemd/system/girginos-<sk>.slice dosyasını yazar.
// CPUQuota, MemoryMax, TasksMax, IOWeight + (varsa) MUTLAK disk G/Ç throttle'ları
// (IO{Read,Write}BandwidthMax / IO{Read,Write}IOPSMax) kural setini kullanır (cgroup v2).
func SystemdSliceYaz(sk string, l Limitler) error {
	content := fmt.Sprintf(`# GirginOSPanel per-domain resource slice — %s
[Unit]
Description=GirginOS panel slice for %s
Before=slices.target

[Slice]
CPUAccounting=yes
MemoryAccounting=yes
TasksAccounting=yes
IOAccounting=yes

CPUQuota=%d%%
MemoryMax=%dM
MemoryHigh=%dM
TasksMax=%d
IOWeight=%d
%s`, sk, sk,
		nonzero(l.CPUYuzde, 100),
		nonzero(l.RAMMB, 512),
		nonzero(l.RAMMB, 512)*90/100, // MemoryHigh = 90% of Max (soft throttle)
		nonzero(l.MaxProcess, 50),
		nonzero(l.IOAgirlik, 100),
		ioSliceLines(l))

	if err := os.WriteFile(slicePath(sk), []byte(content), 0644); err != nil {
		return fmt.Errorf("slice yaz: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// 🔴 CANLI GÜNCELLEME (süreç-öldürmez): slice zaten üye içeriyorsa (aktif),
	// yeni limitleri set-property --runtime ile ANINDA uygula. restart KULLANMA —
	// restart slice üyelerini (php-fpm worker'ları) öldürür. Dosya kalıcı kaynaktır;
	// slice inaktifse (ilk oluşturma) FPM servisi başlarken dosyadaki değerleri alır.
	if out, _ := exec.Command("systemctl", "is-active", sliceName(sk)).CombinedOutput(); strings.TrimSpace(string(out)) == "active" {
		cpu := nonzero(l.CPUYuzde, 100)
		mem := nonzero(l.RAMMB, 512)
		tasks := nonzero(l.MaxProcess, 50)
		io := nonzero(l.IOAgirlik, 100)
		args := []string{"set-property", "--runtime", sliceName(sk),
			fmt.Sprintf("CPUQuota=%d%%", cpu),
			fmt.Sprintf("MemoryMax=%dM", mem),
			fmt.Sprintf("MemoryHigh=%dM", mem*90/100),
			fmt.Sprintf("TasksMax=%d", tasks),
			fmt.Sprintf("IOWeight=%d", io),
		}
		// Mutlak disk G/Ç: >0 ise ayarla, 0 ise BOŞ atama ile temizle (>0→0 geçişi için ŞART).
		args = append(args, ioSetPropertyArgs(l)...)
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			log.Printf("slice set-property %s: %s: %v", sk, strings.TrimSpace(string(out)), err)
		}
		// 🔴 systemd set-property BOŞ-atama (>0→0) yalnız systemd görünümünü sıfırlar;
		// AKTİF cgroup'un io.max'ındaki mevcut cihaz limitini TEMİZLEMEZ (kernel eski
		// wbps değerini tutar). 0'a düşen alanları kernel io.max'a "key=max" yazarak
		// AKTİF olarak kaldır.
		ioClearKernelLimits(sk, l)
	}
	return nil
}

// ioClearKernelLimits: aktif slice'ın cgroup io.max dosyasında, plan alanı 0 (sınırsız)
// olan G/Ç anahtarlarını "max" yazarak kernel seviyesinde sıfırlar. systemd set-property
// boş-atama bunu güvenilir yapmadığı için gerekir. >0 alanlara DOKUNMAZ.
func ioClearKernelLimits(sk string, l Limitler) {
	var clears []string
	if l.IOReadMBps == 0 {
		clears = append(clears, "rbps=max")
	}
	if l.IOWriteMBps == 0 {
		clears = append(clears, "wbps=max")
	}
	if l.IOReadIOPS == 0 {
		clears = append(clears, "riops=max")
	}
	if l.IOWriteIOPS == 0 {
		clears = append(clears, "wiops=max")
	}
	if len(clears) == 0 {
		return // tüm alanlar >0 → temizlenecek bir şey yok
	}
	cgOut, err := exec.Command("systemctl", "show", sliceName(sk), "-p", "ControlGroup", "--value").Output()
	cg := strings.TrimSpace(string(cgOut))
	if err != nil || cg == "" {
		return
	}
	ioMaxPath := filepath.Join("/sys/fs/cgroup", cg, "io.max")
	data, err := os.ReadFile(ioMaxPath)
	if err != nil {
		return
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return // hiç aktif limit yok
	}
	suffix := " " + strings.Join(clears, " ")
	for _, ln := range strings.Split(body, "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		// f[0] = "MAJ:MIN"; o cihaz için 0-alanları max yaz (>0 anahtarlar korunur).
		_ = os.WriteFile(ioMaxPath, []byte(f[0]+suffix), 0644)
	}
}

// ioSliceLines: slice dosyasına yazılacak mutlak disk G/Ç direktifleri (yalnız >0 alanlar).
// 0 alanlar için satır YAZILMAZ (sınırsız). systemd yolu (ioDevicePath) blok cihaza çözer.
func ioSliceLines(l Limitler) string {
	var b strings.Builder
	if l.IOReadMBps > 0 {
		fmt.Fprintf(&b, "IOReadBandwidthMax=%s %dM\n", ioDevicePath, l.IOReadMBps)
	}
	if l.IOWriteMBps > 0 {
		fmt.Fprintf(&b, "IOWriteBandwidthMax=%s %dM\n", ioDevicePath, l.IOWriteMBps)
	}
	if l.IOReadIOPS > 0 {
		fmt.Fprintf(&b, "IOReadIOPSMax=%s %d\n", ioDevicePath, l.IOReadIOPS)
	}
	if l.IOWriteIOPS > 0 {
		fmt.Fprintf(&b, "IOWriteIOPSMax=%s %d\n", ioDevicePath, l.IOWriteIOPS)
	}
	return b.String()
}

// ioSetPropertyArgs: canlı set-property için mutlak disk G/Ç argümanları. Alan >0 ise değeri;
// 0 ise BOŞ atama ("IOWriteBandwidthMax=") → systemd o property'nin TÜM cihaz girdilerini
// sıfırlar. >0→0 geçişinde limiti canlı kaldırmak için ŞART (dosyadan silmek tek başına
// aktif cgroup'tan kaldırmaz).
func ioSetPropertyArgs(l Limitler) []string {
	// mbps=true → değer "N M" (bant genişliği); false → "N" (IOPS). n<=0 → boş atama (sıfırla).
	arg := func(prop string, n int, mbps bool) string {
		if n <= 0 {
			return prop + "=" // boş → sıfırla
		}
		if mbps {
			return fmt.Sprintf("%s=%s %dM", prop, ioDevicePath, n)
		}
		return fmt.Sprintf("%s=%s %d", prop, ioDevicePath, n)
	}
	return []string{
		arg("IOReadBandwidthMax", l.IOReadMBps, true),
		arg("IOWriteBandwidthMax", l.IOWriteMBps, true),
		arg("IOReadIOPSMax", l.IOReadIOPS, false),
		arg("IOWriteIOPSMax", l.IOWriteIOPS, false),
	}
}

// SystemdSliceSil: kayıt varsa siler.
func SystemdSliceSil(sk string) error {
	p := slicePath(sk)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil
	}
	_ = os.Remove(p)
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	return nil
}

// NOT (Batch5A): eski PHPFPMSlicePool kaldırıldı. FPM limit uygulaması artık TEK
// yazıcıda — provisioner.EnableTenantFPM (Seçenek A per-tenant php-fpm servisi +
// girginos-<sk>.slice cgroup üyeliği). kaynaklimit ARTIK pool dosyasına dokunmaz
// (yoksa php.go bir sonraki ayar kaydında yazdıklarımızı silerdi). Slice cgroup'u
// gerçek enforcement'ı sağlar; pool yalnız pm.* + php_admin_value taşır.

// hesaplaPMMaxChildren: plandan pm.max_children türetir. Plan değeri >0 ise onu,
// değilse ram_mb'den max(4, ram_mb/64) (RAM tavanı ile tutarlı → OOM-kill önler),
// hiç yoksa 8.
func hesaplaPMMaxChildren(l Limitler) int {
	if l.PMMaxChildren > 0 {
		return l.PMMaxChildren
	}
	if l.RAMMB > 0 {
		c := l.RAMMB / 64
		if c < 4 {
			c = 4
		}
		return c
	}
	return 8
}

// XFSKotaUygula: xfs_quota project quota (inode + blok) ile kullanıcı dizini kotalar.
// /home XFS ile mount olmalı ve pquota özelliği aktif.
func XFSKotaUygula(sk string, l Limitler) error {
	home := "/home/" + sk
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return nil
	}
	// Project ID = uid (basit eşleme)
	uidOut, err := exec.Command("id", "-u", sk).Output()
	if err != nil {
		return fmt.Errorf("uid al: %w", err)
	}
	projID := strings.TrimSpace(string(uidOut))
	if projID == "" || projID == "0" {
		return fmt.Errorf("geçersiz uid: %s", projID)
	}

	// xfs_quota destekliyorsa dene (destek yoksa sessiz atla)
	// blok limit: KB cinsinden. disk_kota_mb * 1024 = KB.
	blokKB := l.DiskKotaMB * 1024
	inode := l.InodeKota
	if blokKB <= 0 && inode <= 0 {
		return nil
	}
	// Project mapping ekle (idempotent)
	line := fmt.Sprintf("%s:%s\n", projID, home)
	f, _ := os.OpenFile("/etc/projid", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		f.WriteString(line)
	}
	// project quota init (idempotent, hata yut)
	_ = exec.Command("xfs_quota", "-x", "-c",
		fmt.Sprintf("project -s -p %s %s", home, projID), "/home").Run()

	limit := fmt.Sprintf("limit -p bsoft=%dk bhard=%dk isoft=%d ihard=%d %s",
		blokKB, blokKB, inode, inode, projID)
	if out, err := exec.Command("xfs_quota", "-x", "-c", limit, "/home").CombinedOutput(); err != nil {
		// XFS quota özelliği yoksa (`pquota` mount opsiyonu eksikse) sessiz devam
		log.Printf("xfs_quota %s: %s (mount pquota aktif değil olabilir)", sk, strings.TrimSpace(string(out)))
	}
	return nil
}

// reGovernorUser: MySQL kullanıcı adı allowlist'i (backtick/tırnak/boşluk yok → SQLi kapalı).
var reGovernorUser = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

// reGovernorHost: MySQL host allowlist'i (localhost / IP / % / hostname).
var reGovernorHost = regexp.MustCompile(`^[A-Za-z0-9_.%\-]{1,64}$`)

// dbGovernorSystemUsers: ASLA dokunulmayacak sistem/panel/replikasyon kullanıcıları.
// (db_accounts'ta zaten olmamalılar; bu ikinci savunma katmanı.)
var dbGovernorSystemUsers = map[string]bool{
	"root": true, "mysql": true, "mariadb.sys": true, "panel": true,
	"event_scheduler": true, "debian-sys-maint": true, "replication": true,
	"repl": true, "healthcheck": true, "": true,
}

// dbGovernorUserAtlanir: kullanıcı adı güvenli/tenant kullanıcısı değilse true (dokunma).
func dbGovernorUserAtlanir(user string) bool {
	return !reGovernorUser.MatchString(user) || dbGovernorSystemUsers[strings.ToLower(user)]
}

// MySQLLimitUygula: domain'in db_accounts'undaki HER DB-kullanıcısına native MariaDB
// kaynak limitlerini (MAX_USER_CONNECTIONS / MAX_QUERIES_PER_HOUR / MAX_UPDATES_PER_HOUR)
// uygular. 0 = sınırsız (MariaDB'de 0 = limit yok). Yalnız o tenant'ın db_accounts
// kullanıcılarına dokunur; root/panel/sistem kullanıcılarına ASLA (allowlist + regex → SQLi yok).
func MySQLLimitUygula(ctx context.Context, db *sql.DB, domainID int64, l Limitler) error {
	rows, err := db.QueryContext(ctx, `SELECT db_user FROM db_accounts WHERE domain_id=?`, domainID)
	if err != nil {
		return err
	}
	var users []string
	for rows.Next() {
		var u string
		if e := rows.Scan(&u); e == nil {
			users = append(users, u)
		}
	}
	rows.Close()

	var uygulandi int
	for _, user := range users {
		if dbGovernorUserAtlanir(user) {
			log.Printf("governor: DB kullanıcısı atlandı (allowlist dışı): %q", user)
			continue
		}
		// 🔴 db_accounts.db_host'a GÜVENME — WordPress alt-kullanıcıları (wpu_*) mysql.user'da
		// @'%' host'ta kayıtlı olabilir; db_host='localhost' ile ALTER eşleşmez → limit alamaz
		// (bypass). Kullanıcının mysql.user'daki GERÇEK host(lar)ının HEPSİNE uygula.
		hosts := mysqlUserHosts(ctx, user)
		if len(hosts) == 0 {
			log.Printf("governor: %s mysql.user'da host kaydı yok — atlandı", user)
			continue
		}
		for _, host := range hosts {
			if !reGovernorHost.MatchString(host) {
				log.Printf("governor: %s@%q geçersiz host biçimi — atlandı", user, host)
				continue
			}
			stmt := fmt.Sprintf(
				"ALTER USER '%s'@'%s' WITH MAX_USER_CONNECTIONS %d MAX_QUERIES_PER_HOUR %d MAX_UPDATES_PER_HOUR %d;",
				user, host,
				nonNeg(l.MySQLMaxBaglanti), nonNeg(l.DBMaxQueriesPerHr), nonNeg(l.DBMaxUpdatesPerHr))
			if out, e := exec.CommandContext(ctx, "mysql", "-uroot", "-e", stmt).CombinedOutput(); e != nil {
				log.Printf("governor ALTER %s@%s başarısız: %s: %v", user, host, strings.TrimSpace(string(out)), e)
			} else {
				uygulandi++
			}
		}
	}
	if uygulandi > 0 {
		if out, e := exec.CommandContext(ctx, "mysql", "-uroot", "-e", "FLUSH USER_RESOURCES;").CombinedOutput(); e != nil {
			log.Printf("governor FLUSH USER_RESOURCES: %s: %v", strings.TrimSpace(string(out)), e)
		}
	}
	return nil
}

// mysqlUserHosts: mysql.user'da verilen kullanıcı adına kayıtlı TÜM host'ları döner
// (root ile okunur; panel DB-kullanıcısı mysql.user'a erişemeyebilir). user allowlist
// regex'inden geçmiş olmalı (SQLi yok).
func mysqlUserHosts(ctx context.Context, user string) []string {
	if dbGovernorUserAtlanir(user) {
		return nil
	}
	out, err := exec.CommandContext(ctx, "mysql", "-uroot", "-N", "-B", "-e",
		fmt.Sprintf("SELECT Host FROM mysql.user WHERE User='%s'", user)).Output()
	if err != nil {
		return nil
	}
	var hosts []string
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		h := strings.TrimSpace(ln)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func nonNeg(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// governorPollInterval: yavaş-sorgu watchdog tarama aralığı. Kısa tutulur çünkü küçük bir
// db_max_query_seconds eşiği (ör. 3sn) ancak eşikten kısa aralıkla güvenilir yakalanır
// (30sn aralık 3sn'lik bir limiti kaçırırdı). Yük çok düşük (aralıkta tek processlist okuması).
const governorPollInterval = 5 * time.Second

// SlowQueryWatchdog: MySQL Governor yavaş-sorgu bekçisi. Periyodik olarak processlist'i
// tarar; bir tenant DB-kullanıcısının çalışan sorgusu planındaki db_max_query_seconds'ı
// aşarsa KILL QUERY ile öldürür. root/panel/sistem kullanıcılarına DOKUNMAZ (yalnız
// db_accounts'ta bulunan + plan limiti >0 olan kullanıcılar). Panel açılışında bg başlatılır.
func SlowQueryWatchdog(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	t := time.NewTicker(governorPollInterval)
	defer t.Stop()
	log.Printf("MySQL Governor: yavaş-sorgu watchdog başladı (%s tarama aralığı)", governorPollInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			governorScanOnce(db)
		}
	}
}

// governorScanOnce: tek tarama. root ile processlist okur (panel DB-kullanıcısı
// PROCESS/CONNECTION ADMIN'e sahip olmayabilir), limiti aşan tenant sorgularını öldürür.
func governorScanOnce(db *sql.DB) {
	out, err := exec.Command("mysql", "-uroot", "-N", "-B", "-e",
		"SELECT ID,USER,TIME FROM information_schema.PROCESSLIST WHERE COMMAND<>'Sleep' AND TIME>0").Output()
	if err != nil {
		return
	}
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ln == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) < 3 {
			continue
		}
		id, e1 := strconv.Atoi(strings.TrimSpace(f[0]))
		user := strings.TrimSpace(f[1])
		secs, e2 := strconv.Atoi(strings.TrimSpace(f[2]))
		if e1 != nil || e2 != nil || secs <= 0 || dbGovernorUserAtlanir(user) {
			continue
		}
		// user → db_accounts → domain → plan.db_max_query_seconds (db_user UNIQUE → tek satır).
		var limit int
		qerr := db.QueryRow(
			`SELECT COALESCE(p.db_max_query_seconds,0)
			 FROM db_accounts a JOIN domains d ON d.id=a.domain_id
			 LEFT JOIN service_plans p ON p.id=d.plan_id
			 WHERE a.db_user=? LIMIT 1`, user).Scan(&limit)
		if qerr != nil || limit <= 0 || secs <= limit {
			continue // db_accounts'ta değil / limit yok / henüz aşmadı
		}
		if kout, kerr := exec.Command("mysql", "-uroot", "-e", fmt.Sprintf("KILL QUERY %d", id)).CombinedOutput(); kerr != nil {
			log.Printf("Governor: %s KILL başarısız (id=%d): %s: %v", user, id, strings.TrimSpace(string(kout)), kerr)
		} else {
			log.Printf("Governor: %s sorgusu %ds > %ds → KILL (id=%d)", user, secs, limit, id)
		}
	}
}

// UygulaHepsi: bir domain için plan'a göre TÜM limitleri uygular:
// slice (cgroup) + per-tenant FPM (Seçenek A, gerçek enforcement + CageFS izolasyon) +
// pm.max_children (plandan) + xfs + mysql.
func UygulaHepsi(ctx context.Context, db *sql.DB, domainID int64) error {
	var sk, surum string
	if err := db.QueryRowContext(ctx,
		`SELECT sistem_kullanici, COALESCE(php_surum,'8.3') FROM domains WHERE id=?`, domainID).
		Scan(&sk, &surum); err != nil {
		return err
	}
	if sk == "" {
		return fmt.Errorf("sistem_kullanici boş")
	}
	l, err := PlanLimitleriGetir(ctx, db, domainID)
	if err != nil {
		return err
	}
	// Plan atanmamış? Per-tenant FPM'i geri al (paylaşılan düzene) + slice'ı sil.
	if l.CPUYuzde == 0 && l.RAMMB == 0 && l.MaxProcess == 0 {
		if provisioner.TenantFPMActive(sk) {
			if err := provisioner.RollbackToSharedFPM(db, domainID, sk, surum); err != nil {
				log.Printf("rollback shared fpm %s: %v", sk, err)
			}
		}
		_ = SystemdSliceSil(sk)
		return nil
	}
	// 1) slice (cgroup limitleri) — canlı, süreç-öldürmez.
	if err := SystemdSliceYaz(sk, l); err != nil {
		log.Printf("slice yaz %s: %v", sk, err)
	}
	// 2) pm.max_children'ı plandan php_settings'e sür (paylaşılan-mod tutarlılığı;
	//    per-tenant pool'u renderTenantPool zaten plandan hesaplar).
	pmc := hesaplaPMMaxChildren(l)
	if _, err := db.ExecContext(ctx,
		`UPDATE php_settings SET pm_max_children=? WHERE domain_id=?`, pmc, domainID); err != nil {
		log.Printf("php_settings pm_max_children %s: %v", sk, err)
	}
	// 3) 🔴 Seçenek A: per-tenant php-fpm servisi (slice üyeliği + CageFS sandbox).
	//    Limitler ancak böyle GERÇEKTEN enforce edilir. Başarısızlıkta EnableTenantFPM
	//    otomatik olarak paylaşılan düzene rollback eder → site asla düşmez.
	if _, err := provisioner.EnableTenantFPM(db, domainID, sk, surum); err != nil {
		log.Printf("per-tenant fpm %s: %v (paylaşılan düzende kalındı)", sk, err)
	}
	// 4) disk kotası (xfs) + MySQL Governor (domain'in TÜM db_accounts kullanıcılarına
	//    native GRANT limitleri: bağlantı + sorgu/saat + güncelleme/saat).
	if err := XFSKotaUygula(sk, l); err != nil {
		log.Printf("xfs quota %s: %v", sk, err)
	}
	if err := MySQLLimitUygula(ctx, db, domainID, l); err != nil {
		log.Printf("mysql governor %s: %v", sk, err)
	}
	return nil
}

func nonzero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// LimitleriReAssert: ZATEN migrate-edilmiş (per-tenant FPM aktif) tenant için limitleri
// SÜREÇ ÖLDÜRMEDEN idempotent yeniden uygular — slice (set-property canlı) + pm.max_children
// (php_settings/DB) + xfs + MySQL Governor (TÜM db_accounts kullanıcıları, gerçek host'lar).
// FPM cutover/restart YAPMAZ → site kesintiye uğramaz. HealTenantFPM her panel restart'ında
// bunu çağırır → kod-iyileştirmeleri (ör. DB coverage fix) + plan drift'i mevcut tenant'lara
// da otomatik iner.
func LimitleriReAssert(ctx context.Context, db *sql.DB, domainID int64) error {
	var sk string
	if err := db.QueryRowContext(ctx,
		`SELECT sistem_kullanici FROM domains WHERE id=?`, domainID).Scan(&sk); err != nil {
		return err
	}
	if sk == "" {
		return fmt.Errorf("sistem_kullanici boş")
	}
	l, err := PlanLimitleriGetir(ctx, db, domainID)
	if err != nil {
		return err
	}
	if l.CPUYuzde == 0 && l.RAMMB == 0 && l.MaxProcess == 0 {
		return nil // plan yok → dokunma
	}
	// slice: SystemdSliceYaz aktif slice'a set-property --runtime ile CANLI uygular (öldürmez).
	if e := SystemdSliceYaz(sk, l); e != nil {
		log.Printf("re-assert slice %s: %v", sk, e)
	}
	// pm.max_children'ı plandan php_settings'e sür (sonraki doğal reload'da pool alır).
	if _, e := db.ExecContext(ctx,
		`UPDATE php_settings SET pm_max_children=? WHERE domain_id=?`, hesaplaPMMaxChildren(l), domainID); e != nil {
		log.Printf("re-assert pm_max_children %s: %v", sk, e)
	}
	if e := XFSKotaUygula(sk, l); e != nil {
		log.Printf("re-assert xfs %s: %v", sk, e)
	}
	// 🔴 MySQL Governor: domain'in TÜM db_accounts kullanıcılarına (ana + wpu_ + alt) gerçek
	// host'lardan native limit. Coverage fix'i mevcut tenant'lara da böyle iner.
	if e := MySQLLimitUygula(ctx, db, domainID, l); e != nil {
		log.Printf("re-assert mysql governor %s: %v", sk, e)
	}
	return nil
}

// planProbeHTTPS: domain'in nginx :443 üzerinden sağlık kodu (Host header, cert
// doğrulamasız). 0 = ulaşılamadı. Cutover öncesi/sonrası regresyon karşılaştırması için.
func planProbeHTTPS(alanAdi string) int {
	if alanAdi == "" {
		return 0
	}
	out, _ := exec.Command("curl", "-sk", "--max-time", "10",
		"-o", os.DevNull, "-w", "%{http_code}",
		"-H", "Host: "+alanAdi, "https://127.0.0.1/").Output()
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// servisAktif: systemd unit "active" mi.
func servisAktif(unit string) bool {
	out, _ := exec.Command("systemctl", "is-active", unit).CombinedOutput()
	return strings.TrimSpace(string(out)) == "active"
}

// HealTenantFPM: TÜM planlı domain'leri per-tenant FPM'e (Seçenek A) GÜVENLE + plan-driven
// migrate eder — mevcut (pre-Batch5A) müşterilerin cutover'ını otomatik tamamlar. Tenant
// başına BULLETPROOF:
//   - cutover ÖNCESİ baseline HTTP probe (nginx :443),
//   - UygulaHepsi (slice + per-tenant FPM + pm.max_children + xfs + mysql),
//   - cutover SONRASI probe,
//   - 🔴 REGRESYON: servis inaktif VEYA (baseline 2xx-4xx iken post 5xx) → otomatik
//     provisioner.RollbackToSharedFPM + slice sil → site paylaşılan düzende 200 kalır.
//
// İdempotent (migrate olanı atlar), SIRALI (thundering yok), ARKA PLANDA çağrılır
// (panel boot'unu bloklamaz). girginospanel-update her panel restart'ında tetikler →
// update için plan-driven cutover mekanizması. Plan atanmamış domain'e DOKUNMAZ.
func HealTenantFPM(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, sistem_kullanici, COALESCE(php_surum,'8.3'), alan_adi
		 FROM domains WHERE plan_id IS NOT NULL ORDER BY id`)
	if err != nil {
		log.Printf("HealTenantFPM: domain listesi okunamadı: %v", err)
		return
	}
	type dom struct {
		id      int64
		sk      string
		php     string
		alanAdi string
	}
	var list []dom
	for rows.Next() {
		var d dom
		if e := rows.Scan(&d.id, &d.sk, &d.php, &d.alanAdi); e == nil {
			list = append(list, d)
		}
	}
	rows.Close()

	var migrated, zaten, rollback int
	for _, d := range list {
		select {
		case <-ctx.Done():
			log.Printf("HealTenantFPM: iptal (ctx) — %d migrate/%d zaten/%d rollback", migrated, zaten, rollback)
			return
		default:
		}
		if d.sk == "" || !strings.HasPrefix(d.sk, "c_") {
			continue
		}
		if provisioner.TenantFPMActive(d.sk) {
			// Zaten migrate — servisi provisioner.EnsureTenantFPMOnStartup ayakta tutar.
			// 🔴 Limitleri SÜREÇ ÖLDÜRMEDEN idempotent RE-ASSERT et: DB Governor coverage,
			// slice cpu/mem/io, xfs drift'i mevcut tenant'lara da insin (her update'te).
			if e := LimitleriReAssert(ctx, db, d.id); e != nil {
				log.Printf("HealTenantFPM: %s re-assert hata: %v", d.sk, e)
			} else {
				log.Printf("HealTenantFPM: %s zaten-aktif — limitler re-assert edildi (süreç öldürmeden)", d.sk)
			}
			zaten++
			continue
		}
		baseline := planProbeHTTPS(d.alanAdi)
		// Tam plan-driven uygulama: slice + per-tenant FPM + pm.max_children + xfs + mysql.
		if e := UygulaHepsi(ctx, db, d.id); e != nil {
			log.Printf("HealTenantFPM: %s UygulaHepsi hata: %v", d.sk, e)
		}
		// EnableTenantFPM başarısızlıkta kendi içinde rollback etmiş olabilir → unit yoksa
		// site zaten paylaşılan düzende (güvenli), bu tenant'ı atla.
		if !provisioner.TenantFPMActive(d.sk) {
			log.Printf("HealTenantFPM: %s cutover başarısız — paylaşılan düzende kaldı (güvenli)", d.sk)
			continue
		}
		time.Sleep(700 * time.Millisecond) // FPM ısınsın + nginx reload otursun
		aktif := servisAktif("php-fpm-" + d.sk + ".service")
		post := planProbeHTTPS(d.alanAdi)
		regresyon := baseline >= 200 && baseline < 500 && post >= 500
		if !aktif || regresyon {
			log.Printf("HealTenantFPM: %s REGRESYON (aktif=%v baseline=%d post=%d) → RollbackToSharedFPM",
				d.sk, aktif, baseline, post)
			if e := provisioner.RollbackToSharedFPM(db, d.id, d.sk, d.php); e != nil {
				log.Printf("HealTenantFPM: %s rollback HATA: %v", d.sk, e)
			}
			_ = SystemdSliceSil(d.sk)
			rollback++
			continue
		}
		log.Printf("HealTenantFPM: %s cutover OK (baseline=%d post=%d)", d.sk, baseline, post)
		migrated++
	}
	log.Printf("HealTenantFPM tamam: %d migrate / %d zaten-aktif(re-assert) / %d rollback (toplam %d planlı domain)",
		migrated, zaten, rollback, len(list))
}
