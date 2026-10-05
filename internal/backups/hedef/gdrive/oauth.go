package gdrive

// oauth.go — OAuth 2.0 cihaz (limited-input device) akışı, yenileme ve iptal.
//
// NEDEN CİHAZ AKIŞI: panel başsız sunucuda çalışır; localhost yönlendirmesi
// kullanıcının tarayıcısı ile sunucu arasında köprü ister. Google cihaz akışı
// drive.file kapsamını DESTEKLER (belgelenmiş izinli kapsamlar: openid, email,
// profile, drive.appdata, drive.file, youtube...). Kullanıcının KENDİ Cloud
// projesinde "TV'ler ve Sınırlı Girişli Cihazlar" türünde OAuth istemcisi
// oluşturması gerekir (istemci gizli anahtarı yine de verilir ve gönderilir).
//
// 🔴 "Test" yayın durumundaki OAuth onay ekranında yenileme jetonu 7 gün sonra
// geçersiz olur (invalid_grant → KategoriKimlik → arayüzde "yeniden yetkilendir").

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
)

// Kimlik — kullanıcının OAuth istemcisi. ClientSecret gizlidir.
type Kimlik struct {
	ClientID     string
	ClientSecret string
}

func (k Kimlik) sirlar() []string { return []string{k.ClientSecret} }

// CihazYetkisi — kullanıcıya gösterilecek cihaz kodu bilgisi.
type CihazYetkisi struct {
	CihazKodu     string        // sunucuda kalır, kullanıcıya GÖSTERİLMEZ
	KullaniciKodu string        // kullanıcının google.com/device adresine yazacağı kod
	DogrulamaURL  string        // yalnız Google adresi (doğrulanmış)
	Gecerlilik    time.Duration // kodun ömrü
	Aralik        time.Duration // sorgu aralığı (en az)
}

// SorguDurumu — cihaz kodu sorgusunun sonucu.
type SorguDurumu string

const (
	SorguBekliyor    SorguDurumu = "bekliyor"
	SorguTamam       SorguDurumu = "tamam"
	SorguReddedildi  SorguDurumu = "reddedildi"
	SorguSuresiDoldu SorguDurumu = "suresi_doldu"
	SorguYavasla     SorguDurumu = "yavasla"
)

// SorguSonucu — Durum tamam ise YenilemeJetonu doludur (GİZLİ: saklanır, gösterilmez).
type SorguSonucu struct {
	Durum          SorguDurumu
	YenilemeJetonu string
}

// oauthIstemci — kapıdan ve izin listesinden geçen OAuth çağrıları.
type oauthIstemci struct {
	kimlik Kimlik
	u      uclar
	http   *http.Client
}

func yeniOAuth(k Kimlik, sec hedef.KurSecenek) *oauthIstemci {
	return &oauthIstemci{kimlik: k, u: uretimUclari, http: yeniHTTP(sec, HostIzinliMi, false)}
}

// CihazYetkisiIste — Google'dan cihaz kodu ister (kapsam YALNIZ drive.file).
func CihazYetkisiIste(ctx context.Context, k Kimlik, sec hedef.KurSecenek) (*CihazYetkisi, error) {
	return yeniOAuth(k, sec).cihazIste(ctx)
}

// CihazYetkisiSorgula — kullanıcı onayladı mı? Tamam ise yenileme jetonu döner.
func CihazYetkisiSorgula(ctx context.Context, k Kimlik, sec hedef.KurSecenek, cihazKodu string) (SorguSonucu, error) {
	return yeniOAuth(k, sec).cihazSorgula(ctx, cihazKodu)
}

// JetonIptal — yenileme jetonunu Google'da iptal eder (bağlantıyı kes).
func JetonIptal(ctx context.Context, k Kimlik, sec hedef.KurSecenek, jeton string) error {
	return yeniOAuth(k, sec).iptal(ctx, jeton)
}

// form — POST application/x-www-form-urlencoded; yanıt gövdesi ve durum kodu.
func (o *oauthIstemci) form(ctx context.Context, islem, adres string, v url.Values, sirlar []string) (int, []byte, error) {
	ctx, iptal := context.WithTimeout(ctx, 45*time.Second)
	defer iptal()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adres, strings.NewReader(v.Encode()))
	if err != nil {
		return 0, nil, hedef.Sarmala(islem, err, sirlar...)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := o.http.Do(req)
	if err != nil {
		return 0, nil, hedef.Sarmala(islem, agHatasi(err), sirlar...)
	}
	return resp.StatusCode, govdeOku(resp), nil
}

