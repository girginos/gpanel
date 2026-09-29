package kiraci

// Birim: argv/ortam/çalışma dizini sözleşmesi ve yol denetimi (çalıştırmadan).
// Tümleşik: root + ayrıcalıksız "nobody" varsa GERÇEK runuser ile kimlik
// düşürme ve ev köküne kilit t.TempDir içinde sınanır; canlı ev dizinlerine
// dokunulmaz, root değilse atlanır.

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestGecerliSK(t *testing.T) {
	for sk, bek := range map[string]bool{
		"c_ornek":                      true,
		"c_ornek_com2":                 true,
		"c_" + strings.Repeat("a", 30): true,
		"c_" + strings.Repeat("a", 31): false,
		"c_":                           false,
		"root":                         false,
		"c_Ornek":                      false,
		"c_../x":                       false,
		"-c_x":                         false,
		"c_x\n":                        false,
		"":                             false,
	} {
		if GecerliSK(sk) != bek {
			t.Errorf("GecerliSK(%q) = %v, beklenen %v", sk, !bek, bek)
		}
	}
}

func TestGoreli(t *testing.T) {
	k := Kiraci{Kul: "c_ornek", Ev: "/home/c_ornek"}
	for yol, bek := range map[string]string{
		"/home/c_ornek/public_html":            "public_html",
		"/home/c_ornek/public_html/public/":    "public_html/public",
		"/home/c_ornek/a/../public_html":       "public_html",
		"/home/c_ornek":                        "",
		"/home/c_ornek/":                       "",
		"/home/c_ornek/..":                     "",
		"/home/c_ornek/../c_komsu/public_html": "",
		"/home/c_ornek2/public_html":           "",
		"/etc/passwd":                          "",
		"public_html":                          "",
		"":                                     "",
	} {
		rel, err := k.Goreli(yol)
		if bek == "" {
			if err == nil {
				t.Errorf("Goreli(%q) = %q, ev dışı/ev'in kendisi REDDEDİLMELİ", yol, rel)
			}
			continue
		}
		if err != nil || rel != bek {
			t.Errorf("Goreli(%q) = %q, %v; beklenen %q", yol, rel, err, bek)
		}
	}
}

// Komut: kabuk yok, kiracı kimliği, panel ortamı (sırlar) çocuğa geçmez.
func TestKomut_ArgvOrtamDizin(t *testing.T) {
	t.Setenv("GOSP_TEST_PANEL_SIRRI", "gizli-deger-9c1e")
	k := Kiraci{Kul: "c_ornek", Ev: "/home/c_ornek"}
	cmd := k.Komut(context.Background(), "rsync", "-a", "kaynak/", "hedef/")

	if want := []string{"runuser", "-u", "c_ornek", "--", "rsync", "-a", "kaynak/", "hedef/"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("argv:\n got %q\nwant %q", cmd.Args, want)
	}
	if cmd.Dir != "/" {
		t.Errorf("çalışma dizini %q, beklenen /", cmd.Dir)
	}
	for _, e := range cmd.Env {
		if strings.Contains(e, "GOSP_TEST_PANEL_SIRRI") || strings.Contains(e, "gizli-deger-9c1e") {
			t.Fatalf("panel ortamı kiracı sürecine sızdı: %q", e)
		}
	}
	for _, gerekli := range []string{"HOME=/home/c_ornek", "LANG=C"} {
		if !slices.Contains(cmd.Env, gerekli) {
			t.Errorf("ortamda %q yok: %q", gerekli, cmd.Env)
		}
	}
	if cmd.Cancel == nil || cmd.WaitDelay <= 0 {
		t.Errorf("iptal sözleşmesi eksik (Cancel=%v, WaitDelay=%v)", cmd.Cancel != nil, cmd.WaitDelay)
	}
}

// nobodyOrtam: gerçek runuser için root + nobody gerekir; geçici ev nobody'nin
// inebileceği biçimde (yalnız geçme izni) açılır.
func nobodyOrtam(t *testing.T) (base string, k Kiraci) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("runuser ile kiracı kimliği için root gerekli")
	}
	for _, b := range []string{"runuser", "id", "touch"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s yok: %v", b, err)
		}
	}
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	base = t.TempDir()
	for _, d := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(d, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	ev := filepath.Join(base, "c_test")
	if err := os.Mkdir(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, Kiraci{Kul: "nobody", Ev: ev}
}

func TestKomut_KimlikDusurulur(t *testing.T) {
	base, k := nobodyOrtam(t)
	out, err := k.Komut(context.Background(), "id", "-u").Output()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	u, _ := user.Lookup("nobody")
	if got := strings.TrimSpace(string(out)); got != u.Uid {
		t.Fatalf("komut kiracı kimliğinde koşmadı: uid=%s, beklenen %s", got, u.Uid)
	}
	// Kök sahipli dizine kiracı YAZAMAZ (kök yetkisi çocuğa geçmez).
	kok := filepath.Join(base, "kok")
	if err := os.Mkdir(kok, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := k.Komut(context.Background(), "touch", filepath.Join(kok, "x")).Run(); err == nil {
		t.Fatal("kiracı kimliği kök sahipli dizine yazabildi")
	}
	if _, err := os.Lstat(filepath.Join(kok, "x")); err == nil {
		t.Fatal("kök sahipli dizinde dosya oluştu")
	}
}

// EvAc: ev içindeki bağ izlenir, ev dışına çıkan bağ (mutlak ya da ..) reddedilir.
func TestEvAc_DisariCikanBagReddedilir(t *testing.T) {
	base := t.TempDir()
	ev := filepath.Join(base, "c_test")
	dis := filepath.Join(base, "dis")
	for _, d := range []string{filepath.Join(ev, "ic"), dis} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(ev, "ic", "a.txt"), filepath.Join(dis, "gizli.txt")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for bag, hedef := range map[string]string{"mutlak": dis, "goreli": "../dis", "icerde": "ic"} {
		if err := os.Symlink(hedef, filepath.Join(ev, bag)); err != nil {
			t.Fatal(err)
		}
	}
	kok, err := Kiraci{Kul: "nobody", Ev: ev}.EvAc()
	if err != nil {
		t.Fatal(err)
	}
	defer kok.Close()
	for _, p := range []string{"mutlak/gizli.txt", "goreli/gizli.txt", "../dis/gizli.txt"} {
		if f, err := kok.Open(p); err == nil {
			f.Close()
			t.Errorf("%s: ev dışına çıkan yol açıldı", p)
		}
	}
	if f, err := kok.Open("icerde/a.txt"); err != nil {
		t.Errorf("ev içindeki bağ izlenmeliydi: %v", err)
	} else {
		f.Close()
	}
}
