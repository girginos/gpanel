package merkez

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/destek/protokol"
)

// Fiyat: bir modelin milyon token başına USD fiyatları. KODDA SABİT FİYAT YOKTUR;
// tablo yapılandırmadan gelir (fiyat değişince yalnız yapılandırma güncellenir).
type Fiyat struct {
	GirisMTok         float64 `json:"giris_mtok"`
	CikisMTok         float64 `json:"cikis_mtok"`
	CacheYazma5dkMTok float64 `json:"cache_yazma_5dk_mtok"`
	CacheYazma1saMTok float64 `json:"cache_yazma_1sa_mtok"`
	CacheOkumaMTok    float64 `json:"cache_okuma_mtok"`
}

// Modeller: görev başına model kimliği.
type Modeller struct {
	Triyaj    string `json:"triyaj"`    // niyet/dil/selam/konu dışı + çıkış güvenlik ikinci bakış
	Ana       string `json:"ana"`       // teşhis araç döngüsü
	Yukseltme string `json:"yukseltme"` // ana model tur sınırında çözemezse; boş = yükseltme yok
}

// Butce: maliyet tavanları. Aşımda Claude çağrılmaz, kullanıcı insan desteğine aktarılır.
type Butce struct {
	OturumTokenTavani int64   `json:"oturum_token_tavani"` // ücretli eşdeğer token (bkz. Oturum.tokenEkle)
	SunucuGunlukUSD   float64 `json:"sunucu_gunluk_usd"`
	SunucuAylikUSD    float64 `json:"sunucu_aylik_usd"`
	GenelGunlukUSD    float64 `json:"genel_gunluk_usd"`
	GenelAylikUSD     float64 `json:"genel_aylik_usd"`
}

// Yapilandirma: /etc/girginos-destek/destek-hub.json (katı çözülür; bilinmeyen alan → ret).
// Sır İÇERMEZ.
type Yapilandirma struct {
	Dinle   string `json:"dinle"`    // nginx arkası, ör. 127.0.0.1:8470
	IcDinle string `json:"ic_dinle"` // /saglik + /metrik + /ic/v1/* (portal), ör. 127.0.0.1:8471; boş = kapalı; YALNIZ loopback
	// PortalBiletAnahtarlari: portalın (lisans sunucusu) bilet imza açık anahtarları, base64url
	// (32 bayt Ed25519). Boşsa hiçbir sunucu "doğrulanmış" olamaz → eşleştirme kodu verilmez (kapalı başarısız).
	// Birden çok anahtar = anahtar döndürme.
	PortalBiletAnahtarlari []string         `json:"portal_bilet_anahtarlari"`
	VeriDizini             string           `json:"veri_dizini"`
	BotKullaniciAdi        string           `json:"bot_kullanici_adi"`
	Modeller               Modeller         `json:"modeller"`
	Fiyatlar               map[string]Fiyat `json:"fiyatlar"`
	Butce                  Butce            `json:"butce"`
	AcilKesiciDosyasi      string           `json:"acil_kesici_dosyasi"` // dosya VARSA tüm Claude çağrıları durur
	DestekGrubuID          int64            `json:"destek_grubu_id"`     // insan devri grubu; 0 = yalnız log
	CacheTTL               string           `json:"cache_ttl"`           // "5m" (varsayılan) | "1h"
	CacheUyariEsigi        float64          `json:"cache_uyari_esigi"`   // isabet oranı bunun altına düşerse uyarı (varsayılan 0.5)
	CacheUyariPencere      int              `json:"cache_uyari_pencere"` // son N ana model çağrısı (varsayılan 20)
	AnaMaxToken            int64            `json:"ana_max_token"`
	TriyajMaxToken         int64            `json:"triyaj_max_token"`
	CikisKontroluKapali    bool             `json:"cikis_kontrolu_kapali"`
	Efor                   string           `json:"efor"`    // output_config.effort: "" | low | medium | high | max
	Dusunme                bool             `json:"dusunme"` // thinking: adaptive
	SikistirmaEsigi        int64            `json:"sikistirma_esigi"`
	KonusmaSaklamaGun      int              `json:"konusma_saklama_gun"`
	AracTurSiniri          int              `json:"arac_tur_siniri"`
	OturumAracSiniri       int              `json:"oturum_arac_siniri"`
	YukseltmeTurSiniri     int              `json:"yukseltme_tur_siniri"`
	MesajDakikaSiniri      int              `json:"mesaj_dakika_siniri"`
	SunucuOturumSiniri     int              `json:"sunucu_oturum_siniri"`
	OturumBostaSn          int              `json:"oturum_bosta_sn"`
	IsZamanAsimiSn         int              `json:"is_zaman_asimi_sn"`
	AnthropicTabanURL      string           `json:"anthropic_taban_url"` // boş = SDK varsayılanı
	TelegramTabanURL       string           `json:"telegram_taban_url"`  // boş = https://api.telegram.org
}

