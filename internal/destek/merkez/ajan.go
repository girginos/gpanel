package merkez

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/destek/protokol"
)

// LisansDogrulayici: kayit.lisans_ispati doğrulaması.
// TODO(destek faz 2): lic-eu ile ispat doğrulaması (parmak izi ↔ etkin lisans).
type LisansDogrulayici interface {
	Dogrula(ctx context.Context, parmakIzi string, ispat []byte) error
}

// lisansDogrulamaYok: Faz 1 — ispat biçim olarak doğrulanır (protokol), içerik DOĞRULANMAZ.
type lisansDogrulamaYok struct {
	once sync.Once
	m    *Merkez
}

func (l *lisansDogrulamaYok) Dogrula(context.Context, string, []byte) error {
	l.once.Do(func() {
		l.m.log.Warn("lisans ispatı doğrulanmıyor (Faz 1 TODO): kayıt yalnız cihaz anahtarına dayanıyor")
	})
	return nil
}

const (
	kodSinirSunucu   = 20
	kodSinirToplam   = 10000
	meydanSiniri     = 10000
	enUzunKodOmru    = 10*time.Minute + protokol.SaatSapmasi
	eslesmeBeklemesi = 24 * time.Hour
	kayitBeklemesi   = 10 * time.Second
	yoklamaKuyrugu   = 256
)

type isYaniti struct {
	sonuc   *protokol.IsSonucu
	hata    *protokol.HataMesaji
	kapandi bool
}

// ajanOturumu: kimliği doğrulanmış tek ajan bağlantısı (WSS ya da yoklama).
type ajanOturumu struct {
	sunucu  string
	tasima  string
	gonderF func([]byte) error
	kapatF  func()

	mu      sync.Mutex
	isler   map[string]chan isYaniti
	kapandi bool
	sira    uint64
}

func (o *ajanOturumu) gonder(g protokol.Govde) error {
	return o.gonderKimlikle(yeniKimlik(), g)
}

// gonderKimlikle: zarf kimliği çağırandan (ajanın hata.ilgili ile yanıtı eşlenecekse).
func (o *ajanOturumu) gonderKimlikle(kimlik string, g protokol.Govde) error {
	b, err := protokol.Kodla(kimlik, g)
	if err != nil {
		return err
	}
	return o.gonderF(b)
}

func (o *ajanOturumu) kapat() {
	o.mu.Lock()
	if o.kapandi {
		o.mu.Unlock()
		return
	}
	o.kapandi = true
	for k, ch := range o.isler {
		ch <- isYaniti{kapandi: true}
		delete(o.isler, k)
	}
	o.mu.Unlock()
	if o.kapatF != nil {
		o.kapatF()
	}
}

type bekleyenKod struct {
	sunucu string
	bitis  time.Time
}

type eslesmeKaydi struct {
	sunucu string
	tg     int64
	bitis  time.Time
	zarf   string // eslesme_onayla zarf kimliği: ajanın reddi hata.ilgili ile buna döner
}

type bekleyenMeydan struct {
	kayit  *protokol.Kayit
	meydan *protokol.MeydanOkuma
	bitis  time.Time
}

type yoklamaOturumu struct {
	ao     *ajanOturumu
	kuyruk chan []byte
	son    time.Time

	emu       sync.Mutex
	ertelenen []byte // /al sınırına sığmayan zarf: sıradaki /al'ın İLK zarfı
}

// AjanSunucu: ajan uçları + bağlı ajanlar + eşleştirme durumu.
type AjanSunucu struct {
	m *Merkez

	mu         sync.Mutex
	aktif      map[string]*ajanOturumu
	kodlar     map[string]bekleyenKod
	eslesmeler map[string]eslesmeKaydi
	meydanlar  map[string]bekleyenMeydan
	yoklamalar map[string]*yoklamaOturumu // sha256(belirteç) hex → oturum
}

func yeniAjanSunucu(m *Merkez) *AjanSunucu {
	return &AjanSunucu{m: m, aktif: map[string]*ajanOturumu{}, kodlar: map[string]bekleyenKod{},
		eslesmeler: map[string]eslesmeKaydi{}, meydanlar: map[string]bekleyenMeydan{}, yoklamalar: map[string]*yoklamaOturumu{}}
}

