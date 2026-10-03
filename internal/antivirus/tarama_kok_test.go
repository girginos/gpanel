package antivirus

import (
	"os"
	"path/filepath"
	"testing"
)

// Tara ucu kökü: kiracının public_html'i ev dışına (başka kiracı / sistem) symlink
// ise taranmamalı; ev içi symlink ve normal dizin kabul.
func TestTaramaKokuEvIcinde(t *testing.T) {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(d, "c_a")
	ab := filepath.Join(d, "c_ab")
	for _, p := range []string{filepath.Join(a, "www"), filepath.Join(ab, "public_html"), filepath.Join(d, "c_n", "public_html")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bag := func(hedef string) {
		t.Helper()
		_ = os.Remove(filepath.Join(a, "public_html"))
		if err := os.Symlink(hedef, filepath.Join(a, "public_html")); err != nil {
			t.Fatal(err)
		}
	}
	if !taramaKokuEvIcinde(filepath.Join(d, "c_n"), filepath.Join(d, "c_n", "public_html")) {
		t.Fatal("normal public_html reddedildi")
	}
	bag(filepath.Join(a, "www"))
	if !taramaKokuEvIcinde(a, filepath.Join(a, "public_html")) {
		t.Fatal("ev içi symlink reddedildi")
	}
	for _, h := range []string{filepath.Join(ab, "public_html"), "/etc", a, "../c_ab/public_html"} {
		bag(h)
		if taramaKokuEvIcinde(a, filepath.Join(a, "public_html")) {
			t.Fatalf("public_html -> %s kabul edildi", h)
		}
	}
	bag(filepath.Join(d, "yok"))
	if taramaKokuEvIcinde(a, filepath.Join(a, "public_html")) {
		t.Fatal("asılı symlink kabul edildi")
	}
}
