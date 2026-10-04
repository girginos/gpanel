package merkez

// Depolama
//
// 🔴 NEDEN dosya tabanlı (SQLite/MariaDB değil): 182'de hangi veritabanının
// bulunduğu bilinmiyor; CGO'suz SQLite sürücüsü (modernc.org/sqlite) modül
// önbelleğinde yok ve büyük bir bağımlılık ağacı getirir; merkezin kalıcı
// verisi KÜÇÜKTÜR (cihaz kayıtları, bağlantılar, kullanıcı tercihleri, günlük/
// aylık maliyet toplamları — onlarca KB) ve tek süreç yazar. Bu yüzden:
//
//   - durum.json: tümü bellekte, her değişiklikte geçici dosya + fsync + rename
//     (atomik; yarım yazılmış durum dosyası olmaz), 0600.
//   - konusma/YYYY-MM-DD.jsonl: konuşma kaydı (maskeli), gün başına ekleme
//     dosyası; saklama süresi (≤30 gün) dolan dosya bütünüyle silinir.
//
// Depo arayüzü değişmeden SQLite/MariaDB uygulaması eklenebilir.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/destek/protokol"
)

// Cihaz: kayıtlı ajan (müşteri sunucusu). Kimlik = cihaz anahtarı (TOFU).
type Cihaz struct {
	SunucuKimligi   string    `json:"sunucu_kimligi"`
	CihazAnahtari   string    `json:"cihaz_anahtari"`
	LisansParmakIzi string    `json:"lisans_parmak_izi"`
	PanelSurumu     string    `json:"panel_surumu"`
	IlkGorulme      time.Time `json:"ilk_gorulme"`
	SonGorulme      time.Time `json:"son_gorulme"`
	// DogrulamaBitis: lisans biletinin parmak izini doğruladığı an → bu ana kadar "doğrulandı".
	// Sıfır = doğrulanmadı. Her kayıtta yeniden hesaplanır; süre dolunca kendiliğinden düşer.
	DogrulamaBitis time.Time `json:"dogrulama_bitis"`
	DogrulamaNeden string    `json:"dogrulama_neden,omitempty"` // doğrulanamadıysa kısa kod (tanı)
}

// Dogrulandi: lisans parmak izi portal biletiyle doğrulanmış VE doğrulama süresi dolmamış.
func (c Cihaz) Dogrulandi(simdi time.Time) bool {
	return !c.DogrulamaBitis.IsZero() && simdi.Before(c.DogrulamaBitis)
}

// Baglanti: Telegram kullanıcısı ↔ sunucu eşleştirmesi (kapsamın yerel otoritesi ajandır).
type Baglanti struct {
	BaglantiID    string                  `json:"baglanti_id"`
	SunucuKimligi string                  `json:"sunucu_kimligi"`
	TgUserID      int64                   `json:"tg_user_id"`
	Durum         protokol.BaglantiDurumu `json:"durum"`
	Rol           protokol.Rol            `json:"rol"`
	Duzey         protokol.Duzey          `json:"duzey"`
	Hesaplar      []protokol.HesapEtiketi `json:"hesaplar"`
	KVKKKabul     time.Time               `json:"kvkk_kabul"`
	KVKKSurum     string                  `json:"kvkk_surum,omitempty"`  // kabul edilen aydınlatma sürümü (portal)
	KVKKKaynak    string                  `json:"kvkk_kaynak,omitempty"` // "portal" | "telegram" (bot içi düğme) | "" (eski kayıt)
	Guncelleme    time.Time               `json:"guncelleme"`
}

// Kullanici: Telegram kullanıcısının tercihleri.
type Kullanici struct {
	TgUserID       int64  `json:"tg_user_id"`
	Dil            string `json:"dil"` // "" = otomatik
	SeciliBaglanti string `json:"secili_baglanti"`
}

// KonusmaKaydi: maskeli konuşma satırı. Telegram kimliği yerine tek yönlü özet tutulur.
type KonusmaKaydi struct {
	Zaman  time.Time `json:"zaman"`
	Tg     string    `json:"tg"`
	Sunucu string    `json:"sunucu"`
	Yon    string    `json:"yon"` // kullanici | bot | arac | sistem
	Metin  string    `json:"metin"`
}

