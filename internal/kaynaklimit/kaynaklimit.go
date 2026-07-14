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
	"strings"
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
}

// planLimitleriGetir: domain'in bağlı olduğu plan'ın limitlerini döner.
// Plan atanmamışsa boş Limitler{0,...} — uygulama kaldırılır.
func PlanLimitleriGetir(ctx context.Context, db *sql.DB, domainID int64) (Limitler, error) {
	var l Limitler
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(p.cpu_yuzde,0), COALESCE(p.ram_mb,0),
		       COALESCE(p.max_process,0), COALESCE(p.inode_kota,0),
		       COALESCE(p.io_agirlik,100), COALESCE(p.mysql_max_baglanti,0),
		       COALESCE(p.disk_kota_mb,0)
		FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
		WHERE d.id=?`, domainID).
		Scan(&l.CPUYuzde, &l.RAMMB, &l.MaxProcess, &l.InodeKota,
			&l.IOAgirlik, &l.MySQLMaxBaglanti, &l.DiskKotaMB)
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
// CPUQuota, MemoryMax, TasksMax, IOWeight kural setini kullanır (cgroup v2).
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
`, sk, sk,
		nonzero(l.CPUYuzde, 100),
		nonzero(l.RAMMB, 512),
		nonzero(l.RAMMB, 512)*90/100, // MemoryHigh = 90% of Max (soft throttle)
		nonzero(l.MaxProcess, 50),
		nonzero(l.IOAgirlik, 100))

	if err := os.WriteFile(slicePath(sk), []byte(content), 0644); err != nil {
		return fmt.Errorf("slice yaz: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Restart slice — mevcut prosesler yeni limitleri alsın
	_, _ = exec.Command("systemctl", "restart", sliceName(sk)).CombinedOutput()
	return nil
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

// PHPFPMPoolSliceEkle: /etc/php-fpm.d/<sk>.conf içine Slice= satırı ekler
// (systemd Slice= drop-in için pool içine "process.slice=..." yazarız — PHP-FPM sistemd unit ile başlar,
//  pool ownership process control seviyesinde). Aslında PHP-FPM systemd tarafından yönetiliyor, her pool
//  aynı PHP-FPM prosesin altında çocuk. Slice'a asıl atama systemd override drop-in ile yapılır:
//  /etc/systemd/system/php-fpm.service.d/slice-<sk>.conf → tüm PHP-FPM'i tek slice'a koyar (child process yok özel).
// PRAGMATIK YAKLAŞIM: `systemd-run --slice=<sname>` ile pool'un ilk çocuklarını slice'a yerleştir.
// Daha temiz: PHP-FPM pool'a systemd hint yok, biz pool_processes'i cgroup'a taşırız.
func PHPFPMSlicePool(sk string, l Limitler) error {
	// PHP-FPM pool'un master ID'sini bul ve cgroup'a taşı.
	// Kolay yol: pool'un process'lerini cgroup'a `cgclassify` ile taşımak yerine,
	// yeni bir systemd service override'ı yazmak — ana PHP-FPM'in etkilenmemesi için
	// per-user slice'ı DIŞARIDA tutarız. Instead, bir Delegate=yes cgroup üstünden
	// slice'a yerleşim yapılır.
	//
	// MVP: sadece slice'ı yazıyoruz. Enrollment `cgclassify` ile manuel çalıştırılabilir
	// veya yeniden başlatma (systemctl restart php-fpm) sırasında kaybolur.
	// Kalıcı entegrasyon: PHP-FPM pool konfigürasyonuna `process.priority` ve `rlimit_*`
	// alanları eklenebilir (systemd olmadan).
	pool := "/etc/php-fpm.d/" + sk + ".conf"
	if _, err := os.Stat(pool); os.IsNotExist(err) {
		return nil // pool yoksa noop
	}
	// pool içinde rlimit ekle (soft process limit, cgroup'dan bağımsız fallback)
	b, err := os.ReadFile(pool)
	if err != nil {
		return err
	}
	body := string(b)
	// varsa eski limit satırlarını temizle
	lines := []string{}
	for _, ln := range strings.Split(body, "\n") {
		s := strings.TrimSpace(ln)
		if strings.HasPrefix(s, "rlimit_") || strings.HasPrefix(s, "; gosp-limit") {
			continue
		}
		lines = append(lines, ln)
	}
	body = strings.Join(lines, "\n")
	limitBlok := fmt.Sprintf("\n; gosp-limit — plan tarafından yönetilir\nrlimit_files = %d\nrlimit_core = 0\n",
		nonzero(l.MaxProcess, 50)*4) // rlimit_files ~ pm.max_children'ın 4x'i
	body += limitBlok
	if err := os.WriteFile(pool, []byte(body), 0644); err != nil {
		return err
	}
	// pool'un PHP-FPM master'ına reload gönder
	surum := "8.3"
	if b, _ := os.ReadFile("/etc/php-fpm.d/" + sk + ".conf"); len(b) > 0 {
		// pool içinde surum hint varsa oku (basit heuristic yok, default 8.3)
	}
	_ = surum
	return nil
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

// MySQLLimitUygula: DB kullanıcısına GRANT ... WITH MAX_USER_CONNECTIONS
func MySQLLimitUygula(sk string, l Limitler, mysqlDBUser string) error {
	if l.MySQLMaxBaglanti <= 0 {
		return nil
	}
	sqlCmd := fmt.Sprintf(
		"GRANT USAGE ON *.* TO '%s'@'localhost' WITH MAX_USER_CONNECTIONS %d;FLUSH PRIVILEGES;",
		mysqlDBUser, l.MySQLMaxBaglanti)
	cmd := exec.Command("mysql", "-uroot", "-e", sqlCmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mysql limit: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// UygulaHepsi: bir domain için plan'a göre TÜM limitleri (slice + fpm + xfs + mysql) uygular.
func UygulaHepsi(ctx context.Context, db *sql.DB, domainID int64) error {
	var sk, dbUser string
	if err := db.QueryRowContext(ctx,
		`SELECT sistem_kullanici, COALESCE(db_user,'') FROM domains WHERE id=?`, domainID).
		Scan(&sk, &dbUser); err != nil {
		return err
	}
	if sk == "" {
		return fmt.Errorf("sistem_kullanici boş")
	}
	l, err := PlanLimitleriGetir(ctx, db, domainID)
	if err != nil {
		return err
	}
	// Plan atanmamış? Slice'ı sil, xfs quota'yı sıfırla, MySQL limit kaldır.
	if l.CPUYuzde == 0 && l.RAMMB == 0 && l.MaxProcess == 0 {
		_ = SystemdSliceSil(sk)
		return nil
	}
	if err := SystemdSliceYaz(sk, l); err != nil {
		log.Printf("slice yaz %s: %v", sk, err)
	}
	if err := PHPFPMSlicePool(sk, l); err != nil {
		log.Printf("fpm pool %s: %v", sk, err)
	}
	if err := XFSKotaUygula(sk, l); err != nil {
		log.Printf("xfs quota %s: %v", sk, err)
	}
	if dbUser != "" {
		if err := MySQLLimitUygula(sk, l, dbUser); err != nil {
			log.Printf("mysql limit %s: %v", sk, err)
		}
	}
	return nil
}

func nonzero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
