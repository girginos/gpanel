// Package arac — Telegram destek botunun panel tarafı R0 araç yürütücüsü.
//
// Akış (her iş emri): yerel kimlik (KimlikBagla) ↔ oturum bağlamı tutarlılığı →
// protokol.AracGirdisiCoz (katı; rol/düzey/hesap aralığı) → hesap sıra no →
// domain_id + AÇIK kapsam denetimi (DB: alan adı eşleşmesi, bayi sahipliği,
// askı) → araç (kopuk bağlam + araç başına zaman aşımı) → maske → KesSatirlar →
// protokol.IsSonucu (Dogrula ile öz-denetim). Ham çıktı asla dönmez.
//
// Rota sarmalayan araçlar panelin süreç içi chi yönlendiricisine istek atar;
// kimlik middleware.DestekBagla context işaretiyle kurulur (başlıkla DEĞİL) ve
// middleware.DestekRotalari varsayılan-red izin listesinden geçer. Özel araçlar
// sabit argv ile komut çalıştırır; modelden gelen metin komut satırına girmez.
package arac

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"

	"girginospanel/internal/destek/kopru"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/maske"
	"girginospanel/internal/monitor"
)

// Yurutucu — ajanın (internal/destek/ajan) çağırdığı arayüz.
type Yurutucu interface {
	Yurut(ctx context.Context, b protokol.OturumBaglami, emir protokol.IsEmri) protokol.IsSonucu
}

// Kimlik — bağlantının YEREL kaydından (destek_baglantilari) kurulan panel
// kimliği. Ajan, Yurut'tan önce KimlikBagla ile ctx'e koyar; merkezden gelen hiçbir
// veri bu yapıya girmez.
type Kimlik struct {
	Rol protokol.Rol
	// KullaniciID — bayi/kök: users.id (bayide aynı zamanda kapsam anahtarı rid).
	// Müşteride 0.
	KullaniciID int64
	// Kullanici — bayi/kök users.username (kökte boşsa "root").
	Kullanici string
	// HesapIDleri — OturumBaglami.Hesaplar ile AYNI sıra: sıra n → HesapIDleri[n-1]
	// (domains.id). Müşteride tam 1 hesap.
	HesapIDleri []int64
	// TgKisa — denetim aktörü kısa kimliği ("destek-bot (tg:<kısa>)"), ^[A-Za-z0-9_-]{1,24}$.
	TgKisa string
}

type kimlikKey struct{}

// KimlikBagla — yerel kimliği ctx'e koyar (kopya; çağıranın dilimi sonradan değişse de etkilemez).
func KimlikBagla(ctx context.Context, k Kimlik) context.Context {
	k.HesapIDleri = slices.Clone(k.HesapIDleri)
	return context.WithValue(ctx, kimlikKey{}, &k)
}

func kimlikAl(ctx context.Context) (*Kimlik, bool) {
	k, ok := ctx.Value(kimlikKey{}).(*Kimlik)
	return k, ok && k != nil
}

// yurutucu — Yurutucu gerçeklemesi.
type yurutucu struct {
	db *sql.DB
	// gecit — panelin salt-okunur GET uçlarına erişim: panel sürecinde süreç içi
	// (kopru.Yerel), ayrı destek-ajan sürecinde özel köprü istemcisi (kopru.Istemci).
	gecit kopru.Gecit
	mask  *maske.Maskeleyici
	o     ortam
}

// Yeni — db: panel veritabanı; router: panelin kök http.Handler'ı (/api/v1
// rotaları dahil, RequireAuth grubu ile); panelSirlari: çıktıda birebir
// maskelenecek panel sırları (JWT sırrı, DB DSN/parolası, Redis yönetici parolası).
func Yeni(db *sql.DB, router http.Handler, panelSirlari []string) Yurutucu {
	return yeni(db, kopru.Yerel{Router: router}, panelSirlari, varsayilanOrtam())
}

// YeniKopruli — AYRI süreçteki destek ajanı için: panel uçlarına router yerine
// özel Unix soket köprüsü (internal/destek/kopru) üzerinden erişir. Köprü yoksa
// panel-bağımlı araçlar "panel_yanit_vermiyor" döner; panelden bağımsız araçlar
// (servis_durum, nginx_test, dogrula_ozet, log_oku(php_fpm/laravel/cron) …) etkilenmez.
func YeniKopruli(db *sql.DB, g kopru.Gecit, panelSirlari []string) Yurutucu {
	return yeni(db, g, panelSirlari, varsayilanOrtam())
}

