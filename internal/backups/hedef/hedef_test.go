package hedef

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAdDogrula(t *testing.T) {
	gecerli := []string{
		"a", "yedek.tar.gz", "2026-08-25/c_ornek-20260825-233538.tar.gz.age",
		"Yedek Klasörü/müşteri ğüşıöç İŞ yedeği 2026.tar.gz", "a b/c d", "x/y/z/w.age",
		strings.Repeat("a", AzamiAdUzunlugu),
	}
	for _, ad := range gecerli {
		if err := AdDogrula(ad); err != nil {
			t.Errorf("geçerli ad reddedildi: %q", ad)
		}
	}
	gecersiz := []string{
		"", "/mutlak", "son/", "../kacis", "a/../b", "a/./b", "./a", "a//b", `a\b`, "..",
		"satir\nsonu", "sekme\tvar", "nul\x00", "del\x7f", "c1\u0085", " ", "a/ /b",
		strings.Repeat("a", AzamiAdUzunlugu+1), string([]byte{0xff, 0xfe}),
	}
	for _, ad := range gecersiz {
		if err := AdDogrula(ad); !errors.Is(err, ErrGecersizAd) {
			t.Errorf("geçersiz ad KABUL edildi: %q", ad)
		}
	}
}

func TestAdBolOnekBolKatla(t *testing.T) {
	if d, f := AdBol("a/b/c.age"); d != "a/b" || f != "c.age" {
		t.Errorf("AdBol: %q %q", d, f)
	}
	if d, f := AdBol("c.age"); d != "" || f != "c.age" {
		t.Errorf("AdBol kök: %q %q", d, f)
	}
	durumlar := []struct{ onek, dizin, ad string }{
		{"", "", ""}, {"2026-08-25/", "2026-08-25", ""}, {"2026-08-25/c_", "2026-08-25", "c_"}, {"c_", "", "c_"},
	}
	for _, d := range durumlar {
		dz, a, err := OnekBol(d.onek)
		if err != nil || dz != d.dizin || a != d.ad {
			t.Errorf("OnekBol(%q) = %q %q %v", d.onek, dz, a, err)
		}
	}
	for _, kotu := range []string{"/", "../", "a/../", "/a"} {
		if _, _, err := OnekBol(kotu); err == nil {
			t.Errorf("OnekBol(%q) kabul etti", kotu)
		}
	}
	if k := Katla("/yedek/", "a/b"); k != "/yedek/a/b" {
		t.Errorf("Katla: %q", k)
	}
	if k := Katla("", "a"); k != "a" {
		t.Errorf("Katla boş taban: %q", k)
	}
	if k := Katla("/", ""); k != "/" {
		t.Errorf("Katla kök: %q", k)
	}
}

func TestYetenek(t *testing.T) {
	y := YetenekAtomikYukle | YetenekIlerleme
	if !y.Var(YetenekAtomikYukle) || y.Var(YetenekOnekListele) || y.Var(0) {
		t.Errorf("Var yanlış: %v", y)
	}
	if !y.Var(YetenekAtomikYukle|YetenekIlerleme) || y.Var(YetenekAtomikYukle|YetenekBantSiniri) {
		t.Error("çoklu bayrak Var yanlış")
	}
	if s := y.String(); s != "atomik-yukle,ilerleme" {
		t.Errorf("String: %q", s)
	}
}

type bosHedef struct{ tur string }

func (b bosHedef) Tur() string                                             { return b.tur }
func (bosHedef) Yetenekler() Yetenek                                       { return 0 }
func (bosHedef) Test(context.Context) error                                { return nil }
func (bosHedef) Yukle(context.Context, string, string, YukleSecenek) error { return nil }
func (bosHedef) Listele(context.Context, string) ([]Nesne, error)          { return nil, nil }
func (bosHedef) Indir(context.Context, string, string) error               { return nil }
func (bosHedef) Sil(context.Context, string) error                         { return nil }

func TestDefter(t *testing.T) {
	d := &defter{}
	var gelenSir string
	var gelenKapi bool
	d.kaydet("s3", func(ayar json.RawMessage, sirlar map[string]string, sec KurSecenek) (Hedef, error) {
		gelenSir = sirlar["gizli"]
		gelenKapi = sec.KapiAl() != nil
		return bosHedef{tur: "s3"}, nil
	})
	d.kaydet("webdav", func(json.RawMessage, map[string]string, KurSecenek) (Hedef, error) { return bosHedef{"webdav"}, nil })
	h, err := d.kur("s3", json.RawMessage(`{}`), map[string]string{"gizli": "x"}, KurSecenek{})
	if err != nil || h.Tur() != "s3" || gelenSir != "x" || !gelenKapi {
		t.Fatalf("kur: %v %v %q %v", h, err, gelenSir, gelenKapi)
	}
	if _, err := d.kur("yok", nil, nil, KurSecenek{}); !errors.Is(err, ErrBilinmeyenTur) {
		t.Errorf("bilinmeyen tür: %v", err)
	}
	if got := strings.Join(d.turler(), ","); got != "s3,webdav" {
		t.Errorf("turler: %q", got)
	}
	panikBekle := func(ad string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: panic bekleniyordu", ad)
			}
		}()
		f()
	}
	k := func(json.RawMessage, map[string]string, KurSecenek) (Hedef, error) { return nil, nil }
	panikBekle("yinelenen", func() { d.kaydet("s3", k) })
	panikBekle("geçersiz ad", func() { d.kaydet("S3!", k) })
	panikBekle("nil kurucu", func() { d.kaydet("smb", nil) })
	panikBekle("çok uzun", func() { d.kaydet("abcdefghijklmnopq", k) })
}
