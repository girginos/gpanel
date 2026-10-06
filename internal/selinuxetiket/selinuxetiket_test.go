package selinuxetiket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type sahte struct {
	cagri [][]string
	hata  error
}

func kur(t *testing.T, acik, arac bool) *sahte {
	t.Helper()
	s := &sahte{}
	eskiA, eskiV, eskiK, eskiE := selinuxAcik, aracVar, komut, evKoku
	t.Cleanup(func() { selinuxAcik, aracVar, komut, evKoku = eskiA, eskiV, eskiK, eskiE })
	evKoku = os.TempDir()
	selinuxAcik = func() bool { return acik }
	aracVar = func(string) bool { return arac }
	komut = func(_ context.Context, ad string, arg ...string) ([]byte, error) {
		s.cagri = append(s.cagri, append([]string{ad}, arg...))
		if s.hata != nil {
			return []byte("boom"), s.hata
		}
		return nil, nil
	}
	return s
}

func TestEtiketle_AltAgacaRestoreconRF(t *testing.T) {
	s := kur(t, true, true)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "public_html", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Etiketle(home, "public_html", "app"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"restorecon", "-RF", filepath.Join(home, "public_html", "app")}}
	if !reflect.DeepEqual(s.cagri, want) {
		t.Fatalf("çağrı %v, beklenen %v", s.cagri, want)
	}
}

func TestEtiketle_SELinuxKapaliyaSessizGec(t *testing.T) {
	s := kur(t, false, true)
	if err := Etiketle(t.TempDir()); err != nil || len(s.cagri) != 0 {
		t.Fatalf("SELinux kapalı: err=%v çağrı=%v", err, s.cagri)
	}
	s = kur(t, true, false)
	if err := Etiketle(t.TempDir()); err != nil || len(s.cagri) != 0 {
		t.Fatalf("restorecon yok: err=%v çağrı=%v", err, s.cagri)
	}
}

func TestEtiketle_SymlinkBilesenineDokunmaz(t *testing.T) {
	s := kur(t, true, true)
	home := t.TempDir()
	disari := t.TempDir()
	if err := os.Symlink(disari, filepath.Join(home, "public_html")); err != nil {
		t.Fatal(err)
	}
	if err := Etiketle(home, "public_html"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("symlink bileşeni reddedilmeli: %v", err)
	}
	// ara bileşen symlink
	if err := os.MkdirAll(filepath.Join(disari, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Etiketle(home, "public_html", "x"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("ara symlink reddedilmeli: %v", err)
	}
	if len(s.cagri) != 0 {
		t.Fatalf("symlink yolunda restorecon çalıştı: %v", s.cagri)
	}
}

func TestEtiketle_EvDisinaCikisReddedilir(t *testing.T) {
	s := kur(t, true, true)
	home := t.TempDir()
	if err := Etiketle(home, "..", "etc"); !errors.Is(err, ErrYol) {
		t.Fatalf(".. reddedilmeli: %v", err)
	}
	if err := Etiketle("/"); !errors.Is(err, ErrYol) {
		t.Fatalf("kök reddedilmeli: %v", err)
	}
	if err := Etiketle("/etc"); !errors.Is(err, ErrYol) {
		t.Fatalf("ev kökü dışı reddedilmeli: %v", err)
	}
	if err := Etiketle("goreli/yol"); !errors.Is(err, ErrYol) {
		t.Fatalf("göreli ev reddedilmeli: %v", err)
	}
	if len(s.cagri) != 0 {
		t.Fatalf("restorecon çalıştı: %v", s.cagri)
	}
}

func TestEtiketle_OlmayanYolHatasiz(t *testing.T) {
	s := kur(t, true, true)
	if err := Etiketle(t.TempDir(), "yok", "alt"); err != nil || len(s.cagri) != 0 {
		t.Fatalf("var olmayan yol: err=%v çağrı=%v", err, s.cagri)
	}
}

func TestEtiketle_RestoreconHatasiYutulmaz(t *testing.T) {
	s := kur(t, true, true)
	s.hata = errors.New("exit 1")
	err := Etiketle(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("hata çıktıyla birlikte dönmeli: %v", err)
	}
}

func TestMutlak(t *testing.T) {
	s := kur(t, true, true)
	home := t.TempDir()
	hedef := filepath.Join(home, "public_html", "x")
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Mutlak(home, hedef); err != nil {
		t.Fatal(err)
	}
	if len(s.cagri) != 1 || s.cagri[0][2] != hedef {
		t.Fatalf("çağrı %v", s.cagri)
	}
	if err := Mutlak(home, "/etc"); !errors.Is(err, ErrYol) {
		t.Fatalf("ev dışı reddedilmeli: %v", err)
	}
	if err := Mutlak(home, home+"-baska"); !errors.Is(err, ErrYol) {
		t.Fatalf("kardeş dizin reddedilmeli: %v", err)
	}
}

func TestYol_EvKokuAltindaEvCikarir(t *testing.T) {
	s := kur(t, true, true)
	kok := t.TempDir()
	evKoku = kok
	hedef := filepath.Join(kok, "c_x", "public_html", "app")
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Yol(hedef); err != nil {
		t.Fatal(err)
	}
	if len(s.cagri) != 1 || s.cagri[0][2] != hedef {
		t.Fatalf("çağrı %v", s.cagri)
	}
	for _, kotu := range []string{"/etc/passwd", kok, kok + "/", "goreli"} {
		if err := Yol(kotu); !errors.Is(err, ErrYol) {
			t.Fatalf("%q reddedilmeli: %v", kotu, err)
		}
	}
	// ev dizininin kendisi symlink → reddedilir
	disari := t.TempDir()
	if err := os.Symlink(disari, filepath.Join(kok, "c_y")); err != nil {
		t.Fatal(err)
	}
	if err := Yol(filepath.Join(kok, "c_y", "public_html")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("symlink ev reddedilmeli: %v", err)
	}
	if len(s.cagri) != 1 {
		t.Fatalf("fazladan çağrı: %v", s.cagri)
	}
}
