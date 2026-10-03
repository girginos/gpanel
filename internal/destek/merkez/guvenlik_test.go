package merkez

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/protokol"
)

func TestWebhookGizliBaslikReddi(t *testing.T) {
	o := yeniOrtam(t, nil)
	u := func() map[string]any {
		return map[string]any{"message": map[string]any{"message_id": 1, "text": "/start",
			"from": map[string]any{"id": 42, "is_bot": false}, "chat": map[string]any{"id": 42, "type": "private"}}}
	}
	if k := o.webhook(t, u(), ""); k != http.StatusUnauthorized {
		t.Fatalf("başlıksız: %d", k)
	}
	if k := o.webhook(t, u(), "yanlis-gizli-0123456789"); k != http.StatusUnauthorized {
		t.Fatalf("yanlış gizli: %d", k)
	}
	if k := o.webhook(t, u(), tWebhookGizli[:len(tWebhookGizli)-1]); k != http.StatusUnauthorized {
		t.Fatalf("önek gizli: %d", k)
	}
	if len(o.tg.hepsi()) != 0 {
		t.Fatal("reddedilen webhook işlendi")
	}
	if k := o.webhook(t, u(), tWebhookGizli); k != http.StatusOK || len(o.tg.metinler(42)) != 1 {
		t.Fatalf("doğru gizli: %d", k)
	}
	// aynı update_id tekrarı işlenmez
	t2 := u()
	t2["update_id"] = int64(999)
	o.webhook(t, t2, tWebhookGizli)
	t3 := u()
	t3["update_id"] = int64(999)
	o.webhook(t, t3, tWebhookGizli)
	if n := len(o.tg.metinler(42)); n != 2 {
		t.Fatalf("tekrar güncelleme işlendi: %d", n)
	}
}

func TestGrupSohbetiSessiz(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
	for _, tur := range []string{"group", "supergroup", "channel"} {
		for _, txt := range []string{"/start ABCDEF123", "sitem çöktü", "/unutbeni"} {
			o.webhook(t, map[string]any{"message": map[string]any{"message_id": 1, "text": txt,
				"from": map[string]any{"id": tTg, "is_bot": false}, "chat": map[string]any{"id": -1001, "type": tur}}}, tWebhookGizli)
		}
	}
	// bot kullanıcıdan gelen özel mesaj da sessiz
	o.webhook(t, map[string]any{"message": map[string]any{"message_id": 1, "text": "x",
		"from": map[string]any{"id": tTg, "is_bot": true}, "chat": map[string]any{"id": tTg, "type": "private"}}}, tWebhookGizli)
	if n := len(o.tg.hepsi()); n != 0 {
		t.Fatalf("grup/bot mesajına yanıt verildi: %d", n)
	}
	if o.c.toplam() != 0 {
		t.Fatal("grup mesajı Claude'a gitti")
	}
}

func TestHizSiniriBaglantiBasina(t *testing.T) {
	o := yeniOrtam(t, nil)
	for i := 0; i < 13; i++ {
		o.mesaj(t, 8001, "merhaba")
	}
	m := o.tg.metinler(8001)
	hiz := 0
	for _, s := range m {
		if strings.Contains(s, "Çok hızlı") {
			hiz++
		}
	}
	if len(m) != 11 || hiz != 1 {
		t.Fatalf("mesaj %d, hız uyarısı %d (beklenen 11/1)", len(m), hiz)
	}
	// başka kullanıcı etkilenmez
	o.mesaj(t, 8002, "merhaba")
	if len(o.tg.metinler(8002)) != 1 {
		t.Fatal("hız sınırı kullanıcılar arası paylaşılıyor")
	}
}

