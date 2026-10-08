package merkez

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/destek/protokol"
)

const (
	webhookGovdeSiniri  = 1 << 20
	esZamanliGuncelleme = 64
	tgMetinSiniri       = 4000
)

// Bot: Telegram tarafı.
type Bot struct {
	m        *Merkez
	isler    sync.WaitGroup
	sem      chan struct{}
	hiz      *kayanPencere
	startHiz *kayanPencere

	gmu     sync.Mutex
	gorulen map[int64]struct{}
	sira    []int64
}

func yeniBot(m *Merkez) *Bot {
	return &Bot{
		m: m, sem: make(chan struct{}, esZamanliGuncelleme),
		hiz:      yeniKayanPencere(m.yap.MesajDakikaSiniri, time.Minute),
		startHiz: yeniKayanPencere(5, 10*time.Minute),
		gorulen:  map[int64]struct{}{},
	}
}

// yeniMi: update_id tekrarı (Telegram yeniden deneme) atlanır.
func (b *Bot) yeniMi(id int64) bool {
	b.gmu.Lock()
	defer b.gmu.Unlock()
	if _, var_ := b.gorulen[id]; var_ {
		return false
	}
	b.gorulen[id] = struct{}{}
	b.sira = append(b.sira, id)
	if len(b.sira) > 4096 {
		delete(b.gorulen, b.sira[0])
		b.sira = b.sira[1:]
	}
	return true
}

func (b *Bot) unut(id int64) {
	b.gmu.Lock()
	delete(b.gorulen, id)
	b.gmu.Unlock()
}

