package protokol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Tip: zarftaki mesaj tipi.
type Tip string

const (
	TipKayit         Tip = "kayit"          // ajan → merkez
	TipMeydanOkuma   Tip = "meydan_okuma"   // merkez → ajan (merkez imzalı)
	TipMeydanYanit   Tip = "meydan_yanit"   // ajan → merkez (cihaz imzalı)
	TipKayitSonuc    Tip = "kayit_sonuc"    // merkez → ajan
	TipKalpAtisi     Tip = "kalp_atisi"     // iki yön
	TipKodOzet       Tip = "kod_ozet"       // ajan → merkez
	TipEslesmeOnayla Tip = "eslesme_onayla" // merkez → ajan (imzalı)
	TipBaglantiDurum Tip = "baglanti_durum" // ajan → merkez
	TipBaglantiIptal Tip = "baglanti_iptal" // iki yön
	TipIsEmri        Tip = "is_emri"        // merkez → ajan (imzalı)
	TipIsSonucu      Tip = "is_sonucu"      // ajan → merkez
	TipHata          Tip = "hata"           // iki yön
)

// Yon: mesajın gidebileceği taraf.
type Yon uint8

const (
	YonMerkeze Yon = 1 << iota // ajan → merkez
	YonAjana                   // merkez → ajan
)

var tipYonleri = map[Tip]Yon{
	TipKayit:         YonMerkeze,
	TipMeydanOkuma:   YonAjana,
	TipMeydanYanit:   YonMerkeze,
	TipKayitSonuc:    YonAjana,
	TipKalpAtisi:     YonMerkeze | YonAjana,
	TipKodOzet:       YonMerkeze,
	TipEslesmeOnayla: YonAjana,
	TipBaglantiDurum: YonMerkeze,
	TipBaglantiIptal: YonMerkeze | YonAjana,
	TipIsEmri:        YonAjana,
	TipIsSonucu:      YonMerkeze,
	TipHata:          YonMerkeze | YonAjana,
}

// Yonu: tipin izinli yön(ler)i; bilinmeyen tip → 0.
func (t Tip) Yonu() Yon { return tipYonleri[t] }

// Govde: zarfın "govde" alanına giden her mesaj.
type Govde interface {
	Tip() Tip
	Dogrula() error
}

// Zarf: kablodaki tek çerçeve.
type Zarf struct {
	Surum  int             `json:"surum"`
	Tip    Tip             `json:"tip"`
	Kimlik string          `json:"kimlik"` // mesaj kimliği; yanıt/hata ilişkilendirmesi için
	Govde  json.RawMessage `json:"govde"`
}

func yeniGovde(t Tip) Govde {
	switch t {
	case TipKayit:
		return &Kayit{}
	case TipMeydanOkuma:
		return &MeydanOkuma{}
	case TipMeydanYanit:
		return &MeydanYanit{}
	case TipKayitSonuc:
		return &KayitSonuc{}
	case TipKalpAtisi:
		return &KalpAtisi{}
	case TipKodOzet:
		return &KodOzet{}
	case TipEslesmeOnayla:
		return &EslesmeOnayla{}
	case TipBaglantiDurum:
		return &BaglantiDurum{}
	case TipBaglantiIptal:
		return &BaglantiIptal{}
	case TipIsEmri:
		return &IsEmri{}
	case TipIsSonucu:
		return &IsSonucu{}
	case TipHata:
		return &HataMesaji{}
	}
	return nil
}