func (o *oauthIstemci) cihazIste(ctx context.Context) (*CihazYetkisi, error) {
	sirlar := o.kimlik.sirlar()
	kod, govde, err := o.form(ctx, "yetki", o.u.cihaz, url.Values{
		"client_id": {o.kimlik.ClientID}, "scope": {Kapsam},
	}, sirlar)
	if err != nil {
		return nil, err
	}
	if kod != http.StatusOK {
		return nil, govdeCoz(kod, govde).hata("yetki", sirlar)
	}
	var y struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if json.Unmarshal(govde, &y) != nil || y.DeviceCode == "" || y.UserCode == "" {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "yetki", errors.New("drive: cihaz kodu yanıtı geçersiz"), sirlar...)
	}
	adres := y.VerificationURL
	if adres == "" {
		adres = y.VerificationURI
	}
	c := &CihazYetkisi{
		CihazKodu: y.DeviceCode, KullaniciKodu: y.UserCode, DogrulamaURL: dogrulamaAdresi(adres),
		Gecerlilik: time.Duration(y.ExpiresIn) * time.Second, Aralik: time.Duration(y.Interval) * time.Second,
	}
	if c.Gecerlilik <= 0 || c.Gecerlilik > time.Hour {
		c.Gecerlilik = 30 * time.Minute
	}
	if c.Aralik < 5*time.Second {
		c.Aralik = 5 * time.Second
	}
	return c, nil
}

// dogrulamaAdresi — kullanıcıya gösterilecek bağlantı yalnız Google'ın https
// adresi olabilir; aksi halde bilinen varsayılan (yanıt değiştirilse bile
// kullanıcı yabancı bir siteye yönlendirilmez).
func dogrulamaAdresi(s string) string {
	const varsayilan = "https://www.google.com/device"
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return varsayilan
	}
	h := strings.ToLower(u.Hostname())
	if h != "google.com" && !strings.HasSuffix(h, ".google.com") {
		return varsayilan
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

func (o *oauthIstemci) cihazSorgula(ctx context.Context, cihazKodu string) (SorguSonucu, error) {
	sirlar := append(o.kimlik.sirlar(), cihazKodu)
	kod, govde, err := o.form(ctx, "yetki", o.u.token, url.Values{
		"client_id": {o.kimlik.ClientID}, "client_secret": {o.kimlik.ClientSecret},
		"device_code": {cihazKodu}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
	}, sirlar)
	if err != nil {
		return SorguSonucu{}, err
	}
	if kod == http.StatusOK {
		var t struct {
			Refresh string `json:"refresh_token"`
			Scope   string `json:"scope"`
		}
		if json.Unmarshal(govde, &t) != nil || t.Refresh == "" {
			return SorguSonucu{}, hedef.YeniHata(hedef.KategoriKimlik, "yetki",
				errors.New("drive: yenileme jetonu alınamadı (istemci türü ya da onay ekranı)"), sirlar...)
		}
		if t.Scope != "" && !kapsamVar(t.Scope, Kapsam) {
			return SorguSonucu{}, hedef.YeniHata(hedef.KategoriErisim, "yetki",
				errors.New("drive: drive.file izni verilmedi"), append(sirlar, t.Refresh)...)
		}
		return SorguSonucu{Durum: SorguTamam, YenilemeJetonu: t.Refresh}, nil
	}
	a := govdeCoz(kod, govde)
	switch strings.ToLower(a.OAuth) {
	case "authorization_pending":
		return SorguSonucu{Durum: SorguBekliyor}, nil
	case "slow_down":
		return SorguSonucu{Durum: SorguYavasla}, nil
	case "access_denied":
		return SorguSonucu{Durum: SorguReddedildi}, nil
	case "expired_token", "invalid_grant":
		return SorguSonucu{Durum: SorguSuresiDoldu}, nil
	}
	// Google bazı hata kodlarını (428/403) gövdesiz da döner.
	if kod == http.StatusPreconditionRequired {
		return SorguSonucu{Durum: SorguBekliyor}, nil
	}
	return SorguSonucu{}, a.hata("yetki", sirlar)
}

func kapsamVar(liste, aranan string) bool {
	for _, k := range strings.Fields(liste) {
		if k == aranan {
			return true
		}
	}
	return false
}

func (o *oauthIstemci) iptal(ctx context.Context, jeton string) error {
	sirlar := append(o.kimlik.sirlar(), jeton)
	kod, govde, err := o.form(ctx, "iptal", o.u.iptal, url.Values{"token": {jeton}}, sirlar)
	if err != nil {
		return err
	}
	if kod == http.StatusOK {
		return nil
	}
	a := govdeCoz(kod, govde)
	// Jeton zaten geçersiz/iptal: amaç gerçekleşmiş.
	if kod == http.StatusBadRequest && strings.EqualFold(a.OAuth, "invalid_token") {
		return nil
	}
	return a.hata("iptal", sirlar)
}

// ─── Erişim jetonu yöneticisi ───────────────────────────────────────────────

// jetonYonetici — yenileme jetonundan kısa ömürlü erişim jetonu üretir ve önbellekler.
type jetonYonetici struct {
	o     *oauthIstemci
	simdi func() time.Time
	uyku  func(context.Context, time.Duration) error
	// yenilendi — Google yeni bir yenileme jetonu verirse (nadir) çağrılır; kalıcılaştırma
	// çağıranın işidir. nil olabilir.
	yenilendi func(yeniYenileme string)

	mu       sync.Mutex
	yenileme string
	erisim   string
	bitis    time.Time
}

func (j *jetonYonetici) sirlar() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append(j.o.kimlik.sirlar(), j.yenileme, j.erisim)
}