// tgKisa: Telegram kullanıcı kimliğinin kısa, tek yönlü özeti (log/denetim/devir).
func tgKisa(id int64) string {
	h := sha256.Sum256([]byte("girginos-destek/tg/" + strconv.FormatInt(id, 10)))
	return hex.EncodeToString(h[:8])
}

// Depo: merkezin kalıcı verisi.
type Depo interface {
	CihazAl(sunucu string) (Cihaz, bool)
	CihazKaydet(c Cihaz) error
	// CihazlarParmakIzi: lisans parmak izi iddia eden TÜM cihazlar (doğrulanmış olsun olmasın).
	CihazlarParmakIzi(parmakIzi string) []Cihaz
	BaglantiAl(id string) (Baglanti, bool)
	BaglantiKaydet(b Baglanti) error
	BaglantiSil(id string) error
	KullaniciBaglantilari(tg int64) []Baglanti
	// SunucuBaglantilari: bir sunucu kimliğine ait bağlantılar (portal iç API'si).
	SunucuBaglantilari(sunucu string) []Baglanti
	KullaniciAl(tg int64) (Kullanici, bool)
	KullaniciKaydet(k Kullanici) error
	// KullaniciUnut: /unutbeni — tercih, bağlantılar ve konuşma kaydı silinir.
	KullaniciUnut(tg int64) error
	// MaliyetEkle: sunucu ve genel ("*") günlük/aylık toplamlara ekler.
	MaliyetEkle(sunucu string, an time.Time, usd float64) error
	// Maliyet: sunucu ("*" = genel) için o günün ve ayın toplamı (UTC).
	Maliyet(sunucu string, an time.Time) (gunluk, aylik float64)
	KonusmaEkle(k KonusmaKaydi) error
	KonusmaBuda(simdi time.Time, gun int) error
}

type depoDurumu struct {
	Surum        int                  `json:"surum"`
	Cihazlar     map[string]Cihaz     `json:"cihazlar"`
	Baglantilar  map[string]Baglanti  `json:"baglantilar"`
	Kullanicilar map[string]Kullanici `json:"kullanicilar"`
	Maliyet      map[string]float64   `json:"maliyet"`
}

// DosyaDepo: dizin boşsa yalnız bellekte tutar (test).
type DosyaDepo struct {
	mu      sync.Mutex
	dizin   string
	d       depoDurumu
	konusma []KonusmaKaydi // yalnız bellek kipi
}

// YeniBellekDepo: kalıcı olmayan depo (test).
func YeniBellekDepo() *DosyaDepo {
	return &DosyaDepo{d: bosDurum()}
}

func bosDurum() depoDurumu {
	return depoDurumu{Surum: 1, Cihazlar: map[string]Cihaz{}, Baglantilar: map[string]Baglanti{}, Kullanicilar: map[string]Kullanici{}, Maliyet: map[string]float64{}}
}

// DosyaDepoAc: dizini (0700) hazırlar, durum.json'u yükler.
func DosyaDepoAc(dizin string) (*DosyaDepo, error) {
	if !filepath.IsAbs(dizin) {
		return nil, errors.New("depo dizini mutlak olmalı")
	}
	if err := os.MkdirAll(filepath.Join(dizin, "konusma"), 0o700); err != nil {
		return nil, err
	}
	dd := &DosyaDepo{dizin: dizin, d: bosDurum()}
	b, err := os.ReadFile(filepath.Join(dizin, "durum.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &dd.d); err != nil {
			return nil, fmt.Errorf("durum.json bozuk: %w", err)
		}
		if dd.d.Cihazlar == nil {
			dd.d.Cihazlar = map[string]Cihaz{}
		}
		if dd.d.Baglantilar == nil {
			dd.d.Baglantilar = map[string]Baglanti{}
		}
		if dd.d.Kullanicilar == nil {
			dd.d.Kullanicilar = map[string]Kullanici{}
		}
		if dd.d.Maliyet == nil {
			dd.d.Maliyet = map[string]float64{}
		}
	}
	return dd, nil
}