// Kodla: gövdeyi doğrular ve zarflanmış JSON çerçevesi üretir.
func Kodla(kimlik string, g Govde) ([]byte, error) {
	if !kimlikDeseni.MatchString(kimlik) {
		return nil, hataf(HataBicim, "kimlik")
	}
	if g == nil || g.Tip().Yonu() == 0 {
		return nil, hataf(HataTipBilinmiyor, "gövde")
	}
	if err := g.Dogrula(); err != nil {
		return nil, err
	}
	gb, err := json.Marshal(g)
	if err != nil {
		return nil, hataf(HataBicim, "gövde: %v", err)
	}
	b, err := json.Marshal(Zarf{Surum: Surum, Tip: g.Tip(), Kimlik: kimlik, Govde: gb})
	if err != nil {
		return nil, hataf(HataBicim, "zarf: %v", err)
	}
	if len(b) > CerceveSiniri {
		return nil, hataf(HataCerceveBuyuk, "%d bayt", len(b))
	}
	return b, nil
}

// Coz: çerçeveyi katı çözer. gelenYon, çözen tarafın ALDIĞI yöndür
// (ajan YonAjana, merkez YonMerkeze verir); ters yönlü tip reddedilir.
// İmzalı gövdelerde imza burada DOĞRULANMAZ — [Dogrulayici.Dogrula] çağrılmalı.
func Coz(veri []byte, gelenYon Yon) (*Zarf, Govde, error) {
	if len(veri) > CerceveSiniri {
		return nil, nil, hataf(HataCerceveBuyuk, "%d bayt", len(veri))
	}
	// Sürüm, katı çözümlemeden ÖNCE: yeni sürümün ek alanları "bilinmeyen alan" değil "sürüm" hatası versin.
	var on struct {
		Surum json.RawMessage `json:"surum"`
	}
	if err := json.Unmarshal(veri, &on); err != nil {
		return nil, nil, hataf(HataBicim, "zarf: %v", err)
	}
	if string(on.Surum) != "1" {
		return nil, nil, hataf(HataSurum, "surum=%s", string(on.Surum))
	}
	var z Zarf
	if err := katiCoz(veri, &z); err != nil {
		return nil, nil, err
	}
	y := z.Tip.Yonu()
	if y == 0 {
		return nil, nil, hataf(HataTipBilinmiyor, "%q", z.Tip)
	}
	if y&gelenYon == 0 {
		return nil, nil, hataf(HataYon, "%q bu yönde kabul edilmez", z.Tip)
	}
	if !kimlikDeseni.MatchString(z.Kimlik) {
		return nil, nil, hataf(HataBicim, "kimlik")
	}
	if len(bytes.TrimSpace(z.Govde)) == 0 || string(bytes.TrimSpace(z.Govde)) == "null" {
		return nil, nil, hataf(HataBicim, "govde yok")
	}
	g := yeniGovde(z.Tip)
	if err := katiCoz(z.Govde, g); err != nil {
		return nil, nil, err
	}
	if err := g.Dogrula(); err != nil {
		return nil, nil, err
	}
	return &z, g, nil
}

// ---- ortak biçimler ----