// Varsayilanlar: sıfır alanları doldurur (fiyat ve bütçe HARİÇ — onlar zorunludur).
func (y *Yapilandirma) Varsayilanlar() {
	if y.Dinle == "" {
		y.Dinle = "127.0.0.1:8470"
	}
	if y.CacheTTL == "" {
		y.CacheTTL = "5m"
	}
	if y.CacheUyariEsigi == 0 {
		y.CacheUyariEsigi = 0.5
	}
	if y.CacheUyariPencere == 0 {
		y.CacheUyariPencere = 20
	}
	if y.AnaMaxToken == 0 {
		y.AnaMaxToken = 4096
	}
	if y.TriyajMaxToken == 0 {
		y.TriyajMaxToken = 256
	}
	if y.SikistirmaEsigi == 0 {
		y.SikistirmaEsigi = 40000
	}
	if y.KonusmaSaklamaGun == 0 {
		y.KonusmaSaklamaGun = 30
	}
	if y.AracTurSiniri == 0 {
		y.AracTurSiniri = 8
	}
	if y.OturumAracSiniri == 0 {
		y.OturumAracSiniri = 25
	}
	if y.YukseltmeTurSiniri == 0 {
		y.YukseltmeTurSiniri = 4
	}
	if y.MesajDakikaSiniri == 0 {
		y.MesajDakikaSiniri = 10
	}
	if y.SunucuOturumSiniri == 0 {
		y.SunucuOturumSiniri = 2
	}
	if y.OturumBostaSn == 0 {
		y.OturumBostaSn = 900
	}
	if y.IsZamanAsimiSn == 0 {
		y.IsZamanAsimiSn = 45
	}
}

func (y *Yapilandirma) oturumBosta() time.Duration {
	return time.Duration(y.OturumBostaSn) * time.Second
}
func (y *Yapilandirma) isZamanAsimi() time.Duration {
	return time.Duration(y.IsZamanAsimiSn) * time.Second
}

