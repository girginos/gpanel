package websec

// wp-cli taraması kiracının WordPress'ini yükler; kiracı kimliğinde, panel
// ortamı OLMADAN koşmalı. sudo'nun ortam politikası (env_reset/env_keep)
// sudoers'a bağlıdır → tek yol runuser + açık asgari ortam.

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

func TestWPCliJSON_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	_, _ = wpCliJSON(context.Background(), dilimtest.SK, t.TempDir(), "core", "version")
	if cs := k.Cagrilar(t, "sudo"); len(cs) > 0 {
		t.Errorf("sudo kullanıldı (ortam politikası sudoers'a bağlı): %q", cs[0].Argv)
	}
	k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--")
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
}
