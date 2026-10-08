package provisioner

// homeperms_symlink_test.go — denetim-b: hardenHomePerms/hardenHomePermsRecursive kök olarak
// çalışır; kiracı public_html'i symlink'e çevirirse (public_html -> /etc) os.Chown/os.Chmod/
// setfacl bağı izleyip kök sahipli hedefi kiracıya devrederdi (yerel yetki yükseltme).

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHardenHomePerms_PublicHtmlSymlinkIzlenmez(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("kök gerekli (chown denemesi)")
	}
	kok := t.TempDir()
	hedef := filepath.Join(kok, "korunan")
	if err := os.Mkdir(hedef, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(kok, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hedef, filepath.Join(home, "public_html")); err != nil {
		t.Fatal(err)
	}
	const sahteUID = 31337
	hardenHomePerms(home, "c_symtest", sahteUID, sahteUID)
	hardenHomePermsRecursive(filepath.Join(home, "public_html"))

	st := new(syscall.Stat_t)
	if err := syscall.Stat(hedef, st); err != nil {
		t.Fatal(err)
	}
	if int(st.Uid) == sahteUID {
		t.Fatal("symlink izlendi: hedef dizin kiracıya chown edildi")
	}
	if st.Mode&0o7777 != 0o700 {
		t.Fatalf("symlink izlendi: hedef modu değişti: %o", st.Mode&0o7777)
	}
}

func TestHardenHomePerms_GercekPublicHtmlHalaDuzenlenir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("kök gerekli")
	}
	kok := t.TempDir()
	home := filepath.Join(kok, "home")
	ph := filepath.Join(home, "public_html")
	if err := os.MkdirAll(ph, 0o777); err != nil {
		t.Fatal(err)
	}
	const sahteUID = 31337
	hardenHomePerms(home, "c_symtest2", sahteUID, sahteUID)
	st := new(syscall.Stat_t)
	if err := syscall.Stat(ph, st); err != nil {
		t.Fatal(err)
	}
	if int(st.Uid) != sahteUID || st.Mode&0o777 != 0o750 {
		t.Fatalf("gerçek public_html düzenlenmedi: uid=%d mod=%o", st.Uid, st.Mode&0o777)
	}
}
