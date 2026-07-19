package provisioner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// SALDIRGAN yeniden-dogrulama: writeDebugShim symlink/TOCTOU fix'i.
// Tenant, Debug acilmadan ONCE /home/<sk>/.gpanel'i baska bir 'kurban' yola symlink yapar.
// Fix DOGRU ise: installDebugShim symlink'i TAKIP ETMEZ; kurbani chown/yaz ETMEZ; .gpanel'i
// gercek root:root dizine cevirir. Test root gerektirir (fchown/symlink semantigi icin).
func TestInstallDebugShim_SymlinkGpanelAttackBlocked(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root gerekli (symlink/chown semantigi)")
	}
	base := t.TempDir()
	home := filepath.Join(base, "home") // sahte tenant home (guvenli temp)
	if err := os.Mkdir(home, 0o710); err != nil {
		t.Fatal(err)
	}

	// KURBAN dizin: saldirganin .gpanel'i buraya symlink'leyerek chown-to-root DoS +
	// keyfi root-yaz hedefledigi yer. root-DISI sahibe atanir ki chown tespit edilsin.
	victim := filepath.Join(base, "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chown(victim, 65534, 65534); err != nil {
		t.Fatalf("kurban chown-nobody: %v", err)
	}
	sentinel := filepath.Join(victim, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("DOKUNMA"), 0o600); err != nil {
		t.Fatal(err)
	}

	// >>> SALDIRI: home/.gpanel -> victim symlink (tenant Debug'dan once yapardi)
	gpath := filepath.Join(home, ".gpanel")
	if err := os.Symlink(victim, gpath); err != nil {
		t.Fatal(err)
	}

	// writeDebugShim'in GERCEK FS-cekirdegini tetikle (uretim kod yolu).
	installDebugShim(home, "c_toctou_atk", []byte("<?php /* shim */"))

	// (1) .gpanel artik SYMLINK DEGIL, gercek root:root dizin.
	var lst unix.Stat_t
	if err := unix.Lstat(gpath, &lst); err != nil {
		t.Fatalf(".gpanel lstat: %v", err)
	}
	if lst.Mode&unix.S_IFMT == unix.S_IFLNK {
		t.Fatal("BASARISIZ: .gpanel HALA symlink -> symlink TAKIP EDILDI")
	}
	if lst.Mode&unix.S_IFMT != unix.S_IFDIR {
		t.Fatal("BASARISIZ: .gpanel gercek dizin degil")
	}
	if lst.Uid != 0 || lst.Gid != 0 {
		t.Fatalf("BASARISIZ: .gpanel root:root degil (uid=%d gid=%d)", lst.Uid, lst.Gid)
	}

	// (2) KURBAN dokunulmamis: hala nobody-sahipli + sentinel duruyor + shim SIZMADI.
	var vst unix.Stat_t
	if err := unix.Lstat(victim, &vst); err != nil {
		t.Fatalf("kurban lstat: %v", err)
	}
	if vst.Uid != 65534 || vst.Gid != 65534 {
		t.Fatalf("BASARISIZ: kurban chown edildi -> CROSS-TENANT DoS (uid=%d gid=%d)", vst.Uid, vst.Gid)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("BASARISIZ: kurban sentinel kayboldu: %v", err)
	}
	if _, err := os.Stat(filepath.Join(victim, "debug_prepend.php")); err == nil {
		t.Fatal("BASARISIZ: kurbana debug_prepend.php YAZILDI -> keyfi root-yaz")
	}

	// (3) Shim GERCEK .gpanel icine yazildi (fonksiyonellik korundu).
	if _, err := os.Stat(filepath.Join(gpath, "debug_prepend.php")); err != nil {
		t.Fatalf("gercek .gpanel'de shim yok: %v", err)
	}
}