var (
	kimlikDeseni      = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	hex64Deseni       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	anahtarKimDeseni  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	isKoduDeseni      = regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{8}$`)
	aracAdiDeseni     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	panelSurumDeseni  = regexp.MustCompile(`^[0-9A-Za-z.+_-]{1,64}$`)
	tgAdDeseni        = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)
	hesapEtiketDeseni = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)
	kanitDeseni       = regexp.MustCompile(`^[a-z0-9]{1,16}:[A-Za-z0-9_.:-]{1,180}$`)
)

// IsKoduAlfabesi: insanın yazacağı iş kodu (ONAYLA <kod>) — karışan I/L/O/0/1 yok.
const IsKoduAlfabesi = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

var b64 = base64.RawURLEncoding.Strict()

// b64Coz: dolgusuz base64url, kanonik; çözülmüş uzunluk [en, enCok].
func b64Coz(alan, s string, en, enCok int) ([]byte, error) {
	b, err := b64.DecodeString(s)
	if err != nil {
		return nil, hataf(HataBicim, "%s: base64url değil", alan)
	}
	if len(b) < en || len(b) > enCok {
		return nil, hataf(HataBicim, "%s: %d bayt (beklenen %d..%d)", alan, len(b), en, enCok)
	}
	return b, nil
}

func desenDenetle(alan string, d *regexp.Regexp, s string) error {
	if !d.MatchString(s) {
		return hataf(HataBicim, "%s", alan)
	}
	return nil
}

// yasakRune: protokol metinlerinde kabul edilmeyen kontrol ve yön değiştirme karakterleri.
func yasakRune(r rune) bool {
	switch {
	case r == '\t':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f:
		return true
	case r == utf8.RuneError:
		return true
	}
	return false
}

func metinDenetle(alan, s string, enCok int) error {
	if len(s) > enCok {
		return hataf(HataBicim, "%s: %d bayt > %d", alan, len(s), enCok)
	}
	if !utf8.ValidString(s) {
		return hataf(HataBicim, "%s: UTF-8 değil", alan)
	}
	for _, r := range s {
		if yasakRune(r) {
			return hataf(HataBicim, "%s: yasak karakter %U", alan, r)
		}
	}
	return nil
}

// SatirTemizle: ajanın is_sonucu satırı üretirken kullanacağı yardımcı —
// yasak karakterleri '?' yapar, SatirSiniri'na rune sınırında keser.
func SatirTemizle(s string) (string, bool) {
	var b strings.Builder
	kesildi := false
	for _, r := range strings.ToValidUTF8(s, "?") {
		if yasakRune(r) {
			r = '?'
		}
		if b.Len()+utf8.RuneLen(r) > SatirSiniri {
			kesildi = true
			break
		}
		b.WriteRune(r)
	}
	return b.String(), kesildi
}

// ---- gövdeler ----

// Kayit: ajan → merkez, bağlantının ilk mesajı.
type Kayit struct {
	CihazAnahtari   string `json:"cihaz_anahtari"`    // Ed25519 açık anahtar, base64url (32 bayt)
	LisansParmakIzi string `json:"lisans_parmak_izi"` // lisans parmak izi, 64 küçük hex (sha256)
	LisansIspati    string `json:"lisans_ispati"`     // lisans sunucusunun verdiği ispat, base64url (opak; merkez lic-eu ile doğrular)
	PanelSurumu     string `json:"panel_surumu"`
	KatalogOzeti    string `json:"katalog_ozeti"` // ajanın derlendiği KatalogOzeti(); uyuşmazlıkta merkez surum_desteklenmiyor döner
}

func (*Kayit) Tip() Tip { return TipKayit }
func (m *Kayit) Dogrula() error {
	if _, err := b64Coz("cihaz_anahtari", m.CihazAnahtari, 32, 32); err != nil {
		return err
	}
	if _, err := b64Coz("lisans_ispati", m.LisansIspati, 1, LisansIspatSiniri); err != nil {
		return err
	}
	if err := desenDenetle("lisans_parmak_izi", hex64Deseni, m.LisansParmakIzi); err != nil {
		return err
	}
	if err := desenDenetle("panel_surumu", panelSurumDeseni, m.PanelSurumu); err != nil {
		return err
	}
	return desenDenetle("katalog_ozeti", hex64Deseni, m.KatalogOzeti)
}

// MeydanOkuma: merkez → ajan. merkez_imza, merkez anahtarıyla [MeydanOkuma]+[Kayit] kanonik baytları üzerindedir.
type MeydanOkuma struct {
	Meydan               string `json:"meydan"` // 32 bayt rastgele, base64url
	SonGecerlilik        int64  `json:"son_gecerlilik"`
	MerkezAnahtarKimligi string `json:"merkez_anahtar_kimligi"`
	MerkezImza           string `json:"merkez_imza"`
}

func (*MeydanOkuma) Tip() Tip { return TipMeydanOkuma }
func (m *MeydanOkuma) Dogrula() error {
	if _, err := b64Coz("meydan", m.Meydan, 32, 32); err != nil {
		return err
	}
	if m.SonGecerlilik <= 0 {
		return hataf(HataBicim, "son_gecerlilik")
	}
	if err := desenDenetle("merkez_anahtar_kimligi", anahtarKimDeseni, m.MerkezAnahtarKimligi); err != nil {
		return err
	}
	_, err := b64Coz("merkez_imza", m.MerkezImza, 64, 64)
	return err
}

// MeydanYanit: ajan → merkez; imza cihaz anahtarıyla.
type MeydanYanit struct {
	Meydan string `json:"meydan"`
	Imza   string `json:"imza"`
}

func (*MeydanYanit) Tip() Tip { return TipMeydanYanit }
func (m *MeydanYanit) Dogrula() error {
	if _, err := b64Coz("meydan", m.Meydan, 32, 32); err != nil {
		return err
	}
	_, err := b64Coz("imza", m.Imza, 64, 64)
	return err
}

// KayitSonuc: merkez → ajan.
type KayitSonuc struct {
	Kabul           bool     `json:"kabul"`
	SunucuKimligi   string   `json:"sunucu_kimligi"`   // merkezin bu sunucuya verdiği kimlik
	OturumBelirteci string   `json:"oturum_belirteci"` // yalnız uzun yoklamada: 32 bayt base64url; WSS'de boş
	KalpAraligiSn   int      `json:"kalp_araligi_sn"`
	HataKodu        HataKodu `json:"hata_kodu"`
}

func (*KayitSonuc) Tip() Tip { return TipKayitSonuc }
func (m *KayitSonuc) Dogrula() error {
	if !m.Kabul {
		if !m.HataKodu.Gecerli() {
			return hataf(HataBicim, "red için hata_kodu")
		}
		if m.SunucuKimligi != "" || m.OturumBelirteci != "" {
			return hataf(HataBicim, "redde kimlik/belirteç olmaz")
		}
		return nil
	}
	if m.HataKodu != "" {
		return hataf(HataBicim, "kabulde hata_kodu olmaz")
	}
	if err := desenDenetle("sunucu_kimligi", kimlikDeseni, m.SunucuKimligi); err != nil {
		return err
	}
	if m.OturumBelirteci != "" {
		if _, err := b64Coz("oturum_belirteci", m.OturumBelirteci, 32, 32); err != nil {
			return err
		}
	}
	if m.KalpAraligiSn < 5 || m.KalpAraligiSn > 120 {
		return hataf(HataBicim, "kalp_araligi_sn")
	}
	return nil
}

// KalpAtisi: iki yön.
type KalpAtisi struct {
	Sira  uint64 `json:"sira"`
	Zaman int64  `json:"zaman"` // unix sn
}

func (*KalpAtisi) Tip() Tip { return TipKalpAtisi }
func (m *KalpAtisi) Dogrula() error {
	if m.Zaman <= 0 {
		return hataf(HataBicim, "zaman")
	}
	return nil
}

// KodOzet: ajan → merkez; panelde üretilen tek kullanımlık eşleştirme kodunun SHA-256 özeti.
// Kodun kendisi sunucudan çıkmaz.
type KodOzet struct {
	KodOzet       string `json:"kod_ozet"`
	SonGecerlilik int64  `json:"son_gecerlilik"`
}

func (*KodOzet) Tip() Tip { return TipKodOzet }
func (m *KodOzet) Dogrula() error {
	if m.SonGecerlilik <= 0 {
		return hataf(HataBicim, "son_gecerlilik")
	}
	return desenDenetle("kod_ozet", hex64Deseni, m.KodOzet)
}

// ImzaAlanlari: imzalı mesajlarda ortak alanlar (JSON'da düz, gömülü).
type ImzaAlanlari struct {
	AnahtarKimligi string `json:"anahtar_kimligi"` // AnahtarKimligi(merkez açık anahtarı)
	Nonce          string `json:"nonce"`           // 16..32 bayt base64url
	Verilme        int64  `json:"verilme"`         // unix sn
	SonGecerlilik  int64  `json:"son_gecerlilik"`  // unix sn; − verilme ≤ 60
	Imza           string `json:"imza"`            // Ed25519, base64url (64 bayt)
}

func (a *ImzaAlanlari) imza() *ImzaAlanlari { return a }

func (a *ImzaAlanlari) bicimDogrula() error {
	if err := desenDenetle("anahtar_kimligi", anahtarKimDeseni, a.AnahtarKimligi); err != nil {
		return err
	}
	if _, err := b64Coz("nonce", a.Nonce, 16, 32); err != nil {
		return err
	}
	if a.Verilme <= 0 || a.SonGecerlilik <= 0 {
		return hataf(HataBicim, "verilme/son_gecerlilik")
	}
	_, err := b64Coz("imza", a.Imza, 64, 64)
	return err
}

// EslesmeOnayla: merkez → ajan (İMZALI). Telegram kullanıcısı /start <kod> ile kodu sundu.
type EslesmeOnayla struct {
	KodOzet        string `json:"kod_ozet"`
	TgUserID       int64  `json:"tg_user_id"`
	TgKullaniciAdi string `json:"tg_kullanici_adi"` // boş olabilir (kullanıcı adı olmayan hesap)
	ImzaAlanlari
}

func (*EslesmeOnayla) Tip() Tip { return TipEslesmeOnayla }
func (m *EslesmeOnayla) Dogrula() error {
	if err := desenDenetle("kod_ozet", hex64Deseni, m.KodOzet); err != nil {
		return err
	}
	if m.TgUserID <= 0 {
		return hataf(HataBicim, "tg_user_id")
	}
	if m.TgKullaniciAdi != "" {
		if err := desenDenetle("tg_kullanici_adi", tgAdDeseni, m.TgKullaniciAdi); err != nil {
			return err
		}
	}
	return m.bicimDogrula()
}

// Rol: bağlantının panel rolü.
type Rol string

const (
	RolMusteri Rol = "musteri"
	RolBayi    Rol = "bayi"
	RolKok     Rol = "kok"
)

func (r Rol) Gecerli() bool { return r == RolMusteri || r == RolBayi || r == RolKok }

// Risk: araç risk sınıfı. R3 (yasak) protokolde temsil EDİLMEZ.
type Risk string

const (
	R0 Risk = "R0" // okuma
	R1 Risk = "R1" // güvenli onarım
	R2 Risk = "R2" // kritik, ön-yedek zorunlu
)

func (r Risk) Gecerli() bool { return r == R0 || r == R1 || r == R2 }

// Duzey: bağlantının etkin yetki düzeyi = min(kök şalteri, bayi izni, müşteri bağlantısı).
type Duzey string

const (
	DuzeyKapali Duzey = "kapali"
	DuzeyOkuma  Duzey = "okuma"  // R0
	DuzeyOnarim Duzey = "onarim" // R0+R1
	DuzeyTam    Duzey = "tam"    // R0+R1+R2
)

func (d Duzey) Gecerli() bool {
	return d == DuzeyKapali || d == DuzeyOkuma || d == DuzeyOnarim || d == DuzeyTam
}

// IzinVerir: düzey bu riski kapsıyor mu.
func (d Duzey) IzinVerir(r Risk) bool {
	switch d {
	case DuzeyOkuma:
		return r == R0
	case DuzeyOnarim:
		return r == R0 || r == R1
	case DuzeyTam:
		return r == R0 || r == R1 || r == R2
	}
	return false
}

// BaglantiDurumu: eşleştirme bağlantısının hali.
type BaglantiDurumu string

const (
	BaglantiOnayBekliyor BaglantiDurumu = "onay_bekliyor"
	BaglantiEtkin        BaglantiDurumu = "etkin"
	BaglantiIptalEdildi  BaglantiDurumu = "iptal"
)

// HesapEtiketi: oturumdaki hesabın sıra numarası (1'den) ve görünen etiketi (alan adı).
type HesapEtiketi struct {
	Sira   int    `json:"sira"`
	Etiket string `json:"etiket"`
}

// BaglantiDurum: ajan → merkez. Kapsamın YEREL otoritesi ajandır; merkez bu listeyi yalnız gösterir.
type BaglantiDurum struct {
	BaglantiID string         `json:"baglanti_id"`
	KodOzet    string         `json:"kod_ozet"` // eşleştirmeden doğduysa; aksi halde boş
	TgUserID   int64          `json:"tg_user_id"`
	Durum      BaglantiDurumu `json:"durum"`
	Rol        Rol            `json:"rol"`
	Duzey      Duzey          `json:"duzey"`
	Hesaplar   []HesapEtiketi `json:"hesaplar"`
}

func (*BaglantiDurum) Tip() Tip { return TipBaglantiDurum }
func (m *BaglantiDurum) Dogrula() error {
	if err := desenDenetle("baglanti_id", kimlikDeseni, m.BaglantiID); err != nil {
		return err
	}
	if m.KodOzet != "" {
		if err := desenDenetle("kod_ozet", hex64Deseni, m.KodOzet); err != nil {
			return err
		}
	}
	if m.TgUserID <= 0 {
		return hataf(HataBicim, "tg_user_id")
	}
	switch m.Durum {
	case BaglantiOnayBekliyor, BaglantiEtkin, BaglantiIptalEdildi:
	default:
		return hataf(HataBicim, "durum")
	}
	if !m.Rol.Gecerli() || !m.Duzey.Gecerli() {
		return hataf(HataBicim, "rol/duzey")
	}
	if len(m.Hesaplar) > EnCokHesap {
		return hataf(HataBicim, "hesaplar > %d", EnCokHesap)
	}
	for i, h := range m.Hesaplar {
		if h.Sira != i+1 {
			return hataf(HataBicim, "hesaplar[%d].sira %d (beklenen %d)", i, h.Sira, i+1)
		}
		if err := desenDenetle("hesaplar.etiket", hesapEtiketDeseni, h.Etiket); err != nil {
			return err
		}
	}
	return nil
}

// IptalNedeni: baglanti_iptal nedeni.
type IptalNedeni string

const (
	IptalKullanici   IptalNedeni = "kullanici"    // panelden kaldırıldı
	IptalUnutBeni    IptalNedeni = "unutbeni"     // Telegram /unutbeni
	IptalFTPParola   IptalNedeni = "ftp_parola"   // FTP parola değişimi
	IptalAlanSilme   IptalNedeni = "alan_silme"   // domain silme/askı
	IptalBayi        IptalNedeni = "bayi"         // bayi askı/silme/parola sıfırlama
	IptalIkiFA       IptalNedeni = "iki_fa"       // 2FA kapatma
	IptalSuresiDoldu IptalNedeni = "suresi_doldu" // onay süresi doldu
	IptalMerkez      IptalNedeni = "merkez"       // merkez (kötüye kullanım, hız)
)

var iptalNedenleri = []IptalNedeni{IptalKullanici, IptalUnutBeni, IptalFTPParola, IptalAlanSilme, IptalBayi, IptalIkiFA, IptalSuresiDoldu, IptalMerkez}

// BaglantiIptal: iki yön; alıcı bağlantıyı derhal kapsam dışı sayar.
type BaglantiIptal struct {
	BaglantiID string      `json:"baglanti_id"`
	Neden      IptalNedeni `json:"neden"`
}

func (*BaglantiIptal) Tip() Tip { return TipBaglantiIptal }
func (m *BaglantiIptal) Dogrula() error {
	if err := desenDenetle("baglanti_id", kimlikDeseni, m.BaglantiID); err != nil {
		return err
	}
	for _, n := range iptalNedenleri {
		if n == m.Neden {
			return nil
		}
	}
	return hataf(HataBicim, "neden")
}

// IsEmri: merkez → ajan (İMZALI). Param, aracın girdi nesnesi (ham JSON);
// ajan bunu [AracGirdisiCoz] ile KENDİ oturum bağlamına göre doğrular.
type IsEmri struct {
	IsKodu     string          `json:"is_kodu"`
	Oturum     string          `json:"oturum"`
	BaglantiID string          `json:"baglanti_id"`
	Arac       string          `json:"arac"`
	Param      json.RawMessage `json:"param"`
	Risk       Risk            `json:"risk"`
	ImzaAlanlari
}

func (*IsEmri) Tip() Tip { return TipIsEmri }
func (m *IsEmri) Dogrula() error {
	if err := desenDenetle("is_kodu", isKoduDeseni, m.IsKodu); err != nil {
		return err
	}
	if err := desenDenetle("oturum", kimlikDeseni, m.Oturum); err != nil {
		return err
	}
	if err := desenDenetle("baglanti_id", kimlikDeseni, m.BaglantiID); err != nil {
		return err
	}
	if err := desenDenetle("arac", aracAdiDeseni, m.Arac); err != nil {
		return err
	}
	if !m.Risk.Gecerli() {
		return hataf(HataBicim, "risk")
	}
	p := bytes.TrimSpace(m.Param)
	if len(p) == 0 || len(p) > ParamSiniri || p[0] != '{' || !json.Valid(p) {
		return hataf(HataBicim, "param")
	}
	return m.bicimDogrula()
}

// IsSonucu: ajan → merkez. Satırlar MASKELİ ve kesiktir; ham çıktı sunucudan çıkmaz.
type IsSonucu struct {
	IsKodu   string   `json:"is_kodu"`
	Ok       bool     `json:"ok"`
	Ozet     string   `json:"ozet"`
	Satirlar []string `json:"satirlar"`
	Kesildi  bool     `json:"kesildi"`
	Kanit    string   `json:"kanit"` // "tur:deger" (ör. denetim:123, sha256:…); boş olabilir
	HataKodu HataKodu `json:"hata_kodu"`
}

func (*IsSonucu) Tip() Tip { return TipIsSonucu }
func (m *IsSonucu) Dogrula() error {
	if err := desenDenetle("is_kodu", isKoduDeseni, m.IsKodu); err != nil {
		return err
	}
	if m.Ok == (m.HataKodu != "") {
		return hataf(HataBicim, "ok ile hata_kodu çelişiyor")
	}
	if !m.Ok && !m.HataKodu.Gecerli() {
		return hataf(HataBicim, "hata_kodu")
	}
	if err := metinDenetle("ozet", m.Ozet, OzetSiniri); err != nil {
		return err
	}
	if len(m.Satirlar) > SatirSayisiSiniri {
		return hataf(HataBicim, "satirlar > %d", SatirSayisiSiniri)
	}
	for _, s := range m.Satirlar {
		if err := metinDenetle("satirlar", s, SatirSiniri); err != nil {
			return err
		}
	}
	if m.Kanit != "" {
		return desenDenetle("kanit", kanitDeseni, m.Kanit)
	}
	return nil
}

// HataMesaji: iki yön; Ilgili, ilgili zarf kimliği ya da iş kodu.
type HataMesaji struct {
	Kod     HataKodu `json:"kod"`
	Ayrinti string   `json:"ayrinti"`
	Ilgili  string   `json:"ilgili"`
}

func (*HataMesaji) Tip() Tip { return TipHata }
func (m *HataMesaji) Dogrula() error {
	if !m.Kod.Gecerli() {
		return hataf(HataBicim, "kod")
	}
	if err := metinDenetle("ayrinti", m.Ayrinti, AyrintiSiniri); err != nil {
		return err
	}
	if m.Ilgili != "" && !kimlikDeseni.MatchString(m.Ilgili) {
		return hataf(HataBicim, "ilgili")
	}
	return nil
}
