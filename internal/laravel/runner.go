// Package laravel: per-domain Laravel Toolkit (Plesk paritesi).
//
// GÜVENLİK ÇEKİRDEĞİ: tüm artisan/composer/npm/git komutları tenant kullanıcısı
// (c_xxx) OLARAK, tenant ev dizini altında, panel sırları sızdırılmadan çalışır.
// runuser (sudo DEĞİL — git.go env-sızıntı tuzağı) + sıfırdan env + mutlak binary
// yolları (PATH hijack yok) + context timeout + çıktı cap. Root'a geri dönüş yok.
package laravel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	composerBin = "/usr/local/bin/composer"
	ciktiCap    = 64 << 10 // 64KB çıktı tavanı
	kisaTimeout = 120 * time.Second
	sistemPATH  = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	cronDDizin  = "/etc/cron.d"
	logAltDizin = "logs" // /home/c_x/logs
)

// badURLChars: repo URL'de yasak shell/enjeksiyon metakarakterleri (git.go ile aynı)
const badURLChars = "\"'`$();|&<>\\"

// logRootDizin: detached job (kur/deploy) logları ROOT-sahipli, tenant-YAZILAMAZ
// dizinde tutulur. Aksi halde tenant ~/logs/x.log → /etc/passwd symlink'i ile root'un
// (systemd StandardOutput=append + Go OpenFile) symlink-takibini kötüye kullanır.
const logRootDizin = "/var/log/girginos-laravel"

func ensureLogRoot() { _ = os.MkdirAll(logRootDizin, 0755) } // root:root 0755

// execSem: tenant başına eşzamanlı senkron komut sınırı (host tükenmesi DoS savunması).
var execSem sync.Map

func execGate(sk string) func() {
	v, _ := execSem.LoadOrStore(sk, make(chan struct{}, 3))
	c := v.(chan struct{})
	c <- struct{}{}
	return func() { <-c }
}

// idKilit: domain başına mutex (kur/deploy/queue check-then-act yarışını serileştirir).
var idKilit sync.Map

func kilitle(id int64) func() {
	v, _ := idKilit.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

var (
	reAltKomut    = regexp.MustCompile(`^[a-z][a-z0-9:_-]*$`)    // artisan/composer/npm alt-komut
	reArg         = regexp.MustCompile(`^[A-Za-z0-9:_.,=/@-]+$`) // serbest argüman
	reNpmPkg      = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*)(/[a-z0-9._-]+)?(@[a-z0-9._^~<>=* |,-]+)?$`)
	reComposerPkg = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*)/[a-z0-9]([a-z0-9._-]*)(:[\^~<>=0-9.* |,-]+)?$`)
	reNodeSurum   = regexp.MustCompile(`^[0-9]{1,2}(\.[0-9]{1,3}){0,2}$`)
)

// gecerliSK: sistem kullanıcısı c_ ile başlamalı (cross-tenant/root koruması)
func gecerliSK(sk string) bool { return strings.HasPrefix(sk, "c_") && len(sk) > 2 }

// reANSI: ANSI renk/stil kaçış dizileri. Laravel composer.json 'package:discover
// --ansi' ile renk zorlar (bizim --no-ansi'yi ezer) → çıktıyı UI için temizleriz.
var reANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

func ansiTemizle(s string) string { return reANSI.ReplaceAllString(s, "") }

