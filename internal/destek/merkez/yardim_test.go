package merkez

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/destek/bilet"
	"girginospanel/internal/destek/protokol"
)

const (
	tModelTriyaj = "claude-haiku-4-5-20251001"
	tModelAna    = "claude-sonnet-5-5"
	tModelOpus   = "claude-opus-5-5"
	tDestekGrubu = int64(-100777)
)

// ---- sahte Claude ----

type sahteClaude struct {
	mu       sync.Mutex
	istekler []ClaudeIstek
	anaSay   int
	ana      func(n int, ist ClaudeIstek) (*ClaudeYanit, error)
	niyet    string
	guvensiz bool
}

func (s *sahteClaude) Gonder(_ context.Context, ist ClaudeIstek) (*ClaudeYanit, error) {
	s.mu.Lock()
	s.istekler = append(s.istekler, ist)
	switch {
	case bytes.Contains(ist.Govde, []byte(`"tool_choice":{"type":"tool","name":"siniflandir"}`)):
		niyet := s.niyet
		if niyet == "" {
			niyet = "destek"
		}
		s.mu.Unlock()
		return yanit("tool_use", bArac("tu_t", "siniflandir", `{"niyet":"`+niyet+`","dil":"tr"}`)), nil
	case bytes.Contains(ist.Govde, []byte(`"tool_choice":{"type":"tool","name":"denetim"}`)):
		g := !s.guvensiz
		s.mu.Unlock()
		return yanit("tool_use", bArac("tu_d", "denetim", fmt.Sprintf(`{"guvenli":%t,"neden":"yok"}`, g))), nil
	}
	n := s.anaSay
	s.anaSay++
	f := s.ana
	s.mu.Unlock()
	if f == nil {
		return yanit("end_turn", bMetin("tamam")), nil
	}
	return f(n, ist)
}

func (s *sahteClaude) anaIstekler() []ClaudeIstek {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ClaudeIstek
	for _, i := range s.istekler {
		if !bytes.Contains(i.Govde, []byte(`"tool_choice":{"type":"tool","name":"siniflandir"}`)) &&
			!bytes.Contains(i.Govde, []byte(`"tool_choice":{"type":"tool","name":"denetim"}`)) {
			out = append(out, i)
		}
	}
	return out
}

func (s *sahteClaude) toplam() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.istekler)
}

func bMetin(s string) string { return `{"type":"text","text":` + string(jsonDizge(s)) + `}` }
func bArac(id, ad, girdi string) string {
	return `{"type":"tool_use","id":"` + id + `","name":"` + ad + `","input":` + girdi + `}`
}

func yanit(stop string, bloklar ...string) *ClaudeYanit {
	y := &ClaudeYanit{ID: "msg_test", StopReason: stop, Usage: Kullanim{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 4000, CacheCreationInputTokens: 0}}
	for _, b := range bloklar {
		y.Content = append(y.Content, json.RawMessage(b))
	}
	return y
}

// ---- sahte Telegram ----

type tgKayit struct {
	Tur      string
	Chat     int64
	Mesaj    int64
	Metin    string
	Dugmeler [][]Dugme
}

type sahteTG struct {
	mu    sync.Mutex
	kayit []tgKayit
	id    int64
}

func (s *sahteTG) MesajGonder(_ context.Context, m GidenMesaj) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id++
	s.kayit = append(s.kayit, tgKayit{Tur: "gonder", Chat: m.ChatID, Mesaj: s.id, Metin: m.Metin, Dugmeler: m.Dugmeler})
	return s.id, nil
}

func (s *sahteTG) MesajDuzenle(_ context.Context, chat, mesaj int64, metin string, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kayit = append(s.kayit, tgKayit{Tur: "duzenle", Chat: chat, Mesaj: mesaj, Metin: metin})
	return nil
}

func (s *sahteTG) EylemGonder(context.Context, int64, string) error { return nil }

func (s *sahteTG) CallbackYanitla(_ context.Context, id, metin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kayit = append(s.kayit, tgKayit{Tur: "callback", Metin: metin})
	return nil
}

func (s *sahteTG) hepsi() []tgKayit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tgKayit(nil), s.kayit...)
}

func (s *sahteTG) metinler(chat int64) []string {
	var out []string
	for _, k := range s.hepsi() {
		if k.Chat == chat && (k.Tur == "gonder" || k.Tur == "duzenle") {
			out = append(out, k.Metin)
		}
	}
	return out
}

