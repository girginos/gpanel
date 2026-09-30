package arsiv

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

const dumpAd = "cpmove-u/mysql/u_wp.sql"

func TestDumpAc(t *testing.T) {
	for ad, uret := range map[string]func(*testing.T) []byte{
		"gzip": func(t *testing.T) []byte { return gzUret(t, tarUret(t, iyiUyeler()...)) },
		"xz":   func(t *testing.T) []byte { return aracla(t, "xz", tarUret(t, iyiUyeler()...)) },
	} {
		t.Run(ad, func(t *testing.T) {
			a, err := acT(t, uret(t), Sinirlar{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := a.DumpAc(context.Background(), "./"+dumpAd, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || string(b) != "CREATE TABLE t (a int);\nINSERT INTO t VALUES (1);\n" {
				t.Fatalf("döküm = %q, %v", b, err)
			}
			// Erken kapatma takılmaz (açıcı öldürülür).
			rc, err = a.DumpAc(context.Background(), dumpAd, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rc.Read(make([]byte, 4)); err != nil {
				t.Fatal(err)
			}
			rc.Close()
		})
	}
}

func TestDumpAc_Reddedilenler(t *testing.T) {
	a, err := acT(t, gzUret(t, tarUret(t, iyiUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ad := range []string{"yok.sql", "cpmove-u/homedir/public_html/kisayol", "cpmove-u/homedir/public_html/sabit",
		"cpmove-u/homedir", "../" + dumpAd} {
		if _, err := a.DumpAc(context.Background(), ad, 1<<20); !errors.Is(err, ErrYok) {
			t.Errorf("%q: ErrYok beklendi, gelen %v", ad, err)
		}
	}
	if _, err := a.DumpAc(context.Background(), dumpAd, 10); !errors.Is(err, ErrSinir) {
		t.Fatalf("döküm sınırı: %v", err)
	}
}

// TestDumpAc_ArsivDegisti: envanterden sonra arşiv değişirse (üye düşürülmüş, boyut
// değişmiş) okuma envanterle eşleşmez → ErrDegisti.
func TestDumpAc_ArsivDegisti(t *testing.T) {
	yol := dosyala(t, gzUret(t, tarUret(t, iyiUyeler()...)))
	a, err := Ac(context.Background(), yol, Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	uyeler := iyiUyeler()
	uyeler[3].veri += "ek" // döküm öncesindeki üyenin boyutu değişti
	if err := os.WriteFile(yol, gzUret(t, tarUret(t, uyeler...)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DumpAc(context.Background(), dumpAd, 1<<20); !errors.Is(err, ErrDegisti) {
		t.Fatalf("ErrDegisti beklendi, gelen %v", err)
	}
	if err := os.WriteFile(yol, gzUret(t, tarUret(t, uyeler[:2]...)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DumpAc(context.Background(), dumpAd, 1<<20); !errors.Is(err, ErrDegisti) {
		t.Fatalf("kısalmış arşivde ErrDegisti beklendi, gelen %v", err)
	}
}
