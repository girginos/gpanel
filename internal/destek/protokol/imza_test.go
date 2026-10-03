package protokol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var t0 = time.Unix(1_790_000_000, 0)

func sabitSaat(t time.Time) func() time.Time { return func() time.Time { return t } }

func testCifti(t *testing.T, bayt byte) *AnahtarCifti {
	t.Helper()
	c, err := AnahtarCiftiTohumdan(bytes.Repeat([]byte{bayt}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testImzalayici(t *testing.T, c *AnahtarCifti, an time.Time) *Imzalayici {
	s := YeniImzalayici(c)
	s.Saat = sabitSaat(an)
	return s
}

func testDogrulayici(t *testing.T, an time.Time, anahtarlar ...ed25519.PublicKey) *Dogrulayici {
	t.Helper()
	d, err := YeniDogrulayici(sabitSaat(an), YeniBellekOnbellek(0), anahtarlar...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func ornekIsEmri() *IsEmri {
	return &IsEmri{
		IsKodu:     "AB23CD45",
		Oturum:     "oturum_0001",
		BaglantiID: "baglanti_0001",
		Arac:       "log_oku",
		Param:      json.RawMessage(`{ "hesap": 1, "tur": "nginx_error" }`),
		Risk:       R0,
	}
}

func kodBekle(t *testing.T, err error, k HataKodu) {
	t.Helper()
	if got := HataKoduAl(err); got != k {
		t.Fatalf("hata kodu = %q (%v), beklenen %q", got, err, k)
	}
}

func imzaliIsEmri(t *testing.T, c *AnahtarCifti) *IsEmri {
	t.Helper()
	e := ornekIsEmri()
	if err := testImzalayici(t, c, t0).Imzala(e, EnUzunGecerlilik); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestIsEmriGecerliImza(t *testing.T) {
	c := testCifti(t, 1)
	e := imzaliIsEmri(t, c)
	if string(e.Param) != `{"hesap":1,"tur":"nginx_error"}` {
		t.Fatalf("param sıkıştırılmadı: %s", e.Param)
	}
	if e.SonGecerlilik-e.Verilme != 60 {
		t.Fatalf("geçerlilik %d", e.SonGecerlilik-e.Verilme)
	}
	if err := testDogrulayici(t, t0, c.Acik).Dogrula(e); err != nil {
		t.Fatal(err)
	}
}

// Kablodan geçip katı çözülen iş emri aynı imzayla doğrulanmalı.
func TestIsEmriKabloGidisDonus(t *testing.T) {
	c := testCifti(t, 1)
	e := imzaliIsEmri(t, c)
	b, err := Kodla("zarf_0001", e)
	if err != nil {
		t.Fatal(err)
	}
	_, g, err := Coz(b, YonAjana)
	if err != nil {
		t.Fatal(err)
	}
	if err := testDogrulayici(t, t0, c.Acik).Dogrula(g.(*IsEmri)); err != nil {
		t.Fatal(err)
	}
}

func TestIsEmriBozukImza(t *testing.T) {
	c := testCifti(t, 1)
	e := imzaliIsEmri(t, c)
	ham, _ := b64.DecodeString(e.Imza)
	ham[10] ^= 0x01
	e.Imza = b64.EncodeToString(ham)
	kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e), HataImza)
}

func TestIsEmriBaskaAnahtar(t *testing.T) {
	c := testCifti(t, 1)
	sahte := testCifti(t, 2)
	// Saldırgan kendi anahtarıyla imzalar ama gerçek anahtar kimliğini yazar.
	e := ornekIsEmri()
	s := testImzalayici(t, sahte, t0)
	s.cift = &AnahtarCifti{Acik: sahte.Acik, Ozel: sahte.Ozel, Kimlik: c.Kimlik}
	if err := s.Imzala(e, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e), HataImza)
	// Bilinmeyen anahtar kimliği.
	e2 := imzaliIsEmri(t, sahte)
	kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e2), HataAnahtarBilinmiyor)
}

func TestIsEmriSure(t *testing.T) {
	c := testCifti(t, 1)
	sapma := int64(SaatSapmasi / time.Second)
	durumlar := []struct {
		ad   string
		saat time.Time
		hata HataKodu
	}{
		{"tam sinirda", t0.Add(time.Duration(60+sapma) * time.Second), ""},
		{"suresi doldu", t0.Add(time.Duration(60+sapma+1) * time.Second), HataSuresiDoldu},
		{"gelecekten sinirda", t0.Add(-time.Duration(sapma) * time.Second), ""},
		{"gelecekten", t0.Add(-time.Duration(sapma+1) * time.Second), HataZaman},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			e := imzaliIsEmri(t, c)
			v, err := YeniDogrulayici(sabitSaat(t0.Add(-time.Hour)), YeniBellekOnbellek(0), c.Acik)
			if err != nil {
				t.Fatal(err)
			}
			v.Saat = sabitSaat(d.saat)
			kodBekle(t, v.Dogrula(e), d.hata)
		})
	}
}

