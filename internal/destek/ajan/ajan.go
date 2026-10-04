package ajan

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
	"girginospanel/internal/lisans"
	"girginospanel/internal/maske"
	"girginospanel/internal/surum"
)

// PanelSurumu: kayit.panel_surumu (testte değiştirilebilir).
var PanelSurumu = surum.Panel

// lisansParmakIzi: kayit.lisans_parmak_izi (64 küçük hex; testte değiştirilebilir).
var lisansParmakIzi = lisans.Parmakizi

// Yurutucu: araç yürütücüsü (internal/destek/arac). Arayüz sözleşmesi:
// arac paketindeki Yurutucu ile AYNI yöntem kümesi — main.go o paketin değerini atar.
// Ajan çağırmadan ÖNCE imza, bağlantı, düzey ve protokol.AracGirdisiCoz denetimlerini
// yapmıştır; ctx'te baglam.Kapsam vardır (yerel kapsam). Dönen sonuç ajan tarafından
// yeniden maskelenir ve kesilir.
type Yurutucu interface {
	Yurut(ctx context.Context, b p.OturumBaglami, emir p.IsEmri) p.IsSonucu
}

// Denetim eylem kodları (literal sabitler: denetim kataloğu tarayıcısı çözebilsin).
const (
	dEslesmePortal = "destek.eslesme_portal"
	dBaglantiIptal = "destek.baglanti_iptal"
	dAyar          = "destek.ayar"
	dR0Ozet        = "destek.r0_ozet"
	dIsRed         = "destek.is_red"
)

// Bağlantı durumları (arayüz).
const (
	BagKapali       = "kapali"
	BagYapilandirma = "yapilandirma_eksik"
	BagBaglaniyor   = "baglaniyor"
	BagBagli        = "bagli"
	BagHata         = "hata"
	// BagAjanYok: ajan ayrı hizmettir; nabzı yoksa/bayatsa panel bunu gösterir.
	BagAjanYok = "ajan_yok"
)

// Durum: merkez bağlantısının anlık hali (arayüz için; sır taşımaz).
type Durum struct {
	Durum         string `json:"durum"`
	Tasima        string `json:"tasima"`
	SonHata       string `json:"son_hata"`
	SonBaglanti   int64  `json:"son_baglanti"`
	SunucuKimligi string `json:"sunucu_kimligi"`
	CihazKimligi  string `json:"cihaz_kimligi"`
}

// Ajan: panel içi destek ajanı. Yeni ile kurulur, Baslat ile çalışır.
type Ajan struct {
	DB            *sql.DB
	Yap           Yapilandirma
	Yurutucu      Yurutucu
	BilinenSirlar []string
	Saat          func() time.Time
	TLS           *tls.Config // nil → sistem kökleri (test: kendi kökü)
	// IsZamanAsimi: tek iş emrinin yürütme sınırı.
	IsZamanAsimi time.Duration
	// ModulKontrolAraligi: oturum sırasında şalterin yeniden okunma aralığı.
	ModulKontrolAraligi time.Duration
	// BiletYenilemeAraligi: açık oturum bu sürede bir kapatılıp yeniden kurulur — kayıtta gönderilen
	// lisans bileti (merkez doğrulamasının süresini belirler) diskteki taze biletle tazelensin diye.
	// Lisans nabzı her ~6 saatte bileti yeniler; uzun ömürlü bağlantı eski biletle doğrulamasını
	// yitirmesin.
	BiletYenilemeAraligi time.Duration
	// DBYoklamaAraligi: panelin DB'ye yazdığı değişikliklerin (ayar, panel tarafı iptal)
	// ajana ulaşma aralığı. Panel ve ajan AYRI süreçtir; aralarındaki tek bağ DB'dir.
	DBYoklamaAraligi time.Duration
	// NabizAraligi: destek_ajan_nabiz satırının yazılma aralığı (panel durum sayfası canlılığı).
	NabizAraligi time.Duration
	// KopruPing: panel köprüsünün canlılık yoklaması (nil → nabızda köprü durumu boş).
	KopruPing func(ctx context.Context) error

	dogrulayici *p.Dogrulayici
	cihaz       *p.AnahtarCifti
	maske       *maske.Maskeleyici
	durt        chan struct{}
	isSem       chan struct{}

	mu         sync.Mutex
	durum      Durum
	aktif      *oturum
	hesapIzi   map[string]string
	baslatildi bool

	// DB yoklama durumu (yalnız izleme döngüsü yazar).
	ayarIzi       string
	ayarHazir     bool
	iptalSutunYok atomic.Bool
	basladiAt     time.Time
}

