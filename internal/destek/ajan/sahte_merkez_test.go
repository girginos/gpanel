package ajan

// sahte_merkez_test.go — testler için merkez (bot.girginos.io) taklidi: WSS ve uzun
// yoklama uçları, merkez Ed25519 anahtarıyla meydan okuma / imzalı mesaj üretimi.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "girginospanel/internal/destek/protokol"
)

type gelenMesaj struct {
	z *p.Zarf
	g p.Govde
}

type sahteMerkez struct {
	t        *testing.T
	cift     *p.AnahtarCifti
	imz      *p.Imzalayici
	srv      *httptest.Server
	gelen    chan gelenMesaj
	baglanti atomic.Int32
	kayitRed atomic.Value // p.HataKodu
	// sahteci: ayarlanırsa meydan okuma BU (ajanın tanımadığı) anahtarla imzalanır.
	sahteci  *p.Imzalayici
	yanitSay atomic.Int32

	mu       sync.Mutex
	birikim  []gelenMesaj // bekle'nin henüz istenmemiş mesajları
	ws       *sunucuWS
	kuyruk   [][]byte // yoklama: ajana gidecekler
	belirtec string
	meydan   *p.MeydanOkuma
	kayit    *p.Kayit
}

func yeniSahteMerkez(t *testing.T) *sahteMerkez {
	t.Helper()
	cift, err := p.AnahtarCiftiUret(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := &sahteMerkez{t: t, cift: cift, imz: p.YeniImzalayici(cift), gelen: make(chan gelenMesaj, 1000)}
	m.kayitRed.Store(p.HataKodu(""))
	mux := http.NewServeMux()
	mux.HandleFunc(p.WSSYolu, m.wsIsle)
	mux.HandleFunc(p.YoklamaKayitYolu, m.yKayit)
	mux.HandleFunc(p.YoklamaYanitYolu, m.yYanit)
	mux.HandleFunc(p.YoklamaAlYolu, m.yAl)
	mux.HandleFunc(p.YoklamaGonderYolu, m.yGonder)
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *sahteMerkez) kodla(g p.Govde) ([]byte, string) {
	k := mesajKimligi()
	b, err := p.Kodla(k, g)
	if err != nil {
		m.t.Errorf("sahte merkez kodlama: %v", err)
	}
	return b, k
}

func (m *sahteMerkez) wsIsle(w http.ResponseWriter, r *http.Request) {
	m.baglanti.Add(1)
	s, err := wsSunucuEl(w, r, p.WSAltProtokol)
	if err != nil {
		return
	}
	defer s.c.Close()
	veri, err := s.oku()
	if err != nil {
		return
	}
	_, g, err := p.Coz(veri, p.YonMerkeze)
	kay, ok := g.(*p.Kayit)
	if err != nil || !ok {
		m.t.Errorf("ilk mesaj kayit değil: %v", err)
		return
	}
	if red := m.kayitRed.Load().(p.HataKodu); red != "" {
		b, _ := m.kodla(&p.KayitSonuc{Kabul: false, HataKodu: red})
		_ = s.yaz(0x1, b, true)
		return
	}
	imz := m.imz
	if m.sahteci != nil {
		imz = m.sahteci
	}
	mo, err := imz.MeydanOlustur(kay)
	if err != nil {
		m.t.Errorf("meydan: %v", err)
		return
	}
	b, _ := m.kodla(mo)
	_ = s.yaz(0x1, b, true)
	veri, err = s.oku()
	if err != nil {
		return
	}
	m.yanitSay.Add(1)
	_, g, err = p.Coz(veri, p.YonMerkeze)
	y, ok := g.(*p.MeydanYanit)
	if err != nil || !ok {
		m.t.Errorf("meydan_yanit bekleniyordu: %v", err)
		return
	}
	if err := p.MeydanYanitDogrula(mo, kay, y, time.Now()); err != nil {
		m.t.Errorf("cihaz imzası doğrulanamadı: %v", err)
		return
	}
	b, _ = m.kodla(&p.KayitSonuc{Kabul: true, SunucuKimligi: "sunucu-test-0001", KalpAraligiSn: 5})
	_ = s.yaz(0x1, b, true)
	m.mu.Lock()
	m.ws = s
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.ws == s {
			m.ws = nil
		}
		m.mu.Unlock()
	}()
	for {
		veri, err := s.oku()
		if err != nil {
			return
		}
		m.isle(veri)
	}
}

func (m *sahteMerkez) isle(veri []byte) {
	z, g, err := p.Coz(veri, p.YonMerkeze)
	if err != nil {
		m.t.Errorf("ajandan geçersiz çerçeve: %v\n%s", err, veri)
		return
	}
	if _, kalp := g.(*p.KalpAtisi); kalp {
		return
	}
	select {
	case m.gelen <- gelenMesaj{z: z, g: g}:
	default:
	}
}

// ---- uzun yoklama uçları ----

func (m *sahteMerkez) govde(r *http.Request) []byte {
	b, _ := io.ReadAll(io.LimitReader(r.Body, int64(p.CerceveSiniri)))
	return b
}

func (m *sahteMerkez) yKayit(w http.ResponseWriter, r *http.Request) {
	m.baglanti.Add(1)
	_, g, err := p.Coz(m.govde(r), p.YonMerkeze)
	kay, ok := g.(*p.Kayit)
	if err != nil || !ok {
		http.Error(w, "kayit", 400)
		return
	}
	if red := m.kayitRed.Load().(p.HataKodu); red != "" {
		b, _ := m.kodla(&p.KayitSonuc{Kabul: false, HataKodu: red})
		_, _ = w.Write(b)
		return
	}
	mo, _ := m.imz.MeydanOlustur(kay)
	m.mu.Lock()
	m.meydan, m.kayit = mo, kay
	m.mu.Unlock()
	b, _ := m.kodla(mo)
	_, _ = w.Write(b)
}

