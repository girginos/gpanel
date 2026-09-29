package hedeftest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/backups/hedef"
)

// Başvuru uygulaması sözleşmeden geçer.
func TestUyum_Bellek(t *testing.T) {
	Uyum(t, YeniBellek())
}

// ─── Sözleşmenin kendisinin testi: bozuk uygulamalar YAKALANMALI ─────────────

type fatalIsaret struct{}

// kayitci — Errorf/Fatalf'ı kaydeden sinayici; Fatalf yürütmeyi keser.
type kayitci struct {
	gercek  *testing.T
	hatalar []string
}

func (k *kayitci) Helper()                   {}
func (k *kayitci) Logf(string, ...any)       {}
func (k *kayitci) TempDir() string           { return k.gercek.TempDir() }
func (k *kayitci) Errorf(f string, a ...any) { k.hatalar = append(k.hatalar, fmt.Sprintf(f, a...)) }
func (k *kayitci) Fatalf(f string, a ...any) {
	k.hatalar = append(k.hatalar, fmt.Sprintf(f, a...))
	panic(fatalIsaret{})
}

func denemeKos(t *testing.T, h hedef.Hedef, ad string) []string {
	t.Helper()
	o := yeniOrtam(t, h)
	var d *deneme
	ds := denemeler()
	for i := range ds {
		if ds[i].ad == ad {
			d = &ds[i]
		}
	}
	if d == nil {
		t.Fatalf("deneme yok: %s", ad)
	}
	k := &kayitci{gercek: t}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(fatalIsaret); !ok {
					panic(r)
				}
			}
		}()
		d.f(k, o)
	}()
	return k.hatalar
}

// Sağlam uygulamada hiçbir deneme hata kaydetmez (kaydedicinin kendisi doğru).
func TestUyum_KayitciSaglamdaSessiz(t *testing.T) {
	for _, d := range denemeler() {
		if d.ad == "buyuk_64mb" {
			continue // TestUyum_Bellek'te koşuyor
		}
		if h := denemeKos(t, YeniBellek(), d.ad); len(h) != 0 {
			t.Errorf("%s sağlam bellekte hata kaydetti: %v", d.ad, h)
		}
	}
}

type sessizBasari struct{ *Bellek }

// Yukle hiçbir şey yüklemeden nil döner (ör. "aktif değil → sessizce atla").
func (sessizBasari) Yukle(context.Context, string, string, hedef.YukleSecenek) error { return nil }

type silYoksaHata struct{ *Bellek }

func (s silYoksaHata) Sil(ctx context.Context, ad string) error {
	if _, ok := s.Icerik(ad); !ok {
		return hedef.YeniHata(hedef.KategoriBulunamadi, "sil", os.ErrNotExist)
	}
	return s.Bellek.Sil(ctx, ad)
}

type bozukIndirme struct{ *Bellek }

func (b bozukIndirme) Indir(ctx context.Context, ad, yerel string) error {
	if err := b.Bellek.Indir(ctx, ad, yerel); err != nil {
		return err
	}
	v, _ := os.ReadFile(yerel)
	if len(v) > 0 {
		v[len(v)/2] ^= 1
	}
	return os.WriteFile(yerel, v, 0o600)
}

type yanlisBoyut struct{ *Bellek }

func (y yanlisBoyut) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	l, err := y.Bellek.Listele(ctx, onek)
	for i := range l {
		l[i].Boyut--
	}
	return l, err
}

type adBozan struct{ *Bellek }

// Yukle ASCII dışı karakterleri atarak yükler (kodlama hatası benzetimi).
func (a adBozan) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	ascii := strings.Map(func(r rune) rune {
		if r > 127 {
			return -1
		}
		return r
	}, ad)
	return a.Bellek.Yukle(ctx, yerel, ascii, sec)
}

type kategorisiz struct{ *Bellek }

func (k kategorisiz) Indir(ctx context.Context, ad, yerel string) error {
	if _, ok := k.Icerik(ad); !ok {
		return errors.New("dosya yok")
	}
	return k.Bellek.Indir(ctx, ad, yerel)
}

type yarimIndirme struct{ *Bellek }

// Indir, dosya yokken bile yerel yolda boş dosya bırakır.
func (y yarimIndirme) Indir(ctx context.Context, ad, yerel string) error {
	err := y.Bellek.Indir(ctx, ad, yerel)
	if err != nil {
		_ = os.WriteFile(yerel, nil, 0o600)
	}
	return err
}

