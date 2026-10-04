package kopru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"girginospanel/internal/middleware"
)

// Sabitler.
const (
	// SoketDizini / SoketYolu — köprünün varsayılan yeri.
	SoketDizini = "/run/girginospanel"
	SoketYolu   = SoketDizini + "/destek-kopru.sock"

	// IstekSiniri — tek isteğin üst sınırı (bayt). Gerçek istek <1 KB.
	IstekSiniri = 8 << 10
	// YanitSiniri — panel yanıt GÖVDESİNİN üst sınırı (arac/rota.go ile aynı: 8 MB).
	YanitSiniri = 8 << 20
	// SorguSiniri — sorgu dizgesi üst sınırı.
	SorguSiniri = 1024
)

// ErrYok — köprüye ulaşılamadı / yanıt vermedi (soket yok, reddetti, zaman aşımı,
// kesik yanıt). Araç katmanı bunu panel_yanit_vermiyor olarak raporlar.
var ErrYok = errors.New("panel köprüsü yanıt vermiyor")

// Istek — köprü isteği. Kimlik ajan tarafında kurulur (yerel DB kaydından);
// merkezden gelen hiçbir veri buraya girmez.
type Istek struct {
	Kimlik middleware.DestekKimlik `json:"kimlik"`
	Yol    string                  `json:"yol"`
	Sorgu  string                  `json:"sorgu,omitempty"`
	Ping   bool                    `json:"ping,omitempty"`
}

// Yanit — panelin uç yanıtı (kod + sınırlı gövde). Gövde taşan yanıtta Asildi true.
type Yanit struct {
	Kod    int    `json:"kod"`
	Govde  []byte `json:"govde,omitempty"`
	Asildi bool   `json:"asildi,omitempty"`
	Hata   string `json:"hata,omitempty"` // köprü seviyesi ret (kod 0): kısa, sırsız
}

// Gecit — panel uçlarına salt-okunur erişim. Üretimde Istemci (ajan süreci) ya da
// Yerel (panel süreci / testler).
type Gecit interface {
	Cagir(ctx context.Context, i Istek) (Yanit, error)
}

// ---- Yerel: süreç içi çağrı (kimlik işareti BURADA konur) ----

// Yerel — panelin kök yönlendiricisine süreç içi GET atar. Köprü sunucusu ve
// testler kullanır.
type Yerel struct{ Router http.Handler }

// kayitci — sınırlı, eşzamanlı-güvenli ResponseWriter (gövde sınırsız büyümesin;
// zaman aşımında bırakılan işleyici yazmaya devam etse de okuyanla yarışmasın).
type kayitci struct {
	mu     sync.Mutex
	h      http.Header
	kod    int
	govde  bytes.Buffer
	asildi bool
}

func (k *kayitci) Header() http.Header { return k.h }
func (k *kayitci) WriteHeader(kod int) {
	k.mu.Lock()
	if k.kod == 0 {
		k.kod = kod
	}
	k.mu.Unlock()
}
func (k *kayitci) Write(p []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.kod == 0 {
		k.kod = http.StatusOK
	}
	if kalan := YanitSiniri - k.govde.Len(); kalan < len(p) {
		if kalan > 0 {
			k.govde.Write(p[:kalan])
		}
		k.asildi = true
		return len(p), nil
	}
	return k.govde.Write(p)
}
func (k *kayitci) Flush() {}

// Cagir — yönlendiriciye destek kimliğiyle GET atar. Yol/sorgu çağıranın
// sorumluluğundadır (köprü sunucusu YolIzinli ile doğrular).
func (y Yerel) Cagir(ctx context.Context, i Istek) (Yanit, error) {
	yol := i.Yol
	if i.Sorgu != "" {
		yol += "?" + i.Sorgu
	}
	req, err := http.NewRequestWithContext(middleware.DestekBagla(ctx, i.Kimlik), http.MethodGet, yol, nil)
	if err != nil {
		return Yanit{}, errors.New("istek kurulamadı")
	}
	req.RemoteAddr = "127.0.0.1:0"
	req.RequestURI = yol
	k := &kayitci{h: http.Header{}}
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		defer func() {
			if r := recover(); r != nil {
				k.WriteHeader(http.StatusInternalServerError)
			}
		}()
		y.Router.ServeHTTP(k, req)
	}()
	select {
	case <-bitti:
	case <-ctx.Done():
		return Yanit{}, ctx.Err()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	kod := k.kod
	if kod == 0 {
		kod = http.StatusOK
	}
	return Yanit{Kod: kod, Govde: bytes.Clone(k.govde.Bytes()), Asildi: k.asildi}, nil
}

// ---- doğrulama (sunucu tarafı) ----

// sorguTemizle — sorguyu katı çözüp yeniden kodlar (yalnız anahtar=değer; "%00" vb.
// kaçışlar yeniden kodlamada nötrlenir). Hata → red.
func sorguTemizle(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	if len(s) > SorguSiniri {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return "", false
		}
	}
	v, err := url.ParseQuery(s)
	if err != nil {
		return "", false
	}
	for k, vs := range v {
		if k == "" || len(vs) > 8 {
			return "", false
		}
		for _, x := range vs {
			if strings.ContainsRune(x, 0) {
				return "", false
			}
		}
	}
	return v.Encode(), true
}

// kimlikGecerli — köprüye gelen kimlik alanlarının biçimi (middleware zaten rolü
// ve DB'yi ayrıca doğrular; bu yalnız uç değer/uzunluk kapısıdır).
func kimlikGecerli(k middleware.DestekKimlik) bool {
	switch k.Rol {
	case middleware.DestekMusteri, middleware.DestekBayi, middleware.DestekKok:
	default:
		return false
	}
	if len(k.Kullanici) > 64 || len(k.AlanAdi) > 253 || len(k.TgKisa) > 24 || k.KullaniciID < 0 || k.DomainID < 0 {
		return false
	}
	return true
}

// JSON yardımcıları (testler de kullanır).
func istekCoz(b []byte) (Istek, error) {
	var i Istek
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&i); err != nil {
		return Istek{}, err
	}
	if d.More() {
		return Istek{}, errors.New("fazla veri")
	}
	return i, nil
}
