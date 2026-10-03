package hedeftest

// bellek.go — bellek içi sahte hedef.Hedef: sözleşmenin başvuru uygulaması
// (Uyum'dan geçer) ve orkestratör testleri için hata enjeksiyonlu test çifti.
// Ağa, diske (yerel dosyalar dışında) ve canlı sisteme dokunmaz.
//
// Yükleme gerçek bir uzak depo gibi davranır: veri parça parça hedef.UzakGeciciAd
// adlı GÖRÜNÜR bir geçici dosyaya yazılır (Listele onu görür), özet tutarsa
// nihai ada taşınır, hata/iptalde geçici dosya silinir. Nesneler dosya sistemi
// düğümü gibi İŞARETÇİDİR: yükleme açtığı düğüme yazar, yeniden adlandırma
// düğümü taşır (sabit geçici ad kullanan bozuk varyantta başka yüklemenin
// yazdığı veri, taşınmış dosyayı bozar — gerçek depodaki gibi).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
)

type bellekNesne struct {
	veri    []byte
	sha     string
	degisim time.Time
}

// Bellek — eşzamanlı kullanıma güvenli sahte hedef.
type Bellek struct {
	mu       sync.Mutex
	nesneler map[string]*bellekNesne
	hatalar  map[string]error
	cagri    map[string]int
	suren    int
	sir      string

	// Yalnız sözleşme meta-testleri için bilerek BOZUK davranışlar.
	atomikDegil  bool // yükleme doğrudan nihai ada yazılır (yarım dosya kalır)
	adDenetimsiz bool // AdDogrula atlanır
	ilerlemesiz  bool // Ilerleme hiç çağrılmaz
	geciciBirak  bool // hata/iptalde geçici dosya silinmez
	ortakGecici  bool // dizin başına TEK sabit geçici ad (eşzamanlı yüklemeler birbirini bozar)
	sirSizdir    bool // ham hata nedeni sır ile maskelenmeden sarılır
	// shaDenetimsiz — sunucu tarafı özet denetimi yok (ortakGecici ile karışık
	// içerik BAŞARI olarak döner).
	shaDenetimsiz bool
	// eszamanliKayip — nihai ad yalnız o an süren TEK yükleme iken yazılır:
	// eşzamanlı yüklemeler nil döner ama dosya kaybolur.
	eszamanliKayip bool
	// eszamanliGeciciBirak — eşzamanlı başarıda geçici ad da listede kalır.
	eszamanliGeciciBirak bool
}

// YeniBellek — boş sahte hedef.
func YeniBellek() *Bellek {
	return &Bellek{nesneler: map[string]*bellekNesne{}, hatalar: map[string]error{}, cagri: map[string]int{}}
}

// YeniBellekSirli — ham hata nedenlerine nöbetçi sırrı koyan sahte hedef
// (sağlayıcının hata metnine URL/kimlik yansıtmasının benzetimi). Doğru
// uygulama gibi sırrı Sarmala'ya verir; Uyum(t, h, sir) sızıntı arar.
func YeniBellekSirli(sir string) *Bellek {
	b := YeniBellek()
	b.sir = sir
	return b
}

// Tur — "bellek".
func (b *Bellek) Tur() string { return "bellek" }

// Yetenekler — sözleşme üstü yeteneklerin tümü.
func (b *Bellek) Yetenekler() hedef.Yetenek {
	return hedef.YetenekAtomikYukle | hedef.YetenekOnekListele | hedef.YetenekSunucuSha256 |
		hedef.YetenekBantSiniri | hedef.YetenekIlerleme
}

// HataVer — islem ("test", "yukle", "listele", "indir", "sil") çağrıldığında
// err döndür (hedef.Sarmala ile sınıflanır). nil → enjeksiyonu kaldır.
func (b *Bellek) HataVer(islem string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		delete(b.hatalar, islem)
		return
	}
	b.hatalar[islem] = err
}

// Cagri — islem kaç kez çağrıldı.
func (b *Bellek) Cagri(islem string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cagri[islem]
}

// Icerik — uzak dosyanın kopyası.
func (b *Bellek) Icerik(ad string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, ok := b.nesneler[ad]
	if !ok {
		return nil, false
	}
	return bytes.Clone(n.veri), true
}

// Adlar — tüm uzak adlar (sıralı; yarım yüklemelerin geçici adları dahil).
func (b *Bellek) Adlar() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.nesneler))
	for ad := range b.nesneler {
		out = append(out, ad)
	}
	sort.Strings(out)
	return out
}

