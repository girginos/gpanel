package uygulama

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

// tenantKomut (mkdir/mv/rm kiracı olarak) panel sırlarını çocuğa geçirmemeli.
func TestTenantKomut_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	_ = tenantKomut(context.Background(), "nobody", "true")
	k.SizintiYok(t, "runuser", "-u", "nobody", "--")
}

// Dağıtım arşivi çıkarma (tar/unzip) kiracı olarak: panel sırları geçmemeli.
func TestKiraciCikar_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	dir := t.TempDir()
	for _, tur := range []string{"tarball_gz", "tarball_bz2", "zip"} {
		if err := kiraciCikar(context.Background(), "nobody", tur, dir+"/a", dir+"/h"); err != nil {
			t.Fatalf("%s: %v", tur, err)
		}
	}
	if n := len(k.SizintiYok(t, "runuser", "-u", "nobody", "--")); n != 3 {
		t.Fatalf("3 çıkarma çağrısı bekleniyordu, %d", n)
	}
}
