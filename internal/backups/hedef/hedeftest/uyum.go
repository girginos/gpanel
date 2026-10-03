// Package hedeftest — hedef.Hedef sözleşmesinin çalıştırılabilir tanımı (Uyum)
// ve bellek içi sahte hedef (Bellek). Yalnız testlerden içe aktarılır.
//
// Her yeni hedef türü kendi testinde (httptest ya da geçici yerel sunucuya
// karşı) Uyum'u, hedefi kurarken kullandığı TÜM sırlarla (parola, gizli anahtar,
// jeton — nöbetçi değerler) çağırır:
//
//	func TestS3Uyum(t *testing.T) { hedeftest.Uyum(t, s3Hedefi(t, nobet), nobet) }
//
// Uyum, hedefin tabanında rastgele bir "gosp-uyum-<hex>/" alt dizininde çalışır
// ve sonunda oluşturduğu her dosyayı siler. Denetlediği her hata dönüşünde:
// doğrudan *hedef.Hata (sarılmamış), geçerli kategori ve Error()/Ayrinti()'de
// sır YOK.
package hedeftest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/url"
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
	h      hedef.Hedef
	onek   string
	ctx    context.Context
	kisa   bool
	sirlar []string

	mu    sync.Mutex
	adlar map[string]bool
}

func yeniOrtam(t *testing.T, h hedef.Hedef, sirlar ...string) *ortam {
	t.Helper()
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	ctx, iptal := context.WithTimeout(context.Background(), 20*time.Minute)
	o := &ortam{h: h, onek: "gosp-uyum-" + hex.EncodeToString(rnd[:]) + "/", ctx: ctx,
		kisa: testing.Short(), adlar: map[string]bool{}}
	for _, s := range sirlar {
		if strings.TrimSpace(s) != "" {
			o.sirlar = append(o.sirlar, s)
		}
	}
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

// Uyum — h'nin hedef.Hedef sözleşmesine uyduğunu doğrular. sirlar: hedefin
// kurulumunda kullanılan nöbetçi sırlar; hiçbir hata metnine sızmamalı.
func Uyum(t *testing.T, h hedef.Hedef, sirlar ...string) {
	t.Helper()
	o := yeniOrtam(t, h, sirlar...)
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
		{"eszamanli_yukleme", denemeEszamanli},
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
	dogrula(t, o, ad, sha, boyut)
}

// dogrula — uzaktaki ad: Listele'de boyut (+ varsa sunucu sha) ve indirilen sha.
func dogrula(t sinayici, o *ortam, ad, sha string, boyut int64) {
	t.Helper()
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
	hataDenetle(t, o, err, neden+": Indir")
	if _, serr := os.Stat(inen); serr == nil {
		t.Errorf("%s: başarısız Indir yerel dosya bıraktı", neden)
	}
}

// hataDenetle — hata dönüşü sözleşmesi (err nil ise bir şey yapmaz): doğrudan
// *hedef.Hata (saran hata dış metni Error()'a sokar), geçerli kategori ve
// Error()/Ayrinti()'de nöbetçi sır YOK. Sır değeri çıktıya YAZILMAZ.
func hataDenetle(t sinayici, o *ortam, err error, neden string) {
	t.Helper()
	if err == nil {
		return
	}
	metinler := []string{err.Error()}
	var h *hedef.Hata
	if errors.As(err, &h) {
		if err.Error() != h.Error() {
			t.Errorf("%s: *hedef.Hata SARILMIŞ, dış metin Error()'a giriyor: %q", neden, err.Error())
		}
		if !h.Kategori.Gecerli() {
			t.Errorf("%s: geçersiz kategori %q", neden, h.Kategori)
		}
		metinler = append(metinler, h.Ayrinti())
	} else {
		t.Errorf("%s: hata *hedef.Hata değil (%T): ham metin Error()'a sızabilir", neden, err)
	}
	for i, s := range o.sirlar {
		for _, m := range metinler {
			if strings.Contains(m, s) || strings.Contains(m, url.QueryEscape(s)) {
				t.Errorf("%s: %d. nöbetçi sır hata metnine SIZDI (Error ya da Ayrinti)", neden, i+1)
				break
			}
		}
	}
}

// iptalDenetle — iptal edilmiş bağlamla yapılan işlemin hatası: nil DEĞİL ve
// errors.Is(err, context.Canceled) (tek koşul: nil de Canceled değildir). Ayrıca
// hataDenetle. Orkestratör iptali ağ arızasından böyle ayırır (yeniden denemez).
func iptalDenetle(t sinayici, o *ortam, err error, neden string) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("%s: iptal edilmiş bağlamla nil ya da Canceled olmayan hata döndü (errors.Is(context.Canceled) beklenir): %v", neden, err)
	}
	hataDenetle(t, o, err, neden)
}

