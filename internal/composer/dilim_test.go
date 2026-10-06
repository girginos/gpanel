package composer

// Kaynak dilimi (scope) composer'ın çıktısını/çıkış kodunu BOZMAMALI: panel
// cikti + ok alanlarını bunlardan üretir.

import (
	"context"
	"testing"

	"girginospanel/internal/kiraci/dilimtest"
)

func TestKiraciComposer_DilimdeCiktiVeCikisKoduKorunur(t *testing.T) {
	k := dilimtest.KurSabitCikti(t)
	out, err := kiraciComposer(context.Background(), dilimtest.SK, composerBin, "install")
	dilimtest.SabitCiktiDogrula(t, string(out), err)
	if cs := dilimtest.DilimKilidi(t, k, dilimtest.SK); len(cs) != 1 {
		t.Fatalf("1 scope çağrısı bekleniyordu: %d", len(cs))
	}
}