func TestSunucuEszamanliOturumSiniri(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	for _, tg := range []int64{9001, 9002, 9003} {
		o.baglantiEkle(t, tg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		o.mesaj(t, tg, "yardım")
	}
	if !strings.Contains(o.tg.son(9003), "başka destek oturumları") {
		t.Fatalf("3. oturum kabul edildi: %q", o.tg.son(9003))
	}
	if n := len(o.c.anaIstekler()); n != 2 {
		t.Fatalf("ana çağrı %d", n)
	}
	// aynı kullanıcı kendi oturumunu sürdürebilir
	o.mesaj(t, 9001, "devam")
	if n := len(o.c.anaIstekler()); n != 3 {
		t.Fatalf("mevcut oturum sürdürülemedi: %d", n)
	}
}

func TestButceVeKesici(t *testing.T) {
	t.Run("sunucu_gunluk", func(t *testing.T) {
		o := yeniOrtam(t, func(y *Yapilandirma) { y.Butce.SunucuGunlukUSD = 0.5 })
		ajan := sahteAjanBagla(t, o, nil)
		o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		_ = o.depo.MaliyetEkle(ajan.Sunucu, time.Now(), 0.6)
		o.mesaj(t, tTg, "yardım")
		if o.c.toplam() != 0 || !strings.Contains(o.tg.son(tTg), "insan desteğine aktarıyorum") {
			t.Fatalf("bütçe aşımında çağrı yapıldı (%d) / %q", o.c.toplam(), o.tg.son(tTg))
		}
		if len(o.tg.metinler(tDestekGrubu)) != 1 {
			t.Fatal("insan devri bileti yok")
		}
	})
	t.Run("genel_aylik", func(t *testing.T) {
		o := yeniOrtam(t, func(y *Yapilandirma) { y.Butce.GenelAylikUSD = 1 })
		ajan := sahteAjanBagla(t, o, nil)
		o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		_ = o.depo.MaliyetEkle("s_baska", time.Now(), 1.5)
		o.mesaj(t, tTg, "yardım")
		if o.c.toplam() != 0 {
			t.Fatal("genel bütçe aşımında çağrı yapıldı")
		}
	})
	t.Run("oturum_tavani", func(t *testing.T) {
		o := yeniOrtam(t, func(y *Yapilandirma) { y.Butce.OturumTokenTavani = 500 })
		ajan := sahteAjanBagla(t, o, nil)
		o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		o.mesaj(t, tTg, "birinci") // 100+50+4000/10 = 550 ≥ 500
		n := o.c.toplam()
		o.mesaj(t, tTg, "ikinci")
		if o.c.toplam() != n || !strings.Contains(o.tg.son(tTg), "insan desteğine aktarıyorum") {
			t.Fatal("oturum token tavanı uygulanmadı")
		}
	})
	t.Run("acil_kesici", func(t *testing.T) {
		kesici := filepath.Join(t.TempDir(), "kesici")
		o := yeniOrtam(t, func(y *Yapilandirma) { y.AcilKesiciDosyasi = kesici })
		ajan := sahteAjanBagla(t, o, nil)
		o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		o.mesaj(t, tTg, "a")
		n := o.c.toplam()
		if n == 0 {
			t.Fatal("kesici yokken çağrı yapılmadı")
		}
		os.WriteFile(kesici, nil, 0o600)
		o.mesaj(t, tTg, "b")
		if o.c.toplam() != n {
			t.Fatal("acil kesici etkin iken çağrı yapıldı")
		}
	})
	t.Run("defter", func(t *testing.T) {
		o := yeniOrtam(t, nil)
		ajan := sahteAjanBagla(t, o, nil)
		o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
		o.mesaj(t, tTg, "a")
		// triyaj + ana + denetim = 3 çağrı × (100×3 + 50×15 + 4000×0.3)/1e6
		bek := 3 * (100*3.0 + 50*15.0 + 4000*0.3) / 1e6
		g, a := o.depo.Maliyet(ajan.Sunucu, time.Now())
		gg, _ := o.depo.Maliyet("*", time.Now())
		if d := g - bek; d > 1e-12 || d < -1e-12 || a != g || gg != g {
			t.Fatalf("defter: g=%v a=%v genel=%v beklenen=%v", g, a, gg, bek)
		}
	})
}

func TestFiyatUSDVeCacheOlcer(t *testing.T) {
	f := Fiyat{GirisMTok: 3, CikisMTok: 15, CacheYazma5dkMTok: 3.75, CacheYazma1saMTok: 6, CacheOkumaMTok: 0.3}
	k := Kullanim{InputTokens: 1_000_000, CacheCreationInputTokens: 1_000_000}
	if v := f.USD(k, "5m"); v != 3+3.75 {
		t.Fatalf("5m: %v", v)
	}
	if v := f.USD(k, "1h"); v != 3+6 {
		t.Fatalf("1h: %v", v)
	}
	c := yeniCacheOlcer(5, 0.5, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	for i := 0; i < 5; i++ {
		c.Kaydet("m", Kullanim{InputTokens: 100, CacheReadInputTokens: 4000})
	}
	if c.uyariSayisi() != 0 {
		t.Fatal("yüksek isabette uyarı")
	}
	for i := 0; i < 5; i++ {
		c.Kaydet("m", Kullanim{InputTokens: 100, CacheCreationInputTokens: 4000})
	}
	if c.uyariSayisi() != 1 || c.Durum()["m"] > 0.01 {
		t.Fatalf("düşük isabet uyarısı: %d %v", c.uyariSayisi(), c.Durum())
	}
	c.Kaydet("m", Kullanim{InputTokens: 100})
	if c.uyariSayisi() != 1 {
		t.Fatal("uyarı 10 dk içinde tekrarlandı")
	}
}

// Eşleştirme: kod_ozet → /start → İMZALI eslesme_onayla → baglanti_durum(etkin) → KVKK → kabul.
func TestEslesmeAkisi(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod := "Kod_123abcXYZ"
	h := sha256.Sum256([]byte(kod))
	ozet := hex.EncodeToString(h[:])
	ajan.gonder(&protokol.KodOzet{KodOzet: ozet, SonGecerlilik: time.Now().Add(10 * time.Minute).Unix()})
	bekle(t, func() bool { o.m.ajan.mu.Lock(); defer o.m.ajan.mu.Unlock(); _, ok := o.m.ajan.kodlar[ozet]; return ok })

	o.mesaj(t, tTg, "/start "+kod)
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.eslesmeler) == 1 })
	ajan.mu.Lock()
	e := ajan.eslesmeler[0]
	ih := ajan.imzaHatasi
	ajan.mu.Unlock()
	if ih != 0 || e.TgUserID != tTg || e.KodOzet != ozet || e.TgKullaniciAdi != "kullanici_1" {
		t.Fatalf("eslesme_onayla: %+v imza hatası %d", e, ih)
	}
	if !strings.Contains(o.tg.son(tTg), "panelde onay") {
		t.Fatalf("kullanıcı mesajı: %q", o.tg.son(tTg))
	}
	// kod tek kullanımlık
	o.mesaj(t, tTg, "/start "+kod)
	if !strings.Contains(o.tg.son(tTg), "geçersiz") {
		t.Fatal("kod ikinci kez kabul edildi")
	}
	// sahte: eşleştirmesi olmayan başka kullanıcıya bağlantı yazma girişimi
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_sahte_0001", TgUserID: 666, Durum: protokol.BaglantiEtkin, Rol: protokol.RolKok, Duzey: protokol.DuzeyOkuma})
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_sahte_0002", KodOzet: ozet, TgUserID: 667, Durum: protokol.BaglantiEtkin, Rol: protokol.RolKok, Duzey: protokol.DuzeyOkuma})
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.hatalar) >= 2 })
	if len(o.tg.metinler(666))+len(o.tg.metinler(667)) != 0 {
		t.Fatal("eşleştirmesiz bağlantı kullanıcıya mesaj attırdı")
	}
	// gerçek bağlantı
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_gercek_0001", KodOzet: ozet, TgUserID: tTg, Durum: protokol.BaglantiEtkin,
		Rol: protokol.RolMusteri, Duzey: protokol.DuzeyOkuma, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "ornek.com.tr"}}})
	bekle(t, func() bool { return strings.Contains(o.tg.son(tTg), "Aydınlatma metni") })
	var veri string
	for _, k := range o.tg.hepsi() {
		if len(k.Dugmeler) > 0 && strings.HasPrefix(k.Dugmeler[0][0].Veri, "kvkk:") {
			veri = k.Dugmeler[0][0].Veri
		}
	}
	o.mesaj(t, tTg, "site çalışmıyor") // kabul öncesi Claude'a gitmez
	if o.c.toplam() != 0 {
		t.Fatal("KVKK kabulü olmadan Claude çağrıldı")
	}
	o.callback(t, tTg, veri)
	b, _ := o.depo.BaglantiAl("bag_gercek_0001")
	if b.KVKKKabul.IsZero() || b.SunucuKimligi != ajan.Sunucu {
		t.Fatalf("bağlantı: %+v", b)
	}
	o.mesaj(t, tTg, "site çalışmıyor")
	if len(o.c.anaIstekler()) != 1 {
		t.Fatal("kabul sonrası Claude çağrılmadı")
	}
	// /unutbeni → ajana iptal + kayıtlar silinir
	o.mesaj(t, tTg, "/unutbeni")
	o.callback(t, tTg, "unut:evet")
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.iptaller) == 1 })
	if len(o.depo.KullaniciBaglantilari(tTg)) != 0 || len(o.depo.konusmaBellek()) != 0 {
		t.Fatal("unutbeni sonrası kayıt kaldı")
	}
}