// dizinBos — başarısız yüklemeden sonra dizinde (Listele(dizin/)) HİÇBİR dosya
// kalmamalı: ne nihai ad ne de temizlenmemiş geçici dosya. YetenekOnekListele
// yoksa denetlenemez (yetenekler denemesi zaten düşer).
func dizinBos(t sinayici, o *ortam, dizin, neden string) {
	t.Helper()
	if !o.h.Yetenekler().Var(hedef.YetenekOnekListele) {
		return
	}
	liste, err := o.h.Listele(o.ctx, o.onek+dizin+"/")
	if err != nil {
		t.Errorf("%s: Listele(%s/): %v", neden, dizin, err)
		return
	}
	for _, n := range liste {
		if hedef.GeciciMi(n.Ad) {
			t.Errorf("%s: yarım geçici dosya TEMİZLENMEDİ: %q", neden, n.Ad)
		} else {
			t.Errorf("%s: dizinde fazladan dosya KALDI (geçici ise hedef.UzakGeciciAd kullanılmalı): %q", neden, n.Ad)
		}
		o.mu.Lock()
		o.adlar[n.Ad] = true // Uyum sonunda temizlensin
		o.mu.Unlock()
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
	if !y.Var(hedef.YetenekOnekListele) {
		t.Errorf("sözleşme: YetenekOnekListele zorunlu (yarım dosya temizliği ve sahipsiz dosya raporu); yetenekler: %s", y)
	}
	if strings.TrimSpace(o.h.Tur()) == "" {
		t.Errorf("Tur boş")
	}
}

func denemeTest(t sinayici, o *ortam) {
	if err := o.h.Test(o.ctx); err != nil {
		t.Errorf("Test: %v", err)
	}
	// Test gerçekten depoya gidiyorsa iptal edilmiş bağlamla BAŞARAMAZ ("hiçbir
	// şey yapmadan nil" dönen Test yakalanır).
	ctx, iptal := context.WithCancel(o.ctx)
	iptal()
	iptalDenetle(t, o, o.h.Test(ctx), "iptal edilmiş Test (nil ise erişimi gerçekten sınamıyor)")
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
	ad := o.ad("iptal-ortada/yedek.age")
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
	iptalDenetle(t, o, err, "ortada iptal: Yukle")
	yokOlmali(t, o, ad, "ortada iptal")
	dizinBos(t, o, "iptal-ortada", "ortada iptal")
}

func denemeIptalOnceden(t sinayici, o *ortam) {
	yerel, sha := dosyaUret(t, 1<<20, 7)
	ad := o.ad("iptal-onceden/yedek.age")
	ctx, iptal := context.WithCancel(o.ctx)
	iptal()
	iptalDenetle(t, o, o.h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{Sha256: sha}), "önceden iptal: Yukle")
	yokOlmali(t, o, ad, "önceden iptal")
	dizinBos(t, o, "iptal-onceden", "önceden iptal")

	// Diğer işlemler de iptal edilmiş bağlama uyar; iptal edilen Sil dosyayı silmez.
	var2 := o.ad("iptal-onceden/var.age")
	yerel2, sha2 := dosyaUret(t, 2048, 17)
	yukle(t, o, yerel2, var2, sha2, 2048)
	_, lerr := o.h.Listele(ctx, o.onek+"iptal-onceden/")
	iptalDenetle(t, o, lerr, "önceden iptal: Listele")
	inen := filepath.Join(t.TempDir(), "iptal.bin")
	iptalDenetle(t, o, o.h.Indir(ctx, var2, inen), "önceden iptal: Indir")
	if _, serr := os.Stat(inen); serr == nil {
		t.Errorf("iptal edilmiş Indir yerel dosya bıraktı")
	}
	iptalDenetle(t, o, o.h.Sil(ctx, var2), "önceden iptal: Sil")
	if _, ok := bul(t, o, var2); !ok {
		t.Errorf("iptal edilmiş Sil dosyayı SİLDİ")
	}
	if err := o.h.Sil(o.ctx, var2); err != nil {
		t.Fatalf("Sil: %v", err)
	}
}