// Yeni: ajanı kurar (çalıştırmaz). Panel süreci de Yeni ile kurar ama Baslat ÇAĞIRMAZ:
// panel yalnız UI uçlarını (Rotalar) ve DB'ye yazan iptal kancasını (PanelKancasiKur) kullanır;
// ajanı ayrı süreç (cmd/destek-ajan, girginospanel-destek-ajan.service) çalıştırır.
func Yeni(db *sql.DB, yap Yapilandirma) *Ajan {
	a := &Ajan{DB: db, Yap: yap, Saat: time.Now, IsZamanAsimi: 90 * time.Second, ModulKontrolAraligi: 15 * time.Second, BiletYenilemeAraligi: 12 * time.Hour,
		DBYoklamaAraligi: 3 * time.Second, NabizAraligi: 10 * time.Second}
	a.durt = make(chan struct{}, 1)
	a.isSem = make(chan struct{}, 4)
	a.hesapIzi = map[string]string{}
	a.durum = Durum{Durum: BagKapali}
	return a
}

// Baslat: cihaz kimliği + doğrulayıcı + döngüler (merkez bağlantısı, süpürge, DB yoklama,
// nabız). YALNIZ ayrı destek-ajan sürecinde çağrılır. Hata olsa da süreç çalışmaya devam eder.
func (a *Ajan) Baslat(ctx context.Context) {
	a.mu.Lock()
	if a.baslatildi {
		a.mu.Unlock()
		return
	}
	a.baslatildi = true
	a.mu.Unlock()
	a.maske = maske.Yeni(maske.Secenek{BilinenSirlar: a.BilinenSirlar, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true})
	a.basladiAt = a.Saat()
	if len(a.Yap.MerkezAnahtarlari) > 0 {
		d, err := p.YeniDogrulayici(func() time.Time { return a.Saat() }, p.YeniBellekOnbellek(0), a.Yap.MerkezAnahtarlari...)
		if err != nil {
			log.Printf("destek: doğrulayıcı kurulamadı: %v", err)
		} else {
			a.dogrulayici = d
		}
	}
	go a.korunmus(ctx, "bağlantı", a.dongu)
	go a.korunmus(ctx, "süpürge", a.supurgeDongusu)
	go a.korunmus(ctx, "db-yoklama", a.dbYoklamaDongusu)
	go a.korunmus(ctx, "nabız", a.nabizDongusu)
}

// YurutucuAyarla: araç yürütücüsünü (sonradan, döngü çalışırken) bağlar.
func (a *Ajan) YurutucuAyarla(y Yurutucu) {
	a.mu.Lock()
	a.Yurutucu = y
	a.mu.Unlock()
}

func (a *Ajan) yurutucu() Yurutucu {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Yurutucu
}

// Durt: ayar değişti — döngüyü hemen uyandır.
func (a *Ajan) Durt() {
	select {
	case a.durt <- struct{}{}:
	default:
	}
}

// DurumAl: bağlantı durumunun kopyası.
func (a *Ajan) DurumAl() Durum {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.durum
	if a.cihaz != nil {
		d.CihazKimligi = a.cihaz.Kimlik
	}
	return d
}