// atomikYaz: geçici dosya + fsync + rename + dizin fsync.
func atomikYaz(yol string, veri []byte) error {
	dir := filepath.Dir(yol)
	f, err := os.CreateTemp(dir, ".gecici-*")
	if err != nil {
		return err
	}
	gecici := f.Name()
	defer os.Remove(gecici)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(veri); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(gecici, yol); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// kaydet: kilit ALTINDA çağrılır.
func (dd *DosyaDepo) kaydet() error {
	if dd.dizin == "" {
		return nil
	}
	b, err := json.MarshalIndent(dd.d, "", " ")
	if err != nil {
		return err
	}
	return atomikYaz(filepath.Join(dd.dizin, "durum.json"), b)
}

func (dd *DosyaDepo) CihazAl(sunucu string) (Cihaz, bool) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	c, ok := dd.d.Cihazlar[sunucu]
	return c, ok
}

func (dd *DosyaDepo) CihazKaydet(c Cihaz) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.d.Cihazlar[c.SunucuKimligi] = c
	return dd.kaydet()
}

func (dd *DosyaDepo) CihazlarParmakIzi(parmakIzi string) []Cihaz {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	var out []Cihaz
	for _, c := range dd.d.Cihazlar {
		if c.LisansParmakIzi == parmakIzi {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SunucuKimligi < out[j].SunucuKimligi })
	return out
}

func baglantiKopya(b Baglanti) Baglanti {
	b.Hesaplar = slices.Clone(b.Hesaplar)
	return b
}

func (dd *DosyaDepo) BaglantiAl(id string) (Baglanti, bool) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	b, ok := dd.d.Baglantilar[id]
	return baglantiKopya(b), ok
}

func (dd *DosyaDepo) BaglantiKaydet(b Baglanti) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.d.Baglantilar[b.BaglantiID] = baglantiKopya(b)
	return dd.kaydet()
}

func (dd *DosyaDepo) BaglantiSil(id string) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	if _, ok := dd.d.Baglantilar[id]; !ok {
		return nil
	}
	delete(dd.d.Baglantilar, id)
	return dd.kaydet()
}

func (dd *DosyaDepo) KullaniciBaglantilari(tg int64) []Baglanti {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	var out []Baglanti
	for _, b := range dd.d.Baglantilar {
		if b.TgUserID == tg {
			out = append(out, baglantiKopya(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BaglantiID < out[j].BaglantiID })
	return out
}

func (dd *DosyaDepo) SunucuBaglantilari(sunucu string) []Baglanti {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	var out []Baglanti
	for _, b := range dd.d.Baglantilar {
		if b.SunucuKimligi == sunucu {
			out = append(out, baglantiKopya(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BaglantiID < out[j].BaglantiID })
	return out
}

func (dd *DosyaDepo) KullaniciAl(tg int64) (Kullanici, bool) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	k, ok := dd.d.Kullanicilar[strconv.FormatInt(tg, 10)]
	if !ok {
		k.TgUserID = tg
	}
	return k, ok
}

func (dd *DosyaDepo) KullaniciKaydet(k Kullanici) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.d.Kullanicilar[strconv.FormatInt(k.TgUserID, 10)] = k
	return dd.kaydet()
}

func (dd *DosyaDepo) KullaniciUnut(tg int64) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	delete(dd.d.Kullanicilar, strconv.FormatInt(tg, 10))
	for id, b := range dd.d.Baglantilar {
		if b.TgUserID == tg {
			delete(dd.d.Baglantilar, id)
		}
	}
	if err := dd.kaydet(); err != nil {
		return err
	}
	return dd.konusmaSilKilitli(tgKisa(tg))
}

func maliyetAnahtarlari(sunucu string, an time.Time) (gun, ay string) {
	an = an.UTC()
	return "g|" + an.Format("2006-01-02") + "|" + sunucu, "a|" + an.Format("2006-01") + "|" + sunucu
}

func (dd *DosyaDepo) MaliyetEkle(sunucu string, an time.Time, usd float64) error {
	if usd <= 0 {
		return nil
	}
	dd.mu.Lock()
	defer dd.mu.Unlock()
	for _, s := range []string{sunucu, "*"} {
		g, a := maliyetAnahtarlari(s, an)
		dd.d.Maliyet[g] += usd
		dd.d.Maliyet[a] += usd
	}
	// budama: 40 günden eski günlük, 13 aydan eski aylık toplamlar
	esikGun := an.UTC().AddDate(0, 0, -40).Format("2006-01-02")
	esikAy := an.UTC().AddDate(0, -13, 0).Format("2006-01")
	for k := range dd.d.Maliyet {
		p := strings.SplitN(k, "|", 3)
		if len(p) != 3 {
			delete(dd.d.Maliyet, k)
			continue
		}
		if (p[0] == "g" && p[1] < esikGun) || (p[0] == "a" && p[1] < esikAy) {
			delete(dd.d.Maliyet, k)
		}
	}
	return dd.kaydet()
}

func (dd *DosyaDepo) Maliyet(sunucu string, an time.Time) (float64, float64) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	g, a := maliyetAnahtarlari(sunucu, an)
	return dd.d.Maliyet[g], dd.d.Maliyet[a]
}