// Cevrimici: sunucunun etkin ajan bağlantısı var mı.
func (a *AjanSunucu) Cevrimici(sunucu string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.aktif[sunucu] != nil
}

func (a *AjanSunucu) cevrimiciSayi() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.aktif)
}

// bekleyenSayilari: süresi dolmamış eşleştirme kodu ve panel onayı bekleyen eşleştirme sayısı (metrik).
func (a *AjanSunucu) bekleyenSayilari() (kod, eslesme int) {
	simdi := a.m.saat()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.kodlar {
		if k.bitis.After(simdi) {
			kod++
		}
	}
	for _, e := range a.eslesmeler {
		if e.bitis.After(simdi) {
			eslesme++
		}
	}
	return kod, eslesme
}

// sunucuKimligi: cihaz açık anahtarından türetilir (TOFU kimlik).
func sunucuKimligi(cihazAnahtari string) (string, error) {
	acik, err := protokol.AcikAnahtarCoz(cihazAnahtari)
	if err != nil {
		return "", err
	}
	return "s_" + protokol.AnahtarKimligi(acik), nil
}

// kayitDenetle: katalog ve lisans.
func (a *AjanSunucu) kayitDenetle(ctx context.Context, kay *protokol.Kayit) error {
	if kay.KatalogOzeti != protokol.KatalogOzeti() {
		return &protokol.Hata{Kod: protokol.HataSurum, Ayrinti: "katalog özeti uyuşmuyor"}
	}
	ispat, err := base64.RawURLEncoding.DecodeString(kay.LisansIspati)
	if err != nil {
		return &protokol.Hata{Kod: protokol.HataLisans}
	}
	if err := a.m.lisans.Dogrula(ctx, kay.LisansParmakIzi, ispat); err != nil {
		return &protokol.Hata{Kod: protokol.HataLisans}
	}
	return nil
}

// kimlikTamamla: meydan yanıtı doğrulandıktan sonra cihaz kaydı (TOFU) ve sunucu kimliği.
func (a *AjanSunucu) kimlikTamamla(kay *protokol.Kayit, md *protokol.MeydanOkuma, y *protokol.MeydanYanit) (string, error) {
	if err := protokol.MeydanYanitDogrula(md, kay, y, a.m.saat()); err != nil {
		return "", err
	}
	sunucu, err := sunucuKimligi(kay.CihazAnahtari)
	if err != nil {
		return "", err
	}
	simdi := a.m.saat()
	c, var_ := a.m.depo.CihazAl(sunucu)
	if !var_ {
		c = Cihaz{SunucuKimligi: sunucu, CihazAnahtari: kay.CihazAnahtari, IlkGorulme: simdi}
		a.m.log.Info("yeni ajan cihazı", "sunucu", sunucu)
	} else if c.LisansParmakIzi != kay.LisansParmakIzi {
		a.m.log.Warn("ajan lisans parmak izi değişti", "sunucu", sunucu)
	}
	c.LisansParmakIzi = kay.LisansParmakIzi
	c.PanelSurumu = kay.PanelSurumu
	c.SonGorulme = simdi
	if err := a.m.depo.CihazKaydet(c); err != nil {
		return "", &protokol.Hata{Kod: protokol.HataIc}
	}
	return sunucu, nil
}

// oturumEkle: aynı sunucunun eski bağlantısı kapatılır (tek etkin bağlantı).
func (a *AjanSunucu) oturumEkle(ao *ajanOturumu) {
	a.mu.Lock()
	eski := a.aktif[ao.sunucu]
	a.aktif[ao.sunucu] = ao
	a.mu.Unlock()
	if eski != nil {
		eski.kapat()
	}
	a.m.log.Info("ajan bağlandı", "sunucu", ao.sunucu, "tasima", ao.tasima)
}

func (a *AjanSunucu) oturumCikar(ao *ajanOturumu) {
	a.mu.Lock()
	if a.aktif[ao.sunucu] == ao {
		delete(a.aktif, ao.sunucu)
	}
	a.mu.Unlock()
	ao.kapat()
}

// ---- kimliği doğrulanmış mesaj işleme ----

func (a *AjanSunucu) hataGonder(ao *ajanOturumu, kod protokol.HataKodu, ilgili string) {
	h := &protokol.HataMesaji{Kod: kod, Ilgili: ilgili}
	if h.Dogrula() != nil {
		h.Ilgili = ""
	}
	_ = ao.gonder(h)
}

