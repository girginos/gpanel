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

// nobet — sözleşme testlerinin nöbetçi sırrı (Maskele desenlerinin hiçbirine
// uymaz: yalnız çağıranın verdiği sır listesiyle maskelenebilir).
const nobet = "nobet-7f3a9c-SIR"

// Başvuru uygulaması sözleşmeden geçer (ham hata nedenlerinde sır olsa bile).
func TestUyum_Bellek(t *testing.T) {
	Uyum(t, YeniBellekSirli(nobet), nobet)
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

func denemeKos(t *testing.T, h hedef.Hedef, ad string, sirlar ...string) []string {
	t.Helper()
	o := yeniOrtam(t, h, sirlar...)
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
		if h := denemeKos(t, YeniBellekSirli(nobet), d.ad, nobet); len(h) != 0 {
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

type onekListelemeyen struct{ *Bellek }

func (o onekListelemeyen) Yetenekler() hedef.Yetenek {
	return o.Bellek.Yetenekler() &^ hedef.YetenekOnekListele
}

// iptalTanimsiz — iptali kendi metniyle bildirir: errors.Is(Canceled) çalışmaz
// (orkestratör iptali ağ arızası sanıp yeniden dener).
type iptalTanimsiz struct{ *Bellek }

func (i iptalTanimsiz) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	err := i.Bellek.Yukle(ctx, yerel, ad, sec)
	if errors.Is(err, context.Canceled) {
		return hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("aktarım kesildi"))
	}
	return err
}

// onceIptalSessiz — bağlam zaten iptal edilmişse hiçbir şey yapmadan nil döner
// (nihai adda dosya da kalmaz: yalnız hata dönüşü denetimi yakalar).
type onceIptalSessiz struct{ *Bellek }

func (o onceIptalSessiz) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	if ctx.Err() != nil {
		return nil
	}
	return o.Bellek.Yukle(ctx, yerel, ad, sec)
}

// dizindeOzyinelemeli — ad öneki boşken ("dizin/") alt dizinlere de iner; ad
// önekli sorgu doğrudur (yalnız dizin listesinin kesin sayısı yakalar).
type dizindeOzyinelemeli struct{ *Bellek }

func (d dizindeOzyinelemeli) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	if !strings.HasSuffix(onek, "/") {
		return d.Bellek.Listele(ctx, onek)
	}
	var out []hedef.Nesne
	for _, ad := range d.Adlar() {
		if strings.HasPrefix(ad, onek) {
			v, _ := d.Icerik(ad)
			out = append(out, hedef.Nesne{Ad: ad, Boyut: int64(len(v))})
		}
	}
	return out, nil
}

// shaYanlis — sunucu sha256 yeteneğini ilan eder ama yanlış özet bildirir.
type shaYanlis struct{ *Bellek }

func (s shaYanlis) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	l, err := s.Bellek.Listele(ctx, onek)
	for i := range l {
		l[i].Sha256 = strings.Repeat("0", 64)
	}
	return l, err
}

// testHepBasarili — Test depoya hiç gitmeden nil döner.
type testHepBasarili struct{ *Bellek }

func (testHepBasarili) Test(context.Context) error { return nil }

// sarilmisHata — *hedef.Hata'yı fmt.Errorf ile sarar: dış metin (burada uzak ad
// ve uç nokta) Error()'a girer.
type sarilmisHata struct{ *Bellek }

