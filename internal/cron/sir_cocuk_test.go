package cron

// "Şimdi çalıştır": kiracının KENDİ crontab komutu kabukta koşar ve ortamı
// okuyabilir → panel sırları çocuğa geçmemeli.

import (
	"context"
	"errors"
	"testing"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/kiraci/kiracitest"
)

func TestKiraciCronCalistir_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	if _, err := kiraciCronCalistir(context.Background(), "nobody", "env > /tmp/x"); err != nil {
		t.Fatal(err)
	}
	if n := len(k.SizintiYok(t, "runuser", "-u", "nobody", "--", "/bin/sh", "-c", "env > /tmp/x")); n != 1 {
		t.Fatalf("1 çağrı bekleniyordu, %d", n)
	}
}

func TestKiraciCronCalistir_KokReddedilir(t *testing.T) {
	kiracitest.Kur(t)
	if _, err := kiraciCronCalistir(context.Background(), "root", "true"); !errors.Is(err, kiraci.ErrKomut) {
		t.Fatalf("root için ErrKomut bekleniyordu: %v", err)
	}
}
