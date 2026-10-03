package websec

// wp-cli taraması kiracının WordPress'ini yükler; kiracı kimliğinde, panel
// ortamı OLMADAN koşmalı. sudo'nun ortam politikası (env_reset/env_keep)
// sudoers'a bağlıdır → tek yol runuser + açık asgari ortam.

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestWPCliJSON_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	_, _ = wpCliJSON(context.Background(), "nobody", t.TempDir(), "core", "version")
	if cs := k.Cagrilar(t, "sudo"); len(cs) > 0 {
		t.Errorf("sudo kullanıldı (ortam politikası sudoers'a bağlı): %q", cs[0].Argv)
	}
	k.SizintiYok(t, "runuser", "-u", "nobody", "--")
}