// isle: ajan → merkez çerçevesi (kayıttan sonra).
func (a *AjanSunucu) isle(ao *ajanOturumu, veri []byte) {
	z, g, err := protokol.Coz(veri, protokol.YonMerkeze)
	if err != nil {
		a.m.log.Warn("ajan çerçevesi reddedildi", "sunucu", ao.sunucu, "hata", protokol.HataKoduAl(err))
		a.hataGonder(ao, protokol.HataKoduAl(err), "")
		return
	}
	switch m := g.(type) {
	case *protokol.KalpAtisi:
	case *protokol.KodOzet:
		a.kodOzetIsle(ao, m)
	case *protokol.BaglantiDurum:
		if err := a.baglantiDurumIsle(ao, m); err != nil {
			a.m.log.Warn("baglanti_durum reddedildi", "sunucu", ao.sunucu, "hata", err)
			a.hataGonder(ao, protokol.HataKoduAl(err), z.Kimlik)
		}
	case *protokol.BaglantiIptal:
		a.baglantiIptalIsle(ao, m)
	case *protokol.IsSonucu:
		a.yanitIlet(ao, m.IsKodu, isYaniti{sonuc: m})
	case *protokol.HataMesaji:
		if m.Ilgili != "" && a.eslesmeReddi(ao, m) {
			return
		}
		if m.Ilgili != "" && a.yanitIlet(ao, m.Ilgili, isYaniti{hata: m}) {
			return
		}
		a.m.log.Info("ajandan hata", "sunucu", ao.sunucu, "kod", m.Kod)
	default:
		// kayit/meydan_yanit bağlantı kurulduktan sonra geçersiz
		a.hataGonder(ao, protokol.HataBicim, z.Kimlik)
	}
}

func (a *AjanSunucu) yanitIlet(ao *ajanOturumu, isKodu string, y isYaniti) bool {
	ao.mu.Lock()
	ch, ok := ao.isler[isKodu]
	if ok {
		delete(ao.isler, isKodu)
	}
	ao.mu.Unlock()
	if !ok {
		a.m.log.Warn("beklenmeyen iş yanıtı", "sunucu", ao.sunucu, "is_kodu", isKodu)
		return false
	}
	ch <- y
	return true
}

