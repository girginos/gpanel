package ajan

// uyum_test.go — merkez ↔ ajan uçtan uca koşusunda (internal/destek/e2e) bulunan
// uyumsuzlukların ajan tarafı birim testleri.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	p "girginospanel/internal/destek/protokol"
)

// Merkez uzun yoklamada kaydı reddederken 403 + kayit_sonuc{kabul:false} döner. Ajan
// gövdedeki kodu okumalı (lisans/sürüm reddinde 30 dk bekleme kararı buna bağlı);
// yalnız "HTTP 403" görüp kısa aralıkla yeniden denememeli.
func TestYoklamaKayitReddiKoduOkunur(t *testing.T) {
	red, err := p.Kodla("ret_000000001", &p.KayitSonuc{Kabul: false, HataKodu: p.HataLisans})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case p.YoklamaKayitYolu, p.YoklamaYanitYolu:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write(red)
		default:
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"surum":1}`))
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	for _, tip := range []p.Govde{
		&p.Kayit{CihazAnahtari: strings.Repeat("A", 43), LisansParmakIzi: strings.Repeat("ab", 32), LisansIspati: "eA",
			PanelSurumu: "test", KatalogOzeti: p.KatalogOzeti()},
		&p.MeydanYanit{Meydan: strings.Repeat("A", 43), Imza: strings.Repeat("A", 86)},
	} {
		tr := yeniYoklama(u, nil)
		b, err := p.Kodla("gid_000000001", tip)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Gonder(context.Background(), b); err != nil {
			t.Fatalf("%s: red gövdesi hata sayıldı: %v", tip.Tip(), err)
		}
		v, err := tr.Al(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, g, err := p.Coz(v, p.YonAjana)
		ks, ok := g.(*p.KayitSonuc)
		if err != nil || !ok || ks.Kabul || ks.HataKodu != p.HataLisans {
			t.Fatalf("%s: red kodu okunamadı: %v %+v", tip.Tip(), err, g)
		}
	}
	// el sıkışma dışındaki uçlarda 2xx dışı yanıt hata kalır
	tr := yeniYoklama(u, nil)
	tr.belirtec = "x"
	b, _ := p.Kodla("gid_000000002", &p.KalpAtisi{Sira: 1, Zaman: 1})
	if err := tr.Gonder(context.Background(), b); err == nil {
		t.Fatal("gonder 418 hata sayılmadı")
	}
}

// /al dizisi protokol sınırını (YoklamaAlEnCokZarf) aşarsa reddedilir; sınırdaki kabul.
func TestYoklamaAlZarfSiniri(t *testing.T) {
	kalp, _ := p.Kodla("klp_000000001", &p.KalpAtisi{Sira: 1, Zaman: 1})
	var n atomic.Int64
	n.Store(p.YoklamaAlEnCokZarf)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dizi := make([]json.RawMessage, n.Load())
		for i := range dizi {
			dizi[i] = kalp
		}
		_ = json.NewEncoder(w).Encode(dizi)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	tr := yeniYoklama(u, nil)
	tr.belirtec = "x"
	if _, err := tr.Al(context.Background()); err != nil {
		t.Fatalf("sınırdaki dizi reddedildi: %v", err)
	}
	n.Store(p.YoklamaAlEnCokZarf + 1)
	tr = yeniYoklama(u, nil)
	tr.belirtec = "x"
	if _, err := tr.Al(context.Background()); err == nil {
		t.Fatal("sınırı aşan dizi kabul edildi")
	}
}
