package arac

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/middleware"
)

// yanitSiniri — süreç içi rota yanıtının üst sınırı (bayt).
const yanitSiniri = 8 << 20

// kayitci — sınırlı, eşzamanlı-güvenli ResponseWriter (httptest.ResponseRecorder
// yerine: gövde sınırsız büyümesin; zaman aşımında bırakılan işleyici yazmaya
// devam etse de okuyanla yarışmasın).
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
	if kalan := yanitSiniri - k.govde.Len(); kalan < len(p) {
		if kalan > 0 {
			k.govde.Write(p[:kalan])
		}
		k.asildi = true
		return len(p), nil
	}
	return k.govde.Write(p)
}
func (k *kayitci) Flush() {}

// rota — panelin kendi yönlendiricisine destek kimliğiyle GET isteği atar ve JSON
// gövdeyi çözer. hesap verilirse {id} yerine o hesabın kimliği konur (yol
// kalıbı çağırandan, kimlik DB'den gelir; modelden gelen metin yola girmez).
func (c *cagri) rota(ctx context.Context, h *hesap, yol string, sorgu url.Values) (any, error) {
	dk, err := c.destekKimligi(h)
	if err != nil {
		return nil, err
	}
	if h != nil {
		yol = "/api/v1/domains/" + strconv.FormatInt(h.ID, 10) + yol
	}
	if len(sorgu) > 0 {
		yol += "?" + sorgu.Encode()
	}
	req, err := http.NewRequestWithContext(middleware.DestekBagla(ctx, dk), http.MethodGet, yol, nil)
	if err != nil {
		return nil, hataf(protokol.HataIc, "istek kurulamadı")
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
		c.y.router.ServeHTTP(k, req)
	}()
	select {
	case <-bitti:
	case <-ctx.Done():
		return nil, hataf(protokol.HataZamanAsimi, "panel yanıtı gecikti")
	}
	k.mu.Lock()
	kod, govde, asildi := k.kod, bytes.Clone(k.govde.Bytes()), k.asildi
	k.mu.Unlock()
	if kod == 0 {
		kod = http.StatusOK
	}
	if kod != http.StatusOK {
		return nil, rotaHatasi(kod, govde)
	}
	if asildi {
		return nil, hataf(protokol.HataIc, "panel yanıtı çok büyük")
	}
	d := json.NewDecoder(bytes.NewReader(govde))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, hataf(protokol.HataIc, "panel yanıtı çözülemedi")
	}
	return v, nil
}

// rotaHatasi — panel HTTP kodunu protokol koduna çevirir; panelin hata metni
// (maskelenerek) özet olur.
func rotaHatasi(kod int, govde []byte) error {
	var e struct {
		Hata string `json:"hata"`
	}
	_ = json.Unmarshal(govde, &e)
	m := e.Hata
	if len(m) > 300 {
		m = m[:300]
	}
	switch kod {
	case http.StatusUnauthorized, http.StatusForbidden:
		if m == "" {
			m = "panel bu işleme izin vermedi"
		}
		return hataf(protokol.HataYetki, m)
	case http.StatusNotFound:
		if m == "" {
			m = "bulunamadı"
		}
		return hataf(protokol.HataKapsam, m)
	case http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusConflict:
		if m == "" {
			m = "panel meşgul"
		}
		return hataf(protokol.HataMesgul, m)
	case http.StatusBadRequest:
		return hataf(protokol.HataParam, m)
	}
	return hataf(protokol.HataIc, "panel hatası (HTTP "+strconv.Itoa(kod)+")")
}