func denemeYanlisSha(t sinayici, o *ortam) {
	if !o.h.Yetenekler().Var(hedef.YetenekSunucuSha256) {
		t.Logf("YetenekSunucuSha256 yok — atlandı")
		return
	}
	yerel, _ := dosyaUret(t, 4096, 8)
	ad := o.ad("sha-yanlis/yedek.age")
	yanlis := strings.Repeat("0", 64)
	err := o.h.Yukle(o.ctx, yerel, ad, hedef.YukleSecenek{Sha256: yanlis})
	if err == nil {
		t.Errorf("yanlış sha256 ile Yukle başarı döndü")
	}
	hataDenetle(t, o, err, "yanlış sha256: Yukle")
	yokOlmali(t, o, ad, "yanlış sha256")
	dizinBos(t, o, "sha-yanlis", "yanlış sha256")
}

func denemeGecersizAd(t sinayici, o *ortam) {
	yerel, _ := dosyaUret(t, 16, 9)
	kotu := []string{"", "/mutlak.age", o.onek + "../kacis.age", o.onek + "a/./b.age", o.onek + `ters\bolu.age`,
		o.onek + "satir\nsonu.age", o.onek + "son/"}
	for _, ad := range kotu {
		if err := o.h.Yukle(o.ctx, yerel, ad, hedef.YukleSecenek{}); err == nil {
			t.Errorf("geçersiz ad Yukle'de KABUL edildi: %q", ad)
			_ = o.h.Sil(o.ctx, ad)
		} else {
			hataDenetle(t, o, err, "geçersiz ad: Yukle")
		}
		if err := o.h.Indir(o.ctx, ad, filepath.Join(t.TempDir(), "x")); err == nil {
			t.Errorf("geçersiz ad Indir'de KABUL edildi: %q", ad)
		} else {
			hataDenetle(t, o, err, "geçersiz ad: Indir")
		}
		if err := o.h.Sil(o.ctx, ad); err == nil {
			t.Errorf("geçersiz ad Sil'de KABUL edildi: %q", ad)
		} else {
			hataDenetle(t, o, err, "geçersiz ad: Sil")
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

// eszamanliYukle — yüklemeleri AYNI ANDA yürütür. Her yükleme ilk Ilerleme
// çağrısında diğerlerini bekler (en çok 2 sn): böylece hepsi aktarımın ORTASINDA
// üst üste biner. Yüklemeleri kendi içinde sıraya sokan uygulama da meşrudur
// (bekleme süre dolunca sürer).
func eszamanliYukle(o *ortam, yollar, shalar, adlar []string, boyut int64) []error {
	n := len(adlar)
	hepsi := make(chan struct{})
	var sayac atomic.Int32
	hatalar := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var ilk atomic.Bool
			hatalar[i] = o.h.Yukle(o.ctx, yollar[i], adlar[i], hedef.YukleSecenek{Boyut: boyut, Sha256: shalar[i],
				Ilerleme: func(aktarilan int64) {
					if aktarilan <= 0 || !ilk.CompareAndSwap(false, true) {
						return
					}
					if int(sayac.Add(1)) == n {
						close(hepsi)
					}
					bekle := time.NewTimer(2 * time.Second)
					defer bekle.Stop()
					select {
					case <-hepsi:
					case <-bekle.C:
					case <-o.ctx.Done():
					}
				}})
		}(i)
	}
	wg.Wait()
	return hatalar
}

func denemeEszamanli(t sinayici, o *ortam) {
	const n = 3
	const boyut = 256<<10 + 11
	yollar := make([]string, n)
	shalar := make([]string, n)
	for i := range yollar {
		yollar[i], shalar[i] = dosyaUret(t, boyut, uint64(30+i))
	}

	// 1) Aynı dizinde FARKLI adlar: hepsi başarılı, her biri nihai adında
	// (nil dönüp dosyayı kaybeden yükleme yakalanır) ve içerikler karışmamış.
	farkli := make([]string, n)
	for i := range farkli {
		farkli[i] = o.ad(fmt.Sprintf("eszamanli/farkli-%d.age", i))
	}
	farkliHata := eszamanliYukle(o, yollar, shalar, farkli, boyut)
	for i, err := range farkliHata {
		if err != nil {
			t.Errorf("eşzamanlı yükleme (farklı ad) %d başarısız: %v", i, err)
			hataDenetle(t, o, err, "eşzamanlı yükleme")
		}
	}
	for i, ad := range farkli {
		if farkliHata[i] != nil {
			continue
		}
		// Korunan kural "sessiz atlama yok": nil dönen her yükleme doğrulanır.
		// Bu Errorf kaldırılırsa dogrula'nın "Listele'de yok" Fatalf'ı aynı
		// durumu yine yakalar (neg_control: `false && !ok` KAÇAR); asıl gerileme
		// eski `if ok { dogrula }` biçimine dönmek olurdu (neg_control yakalar).
		// Satır, kaybı hangi eşzamanlı yüklemenin yaşadığını söyleyen mesaj için.
		if _, ok := bul(t, o, ad); !ok {
			t.Errorf("eşzamanlı yükleme (farklı ad) %d nil döndü ama %q Listele'de YOK (yükleme kayboldu)", i, ad)
			continue
		}
		dogrula(t, o, ad, shalar[i], boyut)
	}

	// 2) AYNI ada eşzamanlı yüklemeler: en az biri başarılı, kalan içerik
	// başarılı yüklemelerden BİRİNİN tam hali (karışım değil). Sağlayıcı
	// yarışanlardan birini hatayla reddedebilir (ör. oran sınırı).
	ayni := o.ad("eszamanli/ayni.age")
	basarili := map[string]bool{}
	for i, err := range eszamanliYukle(o, yollar, shalar, []string{ayni, ayni, ayni}, boyut) {
		if err == nil {
			basarili[shalar[i]] = true
		} else {
			hataDenetle(t, o, err, "eşzamanlı yükleme (aynı ad)")
		}
	}
	if len(basarili) == 0 {
		t.Errorf("aynı ada eşzamanlı %d yüklemenin HİÇBİRİ başarılı olmadı", n)
	} else {
		inen := filepath.Join(t.TempDir(), "ayni.bin")
		if err := o.h.Indir(o.ctx, ayni, inen); err != nil {
			t.Errorf("Indir(aynı ad): %v", err)
		} else if got := dosyaSha(t, inen); !basarili[got] {
			t.Errorf("aynı ada eşzamanlı yüklemeler KARIŞTI: kalan içerik hiçbir başarılı yüklemeye eşit değil")
		}
	}

	// 3) Dizin listesi TAM olarak başarılı yüklemelerin adlarını içerir: her
	// beklenen ad listede (eksik dizin listesi yakalanır), fazladan dosya ve
	// yarım/geçici dosya yok.
	if o.h.Yetenekler().Var(hedef.YetenekOnekListele) {
		liste, err := o.h.Listele(o.ctx, o.onek+"eszamanli/")
		if err != nil {
			t.Fatalf("Listele(eszamanli/): %v", err)
		}
		var beklenenSira []string
		for i, ad := range farkli {
			if farkliHata[i] == nil {
				beklenenSira = append(beklenenSira, ad)
			}
		}
		if len(basarili) > 0 {
			beklenenSira = append(beklenenSira, ayni)
		}
		beklenen := map[string]bool{ayni: true}
		for _, ad := range farkli {
			beklenen[ad] = true
		}
		listede := map[string]bool{}
		for _, nes := range liste {
			listede[nes.Ad] = true
		}
		for _, ad := range beklenenSira {
			if !listede[ad] {
				t.Errorf("eşzamanlı yüklemeler sonrası başarılı %q dizin listesinde (Listele(eszamanli/)) YOK", ad)
			}
		}
		for _, nes := range liste {
			if !beklenen[nes.Ad] {
				t.Errorf("eşzamanlı yüklemeler sonrası fazladan dosya (geçici=%v): %q", hedef.GeciciMi(nes.Ad), nes.Ad)
				o.mu.Lock()
				o.adlar[nes.Ad] = true
				o.mu.Unlock()
			}
		}
	}
}
