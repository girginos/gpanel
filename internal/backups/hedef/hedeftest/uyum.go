// Package hedeftest — hedef.Hedef sözleşmesinin çalıştırılabilir tanımı (Uyum)
// ve bellek içi sahte hedef (Bellek). Yalnız testlerden içe aktarılır.
//
// Her yeni hedef türü kendi testinde (httptest ya da geçici yerel sunucuya
// karşı) Uyum'u çağırır:
//
//	func TestS3Uyum(t *testing.T) { hedeftest.Uyum(t, s3Hedefi(t)) }
//
// Uyum, hedefin tabanında rastgele bir "gosp-uyum-<hex>/" alt dizininde çalışır
// ve sonunda oluşturduğu her dosyayı siler.
package hedeftest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
)

// sinayici — *testing.T'nin Uyum'un kullandığı alt kümesi (sözleşmenin kendi
// testleri bozuk uygulamaların YAKALANDIĞINI kaydedici bir sinayici ile ölçer).
type sinayici interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	TempDir() string
}

type ortam struct {
	h    hedef.Hedef
	onek string
	ctx  context.Context
	kisa bool

	mu    sync.Mutex
	adlar map[string]bool
}

func yeniOrtam(t *testing.T, h hedef.Hedef) *ortam {
	t.Helper()
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	ctx, iptal := context.WithTimeout(context.Background(), 20*time.Minute)
	o := &ortam{h: h, onek: "gosp-uyum-" + hex.EncodeToString(rnd[:]) + "/", ctx: ctx,
		kisa: testing.Short(), adlar: map[string]bool{}}
	t.Cleanup(func() {
		defer iptal()
		o.mu.Lock()
		defer o.mu.Unlock()
		for ad := range o.adlar {
			sctx, sIptal := context.WithTimeout(context.Background(), time.Minute)
			if err := h.Sil(sctx, ad); err != nil {
				t.Logf("temizlik: %s silinemedi: %v", ad, err)
			}
			sIptal()
		}
	})
	return o
}

// ad — uyum alt dizininde bir ad üretir ve temizlik listesine ekler.
func (o *ortam) ad(parca string) string {
	ad := o.onek + parca
	o.mu.Lock()
	o.adlar[ad] = true
	o.mu.Unlock()
	return ad
}

type deneme struct {
	ad string
	f  func(t sinayici, o *ortam)
}

// Uyum — h'nin hedef.Hedef sözleşmesine uyduğunu doğrular.
func Uyum(t *testing.T, h hedef.Hedef) {
	t.Helper()
	o := yeniOrtam(t, h)
	for _, d := range denemeler() {
		t.Run(d.ad, func(t *testing.T) { d.f(t, o) })
	}
}

func denemeler() []deneme {
	return []deneme{
		{"yetenekler", denemeYetenekler},
		{"test_erisim", denemeTest},
		{"gidis_donus", denemeGidisDonus},
		{"turkce_bosluklu_ad", denemeTurkceAd},
		{"ustune_yazma", denemeUstuneYazma},
		{"onek_listeleme", denemeOnekListeleme},
		{"buyuk_64mb", denemeBuyuk},
		{"iptal_ortada", denemeIptalOrtada},
		{"iptal_onceden", denemeIptalOnceden},
		{"yanlis_sha", denemeYanlisSha},
		{"gecersiz_ad", denemeGecersizAd},
		{"olmayan_sil", denemeOlmayanSil},
	}
}

// ─── yardımcılar ────────────────────────────────────────────────────────────

// dosyaUret — tohumdan belirlenimli sözde rastgele içerikli dosya; yol ve sha256.
func dosyaUret(t sinayici, boyut int64, tohum uint64) (string, string) {
	t.Helper()
	yol := filepath.Join(t.TempDir(), "kaynak.bin")
	f, err := os.Create(yol)
	if err != nil {
		t.Fatalf("yerel dosya: %v", err)
	}
	defer f.Close()
	var tohum32 [32]byte
	for i := range tohum32 {
		tohum32[i] = byte(tohum >> (8 * (i % 8)))
	}
	r := mrand.NewChaCha8(tohum32)
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(f, h), r, boyut); err != nil {
		t.Fatalf("yerel dosya yazılamadı: %v", err)
	}
	return yol, hex.EncodeToString(h.Sum(nil))
}