// Dikisler — dış dünya dikişleri: uçtan uca testler (internal/destek/e2e) gerçek
// yürütücüyü kök komutu / runuser / ağ yoklaması çalıştırmadan sürsün diye. nil
// alan gerçek uygulamayı kullanır. Üretim kodu Yeni'yi çağırır; bunu ÇAĞIRMAZ.
type Dikisler struct {
	Komut       func(ctx context.Context, argv ...string) ([]byte, error)
	KiraciKomut func(ctx context.Context, sk string, argv ...string) ([]byte, error)
	Yokla       func(hedefURL string) monitor.DomainHealth
	Dig         func(ctx context.Context, sunucu, ad, tur string) ([]string, error)
}

// YeniDikisli — Yeni ile aynı yürütücü; yalnız dış dünya dikişleri değiştirilmiş.
func YeniDikisli(db *sql.DB, router http.Handler, panelSirlari []string, d Dikisler) Yurutucu {
	o := varsayilanOrtam()
	if d.Komut != nil {
		o.komut = d.Komut
	}
	if d.KiraciKomut != nil {
		o.kiraciKomut = d.KiraciKomut
	}
	if d.Yokla != nil {
		o.yokla = d.Yokla
	}
	if d.Dig != nil {
		o.dig = d.Dig
	}
	return yeni(db, kopru.Yerel{Router: router}, panelSirlari, o)
}

// YeniDikisliKopruli — YeniDikisli'nin köprülü karşılığı (ayrı süreç e2e testleri).
func YeniDikisliKopruli(db *sql.DB, g kopru.Gecit, panelSirlari []string, d Dikisler) Yurutucu {
	o := varsayilanOrtam()
	if d.Komut != nil {
		o.komut = d.Komut
	}
	if d.KiraciKomut != nil {
		o.kiraciKomut = d.KiraciKomut
	}
	if d.Yokla != nil {
		o.yokla = d.Yokla
	}
	if d.Dig != nil {
		o.dig = d.Dig
	}
	return yeni(db, g, panelSirlari, o)
}

func yeni(db *sql.DB, g kopru.Gecit, panelSirlari []string, o ortam) *yurutucu {
	return &yurutucu{
		db:    db,
		gecit: g,
		mask: maske.Yeni(maske.Secenek{
			BilinenSirlar: slices.Clone(panelSirlari),
			IPKirp:        true,
			EpostaMaskele: true,
			EvOnekSoy:     true,
		}),
		o: o,
	}
}

// aracHata — aracın döndürdüğü, merkeze gidecek kodlu hata. mesaj güvenli
// (kodun ürettiği ya da maskelenecek) kısa metindir.
type aracHata struct {
	kod   protokol.HataKodu
	mesaj string
}

func (e *aracHata) Error() string { return string(e.kod) + ": " + e.mesaj }

func hataf(kod protokol.HataKodu, mesaj string) error { return &aracHata{kod: kod, mesaj: mesaj} }

// Yurut — tek iş emrini yürütür. Asla panik yaymaz; her yol bir IsSonucu döner.
func (y *yurutucu) Yurut(ctx context.Context, b protokol.OturumBaglami, emir protokol.IsEmri) (s protokol.IsSonucu) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("destek/arac: %s aracında panik: %v", emir.Arac, r)
			s = y.hataSonucu(emir.IsKodu, hataf(protokol.HataIc, "araç çalıştırılamadı"))
		}
	}()
	c, err := y.hazirla(ctx, b, emir)
	if err != nil {
		return y.hataSonucu(emir.IsKodu, err)
	}
	t := araclar[c.g.Arac]
	actx, iptal := context.WithTimeout(context.WithoutCancel(ctx), t.sure)
	defer iptal()
	out, err := t.fn(actx, c)
	if err != nil {
		if errors.Is(actx.Err(), context.DeadlineExceeded) {
			err = hataf(protokol.HataZamanAsimi, "araç süresi doldu")
		}
		return y.hataSonucu(emir.IsKodu, err)
	}
	return y.sonucKur(emir.IsKodu, out)
}

