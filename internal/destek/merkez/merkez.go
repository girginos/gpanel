package merkez

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/maske"
)

// Secenekler: Merkez bağımlılıkları (testte sahteler verilir).
type Secenekler struct {
	Yap      *Yapilandirma
	Sirlar   *Sirlar
	Depo     Depo
	Claude   ClaudeIstemci
	Telegram TelegramIstemci
	Lisans   LisansDogrulayici
	Log      *slog.Logger
	Saat     func() time.Time
}

// Merkez: tüm bileşenlerin bağlandığı servis.
type Merkez struct {
	yap       *Yapilandirma
	sir       *Sirlar
	depo      Depo
	claude    ClaudeIstemci
	tg        TelegramIstemci
	lisans    LisansDogrulayici
	log       *slog.Logger
	saat      func() time.Time
	imza      *protokol.Imzalayici
	ajan      *AjanSunucu
	bot       *Bot
	defter    *Defter
	oturumlar *OturumYoneticisi
	suzgec    *CikisSuzgeci

	sayacMu sync.Mutex
	sayac   map[string]int64
}

// Yeni: bağımlılıkları doğrular ve bileşenleri kurar.
func Yeni(s Secenekler) (*Merkez, error) {
	if s.Yap == nil || s.Sirlar == nil || s.Sirlar.MerkezAnahtar == nil || s.Depo == nil || s.Claude == nil || s.Telegram == nil {
		return nil, errors.New("merkez: eksik bağımlılık")
	}
	if err := s.Yap.Dogrula(); err != nil {
		return nil, err
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if s.Saat == nil {
		s.Saat = time.Now
	}
	m := &Merkez{yap: s.Yap, sir: s.Sirlar, depo: s.Depo, claude: s.Claude, tg: s.Telegram, log: s.Log, saat: s.Saat, sayac: map[string]int64{}}
	m.imza = protokol.YeniImzalayici(s.Sirlar.MerkezAnahtar)
	m.imza.Saat = s.Saat
	m.lisans = s.Lisans
	if m.lisans == nil {
		m.lisans = &lisansDogrulamaYok{m: m}
	}
	m.ajan = yeniAjanSunucu(m)
	m.defter = yeniDefter(s.Yap, s.Depo, s.Log, s.Saat)
	m.oturumlar = yeniOturumYoneticisi(s.Yap.SunucuOturumSiniri, s.Yap.oturumBosta(), s.Saat)
	m.suzgec = yeniCikisSuzgeci(s.Sirlar.Bilinenler())
	m.bot = yeniBot(m)
	return m, nil
}

func (m *Merkez) say(ad string) {
	m.sayacMu.Lock()
	m.sayac[ad]++
	m.sayacMu.Unlock()
}

func (m *Merkez) sayacAl(ad string) int64 {
	m.sayacMu.Lock()
	defer m.sayacMu.Unlock()
	return m.sayac[ad]
}

// Handler: dış uçlar (nginx → 127.0.0.1). Yalnız sözleşmedeki yollar.
func (m *Merkez) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+protokol.TelegramWebhookYolu, m.bot.WebhookIsle)
	mux.HandleFunc("GET "+protokol.WSSYolu, m.ajan.WSIsle)
	mux.HandleFunc("POST "+protokol.YoklamaKayitYolu, m.ajan.YoklamaKayit)
	mux.HandleFunc("POST "+protokol.YoklamaYanitYolu, m.ajan.YoklamaYanit)
	mux.HandleFunc("POST "+protokol.YoklamaAlYolu, m.ajan.YoklamaAl)
	mux.HandleFunc("POST "+protokol.YoklamaGonderYolu, m.ajan.YoklamaGonder)
	return mux
}

// IcHandler: yalnız yerel dinleyicide (sağlık + metrik; sır/kişisel veri yok).
func (m *Merkez) IcHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /saglik", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /metrik", func(w http.ResponseWriter, _ *http.Request) {
		gg, ga := m.depo.Maliyet("*", m.saat())
		m.sayacMu.Lock()
		sayac := map[string]int64{}
		for k, v := range m.sayac {
			sayac[k] = v
		}
		m.sayacMu.Unlock()
		bk, be := m.ajan.bekleyenSayilari()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cevrimici_ajan":   m.ajan.cevrimiciSayi(),
			"bekleyen_kod":     bk,
			"bekleyen_eslesme": be,
			"etkin_oturum":     m.oturumlar.Sayi(),
			"cache_isabet":     m.defter.cache.Durum(),
			"genel_gunluk_usd": gg,
			"genel_aylik_usd":  ga,
			"sayac":            sayac,
		})
	})
	return mux
}