func (dd *DosyaDepo) KonusmaEkle(k KonusmaKaydi) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	if dd.dizin == "" {
		dd.konusma = append(dd.konusma, k)
		return nil
	}
	b, err := json.Marshal(k)
	if err != nil {
		return err
	}
	yol := filepath.Join(dd.dizin, "konusma", k.Zaman.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(yol, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// KonusmaBuda: saklama süresi dolan gün dosyalarını siler.
func (dd *DosyaDepo) KonusmaBuda(simdi time.Time, gun int) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	esik := simdi.UTC().AddDate(0, 0, -gun)
	if dd.dizin == "" {
		dd.konusma = slices.DeleteFunc(dd.konusma, func(k KonusmaKaydi) bool { return k.Zaman.Before(esik) })
		return nil
	}
	esikAd := esik.Format("2006-01-02") + ".jsonl"
	girdiler, err := os.ReadDir(filepath.Join(dd.dizin, "konusma"))
	if err != nil {
		return err
	}
	for _, g := range girdiler {
		ad := g.Name()
		if strings.HasSuffix(ad, ".jsonl") && ad < esikAd {
			if err := os.Remove(filepath.Join(dd.dizin, "konusma", ad)); err != nil {
				return err
			}
		}
	}
	return nil
}

// konusmaSilKilitli: tg özetine ait satırları tüm gün dosyalarından çıkarır.
func (dd *DosyaDepo) konusmaSilKilitli(tg string) error {
	if dd.dizin == "" {
		dd.konusma = slices.DeleteFunc(dd.konusma, func(k KonusmaKaydi) bool { return k.Tg == tg })
		return nil
	}
	dir := filepath.Join(dd.dizin, "konusma")
	girdiler, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, g := range girdiler {
		if !strings.HasSuffix(g.Name(), ".jsonl") {
			continue
		}
		yol := filepath.Join(dir, g.Name())
		b, err := os.ReadFile(yol)
		if err != nil {
			return err
		}
		var kalan bytes.Buffer
		degisti := false
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var k KonusmaKaydi
			if json.Unmarshal(sc.Bytes(), &k) == nil && k.Tg == tg {
				degisti = true
				continue
			}
			kalan.Write(sc.Bytes())
			kalan.WriteByte('\n')
		}
		if err := sc.Err(); err != nil {
			return err
		}
		if degisti {
			if err := atomikYaz(yol, kalan.Bytes()); err != nil {
				return err
			}
		}
	}
	return nil
}

// konusmaBellek: bellek kipinde kayıtlar (test).
func (dd *DosyaDepo) konusmaBellek() []KonusmaKaydi {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	return slices.Clone(dd.konusma)
}