// WebhookIsle: POST protokol.TelegramWebhookYolu. Gizli başlık sabit zamanlı karşılaştırılır;
// geçersizse 401 ve HİÇBİR işlem yapılmaz. Geçerli güncelleme hemen 200 alır, arka planda işlenir.
func (b *Bot) WebhookIsle(w http.ResponseWriter, r *http.Request) {
	gizli := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if gizli == "" || subtle.ConstantTimeCompare([]byte(gizli), []byte(b.m.sir.WebhookGizli)) != 1 {
		b.m.say("webhook_red")
		http.Error(w, "yetkisiz", http.StatusUnauthorized)
		return
	}
	veri, err := io.ReadAll(io.LimitReader(r.Body, webhookGovdeSiniri+1))
	if err != nil || len(veri) > webhookGovdeSiniri {
		http.Error(w, "gövde", http.StatusRequestEntityTooLarge)
		return
	}
	var u TgGuncelleme
	if err := json.Unmarshal(veri, &u); err != nil {
		w.WriteHeader(http.StatusOK) // bozuk güncellemeyi yeniden denetme
		return
	}
	if !b.yeniMi(u.UpdateID) {
		w.WriteHeader(http.StatusOK)
		return
	}
	select {
	case b.sem <- struct{}{}:
	default:
		// 503 → Telegram yeniden dener; update_id yeniden işlenebilsin diye unutulur
		b.unut(u.UpdateID)
		b.m.log.Warn("webhook kuyruğu dolu; Telegram yeniden deneyecek")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	b.isler.Add(1) // 200'den ÖNCE: Bekle() yanıtı görmüş çağırana karşı yarışsız
	w.WriteHeader(http.StatusOK)
	go func() {
		defer b.isler.Done()
		defer func() { <-b.sem }()
		// Tek bir kötü güncelleme (nil/dizin hatası) tüm destek merkezi sürecini
		// düşürmesin: goroutine'de kurtarılmayan panik süreci sonlandırır.
		defer func() {
			if r := recover(); r != nil {
				b.m.log.Error("telegram güncellemesi işlenirken panik (güncelleme atlandı)", "panik", fmt.Sprint(r))
			}
		}()
		ctx, iptal := context.WithTimeout(context.Background(), 5*time.Minute)
		defer iptal()
		b.guncellemeIsle(ctx, &u)
	}()
}

func (b *Bot) gonder(ctx context.Context, chat int64, s string, html bool, dugmeler ...[]Dugme) int64 {
	id, err := b.m.tg.MesajGonder(ctx, GidenMesaj{ChatID: chat, Metin: s, HTML: html, Dugmeler: dugmeler})
	if err != nil {
		b.m.log.Warn("telegram gönderilemedi", "hata", err)
	}
	return id
}

func (b *Bot) guncellemeIsle(ctx context.Context, u *TgGuncelleme) {
	if c := u.CallbackQuery; c != nil {
		if c.Message == nil || c.Message.Chat.Type != "private" || c.From.IsBot {
			return
		}
		b.callbackIsle(ctx, c)
		return
	}
	msg := u.Message
	if msg == nil || msg.Chat.Type != "private" || msg.From == nil || msg.From.IsBot || msg.Chat.ID != msg.From.ID {
		return // grup/kanal/bot: SESSİZ
	}
	kul := msg.From
	k, _ := b.m.depo.KullaniciAl(kul.ID)
	dil := kullaniciDili(k, kul.LanguageCode)
	izin, ilkRed := b.hiz.Izin(tgKisa(kul.ID), b.m.saat())
	if !izin {
		b.m.say("hiz_red")
		if ilkRed {
			b.gonder(ctx, kul.ID, metin(dil, "hiz"), false)
		}
		return
	}
	txt := strings.TrimSpace(msg.Text)
	if txt == "" {
		b.gonder(ctx, kul.ID, metin(dil, "metin_yok"), false)
		return
	}
	if strings.HasPrefix(txt, "/") {
		b.komutIsle(ctx, kul, k, dil, txt)
		return
	}
	b.destekIsle(ctx, kul, k, dil, txt)
}

func komutAyir(txt string) (string, string) {
	kom, arg, _ := strings.Cut(txt, " ")
	if i := strings.IndexByte(kom, '@'); i >= 0 {
		kom = kom[:i]
	}
	return strings.ToLower(kom), strings.TrimSpace(arg)
}

func (b *Bot) komutIsle(ctx context.Context, kul *TgKullanici, k Kullanici, dil, txt string) {
	kom, arg := komutAyir(txt)
	switch kom {
	case "/start":
		b.startIsle(ctx, kul, dil, arg)
	case "/hesaplarim":
		b.hesaplarim(ctx, kul, k, dil)
	case "/durum":
		b.durum(ctx, kul, k, dil)
	case "/insan":
		bg := b.seciliBaglanti(kul.ID, k)
		b.gonder(ctx, kul.ID, metin(dil, b.m.insanDevri(ctx, kul, bg, "kullanici_istegi", "")), false)
	case "/dil":
		if arg != "tr" && arg != "en" {
			b.gonder(ctx, kul.ID, metin(dil, "dil_bicim"), false)
			return
		}
		k.TgUserID = kul.ID
		k.Dil = arg
		_ = b.m.depo.KullaniciKaydet(k)
		b.gonder(ctx, kul.ID, metin(arg, "dil_tamam"), false)
	case "/unutbeni":
		b.gonder(ctx, kul.ID, metin(dil, "unut_sor"), false, []Dugme{{Metin: metin(dil, "unut_dugme"), Veri: "unut:evet"}})
	default:
		b.gonder(ctx, kul.ID, metin(dil, "yardim"), false)
	}
}

func (b *Bot) startIsle(ctx context.Context, kul *TgKullanici, dil, kod string) {
	if kod == "" {
		b.gonder(ctx, kul.ID, metin(dil, "hosgeldin"), true)
		return
	}
	// Kod kuralı protokolde (eslesme.go): merkez kodu NORMALLEŞTİRİLMİŞ haliyle özetler;
	// portal kodu "ABCD-EFGH-…" gösterir, kullanıcı küçük harf ya da boşlukla yazabilir.
	ozet, gecerli := protokol.EslesmeKoduOzet(kod)
	if !gecerli {
		b.gonder(ctx, kul.ID, metin(dil, "kod_bicim"), false)
		return
	}
	if izin, _ := b.startHiz.Izin(tgKisa(kul.ID), b.m.saat()); !izin {
		b.gonder(ctx, kul.ID, metin(dil, "kod_cok_deneme"), false)
		return
	}
	_, err := b.m.ajan.EslesmeBaslat(ozet, kul.ID, kul.Username)
	switch {
	case errors.Is(err, ErrKodGecersiz):
		b.gonder(ctx, kul.ID, metin(dil, "kod_gecersiz"), false)
	case errors.Is(err, ErrAjanCevrimdisi):
		b.gonder(ctx, kul.ID, metin(dil, "ajan_cevrimdisi"), false)
	case errors.Is(err, ErrDogrulanmadi):
		b.gonder(ctx, kul.ID, metin(dil, "sunucu_dogrulanmadi"), false)
	case err != nil:
		b.m.log.Error("eşleştirme başlatılamadı", "hata", err)
		b.gonder(ctx, kul.ID, metin(dil, "gecici_hata"), false)
	default:
		b.gonder(ctx, kul.ID, metin(dil, "eslesme_bekliyor"), false)
	}
}

func baglantiKisa(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:8])
}

