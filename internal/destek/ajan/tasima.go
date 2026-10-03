package ajan

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	p "girginospanel/internal/destek/protokol"
)

// tasima: tek bir merkez oturumunun çerçeve kanalı (WSS ya da uzun yoklama).
// Al: sıradaki çerçeve; (nil, nil) = mesajsız canlılık (yoklama boş döndü).
type tasima interface {
	Gonder(ctx context.Context, veri []byte) error
	Al(ctx context.Context) ([]byte, error)
	Kapat() error
	Tur() string
}

// ---- WSS ----

type wsTasima struct{ w *wsBag }

func (t *wsTasima) Gonder(_ context.Context, veri []byte) error { return t.w.Gonder(veri) }
func (t *wsTasima) Al(ctx context.Context) ([]byte, error) {
	// Merkez kalp atışı KalpAraligi'nda gelir; KalpZamanAsimi sessizlik → kopuk say.
	return t.w.Al(p.KalpZamanAsimi)
}
func (t *wsTasima) Kapat() error { return t.w.Kapat() }
func (t *wsTasima) Tur() string  { return "wss" }

// ---- uzun yoklama ----

// yoklamaTasima: protokol.Yoklama* uçları. El sıkışma (kayit → meydan_okuma,
// meydan_yanit → kayit_sonuc) istek/yanıt; sonrası Bearer <oturum_belirteci> ile
// al (Zarf dizisi, en çok YoklamaBekleme) ve gonder.
type yoklamaTasima struct {
	taban    *url.URL
	istemci  *http.Client
	mu       sync.Mutex
	belirtec string
	gelen    [][]byte
}

func yeniYoklama(taban *url.URL, tlsCfg *tls.Config) *yoklamaTasima {
	tr := &http.Transport{
		Proxy:               nil, // vekil YOK: merkez trafiği doğrudan (WSS ile tutarlı)
		TLSClientConfig:     tlsCfg,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        2,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	return &yoklamaTasima{taban: taban, istemci: &http.Client{Transport: tr, Timeout: p.YoklamaBekleme + 20*time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (t *yoklamaTasima) Tur() string { return "yoklama" }

func (t *yoklamaTasima) post(ctx context.Context, yol string, govde []byte, belirtecli bool) ([]byte, error) {
	u := *t.taban
	u.Path = yol
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(govde))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "girginospanel-destek/1")
	if belirtecli {
		t.mu.Lock()
		b := t.belirtec
		t.mu.Unlock()
		if b == "" {
			return nil, errors.New("yoklama: oturum belirteci yok")
		}
		req.Header.Set(p.YoklamaBelirtecBasligi, "Bearer "+b)
	}
	resp, err := t.istemci.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	veri, err := io.ReadAll(io.LimitReader(resp.Body, 8*int64(p.CerceveSiniri)+1))
	if err != nil {
		return nil, err
	}
	if len(veri) > 8*p.CerceveSiniri {
		return nil, errors.New("yoklama: yanıt çok büyük")
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode/100 != 2 {
		return veri, &yoklamaHTTPHatasi{yol: yol, kod: resp.StatusCode}
	}
	return veri, nil
}

// yoklamaHTTPHatasi: 2xx dışı yanıt. Merkez kayit/yanit reddinde gövdeye
// kayit_sonuc{kabul:false, hata_kodu} koyar (400/403/503) — ajan kodu okuyabilsin.
type yoklamaHTTPHatasi struct {
	yol string
	kod int
}

func (e *yoklamaHTTPHatasi) Error() string { return fmt.Sprintf("yoklama %s: HTTP %d", e.yol, e.kod) }

// redGovdesi: el sıkışma uçlarının 2xx dışı yanıtındaki kayit_sonuc reddi (yoksa nil).
func redGovdesi(veri []byte, err error) []byte {
	var he *yoklamaHTTPHatasi
	if !errors.As(err, &he) {
		return nil
	}
	if _, g, cerr := p.Coz(veri, p.YonAjana); cerr == nil {
		if ks, ok := g.(*p.KayitSonuc); ok && !ks.Kabul {
			return veri
		}
	}
	return nil
}

func zarfTipi(veri []byte) p.Tip {
	var z struct {
		Tip p.Tip `json:"tip"`
	}
	_ = json.Unmarshal(veri, &z)
	return z.Tip
}

func (t *yoklamaTasima) Gonder(ctx context.Context, veri []byte) error {
	switch zarfTipi(veri) {
	case p.TipKayit:
		y, err := t.post(ctx, p.YoklamaKayitYolu, veri, false)
		if red := redGovdesi(y, err); red != nil {
			t.kuyruga(red) // bekleyenAl → kayitRed (lisans/sürüm → uzun bekleme)
			return nil
		}
		if err != nil {
			return err
		}
		t.kuyruga(y)
		return nil
	case p.TipMeydanYanit:
		y, err := t.post(ctx, p.YoklamaYanitYolu, veri, false)
		if red := redGovdesi(y, err); red != nil {
			t.kuyruga(red)
			return nil
		}
		if err != nil {
			return err
		}
		if _, g, err := p.Coz(y, p.YonAjana); err == nil {
			if ks, ok := g.(*p.KayitSonuc); ok && ks.Kabul {
				if ks.OturumBelirteci == "" {
					return errors.New("yoklama: kayit_sonuc oturum belirteci taşımıyor")
				}
				t.mu.Lock()
				t.belirtec = ks.OturumBelirteci
				t.mu.Unlock()
			}
		}
		t.kuyruga(y)
		return nil
	default:
		_, err := t.post(ctx, p.YoklamaGonderYolu, veri, true)
		return err
	}
}

func (t *yoklamaTasima) kuyruga(y []byte) {
	if len(bytes.TrimSpace(y)) == 0 {
		return
	}
	t.mu.Lock()
	t.gelen = append(t.gelen, y)
	t.mu.Unlock()
}

func (t *yoklamaTasima) Al(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	if len(t.gelen) > 0 {
		v := t.gelen[0]
		t.gelen = t.gelen[1:]
		t.mu.Unlock()
		return v, nil
	}
	t.mu.Unlock()
	y, err := t.post(ctx, p.YoklamaAlYolu, nil, true)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(y)) == 0 {
		return nil, nil
	}
	var dizi []json.RawMessage
	if err := json.Unmarshal(y, &dizi); err != nil {
		return nil, fmt.Errorf("yoklama al: zarf dizisi bekleniyordu: %w", err)
	}
	if len(dizi) > p.YoklamaAlEnCokZarf {
		return nil, errors.New("yoklama al: çok fazla zarf")
	}
	if len(dizi) == 0 {
		return nil, nil
	}
	t.mu.Lock()
	for _, z := range dizi[1:] {
		t.gelen = append(t.gelen, []byte(z))
	}
	t.mu.Unlock()
	return []byte(dizi[0]), nil
}

func (t *yoklamaTasima) Kapat() error {
	t.istemci.CloseIdleConnections()
	return nil
}