// Koy — test hazırlığı: uzak dosyayı doğrudan yerleştirir.
func (b *Bellek) Koy(ad string, veri []byte) {
	s := sha256.Sum256(veri)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nesneler[ad] = &bellekNesne{veri: bytes.Clone(veri), sha: hex.EncodeToString(s[:]), degisim: time.Now()}
}

// Boz — uzak kopyanın ilk baytını çevirir (sessiz bit çürümesi). Sağlayıcının
// yükleme anında kaydettiği özet DEĞİŞMEZ: orkestratör indirdiği dosyanın
// özetini kendisi karşılaştırmalıdır.
func (b *Bellek) Boz(ad string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, ok := b.nesneler[ad]
	if !ok || len(n.veri) == 0 {
		return false
	}
	v := bytes.Clone(n.veri)
	v[0] ^= 0xff
	n.veri = v
	return true
}

// SurenYukleme — o anda süren yükleme sayısı.
func (b *Bellek) SurenYukleme() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.suren
}

// neden — ham hata nedeni; nöbetçi sır ayarlıysa metninde sır geçer.
func (b *Bellek) neden(err error) error {
	if b.sir == "" {
		return err
	}
	return fmt.Errorf("bellek deposu (anahtar %s): %w", b.sir, err)
}

func (b *Bellek) sirlar() []string {
	if b.sirSizdir {
		return nil
	}
	return []string{b.sir}
}

func (b *Bellek) sarmala(islem string, err error) error {
	if err == nil {
		return nil
	}
	return hedef.Sarmala(islem, b.neden(err), b.sirlar()...)
}

func (b *Bellek) yeniHata(k hedef.Kategori, islem string, err error) error {
	return hedef.YeniHata(k, islem, b.neden(err), b.sirlar()...)
}

func (b *Bellek) enjekte(islem string) error {
	b.mu.Lock()
	b.cagri[islem]++
	err := b.hatalar[islem]
	b.mu.Unlock()
	return b.sarmala(islem, err)
}

func (b *Bellek) adDenetle(islem, ad string) error {
	if b.adDenetimsiz {
		return nil
	}
	if err := hedef.AdDogrula(ad); err != nil {
		return b.yeniHata(hedef.KategoriBilinmeyen, islem, err)
	}
	return nil
}

// Test — dizin oluştur → yaz → oku → sil benzetimi.
func (b *Bellek) Test(ctx context.Context) error {
	if err := b.enjekte("test"); err != nil {
		return err
	}
	return b.sarmala("test", ctx.Err())
}

// bellekYazici — açık düğüme parça parça ekler; her parça kilit altında görünür
// olur (gerçek depodaki yarım dosya gibi). Düğüm sonradan yeniden adlandırılsa
// da yazı ona gider (açık dosya tanıtıcısı).
type bellekYazici struct {
	b   *Bellek
	dug *bellekNesne
}

func (w bellekYazici) Write(p []byte) (int, error) {
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	w.dug.veri = append(w.dug.veri, p...)
	w.dug.degisim = time.Now()
	return len(p), nil
}

// geciciAd — yüklemenin yazacağı uzak ad (bozuk varyantlar burada ayrışır).
func (b *Bellek) geciciAd(uzakAd string) (string, error) {
	switch {
	case b.atomikDegil:
		return uzakAd, nil
	case b.adDenetimsiz:
		return uzakAd + "~", nil
	case b.ortakGecici:
		d, _ := hedef.AdBol(uzakAd)
		if d != "" {
			d += "/"
		}
		return d + hedef.UzakGeciciOnek + "0-ortak", nil
	}
	return hedef.UzakGeciciAd(uzakAd)
}

