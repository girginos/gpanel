package laravel

// Kiracı kimliğinde çalışan kısa çağrılar (mkdir, .env tee) panel sırlarını
// çocuğa geçirmemeli: /proc/<pid>/environ kiracı uid'sine açıktır.

import (
	"path/filepath"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestKiraciCagrilari_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	dir := filepath.Join(t.TempDir(), "app")
	_ = mkdirTenant("nobody", dir)
	_ = envYaz("nobody", dir, "APP_KEY=x\n")
	if n := len(k.SizintiYok(t, "runuser", "-u", "nobody", "--")); n != 2 {
		t.Fatalf("2 kiracı çağrısı bekleniyordu, %d", n)
	}
}
