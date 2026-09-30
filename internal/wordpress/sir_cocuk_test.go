package wordpress

// Derinlemesine savunma kilidi: wp-cli kiracının wp-config.php'sini ve
// eklentilerini YÜKLER — kiracı PHP'si getenv() ile ne görürse panel sırrı
// odur. Çağrı yolları çocuğa panel ortamını geçirmemeli (sahte runuser ortamı
// kaydeder; kök gerekmez).

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestWPKomut_PanelSirriKiraciyaGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	_, _ = wpKomut("nobody", "core", "version")
	_, _ = wpStdout(context.Background(), "nobody", "plugin", "list", "--format=json")
	k.SizintiYok(t, "runuser", "-u", "nobody", "--")
}