// Dogrula: tutarlılık. Fiyatı olmayan model ve sıfır bütçe başlangıçta reddedilir (kapalı başarısız).
func (y *Yapilandirma) Dogrula() error {
	if y.VeriDizini != "" && !filepath.IsAbs(y.VeriDizini) {
		return errors.New("veri_dizini mutlak yol olmalı")
	}
	if y.Modeller.Triyaj == "" || y.Modeller.Ana == "" {
		return errors.New("modeller.triyaj ve modeller.ana zorunlu")
	}
	for _, m := range []string{y.Modeller.Triyaj, y.Modeller.Ana, y.Modeller.Yukseltme} {
		if m == "" {
			continue
		}
		f, ok := y.Fiyatlar[m]
		if !ok {
			return fmt.Errorf("fiyatlar: %q modeli için fiyat yok", m)
		}
		if f.GirisMTok <= 0 || f.CikisMTok <= 0 || f.CacheOkumaMTok <= 0 || f.CacheYazma5dkMTok <= 0 || f.CacheYazma1saMTok <= 0 {
			return fmt.Errorf("fiyatlar.%s: tüm fiyatlar > 0 olmalı", m)
		}
	}
	b := y.Butce
	if b.OturumTokenTavani <= 0 || b.SunucuGunlukUSD <= 0 || b.SunucuAylikUSD <= 0 || b.GenelGunlukUSD <= 0 || b.GenelAylikUSD <= 0 {
		return errors.New("butce: tüm tavanlar > 0 olmalı")
	}
	switch y.Efor {
	case "", "low", "medium", "high", "max":
	default:
		return fmt.Errorf("efor %q geçersiz", y.Efor)
	}
	if y.CacheTTL != "5m" && y.CacheTTL != "1h" {
		return fmt.Errorf("cache_ttl %q geçersiz (5m|1h)", y.CacheTTL)
	}
	if y.CacheUyariEsigi < 0 || y.CacheUyariEsigi > 1 {
		return errors.New("cache_uyari_esigi 0..1")
	}
	if y.AracTurSiniri < 1 || y.AracTurSiniri > 8 {
		return errors.New("arac_tur_siniri 1..8")
	}
	if y.OturumAracSiniri < 1 || y.OturumAracSiniri > 25 {
		return errors.New("oturum_arac_siniri 1..25")
	}
	if y.YukseltmeTurSiniri < 1 || y.YukseltmeTurSiniri > 8 {
		return errors.New("yukseltme_tur_siniri 1..8")
	}
	if y.IsZamanAsimiSn < 5 || y.IsZamanAsimiSn > 55 {
		return errors.New("is_zaman_asimi_sn 5..55 (imzalı iş emri ≤60 sn geçerli)")
	}
	if y.MesajDakikaSiniri < 1 || y.SunucuOturumSiniri < 1 || y.OturumBostaSn < 60 {
		return errors.New("hız sınırları geçersiz")
	}
	if y.KonusmaSaklamaGun < 1 || y.KonusmaSaklamaGun > 30 {
		return errors.New("konusma_saklama_gun 1..30")
	}
	if y.IcDinle != "" {
		if err := loopbackDinle(y.IcDinle); err != nil {
			return err
		}
	}
	if len(y.PortalBiletAnahtarlari) > 8 {
		return errors.New("portal_bilet_anahtarlari en çok 8")
	}
	if _, err := y.PortalAnahtarlari(); err != nil {
		return err
	}
	if y.BotKullaniciAdi != "" && !regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`).MatchString(y.BotKullaniciAdi) {
		return errors.New("bot_kullanici_adi geçersiz")
	}
	return nil
}

// loopbackDinle: iç API'nin dinleme adresi YALNIZ loopback olabilir (0.0.0.0, boş host, dış IP ve
// ad çözümü gerektiren adlar reddedilir — nginx/tünel arkasından erişim ayrı katmandır).
func loopbackDinle(adres string) error {
	host, port, err := net.SplitHostPort(adres)
	if err != nil || port == "" {
		return errors.New("ic_dinle host:port olmalı")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("ic_dinle yalnız loopback olabilir (127.0.0.1 / [::1])")
	}
	return nil
}

// PortalAnahtarlari: yapılandırmadaki portal bilet açık anahtarları (çözülmüş).
func (y *Yapilandirma) PortalAnahtarlari() ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, s := range y.PortalBiletAnahtarlari {
		a, err := protokol.AcikAnahtarCoz(strings.TrimSpace(s))
		if err != nil {
			return nil, errors.New("portal_bilet_anahtarlari: geçersiz açık anahtar")
		}
		out = append(out, a)
	}
	return out, nil
}

// YapilandirmaYukle: dosyayı katı çözer, varsayılanları uygular, doğrular.
func YapilandirmaYukle(yol string) (*Yapilandirma, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	veri, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(veri) > 1<<20 {
		return nil, errors.New("yapılandırma çok büyük")
	}
	d := json.NewDecoder(bytes.NewReader(veri))
	d.DisallowUnknownFields()
	var y Yapilandirma
	if err := d.Decode(&y); err != nil {
		return nil, fmt.Errorf("yapılandırma: %w", err)
	}
	if d.More() {
		return nil, errors.New("yapılandırma: nesneden sonra ek veri")
	}
	y.Varsayilanlar()
	if err := y.Dogrula(); err != nil {
		return nil, err
	}
	return &y, nil
}

// ---- sırlar ----

// Sır dosya adları (systemd LoadCredential=<ad>:<kaynak>).
const (
	SirTelegram      = "telegram-token"
	SirAnthropic     = "anthropic-key"
	SirWebhook       = "telegram-webhook-gizli"
	SirMerkezAnahtar = "merkez-imza-anahtari"
	// SirPortalIc: portalın iç API'yi (/ic/v1/*) çağırırken sunduğu Bearer sırrı.
	SirPortalIc = "portal-ic-anahtar"
)

// Sirlar: bellekteki sırlar. String() / %v sırrı YAZMAZ.
type Sirlar struct {
	TelegramToken    string
	AnthropicAnahtar string
	WebhookGizli     string
	MerkezAnahtar    *protokol.AnahtarCifti
	PortalIcAnahtar  string // iç API Bearer sırrı; boşsa /ic/v1/* tümüyle kapalı (503)
}

func (*Sirlar) String() string   { return "Sirlar{***}" }
func (*Sirlar) GoString() string { return "Sirlar{***}" }

// Bilinenler: çıkış maskesine birebir verilecek sırlar.
func (s *Sirlar) Bilinenler() []string {
	if s == nil {
		return nil
	}
	return []string{s.TelegramToken, s.AnthropicAnahtar, s.WebhookGizli, s.PortalIcAnahtar}
}

var (
	telegramTokenDeseni = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{30,64}$`)
	webhookGizliDeseni  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)
	portalIcDeseni      = regexp.MustCompile(`^[A-Za-z0-9_-]{32,256}$`)
)