// seciliBaglanti: tercih edilen, yoksa tek etkin bağlantı, yoksa ilk bağlantı.
func (b *Bot) seciliBaglanti(tg int64, k Kullanici) *Baglanti {
	bs := b.m.depo.KullaniciBaglantilari(tg)
	if len(bs) == 0 {
		return nil
	}
	for i := range bs {
		if bs[i].BaglantiID == k.SeciliBaglanti {
			return &bs[i]
		}
	}
	for i := range bs {
		if bs[i].Durum == protokol.BaglantiEtkin {
			return &bs[i]
		}
	}
	return &bs[0]
}

func (b *Bot) baglantiEtiketi(bg Baglanti) string {
	if len(bg.Hesaplar) > 0 {
		return bg.Hesaplar[0].Etiket + " (" + bg.SunucuKimligi + ")"
	}
	return bg.SunucuKimligi
}

func (b *Bot) hesaplarim(ctx context.Context, kul *TgKullanici, k Kullanici, dil string) {
	bs := b.m.depo.KullaniciBaglantilari(kul.ID)
	if len(bs) == 0 {
		b.gonder(ctx, kul.ID, metin(dil, "baglanti_yok"), false)
		return
	}
	sec := b.seciliBaglanti(kul.ID, k)
	var sb strings.Builder
	sb.WriteString(metin(dil, "hesaplarim_baslik"))
	var dugmeler [][]Dugme
	for _, bg := range bs {
		isaret := "▫️"
		if sec != nil && sec.BaglantiID == bg.BaglantiID {
			isaret = "▶️"
		}
		sb.WriteString("\n\n" + isaret + " <b>" + htmlKacis(bg.SunucuKimligi) + "</b> — " + htmlKacis(string(bg.Rol)) + "/" + htmlKacis(string(bg.Duzey)) + " — " + htmlKacis(string(bg.Durum)))
		for i, h := range bg.Hesaplar {
			if i == 20 {
				sb.WriteString("\n  …")
				break
			}
			sb.WriteString("\n  " + htmlKacis(h.Etiket))
		}
		if len(bs) > 1 {
			dugmeler = append(dugmeler, []Dugme{{Metin: b.baglantiEtiketi(bg), Veri: "sec:" + baglantiKisa(bg.BaglantiID)}})
		}
	}
	b.gonder(ctx, kul.ID, sb.String(), true, dugmeler...)
}

func (b *Bot) durum(ctx context.Context, kul *TgKullanici, k Kullanici, dil string) {
	bs := b.m.depo.KullaniciBaglantilari(kul.ID)
	if len(bs) == 0 {
		b.gonder(ctx, kul.ID, metin(dil, "baglanti_yok"), false)
		return
	}
	var sb strings.Builder
	sb.WriteString(metin(dil, "durum_baslik"))
	for _, bg := range bs {
		ag := metin(dil, "cevrimdisi")
		if b.m.ajan.Cevrimici(bg.SunucuKimligi) {
			ag = metin(dil, "cevrimici")
		}
		sb.WriteString("\n• " + htmlKacis(b.baglantiEtiketi(bg)) + ": " + htmlKacis(string(bg.Durum)) + ", " + htmlKacis(string(bg.Duzey)) + ", " + ag)
	}
	b.gonder(ctx, kul.ID, sb.String(), true)
}

func (b *Bot) kvkkGonder(ctx context.Context, bg Baglanti, dil string) {
	b.gonder(ctx, bg.TgUserID, metin(dil, "kvkk", htmlKacis(b.baglantiEtiketi(bg))), true,
		[]Dugme{{Metin: metin(dil, "kvkk_dugme"), Veri: "kvkk:" + baglantiKisa(bg.BaglantiID)}})
}

