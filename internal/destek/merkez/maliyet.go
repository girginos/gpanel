package merkez

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Bütçe hataları: hepsi "insan desteğine aktarıyorum" ile sonuçlanır.
var (
	ErrAcilKesici   = errors.New("acil kesici etkin")
	ErrOturumTavani = errors.New("oturum token tavanı")
	ErrSunucuButce  = errors.New("sunucu bütçesi aşıldı")
	ErrGenelButce   = errors.New("genel bütçe aşıldı")
	ErrFiyatYok     = errors.New("model fiyatı yapılandırmada yok")
)

// USD: kullanımın maliyeti. Cache yazma TTL'e göre ayrı fiyatlanır; usage.cache_creation
// kırılımı yoksa tüm yazma yapılandırılmış TTL'in fiyatıyla sayılır.
func (f Fiyat) USD(k Kullanim, ttl string) float64 {
	yazma5, yazma1 := k.CacheCreationInputTokens, int64(0)
	if k.CacheCreation != nil && (k.CacheCreation.Ephemeral5m+k.CacheCreation.Ephemeral1h) > 0 {
		yazma5, yazma1 = k.CacheCreation.Ephemeral5m, k.CacheCreation.Ephemeral1h
	} else if ttl == "1h" {
		yazma5, yazma1 = 0, k.CacheCreationInputTokens
	}
	t := float64(k.InputTokens)*f.GirisMTok +
		float64(k.OutputTokens)*f.CikisMTok +
		float64(yazma5)*f.CacheYazma5dkMTok +
		float64(yazma1)*f.CacheYazma1saMTok +
		float64(k.CacheReadInputTokens)*f.CacheOkumaMTok
	return t / 1e6
}

// ucretliEsdeger: oturum tavanı için token sayımı — önbellekten okunan token
// ~0.1× fiyatlandığından onda biri sayılır; böylece uzun araç döngüsünde her
// istekte yeniden okunan önek tavanı haksız yere tüketmez.
func ucretliEsdeger(k Kullanim) int64 {
	return k.InputTokens + k.CacheCreationInputTokens + k.OutputTokens + k.CacheReadInputTokens/10
}

// Defter: maliyet defteri + bütçe denetimi + cache isabet ölçümü.
type Defter struct {
	yap   *Yapilandirma
	depo  Depo
	log   *slog.Logger
	saat  func() time.Time
	cache *cacheOlcer
}

func yeniDefter(y *Yapilandirma, d Depo, log *slog.Logger, saat func() time.Time) *Defter {
	return &Defter{yap: y, depo: d, log: log, saat: saat, cache: yeniCacheOlcer(y.CacheUyariPencere, y.CacheUyariEsigi, log, saat)}
}

// Uygun: Claude çağrısından ÖNCE. nil değilse çağrı yapılmaz.
func (d *Defter) Uygun(sunucu string, oturumToken int64) error {
	if d.yap.AcilKesiciDosyasi != "" {
		if _, err := os.Stat(d.yap.AcilKesiciDosyasi); err == nil {
			return ErrAcilKesici
		}
	}
	b := d.yap.Butce
	if oturumToken >= b.OturumTokenTavani {
		return ErrOturumTavani
	}
	simdi := d.saat()
	gg, ga := d.depo.Maliyet("*", simdi)
	if gg >= b.GenelGunlukUSD || ga >= b.GenelAylikUSD {
		return ErrGenelButce
	}
	sg, sa := d.depo.Maliyet(sunucu, simdi)
	if sg >= b.SunucuGunlukUSD || sa >= b.SunucuAylikUSD {
		return ErrSunucuButce
	}
	return nil
}

// Kaydet: yanıt usage'ını deftere işler. anaModel → cache isabet ölçümüne girer.
func (d *Defter) Kaydet(sunucu, model string, k Kullanim, anaModel bool) (float64, error) {
	f, ok := d.yap.Fiyatlar[model]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrFiyatYok, model)
	}
	usd := f.USD(k, d.yap.CacheTTL)
	if err := d.depo.MaliyetEkle(sunucu, d.saat(), usd); err != nil {
		d.log.Error("maliyet defteri yazılamadı", "hata", err)
	}
	if anaModel {
		d.cache.Kaydet(model, k)
	}
	d.log.Info("claude kullanım", "sunucu", sunucu, "model", model,
		"giris", k.InputTokens, "cikis", k.OutputTokens,
		"cache_yazma", k.CacheCreationInputTokens, "cache_okuma", k.CacheReadInputTokens,
		"usd", fmt.Sprintf("%.6f", usd))
	return usd, nil
}

// ---- cache isabet oranı ----

type cacheOrnek struct{ okuma, toplam int64 }

type cacheOlcer struct {
	mu       sync.Mutex
	pencere  int
	esik     float64
	log      *slog.Logger
	saat     func() time.Time
	ornekler map[string][]cacheOrnek
	sonUyari map[string]time.Time
	uyariSay int
}

func yeniCacheOlcer(pencere int, esik float64, log *slog.Logger, saat func() time.Time) *cacheOlcer {
	if pencere <= 0 {
		pencere = 20
	}
	return &cacheOlcer{pencere: pencere, esik: esik, log: log, saat: saat, ornekler: map[string][]cacheOrnek{}, sonUyari: map[string]time.Time{}}
}

// Kaydet: oran = okuma / (okuma + yazma + önbelleksiz giriş), son N çağrı üzerinden.
// Pencere dolduktan sonra oran eşiğin altındaysa (10 dk'da en çok bir kez) UYARI log'u.
func (c *cacheOlcer) Kaydet(model string, k Kullanim) {
	c.mu.Lock()
	defer c.mu.Unlock()
	o := append(c.ornekler[model], cacheOrnek{okuma: k.CacheReadInputTokens, toplam: k.CacheReadInputTokens + k.CacheCreationInputTokens + k.InputTokens})
	if len(o) > c.pencere {
		o = o[len(o)-c.pencere:]
	}
	c.ornekler[model] = o
	if len(o) < c.pencere {
		return
	}
	oran := oranHesapla(o)
	simdi := c.saat()
	if oran < c.esik && simdi.Sub(c.sonUyari[model]) >= 10*time.Minute {
		c.sonUyari[model] = simdi
		c.uyariSay++
		c.log.Warn("prompt cache isabet oranı düşük — önek bozuluyor olabilir (ToolsJSON/SistemSabit/model değişimi?)",
			"model", model, "oran", fmt.Sprintf("%.3f", oran), "esik", c.esik, "pencere", len(o))
	}
}

func oranHesapla(o []cacheOrnek) float64 {
	var ok, top int64
	for _, x := range o {
		ok += x.okuma
		top += x.toplam
	}
	if top == 0 {
		return 1
	}
	return float64(ok) / float64(top)
}

// Durum: model → oran (metrik ucu).
func (c *cacheOlcer) Durum() map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]float64{}
	for m, o := range c.ornekler {
		out[m] = oranHesapla(o)
	}
	return out
}

func (c *cacheOlcer) uyariSayisi() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uyariSay
}
