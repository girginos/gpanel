package evalti

// Regresyon: ev köküne çapalı root dosya işlemleri ve kiracı komutu. Tümü t.TempDir
// içinde; kiracı yerine ayrıcalıksız "nobody" kullanılır, canlı /home'a dokunulmaz.

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// nobodyKimlik: root ise nobody'nin uid/gid'i; değilse test atlanır (başka kullanıcıya
// sahiplik vermek ve runuser root ister).
func nobodyKimlik(t *testing.T) (int, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("başka kullanıcı kimliği için root gerekli")
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid
}

func nobodyKullan(t *testing.T) {
	t.Helper()
	eski := OSKullanici
	OSKullanici = func(string) string { return "nobody" }
	t.Cleanup(func() { OSKullanici = eski })
}

func dosyaYaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGecerliSK(t *testing.T) {
	for sk, want := range map[string]bool{
		"c_site":                       true,
		"c_a1_b2":                      true,
		"c_" + strings.Repeat("a", 30): true,
		"c_" + strings.Repeat("a", 31): false,
		"c_":                           false,
		"root":                         false,
		"nobody":                       false,
		"c_A":                          false,
		"c_x/y":                        false,
		"c_..":                         false,
		"c_x\n":                        false,
		"-c_x":                         false,
		"c_x-y":                        false,
		"c_x y":                        false,
	} {
		if got := GecerliSK(sk); got != want {
			t.Errorf("GecerliSK(%q) = %v, beklenen %v", sk, got, want)
		}
	}
}

func TestEvDizini_Kimlik(t *testing.T) {
	uid, gid := nobodyKimlik(t)
	nobodyKullan(t)
	eski := EvKoku
	EvKoku = "/tmp/evkoku"
	t.Cleanup(func() { EvKoku = eski })

	if ev, err := EvDizini("c_site"); err != nil || ev != "/tmp/evkoku/c_site" {
		t.Fatalf("EvDizini = %q, %v", ev, err)
	}
	for _, sk := range []string{"", "root", "c_../x", "c_x/y"} {
		if _, err := EvDizini(sk); err == nil {
			t.Errorf("EvDizini(%q) hata vermeli", sk)
		}
		if _, _, err := Kimlik(sk); err == nil {
			t.Errorf("Kimlik(%q) hata vermeli", sk)
		}
	}
	u, g, err := Kimlik("c_site")
	if err != nil || u != uid || g != gid {
		t.Fatalf("Kimlik = %d:%d, %v; beklenen %d:%d", u, g, err, uid, gid)
	}
}

func TestAc_SymlinkVeUstDizinReddi(t *testing.T) {
	home := t.TempDir()
	dis := t.TempDir()
	dosyaYaz(t, filepath.Join(dis, "gizli"), "DIS")
	if err := os.Mkdir(filepath.Join(home, "gercek"), 0o755); err != nil {
		t.Fatal(err)
	}
	dosyaYaz(t, filepath.Join(home, "gercek", "f"), "ICERDE")
	if err := os.Symlink(dis, filepath.Join(home, "bag")); err != nil {
		t.Fatal(err)
	}
	// Göreli ve ev içinde kalan bağ: RESOLVE_BENEATH tek başına bunu izlerdi.
	if err := os.Symlink("gercek", filepath.Join(home, "icbag")); err != nil {
		t.Fatal(err)
	}

	fd, err := Ac(home, "gercek/f", unix.O_RDONLY)
	if err != nil {
		t.Fatalf("düz yol açılmalı: %v", err)
	}
	unix.Close(fd)
	fd, err = Ac(home, "", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		t.Fatalf("ev kökü açılmalı: %v", err)
	}
	unix.Close(fd)

	for _, rel := range []string{
		"bag/gizli", // dışarı çıkan bağ
		"icbag/f",   // içeride kalan bağ da izlenmez
		"../" + filepath.Base(dis) + "/gizli",
		"gercek/../gercek/f", // '..' kırpılmaz, reddedilir
		"/etc/passwd",
	} {
		if fd, err := Ac(home, rel, unix.O_RDONLY); err == nil {
			unix.Close(fd)
			t.Errorf("Ac(%q) reddedilmeliydi", rel)
		}
	}

	// Ev yolunun kendisi bağ ise açılmaz.
	evBag := filepath.Join(t.TempDir(), "evbag")
	if err := os.Symlink(home, evBag); err != nil {
		t.Fatal(err)
	}
	if fd, err := Ac(evBag, "gercek/f", unix.O_RDONLY); err == nil {
		unix.Close(fd)
		t.Error("bağ olan ev dizini reddedilmeliydi")
	}
}