func (s *sahteTG) son(chat int64) string {
	m := s.metinler(chat)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

// ---- ortam ----

// Sahte sırlar çalışma zamanında birleştirilir (gitleaks gerçek sır sanmasın).
var (
	tTelegramToken = "123456789" + ":" + strings.Repeat("A", 35)
	tAnthropic     = "sk-" + "ant-api03-TESTANAHTARI-0123456789abcdef"
	tWebhookGizli  = "webhook-gizli-0123456789"
	tPortalIc      = "portal-ic-test-anahtari-0123456789abcdef"
)

type ortam struct {
	m    *Merkez
	c    *sahteClaude
	tg   *sahteTG
	srv  *httptest.Server
	ic   *httptest.Server // iç dinleyici (portal API'si)
	depo *DosyaDepo
	sir  *Sirlar
	upd  atomic.Int64
	// portalSk: sahte portalın (lisans sunucusu) bilet imza anahtarı; açık yarısı yapılandırmadadır.
	portalSk ed25519.PrivateKey
}

func testYap() *Yapilandirma {
	f := Fiyat{GirisMTok: 3, CikisMTok: 15, CacheYazma5dkMTok: 3.75, CacheYazma1saMTok: 6, CacheOkumaMTok: 0.3}
	y := &Yapilandirma{
		Modeller:      Modeller{Triyaj: tModelTriyaj, Ana: tModelAna, Yukseltme: tModelOpus},
		Fiyatlar:      map[string]Fiyat{tModelTriyaj: f, tModelAna: f, tModelOpus: f},
		Butce:         Butce{OturumTokenTavani: 1_000_000, SunucuGunlukUSD: 100, SunucuAylikUSD: 1000, GenelGunlukUSD: 100, GenelAylikUSD: 1000},
		DestekGrubuID: tDestekGrubu,
	}
	y.Varsayilanlar()
	return y
}

func yeniOrtam(t *testing.T, degis func(*Yapilandirma)) *ortam {
	t.Helper()
	y := testYap()
	if degis != nil {
		degis(y)
	}
	cift, err := protokol.AnahtarCiftiUret(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	y.PortalBiletAnahtarlari = []string{protokol.AcikAnahtarKodla(pub)}
	o := &ortam{c: &sahteClaude{}, tg: &sahteTG{}, depo: YeniBellekDepo(), portalSk: sk,
		sir: &Sirlar{TelegramToken: tTelegramToken, AnthropicAnahtar: tAnthropic, WebhookGizli: tWebhookGizli, MerkezAnahtar: cift, PortalIcAnahtar: tPortalIc}}
	o.m, err = Yeni(Secenekler{Yap: y, Sirlar: o.sir, Depo: o.depo, Claude: o.c, Telegram: o.tg,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	o.srv = httptest.NewServer(o.m.Handler())
	o.ic = httptest.NewServer(o.m.IcHandler())
	t.Cleanup(func() {
		o.srv.CloseClientConnections()
		o.srv.Close()
		o.ic.Close()
	})
	return o
}

// bilet: sahte portalın imzaladığı lisans bileti (fp için geçerli; mut ile bozulabilir).
func (o *ortam) bilet(fp string, mut func(*bilet.Govde)) []byte {
	g := bilet.Govde{FP: fp, Urun: "gpanel-suite", Durum: bilet.DurumAktif,
		Bitis: time.Now().Add(24 * time.Hour).Unix(), Iat: time.Now().Add(-time.Minute).Unix(), Pkt: "0"}
	if mut != nil {
		mut(&g)
	}
	b, _ := json.Marshal(g)
	e := base64.RawURLEncoding
	return []byte(e.EncodeToString(b) + "." + e.EncodeToString(ed25519.Sign(o.portalSk, b)))
}

// portal: iç API çağrısı (secret boşsa Authorization başlığı yok). Dönüş: durum kodu + JSON gövdesi.
func (o *ortam) portal(t *testing.T, yontem, yol string, govde any, secret string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if govde != nil {
		b, _ := json.Marshal(govde)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(yontem, o.ic.URL+yol, r)
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// kodAl: portal iç API'sinden eşleştirme kodu ister; kodu ve özetini döndürür.
func (o *ortam) kodAl(t *testing.T, fp, duzey string, gun int) (string, string) {
	t.Helper()
	d, m := o.portal(t, http.MethodPost, "/ic/v1/eslesme-kodu", map[string]any{"parmak_izi": fp, "duzey": duzey, "sure_gun": gun,
		"portal_kullanici_id": "u_42", "aydinlatma_surum": "2026-10-04.1"}, tPortalIc)
	if d != http.StatusOK {
		t.Fatalf("eslesme-kodu: %d %v", d, m)
	}
	kod, _ := m["kod"].(string)
	ozet, ok := protokol.EslesmeKoduOzet(kod)
	if !ok {
		t.Fatalf("kod biçimi: %q", kod)
	}
	return kod, ozet
}

func (o *ortam) webhook(t *testing.T, u map[string]any, gizli string) int {
	t.Helper()
	if _, var_ := u["update_id"]; !var_ {
		u["update_id"] = o.upd.Add(1)
	}
	b, _ := json.Marshal(u)
	req, _ := http.NewRequest(http.MethodPost, o.srv.URL+protokol.TelegramWebhookYolu, bytes.NewReader(b))
	if gizli != "" {
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", gizli)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	o.m.Bekle()
	return resp.StatusCode
}

func (o *ortam) mesaj(t *testing.T, tg int64, txt string) {
	t.Helper()
	o.webhook(t, map[string]any{"message": map[string]any{
		"message_id": 1, "text": txt,
		"from": map[string]any{"id": tg, "is_bot": false, "username": "kullanici_" + fmt.Sprint(tg%1000)},
		"chat": map[string]any{"id": tg, "type": "private"},
	}}, tWebhookGizli)
}

func (o *ortam) callback(t *testing.T, tg int64, veri string) {
	t.Helper()
	o.webhook(t, map[string]any{"callback_query": map[string]any{
		"id": "cb1", "data": veri, "from": map[string]any{"id": tg, "is_bot": false},
		"message": map[string]any{"message_id": 5, "chat": map[string]any{"id": tg, "type": "private"}},
	}}, tWebhookGizli)
}

func (o *ortam) baglantiEkle(t *testing.T, tg int64, sunucu string, rol protokol.Rol, alanlar ...string) Baglanti {
	t.Helper()
	b := Baglanti{BaglantiID: fmt.Sprintf("bag_%d_%s", tg, strings.TrimPrefix(sunucu, "s_")), SunucuKimligi: sunucu, TgUserID: tg,
		Durum: protokol.BaglantiEtkin, Rol: rol, Duzey: protokol.DuzeyOkuma, KVKKKabul: time.Now()}
	for i, a := range alanlar {
		b.Hesaplar = append(b.Hesaplar, protokol.HesapEtiketi{Sira: i + 1, Etiket: a})
	}
	if err := o.depo.BaglantiKaydet(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func bekle(t *testing.T, kosul func() bool) {
	t.Helper()
	son := time.Now().Add(3 * time.Second)
	for time.Now().Before(son) {
		if kosul() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("koşul zaman aşımı")
}

// ---- test WebSocket istemcisi (ajan tarafı) ----

type testWS struct {
	c   net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

func wsBaglan(t *testing.T, srvURL string, basliklar map[string]string) (*testWS, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srvURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	k := make([]byte, 16)
	_, _ = rand.Read(k)
	anahtar := base64.StdEncoding.EncodeToString(k)
	h := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Sec-WebSocket-Key": anahtar,
		"Sec-WebSocket-Version": "13", "Sec-WebSocket-Protocol": protokol.WSAltProtokol}
	for a, v := range basliklar {
		h[a] = v
	}
	var sb strings.Builder
	sb.WriteString("GET " + protokol.WSSYolu + " HTTP/1.1\r\nHost: test\r\n")
	for a, v := range h {
		if v != "" {
			sb.WriteString(a + ": " + v + "\r\n")
		}
	}
	sb.WriteString("\r\n")
	if _, err := c.Write([]byte(sb.String())); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		s := sha1.Sum([]byte(anahtar + wsGUID))
		if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(s[:]) {
			t.Fatal("accept uyuşmuyor")
		}
	}
	t.Cleanup(func() { c.Close() })
	return &testWS{c: c, br: br}, resp
}

func (w *testWS) cerceve(op byte, veri []byte, fin, maskeli bool) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	b0 := op
	if fin {
		b0 |= 0x80
	}
	hdr := []byte{b0}
	m := byte(0)
	if maskeli {
		m = 0x80
	}
	n := len(veri)
	switch {
	case n <= 125:
		hdr = append(hdr, m|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, m|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, m|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	out := append([]byte(nil), veri...)
	if maskeli {
		mk := []byte{0x11, 0x22, 0x33, 0x44}
		hdr = append(hdr, mk...)
		for i := range out {
			out[i] ^= mk[i%4]
		}
	}
	_, err := w.c.Write(append(hdr, out...))
	return err
}

func (w *testWS) yaz(veri []byte) error { return w.cerceve(wsOpMetin, veri, true, true) }

// oku: sunucu çerçevesi (maskesiz). Kalp atışı dahil her metin mesajını döndürür.
func (w *testWS) oku(sure time.Duration) (byte, []byte, error) {
	_ = w.c.SetReadDeadline(time.Now().Add(sure))
	var bas [2]byte
	if _, err := io.ReadFull(w.br, bas[:]); err != nil {
		return 0, nil, err
	}
	n := uint64(bas[1] & 0x7F)
	switch n {
	case 126:
		var u [2]byte
		io.ReadFull(w.br, u[:])
		n = uint64(binary.BigEndian.Uint16(u[:]))
	case 127:
		var u [8]byte
		io.ReadFull(w.br, u[:])
		n = binary.BigEndian.Uint64(u[:])
	}
	v := make([]byte, n)
	if _, err := io.ReadFull(w.br, v); err != nil {
		return 0, nil, err
	}
	return bas[0] & 0x0F, v, nil
}

// ---- sahte ajan ----

type sahteAjan struct {
	t      *testing.T
	ws     *testWS
	cift   *protokol.AnahtarCifti
	kayit  *protokol.Kayit
	dog    *protokol.Dogrulayici
	Sunucu string
	baglam protokol.OturumBaglami
	isle   func(e *protokol.IsEmri) *protokol.IsSonucu

	mu         sync.Mutex
	emirler    []*protokol.IsEmri
	eslesmeler []*protokol.EslesmeOnayla
	eslesmeZrf []string // eslesme_onayla zarf kimlikleri (hata.ilgili ile yanıt için)
	hatalar    []*protokol.HataMesaji
	iptaller   []*protokol.BaglantiIptal
	imzaHatasi int
	yerelRed   int
}

func yeniKayit(t *testing.T, cift *protokol.AnahtarCifti) *protokol.Kayit {
	t.Helper()
	return &protokol.Kayit{
		CihazAnahtari:   protokol.AcikAnahtarKodla(cift.Acik),
		LisansParmakIzi: strings.Repeat("ab", 32),
		LisansIspati:    base64.RawURLEncoding.EncodeToString([]byte("lisans-ispati")),
		PanelSurumu:     "0.3.0-test",
		KatalogOzeti:    protokol.KatalogOzeti(),
	}
}

func zarfla(t *testing.T, g protokol.Govde) []byte {
	t.Helper()
	b, err := protokol.Kodla(yeniKimlik(), g)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (a *sahteAjan) okuGovde(sure time.Duration) protokol.Govde {
	a.t.Helper()
	for {
		op, v, err := a.ws.oku(sure)
		if err != nil {
			a.t.Fatalf("ajan okuma: %v", err)
		}
		if op != wsOpMetin {
			continue
		}
		_, g, err := protokol.Coz(v, protokol.YonAjana)
		if err != nil {
			a.t.Fatalf("ajan çözme: %v", err)
		}
		if _, ok := g.(*protokol.KalpAtisi); ok {
			continue
		}
		return g
	}
}

// sahteAjanBagla: WSS üzerinden tam kayıt el sıkışması (merkez imzası sabitlenmiş anahtarla doğrulanır).
func sahteAjanBagla(t *testing.T, o *ortam, isle func(e *protokol.IsEmri) *protokol.IsSonucu) *sahteAjan {
	t.Helper()
	return sahteAjanBaglaFP(t, o, isle, strings.Repeat("ab", 32), true)
}

// sahteAjanBaglaFP: parmak izi seçilebilir; gecerliBilet=false → ispat geçersiz ("bilet-yok").
func sahteAjanBaglaFP(t *testing.T, o *ortam, isle func(e *protokol.IsEmri) *protokol.IsSonucu, fp string, gecerliBilet bool) *sahteAjan {
	t.Helper()
	ispat := []byte("bilet-yok")
	if gecerliBilet {
		ispat = o.bilet(fp, nil)
	}
	return sahteAjanBaglaOzel(t, o, isle, fp, ispat)
}

// sahteAjanBaglaOzel: ham ispat baytlarıyla kayıt.
func sahteAjanBaglaOzel(t *testing.T, o *ortam, isle func(e *protokol.IsEmri) *protokol.IsSonucu, fp string, ispat []byte) *sahteAjan {
	t.Helper()
	cift, _ := protokol.AnahtarCiftiUret(nil)
	dog, err := protokol.YeniDogrulayici(nil, protokol.YeniBellekOnbellek(0), o.sir.MerkezAnahtar.Acik)
	if err != nil {
		t.Fatal(err)
	}
	ws, resp := wsBaglan(t, o.srv.URL, nil)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("ws: %d", resp.StatusCode)
	}
	a := &sahteAjan{t: t, ws: ws, cift: cift, kayit: yeniKayit(t, cift), dog: dog, isle: isle}
	a.kayit.LisansParmakIzi = fp
	a.kayit.LisansIspati = base64.RawURLEncoding.EncodeToString(ispat)
	if err := ws.yaz(zarfla(t, a.kayit)); err != nil {
		t.Fatal(err)
	}
	md, ok := a.okuGovde(3 * time.Second).(*protokol.MeydanOkuma)
	if !ok {
		t.Fatal("meydan_okuma bekleniyordu")
	}
	if err := dog.MeydanDogrula(md, a.kayit); err != nil {
		t.Fatalf("merkez meydan imzası doğrulanamadı: %v", err)
	}
	y, err := protokol.MeydanYanitla(cift.Ozel, md, a.kayit)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.yaz(zarfla(t, y)); err != nil {
		t.Fatal(err)
	}
	ks, ok := a.okuGovde(3 * time.Second).(*protokol.KayitSonuc)
	if !ok || !ks.Kabul {
		t.Fatalf("kayıt reddedildi: %+v", ks)
	}
	a.Sunucu = ks.SunucuKimligi
	go a.dongu()
	bekle(t, func() bool { return o.m.ajan.Cevrimici(a.Sunucu) })
	return a
}

func (a *sahteAjan) gonder(g protokol.Govde) {
	b, err := protokol.Kodla(yeniKimlik(), g)
	if err != nil {
		panic(err)
	}
	_ = a.ws.yaz(b)
}

func (a *sahteAjan) dongu() {
	for {
		op, v, err := a.ws.oku(time.Minute)
		if err != nil {
			return
		}
		if op != wsOpMetin {
			continue
		}
		z, g, err := protokol.Coz(v, protokol.YonAjana)
		if err != nil {
			continue
		}
		switch m := g.(type) {
		case *protokol.IsEmri:
			if err := a.dog.Dogrula(m); err != nil {
				a.mu.Lock()
				a.imzaHatasi++
				a.mu.Unlock()
				a.gonder(&protokol.HataMesaji{Kod: protokol.HataKoduAl(err), Ilgili: m.IsKodu})
				continue
			}
			// 2. hat (bağlayıcı): YEREL kapsamla doğrulama
			if _, err := protokol.AracGirdisiCoz(a.baglam, m.Arac, m.Param); err != nil {
				a.mu.Lock()
				a.yerelRed++
				a.mu.Unlock()
				a.gonder(&protokol.IsSonucu{IsKodu: m.IsKodu, HataKodu: protokol.HataKoduAl(err)})
				continue
			}
			a.mu.Lock()
			a.emirler = append(a.emirler, m)
			a.mu.Unlock()
			s := &protokol.IsSonucu{Ok: true, Ozet: "ok"}
			if a.isle != nil {
				s = a.isle(m)
			}
			s.IsKodu = m.IsKodu
			a.gonder(s)
		case *protokol.EslesmeOnayla:
			if err := a.dog.Dogrula(m); err != nil {
				a.mu.Lock()
				a.imzaHatasi++
				a.mu.Unlock()
				continue
			}
			a.mu.Lock()
			a.eslesmeler = append(a.eslesmeler, m)
			a.eslesmeZrf = append(a.eslesmeZrf, z.Kimlik)
			a.mu.Unlock()
		case *protokol.HataMesaji:
			a.mu.Lock()
			a.hatalar = append(a.hatalar, m)
			a.mu.Unlock()
		case *protokol.BaglantiIptal:
			a.mu.Lock()
			a.iptaller = append(a.iptaller, m)
			a.mu.Unlock()
		}
	}
}

func (a *sahteAjan) emirAdlari() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.emirler {
		out = append(out, e.Arac)
	}
	return out
}