func (s sarilmisHata) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	if err := s.Bellek.Yukle(ctx, yerel, ad, sec); err != nil {
		return fmt.Errorf("s3 put https://depo.ornek/%s: %w", ad, err)
	}
	return nil
}

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
		{"önek listeleyemeyen", onekListelemeyen{YeniBellek()}, "yetenekler"},
		// İlk turda neg_control'den KAÇAN dört denetimin bozuk karşılıkları.
		{"iptal Canceled olarak tanınmıyor", iptalTanimsiz{YeniBellek()}, "iptal_ortada"},
		{"önceden iptalde sessiz nil", onceIptalSessiz{YeniBellek()}, "iptal_onceden"},
		{"dizin listesi alt dizine iniyor", dizindeOzyinelemeli{YeniBellek()}, "onek_listeleme"},
		{"sunucu sha yanlış", shaYanlis{YeniBellek()}, "gidis_donus"},
		// Y0 incelemesinin istediği yeni denetimler.
		{"yarım geçici dosya temizlenmiyor (ortada iptal)", bellekVaryant(func(b *Bellek) { b.geciciBirak = true }), "iptal_ortada"},
		{"yarım geçici dosya temizlenmiyor (önceden iptal)", bellekVaryant(func(b *Bellek) { b.geciciBirak = true }), "iptal_onceden"},
		{"yarım geçici dosya temizlenmiyor (yanlış sha)", bellekVaryant(func(b *Bellek) { b.geciciBirak = true }), "yanlis_sha"},
		{"dizin başına ortak geçici ad", bellekVaryant(func(b *Bellek) { b.ortakGecici = true }), "eszamanli_yukleme"},
		{"Test depoya gitmiyor", testHepBasarili{YeniBellek()}, "test_erisim"},
		{"sarılmış hata (iptal)", sarilmisHata{YeniBellek()}, "iptal_ortada"},
		{"sarılmış hata (yanlış sha)", sarilmisHata{YeniBellek()}, "yanlis_sha"},
	}
	for _, d := range durumlar {
		if h := denemeKos(t, d.h, d.deneme); len(h) == 0 {
			t.Errorf("%s: %s denemesi bozukluğu YAKALAMADI", d.ad, d.deneme)
		} else {
			t.Logf("%s → yakalandı: %s", d.ad, h[0])
		}
	}
}

// Nöbetçi sır: sırrı maskelemeden saran uygulama yakalanır; aynı uygulama
// sır listesi verilmeden koşulursa denetim sessiz kalır (denetim sırra bağlı).
func TestUyum_SirSizintisiYakalanir(t *testing.T) {
	sizdiran := func() *Bellek {
		b := YeniBellekSirli(nobet)
		b.sirSizdir = true
		return b
	}
	for _, deneme := range []string{"gidis_donus", "iptal_onceden", "gecersiz_ad", "yanlis_sha"} {
		h := denemeKos(t, sizdiran(), deneme, nobet)
		if len(h) == 0 {
			t.Errorf("%s: maskelenmeyen sır YAKALANMADI", deneme)
			continue
		}
		for _, m := range h {
			if strings.Contains(m, nobet) {
				t.Errorf("%s: denetim mesajı sırrın kendisini yazdı: %s", deneme, m)
			}
		}
	}
	if h := denemeKos(t, sizdiran(), "gidis_donus"); len(h) != 0 {
		t.Errorf("sır verilmeden denetim hata kaydetti (beklenmez): %v", h)
	}
}

// Sağlam Bellek'te ham hata nedeni gerçekten sırrı taşıyor: denetimin anlamlı
// olduğunun kanıtı (Ayrinti maskeli, sızdıranda ham).
func TestBellek_SirliHataNedeni(t *testing.T) {
	ctx := context.Background()
	for _, d := range []struct {
		b          *Bellek
		sirGorunur bool
	}{{YeniBellekSirli(nobet), false}, {func() *Bellek { b := YeniBellekSirli(nobet); b.sirSizdir = true; return b }(), true}} {
		err := d.b.Indir(ctx, "yok/dosya.age", filepath.Join(t.TempDir(), "x"))
		var h *hedef.Hata
		if !errors.As(err, &h) || h.Kategori != hedef.KategoriBulunamadi {
			t.Fatalf("Indir hatası: %v", err)
		}
		if got := strings.Contains(h.Ayrinti(), nobet); got != d.sirGorunur {
			t.Errorf("Ayrinti'de sır görünür=%v, beklenen %v", got, d.sirGorunur)
		}
		if strings.Contains(err.Error(), nobet) {
			t.Errorf("Error() sır taşıdı")
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

func TestBellek_GeciciDosyaGorunurVeTasinir(t *testing.T) {
	b := YeniBellek()
	ctx, iptal := context.WithCancel(context.Background())
	defer iptal()
	yerel := filepath.Join(t.TempDir(), "k.bin")
	if err := os.WriteFile(yerel, make([]byte, 3<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	var arada []string
	err := b.Yukle(ctx, yerel, "d/yedek.age", hedef.YukleSecenek{Ilerleme: func(n int64) {
		if arada == nil {
			arada = b.Adlar()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arada) != 1 || !hedef.GeciciMi(arada[0]) || !strings.HasPrefix(arada[0], "d/") {
		t.Errorf("yükleme sırasında aynı dizinde tek geçici dosya beklenirdi: %v", arada)
	}
	if got := strings.Join(b.Adlar(), ","); got != "d/yedek.age" {
		t.Errorf("başarıdan sonra yalnız nihai ad kalmalı: %s", got)
	}
}