// Log dosyasi symlink saldirisi: mesru root .gpanel icinde php_debug.log bir kurban dosyaya
// symlink'lenirse, O_NOFOLLOW openat onu TAKIP ETMEMELI (kurban dosya truncate/yaz olmamali).
func TestInstallDebugShim_LogSymlinkNotFollowed(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root gerekli")
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o710); err != nil {
		t.Fatal(err)
	}
	// mesru root .gpanel (ilk Debug acilisi yapmis gibi)
	gp := filepath.Join(home, ".gpanel")
	if err := os.Mkdir(gp, 0o755); err != nil {
		t.Fatal(err)
	}
	victimLog := filepath.Join(base, "victim.conf")
	if err := os.WriteFile(victimLog, []byte("ONEMLI-VERI\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// >>> SALDIRI: .gpanel/php_debug.log -> victim.conf
	if err := os.Symlink(victimLog, filepath.Join(gp, "php_debug.log")); err != nil {
		t.Fatal(err)
	}

	installDebugShim(home, "c_toctou_log", []byte("<?php /* shim */"))

	// kurban dosya icerigi DEGISMEMIS (O_CREAT|O_APPEND|O_NOFOLLOW -> ELOOP, atlanir).
	b, err := os.ReadFile(victimLog)
	if err != nil {
		t.Fatalf("kurban okunamadi: %v", err)
	}
	if string(b) != "ONEMLI-VERI\n" {
		t.Fatalf("BASARISIZ: kurban log symlink uzerinden yazildi: %q", string(b))
	}
	// prepend yine de yazilabilmis olmali (ayri dosya, symlink yok)
	if _, err := os.Stat(filepath.Join(gp, "debug_prepend.php")); err != nil {
		t.Fatalf("prepend yazilmadi: %v", err)
	}
}

// ensureRootDirAt: tenant-sahipli (root-disi) mevcut .gpanel dizinini de guvensiz sayip
// root:root yeniden yaratmali (tenant onceden yazilabilir dizin koyup shim'i degistiremesin).
func TestEnsureRootDirAt_RejectsTenantOwnedDir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root gerekli")
	}
	base := t.TempDir()
	// tenant-sahipli .gpanel (uid nobody) — saldirgan onceden koymus
	gp := filepath.Join(base, ".gpanel")
	if err := os.Mkdir(gp, 0o777); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(gp, "tenant_owned.php")
	if err := os.WriteFile(planted, []byte("evil"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chown(gp, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	parentFd, err := unix.Open(base, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(parentFd)

	fd, ok := ensureRootDirAt(parentFd, ".gpanel")
	if !ok {
		t.Fatal("ensureRootDirAt basarisiz")
	}
	defer unix.Close(fd)
	var fst unix.Stat_t
	if err := unix.Fstat(fd, &fst); err != nil {
		t.Fatal(err)
	}
	if fst.Uid != 0 || fst.Gid != 0 {
		t.Fatalf("BASARISIZ: .gpanel root:root degil (uid=%d gid=%d)", fst.Uid, fst.Gid)
	}
	// tenant'in ektigi dosya temizlenmis olmali (dizin yeniden yaratildi)
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("BASARISIZ: tenant-ekli dosya korundu -> guvensiz dizin yeniden yaratilmadi")
	}
}

// Uretilen shim PHP'si sozdizimsel gecerli + fatal-yakalama iskeleti mevcut (php -l).
func TestRenderDebugPrependPHP_LintAndFatalGuard(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php yok")
	}
	src := renderDebugPrependPHP("c_lint", "")
	for _, needle := range []string{"register_shutdown_function", "error_get_last", "E_ERROR"} {
		if !strings.Contains(src, needle) {
			t.Fatalf("shim '%s' icermiyor:\n%s", needle, src)
		}
	}
	f := filepath.Join(t.TempDir(), "shim.php")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(php, "-l", f).CombinedOutput()
	if err != nil {
		t.Fatalf("php -l BASARISIZ: %v\n%s", err, out)
	}
}
