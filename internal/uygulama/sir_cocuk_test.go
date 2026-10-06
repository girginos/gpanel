package uygulama

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

// tenantKomut (mkdir/mv/rm kiracı olarak) panel sırlarını çocuğa geçirmemeli.
func TestTenantKomut_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	_ = tenantKomut(context.Background(), dilimtest.SK, "true")
	k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--")
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
}

// Dağıtım arşivi çıkarma (tar/unzip) kiracı olarak: panel sırları geçmemeli.
func TestKiraciCikar_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	dir := t.TempDir()
	for _, tur := range []string{"tarball_gz", "tarball_bz2", "zip"} {
		if err := kiraciCikar(context.Background(), dilimtest.SK, tur, dir+"/a", dir+"/h"); err != nil {
			t.Fatalf("%s: %v", tur, err)
		}
	}
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
	if n := len(k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--")); n != 3 {
		t.Fatalf("3 çıkarma çağrısı bekleniyordu, %d", n)
	}
}
