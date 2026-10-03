package kaynaklimit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oturumSahte: geçici systemd dizini, sahte uid çözümü ve kayıtlı systemctl.
func oturumSahte(t *testing.T, aktif bool) *[][]string {
	t.Helper()
	eKok, eUID, eSys := oturumDilimKok, uidBul, systemctlCalistir
	t.Cleanup(func() { oturumDilimKok, uidBul, systemctlCalistir = eKok, eUID, eSys })
	oturumDilimKok = t.TempDir()
	uidBul = func(sk string) (string, error) {
		switch sk {
		case "c_ornek_com":
			return "5007", nil
		case "c_sistem":
			return "999", nil
		case "c_kok":
			return "0", nil
		}
		return "", errors.New("yok")
	}
	var cagrilar [][]string
	systemctlCalistir = func(args ...string) ([]byte, error) {
		cagrilar = append(cagrilar, args)
		if args[0] == "is-active" {
			if aktif {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), errors.New("3")
		}
		return nil, nil
	}
	return &cagrilar
}

func TestOturumDilimi_YazCanliSil(t *testing.T) {
	cagrilar := oturumSahte(t, true)
	l := Limitler{CPUYuzde: 150, RAMMB: 1024, MaxProcess: 80, IOAgirlik: 200, IOWriteMBps: 20}
	// Aynı UID'e eski kiracı adıyla kalmış drop-in (UID yeniden kullanımı) silinmeli.
	dir := filepath.Join(oturumDilimKok, "user-5007.slice.d")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "50-girginospanel-c_eski.conf"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "10-yonetici.conf"), []byte("x"), 0o644)

	uid, err := oturumDilimiYaz("c_ornek_com", l)
	if err != nil || uid != "5007" {
		t.Fatalf("yaz: %q %v", uid, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "50-girginospanel-c_ornek_com.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"[Slice]\n", "CPUQuota=150%\n", "MemoryMax=1024M\n", "MemoryHigh=921M\n", "TasksMax=80\n", "IOWeight=200\n", "IOWriteBandwidthMax=/home 20M\n"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("drop-in %q içermiyor:\n%s", s, b)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "50-girginospanel-c_eski.conf")); err == nil {
		t.Error("aynı UID'deki eski kiracı drop-in'i kalmamalı")
	}
	if _, err := os.Stat(filepath.Join(dir, "10-yonetici.conf")); err != nil {
		t.Error("panele ait olmayan drop-in'e dokunulmamalı")
	}

	oturumDilimiCanli(uid, l)
	son := (*cagrilar)[len(*cagrilar)-1]
	if strings.Join(son[:3], " ") != "set-property --runtime user-5007.slice" || !strings.Contains(strings.Join(son, " "), "MemoryMax=1024M") || !strings.Contains(strings.Join(son, " "), "TasksMax=80") {
		t.Fatalf("canlı uygulama: %q", son)
	}

	*cagrilar = nil
	oturumDilimiSil("c_ornek_com") // uid çözülmese de (userdel sonrası) glob ile bulunur
	if _, err := os.Stat(filepath.Join(dir, "50-girginospanel-c_ornek_com.conf")); err == nil {
		t.Fatal("drop-in silinmedi")
	}
	j := strings.Join(func() []string {
		var o []string
		for _, c := range *cagrilar {
			o = append(o, strings.Join(c, " "))
		}
		return o
	}(), "\n")
	if !strings.Contains(j, "set-property --runtime user-5007.slice CPUQuota= MemoryMax=infinity") || !strings.Contains(j, "daemon-reload") {
		t.Fatalf("silmede çalışma zamanı sıfırlama/daemon-reload yok:\n%s", j)
	}
}

func TestOturumDilimi_SistemKullanicisinaDokunmaz(t *testing.T) {
	oturumSahte(t, false)
	for _, sk := range []string{"c_sistem", "c_kok", "c_yok", "root", "c_x/../y", "c_X"} {
		if _, err := oturumDilimiYaz(sk, Limitler{RAMMB: 1}); err == nil {
			t.Errorf("%q reddedilmeliydi", sk)
		}
	}
	ents, _ := os.ReadDir(oturumDilimKok)
	if len(ents) != 0 {
		t.Fatalf("reddedilen kiracı için dizin yazıldı: %v", ents)
	}
}