func (a *AjanSunucu) kodOzetIsle(ao *ajanOturumu, k *protokol.KodOzet) {
	simdi := a.m.saat()
	bitis := time.Unix(k.SonGecerlilik, 0)
	if !bitis.After(simdi) || bitis.Sub(simdi) > enUzunKodOmru {
		a.hataGonder(ao, protokol.HataZaman, "")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for o, b := range a.kodlar {
		if !b.bitis.After(simdi) {
			delete(a.kodlar, o)
			continue
		}
		if b.sunucu == ao.sunucu {
			n++
		}
	}
	if mevcut, var_ := a.kodlar[k.KodOzet]; var_ && mevcut.sunucu != ao.sunucu {
		a.m.log.Warn("kod özeti çakışması (başka sunucu)", "sunucu", ao.sunucu)
		return
	}
	if n >= kodSinirSunucu || len(a.kodlar) >= kodSinirToplam {
		go a.hataGonder(ao, protokol.HataKota, "")
		return
	}
	a.kodlar[k.KodOzet] = bekleyenKod{sunucu: ao.sunucu, bitis: bitis}
}

// Eşleştirme hataları (kullanıcıya gösterilecek).
var (
	ErrKodGecersiz    = errors.New("eşleştirme kodu geçersiz ya da süresi dolmuş")
	ErrAjanCevrimdisi = errors.New("sunucu şu an merkeze bağlı değil")
)

// EslesmeBaslat: /start <kod>. Kod merkezde TEK KULLANIMLIKTIR (ajan da kendi tarafında tüketir).
func (a *AjanSunucu) EslesmeBaslat(kodOzet string, tg int64, tgAd string) (string, error) {
	simdi := a.m.saat()
	a.mu.Lock()
	k, ok := a.kodlar[kodOzet]
	if !ok || !k.bitis.After(simdi) {
		delete(a.kodlar, kodOzet)
		a.mu.Unlock()
		return "", ErrKodGecersiz
	}
	ao := a.aktif[k.sunucu]
	if ao == nil {
		a.mu.Unlock()
		return "", ErrAjanCevrimdisi
	}
	delete(a.kodlar, kodOzet)
	zarf := yeniKimlik()
	a.eslesmeler[kodOzet] = eslesmeKaydi{sunucu: k.sunucu, tg: tg, bitis: simdi.Add(eslesmeBeklemesi), zarf: zarf}
	a.mu.Unlock()

	if !tgAdDeseni(tgAd) {
		tgAd = ""
	}
	e := &protokol.EslesmeOnayla{KodOzet: kodOzet, TgUserID: tg, TgKullaniciAdi: tgAd}
	if err := a.m.imza.Imzala(e, protokol.EnUzunGecerlilik); err != nil {
		return "", err
	}
	if err := ao.gonderKimlikle(zarf, e); err != nil {
		return "", ErrAjanCevrimdisi
	}
	a.m.log.Info("eşleştirme onayı gönderildi", "sunucu", k.sunucu, "tg", tgKisa(tg))
	return k.sunucu, nil
}

// eslesmeReddi: ajan eslesme_onayla'yı reddetti (kod tüketilmiş, destek kapalı, kota…).
// Kullanıcıya "panelde onay verin" denmişti — sessiz kalmasın; kayıt silinir.
func (a *AjanSunucu) eslesmeReddi(ao *ajanOturumu, h *protokol.HataMesaji) bool {
	a.mu.Lock()
	var (
		bulundu bool
		kayit   eslesmeKaydi
	)
	for oz, e := range a.eslesmeler {
		if e.zarf != "" && e.zarf == h.Ilgili && e.sunucu == ao.sunucu {
			kayit, bulundu = e, true
			delete(a.eslesmeler, oz)
			break
		}
	}
	a.mu.Unlock()
	if !bulundu {
		return false
	}
	a.m.say("eslesme_red")
	a.m.log.Info("ajan eşleştirmeyi reddetti", "sunucu", ao.sunucu, "kod", h.Kod)
	go a.m.eslesmeReddedildi(kayit.tg, h.Kod)
	return true
}

func tgAdDeseni(s string) bool {
	if len(s) < 5 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// baglantiDurumIsle: YENİ bağlantı yalnız merkezin başlattığı eşleştirmeden
// (aynı kod özeti, aynı sunucu, aynı Telegram kullanıcısı) doğabilir; ajan
// keyfi bir Telegram kullanıcısına bağlantı yazıp mesaj attıramaz.
func (a *AjanSunucu) baglantiDurumIsle(ao *ajanOturumu, d *protokol.BaglantiDurum) error {
	simdi := a.m.saat()
	mevcut, var_ := a.m.depo.BaglantiAl(d.BaglantiID)
	if var_ {
		if mevcut.SunucuKimligi != ao.sunucu || mevcut.TgUserID != d.TgUserID {
			return &protokol.Hata{Kod: protokol.HataYetki, Ayrinti: "bağlantı başka sunucu/kullanıcıya ait"}
		}
	} else {
		a.mu.Lock()
		e, ok := a.eslesmeler[d.KodOzet]
		if ok && !e.bitis.After(simdi) {
			delete(a.eslesmeler, d.KodOzet)
			ok = false
		}
		a.mu.Unlock()
		if d.KodOzet == "" || !ok || e.sunucu != ao.sunucu || e.tg != d.TgUserID {
			// Merkezde karşılığı olmayan bağlantı (ör. ajan çevrimdışıyken /unutbeni ya da
			// merkez kaydı silindi): merkez onu ASLA kullanamaz — ajana da kapattırılır ki
			// panelde "aktif" görünüp süresi dolana dek asılı kalmasın.
			if d.Durum != protokol.BaglantiIptalEdildi {
				_ = ao.gonder(&protokol.BaglantiIptal{BaglantiID: d.BaglantiID, Neden: protokol.IptalMerkez})
			}
			return &protokol.Hata{Kod: protokol.HataEslesmeKodu, Ayrinti: "merkezde eşleştirme kaydı yok"}
		}
	}
	if d.Durum == protokol.BaglantiIptalEdildi {
		if var_ {
			_ = a.m.depo.BaglantiSil(d.BaglantiID)
			a.m.oturumlar.KapatBaglanti(d.BaglantiID)
			a.m.baglantiKapandi(mevcut, protokol.IptalKullanici)
		}
		a.eslesmeSil(d.KodOzet)
		return nil
	}
	b := Baglanti{
		BaglantiID: d.BaglantiID, SunucuKimligi: ao.sunucu, TgUserID: d.TgUserID, Durum: d.Durum,
		Rol: d.Rol, Duzey: d.Duzey, Hesaplar: d.Hesaplar, KVKKKabul: mevcut.KVKKKabul, Guncelleme: simdi,
	}
	if err := a.m.depo.BaglantiKaydet(b); err != nil {
		return &protokol.Hata{Kod: protokol.HataIc}
	}
	a.m.oturumlar.KapatBaglanti(d.BaglantiID) // kapsam değişmiş olabilir
	if d.Durum == protokol.BaglantiEtkin {
		a.eslesmeSil(d.KodOzet)
		if !var_ || mevcut.Durum != protokol.BaglantiEtkin {
			a.m.baglantiEtkin(b)
		}
	}
	return nil
}

func (a *AjanSunucu) eslesmeSil(kodOzet string) {
	if kodOzet == "" {
		return
	}
	a.mu.Lock()
	delete(a.eslesmeler, kodOzet)
	a.mu.Unlock()
}

func (a *AjanSunucu) baglantiIptalIsle(ao *ajanOturumu, i *protokol.BaglantiIptal) {
	b, ok := a.m.depo.BaglantiAl(i.BaglantiID)
	if !ok || b.SunucuKimligi != ao.sunucu {
		// İptal idempotenttir: ajan her yeniden bağlanışta son 7 günün iptallerini
		// (senkron) yeniden bildirir; merkezde olmayan kayıt için hata YANITLANMAZ —
		// yanıtlansaydı yüzlerce iptal yoklama kuyruğunu doldurup oturumu düşürürdü.
		// Başka sunucunun bağlantısına dokunulmaz (yetki_yok).
		if ok {
			a.m.log.Warn("başka sunucunun bağlantısı için iptal reddedildi", "sunucu", ao.sunucu)
			a.hataGonder(ao, protokol.HataYetki, "")
		}
		return
	}
	_ = a.m.depo.BaglantiSil(i.BaglantiID)
	a.m.oturumlar.KapatBaglanti(i.BaglantiID)
	a.m.baglantiKapandi(b, i.Neden)
}

// BaglantiIptalGonder: merkez → ajan (ör. /unutbeni). Ajan çevrimdışıysa false.
func (a *AjanSunucu) BaglantiIptalGonder(sunucu, baglanti string, neden protokol.IptalNedeni) bool {
	a.mu.Lock()
	ao := a.aktif[sunucu]
	a.mu.Unlock()
	if ao == nil {
		return false
	}
	return ao.gonder(&protokol.BaglantiIptal{BaglantiID: baglanti, Neden: neden}) == nil
}

// IsCalistir: İMZALI iş emri gönderir ve is_sonucu'nu bekler.
func (a *AjanSunucu) IsCalistir(ctx context.Context, sunucu, baglanti, oturum, arac string, param json.RawMessage, risk protokol.Risk) (*protokol.IsSonucu, error) {
	a.mu.Lock()
	ao := a.aktif[sunucu]
	a.mu.Unlock()
	if ao == nil {
		return nil, &protokol.Hata{Kod: protokol.HataBaglantiYok, Ayrinti: "ajan çevrimdışı"}
	}
	kod, err := protokol.YeniIsKodu(nil)
	if err != nil {
		return nil, err
	}
	emir := &protokol.IsEmri{IsKodu: kod, Oturum: oturum, BaglantiID: baglanti, Arac: arac, Param: bytes.Clone(param), Risk: risk}
	if err := a.m.imza.Imzala(emir, protokol.EnUzunGecerlilik); err != nil {
		return nil, err
	}
	ch := make(chan isYaniti, 1)
	ao.mu.Lock()
	if ao.kapandi {
		ao.mu.Unlock()
		return nil, &protokol.Hata{Kod: protokol.HataBaglantiYok}
	}
	ao.isler[kod] = ch
	ao.mu.Unlock()
	temizle := func() {
		ao.mu.Lock()
		delete(ao.isler, kod)
		ao.mu.Unlock()
	}
	if err := ao.gonder(emir); err != nil {
		temizle()
		return nil, &protokol.Hata{Kod: protokol.HataBaglantiYok}
	}
	t := time.NewTimer(a.m.yap.isZamanAsimi())
	defer t.Stop()
	select {
	case y := <-ch:
		switch {
		case y.kapandi:
			return nil, &protokol.Hata{Kod: protokol.HataBaglantiYok, Ayrinti: "ajan bağlantısı kapandı"}
		case y.hata != nil:
			return nil, &protokol.Hata{Kod: y.hata.Kod, Ayrinti: y.hata.Ayrinti}
		}
		return y.sonuc, nil
	case <-t.C:
		temizle()
		return nil, &protokol.Hata{Kod: protokol.HataZamanAsimi}
	case <-ctx.Done():
		temizle()
		return nil, ctx.Err()
	}
}

// ---- WSS ----

// WSIsle: GET protokol.WSSYolu.
func (a *AjanSunucu) WSIsle(w http.ResponseWriter, r *http.Request) {
	c, err := wsYukselt(w, r, protokol.WSAltProtokol, protokol.CerceveSiniri)
	if err != nil {
		return
	}
	reddet := func(kod protokol.HataKodu) {
		if b, err := protokol.Kodla(yeniKimlik(), &protokol.KayitSonuc{Kabul: false, HataKodu: kod}); err == nil {
			_ = c.MetinYaz(b)
		}
		c.Kapat(1008)
	}
	veri, err := c.Oku(time.Now().Add(kayitBeklemesi))
	if err != nil {
		c.Kapat(1002)
		return
	}
	_, g, err := protokol.Coz(veri, protokol.YonMerkeze)
	kay, ok := g.(*protokol.Kayit)
	if err != nil || !ok {
		reddet(hataKoduVarsayilan(err, protokol.HataBicim))
		return
	}
	if err := a.kayitDenetle(r.Context(), kay); err != nil {
		reddet(protokol.HataKoduAl(err))
		return
	}
	md, err := a.m.imza.MeydanOlustur(kay)
	if err != nil {
		reddet(protokol.HataIc)
		return
	}
	b, _ := protokol.Kodla(yeniKimlik(), md)
	if err := c.MetinYaz(b); err != nil {
		c.Kapat(1011)
		return
	}
	veri, err = c.Oku(time.Now().Add(protokol.MeydanGecerlilik))
	if err != nil {
		c.Kapat(1002)
		return
	}
	_, g, err = protokol.Coz(veri, protokol.YonMerkeze)
	y, ok := g.(*protokol.MeydanYanit)
	if err != nil || !ok {
		reddet(hataKoduVarsayilan(err, protokol.HataMeydan))
		return
	}
	sunucu, err := a.kimlikTamamla(kay, md, y)
	if err != nil {
		a.m.log.Warn("ajan kimliği doğrulanamadı", "hata", protokol.HataKoduAl(err))
		reddet(protokol.HataKoduAl(err))
		return
	}
	b, _ = protokol.Kodla(yeniKimlik(), &protokol.KayitSonuc{Kabul: true, SunucuKimligi: sunucu, KalpAraligiSn: int(protokol.KalpAraligi / time.Second)})
	if err := c.MetinYaz(b); err != nil {
		c.Kapat(1011)
		return
	}
	ao := &ajanOturumu{sunucu: sunucu, tasima: "wss", gonderF: c.MetinYaz, kapatF: func() { c.Kapat(1000) }, isler: map[string]chan isYaniti{}}
	a.oturumEkle(ao)
	defer a.oturumCikar(ao)

	dur := make(chan struct{})
	defer close(dur)
	go func() {
		t := time.NewTicker(protokol.KalpAraligi)
		defer t.Stop()
		for {
			select {
			case <-dur:
				return
			case <-t.C:
				ao.mu.Lock()
				ao.sira++
				s := ao.sira
				ao.mu.Unlock()
				if ao.gonder(&protokol.KalpAtisi{Sira: s, Zaman: a.m.saat().Unix()}) != nil {
					c.Kapat(1011)
					return
				}
			}
		}
	}()
	for {
		veri, err := c.Oku(time.Now().Add(protokol.KalpZamanAsimi))
		if err != nil {
			return
		}
		a.isle(ao, veri)
	}
}

func hataKoduVarsayilan(err error, v protokol.HataKodu) protokol.HataKodu {
	if err != nil {
		return protokol.HataKoduAl(err)
	}
	return v
}

// ---- uzun yoklama ----

func govdeOku(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "yöntem", http.StatusMethodNotAllowed)
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, protokol.CerceveSiniri+1))
	if err != nil || len(b) > protokol.CerceveSiniri {
		http.Error(w, "gövde", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return b, true
}

func zarfYaz(w http.ResponseWriter, durum int, g protokol.Govde) {
	b, err := protokol.Kodla(yeniKimlik(), g)
	if err != nil {
		http.Error(w, "iç hata", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(durum)
	_, _ = w.Write(b)
}

// YoklamaKayit: POST kayit → meydan_okuma.
func (a *AjanSunucu) YoklamaKayit(w http.ResponseWriter, r *http.Request) {
	veri, ok := govdeOku(w, r)
	if !ok {
		return
	}
	_, g, err := protokol.Coz(veri, protokol.YonMerkeze)
	kay, ok := g.(*protokol.Kayit)
	if err != nil || !ok {
		zarfYaz(w, http.StatusBadRequest, &protokol.KayitSonuc{HataKodu: hataKoduVarsayilan(err, protokol.HataBicim)})
		return
	}
	if err := a.kayitDenetle(r.Context(), kay); err != nil {
		zarfYaz(w, http.StatusForbidden, &protokol.KayitSonuc{HataKodu: protokol.HataKoduAl(err)})
		return
	}
	md, err := a.m.imza.MeydanOlustur(kay)
	if err != nil {
		zarfYaz(w, http.StatusInternalServerError, &protokol.KayitSonuc{HataKodu: protokol.HataIc})
		return
	}
	simdi := a.m.saat()
	a.mu.Lock()
	for k, v := range a.meydanlar {
		if !v.bitis.After(simdi) {
			delete(a.meydanlar, k)
		}
	}
	if len(a.meydanlar) >= meydanSiniri {
		a.mu.Unlock()
		zarfYaz(w, http.StatusServiceUnavailable, &protokol.KayitSonuc{HataKodu: protokol.HataMesgul})
		return
	}
	a.meydanlar[md.Meydan] = bekleyenMeydan{kayit: kay, meydan: md, bitis: time.Unix(md.SonGecerlilik, 0)}
	a.mu.Unlock()
	zarfYaz(w, http.StatusOK, md)
}

// YoklamaYanit: POST meydan_yanit → kayit_sonuc (oturum_belirteci).
func (a *AjanSunucu) YoklamaYanit(w http.ResponseWriter, r *http.Request) {
	veri, ok := govdeOku(w, r)
	if !ok {
		return
	}
	_, g, err := protokol.Coz(veri, protokol.YonMerkeze)
	y, ok := g.(*protokol.MeydanYanit)
	if err != nil || !ok {
		zarfYaz(w, http.StatusBadRequest, &protokol.KayitSonuc{HataKodu: hataKoduVarsayilan(err, protokol.HataBicim)})
		return
	}
	a.mu.Lock()
	bm, ok := a.meydanlar[y.Meydan]
	delete(a.meydanlar, y.Meydan) // tek kullanımlık
	a.mu.Unlock()
	if !ok {
		zarfYaz(w, http.StatusForbidden, &protokol.KayitSonuc{HataKodu: protokol.HataMeydan})
		return
	}
	sunucu, err := a.kimlikTamamla(bm.kayit, bm.meydan, y)
	if err != nil {
		zarfYaz(w, http.StatusForbidden, &protokol.KayitSonuc{HataKodu: protokol.HataKoduAl(err)})
		return
	}
	ham := make([]byte, 32)
	if _, err := rand.Read(ham); err != nil {
		zarfYaz(w, http.StatusInternalServerError, &protokol.KayitSonuc{HataKodu: protokol.HataIc})
		return
	}
	belirtec := base64.RawURLEncoding.EncodeToString(ham)
	yo := &yoklamaOturumu{kuyruk: make(chan []byte, yoklamaKuyrugu), son: a.m.saat()}
	ao := &ajanOturumu{sunucu: sunucu, tasima: "yoklama", isler: map[string]chan isYaniti{}}
	ao.gonderF = func(b []byte) error {
		select {
		case yo.kuyruk <- b:
			return nil
		default:
			return errors.New("yoklama kuyruğu dolu")
		}
	}
	ozet := belirtecOzeti(belirtec)
	ao.kapatF = func() {
		a.mu.Lock()
		delete(a.yoklamalar, ozet)
		a.mu.Unlock()
	}
	yo.ao = ao
	a.mu.Lock()
	a.yoklamalar[ozet] = yo
	a.mu.Unlock()
	a.oturumEkle(ao)
	zarfYaz(w, http.StatusOK, &protokol.KayitSonuc{Kabul: true, SunucuKimligi: sunucu, OturumBelirteci: belirtec, KalpAraligiSn: int(protokol.KalpAraligi / time.Second)})
}

func belirtecOzeti(b string) string {
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}

func (a *AjanSunucu) yoklamaBul(r *http.Request) *yoklamaOturumu {
	v := r.Header.Get(protokol.YoklamaBelirtecBasligi)
	if !strings.HasPrefix(v, "Bearer ") {
		return nil
	}
	ozet := belirtecOzeti(strings.TrimPrefix(v, "Bearer "))
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, yo := range a.yoklamalar {
		if subtle.ConstantTimeCompare([]byte(k), []byte(ozet)) == 1 {
			yo.son = a.m.saat()
			return yo
		}
	}
	return nil
}

// YoklamaAl: POST → merkez→ajan zarfları dizisi (en çok YoklamaBekleme bekler).
func (a *AjanSunucu) YoklamaAl(w http.ResponseWriter, r *http.Request) {
	if _, ok := govdeOku(w, r); !ok {
		return
	}
	yo := a.yoklamaBul(r)
	if yo == nil {
		http.Error(w, "belirteç", http.StatusUnauthorized)
		return
	}
	// Sözleşme: tek yanıtta en çok YoklamaAlEnCokZarf zarf ve YoklamaAlEnCokBayt bayt
	// (ajan fazlasını reddedip oturumu düşürür). Sığmayan zarf kaybolmaz: ertelenir.
	var cikti [][]byte
	boyut := 2
	ekle := func(b []byte) bool {
		if len(cikti) > 0 && (len(cikti) >= protokol.YoklamaAlEnCokZarf || boyut+len(b)+1 > protokol.YoklamaAlEnCokBayt) {
			return false
		}
		cikti = append(cikti, b)
		boyut += len(b) + 1
		return true
	}
	yo.emu.Lock()
	if yo.ertelenen != nil {
		ekle(yo.ertelenen)
		yo.ertelenen = nil
	}
	yo.emu.Unlock()
	if len(cikti) == 0 {
		t := time.NewTimer(protokol.YoklamaBekleme)
		select {
		case b := <-yo.kuyruk:
			ekle(b)
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return
		}
		t.Stop()
	}
doldur:
	for {
		select {
		case b := <-yo.kuyruk:
			if !ekle(b) {
				yo.emu.Lock()
				yo.ertelenen = b
				yo.emu.Unlock()
				break doldur
			}
		default:
			break doldur
		}
	}
	var out bytes.Buffer
	out.WriteByte('[')
	for i, b := range cikti {
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(b)
	}
	out.WriteByte(']')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Bytes())
}

// YoklamaGonder: POST ajan→merkez zarfı.
func (a *AjanSunucu) YoklamaGonder(w http.ResponseWriter, r *http.Request) {
	veri, ok := govdeOku(w, r)
	if !ok {
		return
	}
	yo := a.yoklamaBul(r)
	if yo == nil {
		http.Error(w, "belirteç", http.StatusUnauthorized)
		return
	}
	a.isle(yo.ao, veri)
	w.WriteHeader(http.StatusNoContent)
}

// yoklamaBuda: KalpZamanAsimi boyunca /al ya da /gonder görmeyen yoklama oturumları.
func (a *AjanSunucu) yoklamaBuda() {
	simdi := a.m.saat()
	var kapat []*ajanOturumu
	a.mu.Lock()
	for k, yo := range a.yoklamalar {
		if simdi.Sub(yo.son) > protokol.KalpZamanAsimi+protokol.YoklamaBekleme {
			delete(a.yoklamalar, k)
			kapat = append(kapat, yo.ao)
		}
	}
	for k, v := range a.eslesmeler {
		if !v.bitis.After(simdi) {
			delete(a.eslesmeler, k)
		}
	}
	a.mu.Unlock()
	for _, ao := range kapat {
		a.oturumCikar(ao)
	}
}