func (b *Bot) callbackIsle(ctx context.Context, c *TgCallback) {
	kul := c.From
	k, _ := b.m.depo.KullaniciAl(kul.ID)
	dil := kullaniciDili(k, kul.LanguageCode)
	if izin, _ := b.hiz.Izin(tgKisa(kul.ID), b.m.saat()); !izin {
		_ = b.m.tg.CallbackYanitla(ctx, c.ID, metin(dil, "hiz"))
		return
	}
	tur, deger, _ := strings.Cut(c.Data, ":")
	bul := func() *Baglanti {
		for _, bg := range b.m.depo.KullaniciBaglantilari(kul.ID) {
			if baglantiKisa(bg.BaglantiID) == deger {
				return &bg
			}
		}
		return nil
	}
	switch tur {
	case "kvkk":
		bg := bul()
		if bg == nil {
			_ = b.m.tg.CallbackYanitla(ctx, c.ID, "")
			return
		}
		if bg.KVKKKabul.IsZero() {
			// 🔴 KVKK: bu yol yalnız portal onayı OLMAYAN eski kayıtlar için kalır (yeni bağlantılar
			// portalda imzalı v2 onayından doğar ve KVKKKabul'u zaten taşır). Portal yolu gibi
			// kaynak + metin sürümü + zaman kaydedilir; sürüm bot metninin etiketidir (KvkkMetinSurumu).
			bg.KVKKKabul = b.m.saat().UTC()
			bg.KVKKSurum = KvkkMetinSurumu
			bg.KVKKKaynak = KvkkKaynakTelegram
			_ = b.m.depo.BaglantiKaydet(*bg)
		}
		_ = b.m.tg.CallbackYanitla(ctx, c.ID, "")
		b.gonder(ctx, kul.ID, metin(dil, "kvkk_tamam"), false)
	case "sec":
		bg := bul()
		if bg == nil {
			_ = b.m.tg.CallbackYanitla(ctx, c.ID, "")
			return
		}
		k.TgUserID = kul.ID
		k.SeciliBaglanti = bg.BaglantiID
		_ = b.m.depo.KullaniciKaydet(k)
		_ = b.m.tg.CallbackYanitla(ctx, c.ID, metin(dil, "secildi", b.baglantiEtiketi(*bg)))
	case "unut":
		if deger != "evet" {
			return
		}
		b.unutBeni(ctx, kul.ID, c.ID, dil)
	default:
		_ = b.m.tg.CallbackYanitla(ctx, c.ID, "")
	}
}

// unutBeni: /unutbeni onayı — gerçek silme. Hub'da: kullanıcı tercihi, TÜM bağlantılar, o kullanıcıya
// ait konuşma satırları (tüm gün dosyalarında, atomik yeniden yazım), bellek oturumları, bekleyen
// eşleştirmeler ve hız sayaçları silinir. Ajana (sunucu) her bağlantı için BaglantiIptal{unutbeni}
// gider; ajan Telegram kimliğini anonimleştirir. Ajan çevrimdışıysa hub kaydı yine silinir; ajan
// bir sonraki bağlanışta bağlantıyı bildirir, merkezde karşılığı olmadığından iptal+anonimleştirme
// TAMAMLANIR (merkez/ajan.go baglantiDurumIsle). Yanıt metni ikisini ayrı söyler.
func (b *Bot) unutBeni(ctx context.Context, tg int64, callbackID, dil string) {
	cevrimdisi := false
	for _, bg := range b.m.depo.KullaniciBaglantilari(tg) {
		if !b.m.ajan.BaglantiIptalGonder(bg.SunucuKimligi, bg.BaglantiID, protokol.IptalUnutBeni) {
			cevrimdisi = true
			b.m.log.Warn("unutbeni: ajan çevrimdışı; sunucu tarafı bir sonraki bağlanışta tamamlanacak (merkez kaydı yine silinir)", "sunucu", bg.SunucuKimligi)
		}
	}
	b.m.unutuldu(tg) // uçuştaki işlem silmeden sonra kayıt eklemesin
	b.m.oturumlar.KapatKullanici(tg)
	b.m.ajan.eslesmeTemizle(tg)
	if err := b.m.depo.KullaniciUnut(tg); err != nil {
		b.m.log.Error("unutbeni başarısız", "hata", err)
		_ = b.m.tg.CallbackYanitla(ctx, callbackID, "")
		b.gonder(ctx, tg, metin(dil, "unut_hata"), false)
		return
	}
	b.hiz.Sil(tgKisa(tg))
	b.startHiz.Sil(tgKisa(tg))
	_ = b.m.tg.CallbackYanitla(ctx, callbackID, "")
	if cevrimdisi {
		b.gonder(ctx, tg, metin(dil, "unut_tamam_bekliyor"), false)
		return
	}
	b.gonder(ctx, tg, metin(dil, "unut_tamam"), false)
}

// ilerlemeYazici: ilerleme mesajını en çok saniyede bir düzenler.
type ilerlemeYazici struct {
	b     *Bot
	ctx   context.Context
	chat  int64
	mesaj int64
	son   time.Time
	metin string
}

func (i *ilerlemeYazici) yaz(s string) {
	if i.mesaj == 0 || s == i.metin || time.Since(i.son) < time.Second {
		return
	}
	i.son, i.metin = time.Now(), s
	_ = i.b.m.tg.EylemGonder(i.ctx, i.chat, "typing")
	if err := i.b.m.tg.MesajDuzenle(i.ctx, i.chat, i.mesaj, s, false); err != nil {
		i.b.m.log.Debug("ilerleme düzenlenemedi", "hata", err)
	}
}