func (a *Ajan) durumYaz(f func(d *Durum)) {
	a.mu.Lock()
	f(&a.durum)
	a.mu.Unlock()
}

// korunmus: panik panel sürecini DÜŞÜRMEZ; döngü 30 sn sonra yeniden başlar.
func (a *Ajan) korunmus(ctx context.Context, ad string, fn func(context.Context)) {
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("🔴 destek %s döngüsünde panik (yeniden başlatılacak): %v", ad, r)
					a.durumYaz(func(d *Durum) { d.Durum, d.SonHata = BagHata, "iç hata (panik)" })
				}
			}()
			fn(ctx)
		}()
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (a *Ajan) bekle(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-a.durt:
		return true
	case <-t.C:
		return true
	}
}

// geriCekilme: üstel (2 sn → 5 dk) + tam titreşim [taban/2, taban].
func geriCekilme(deneme int) time.Duration {
	taban := 2 * time.Second
	for i := 1; i < deneme && taban < 5*time.Minute; i++ {
		taban *= 2
	}
	if taban > 5*time.Minute {
		taban = 5 * time.Minute
	}
	yarim := int64(taban / 2)
	n, err := rand.Int(rand.Reader, big.NewInt(yarim+1))
	if err != nil {
		return taban
	}
	return time.Duration(yarim + n.Int64())
}

// kayitRed: merkez kaydı reddetti.
type kayitRed struct{ kod p.HataKodu }

func (k *kayitRed) Error() string { return "merkez kaydı reddetti: " + string(k.kod) }

func (a *Ajan) dongu(ctx context.Context) {
	deneme := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if !modulAcik(ctx, a.DB) {
			deneme = 0
			a.durumYaz(func(d *Durum) { d.Durum, d.Tasima, d.SonHata = BagKapali, "", "" })
			if !a.bekle(ctx, 30*time.Second) {
				return
			}
			continue
		}
		if a.cihaz == nil {
			c, err := cihazAnahtariYukle(a.Yap.CihazAnahtarYolu)
			if err != nil {
				log.Printf("destek: %v", err)
				a.durumYaz(func(d *Durum) { d.Durum, d.SonHata = BagYapilandirma, err.Error() })
				if !a.bekle(ctx, 5*time.Minute) {
					return
				}
				continue
			}
			a.mu.Lock()
			a.cihaz = c
			a.mu.Unlock()
		}
		if a.dogrulayici == nil {
			a.durumYaz(func(d *Durum) { d.Durum, d.SonHata = BagYapilandirma, "merkez açık anahtarı yapılandırılmamış" })
			if !a.bekle(ctx, 5*time.Minute) {
				return
			}
			continue
		}
		basla := time.Now()
		a.durumYaz(func(d *Durum) { d.Durum = BagBaglaniyor })
		err := a.oturumCalistir(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(basla) > 2*time.Minute {
			deneme = 0
		}
		deneme++
		bekleme := geriCekilme(deneme)
		var kr *kayitRed
		if errors.As(err, &kr) && (kr.kod == p.HataLisans || kr.kod == p.HataSurum) {
			bekleme = 30 * time.Minute
		}
		if err != nil {
			msg := err.Error()
			log.Printf("destek: merkez oturumu bitti: %s (sonraki deneme %s)", msg, bekleme.Round(time.Second))
			a.durumYaz(func(d *Durum) { d.Durum, d.SonHata = BagHata, kesRune(msg, 200) })
		}
		if !a.bekle(ctx, bekleme) {
			return
		}
	}
}

func mesajKimligi() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// tasimaKur: önce WSS; el sıkışma başarısızsa uzun yoklama.
func (a *Ajan) tasimaKur(ctx context.Context) (tasima, error) {
	wsU, taban, err := a.Yap.adresler()
	if err != nil {
		return nil, err
	}
	if !a.Yap.YoklamaZorla {
		dctx, iptal := context.WithTimeout(ctx, 20*time.Second)
		w, err := wsBaglan(dctx, wsU, p.WSAltProtokol, a.TLS, p.CerceveSiniri)
		iptal()
		if err == nil {
			return &wsTasima{w: w}, nil
		}
		log.Printf("destek: WSS kurulamadı (%v) — uzun yoklamaya geçiliyor", err)
	}
	return yeniYoklama(taban, a.TLS), nil
}

