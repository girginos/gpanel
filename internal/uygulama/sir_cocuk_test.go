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
