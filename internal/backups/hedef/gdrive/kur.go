package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"girginospanel/internal/backups/hedef"
)

func init() {
	hedef.Kaydet(TurAdi, func(ayar json.RawMessage, sirlar map[string]string, sec hedef.KurSecenek) (hedef.Hedef, error) {
		return Yeni(ayar, sirlar, sec, Secenek{})
	})
}

// Ayar — backup_hedefler.ayar (SIRSIZ). Uç adresleri BİLEREK yok: ayar kullanıcı
// girdisinden geçer, jeton gönderilen adres ayardan okunsaydı SSRF/jeton
// sızdırma olurdu. Bilinmeyen alan reddedilir.
type Ayar struct {
	// ClientID — kullanıcının OAuth istemci kimliği (gizli değil).
	ClientID string `json:"client_id"`
	// Klasor — Drive kökünde açılacak yedek klasörünün adı.
	Klasor string `json:"klasor,omitempty"`
}

// Sır anahtarları (backup_hedefler.sirlar çözülmüş haritası).
const (
	SirClientSecret = "client_secret"
	SirYenileme     = "refresh_token"
)

// Secenek — kayıt defteri dışı kurucuya ek bağlam.
type Secenek struct {
	// YenilemeKaydet — Google yeni bir yenileme jetonu verirse çağrılır (nadir);
	// çağıran şifreli olarak kalıcılaştırır. nil olabilir.
	YenilemeKaydet func(yeni string)
}

var clientIDRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{5,200}\.apps\.googleusercontent\.com$`)

// ClientIDGecerli — OAuth istemci kimliği biçimi.
func ClientIDGecerli(s string) bool { return clientIDRe.MatchString(s) }

// ClientSecretGecerli — gizli anahtar biçimi (görünür ASCII, boşluksuz, 8-200).
func ClientSecretGecerli(s string) bool {
	if len(s) < 8 || len(s) > 200 {
		return false
	}
	for _, r := range s {
		if r <= 0x20 || r >= 0x7f {
			return false
		}
	}
	return true
}

// KlasorAdiGecerli — Drive klasör adı: boş değil, en çok 100 karakter, "/" ve
// kontrol karakteri yok.
func KlasorAdiGecerli(s string) bool {
	if strings.TrimSpace(s) == "" || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) > 100 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// AyarCoz — ayar JSON'unu doğrular (bilinmeyen alan yok) ve varsayılanları uygular.
func AyarCoz(ham json.RawMessage) (Ayar, error) {
	var a Ayar
	d := json.NewDecoder(bytes.NewReader(ham))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return Ayar{}, hedef.YeniHata(hedef.KategoriBilinmeyen, "kur", errors.New("drive: ayar geçersiz"))
	}
	if !ClientIDGecerli(a.ClientID) {
		return Ayar{}, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("drive: client_id geçersiz"))
	}
	if a.Klasor == "" {
		a.Klasor = VarsayilanKlasor
	}
	if !KlasorAdiGecerli(a.Klasor) {
		return Ayar{}, hedef.YeniHata(hedef.KategoriBilinmeyen, "kur", errors.New("drive: klasör adı geçersiz"))
	}
	return a, nil
}

// Yeni — Drive hedefi kurar (ağ bağlantısı AÇMAZ; ilk işlemde açılır).
func Yeni(ayar json.RawMessage, sirlar map[string]string, sec hedef.KurSecenek, opt Secenek) (hedef.Hedef, error) {
	a, err := AyarCoz(ayar)
	if err != nil {
		return nil, err
	}
	k := Kimlik{ClientID: a.ClientID, ClientSecret: sirlar[SirClientSecret]}
	if !ClientSecretGecerli(k.ClientSecret) {
		return nil, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("drive: client_secret eksik ya da geçersiz"))
	}
	yenileme := sirlar[SirYenileme]
	if strings.TrimSpace(yenileme) == "" {
		return nil, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("drive: yetkilendirme yapılmamış (yenileme jetonu yok)"), k.ClientSecret)
	}
	return yeni(a, k, yenileme, uretimUclari, yeniHTTP(sec, HostIzinliMi, false), opt.YenilemeKaydet), nil
}

// yeni — iç kurucu (testler uç adreslerini ve istemciyi değiştirir).
func yeni(a Ayar, k Kimlik, yenileme string, u uclar, hc *http.Client, kaydet func(string)) *driveHedef {
	o := &oauthIstemci{kimlik: k, u: u, http: hc}
	h := &driveHedef{
		ayar: a, kimlik: k, u: u, http: hc,
		uyku: uykuGercek, simdi: time.Now,
		kilit:    &kilitler{m: map[string]*kilitGirdi{}},
		dizinOnb: map[string]string{},
	}
	h.jeton = &jetonYonetici{o: o, simdi: func() time.Time { return h.simdi() },
		uyku:      func(ctx context.Context, d time.Duration) error { return h.uyku(ctx, d) },
		yenilendi: kaydet, yenileme: yenileme}
	return h
}
