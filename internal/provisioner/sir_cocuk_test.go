package provisioner

import (
	"os/user"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

// nginxOkuyabilir: nginx kimliğinde test -r — panel sırları çocuğa geçmemeli.
func TestNginxOkuyabilir_PanelSirriGecmez(t *testing.T) {
	if _, err := user.Lookup("nginx"); err != nil {
		t.Skipf("nginx kullanıcısı yok: %v", err)
	}
	k := kiracitest.Kur(t)
	_ = nginxOkuyabilir(t.TempDir())
	k.SizintiYok(t, "runuser", "-u", "nginx", "--")
}
