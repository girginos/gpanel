package git

// kiraciCalistirGercek: sudo birincil yolu da runuser yedeği de panel
// ortamını kiracı komutuna geçirmemeli. sudo'nun env_reset'i sudoers'a
// bağlıdır (env_keep / !env_reset) → ortam AÇIKÇA verilir.

import (
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestKiraciCalistirGercek_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t, "sudo", "runuser")
	if _, e, err := kiraciCalistirGercek(testSK, t.TempDir(), []byte("x"), "true"); err != nil {
		t.Fatalf("beklenmeyen hata: %v (%s)", err, e)
	}
	k.SizintiYok(t, "sudo", "-u", testSK)
}
