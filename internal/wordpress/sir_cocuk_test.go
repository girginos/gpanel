package wordpress

// Derinlemesine savunma kilidi: wp-cli kiracının wp-config.php'sini ve
// eklentilerini YÜKLER — kiracı PHP'si getenv() ile ne görürse panel sırrı
// odur. Çağrı yolları çocuğa panel ortamını geçirmemeli (sahte runuser ortamı
// kaydeder; kök gerekmez).

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

func TestWPKomut_PanelSirriKiraciyaGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	_, _ = wpKomut(dilimtest.SK, "core", "version")
	_, _ = wpStdout(context.Background(), dilimtest.SK, "plugin", "list", "--format=json")
	k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--")
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
}

// Dilim (scope) wp-cli çıktısını/çıkış kodunu bozmamalı (panel bunlardan sonuç üretir).
func TestWPKomut_DilimdeCiktiVeCikisKoduKorunur(t *testing.T) {
	k := dilimtest.KurSabitCikti(t)
	out, err := wpKomut(dilimtest.SK, "core", "version")
	dilimtest.SabitCiktiDogrula(t, string(out), err)
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
}