// Calistir: arka plan bakımı (yoklama budama, konuşma kaydı saklama). ctx bitince döner.
func (m *Merkez) Calistir(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	sonBudama := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.ajan.yoklamaBuda()
			if time.Since(sonBudama) > time.Hour {
				sonBudama = time.Now()
				if err := m.depo.KonusmaBuda(m.saat(), m.yap.KonusmaSaklamaGun); err != nil {
					m.log.Error("konuşma kaydı budanamadı", "hata", err)
				}
			}
		}
	}
}

// Bekle: süren webhook işlemlerinin bitmesini bekler (kapanış/test).
func (m *Merkez) Bekle() { m.bot.isler.Wait() }

// konusmaKaydet: maskeli, kesik (≤4000 rune) konuşma satırı.
func (m *Merkez) konusmaKaydet(tg int64, sunucu, yon, metin string) {
	satirlar := strings.Split(metin, "\n")
	satirlar = maske.MaskeleSatirlar(satirlar, maske.Secenek{BilinenSirlar: m.sir.Bilinenler(), IPKirp: true, EpostaMaskele: true})
	k := KonusmaKaydi{Zaman: m.saat().UTC(), Tg: tgKisa(tg), Sunucu: sunucu, Yon: yon, Metin: maske.KesRune(strings.Join(satirlar, "\n"), 4000)}
	if err := m.depo.KonusmaEkle(k); err != nil {
		m.log.Error("konuşma kaydı yazılamadı", "hata", err)
	}
}

// baglantiEtkin: panelde ikinci onay verildi → kullanıcıya karşılama + KVKK aydınlatma.
func (m *Merkez) baglantiEtkin(b Baglanti) {
	k, _ := m.depo.KullaniciAl(b.TgUserID)
	if k.SeciliBaglanti == "" {
		k.SeciliBaglanti = b.BaglantiID
		_ = m.depo.KullaniciKaydet(k)
	}
	// ajan okuma döngüsünü Telegram çağrısıyla bekletme
	go m.bot.kvkkGonder(context.Background(), b, kullaniciDili(k, ""))
}

// baglantiKapandi: ajan ya da panel bağlantıyı iptal etti.
func (m *Merkez) baglantiKapandi(b Baglanti, neden protokol.IptalNedeni) {
	k, _ := m.depo.KullaniciAl(b.TgUserID)
	if k.SeciliBaglanti == b.BaglantiID {
		k.SeciliBaglanti = ""
		_ = m.depo.KullaniciKaydet(k)
	}
	if neden == protokol.IptalUnutBeni {
		return
	}
	dil := kullaniciDili(k, "")
	go func() {
		_, err := m.tg.MesajGonder(context.Background(), GidenMesaj{ChatID: b.TgUserID, Metin: metin(dil, "baglanti_iptal", string(neden))})
		if err != nil {
			m.log.Warn("telegram gönderilemedi", "hata", err)
		}
	}()
}

// eslesmeReddedildi: ajan /start sonrası eşleştirmeyi reddetti.
func (m *Merkez) eslesmeReddedildi(tg int64, kod protokol.HataKodu) {
	k, _ := m.depo.KullaniciAl(tg)
	if _, err := m.tg.MesajGonder(context.Background(), GidenMesaj{ChatID: tg, Metin: metin(kullaniciDili(k, ""), "eslesme_red", string(kod))}); err != nil {
		m.log.Warn("telegram gönderilemedi", "hata", err)
	}
}

// insanDevri: destek grubuna kısa, maskeli bilet; grup yoksa yalnız log.
// Dönüş: kullanıcıya gösterilecek metin anahtarı.
func (m *Merkez) insanDevri(ctx context.Context, kul *TgKullanici, b *Baglanti, neden, sonMesaj string) string {
	m.say("insan_devri")
	sunucu, rol, duzey := "-", "-", "-"
	if b != nil {
		sunucu, rol, duzey = b.SunucuKimligi, string(b.Rol), string(b.Duzey)
	}
	m.log.Info("insan devri", "tg", tgKisa(kul.ID), "sunucu", sunucu, "neden", neden)
	if m.yap.DestekGrubuID == 0 {
		return "devir_grup_yok"
	}
	ad := ""
	if tgAdDeseni(kul.Username) {
		ad = " @" + kul.Username
	}
	son := maske.KesRune(maske.MaskeleSatir(strings.ReplaceAll(sonMesaj, "\n", " "), maske.Secenek{BilinenSirlar: m.sir.Bilinenler(), IPKirp: true, EpostaMaskele: true}), 500)
	g := "🆘 Destek devri\nNeden: " + neden + "\nKullanıcı: tg:" + tgKisa(kul.ID) + ad +
		"\nSunucu: " + sunucu + "\nRol/düzey: " + rol + "/" + duzey + "\nSon mesaj: " + son
	if _, err := m.tg.MesajGonder(ctx, GidenMesaj{ChatID: m.yap.DestekGrubuID, Metin: g}); err != nil {
		m.log.Error("destek grubuna gönderilemedi", "hata", err)
		return "devir_grup_yok"
	}
	return "devir"
}
