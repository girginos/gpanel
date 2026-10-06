package kiraci

// Dilim sözleşmeleri: (1) DilimSK slice adını belirler, Kul kimliği belirler;
// (2) slice birimi/dosyası YOKSA bile komut çalışır (sınırsız) ve çıkış kodu +
// stdout/stderr aynen geçer — GERÇEK systemd-run ile (root, 181).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

func TestKomut_DilimSK_SliceAdiniBelirler(t *testing.T) {
	sahteKiraciKullanici(t)
	// Kul c_ornek_com (kimlik), slice c_baska_sk'den.
	cmd, err := Kiraci{Kul: "c_ornek_com", Ev: "/home/x", Dilim: true, DilimSK: "c_baska_sk"}.Komut(context.Background(), "id")
	if err != nil {
		t.Fatal(err)
	}
	var sl, desc, kul string
	for i, a := range cmd.Args {
		switch {
		case strings.HasPrefix(a, "--slice="):
			sl = a
		case strings.HasPrefix(a, "--description="):
			desc = a
		case a == "-u" && i+1 < len(cmd.Args):
			kul = cmd.Args[i+1]
		}
	}
	if sl != "--slice=girginos-c_baska_sk.slice" || !strings.HasSuffix(desc, "c_baska_sk") || kul != "c_ornek_com" {
		t.Fatalf("slice=%q desc=%q kul=%q argv=%q", sl, desc, kul, cmd.Args)
	}
	// Biçimsiz DilimSK reddedilir (argv enjeksiyonu), dilimsiz yola düşülmez.
	for _, kotu := range []string{"root", "c_x --uid=0", "../c_x", "-c_x"} {
		c, err := Kiraci{Kul: "c_ornek_com", Ev: "/home/x", Dilim: true, DilimSK: kotu}.Komut(context.Background(), "id")
		if !errors.Is(err, ErrKomut) || c != nil {
			t.Errorf("DilimSK=%q reddedilmeliydi: %v", kotu, err)
		}
	}
}

// Gerçek systemd-run: slice birimi YOK (girginos-c_slctest_yok.slice hiç yazılmadı).
// Komut yine çalışır, çıkış kodu ve çıktılar korunur, süreç istenen slice'ın
// (örtük) cgroup'una girer, kimlik Kul'dur.
func TestKomut_Dilim_SliceYokkenCalisir_GercekSystemdRun(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("kapsam açmak root gerektirir (181'de testler root koşar)")
	}
	for _, ad := range []string{"systemd-run", "runuser"} {
		if _, err := exec.LookPath(ad); err != nil {
			t.Skipf("%s yok: %v", ad, err)
		}
	}
	if fi, err := os.Stat("/run/systemd/system"); err != nil || !fi.IsDir() {
		t.Skip("systemd çalışmıyor")
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody yok: %v", err)
	}
	const sk = "c_slctest_yok"
	if _, err := os.Stat("/etc/systemd/system/girginos-" + sk + ".slice"); err == nil {
		t.Skip("test slice'ı beklenmedik biçimde var")
	}
	cmd, err := Kiraci{Kul: "nobody", Ev: "/", Dilim: true, DilimSK: sk}.Komut(context.Background(),
		"/bin/sh", "-c", "id -u; echo cikti; echo hata >&2; cat /proc/self/cgroup; exit 7")
	if err != nil {
		t.Fatal(err)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("çıkış kodu 7 korunmalı: %v (stdout %q stderr %q)", err, so.String(), se.String())
	}
	if !strings.HasPrefix(so.String(), u.Uid+"\ncikti\n") {
		t.Errorf("kimlik/stdout bozuk: %q", so.String())
	}
	if strings.TrimSpace(se.String()) != "hata" {
		t.Errorf("stderr bozuk: %q", se.String())
	}
	if !strings.Contains(so.String(), "girginos-"+sk+".slice") {
		t.Errorf("süreç istenen slice'a girmedi: %q", so.String())
	}
}
