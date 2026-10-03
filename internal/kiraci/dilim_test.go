package kiraci

// Dilim: kiracının KENDİ kodunu koşan yollar (cron) süreç ağacını kiracı kaynak
// dilimine koyar. Birim: argv/ortam sözleşmesi (çalıştırmadan; sahte kullanıcı
// çözümü). Gerçek kapsam açmak sistemde birim yaratır → 181 canlı doğrulamada.

import (
	"context"
	"errors"
	"os/user"
	"reflect"
	"testing"
)

// sahteKiraciKullanici: c_ biçimli bir adı sistemde olmadan çözer (uid 4242).
func sahteKiraciKullanici(t *testing.T) {
	t.Helper()
	eski := kullaniciBul
	kullaniciBul = func(ad string) (*user.User, error) {
		switch ad {
		case "c_ornek_com":
			return &user.User{Username: ad, Uid: "4242", Gid: "4242"}, nil
		case "c_kok":
			return &user.User{Username: ad, Uid: "0", Gid: "0"}, nil
		}
		return eski(ad)
	}
	t.Cleanup(func() { kullaniciBul = eski })
}

func TestDilimAdi(t *testing.T) {
	if d, err := DilimAdi("c_ornek_com"); err != nil || d != "girginos-c_ornek_com.slice" {
		t.Fatalf("DilimAdi = %q, %v", d, err)
	}
	for _, sk := range []string{"", "root", "nobody", "c_x.slice", "c_x --uid=0", "-c_x", "c_X", "c_a/b", "c_x\n"} {
		if _, err := DilimAdi(sk); !errors.Is(err, ErrSK) {
			t.Errorf("DilimAdi(%q) reddedilmeliydi: %v", sk, err)
		}
	}
}

func TestKomut_Dilim_ArgvOrtam(t *testing.T) {
	sahteKiraciKullanici(t)
	t.Setenv("GOSP_TEST_PANEL_SIRRI", "gizli-deger-9c1e")
	k := Kiraci{Kul: "c_ornek_com", Ev: "/home/c_ornek_com", Dilim: true}
	cmd, err := k.Komut(context.Background(), "/bin/sh", "-c", "echo $HOME; x")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"systemd-run", "--scope", "--quiet", "--collect",
		"--slice=girginos-c_ornek_com.slice", "--description=girginospanel kiraci c_ornek_com",
		"--", "runuser", "-u", "c_ornek_com", "--", "/bin/sh", "-c", "echo $HOME; x"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("argv:\n got %q\nwant %q", cmd.Args, want)
	}
	// --uid YOK: kapsam kipinde systemd-run ek grupları düşürmez; kimliği runuser düşürür.
	for _, a := range cmd.Args {
		if len(a) >= 5 && (a[:5] == "--uid" || a[:5] == "--gid") {
			t.Fatalf("systemd-run kimlik düşürmemeli (ek gruplar): %q", cmd.Args)
		}
	}
	if want := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/home/c_ornek_com", "LANG=C"}; !reflect.DeepEqual(cmd.Env, want) {
		t.Fatalf("ortam:\n got %q\nwant %q", cmd.Env, want)
	}
	if cmd.Cancel == nil || cmd.WaitDelay <= 0 {
		t.Error("iptal sözleşmesi eksik")
	}
}

// Dilim yalnız panel biçimli kiracıda: biçim dışı ad (dilim adı üretilemez) ve
// uid 0'a çözülen ad reddedilir; dilimsiz runuser'a sessizce DÜŞÜLMEZ.
func TestKomut_Dilim_Reddeder(t *testing.T) {
	sahteKiraciKullanici(t)
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skipf("nobody yok: %v", err)
	}
	for _, kul := range []string{"nobody", "c_kok", "c_yok_boyle_kiraci_7f3e", ""} {
		cmd, err := Kiraci{Kul: kul, Ev: "/home/x", Dilim: true}.Komut(context.Background(), "id")
		if !errors.Is(err, ErrKomut) || cmd != nil {
			t.Errorf("Dilim Komut(Kul=%q) reddedilmeliydi: %v", kul, err)
		}
	}
	// Dilimsiz yol değişmedi.
	cmd, err := Kiraci{Kul: "nobody", Ev: "/home/x"}.Komut(context.Background(), "id")
	if err != nil || cmd.Args[0] != "runuser" {
		t.Fatalf("dilimsiz Komut runuser olmalı: %v %v", cmd, err)
	}
}