func TestIsEmriGecerlilikUzun(t *testing.T) {
	c := testCifti(t, 1)
	s := testImzalayici(t, c, t0)
	if err := s.Imzala(ornekIsEmri(), 61*time.Second); HataKoduAl(err) != HataGecerlilikUzun {
		t.Fatalf("imzalayıcı 61 sn kabul etti: %v", err)
	}
	e := ornekIsEmri()
	if err := s.imzalaZaman(e, "AAAAAAAAAAAAAAAAAAAAAA", t0.Unix(), t0.Unix()+61); err != nil {
		t.Fatal(err)
	}
	kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e), HataGecerlilikUzun)
	e = ornekIsEmri()
	if err := s.imzalaZaman(e, "AAAAAAAAAAAAAAAAAAAAAA", t0.Unix(), t0.Unix()+60); err != nil {
		t.Fatal(err)
	}
	if err := testDogrulayici(t, t0, c.Acik).Dogrula(e); err != nil {
		t.Fatalf("60 sn reddedildi: %v", err)
	}
	e = ornekIsEmri()
	if err := s.imzalaZaman(e, "AAAAAAAAAAAAAAAAAAAAAA", t0.Unix(), t0.Unix()); err != nil {
		t.Fatal(err)
	}
	kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e), HataZaman)
}

func TestIsEmriYenidenBaslatmaOncesi(t *testing.T) {
	c := testCifti(t, 1)
	e := imzaliIsEmri(t, c)
	// Ajan t0+10'da (yeniden) başladı: önbellek boş, t0'da verilmiş emir hâlâ süresi içinde.
	kodBekle(t, testDogrulayici(t, t0.Add(10*time.Second), c.Acik).Dogrula(e), HataSuresiDoldu)
}

func TestIsEmriTekrar(t *testing.T) {
	c := testCifti(t, 1)
	v := testDogrulayici(t, t0, c.Acik)
	e := imzaliIsEmri(t, c)
	if err := v.Dogrula(e); err != nil {
		t.Fatal(err)
	}
	kopya := *e
	kodBekle(t, v.Dogrula(&kopya), HataTekrar)
	// Aynı içerik, yeni nonce → kabul.
	if err := v.Dogrula(imzaliIsEmri(t, c)); err != nil {
		t.Fatal(err)
	}
}

// İmzalı her alanın değişmesi imzayı bozmalı.
func TestIsEmriAlanDegisikligi(t *testing.T) {
	c := testCifti(t, 1)
	degisimler := []struct {
		ad   string
		f    func(e *IsEmri)
		hata HataKodu
	}{
		{"is_kodu", func(e *IsEmri) { e.IsKodu = "ZZ23CD45" }, HataImza},
		{"oturum", func(e *IsEmri) { e.Oturum = "oturum_0002" }, HataImza},
		{"baglanti_id", func(e *IsEmri) { e.BaglantiID = "baglanti_0002" }, HataImza},
		{"arac", func(e *IsEmri) { e.Arac = "hesap_ozet" }, HataImza},
		{"risk", func(e *IsEmri) { e.Risk = R2 }, HataImza},
		{"param", func(e *IsEmri) { e.Param = json.RawMessage(`{"hesap":2,"tur":"nginx_error"}`) }, HataImza},
		{"param anahtar sirasi", func(e *IsEmri) { e.Param = json.RawMessage(`{"tur":"nginx_error","hesap":1}`) }, HataImza},
		{"nonce", func(e *IsEmri) { e.Nonce = "BBBBBBBBBBBBBBBBBBBBBA" }, HataImza},
		{"verilme", func(e *IsEmri) { e.Verilme-- }, HataImza},
		{"son_gecerlilik", func(e *IsEmri) { e.SonGecerlilik-- }, HataImza},
		{"anahtar_kimligi", func(e *IsEmri) { e.AnahtarKimligi = "0000000000000000" }, HataAnahtarBilinmiyor},
		{"param bosluk serbest", func(e *IsEmri) { e.Param = json.RawMessage(`{"hesap": 1,  "tur":"nginx_error"}`) }, ""},
	}
	for _, d := range degisimler {
		t.Run(d.ad, func(t *testing.T) {
			e := imzaliIsEmri(t, c)
			d.f(e)
			kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(e), d.hata)
		})
	}
}