// Sahte cihaz imzası / yanlış katalog / alt protokolsüz el sıkışma reddedilir.
func TestAjanKayitRedleri(t *testing.T) {
	o := yeniOrtam(t, nil)
	t.Run("imza", func(t *testing.T) {
		ws, _ := wsBaglan(t, o.srv.URL, nil)
		cift, _ := protokol.AnahtarCiftiUret(nil)
		baska, _ := protokol.AnahtarCiftiUret(nil)
		kay := yeniKayit(t, cift)
		ws.yaz(zarfla(t, kay))
		a := &sahteAjan{t: t, ws: ws}
		md := a.okuGovde(3 * time.Second).(*protokol.MeydanOkuma)
		kay2 := *kay
		kay2.CihazAnahtari = protokol.AcikAnahtarKodla(baska.Acik)
		y, _ := protokol.MeydanYanitla(baska.Ozel, md, &kay2) // başka anahtarla imza
		ws.yaz(zarfla(t, y))
		ks := a.okuGovde(3 * time.Second).(*protokol.KayitSonuc)
		if ks.Kabul || ks.HataKodu != protokol.HataImza {
			t.Fatalf("sahte imza kabul: %+v", ks)
		}
	})
	t.Run("katalog", func(t *testing.T) {
		ws, _ := wsBaglan(t, o.srv.URL, nil)
		cift, _ := protokol.AnahtarCiftiUret(nil)
		kay := yeniKayit(t, cift)
		kay.KatalogOzeti = strings.Repeat("0", 64)
		ws.yaz(zarfla(t, kay))
		a := &sahteAjan{t: t, ws: ws}
		ks := a.okuGovde(3 * time.Second).(*protokol.KayitSonuc)
		if ks.Kabul || ks.HataKodu != protokol.HataSurum {
			t.Fatalf("katalog uyuşmazlığı kabul: %+v", ks)
		}
	})
	t.Run("alt_protokol", func(t *testing.T) {
		_, resp := wsBaglan(t, o.srv.URL, map[string]string{"Sec-WebSocket-Protocol": "baska"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("alt protokolsüz: %d", resp.StatusCode)
		}
	})
	t.Run("maskesiz_cerceve", func(t *testing.T) {
		ws, _ := wsBaglan(t, o.srv.URL, nil)
		ws.cerceve(wsOpMetin, []byte(`{}`), true, false)
		op, _, err := ws.oku(2 * time.Second)
		if err == nil && op != wsOpKapat {
			t.Fatalf("maskesiz çerçeve kabul: op=%d", op)
		}
	})
	if o.m.ajan.cevrimiciSayi() != 0 {
		t.Fatal("reddedilen ajan çevrimiçi sayıldı")
	}
}

// WS: parçalı mesaj birleştirilir; sınır aşımı kapatılır.
func TestWSParcaliVeSinir(t *testing.T) {
	o := yeniOrtam(t, nil)
	ws, _ := wsBaglan(t, o.srv.URL, nil)
	cift, _ := protokol.AnahtarCiftiUret(nil)
	b := zarfla(t, yeniKayit(t, cift))
	ws.cerceve(wsOpMetin, b[:10], false, true)
	ws.cerceve(wsOpPing, []byte("p"), true, true)
	ws.cerceve(wsOpDevam, b[10:], true, true)
	op, v, err := ws.oku(2 * time.Second)
	if err != nil || op != wsOpPong {
		t.Fatalf("pong bekleniyordu: %v %d", err, op)
	}
	op, v, err = ws.oku(2 * time.Second)
	if err != nil || op != wsOpMetin || !bytes.Contains(v, []byte(`"meydan_okuma"`)) {
		t.Fatalf("parçalı kayıt işlenmedi: %v %s", err, v)
	}
	ws2, _ := wsBaglan(t, o.srv.URL, nil)
	ws2.cerceve(wsOpMetin, make([]byte, protokol.CerceveSiniri+1), true, true)
	op, _, err = ws2.oku(2 * time.Second)
	if err == nil && op != wsOpKapat {
		t.Fatal("sınır aşan mesaj kabul edildi")
	}
}

// Uzun yoklama: kayıt → yanıt (belirteç) → al (imzalı iş emri) → gonder (is_sonucu).
func TestUzunYoklamaUctanUca(t *testing.T) {
	o := yeniOrtam(t, nil)
	cift, _ := protokol.AnahtarCiftiUret(nil)
	kay := yeniKayit(t, cift)
	dog, _ := protokol.YeniDogrulayici(nil, protokol.YeniBellekOnbellek(0), o.sir.MerkezAnahtar.Acik)
	post := func(yol, belirtec string, govde []byte) (int, []byte) {
		req, _ := http.NewRequest(http.MethodPost, o.srv.URL+yol, bytes.NewReader(govde))
		if belirtec != "" {
			req.Header.Set("Authorization", "Bearer "+belirtec)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	k, b := post(protokol.YoklamaKayitYolu, "", zarfla(t, kay))
	_, g, err := protokol.Coz(b, protokol.YonAjana)
	md, ok := g.(*protokol.MeydanOkuma)
	if k != 200 || err != nil || !ok || dog.MeydanDogrula(md, kay) != nil {
		t.Fatalf("kayit: %d %v", k, err)
	}
	y, _ := protokol.MeydanYanitla(cift.Ozel, md, kay)
	k, b = post(protokol.YoklamaYanitYolu, "", zarfla(t, y))
	_, g, _ = protokol.Coz(b, protokol.YonAjana)
	ks := g.(*protokol.KayitSonuc)
	if k != 200 || !ks.Kabul || ks.OturumBelirteci == "" {
		t.Fatalf("yanit: %d %+v", k, ks)
	}
	// meydan tek kullanımlık
	if k, _ := post(protokol.YoklamaYanitYolu, "", zarfla(t, y)); k != http.StatusForbidden {
		t.Fatalf("meydan yeniden kullanıldı: %d", k)
	}
	if k, _ := post(protokol.YoklamaAlYolu, "yanlis", nil); k != http.StatusUnauthorized {
		t.Fatal("yanlış belirteç kabul")
	}
	bag := protokol.OturumBaglami{Rol: protokol.RolMusteri, Duzey: protokol.DuzeyOkuma, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "ornek.com.tr"}}}
	sonuc := make(chan *protokol.IsSonucu, 1)
	go func() {
		s, err := o.m.ajan.IsCalistir(context.Background(), ks.SunucuKimligi, "bag_yoklama_01", "oturum_yoklama_01", "ssl_durum", json.RawMessage(`{"hesap":1}`), protokol.R0)
		if err != nil {
			t.Error(err)
		}
		sonuc <- s
	}()
	k, b = post(protokol.YoklamaAlYolu, ks.OturumBelirteci, nil)
	var dizi []json.RawMessage
	if k != 200 || json.Unmarshal(b, &dizi) != nil || len(dizi) == 0 {
		t.Fatalf("al: %d %s", k, b)
	}
	var emir *protokol.IsEmri
	for _, z := range dizi {
		if _, g, err := protokol.Coz(z, protokol.YonAjana); err == nil {
			if e, ok := g.(*protokol.IsEmri); ok {
				emir = e
			}
		}
	}
	if emir == nil || dog.Dogrula(emir) != nil {
		t.Fatal("imzalı iş emri alınamadı/doğrulanamadı")
	}
	if dog.Dogrula(emir) == nil {
		t.Fatal("iş emri tekrar oynatılabildi")
	}
	if _, err := protokol.AracGirdisiCoz(bag, emir.Arac, emir.Param); err != nil {
		t.Fatal(err)
	}
	if k, _ := post(protokol.YoklamaGonderYolu, ks.OturumBelirteci, zarfla(t, &protokol.IsSonucu{IsKodu: emir.IsKodu, Ok: true, Ozet: "sertifika geçerli"})); k != http.StatusNoContent {
		t.Fatalf("gonder: %d", k)
	}
	select {
	case s := <-sonuc:
		if s == nil || s.Ozet != "sertifika geçerli" {
			t.Fatalf("sonuç: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("is_sonucu iletilmedi")
	}
}

func TestIsZamanAsimiVeCevrimdisi(t *testing.T) {
	o := yeniOrtam(t, func(y *Yapilandirma) { y.IsZamanAsimiSn = 5 })
	if _, err := o.m.ajan.IsCalistir(context.Background(), "s_yok", "bag_00000001", "oturum_000001", "ssl_durum", json.RawMessage(`{"hesap":1}`), protokol.R0); protokol.HataKoduAl(err) != protokol.HataBaglantiYok {
		t.Fatalf("çevrimdışı: %v", err)
	}
	ajan := sahteAjanBagla(t, o, func(e *protokol.IsEmri) *protokol.IsSonucu {
		time.Sleep(6 * time.Second)
		return &protokol.IsSonucu{Ok: true}
	})
	ajan.baglam = protokol.OturumBaglami{Rol: protokol.RolMusteri, Duzey: protokol.DuzeyOkuma, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "a.com"}}}
	_, err := o.m.ajan.IsCalistir(context.Background(), ajan.Sunucu, "bag_00000001", "oturum_000001", "ssl_durum", json.RawMessage(`{"hesap":1}`), protokol.R0)
	if protokol.HataKoduAl(err) != protokol.HataZamanAsimi {
		t.Fatalf("zaman aşımı: %v", err)
	}
}

// SDK istemcisi: gövde OLDUĞU GİBİ gider; anahtar ortamdan değil verilen değerden.
func TestSDKIstemciGovdeAynen(t *testing.T) {
	var gelen []byte
	var anahtar, yol string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gelen, _ = io.ReadAll(r.Body)
		anahtar, yol = r.Header.Get("X-Api-Key"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"text","text":"merhaba"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":4000}}`)
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "ortamdan-gelmemeli")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	c := YeniSDKIstemci("sk-ant-test-verilen-anahtar-0001", srv.URL)
	govde := anaIstekKur(istekSecenek{Model: tModelAna, MaxTokens: 1024, CacheTTL: "5m"}, []apiMesaj{{Rol: "user", Icerik: []json.RawMessage{metinBlok("selam")}}})
	y, err := c.Gonder(context.Background(), ClaudeIstek{Model: tModelAna, MaxTokens: 1024, Govde: govde})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gelen, govde) {
		t.Fatal("SDK gövdeyi değiştirdi")
	}
	if anahtar != "sk-ant-test-verilen-anahtar-0001" || yol != "/v1/messages" {
		t.Fatalf("anahtar/yol: %q %q", anahtar, yol)
	}
	if y.StopReason != "end_turn" || y.Usage.CacheReadInputTokens != 4000 || metinTopla(y.Content) != "merhaba" {
		t.Fatalf("yanıt: %+v", y)
	}
}

