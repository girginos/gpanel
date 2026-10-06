package opsbetik

// girginospanel-repair sec_clamav: imza dizininin sahibi ADLA (clamupdate) bulunmalı, sabit UID ile DEĞİL.
//
// Gerçek arıza (2026-10-06, temiz AlmaLinux 10 test sunucusu): betik `getent passwd 992`'yi ClamAV kullanıcısı
// sanıyordu; o kurulumda 992 = valkey idi → /var/lib/clamav valkey'e devredildi, freshclam (clamupdate olarak
// çalışır) "Can't create temporary directory" ile her güncellemede düştü, imzalar bayatladı.
// CANLI SİSTEME DOKUNMAZ: dizin t.TempDir, UID araması sahte `getent` ile (PATH'in başına konur).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestSecClamav_SahipAdlaBulunur_SabitUIDDegil(t *testing.T) {
	rootGerek(t)
	out, err := exec.Command("id", "-u", "clamupdate").Output()
	if err != nil {
		t.Skip("clamupdate kullanıcısı yok")
	}
	beklenen, _ := strconv.Atoi(strings.TrimSpace(string(out)))

	d := t.TempDir()
	imza := filepath.Join(d, "clamav")
	if err := os.MkdirAll(imza, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imza, "daily.cvd"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sahte getent: 992 → nobody (eski kod ClamAV sahibi diye bunu seçerdi).
	bin := filepath.Join(d, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sahte := "#!/bin/sh\n[ \"$1\" = passwd ] && [ \"$2\" = 992 ] && { echo 'nobody:x:992:992::/:/sbin/nologin'; exit 0; }\nexec /usr/bin/getent \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "getent"), []byte(sahte), 0o755); err != nil {
		t.Fatal(err)
	}

	betik := `f=$1; set --; source "$f" || exit 90; SEL=0; sec_clamav; exit 0`
	cmd := exec.Command("bash", "-c", betik, "bash", betikYolu(t))
	cmd.Env = append(os.Environ(), "GOSP_REPAIR_KUTUPHANE=1", "LC_ALL=C", "GOSP_CLAMAV_DIZIN="+imza, "PATH="+bin+":"+os.Getenv("PATH"))
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("betik hatası (%v): %s", err, o)
	}
	for _, p := range []string{imza, filepath.Join(imza, "daily.cvd")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if uid := int(fi.Sys().(*syscall.Stat_t).Uid); uid != beklenen {
			t.Errorf("%s sahibi uid=%d, clamupdate (%d) olmalı — sabit UID araması geri gelmiş", p, uid, beklenen)
		}
	}
}