// oturum: kurulmuş (kayıt kabul edilmiş) merkez oturumu.
type oturum struct {
	t     tasima
	giden chan p.Govde
	iptal context.CancelFunc
}

// gonder: kuyruğa koyar (engellemez); kuyruk doluysa düşer — yeniden bağlanınca senkron tamamlar.
func (o *oturum) gonder(g p.Govde) {
	select {
	case o.giden <- g:
	default:
		log.Printf("destek: giden kuyruk dolu — %s düştü", g.Tip())
	}
}

func (o *oturum) hata(ilgili string, err error) {
	h := &p.HataMesaji{Kod: p.HataKoduAl(err)}
	if ilgili != "" {
		h.Ilgili = ilgili
	}
	if a, _ := p.SatirTemizle(err.Error()); a != "" {
		h.Ayrinti = kesRune(a, 200)
	}
	o.gonder(h)
}

func (a *Ajan) oturumCalistir(ctx context.Context) error {
	sctx, iptal := context.WithCancel(ctx)
	defer iptal()
	t, err := a.tasimaKur(sctx)
	if err != nil {
		return err
	}
	go func() { <-sctx.Done(); _ = t.Kapat() }()

	kay := &p.Kayit{
		CihazAnahtari:   p.AcikAnahtarKodla(a.cihaz.Acik),
		LisansParmakIzi: a.Yap.parmakIzi(),
		LisansIspati:    base64.RawURLEncoding.EncodeToString(a.Yap.lisansIspati()),
		PanelSurumu:     PanelSurumu,
		KatalogOzeti:    p.KatalogOzeti(),
	}
	if err := a.dogrudanGonder(sctx, t, kay); err != nil {
		return fmt.Errorf("kayıt gönderilemedi: %w", err)
	}
	g, err := a.bekleyenAl(sctx, t)
	if err != nil {
		return fmt.Errorf("meydan okuma alınamadı: %w", err)
	}
	m, ok := g.(*p.MeydanOkuma)
	if !ok {
		if h, ok := g.(*p.HataMesaji); ok {
			return &kayitRed{kod: h.Kod}
		}
		// Merkez kaydı meydan okumadan önce de reddedebilir (lisans/sürüm).
		if ks, ok := g.(*p.KayitSonuc); ok && !ks.Kabul {
			return &kayitRed{kod: ks.HataKodu}
		}
		return fmt.Errorf("meydan okuma yerine %s geldi", g.Tip())
	}
	// 🔴 Merkez Cloudflare arkasında: TLS merkezi kanıtlamaz. Meydan okuma panelde
	// SABİT merkez anahtarıyla doğrulanmadan cihaz imzası verilmez.
	if err := a.dogrulayici.MeydanDogrula(m, kay); err != nil {
		return fmt.Errorf("meydan okuma doğrulanamadı: %w", err)
	}
	y, err := p.MeydanYanitla(a.cihaz.Ozel, m, kay)
	if err != nil {
		return err
	}
	if err := a.dogrudanGonder(sctx, t, y); err != nil {
		return err
	}
	g, err = a.bekleyenAl(sctx, t)
	if err != nil {
		return fmt.Errorf("kayıt sonucu alınamadı: %w", err)
	}
	ks, ok := g.(*p.KayitSonuc)
	if !ok {
		if h, ok := g.(*p.HataMesaji); ok {
			return &kayitRed{kod: h.Kod}
		}
		return fmt.Errorf("kayıt sonucu yerine %s geldi", g.Tip())
	}
	if !ks.Kabul {
		return &kayitRed{kod: ks.HataKodu}
	}

	o := &oturum{t: t, giden: make(chan p.Govde, 256), iptal: iptal}
	a.mu.Lock()
	a.aktif = o
	a.hesapIzi = map[string]string{}
	a.durum = Durum{Durum: BagBagli, Tasima: t.Tur(), SonBaglanti: a.Saat().Unix(), SunucuKimligi: ks.SunucuKimligi}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.aktif == o {
			a.aktif = nil
		}
		a.mu.Unlock()
	}()
	log.Printf("destek: merkeze bağlanıldı (%s, sunucu %s)", t.Tur(), ks.SunucuKimligi)

	yazHata := make(chan error, 1)
	go func() { // yazıcı: tek sıra
		for {
			select {
			case <-sctx.Done():
				return
			case g := <-o.giden:
				if err := a.dogrudanGonder(sctx, t, g); err != nil {
					select {
					case yazHata <- err:
					default:
					}
					iptal()
					return
				}
			}
		}
	}()
	kalp := time.Duration(ks.KalpAraligiSn) * time.Second
	go func() {
		tk := time.NewTicker(kalp)
		defer tk.Stop()
		modTk := time.NewTicker(a.ModulKontrolAraligi)
		defer modTk.Stop()
		yenileTk := time.NewTicker(a.BiletYenilemeAraligi)
		defer yenileTk.Stop()
		var sira uint64
		for {
			select {
			case <-sctx.Done():
				return
			case <-tk.C:
				sira++
				o.gonder(&p.KalpAtisi{Sira: sira, Zaman: a.Saat().Unix()})
			case <-yenileTk.C:
				log.Printf("destek: lisans bileti tazelemesi için merkez oturumu yeniden kuruluyor")
				iptal()
				return
			case <-modTk.C:
				if !modulAcik(sctx, a.DB) {
					log.Printf("destek: modül kapatıldı — merkez bağlantısı kesiliyor")
					iptal()
					return
				}
			}
		}
	}()
	a.senkron(sctx, o)

	for {
		veri, err := t.Al(sctx)
		if err != nil {
			select {
			case e := <-yazHata:
				return fmt.Errorf("yazma: %w", e)
			default:
			}
			if sctx.Err() != nil && ctx.Err() == nil {
				return errors.New("oturum kapatıldı")
			}
			return err
		}
		if veri == nil {
			continue
		}
		a.mesajIsle(sctx, o, veri)
	}
}