// Al — geçerli erişim jetonu (süresi dolmak üzereyse yeniler).
func (j *jetonYonetici) Al(ctx context.Context) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.erisim != "" && j.simdi().Add(time.Minute).Before(j.bitis) {
		return j.erisim, nil
	}
	return j.yenile(ctx)
}

// Gecersiz — sunucu 401 verdi: o jeton önbellekten düşer (başkası yenilediyse dokunulmaz).
func (j *jetonYonetici) Gecersiz(jeton string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.erisim == jeton {
		j.erisim, j.bitis = "", time.Time{}
	}
}

// yenile — kilit altında çağrılır.
func (j *jetonYonetici) yenile(ctx context.Context) (string, error) {
	sirlar := append(j.o.kimlik.sirlar(), j.yenileme, j.erisim)
	var son error
	for deneme := 0; deneme < 3; deneme++ {
		kod, govde, err := j.o.form(ctx, "jeton", j.o.u.token, url.Values{
			"client_id": {j.o.kimlik.ClientID}, "client_secret": {j.o.kimlik.ClientSecret},
			"refresh_token": {j.yenileme}, "grant_type": {"refresh_token"},
		}, sirlar)
		if err != nil {
			if ctx.Err() != nil || reddiMi(err) {
				return "", err
			}
			son = err
		} else if kod == http.StatusOK {
			var t struct {
				Erisim string `json:"access_token"`
				Sure   int    `json:"expires_in"`
				Yenile string `json:"refresh_token"`
			}
			if json.Unmarshal(govde, &t) != nil || t.Erisim == "" {
				return "", hedef.YeniHata(hedef.KategoriBilinmeyen, "jeton", errors.New("drive: jeton yanıtı geçersiz"), sirlar...)
			}
			if t.Sure <= 0 {
				t.Sure = 3000
			}
			j.erisim, j.bitis = t.Erisim, j.simdi().Add(time.Duration(t.Sure)*time.Second)
			if t.Yenile != "" && t.Yenile != j.yenileme {
				j.yenileme = t.Yenile
				if j.yenilendi != nil {
					j.yenilendi(t.Yenile)
				}
			}
			return j.erisim, nil
		} else {
			a := govdeCoz(kod, govde)
			if !a.yenidenDenenir() {
				// invalid_grant: yenileme jetonu iptal/süresi dolmuş (Test modunda 7 gün)
				// → KategoriKimlik; yeniden yetkilendirme gerekir.
				return "", a.hata("jeton", sirlar)
			}
			son = a.hata("jeton", sirlar)
		}
		if deneme < 2 {
			if err := j.uyku(ctx, geriCekilme(deneme)); err != nil {
				return "", hedef.Sarmala("jeton", err)
			}
		}
	}
	return "", son
}

// Sifirla — önbellekteki erişim jetonu düşer (401 sonrası yeniden yenilemek için).
func (j *jetonYonetici) Sifirla() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.erisim, j.bitis = "", time.Time{}
}
