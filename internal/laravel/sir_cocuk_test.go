package laravel

// Kiracı kimliğinde çalışan kısa çağrılar (mkdir, .env tee) panel sırlarını
// çocuğa geçirmemeli: /proc/<pid>/environ kiracı uid'sine açıktır.

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"girginospanel/internal/kiraci"
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

// TenantExec/tenantExecEnv çekirdeği: artisan/composer/npm kiracının KODUNU
// yükler → panel sırları geçmemeli; tenantEnv (COMPOSER_HOME vb.) eklenmeli.
func TestTenantCalistir_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	dir := t.TempDir()
	if _, err := tenantCalistir(context.Background(), "nobody", dir, tenantEnv("nobody"), "/usr/bin/php", "artisan", "list"); err != nil {
		t.Fatal(err)
	}
	cs := k.SizintiYok(t, "runuser", "-u", "nobody", "--", "/usr/bin/php", "artisan", "list")
	if len(cs) != 1 {
		t.Fatalf("1 çağrı bekleniyordu, %d", len(cs))
	}
	if !slices.Contains(cs[0].Env, "COMPOSER_HOME=/home/nobody/.composer") {
		t.Errorf("tenantEnv eklenmedi: %q", cs[0].Env)
	}
	if _, err := tenantCalistir(context.Background(), "root", dir, nil, "/bin/true"); !errors.Is(err, kiraci.ErrKomut) {
		t.Errorf("root için ErrKomut bekleniyordu: %v", err)
	}
}
