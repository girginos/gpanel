package arac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"girginospanel/internal/destek/kopru"
	"girginospanel/internal/destek/protokol"
)

// yanitSiniri — panel rota yanıtının üst sınırı (bayt); köprüyle aynı değer.
const yanitSiniri = kopru.YanitSiniri

// PanelYanitVermiyor — panel-bağımlı bir araç panele ulaşamadığında sonuç özetinin
// başındaki SABİT işaret (modelin ve operatörün aradığı ad). Telde hata kodu olarak
// protokol.HataMesgul gider (eski merkez sürümleri yeni bir kodu REDDEDERDİ — bkz.
// KURULUM.md "uyumluluk"); ayırt edici ad özet metnindedir.
const PanelYanitVermiyor = "panel_yanit_vermiyor"

// panelYok — köprü/panel yanıt vermiyor. Özet modele panel-bağımsız araçları önerir.
func panelYok() error {
	return hataf(protokol.HataMesgul, PanelYanitVermiyor+": panel hizmeti yanıt vermiyor; bu araç panele bağlı. "+
		"servis_durum, dogrula_ozet, nginx_test ve log_oku (php_fpm/laravel/cron) panelden bağımsız çalışır")
}

// rota — panelin salt-okunur GET ucuna destek kimliğiyle istek atar ve JSON
// gövdeyi çözer. Panel sürecinde kopru.Yerel (süreç içi), ayrı destek-ajan
// sürecinde köprü istemcisi arkadadır. hesap verilirse {id} yerine o hesabın
// kimliği konur (yol kalıbı çağırandan, kimlik DB'den gelir; modelden gelen
// metin yola girmez).
func (c *cagri) rota(ctx context.Context, h *hesap, yol string, sorgu url.Values) (any, error) {
	dk, err := c.destekKimligi(h)
	if err != nil {
		return nil, err
	}
	if h != nil {
		yol = "/api/v1/domains/" + strconv.FormatInt(h.ID, 10) + yol
	}
	y, err := c.y.gecit.Cagir(ctx, kopru.Istek{Kimlik: dk, Yol: yol, Sorgu: sorgu.Encode()})
	if err != nil {
		switch {
		case errors.Is(err, kopru.ErrYok):
			return nil, panelYok()
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			return nil, hataf(protokol.HataZamanAsimi, "panel yanıtı gecikti")
		}
		return nil, hataf(protokol.HataYetki, "köprü isteği reddetti")
	}
	kod, govde, asildi := y.Kod, y.Govde, y.Asildi
	if kod == 0 {
		kod = http.StatusOK
	}
	if kod != http.StatusOK {
		return nil, rotaHatasi(kod, govde)
	}
	if asildi || len(govde) > yanitSiniri {
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