// phpBin: domains.php_surum ("8.3") → CLI binary. /usr/bin/php83 vb. mevcut; yoksa
// varsayılan /usr/bin/php. Mutlak yol → tenant PATH hijack'i etkisiz.
func phpBin(surum string) string {
	kod := strings.ReplaceAll(strings.TrimSpace(surum), ".", "")
	if kod != "" {
		cand := "/usr/bin/php" + kod
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return "/usr/bin/php"
}

// guvenliAppDir: app_root ("public_html" veya "public_html/alt") → dogrulanmis MUTLAK
// dizin. public_html ALTINDA olmalı (docroot confinement + operatör kararı). '..'/
// mutlak/garip-karakter reddi + EvalSymlinks ile ara-symlink kaçışı kapalı.
func guvenliAppDir(sk, appRoot string) (string, error) {
	if !gecerliSK(sk) {
		return "", fmt.Errorf("geçersiz kullanıcı")
	}
	ar := strings.Trim(strings.TrimSpace(appRoot), "/")
	if ar == "" {
		ar = "public_html"
	}
	if strings.Contains(ar, "..") || !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(ar) {
		return "", fmt.Errorf("geçersiz uygulama dizini")
	}
	if ar != "public_html" && !strings.HasPrefix(ar, "public_html/") {
		return "", fmt.Errorf("uygulama public_html altında olmalı")
	}
	base := "/home/" + sk + "/public_html"
	abs := filepath.Clean("/home/" + sk + "/" + ar)
	if abs != base && !strings.HasPrefix(abs, base+"/") {
		return "", fmt.Errorf("dizin public_html dışına çıkamaz")
	}
	// Symlink kaçışını kapat — leaf henüz yoksa (kur öncesi) EN DERİN MEVCUT atayı çöz
	// ve base altında olduğunu doğrula. Aksi halde public_html→/etc symlink'iyle mkdir
	// root'un keyfi konumda dizin yaratması sömürülür (denetim bulgusu #4).
	kontrol := abs
	for kontrol == base || strings.HasPrefix(kontrol, base+"/") {
		if real, err := filepath.EvalSymlinks(kontrol); err == nil {
			if real != base && !strings.HasPrefix(real, base+"/") {
				return "", fmt.Errorf("dizin (symlink dahil) public_html dışına çıkamaz")
			}
			break // en derin mevcut ata base altında — güvenli
		}
		if kontrol == base {
			break // public_html yok — tenant olarak oluşturulacak (root değil)
		}
		kontrol = filepath.Dir(kontrol)
	}
	return abs, nil
}

// tenantEnv: alt-sürece verilecek SIFIRDAN ortam — panel sırları YOK.
func tenantEnv(sk string) []string {
	return []string{
		"HOME=/home/" + sk,
		"USER=" + sk, "LOGNAME=" + sk,
		"PATH=" + sistemPATH,
		"COMPOSER_HOME=/home/" + sk + "/.composer",
		"COMPOSER_ALLOW_SUPERUSER=0",
		"NPM_CONFIG_CACHE=/home/" + sk + "/.npm",
		"LANG=C.UTF-8",
	}
}

// TenantExec: kısa/senkron komut. runuser -u sk -- bin args... ; cwd tenant home
// altında (EvalSymlinks confine); timeout + çıktı cap. bin MUTLAK yol olmalı.
func TenantExec(ctx context.Context, sk, cwd, bin string, args ...string) (string, bool) {
	return tenantExecEnv(ctx, sk, cwd, tenantEnv(sk), bin, args...)
}

// tenantExecEnv: TenantExec çekirdeği + özel env (npm için PATH prefix vb.).
func tenantExecEnv(ctx context.Context, sk, cwd string, env []string, bin string, args ...string) (string, bool) {
	if !gecerliSK(sk) {
		return "geçersiz kullanıcı", false
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil || !(real == "/home/"+sk || strings.HasPrefix(real, "/home/"+sk+"/")) {
		return "çalışma dizini tenant ev dizini dışında", false
	}
	release := execGate(sk) // tenant başına eşzamanlılık sınırı (DoS savunması)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, kisaTimeout)
	defer cancel()
	argv := append([]string{"-u", sk, "--", bin}, args...)
	cmd := exec.CommandContext(ctx, "runuser", argv...)
	cmd.Dir = real
	cmd.Env = env
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	runErr := cmd.Run()
	out := ansiTemizle(buf.String()) // ANSI renk kodlarını temizle (Laravel --ansi zorlar)
	if len(out) > ciktiCap {
		out = out[len(out)-ciktiCap:]
	}
	if ctx.Err() == context.DeadlineExceeded {
		return out + "\n\n[zaman aşımı: komut 120sn'de tamamlanmadı, sonlandırıldı]", false
	}
	return out, runErr == nil
}

// systemdRunDetached: uzun tek-seferlik iş (create-project, deploy). Transient unit
// olarak tenant uid/gid + cgroup slice içinde başlatır; çıktı logPath'e append edilir.
// Panel goroutine'i beklemez → frontend log-tail poll eder. Unit adı döner.
func systemdRunDetached(sk, cwd, unit, logPath string, argv ...string) error {
	if !gecerliSK(sk) {
		return fmt.Errorf("geçersiz kullanıcı")
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil || !(real == "/home/"+sk || strings.HasPrefix(real, "/home/"+sk+"/")) {
		return fmt.Errorf("çalışma dizini tenant ev dizini dışında")
	}
	// log ROOT-sahipli, tenant-YAZILAMAZ dizinde (symlink-takip felaketi önlenir —
	// denetim #1). Tenant burada symlink yaratamaz; chown YOK. Taze başla.
	ensureLogRoot()
	_ = os.Remove(logPath)
	sr := []string{
		"--unit=" + unit,
		"--uid=" + sk, "--gid=" + sk,
		"--working-directory=" + real,
		"--slice=girginos-" + sk + ".slice",
		"-p", "RuntimeMaxSec=1800",
		"-p", "StandardOutput=append:" + logPath,
		"-p", "StandardError=append:" + logPath,
		"-E", "HOME=/home/" + sk,
		"-E", "PATH=" + sistemPATH,
		"-E", "COMPOSER_HOME=/home/" + sk + "/.composer",
		"-E", "COMPOSER_ALLOW_SUPERUSER=0",
		"-E", "NPM_CONFIG_CACHE=/home/" + sk + "/.npm",
		"-E", "LANG=C.UTF-8",
		"--",
	}
	sr = append(sr, argv...)
	out, err := exec.Command("systemd-run", sr...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, string(out))
	}
	return nil
}

// unitDurum: transient/servis unit ActiveState (tek -p → --value güvenli).
func unitDurum(unit string) string {
	out, _ := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", unit).Output()
	return strings.TrimSpace(string(out))
}

// dosyaTail: dosyanın son max byte'ı (log kuyruğu). O_NOFOLLOW + düzenli-dosya +
// LimitReader → symlink takibi (/etc/shadow) ve /dev/zero OOM'u reddedilir (denetim #3/#5).
func dosyaTail(path string, max int64) string {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi.Size() > max {
		_, _ = f.Seek(-max, io.SeekEnd)
	}
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return ansiTemizle(string(b)) // deploy/kur logları da ANSI içerebilir
}

// gecerliRepoURL: git.go ile aynı katılık (shell metakarakter reddi + şema).
func gecerliRepoURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" || len(u) > 512 {
		return false
	}
	for _, c := range u {
		if c <= ' ' || strings.ContainsRune(badURLChars, c) {
			return false
		}
	}
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://")
}

// envOku: appRoot/.env oku. O_NOFOLLOW + düzenli-dosya + LimitReader → tenant'ın
// .env'i /etc/girginospanel/env veya başka tenant .env'ine symlink'lemesiyle root'un
// panel sırlarını/cross-tenant okuması VE /dev/zero OOM'u reddedilir (denetim #2/#5).
func envOku(appDir string) (string, error) {
	p := filepath.Join(appDir, ".env")
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf(".env düzenli bir dosya değil")
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 2<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// envYaz: .env'i TENANT OLARAK yaz (runuser tee) → doğru sahiplik + uid-confined
// (tenant kendi izinleri dışına yazamaz; root symlink-follow felaketi YOK).
func envYaz(sk, appDir, icerik string) error {
	if len(icerik) > 5<<20 {
		return fmt.Errorf(".env çok büyük")
	}
	dst := filepath.Join(appDir, ".env")
	cmd := exec.Command("runuser", "-u", sk, "--", "/usr/bin/tee", dst)
	cmd.Env = tenantEnv(sk)
	cmd.Stdin = strings.NewReader(icerik)
	var errBuf bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(".env yazılamadı: %v: %s", err, errBuf.String())
	}
	return nil
}