func (m *sahteMerkez) yYanit(w http.ResponseWriter, r *http.Request) {
	_, g, err := p.Coz(m.govde(r), p.YonMerkeze)
	y, ok := g.(*p.MeydanYanit)
	m.mu.Lock()
	mo, kay := m.meydan, m.kayit
	m.mu.Unlock()
	if err != nil || !ok || mo == nil || p.MeydanYanitDogrula(mo, kay, y, time.Now()) != nil {
		http.Error(w, "yanit", 400)
		return
	}
	bel := make([]byte, 32)
	_, _ = rand.Read(bel)
	b64 := base64.RawURLEncoding.EncodeToString(bel)
	m.mu.Lock()
	m.belirtec = b64
	m.meydan = nil
	m.mu.Unlock()
	b, _ := m.kodla(&p.KayitSonuc{Kabul: true, SunucuKimligi: "sunucu-test-0001", OturumBelirteci: b64, KalpAraligiSn: 5})
	_, _ = w.Write(b)
}

func (m *sahteMerkez) yetkili(r *http.Request) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.belirtec != "" && r.Header.Get(p.YoklamaBelirtecBasligi) == "Bearer "+m.belirtec
}

func (m *sahteMerkez) yAl(w http.ResponseWriter, r *http.Request) {
	if !m.yetkili(r) {
		http.Error(w, "yetki", 401)
		return
	}
	son := time.Now().Add(time.Second)
	for time.Now().Before(son) {
		m.mu.Lock()
		if len(m.kuyruk) > 0 {
			dizi := make([]json.RawMessage, 0, len(m.kuyruk))
			for _, k := range m.kuyruk {
				dizi = append(dizi, json.RawMessage(k))
			}
			m.kuyruk = nil
			m.mu.Unlock()
			b, _ := json.Marshal(dizi)
			_, _ = w.Write(b)
			return
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = w.Write([]byte("[]"))
}

func (m *sahteMerkez) yGonder(w http.ResponseWriter, r *http.Request) {
	if !m.yetkili(r) {
		http.Error(w, "yetki", 401)
		return
	}
	m.isle(m.govde(r))
	w.WriteHeader(http.StatusNoContent)
}

// ---- test yardımcıları ----

// gonderHam: çerçeveyi ajana yollar (WSS ya da yoklama kuyruğu).
func (m *sahteMerkez) gonderHam(b []byte) {
	m.mu.Lock()
	s := m.ws
	if s == nil {
		m.kuyruk = append(m.kuyruk, b)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	if err := s.yaz(0x1, b, true); err != nil {
		m.t.Errorf("ajana yazılamadı: %v", err)
	}
}

// gonder: gövdeyi kodlar ve yollar; zarf kimliğini döner.
func (m *sahteMerkez) gonder(g p.Govde) (string, []byte) {
	b, k := m.kodla(g)
	m.gonderHam(b)
	return k, b
}

func (m *sahteMerkez) imzala(g p.ImzaliMesaj) {
	if err := m.imz.Imzala(g, 30*time.Second); err != nil {
		m.t.Fatalf("imza: %v", err)
	}
}

// bekle: tipi (ve koşulu) tutan sıradaki gövde; tutmayanlar birikimde SAKLANIR
// (sonraki bekle onları görür — sıra korunur).
func (m *sahteMerkez) bekle(tip p.Tip, kosul func(p.Govde) bool) p.Govde {
	m.t.Helper()
	tutar := func(gm gelenMesaj) bool { return gm.g.Tip() == tip && (kosul == nil || kosul(gm.g)) }
	m.mu.Lock()
	for i, gm := range m.birikim {
		if tutar(gm) {
			m.birikim = append(m.birikim[:i:i], m.birikim[i+1:]...)
			m.mu.Unlock()
			return gm.g
		}
	}
	m.mu.Unlock()
	son := time.After(10 * time.Second)
	for {
		select {
		case gm := <-m.gelen:
			if tutar(gm) {
				return gm.g
			}
			m.mu.Lock()
			m.birikim = append(m.birikim, gm)
			m.mu.Unlock()
		case <-son:
			m.t.Fatalf("merkez %s mesajını almadı", tip)
			return nil
		}
	}
}

// isSonucu: is_kodu için sonuç.
func (m *sahteMerkez) isSonucu(isKodu string) *p.IsSonucu {
	m.t.Helper()
	return m.bekle(p.TipIsSonucu, func(g p.Govde) bool { return g.(*p.IsSonucu).IsKodu == isKodu }).(*p.IsSonucu)
}

// hata: ilgili zarf kimliği için hata mesajı.
func (m *sahteMerkez) hata(ilgili string) *p.HataMesaji {
	m.t.Helper()
	return m.bekle(p.TipHata, func(g p.Govde) bool { return g.(*p.HataMesaji).Ilgili == ilgili }).(*p.HataMesaji)
}

// isEmri: imzalı iş emri üretir (gönderMEZ).
func (m *sahteMerkez) isEmri(bagID, arac, param string) *p.IsEmri {
	k, _ := p.YeniIsKodu(nil)
	e := &p.IsEmri{IsKodu: k, Oturum: "oturum-test-01", BaglantiID: bagID, Arac: arac, Param: json.RawMessage(param), Risk: p.R0}
	m.imzala(e)
	return e
}

func (m *sahteMerkez) bosalt() {
	for {
		select {
		case <-m.gelen:
		default:
			return
		}
	}
}

var _ = strings.TrimSpace
