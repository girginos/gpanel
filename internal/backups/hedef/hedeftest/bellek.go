package hedeftest

// bellek.go — bellek içi sahte hedef.Hedef: sözleşmenin başvuru uygulaması
// (Uyum'dan geçer) ve orkestratör testleri için hata enjeksiyonlu test çifti.
// Ağa, diske (yerel dosyalar dışında) ve canlı sisteme dokunmaz.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	nesneler map[string]bellekNesne
	hatalar  map[string]error
	cagri    map[string]int
	suren    int

	// Yalnız sözleşme meta-testleri için bilerek BOZUK davranışlar.
	atomikDegil  bool // yükleme doğrudan nihai ada yazılır (yarım dosya kalır)
	adDenetimsiz bool // AdDogrula atlanır
	ilerlemesiz  bool // Ilerleme hiç çağrılmaz
}

// YeniBellek — boş sahte hedef.
func YeniBellek() *Bellek {
	return &Bellek{nesneler: map[string]bellekNesne{}, hatalar: map[string]error{}, cagri: map[string]int{}}
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

// Adlar — tüm uzak adlar (sıralı).
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
	b.nesneler[ad] = bellekNesne{veri: bytes.Clone(veri), sha: hex.EncodeToString(s[:]), degisim: time.Now()}
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
	b.nesneler[ad] = n
	return true
}

// SurenYukleme — o anda süren yükleme sayısı.
func (b *Bellek) SurenYukleme() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.suren
}

func (b *Bellek) enjekte(islem string) error {
	b.mu.Lock()
	b.cagri[islem]++
	err := b.hatalar[islem]
	b.mu.Unlock()
	if err != nil {
		return hedef.Sarmala(islem, err)
	}
	return nil
}

func (b *Bellek) adDenetle(islem, ad string) error {
	if b.adDenetimsiz {
		return nil
	}
	if err := hedef.AdDogrula(ad); err != nil {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, islem, err)
	}
	return nil
}

// Test — dizin oluştur → yaz → oku → sil benzetimi.
func (b *Bellek) Test(ctx context.Context) error {
	if err := b.enjekte("test"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("test", err)
	}
	return nil
}

// Yukle — atomik: veri önce geçici alana akar, özet tutarsa nihai ada konur.
func (b *Bellek) Yukle(ctx context.Context, yerelYol, uzakAd string, sec hedef.YukleSecenek) error {
	if err := b.adDenetle("yukle", uzakAd); err != nil {
		return err
	}
	if err := b.enjekte("yukle"); err != nil {
		return err
	}
	f, err := os.Open(yerelYol)
	if err != nil {
		return hedef.Sarmala("yukle", err)
	}
	defer f.Close()

	b.mu.Lock()
	b.suren++
	if b.atomikDegil {
		b.nesneler[uzakAd] = bellekNesne{degisim: time.Now()}
	}
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
	var buf bytes.Buffer
	h := sha256.New()
	_, kopyaHata := io.Copy(io.MultiWriter(&buf, h), r)
	if kopyaHata == nil {
		kopyaHata = ctx.Err()
	}
	if kopyaHata != nil {
		if b.atomikDegil {
			b.mu.Lock()
			b.nesneler[uzakAd] = bellekNesne{veri: buf.Bytes(), degisim: time.Now()}
			b.mu.Unlock()
		}
		return hedef.Sarmala("yukle", kopyaHata)
	}
	ozet := hex.EncodeToString(h.Sum(nil))
	if sec.Sha256 != "" && !strings.EqualFold(sec.Sha256, ozet) {
		if b.atomikDegil {
			b.mu.Lock()
			b.nesneler[uzakAd] = bellekNesne{veri: buf.Bytes(), sha: ozet, degisim: time.Now()}
			b.mu.Unlock()
		}
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("sha256 uyuşmuyor (aktarım bozuk)"))
	}
	b.mu.Lock()
	b.nesneler[uzakAd] = bellekNesne{veri: buf.Bytes(), sha: ozet, degisim: time.Now()}
	b.mu.Unlock()
	return nil
}

// Listele — önekin dizinindeki, adı önekin son bileşeniyle başlayan dosyalar.
func (b *Bellek) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	dizin, adOneki, err := hedef.OnekBol(onek)
	if err != nil {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "listele", err)
	}
	if err := b.enjekte("listele"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, hedef.Sarmala("listele", err)
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
	veri := bytes.Clone(n.veri)
	b.mu.Unlock()
	if !ok {
		return hedef.YeniHata(hedef.KategoriBulunamadi, "indir", fs.ErrNotExist)
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
		return hedef.Sarmala("sil", err)
	}
	b.mu.Lock()
	delete(b.nesneler, uzakAd)
	b.mu.Unlock()
	return nil
}