func TestSuzgec(t *testing.T) {
	c := yeniCikisSuzgeci([]string{"GIZLI-MERKEZ-SIRRI-123"})
	for _, tc := range []struct {
		girdi string
		red   bool
	}{
		{"Lütfen parolanızı bana yazın.", true},
		{"Please send me your password.", true},
		{"Kart numaranızı iletin.", true},
		{"SMS kodunu paylaşın lütfen", true},
		{"Parolanızı kimseyle paylaşmayın; panelden değiştirin.", false},
		{"Şifrenizi panelde Hesap > Güvenlik bölümünden sıfırlayabilirsiniz.", false},
		{"ping atıp sonucu yazın", false},
	} {
		_, red, _ := c.Suz(tc.girdi, nil)
		if red != tc.red {
			t.Errorf("%q: red=%t", tc.girdi, red)
		}
	}
	out, _, _ := c.Suz("sır: GIZLI-MERKEZ-SIRRI-123 dosya wp-config.php, index.html, php-fpm.service; bak: www.kotu.com ve sub.ornek.com.tr ile tg://resolve?domain=x", []string{"ornek.com.tr"})
	if strings.Contains(out, "GIZLI-MERKEZ") || !strings.Contains(out, "wp-config.php") || !strings.Contains(out, "index.html") ||
		!strings.Contains(out, "php-fpm.service") || !strings.Contains(out, "www[.]kotu[.]com") || !strings.Contains(out, "sub.ornek.com.tr") || strings.Contains(out, "tg://") {
		t.Fatalf("süzgeç: %q", out)
	}
}

