package kiraci

// Panel sırları (PANEL_JWT_SECRET, PANEL_DB_DSN, PANEL_REDIS_ADMIN_PASS) Komut'un
// başlattığı kiracı sürecine GEÇMEZ — hem hazırlanan ortamda (birim) hem gerçek
// runuser + /usr/bin/env çıktısında (root, 181). Değerler sahtedir (kiracitest).

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func sahteSirlar(t *testing.T) {
	t.Helper()
	for k, v := range kiracitest.SahteSirlar {
		t.Setenv(k, v)
	}
}

// Birim: Env nil DEĞİL (nil = panel ortamını miras) ve sır içermiyor.
func TestKomut_PanelSirlariOrtamdaYok(t *testing.T) {
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	sahteSirlar(t)
	cmd, err := Kiraci{Kul: "nobody", Ev: "/home/c_ornek"}.Komut(context.Background(), "/usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Env == nil {
		t.Fatal("cmd.Env nil: çocuk panelin TÜM süreç ortamını (sırlar dahil) miras alır")
	}
	if s := kiracitest.Sizinti(cmd.Env); len(s) > 0 {
		t.Fatalf("hazırlanan ortamda panel sırrı: %v", s)
	}
	// exec'in çocuğa gerçekten vereceği ortam (Environ: nil ise os.Environ).
	if s := kiracitest.Sizinti(cmd.Environ()); len(s) > 0 {
		t.Fatalf("çocuğa verilecek ortamda panel sırrı: %v", s)
	}
}

// Sahte runuser (PATH başı): Komut'un çocuğa verdiği ortam kaydedilir, kök gerekmez.
func TestKomut_SahteRunuserOrtami(t *testing.T) {
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	k := kiracitest.Kur(t, "runuser")
	cmd, err := Kiraci{Kul: "nobody", Ev: "/home/c_ornek"}.Komut(context.Background(), "id")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cs := k.SizintiYok(t, "runuser", "-u", "nobody", "--", "id")
	var home bool
	for _, satir := range cs[0].Env {
		home = home || satir == "HOME=/home/c_ornek"
	}
	if !home {
		t.Errorf("HOME kiracının evi değil: %q", cs[0].Env)
	}
}

// Tümleşik: GERÇEK runuser ile kiracı kimliğinde /usr/bin/env — çıktıda panel sırrı yok.
func TestKomut_GercekRunuser_EnvCiktisindaSirYok(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("runuser ile kimlik düşürme root gerektirir (181'de testler root koşar)")
	}
	if _, err := exec.LookPath("runuser"); err != nil {
		t.Skipf("runuser yok: %v", err)
	}
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("/usr/bin/env yok")
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	sahteSirlar(t)
	cmd, err := Kiraci{Kul: "nobody", Ev: "/"}.Komut(context.Background(), "/bin/sh", "-c", "id -u; /usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("runuser: %v", err)
	}
	satirlar := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(satirlar) == 0 || satirlar[0] != u.Uid {
		t.Fatalf("kiracı kimliğinde koşmadı: %q (beklenen uid %s)", satirlar, u.Uid)
	}
	if s := kiracitest.Sizinti(satirlar[1:]); len(s) > 0 {
		t.Fatalf("kiracı süreci panel sırlarını GÖRDÜ: %v", s)
	}
}