func TestMkdirHepsi(t *testing.T) {
	uid, gid := nobodyKimlik(t)
	home := t.TempDir()
	dis := t.TempDir()

	if err := MkdirHepsi(home, "a/b/c", uid, gid, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a", "a/b", "a/b/c"} {
		st, err := os.Lstat(filepath.Join(home, rel))
		if err != nil {
			t.Fatal(err)
		}
		sys := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm() != 0o750 || int(sys.Uid) != uid || int(sys.Gid) != gid {
			t.Errorf("%s: mod=%v sahip=%d:%d", rel, st.Mode(), sys.Uid, sys.Gid)
		}
	}

	// Var olan bileşenin sahipliğine/iznine dokunulmaz.
	if err := os.Mkdir(filepath.Join(home, "var"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MkdirHepsi(home, "var/yeni", uid, gid, 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Lstat(filepath.Join(home, "var"))
	if sys := st.Sys().(*syscall.Stat_t); sys.Uid != 0 || st.Mode().Perm() != 0o700 {
		t.Errorf("var olan dizin değişti: mod=%v uid=%d", st.Mode(), sys.Uid)
	}

	// Bileşen symlink ise durur, dışarıda dizin yaratılmaz.
	if err := os.Symlink(dis, filepath.Join(home, "bag")); err != nil {
		t.Fatal(err)
	}
	if err := MkdirHepsi(home, "bag/kacis", uid, gid, 0o755); err == nil {
		t.Error("symlink bileşen reddedilmeliydi")
	}
	if _, err := os.Lstat(filepath.Join(dis, "kacis")); err == nil {
		t.Error("ev dışında dizin yaratıldı")
	}

	// '..' kırpılmaz, reddedilir; hiçbir şey yaratılmaz.
	if err := MkdirHepsi(home, "x/../../kacis2", uid, gid, 0o755); err == nil {
		t.Error("'..' reddedilmeliydi")
	}
	if _, err := os.Lstat(filepath.Join(home, "x")); err == nil {
		t.Error("'..' içeren yolda dizin yaratıldı")
	}

	// Mutlak yol ev köküne göre yeniden yorumlanmaz, reddedilir.
	if err := MkdirHepsi(home, "/mutlak/y", uid, gid, 0o755); err == nil {
		t.Error("mutlak yol reddedilmeliydi")
	}
	if _, err := os.Lstat(filepath.Join(home, "mutlak")); err == nil {
		t.Error("mutlak yol ev altında yaratıldı")
	}
}

func TestMkdirHepsi_AdTakasi(t *testing.T) {
	uid, gid := nobodyKimlik(t)
	home := t.TempDir()
	dolu := filepath.Join(home, "dolu")
	if err := os.Mkdir(dolu, 0o700); err != nil {
		t.Fatal(err)
	}
	dosyaYaz(t, filepath.Join(dolu, "kok.txt"), "KOK")

	yarisKancasi = func(dfd int, ad string) {
		if err := unix.Renameat2(dfd, ad, dfd, "dolu", unix.RENAME_EXCHANGE); err != nil {
			t.Errorf("takas: %v", err)
		}
	}
	t.Cleanup(func() { yarisKancasi = nil })

	if err := MkdirHepsi(home, "yeni", uid, gid, 0o755); err == nil {
		t.Fatal("ad takası yakalanmalıydı")
	}
	// Takasla "yeni" adına gelen dolu (root) dizin kiracıya devredilmemeli.
	st, err := os.Lstat(filepath.Join(home, "yeni"))
	if err != nil {
		t.Fatal(err)
	}
	if sys := st.Sys().(*syscall.Stat_t); sys.Uid != 0 || st.Mode().Perm() != 0o700 {
		t.Fatalf("dolu dizin kiracıya geçti: mod=%v uid=%d", st.Mode(), sys.Uid)
	}
}

func TestKiraciKomut(t *testing.T) {
	nobodyKullan(t)
	eski := EvKoku
	EvKoku = "/tmp/evkoku"
	t.Cleanup(func() { EvKoku = eski })

	cmd, err := KiraciKomut(context.Background(), "c_site", "env")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"runuser", "-u", "nobody", "--", "env"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("argv = %q, beklenen %q", cmd.Args, want)
	}
	if want := []string{kiraciPATH, "HOME=/tmp/evkoku/c_site", "LANG=C"}; !reflect.DeepEqual(cmd.Env, want) {
		t.Fatalf("env = %q, beklenen %q", cmd.Env, want)
	}
	if cmd.Dir != "/" || cmd.Cancel == nil || cmd.WaitDelay == 0 {
		t.Fatalf("dizin/iptal ayarı eksik: dir=%q", cmd.Dir)
	}

	for _, c := range []struct {
		sk   string
		argv []string
	}{
		{"../x", []string{"id"}},
		{"root", []string{"id"}},
		{"c_site", nil},          // argv'siz runuser kabuk açardı
		{"c_site", []string{""}}, // boş komut adı
	} {
		if _, err := KiraciKomut(context.Background(), c.sk, c.argv...); err == nil {
			t.Errorf("KiraciKomut(%q, %q) reddedilmeliydi", c.sk, c.argv)
		}
	}
}

// TestKiraciKomut_GercekKimlik: runuser gerçekten kiracı kimliğinde ve panel ortamı
// olmadan koşar.
func TestKiraciKomut_GercekKimlik(t *testing.T) {
	nobodyKimlik(t)
	if _, err := exec.LookPath("runuser"); err != nil {
		t.Skip("runuser yok")
	}
	nobodyKullan(t)
	t.Setenv("GOSP_PANEL_SIRRI_TEST", "SIZMAMALI")

	cmd, err := KiraciKomut(context.Background(), "c_site", "sh", "-c", "id -un; env")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("runuser: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "nobody\n") {
		t.Fatalf("kiracı kimliği değil: %q", s)
	}
	if strings.Contains(s, "SIZMAMALI") {
		t.Fatal("panel ortamı kiracı komutuna sızdı")
	}
}