func (a *Ajan) dogrudanGonder(ctx context.Context, t tasima, g p.Govde) error {
	b, err := p.Kodla(mesajKimligi(), g)
	if err != nil {
		return err
	}
	return t.Gonder(ctx, b)
}

// bekleyenAl: el sıkışma sırasında sıradaki gövde (30 sn içinde).
func (a *Ajan) bekleyenAl(ctx context.Context, t tasima) (p.Govde, error) {
	sinir := time.Now().Add(30 * time.Second)
	for time.Now().Before(sinir) {
		veri, err := t.Al(ctx)
		if err != nil {
			return nil, err
		}
		if veri == nil {
			continue
		}
		_, g, err := p.Coz(veri, p.YonAjana)
		if err != nil {
			return nil, err
		}
		if _, kalp := g.(*p.KalpAtisi); kalp {
			continue
		}
		return g, nil
	}
	return nil, errors.New("zaman aşımı")
}

// bildir: oturum varsa merkeze gönderir (yoksa yeniden bağlanınca senkron tamamlar).
func (a *Ajan) bildir(g p.Govde) bool {
	a.mu.Lock()
	o := a.aktif
	a.mu.Unlock()
	if o == nil {
		return false
	}
	o.gonder(g)
	return true
}

// Bagli: merkez oturumu kurulu mu.
func (a *Ajan) Bagli() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.aktif != nil
}

