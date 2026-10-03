package tasima

// rsync_ozel_linux_test.go — RsyncCek'in GERÇEK yolu (kabuk == nil): kaynak sunucudaki
// aygıt düğümü, fifo, soket ve yabancı uid/gid hedefe TAŞINMAZ. Aynı düzenek (sahteSSH)
// aktar_dosya_rsync_linux_test.go'daki uçtan uca yerleşim testinde de kullanılır.
//
// Ağa çıkılmaz: PATH'in başına sshpass ve ssh taklitleri konur. sshpass taklidi -e ile
// SSHPASS'ı denetleyip rsync'i çalıştırır; ssh taklidi host'a kadarki ssh bayraklarını
// atlar ve uzak komutu (rsync --server --sender …) gerçek ssh'in uzak kabuğu gibi yerelde
// sh -c ile çalıştırır. Böylece alıcı ve gönderici tarafında GERÇEK rsync koşar; kaynak,
// t.TempDir içinde kurulan sahte sunucu ağacıdır. mknod/chown için euid 0 gerekir (181'de
// testler root koşar); değilse atlanır.

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sahteSSHHost — ssh taklidinin tanıdığı tek ad (TEST-NET-1; ağa çıkılmaz).
const sahteSSHHost = "192.0.2.10"

// sahteSSH — PATH'in başına sshpass ve ssh taklitlerini koyar (bkz. dosya başı).
// Döner: ssh taklidinin son argv'sinin ve sshpass'a gelen yerel rsync argv'lerinin
// (her çağrı "----" satırıyla biter) kayıt dosyaları.
func sahteSSH(t *testing.T) (sshArgv, rsyncArgv string) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync yok")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh yok")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sshArgv, rsyncArgv = filepath.Join(tmp, "ssh.argv"), filepath.Join(tmp, "rsync.argv")
	for ad, betik := range map[string]string{
		"sshpass": `#!/bin/sh
if [ "$1" != -e ] || [ -z "$SSHPASS" ]; then echo 'sshpass taklidi: -e/SSHPASS yok' >&2; exit 3; fi
shift
printf '%s\n' "$@" ---- >> '` + rsyncArgv + `'
exec "$@"
`,
		"ssh": `#!/bin/sh
printf '%s\n' "$@" > '` + sshArgv + `'
while [ $# -gt 0 ]; do
	a=$1
	shift
	if [ "$a" = '` + sahteSSHHost + `' ]; then exec /bin/sh -c "$*"; fi
done
echo 'ssh taklidi: host yok' >&2
exit 255
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, ad), []byte(betik), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return sshArgv, rsyncArgv
}

// sahteSSHUzakKomut — ssh taklidine rsync gönderici komutu gerçekten gitti mi?
func sahteSSHUzakKomut(t *testing.T, sshArgv string) {
	t.Helper()
	argv, err := os.ReadFile(sshArgv)
	if err != nil {
		t.Fatalf("ssh taklidi çağrılmadı: %v", err)
	}
	for _, parca := range []string{"\n" + sahteSSHHost + "\nrsync\n", "\n--server\n", "\n--sender\n"} {
		if !strings.Contains(string(argv), parca) {
			t.Fatalf("ssh taklidine beklenen uzak komut gitmedi (%q yok):\n%s", parca, argv)
		}
	}
}

func TestRsyncCekOzelDosyaVeSahiplikTasimaz(t *testing.T) {
	const (
		yabanciUID = 12345
		yabanciGID = 23456
	)
	if os.Geteuid() != 0 {
		t.Skip("mknod/chown için root gerekir")
	}
	sshArgv, _ := sahteSSH(t)

	tmp := t.TempDir()
	kaynak, hedef := filepath.Join(tmp, "kaynak"), filepath.Join(tmp, "hedef")
	for _, d := range []string{kaynak, filepath.Join(kaynak, "alt"), hedef} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaz := func(yol, icerik string, mod os.FileMode) {
		t.Helper()
		if err := os.WriteFile(yol, []byte(icerik), mod); err != nil {
			t.Fatal(err)
		}
	}

	// Sahte kaynak ağacı: meşru içerik (dosya, alt dizin, göreli bağ) + aygıt düğümleri
	// (1:3 = null eşdeğeri, 7:250 = kullanılmayan loop), fifo ve soket; HEPSİ yabancı uid/gid.
	yaz(filepath.Join(kaynak, "normal.txt"), "merhaba", 0o644)
	yaz(filepath.Join(kaynak, "alt", "ic.txt"), "ic", 0o644)
	if err := os.Symlink("normal.txt", filepath.Join(kaynak, "bag")); err != nil {
		t.Fatal(err)
	}
	ozeller := map[string]uint32{
		"aygit-c": unix.S_IFCHR | 0o666,
		"aygit-b": unix.S_IFBLK | 0o660,
		"fifo":    unix.S_IFIFO | 0o644,
		"soket":   unix.S_IFSOCK | 0o644,
	}
	for ad, mod := range ozeller {
		dev := 0
		switch mod & unix.S_IFMT {
		case unix.S_IFCHR:
			dev = int(unix.Mkdev(1, 3))
		case unix.S_IFBLK:
			dev = int(unix.Mkdev(7, 250))
		}
		if err := unix.Mknod(filepath.Join(kaynak, ad), mod, dev); err != nil {
			t.Fatalf("mknod %s: %v", ad, err)
		}
	}
	if err := filepath.WalkDir(kaynak, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, yabanciUID, yabanciGID)
	}); err != nil {
		t.Fatal(err)
	}
	// Fikstür anlamlı mı: kaynakta her girdi yabancı uid/gid'e ait ve özel dosyalar var.
	if fi, err := os.Lstat(filepath.Join(kaynak, "aygit-b")); err != nil || fi.Mode()&fs.ModeDevice == 0 ||
		fi.Sys().(*syscall.Stat_t).Uid != yabanciUID || fi.Sys().(*syscall.Stat_t).Gid != yabanciGID {
		t.Fatalf("fikstür kurulamadı: %v", err)
	}

	k := &Kaynak{Tip: "plesk", Host: sahteSSHHost, Port: 22, Kullanici: "root", Parola: "taklit-parola"}
	ctx, iptal := context.WithTimeout(context.Background(), 60*time.Second)
	defer iptal()
	if _, err := k.RsyncCek(ctx, kaynak+"/", hedef+"/"); err != nil {
		t.Fatalf("RsyncCek: %v", err)
	}

	// Aktarım gerçekten ssh taklidi → yerel rsync gönderici üzerinden koştu.
	sahteSSHUzakKomut(t, sshArgv)

	// Meşru içerik geldi (test boşuna geçmiyor).
	for ad, beklenen := range map[string]string{"normal.txt": "merhaba", "alt/ic.txt": "ic"} {
		if b, err := os.ReadFile(filepath.Join(hedef, ad)); err != nil || string(b) != beklenen {
			t.Fatalf("%s aktarılmadı: %q, %v", ad, b, err)
		}
	}
	if l, err := os.Readlink(filepath.Join(hedef, "bag")); err != nil || l != "normal.txt" {
		t.Fatalf("göreli bağ aktarılmadı: %q, %v", l, err)
	}

	for ad := range ozeller {
		if _, err := os.Lstat(filepath.Join(hedef, ad)); !os.IsNotExist(err) {
			t.Errorf("özel dosya hedefe taşındı: %s (%v)", ad, err)
		}
	}
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	if err := filepath.WalkDir(hedef, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		ad, _ := filepath.Rel(hedef, p)
		switch tip := fi.Mode().Type(); tip {
		case 0, fs.ModeDir, fs.ModeSymlink:
		default:
			t.Errorf("hedefte özel dosya: %s (%v)", ad, tip)
		}
		st := fi.Sys().(*syscall.Stat_t)
		if st.Uid != uid {
			t.Errorf("yabancı sahip hedefe taşındı: %s uid=%d", ad, st.Uid)
		}
		if st.Gid != gid {
			t.Errorf("yabancı grup hedefe taşındı: %s gid=%d", ad, st.Gid)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
