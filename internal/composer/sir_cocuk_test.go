package composer

// composer kiracının composer.json'unu işler (kiracı kodu): panel sırları
// çocuğa geçmemeli, yalnız asgari ortam + composer ayarları.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/kiraci/dilimtest"
)

func TestKiraciComposer_PanelSirriGecmez(t *testing.T) {
	k := dilimtest.Kur(t)
	if _, err := kiraciComposer(context.Background(), dilimtest.SK, composerBin, "install", "--no-interaction"); err != nil {
		t.Fatal(err)
	}
	cs := k.SizintiYok(t, "runuser", "-u", dilimtest.SK, "--", composerBin, "install")
	if len(cs) != 1 {
		t.Fatalf("1 çağrı bekleniyordu, %d", len(cs))
	}
	dilimtest.DilimKilidi(t, k, dilimtest.SK)
	for _, v := range []string{"COMPOSER_HOME=/home/" + dilimtest.SK + "/.composer", "COMPOSER_ALLOW_SUPERUSER=0"} {
		if !slices.Contains(cs[0].Env, v) {
			t.Errorf("composer ortamında %q yok: %q", v, cs[0].Env)
		}
	}
}

func TestKiraciComposer_KokReddedilir(t *testing.T) {
	dilimtest.Kur(t)
	if _, err := kiraciComposer(context.Background(), "root", composerBin, "install"); !errors.Is(err, kiraci.ErrKomut) {
		t.Fatalf("root için ErrKomut bekleniyordu: %v", err)
	}
}