// destekIsle: serbest metin → Claude.
func (b *Bot) destekIsle(ctx context.Context, kul *TgKullanici, k Kullanici, dil, txt string) {
	bg := b.seciliBaglanti(kul.ID, k)
	switch {
	case bg == nil:
		b.gonder(ctx, kul.ID, metin(dil, "baglanti_yok"), false)
		return
	case bg.Durum != protokol.BaglantiEtkin:
		b.gonder(ctx, kul.ID, metin(dil, "onay_bekliyor"), false)
		return
	case bg.KVKKKabul.IsZero():
		b.gonder(ctx, kul.ID, metin(dil, "kvkk_gerekli"), false)
		b.kvkkGonder(ctx, *bg, dil)
		return
	case bg.Duzey == protokol.DuzeyKapali:
		b.gonder(ctx, kul.ID, metin(dil, "destek_kapali"), false)
		return
	}
	o, err := b.m.oturumlar.Al(*bg, dil)
	switch {
	case errors.Is(err, ErrMesgul):
		b.m.say("mesgul")
		b.gonder(ctx, kul.ID, metin(dil, "mesgul"), false)
		return
	case errors.Is(err, ErrIsleniyor):
		b.gonder(ctx, kul.ID, metin(dil, "isleniyor"), false)
		return
	case err != nil:
		return
	}
	bitir := false
	defer func() { b.m.oturumlar.Birak(o, bitir) }()

	// 🔴 KVKK: ham kullanıcı metni bu noktadan sonra HİÇBİR yere gitmez (konuşma kaydı,
	// triyaj/ana model, insan devri mesajı) — önce maskelenir (dongu.go maskeliKullaniciMetni).
	txt = b.m.maskeliKullaniciMetni(txt)
	b.m.konusmaKaydet(kul.ID, bg.SunucuKimligi, "kullanici", txt)
	_ = b.m.tg.EylemGonder(ctx, kul.ID, "typing")
	il := &ilerlemeYazici{b: b, ctx: ctx, chat: kul.ID, metin: metin(dil, "inceleniyor")}
	il.mesaj = b.gonder(ctx, kul.ID, il.metin, false)

	s := b.m.destekYanitla(ctx, o, txt, dil, il.yaz)
	bitir = s.OturumBitir
	if s.Devir && s.Metin == "" {
		s.Metin = metin(dil, b.m.insanDevri(ctx, kul, bg, s.Neden, txt))
		s.Sabit = true
	} else if s.Devir {
		devir := b.m.insanDevri(ctx, kul, bg, s.Neden, txt)
		s.Metin += "\n\n" + metin(dil, devir)
	}
	cikti := s.Metin
	if !s.Sabit {
		temiz, red, neden := b.m.suzgec.Suz(s.Metin, hesapAlanlari(o.Baglam))
		if !red {
			if ok, n := b.m.cikisDenetle(ctx, o.Sunucu, temiz); !ok {
				red, neden = true, "denetim:"+n
			}
		}
		if red {
			b.m.say("cikis_red")
			b.m.log.Warn("yanıt çıkış süzgecinde reddedildi", "sunucu", o.Sunucu, "neden", neden)
			bitir = true
			devir := b.m.insanDevri(ctx, kul, bg, "cikis_red:"+neden, txt)
			temiz = metin(dil, "cikis_red") + "\n\n" + metin(dil, devir)
		}
		cikti = temiz
	}
	b.m.konusmaKaydet(kul.ID, bg.SunucuKimligi, "bot", cikti)
	b.yanitGonder(ctx, kul.ID, il.mesaj, cikti)
}

func hesapAlanlari(o protokol.OturumBaglami) []string {
	out := make([]string, 0, len(o.Hesaplar))
	for _, h := range o.Hesaplar {
		out = append(out, h.Etiket)
	}
	return out
}

// yanitGonder: ilk parça ilerleme mesajını düzenler, kalanlar yeni mesaj (düz metin; HTML yorumlanmaz).
func (b *Bot) yanitGonder(ctx context.Context, chat, mesaj int64, s string) {
	parcalar := parcala(s, tgMetinSiniri)
	basla := 0
	if mesaj != 0 {
		if err := b.m.tg.MesajDuzenle(ctx, chat, mesaj, parcalar[0], false); err == nil {
			basla = 1
		}
	}
	for _, p := range parcalar[basla:] {
		b.gonder(ctx, chat, p, false)
	}
}