func dosyaSha(t sinayici, yol string) string {
	t.Helper()
	f, err := os.Open(yol)
	if err != nil {
		t.Fatalf("indirilen dosya açılamadı: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("indirilen dosya okunamadı: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func bul(t sinayici, o *ortam, ad string) (hedef.Nesne, bool) {
	t.Helper()
	liste, err := o.h.Listele(o.ctx, ad)
	if err != nil {
		t.Fatalf("Listele(%q): %v", ad, err)
	}
	for _, n := range liste {
		if n.Ad == ad {
			return n, true
		}
	}
	return hedef.Nesne{}, false
}

func yukle(t sinayici, o *ortam, yerel, ad, sha string, boyut int64) {
	t.Helper()
	if err := o.h.Yukle(o.ctx, yerel, ad, hedef.YukleSecenek{Boyut: boyut, Sha256: sha}); err != nil {
		t.Fatalf("Yukle(%q): %v", ad, err)
	}
}

// gidisDonus — yükle → listele (boyut, varsa sunucu sha) → indir → sha eşit.
func gidisDonus(t sinayici, o *ortam, ad string, boyut int64, tohum uint64) {
	t.Helper()
	yerel, sha := dosyaUret(t, boyut, tohum)
	yukle(t, o, yerel, ad, sha, boyut)
	n, ok := bul(t, o, ad)
	if !ok {
		t.Fatalf("yüklenen %q Listele'de yok", ad)
	}
	if n.Boyut != boyut {
		t.Errorf("Listele boyutu %d, beklenen %d", n.Boyut, boyut)
	}
	if o.h.Yetenekler().Var(hedef.YetenekSunucuSha256) && !strings.EqualFold(n.Sha256, sha) {
		t.Errorf("sunucu sha256 %q, beklenen %q", n.Sha256, sha)
	}
	inen := filepath.Join(t.TempDir(), "inen.bin")
	if err := o.h.Indir(o.ctx, ad, inen); err != nil {
		t.Fatalf("Indir(%q): %v", ad, err)
	}
	if got := dosyaSha(t, inen); got != sha {
		t.Errorf("indirilen sha256 %s, yüklenen %s", got, sha)
	}
}

func yokOlmali(t sinayici, o *ortam, ad, neden string) {
	t.Helper()
	if _, ok := bul(t, o, ad); ok {
		t.Errorf("%s: nihai adda (%q) dosya KALDI", neden, ad)
	}
	inen := filepath.Join(t.TempDir(), "olmamali.bin")
	err := o.h.Indir(o.ctx, ad, inen)
	if err == nil {
		t.Errorf("%s: olmayan dosya indirilebildi", neden)
	} else if k := hedef.KategoriAl(err); k != hedef.KategoriBulunamadi {
		t.Errorf("%s: olmayan dosyada kategori %q, beklenen bulunamadi (%v)", neden, k, err)
	}
	var h *hedef.Hata
	if err != nil && !errors.As(err, &h) {
		t.Errorf("%s: hata *hedef.Hata değil: %T", neden, err)
	}
	if _, serr := os.Stat(inen); serr == nil {
		t.Errorf("%s: başarısız Indir yerel dosya bıraktı", neden)
	}
}

// ─── denemeler ──────────────────────────────────────────────────────────────

func denemeYetenekler(t sinayici, o *ortam) {
	y := o.h.Yetenekler()
	if !y.Var(hedef.YetenekAtomikYukle) {
		t.Errorf("sözleşme: YetenekAtomikYukle zorunlu (hata/iptal nihai adda dosya bırakmamalı); yetenekler: %s", y)
	}
	if !y.Var(hedef.YetenekIlerleme) {
		t.Errorf("sözleşme: YetenekIlerleme zorunlu (iptal ve arayüz ilerlemesi); yetenekler: %s", y)
	}
	if strings.TrimSpace(o.h.Tur()) == "" {
		t.Errorf("Tur boş")
	}
}

func denemeTest(t sinayici, o *ortam) {
	if err := o.h.Test(o.ctx); err != nil {
		t.Errorf("Test: %v", err)
	}
}

func denemeGidisDonus(t sinayici, o *ortam) {
	ad := o.ad("gidis-donus/c_ornek-20260825-233538.tar.gz.age")
	gidisDonus(t, o, ad, 1<<20+123, 1)
	if err := o.h.Sil(o.ctx, ad); err != nil {
		t.Fatalf("Sil: %v", err)
	}
	yokOlmali(t, o, ad, "silindikten sonra")
	if err := o.h.Sil(o.ctx, ad); err != nil {
		t.Errorf("ikinci Sil hata verdi (yoksa nil olmalı): %v", err)
	}
}

func denemeTurkceAd(t sinayici, o *ortam) {
	ad := o.ad("Yedek Klasörü/müşteri ğüşıöç İŞ yedeği 2026-08-25.tar.gz.age")
	gidisDonus(t, o, ad, 4096+7, 2)
	if err := o.h.Sil(o.ctx, ad); err != nil {
		t.Fatalf("Sil: %v", err)
	}
	yokOlmali(t, o, ad, "Türkçe/boşluklu ad silindikten sonra")
}

func denemeUstuneYazma(t sinayici, o *ortam) {
	ad := o.ad("ustune/yedek.age")
	gidisDonus(t, o, ad, 10000, 3)
	gidisDonus(t, o, ad, 20000, 4) // aynı ad, farklı içerik → yenisi kazanır
}

func denemeOnekListeleme(t sinayici, o *ortam) {
	if !o.h.Yetenekler().Var(hedef.YetenekOnekListele) {
		t.Logf("YetenekOnekListele yok — atlandı")
		return
	}
	adlar := []string{o.ad("onek/a1.age"), o.ad("onek/a2.age"), o.ad("onek/b1.age"), o.ad("onek/alt/a3.age")}
	for i, ad := range adlar {
		yerel, sha := dosyaUret(t, int64(100+i), uint64(10+i))
		yukle(t, o, yerel, ad, sha, int64(100+i))
	}
	liste, err := o.h.Listele(o.ctx, o.onek+"onek/a")
	if err != nil {
		t.Fatalf("Listele önek: %v", err)
	}
	var got []string
	for _, n := range liste {
		got = append(got, n.Ad)
	}
	if strings.Join(got, ",") != adlar[0]+","+adlar[1] {
		t.Errorf("önek listesi %v, beklenen [%s %s] (alt dizine inmemeli)", got, adlar[0], adlar[1])
	}
	tum, err := o.h.Listele(o.ctx, o.onek+"onek/")
	if err != nil || len(tum) != 3 {
		t.Errorf("dizin listesi %d öğe (beklenen 3, alt dizin hariç): %v", len(tum), err)
	}
}

func denemeBuyuk(t sinayici, o *ortam) {
	if o.kisa {
		t.Logf("-short: 64 MiB denemesi atlandı")
		return
	}
	ad := o.ad("buyuk/64mb.tar.gz.age")
	gidisDonus(t, o, ad, 64<<20, 5)
}

func denemeIptalOrtada(t sinayici, o *ortam) {
	const boyut = 8 << 20
	yerel, sha := dosyaUret(t, boyut, 6)
	ad := o.ad("iptal/ortada.age")
	ctx, iptal := context.WithCancel(o.ctx)
	defer iptal()
	// Ilerleme uygulamanın kendi goroutine'inden çağrılabilir → atomik bayrak.
	var araDeger atomic.Bool
	err := o.h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{Boyut: boyut, Sha256: sha, Ilerleme: func(n int64) {
		if n > 0 && n < boyut && araDeger.CompareAndSwap(false, true) {
			iptal()
		}
	}})
	if !araDeger.Load() {
		t.Errorf("Ilerleme aktarım SIRASINDA hiç çağrılmadı — ortada iptal sınanamadı")
	}
	if err == nil {
		t.Errorf("aktarım ortasında iptal edilen Yukle başarı döndü")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("iptal hatası errors.Is(context.Canceled) ile tanınmalı: %v", err)
	}
	yokOlmali(t, o, ad, "ortada iptal")
}

func denemeIptalOnceden(t sinayici, o *ortam) {
	yerel, sha := dosyaUret(t, 1<<20, 7)
	ad := o.ad("iptal/onceden.age")
	ctx, iptal := context.WithCancel(o.ctx)
	iptal()
	if err := o.h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{Sha256: sha}); err == nil {
		t.Errorf("iptal edilmiş bağlamla Yukle başarı döndü")
	}
	yokOlmali(t, o, ad, "önceden iptal")
}

func denemeYanlisSha(t sinayici, o *ortam) {
	if !o.h.Yetenekler().Var(hedef.YetenekSunucuSha256) {
		t.Logf("YetenekSunucuSha256 yok — atlandı")
		return
	}
	yerel, _ := dosyaUret(t, 4096, 8)
	ad := o.ad("sha/yanlis.age")
	yanlis := strings.Repeat("0", 64)
	if err := o.h.Yukle(o.ctx, yerel, ad, hedef.YukleSecenek{Sha256: yanlis}); err == nil {
		t.Errorf("yanlış sha256 ile Yukle başarı döndü")
	}
	yokOlmali(t, o, ad, "yanlış sha256")
}

func denemeGecersizAd(t sinayici, o *ortam) {
	yerel, _ := dosyaUret(t, 16, 9)
	kotu := []string{"", "/mutlak.age", o.onek + "../kacis.age", o.onek + "a/./b.age", o.onek + `ters\bolu.age`,
		o.onek + "satir\nsonu.age", o.onek + "son/"}
	for _, ad := range kotu {
		if err := o.h.Yukle(o.ctx, yerel, ad, hedef.YukleSecenek{}); err == nil {
			t.Errorf("geçersiz ad Yukle'de KABUL edildi: %q", ad)
			_ = o.h.Sil(o.ctx, ad)
		}
		if err := o.h.Indir(o.ctx, ad, filepath.Join(t.TempDir(), "x")); err == nil {
			t.Errorf("geçersiz ad Indir'de KABUL edildi: %q", ad)
		}
		if err := o.h.Sil(o.ctx, ad); err == nil {
			t.Errorf("geçersiz ad Sil'de KABUL edildi: %q", ad)
		}
	}
}

func denemeOlmayanSil(t sinayici, o *ortam) {
	if err := o.h.Sil(o.ctx, o.onek+"hic-olmadi/yok.age"); err != nil {
		t.Errorf("olmayan dosyayı Sil hata verdi (nil olmalı): %v", err)
	}
	liste, err := o.h.Listele(o.ctx, o.onek+"hic-olmadi/yok")
	if err != nil || len(liste) != 0 {
		t.Errorf("olmayan önek: boş liste ve nil hata beklenirdi: %v %v", liste, err)
	}
}