// hazirla — kimlik, girdi ve hesap kapsamı. Araç çalışmadan önceki TÜM ret yolları burada.
func (y *yurutucu) hazirla(ctx context.Context, b protokol.OturumBaglami, emir protokol.IsEmri) (*cagri, error) {
	k, ok := kimlikAl(ctx)
	if !ok {
		return nil, hataf(protokol.HataYetki, "destek kimliği yok")
	}
	if k.Rol != b.Rol {
		return nil, hataf(protokol.HataYetki, "oturum rolü yerel kimlikle uyuşmuyor")
	}
	if len(k.HesapIDleri) != len(b.Hesaplar) {
		return nil, hataf(protokol.HataKapsam, "oturum hesap listesi yerel kimlikle uyuşmuyor")
	}
	if k.Rol == protokol.RolMusteri && len(k.HesapIDleri) != 1 {
		return nil, hataf(protokol.HataKapsam, "müşteri bağlantısı tek hesaplıdır")
	}
	g, err := protokol.AracGirdisiCoz(b, emir.Arac, emir.Param)
	if err != nil {
		return nil, err
	}
	if emir.Risk != g.Risk {
		return nil, hataf(protokol.HataParam, "iş emri riski katalogla uyuşmuyor")
	}
	if _, ok := araclar[g.Arac]; !ok {
		return nil, hataf(protokol.HataAracBilinmiyor, "araç bu panelde uygulanmamış")
	}
	if err := y.kullaniciDogrula(ctx, k); err != nil {
		return nil, err
	}
	c := &cagri{y: y, k: k, b: b, g: g}
	if n, ok := g.Tamsayi("hesap"); ok {
		i := int(n) - 1 // AracGirdisiCoz 1..len aralığını zorladı
		h, err := y.hesapKapsam(ctx, k, k.HesapIDleri[i], b.Hesaplar[i].Etiket)
		if err != nil {
			return nil, err
		}
		c.h = h
	}
	return c, nil
}

// sonucKur — maske → KesSatirlar → protokol sınırları → öz-denetim.
func (y *yurutucu) sonucKur(isKodu string, out *cikti) protokol.IsSonucu {
	if out == nil {
		out = &cikti{}
	}
	ham := make([]string, len(out.satirlar))
	for i, s := range out.satirlar {
		ham[i] = ansiSoy(s)
	}
	kesildi := out.kesildi || len(ham) > protokol.SatirSayisiSiniri
	satirlar := maske.KesSatirlar(y.mask.Satirlar(ham), protokol.SatirSayisiSiniri, 0)
	for i, s := range satirlar {
		t, k := protokol.SatirTemizle(s)
		satirlar[i] = t
		kesildi = kesildi || k
	}
	ozet, _ := baytKes(y.mask.Satir(maske.TekSatir(ansiSoy(out.ozet))), protokol.OzetSiniri)
	h := sha256.New()
	h.Write([]byte(ozet))
	for _, s := range satirlar {
		h.Write([]byte{0})
		h.Write([]byte(s))
	}
	s := protokol.IsSonucu{
		IsKodu:   isKodu,
		Ok:       true,
		Ozet:     ozet,
		Satirlar: satirlar,
		Kesildi:  kesildi,
		Kanit:    "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
	if err := s.Dogrula(); err != nil {
		log.Printf("destek/arac: sonuç öz-denetimi düştü (%s): %v", isKodu, err)
		return y.hataSonucu(isKodu, hataf(protokol.HataIc, "sonuç biçimi"))
	}
	return s
}

// hataSonucu — kodlu hata sonucu. Bilinmeyen (yabancı) hatanın metni dışarı çıkmaz.
func (y *yurutucu) hataSonucu(isKodu string, err error) protokol.IsSonucu {
	kod, mesaj := protokol.HataIc, "iç hata"
	var ah *aracHata
	var ph *protokol.Hata
	switch {
	case errors.As(err, &ah):
		kod, mesaj = ah.kod, ah.mesaj
	case errors.As(err, &ph):
		kod, mesaj = ph.Kod, ph.Ayrinti
	case errors.Is(err, context.DeadlineExceeded):
		kod, mesaj = protokol.HataZamanAsimi, "araç süresi doldu"
	default:
		log.Printf("destek/arac: iç hata (%s): %v", isKodu, err)
	}
	if !kod.Gecerli() {
		kod = protokol.HataIc
	}
	ozet, _ := baytKes(y.mask.Satir(maske.TekSatir(mesaj)), protokol.OzetSiniri)
	return protokol.IsSonucu{IsKodu: isKodu, Ok: false, Ozet: ozet, Satirlar: []string{}, HataKodu: kod}
}

// baytKes — protokol metin kuralı (yasak karakter → '?') + en çok n bayt.
func baytKes(s string, n int) (string, bool) {
	var b strings.Builder
	kesildi := false
	for _, r := range strings.ToValidUTF8(s, "?") {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) ||
			(r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f || r == 0xfffd {
			r = '?'
		}
		if b.Len()+len(string(r)) > n {
			kesildi = true
			break
		}
		b.WriteRune(r)
	}
	return b.String(), kesildi
}

// ansiSoy — renk/denetim kaçış dizilerini (ESC[…m) siler; maske kontrol
// karakterini boşluğa çevirse de "[33m" artığı kalırdı.
func ansiSoy(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	return ansiRe.ReplaceAllString(s, "")
}