func TestKanonikAlanAyiriciVeUzunlukOneki(t *testing.T) {
	// "ab"+"c" ile "a"+"bc" aynı baytları vermemeli (uzunluk öneki).
	e1, e2 := ornekIsEmri(), ornekIsEmri()
	e1.Oturum, e1.BaglantiID = "oturum_0001a", "baglanti_01"
	e2.Oturum, e2.BaglantiID = "oturum_0001", "abaglanti_01"
	k1, _ := KanonikBaytlar(e1)
	k2, _ := KanonikBaytlar(e2)
	if bytes.Equal(k1, k2) {
		t.Fatal("uzunluk öneki yok: farklı alan bölünmesi aynı bayt")
	}
	// Tip ayırıcı: eşleşme onayının imzası iş emrinde geçmemeli.
	es := &EslesmeOnayla{}
	k3, _ := KanonikBaytlar(es)
	if !bytes.HasPrefix(k3[4:], []byte(alanOneki+"eslesme_onayla")) || !bytes.HasPrefix(k1[4:], []byte(alanOneki+"is_emri")) {
		t.Fatal("alan ayırıcı eksik")
	}
}

func TestEslesmeOnaylaImza(t *testing.T) {
	c := testCifti(t, 1)
	s := testImzalayici(t, c, t0)
	m := &EslesmeOnayla{KodOzet: string(bytes.Repeat([]byte("a"), 64)), TgUserID: 123456789, TgKullaniciAdi: "musteri_1"}
	if err := s.Imzala(m, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	b, err := Kodla("zarf_0002", m)
	if err != nil {
		t.Fatal(err)
	}
	_, g, err := Coz(b, YonAjana)
	if err != nil {
		t.Fatal(err)
	}
	if err := testDogrulayici(t, t0, c.Acik).Dogrula(g.(*EslesmeOnayla)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func(m *EslesmeOnayla){
		func(m *EslesmeOnayla) { m.TgUserID++ },
		func(m *EslesmeOnayla) { m.TgKullaniciAdi = "saldirgan" },
		func(m *EslesmeOnayla) { m.KodOzet = string(bytes.Repeat([]byte("b"), 64)) },
	} {
		k := *g.(*EslesmeOnayla)
		f(&k)
		kodBekle(t, testDogrulayici(t, t0, c.Acik).Dogrula(&k), HataImza)
	}
}

func ornekKayit(t *testing.T, cihaz *AnahtarCifti) *Kayit {
	return &Kayit{
		CihazAnahtari:   AcikAnahtarKodla(cihaz.Acik),
		LisansParmakIzi: string(bytes.Repeat([]byte("c"), 64)),
		LisansIspati:    "aXNwYXQ",
		PanelSurumu:     "0.3.0-20261003",
		KatalogOzeti:    KatalogOzeti(),
	}
}

func TestMeydanAkisi(t *testing.T) {
	merkez := testCifti(t, 1)
	cihaz := testCifti(t, 7)
	kay := ornekKayit(t, cihaz)
	s := testImzalayici(t, merkez, t0)
	m, err := s.MeydanOlustur(kay)
	if err != nil {
		t.Fatal(err)
	}
	v := testDogrulayici(t, t0, merkez.Acik)
	if err := v.MeydanDogrula(m, kay); err != nil {
		t.Fatal(err)
	}
	y, err := MeydanYanitla(cihaz.Ozel, m, kay)
	if err != nil {
		t.Fatal(err)
	}
	if err := MeydanYanitDogrula(m, kay, y, t0); err != nil {
		t.Fatal(err)
	}

	// Ajan: sahte merkez (başka anahtar, gerçek kimlik).
	sahte := testImzalayici(t, testCifti(t, 2), t0)
	sahte.cift.Kimlik = merkez.Kimlik
	ms, _ := sahte.MeydanOlustur(kay)
	kodBekle(t, v.MeydanDogrula(ms, kay), HataImza)
	// Ajan: başka cihaza verilmiş meydan (aktarma).
	baska := ornekKayit(t, testCifti(t, 8))
	kodBekle(t, v.MeydanDogrula(m, baska), HataImza)
	// Ajan: süresi dolmuş meydan.
	v.Saat = sabitSaat(t0.Add(MeydanGecerlilik + SaatSapmasi + time.Second))
	kodBekle(t, v.MeydanDogrula(m, kay), HataSuresiDoldu)

	// Merkez: yanlış cihaz anahtarıyla yanıt.
	if _, err := MeydanYanitla(testCifti(t, 9).Ozel, m, kay); HataKoduAl(err) != HataMeydan {
		t.Fatalf("kayıtla uyuşmayan cihaz anahtarı: %v", err)
	}
	ybaska := &MeydanYanit{Meydan: m.Meydan, Imza: b64.EncodeToString(ed25519.Sign(testCifti(t, 9).Ozel, meydanKanonik(string(TipMeydanYanit), m, kay)))}
	kodBekle(t, MeydanYanitDogrula(m, kay, ybaska, t0), HataImza)
	// Merkez: meydan okuma imzası yanıt olarak geri yansıtılamaz (alan ayırıcı).
	yansit := &MeydanYanit{Meydan: m.Meydan, Imza: m.MerkezImza}
	kodBekle(t, MeydanYanitDogrula(m, kay, yansit, t0), HataImza)
	// Merkez: başka meydan.
	m2, _ := s.MeydanOlustur(kay)
	kodBekle(t, MeydanYanitDogrula(m2, kay, y, t0), HataMeydan)
	// Merkez: süre doldu.
	kodBekle(t, MeydanYanitDogrula(m, kay, y, t0.Add(MeydanGecerlilik+time.Second)), HataSuresiDoldu)
}

func TestBellekOnbellekKapasite(t *testing.T) {
	o := YeniBellekOnbellek(2)
	if err := o.Isaretle("a", t0.Add(time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := o.Isaretle("b", t0.Add(time.Second), t0); err != nil {
		t.Fatal(err)
	}
	kodBekle(t, o.Isaretle("c", t0.Add(time.Minute), t0), HataMesgul)
	kodBekle(t, o.Isaretle("a", t0.Add(time.Minute), t0), HataTekrar)
	// b'nin süresi doldu → budanır, c girer.
	if err := o.Isaretle("c", t0.Add(time.Minute), t0.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if o.Uzunluk() != 2 {
		t.Fatalf("uzunluk %d", o.Uzunluk())
	}
}

func TestAnahtarYardimcilari(t *testing.T) {
	c, err := AnahtarCiftiUret(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := AcikAnahtarCoz(AcikAnahtarKodla(c.Acik))
	if err != nil || !a.Equal(c.Acik) || len(c.Kimlik) != 16 {
		t.Fatal("gidiş-dönüş")
	}
	if _, err := AcikAnahtarCoz(AcikAnahtarKodla(c.Acik) + "A"); err == nil {
		t.Fatal("bozuk açık anahtar kabul edildi")
	}
	if _, err := YeniDogrulayici(nil, YeniBellekOnbellek(0), ed25519.PublicKey{1, 2}); err == nil {
		t.Fatal("kısa anahtar kabul edildi")
	}
	k, err := YeniIsKodu(nil)
	if err != nil || !isKoduDeseni.MatchString(k) {
		t.Fatalf("iş kodu %q %v", k, err)
	}
	if !errors.Is(hataf(HataTekrar, "x"), &Hata{Kod: HataTekrar}) {
		t.Fatal("errors.Is")
	}
}