// Yukle — atomik: veri önce geçici uzak dosyaya akar, özet tutarsa nihai ada
// taşınır; hata/iptalde geçici dosya silinir.
func (b *Bellek) Yukle(ctx context.Context, yerelYol, uzakAd string, sec hedef.YukleSecenek) error {
	if err := b.adDenetle("yukle", uzakAd); err != nil {
		return err
	}
	if err := b.enjekte("yukle"); err != nil {
		return err
	}
	gecici, err := b.geciciAd(uzakAd)
	if err != nil {
		return b.yeniHata(hedef.KategoriBilinmeyen, "yukle", err)
	}
	f, err := os.Open(yerelYol)
	if err != nil {
		return b.sarmala("yukle", err)
	}
	defer f.Close()

	b.mu.Lock()
	b.suren++
	dug := b.nesneler[gecici]
	if dug == nil || !b.ortakGecici {
		dug = &bellekNesne{} // oluştur (O_CREAT|O_TRUNC, yeni düğüm)
		b.nesneler[gecici] = dug
	}
	dug.veri = dug.veri[:0] // sabit geçici adda: var olan düğümü kırp
	dug.degisim = time.Now()
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.suren--
		b.mu.Unlock()
	}()

	ilerleme := sec.Ilerleme
	if b.ilerlemesiz {
		ilerleme = nil
	}
	r := hedef.IlerlemeOkuyucu(ctx, hedef.SinirliOkuyucu(ctx, f, sec.BantKBps), ilerleme)
	_, kopyaHata := io.Copy(bellekYazici{b: b, dug: dug}, r)
	if kopyaHata == nil {
		kopyaHata = ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	s := sha256.Sum256(dug.veri)
	ozet := hex.EncodeToString(s[:])
	var sonuc error
	switch {
	case kopyaHata != nil:
		sonuc = b.sarmala("yukle", kopyaHata)
	case !b.shaDenetimsiz && sec.Sha256 != "" && !strings.EqualFold(sec.Sha256, ozet):
		// Sunucu tarafı özet denetimi: depoya ulaşan veri beklenen değil.
		sonuc = b.yeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("sha256 uyuşmuyor (aktarım bozuk)"))
	}
	if sonuc != nil {
		if !b.atomikDegil && !b.geciciBirak && b.nesneler[gecici] == dug {
			delete(b.nesneler, gecici)
		}
		return sonuc
	}
	if b.eszamanliKayip && b.suren > 1 {
		if b.nesneler[gecici] == dug {
			delete(b.nesneler, gecici)
		}
		return nil // BOZUK: başarı bildirir, nihai ad yazılmaz
	}
	dug.sha = ozet
	dug.degisim = time.Now()
	b.nesneler[uzakAd] = dug // yeniden adlandırma: düğüm taşınır
	if gecici != uzakAd && b.nesneler[gecici] == dug && !(b.eszamanliGeciciBirak && b.suren > 1) {
		delete(b.nesneler, gecici)
	}
	return nil
}

// Listele — önekin dizinindeki, adı önekin son bileşeniyle başlayan dosyalar
// (yarım yüklemelerin geçici dosyaları dahil).
func (b *Bellek) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	dizin, adOneki, err := hedef.OnekBol(onek)
	if err != nil {
		return nil, b.yeniHata(hedef.KategoriBilinmeyen, "listele", err)
	}
	if err := b.enjekte("listele"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, b.sarmala("listele", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []hedef.Nesne
	for ad, n := range b.nesneler {
		d, dosya := hedef.AdBol(ad)
		if d == dizin && strings.HasPrefix(dosya, adOneki) {
			out = append(out, hedef.Nesne{Ad: ad, Boyut: int64(len(n.veri)), Degisim: n.degisim, Sha256: n.sha})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ad < out[j].Ad })
	return out, nil
}

// Indir — geçici ada yazar, sonra yeniden adlandırır (yarım içerik yerelYol'a düşmez).
func (b *Bellek) Indir(ctx context.Context, uzakAd, yerelYol string) error {
	if err := b.adDenetle("indir", uzakAd); err != nil {
		return err
	}
	if err := b.enjekte("indir"); err != nil {
		return err
	}
	b.mu.Lock()
	n, ok := b.nesneler[uzakAd]
	var veri []byte
	if ok {
		veri = bytes.Clone(n.veri)
	}
	b.mu.Unlock()
	if !ok {
		return b.yeniHata(hedef.KategoriBulunamadi, "indir", fs.ErrNotExist)
	}
	return hedef.AtomikYaz(ctx, yerelYol, bytes.NewReader(veri))
}

// Sil — yoksa nil.
func (b *Bellek) Sil(ctx context.Context, uzakAd string) error {
	if err := b.adDenetle("sil", uzakAd); err != nil {
		return err
	}
	if err := b.enjekte("sil"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return b.sarmala("sil", err)
	}
	b.mu.Lock()
	delete(b.nesneler, uzakAd)
	b.mu.Unlock()
	return nil
}