// KimlikDizini: --kimlik-dizini bayrağı, yoksa systemd'nin verdiği CREDENTIALS_DIRECTORY
// (bu bir YOL'dur; sır ortamdan okunmaz).
func KimlikDizini(bayrak string) (string, error) {
	if bayrak != "" {
		return bayrak, nil
	}
	d := os.Getenv("CREDENTIALS_DIRECTORY")
	if d == "" {
		return "", errors.New("CREDENTIALS_DIRECTORY yok: systemd LoadCredential= ile çalıştırın ya da --kimlik-dizini verin")
	}
	return d, nil
}

// sirOku: tek sır dosyası; içerik hata metnine KONMAZ.
func sirOku(dizin, ad string) ([]byte, error) {
	yol := filepath.Join(dizin, ad)
	fi, err := os.Lstat(yol)
	if err != nil {
		return nil, fmt.Errorf("sır %s okunamadı", ad)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("sır %s düzenli dosya değil", ad)
	}
	if !sirIzniGuvenli(fi) {
		return nil, fmt.Errorf("sır %s grup/diğer erişimine açık (%o)", ad, fi.Mode().Perm())
	}
	f, err := os.Open(yol)
	if err != nil {
		return nil, fmt.Errorf("sır %s açılamadı", ad)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 {
		return nil, fmt.Errorf("sır %s okunamadı ya da çok büyük", ad)
	}
	return b, nil
}

// sirIzniGuvenli: "diğer" hiçbir zaman erişemez; grup en çok OKUYABİLİR ve yalnız grup
// root (gid 0) ise. systemd ≥ 256 LoadCredential dosyalarını root:root 0440 açar ve servis
// kullanıcısına okumayı ACL ile verir — root grubu zaten root'a denk olduğundan güvenli.
func sirIzniGuvenli(fi os.FileInfo) bool {
	izin := fi.Mode().Perm()
	if izin&0o007 != 0 || izin&0o030 != 0 {
		return false
	}
	if izin&0o040 == 0 {
		return true
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Gid == 0
}

// tohumCoz: 32 bayt ham, 64 hex ya da base64(url) metin.
func tohumCoz(ham []byte) ([]byte, error) {
	if len(ham) == 32 {
		return bytes.Clone(ham), nil
	}
	s := strings.TrimSpace(string(ham))
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("merkez imza anahtarı 32 baytlık Ed25519 tohumu değil")
}

// SirlariYukle: beş sırrı dizinden okur ve biçimlerini doğrular.
func SirlariYukle(dizin string) (*Sirlar, error) {
	s := &Sirlar{}
	tg, err := sirOku(dizin, SirTelegram)
	if err != nil {
		return nil, err
	}
	s.TelegramToken = strings.TrimSpace(string(tg))
	if !telegramTokenDeseni.MatchString(s.TelegramToken) {
		return nil, errors.New("telegram-token biçimi geçersiz")
	}
	an, err := sirOku(dizin, SirAnthropic)
	if err != nil {
		return nil, err
	}
	s.AnthropicAnahtar = strings.TrimSpace(string(an))
	if len(s.AnthropicAnahtar) < 20 || strings.ContainsAny(s.AnthropicAnahtar, " \t\r\n") {
		return nil, errors.New("anthropic-key biçimi geçersiz")
	}
	wh, err := sirOku(dizin, SirWebhook)
	if err != nil {
		return nil, err
	}
	s.WebhookGizli = strings.TrimSpace(string(wh))
	if !webhookGizliDeseni.MatchString(s.WebhookGizli) {
		return nil, errors.New("telegram-webhook-gizli biçimi geçersiz ([A-Za-z0-9_-]{16,256})")
	}
	pi, err := sirOku(dizin, SirPortalIc)
	if err != nil {
		return nil, err
	}
	s.PortalIcAnahtar = strings.TrimSpace(string(pi))
	if !portalIcDeseni.MatchString(s.PortalIcAnahtar) {
		return nil, errors.New("portal-ic-anahtar biçimi geçersiz ([A-Za-z0-9_-]{32,256})")
	}
	th, err := sirOku(dizin, SirMerkezAnahtar)
	if err != nil {
		return nil, err
	}
	tohum, err := tohumCoz(th)
	if err != nil {
		return nil, err
	}
	s.MerkezAnahtar, err = protokol.AnahtarCiftiTohumdan(tohum)
	if err != nil {
		return nil, errors.New("merkez imza anahtarı geçersiz")
	}
	return s, nil
}