func (a *Ajan) mesajIsle(ctx context.Context, o *oturum, veri []byte) {
	z, g, err := p.Coz(veri, p.YonAjana)
	if err != nil {
		o.hata("", err)
		return
	}
	switch m := g.(type) {
	case *p.KalpAtisi:
	case *p.EslesmeOnayla:
		a.eslesmeOnayla(ctx, o, z.Kimlik, m)
	case *p.BaglantiIptal:
		a.merkezIptal(ctx, m)
	case *p.IsEmri:
		a.isEmriKabul(ctx, o, m)
	case *p.HataMesaji:
		log.Printf("destek: merkez hatası %s (ilgili %s)", m.Kod, m.Ilgili)
	default:
		log.Printf("destek: oturumda beklenmeyen mesaj %s", g.Tip())
	}
}

// senkron: bağlantı (yeniden) kurulunca merkezin bilmesi gereken durum.
func (a *Ajan) senkron(ctx context.Context, o *oturum) {
	bs, err := baglantilarSorgu(ctx, a.DB, `durum IN ('bekliyor','aktif') ORDER BY id LIMIT 1000`)
	if err != nil {
		if !iptal.TabloYok(err) {
			log.Printf("destek senkron: %v", err)
		}
		return
	}
	for _, b := range bs {
		a.durumBildir(ctx, b)
	}
	sonIptal, err := baglantilarSorgu(ctx, a.DB, `durum='iptal' AND iptal_at >= NOW() - INTERVAL 7 DAY ORDER BY id LIMIT 1000`)
	if err == nil {
		for _, b := range sonIptal {
			o.gonder(&p.BaglantiIptal{BaglantiID: b.Kimlik, Neden: iptalNedeniProtokol(b.IptalNedeni)})
			a.hubBildirildiIsaretle(ctx, b.ID) // yoklama döngüsü aynı iptali ikinci kez yollamasın
		}
	}
}

func iptalNedeniProtokol(s string) p.IptalNedeni {
	n := p.IptalNedeni(s)
	if (&p.BaglantiIptal{BaglantiID: "xxxxxxxx", Neden: n}).Dogrula() == nil {
		return n
	}
	return p.IptalKullanici
}

// durumBildir: bağlantının güncel durumunu (etkin düzey + hesap listesi) merkeze yollar
// ve gönderilen hesap listesinin özetini saklar (iş emrinde liste değişimi denetimi).
func (a *Ajan) durumBildir(ctx context.Context, b *baglanti) {
	bd := &p.BaglantiDurum{BaglantiID: b.Kimlik, KodOzet: b.KodOzet, TgUserID: b.TgUserID, Durum: durumProtokol(b.Durum),
		Rol: b.Rol, Duzey: p.DuzeyKapali, Hesaplar: []p.HesapEtiketi{}}
	if b.Durum == durumAktif {
		bd.Duzey = EtkinDuzey(ctx, a.DB, b.Rol, b.ResellerID, b.Duzey)
		hs, err := hesaplarOku(ctx, a.DB, b)
		if err != nil {
			log.Printf("destek: hesap listesi okunamadı (%s): %v", b.Kimlik, err)
			return
		}
		bd.Hesaplar = protokolHesaplari(hs)
		a.mu.Lock()
		a.hesapIzi[b.Kimlik] = hesapListesiOzeti(hs)
		a.mu.Unlock()
	}
	if err := bd.Dogrula(); err != nil {
		log.Printf("destek: bağlantı durumu biçimsiz (%s): %v", b.Kimlik, err)
		return
	}
	a.bildir(bd)
}

// ---- merkezden gelen eşleştirme / iptal ----

// Portal eşleştirmesi sınırları (sunucu sahibi = kök; bayi/müşteri eşleştirmesi YOK).
const (
	aktifBaglantiSiniri = 20 // sunucudaki toplam aktif destek bağlantısı
	portalSahipOneki    = "portal:"
)