type yeteneksiz struct{ *Bellek }

func (yeteneksiz) Yetenekler() hedef.Yetenek { return hedef.YetenekOnekListele }

func bellekVaryant(f func(b *Bellek)) *Bellek {
	b := YeniBellek()
	f(b)
	return b
}

func TestUyum_BozukUygulamalarYakalanir(t *testing.T) {
	durumlar := []struct {
		ad     string
		h      hedef.Hedef
		deneme string
	}{
		{"atomik olmayan yükleme (ortada iptal)", bellekVaryant(func(b *Bellek) { b.atomikDegil = true }), "iptal_ortada"},
		{"atomik olmayan yükleme (önceden iptal)", bellekVaryant(func(b *Bellek) { b.atomikDegil = true }), "iptal_onceden"},
		{"atomik olmayan yükleme (yanlış sha)", bellekVaryant(func(b *Bellek) { b.atomikDegil = true }), "yanlis_sha"},
		{"ilerleme bildirmeyen", bellekVaryant(func(b *Bellek) { b.ilerlemesiz = true }), "iptal_ortada"},
		{"ad denetimsiz", bellekVaryant(func(b *Bellek) { b.adDenetimsiz = true }), "gecersiz_ad"},
		{"sessiz başarı", sessizBasari{YeniBellek()}, "gidis_donus"},
		{"sil yoksa hata", silYoksaHata{YeniBellek()}, "gidis_donus"},
		{"sil yoksa hata (hiç olmayan)", silYoksaHata{YeniBellek()}, "olmayan_sil"},
		{"bozuk indirme", bozukIndirme{YeniBellek()}, "gidis_donus"},
		{"yanlış boyut", yanlisBoyut{YeniBellek()}, "gidis_donus"},
		{"Türkçe ad bozan", adBozan{YeniBellek()}, "turkce_bosluklu_ad"},
		{"kategorisiz hata", kategorisiz{YeniBellek()}, "gidis_donus"},
		{"yarım indirme dosyası", yarimIndirme{YeniBellek()}, "gidis_donus"},
		{"zorunlu yetenek yok", yeteneksiz{YeniBellek()}, "yetenekler"},
	}
	for _, d := range durumlar {
		if h := denemeKos(t, d.h, d.deneme); len(h) == 0 {
			t.Errorf("%s: %s denemesi bozukluğu YAKALAMADI", d.ad, d.deneme)
		} else {
			t.Logf("%s → yakalandı: %s", d.ad, h[0])
		}
	}
}

// ─── Bellek'in test çifti olarak ek davranışları ─────────────────────────────

func TestBellek_HataEnjeksiyonuVeBozma(t *testing.T) {
	b := YeniBellek()
	ctx := context.Background()
	yerel := filepath.Join(t.TempDir(), "k.bin")
	if err := os.WriteFile(yerel, []byte("icerik"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.HataVer("yukle", errors.New("Login failed: 530 parola=Gizli123"))
	err := b.Yukle(ctx, yerel, "a/b.age", hedef.YukleSecenek{})
	if hedef.KategoriAl(err) != hedef.KategoriKimlik || strings.Contains(err.Error(), "Gizli123") {
		t.Fatalf("enjekte hata: %v", err)
	}
	var h *hedef.Hata
	if !errors.As(err, &h) || strings.Contains(h.Ayrinti(), "Gizli123") {
		t.Fatalf("ayrıntı maskelenmeli: %v", h)
	}
	b.HataVer("yukle", nil)
	if err := b.Yukle(ctx, yerel, "a/b.age", hedef.YukleSecenek{}); err != nil {
		t.Fatal(err)
	}
	if b.Cagri("yukle") != 2 || b.SurenYukleme() != 0 {
		t.Errorf("çağrı=%d süren=%d", b.Cagri("yukle"), b.SurenYukleme())
	}
	if !b.Boz("a/b.age") {
		t.Fatal("Boz")
	}
	v, _ := b.Icerik("a/b.age")
	if string(v) == "icerik" {
		t.Error("Boz içeriği değiştirmedi")
	}
	b.Koy("c.age", []byte("x"))
	if got := strings.Join(b.Adlar(), ","); got != "a/b.age,c.age" {
		t.Errorf("Adlar: %s", got)
	}
}
