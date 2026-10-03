package cron

// "Şimdi çalıştır" ve zamanlayıcı: kiracının KENDİ komutu kabukta koşar ve
// ortamı okuyabilir → panel sırları çocuğa geçmemeli; komut kiracı kimliğinde
// ve KAYNAK DİLİMİNDE koşmalı.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/kiraci/kiracitest"
)

// sahteKiraci: görevleri sahte runuser'a (kiracitest) yönlendirir — Dilim'siz,
// çünkü sahte c_ kullanıcısı sistemde yok. Üretim değeri ayrıca kilitli
// (TestKiraciIcin_KaynakDilimiZorunlu).
func sahteKiraci(t *testing.T, kul string) {
	t.Helper()
	eski := kiraciIcin
	kiraciIcin = func(string) kiraci.Kiraci { return kiraci.Kiraci{Kul: kul, Ev: t.TempDir()} }
	t.Cleanup(func() { kiraciIcin = eski })
}

func TestKiraciIcin_KaynakDilimiZorunlu(t *testing.T) {
	k := kiraciIcin("c_ornek_com")
	if want := (kiraci.Kiraci{Kul: "c_ornek_com", Ev: filepath.Join(kiraci.EvKoku, "c_ornek_com"), Dilim: true}); k != want {
		t.Fatalf("kiraciIcin = %+v, beklenen %+v (cron kiracı diliminde koşmalı)", k, want)
	}
}

func TestKiraciCronCalistir_PanelSirriGecmez(t *testing.T) {
	k := kiracitest.Kur(t)
	sahteKiraci(t, "nobody")
	if _, err := kiraciCronCalistir(context.Background(), "c_x", Gorev{Tip: "komut", Komut: "env > /tmp/x"}); err != nil {
		t.Fatal(err)
	}
	// Komut kabuk dizgesine EKLENMEZ: ayrı argüman ($1); kabuk önce $HOME'a geçer.
	if n := len(k.SizintiYok(t, "runuser", "-u", "nobody", "--", "/bin/sh", "-c", evdeCalistir, "gosp-cron", "env > /tmp/x")); n != 1 {
		t.Fatalf("1 çağrı bekleniyordu, %d", n)
	}
}

func TestKiraciCronCalistir_KokReddedilir(t *testing.T) {
	kiracitest.Kur(t)
	sahteKiraci(t, "root")
	if _, err := kiraciCronCalistir(context.Background(), "c_x", Gorev{Komut: "true"}); !errors.Is(err, kiraci.ErrKomut) {
		t.Fatalf("root için ErrKomut bekleniyordu: %v", err)
	}
}