// kokYonetici: kök bağlantıların sahibi — panelin etkin ilk yöneticisi (users.role='admin').
// Bağlantının kimlik izi (parola özeti, 2FA) bu kullanıcıdan alınır; parolası değişirse ya da
// hesabı askıya alınırsa bağlantı mevcut süpürge/iz denetimiyle kendiliğinden kapanır.
func (a *Ajan) kokYonetici(ctx context.Context) (int64, error) {
	var id int64
	err := a.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE role='admin' AND COALESCE(status,'active')='active' ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

// eslesmeOnayla: merkezin İMZALI v2 onayı (portal eşleştirmesi). Yerel kod YOKTUR: yetki,
// imzayla kapsanan düzey/süre/portal onayından gelir. Bağlantı DOĞRUDAN etkin ve rol=kök açılır;
// panelde ikinci onay adımı yoktur (sunucu sahibi portalda seçip aydınlatmayı kabul etti).
func (a *Ajan) eslesmeOnayla(ctx context.Context, o *oturum, zk string, m *p.EslesmeOnayla) {
	if err := a.dogrulayici.Dogrula(m); err != nil {
		o.hata(zk, err)
		return
	}
	// Başka sunucuya yönlendirilmiş (ya da bu oturumun kimliğiyle uyuşmayan) onay reddedilir.
	if kim := a.DurumAl().SunucuKimligi; kim == "" || kim != m.SunucuKimligi {
		o.hata(zk, &p.Hata{Kod: p.HataYetki, Ayrinti: "onay başka sunucu için"})
		return
	}
	if Tavan(ctx, a.DB, p.RolKok, 0) == p.DuzeyKapali {
		o.hata(zk, &p.Hata{Kod: p.HataDevreDisi})
		return
	}
	var aktif int
	_ = a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM destek_baglantilari WHERE durum IN ('aktif','bekliyor')`).Scan(&aktif)
	if aktif >= aktifBaglantiSiniri {
		o.hata(zk, &p.Hata{Kod: p.HataKota, Ayrinti: "aktif bağlantı sınırı"})
		return
	}
	// Kod TEK KULLANIMLIKTIR: aynı kod özetiyle ikinci kayıt (yeniden iletim, başka Telegram
	// kullanıcısı) açılmaz.
	var var_ int
	if a.DB.QueryRowContext(ctx, `SELECT 1 FROM destek_baglantilari WHERE kod_ozet=? LIMIT 1`, m.KodOzet).Scan(&var_) == nil {
		o.hata(zk, &p.Hata{Kod: p.HataEslesmeKodu, Ayrinti: "kod daha önce kullanılmış"})
		return
	}
	yonetici, err := a.kokYonetici(ctx)
	if err != nil {
		log.Printf("destek: kök yönetici bulunamadı: %v", err)
		o.hata(zk, &p.Hata{Kod: p.HataDevreDisi})
		return
	}
	kd, err := kimlikIzi(ctx, a.DB, p.RolKok, 0, 0, yonetici)
	if err != nil || kd.neden != "" {
		o.hata(zk, &p.Hata{Kod: p.HataDevreDisi})
		return
	}
	kimlik, err := yeniKimlik()
	if err != nil {
		o.hata(zk, &p.Hata{Kod: p.HataIc})
		return
	}
	sahip := kesRune(portalSahipOneki+m.PortalKullanici, 64)
	_, err = a.DB.ExecContext(ctx, `INSERT INTO destek_baglantilari (kimlik, kod_ozet, tg_user_id, tg_kullanici_adi, rol, domain_id,
		ftp_hesap_id, reseller_id, panel_user_id, duzey, sure_gun, bitis, durum, olusturan, onaylayan, onay_at, aydinlatma_surum,
		aydinlatma_zamani, durum_izi, iki_fa) VALUES (?,?,?,?,'kok',NULL,NULL,0,?,?,?,CURRENT_TIMESTAMP + INTERVAL ? DAY,'aktif',?,?,
		CURRENT_TIMESTAMP,?,CURRENT_TIMESTAMP,?,?)`,
		kimlik, m.KodOzet, m.TgUserID, m.TgKullaniciAdi, yonetici, string(m.Duzey), m.SureGun, m.SureGun, sahip, sahip,
		kesRune(m.AydinlatmaSurum, 32), kd.iz, kd.ikiFA)
	if err != nil {
		log.Printf("destek: portal bağlantısı yazılamadı: %v", err)
		o.hata(zk, &p.Hata{Kod: p.HataIc})
		return
	}
	b, err := baglantiKimlikle(ctx, a.DB, kimlik)
	if err != nil {
		o.hata(zk, &p.Hata{Kod: p.HataIc})
		return
	}
	a.denetimBot(ctx, b, dEslesmePortal, fmt.Sprintf("düzey=%s gün=%d aydınlatma=%s kaynak=portal", m.Duzey, m.SureGun, kesRune(m.AydinlatmaSurum, 32)), true)
	a.durumBildir(ctx, b)
}

func (a *Ajan) merkezIptal(ctx context.Context, m *p.BaglantiIptal) {
	b, err := baglantiKimlikle(ctx, a.DB, m.BaglantiID)
	if err != nil || b.Durum == durumIptal {
		return
	}
	if iptal.Tek(ctx, a.DB, b.ID, m.Neden, "merkez") {
		a.denetimBot(ctx, b, dBaglantiIptal, "neden="+string(m.Neden)+" kaynak=telegram", true)
		a.temizleIz(b.Kimlik)
		a.hubBildirildiIsaretle(ctx, b.ID) // merkez zaten biliyor
	}
}

func (a *Ajan) temizleIz(kimlik string) {
	a.mu.Lock()
	delete(a.hesapIzi, kimlik)
	a.mu.Unlock()
}

// otomatikIptal: kimlik izi/süre nedeniyle iptal + denetim + merkeze bildirim.
func (a *Ajan) otomatikIptal(ctx context.Context, b *baglanti, neden p.IptalNedeni) {
	if iptal.Tek(ctx, a.DB, b.ID, neden, "sistem") {
		a.denetimSistem(ctx, b, dBaglantiIptal, "neden="+string(neden)+" kaynak=otomatik", true)
		a.temizleIz(b.Kimlik)
		if a.bildir(&p.BaglantiIptal{BaglantiID: b.Kimlik, Neden: neden}) {
			a.hubBildirildiIsaretle(ctx, b.ID)
		}
	}
}

func hedefTg(ad string, id int64) string { return "tg:" + tgKisa(ad, id) }

// denetimBot: aktörü "destek-bot (tg:<kısa>)" olan denetim kaydı.
func (a *Ajan) denetimBot(ctx context.Context, b *baglanti, eylem, detay string, ok bool) {
	httpx.DenetimSistem(a.DB, 0, botAktor(b), eylem, a.hedefAdi(ctx, b), detay, b.ResellerID, ok)
}

// denetimSistem: aktörü "sistem" olan denetim kaydı (süpürge, otomatik iptal).
func (a *Ajan) denetimSistem(ctx context.Context, b *baglanti, eylem, detay string, ok bool) {
	httpx.DenetimSistem(a.DB, 0, "sistem", eylem, a.hedefAdi(ctx, b), detay, b.ResellerID, ok)
}

// hedefAdi: denetim hedefi — müşteri: alan adı; diğerleri: tg:<kısa>.
func (a *Ajan) hedefAdi(ctx context.Context, b *baglanti) string {
	if b.Rol == p.RolMusteri && b.DomainID > 0 {
		var ad string
		if a.DB.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, b.DomainID).Scan(&ad) == nil && ad != "" {
			return ad + " (" + hedefTg(b.TgKullaniciAdi, b.TgUserID) + ")"
		}
	}
	return hedefTg(b.TgKullaniciAdi, b.TgUserID)
}
