package redis

// wp-cli (kiracı PHP'si) ve kiracı cp/rm çağrıları panel sırlarını (özellikle
// PANEL_REDIS_ADMIN_PASS) çocuğa geçirmemeli.

import (
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestWPKomut_PanelSirriKiraciyaGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	_, _ = wpKomut("nobody", "config", "get", "WP_CACHE")
	k.SizintiYok(t, "runuser", "-u", "nobody", "--")
}