func TestYapilandirmaVeSirlar(t *testing.T) {
	d := t.TempDir()
	yaz := func(ad, icerik string, izin os.FileMode) {
		if err := os.WriteFile(filepath.Join(d, ad), []byte(icerik), izin); err != nil {
			t.Fatal(err)
		}
	}
	yaz(SirTelegram, tTelegramToken+"\n", 0o400)
	yaz(SirAnthropic, tAnthropic, 0o400)
	yaz(SirWebhook, tWebhookGizli, 0o400)
	yaz(SirMerkezAnahtar, strings.Repeat("ab", 32), 0o400)
	s, err := SirlariYukle(d)
	if err != nil {
		t.Fatal(err)
	}
	if s.TelegramToken != tTelegramToken || s.MerkezAnahtar == nil {
		t.Fatal("sırlar okunamadı")
	}
	if strings.Contains(s.String()+strings.Join([]string{}, ""), tAnthropic) {
		t.Fatal("String() sır yazıyor")
	}
	os.Chmod(filepath.Join(d, SirAnthropic), 0o644)
	if _, err := SirlariYukle(d); err == nil || strings.Contains(err.Error(), tAnthropic) {
		t.Fatalf("herkese açık sır dosyası kabul edildi / hata sır içeriyor: %v", err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if _, err := KimlikDizini(""); err == nil {
		t.Fatal("kimlik dizini yokken hata yok")
	}
	yd := filepath.Join(d, "y.json")
	os.WriteFile(yd, []byte(`{"modeller":{"triyaj":"a","ana":"b"},"fiyatlar":{"a":{"giris_mtok":1,"cikis_mtok":1,"cache_yazma_5dk_mtok":1,"cache_yazma_1sa_mtok":1,"cache_okuma_mtok":0.1}},"butce":{"oturum_token_tavani":1,"sunucu_gunluk_usd":1,"sunucu_aylik_usd":1,"genel_gunluk_usd":1,"genel_aylik_usd":1}}`), 0o600)
	if _, err := YapilandirmaYukle(yd); err == nil || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("fiyatsız model kabul edildi: %v", err)
	}
	os.WriteFile(yd, []byte(`{"bilinmeyen":1}`), 0o600)
	if _, err := YapilandirmaYukle(yd); err == nil {
		t.Fatal("bilinmeyen alan kabul edildi")
	}
}

func TestDosyaDepoKalicilikVeBudama(t *testing.T) {
	d := t.TempDir()
	dd, err := DosyaDepoAc(d)
	if err != nil {
		t.Fatal(err)
	}
	simdi := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	dd.BaglantiKaydet(Baglanti{BaglantiID: "bag_00000001", TgUserID: 1, SunucuKimligi: "s_1"})
	dd.MaliyetEkle("s_1", simdi, 0.25)
	dd.KonusmaEkle(KonusmaKaydi{Zaman: simdi.AddDate(0, 0, -31), Tg: tgKisa(1), Metin: "eski"})
	dd.KonusmaEkle(KonusmaKaydi{Zaman: simdi, Tg: tgKisa(1), Metin: "yeni"})
	dd.KonusmaEkle(KonusmaKaydi{Zaman: simdi, Tg: tgKisa(2), Metin: "baska"})
	if err := dd.KonusmaBuda(simdi, 30); err != nil {
		t.Fatal(err)
	}
	dd2, err := DosyaDepoAc(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dd2.BaglantiAl("bag_00000001"); !ok {
		t.Fatal("bağlantı kalıcı değil")
	}
	if g, a := dd2.Maliyet("s_1", simdi); g != 0.25 || a != 0.25 {
		t.Fatal("maliyet kalıcı değil")
	}
	ff, _ := os.ReadDir(filepath.Join(d, "konusma"))
	if len(ff) != 1 {
		t.Fatalf("budama: %d dosya", len(ff))
	}
	if err := dd2.KullaniciUnut(1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "konusma", ff[0].Name()))
	if strings.Contains(string(b), "yeni") || !strings.Contains(string(b), "baska") {
		t.Fatalf("unut: %s", b)
	}
	fi, _ := os.Stat(filepath.Join(d, "durum.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("durum.json izni %o", fi.Mode().Perm())
	}
}
