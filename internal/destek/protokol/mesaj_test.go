package protokol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func h64(c string) string { return strings.Repeat(c, 64) }

func ornekGovdeler(t *testing.T) []Govde {
	c := testCifti(t, 1)
	s := testImzalayici(t, c, t0)
	kay := ornekKayit(t, testCifti(t, 7))
	m, err := s.MeydanOlustur(kay)
	if err != nil {
		t.Fatal(err)
	}
	y, err := MeydanYanitla(testCifti(t, 7).Ozel, m, kay)
	if err != nil {
		t.Fatal(err)
	}
	es := &EslesmeOnayla{KodOzet: h64("a"), TgUserID: 42, TgKullaniciAdi: ""}
	if err := s.Imzala(es, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	return []Govde{
		kay, m, y,
		&KayitSonuc{Kabul: true, SunucuKimligi: "sunucu_0001", KalpAraligiSn: 20},
		&KayitSonuc{Kabul: false, HataKodu: HataLisans},
		&KalpAtisi{Sira: 7, Zaman: t0.Unix()},
		&KodOzet{KodOzet: h64("b"), SonGecerlilik: t0.Unix() + 600},
		es,
		&BaglantiDurum{BaglantiID: "baglanti_0001", KodOzet: h64("b"), TgUserID: 42, Durum: BaglantiEtkin, Rol: RolMusteri, Duzey: DuzeyOkuma,
			Hesaplar: []HesapEtiketi{{1, "ornek.com"}, {2, "xn--rnek-zoa.com.tr"}}},
		&BaglantiIptal{BaglantiID: "baglanti_0001", Neden: IptalUnutBeni},
		imzaliIsEmri(t, c),
		&IsSonucu{IsKodu: "AB23CD45", Ok: true, Ozet: "nginx hata günlüğü", Satirlar: []string{"satır 1", "\tgirintili"}, Kesildi: true, Kanit: "denetim:123"},
		&IsSonucu{IsKodu: "AB23CD45", Ok: false, HataKodu: HataKapsam, Satirlar: []string{}},
		&HataMesaji{Kod: HataBicim, Ayrinti: "x", Ilgili: "zarf_0001"},
	}
}

func alanYonu(g Govde) Yon {
	if g.Tip().Yonu()&YonAjana != 0 {
		return YonAjana
	}
	return YonMerkeze
}

func TestGidisDonusTumTipler(t *testing.T) {
	goruldu := map[Tip]bool{}
	for _, g := range ornekGovdeler(t) {
		goruldu[g.Tip()] = true
		b, err := Kodla("zarf_0001", g)
		if err != nil {
			t.Fatalf("%s: %v", g.Tip(), err)
		}
		_, c, err := Coz(b, alanYonu(g))
		if err != nil {
			t.Fatalf("%s: %v", g.Tip(), err)
		}
		if !reflect.DeepEqual(c, g) {
			t.Fatalf("%s gidiş-dönüş farklı:\n%#v\n%#v", g.Tip(), c, g)
		}
	}
	for tp := range tipYonleri {
		if !goruldu[tp] {
			t.Errorf("%s için gidiş-dönüş örneği yok", tp)
		}
	}
}

func zarfMetni(tip, govde string) []byte {
	return []byte(`{"surum":1,"tip":"` + tip + `","kimlik":"zarf_0001","govde":` + govde + `}`)
}

func TestCozKatilik(t *testing.T) {
	kalp := `{"sira":1,"zaman":1790000000}`
	durumlar := []struct {
		ad   string
		veri []byte
		yon  Yon
		hata HataKodu
	}{
		{"gecerli", zarfMetni("kalp_atisi", kalp), YonAjana, ""},
		{"govdede bilinmeyen alan", zarfMetni("kalp_atisi", `{"sira":1,"zaman":1790000000,"ek":1}`), YonAjana, HataBilinmeyenAlan},
		{"govdede buyuk harf alan", zarfMetni("kalp_atisi", `{"Sira":1,"zaman":1790000000}`), YonAjana, HataBilinmeyenAlan},
		{"zarfta bilinmeyen alan", []byte(`{"surum":1,"tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `,"x":true}`), YonAjana, HataBilinmeyenAlan},
		{"zarfta buyuk harf", []byte(`{"surum":1,"Tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `}`), YonAjana, HataBilinmeyenAlan},
		{"yinelenen anahtar govde", zarfMetni("kalp_atisi", `{"sira":1,"zaman":1790000000,"zaman":5}`), YonAjana, HataBicim},
		{"yinelenen anahtar zarf", []byte(`{"surum":1,"tip":"hata","tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `}`), YonAjana, HataBicim},
		{"ek veri", append(zarfMetni("kalp_atisi", kalp), []byte(` {}`)...), YonAjana, HataBicim},
		{"surum 2", []byte(`{"surum":2,"tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `,"yeni":1}`), YonAjana, HataSurum},
		{"surum dizge", []byte(`{"surum":"1","tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `}`), YonAjana, HataSurum},
		{"surum yok", []byte(`{"tip":"kalp_atisi","kimlik":"zarf_0001","govde":` + kalp + `}`), YonAjana, HataSurum},
		{"bilinmeyen tip", zarfMetni("kabuk", kalp), YonAjana, HataTipBilinmiyor},
		{"ters yon is_emri", zarfMetni("is_emri", `{}`), YonMerkeze, HataYon},
		{"ters yon is_sonucu", zarfMetni("is_sonucu", `{}`), YonAjana, HataYon},
		{"govde null", zarfMetni("kalp_atisi", `null`), YonAjana, HataBicim},
		{"kimlik kisa", []byte(`{"surum":1,"tip":"kalp_atisi","kimlik":"x","govde":` + kalp + `}`), YonAjana, HataBicim},
		{"cerceve buyuk", append(zarfMetni("kalp_atisi", kalp), bytes.Repeat([]byte(" "), CerceveSiniri)...), YonAjana, HataCerceveBuyuk},
		{"govde dogrulama", zarfMetni("kalp_atisi", `{"sira":1,"zaman":0}`), YonAjana, HataBicim},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			_, _, err := Coz(d.veri, d.yon)
			kodBekle(t, err, d.hata)
		})
	}
}

func TestIsSonucuSiniri(t *testing.T) {
	gecerli := func() *IsSonucu {
		return &IsSonucu{IsKodu: "AB23CD45", Ok: true, Satirlar: []string{"a"}}
	}
	durumlar := []struct {
		ad   string
		f    func(s *IsSonucu)
		hata HataKodu
	}{
		{"gecerli", func(s *IsSonucu) {}, ""},
		{"ok ve hata kodu", func(s *IsSonucu) { s.HataKodu = HataIc }, HataBicim},
		{"hata kodsuz", func(s *IsSonucu) { s.Ok = false }, HataBicim},
		{"tanimsiz hata kodu", func(s *IsSonucu) { s.Ok = false; s.HataKodu = "uydurma" }, HataBicim},
		{"300 satir", func(s *IsSonucu) { s.Satirlar = make([]string, SatirSayisiSiniri) }, ""},
		{"301 satir", func(s *IsSonucu) { s.Satirlar = make([]string, SatirSayisiSiniri+1) }, HataBicim},
		{"satir sinirda", func(s *IsSonucu) { s.Satirlar = []string{strings.Repeat("x", SatirSiniri)} }, ""},
		{"satir uzun", func(s *IsSonucu) { s.Satirlar = []string{strings.Repeat("x", SatirSiniri+1)} }, HataBicim},
		{"kontrol karakteri", func(s *IsSonucu) { s.Satirlar = []string{"a\x1b[31m"} }, HataBicim},
		{"yon degistirme", func(s *IsSonucu) { s.Ozet = "abc‮" }, HataBicim},
		{"ozet uzun", func(s *IsSonucu) { s.Ozet = strings.Repeat("x", OzetSiniri+1) }, HataBicim},
		{"kanit bicimi", func(s *IsSonucu) { s.Kanit = "serbest metin" }, HataBicim},
		{"is kodu kucuk harf", func(s *IsSonucu) { s.IsKodu = "ab23cd45" }, HataBicim},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			s := gecerli()
			d.f(s)
			kodBekle(t, s.Dogrula(), d.hata)
		})
	}
}

func TestBaglantiDurumSira(t *testing.T) {
	b := &BaglantiDurum{BaglantiID: "baglanti_0001", TgUserID: 1, Durum: BaglantiEtkin, Rol: RolBayi, Duzey: DuzeyOkuma,
		Hesaplar: []HesapEtiketi{{1, "a.com"}, {3, "b.com"}}}
	kodBekle(t, b.Dogrula(), HataBicim)
	b.Hesaplar[1].Sira = 2
	b.Hesaplar[1].Etiket = "B.com<x>"
	kodBekle(t, b.Dogrula(), HataBicim)
	b.Hesaplar[1].Etiket = "b.com"
	if err := b.Dogrula(); err != nil {
		t.Fatal(err)
	}
}

func TestIsEmriParamBicimi(t *testing.T) {
	for _, p := range []string{``, `[]`, `"x"`, `{`, `null`} {
		e := imzaliIsEmri(t, testCifti(t, 1))
		e.Param = json.RawMessage(p)
		kodBekle(t, e.Dogrula(), HataBicim)
	}
}

func TestSatirTemizle(t *testing.T) {
	s, k := SatirTemizle("a\x00b‮c")
	if s != "a?b?c" || k {
		t.Fatalf("%q %v", s, k)
	}
	s, k = SatirTemizle(strings.Repeat("ş", SatirSiniri))
	if !k || len(s) > SatirSiniri || !strings.HasSuffix(s, "ş") {
		t.Fatalf("kesme: %d %v", len(s), k)
	}
	if err := metinDenetle("x", s, SatirSiniri); err != nil {
		t.Fatal(err)
	}
}

func TestHataKodlariBenzersiz(t *testing.T) {
	g := map[HataKodu]bool{}
	for _, k := range HataKodlari {
		if g[k] || !k.Gecerli() {
			t.Fatalf("yinelenen/geçersiz %q", k)
		}
		g[k] = true
	}
	if HataKodu("yok").Gecerli() {
		t.Fatal("tanımsız kod geçerli sayıldı")
	}
}

// katiCoz doğrudan: Coz'un ön denetimlerinden bağımsız katmanlar.
func TestKatiCozDogrudan(t *testing.T) {
	var k KalpAtisi
	kodBekle(t, katiCoz([]byte(`{"sira":1,"zaman":2} {"sira":3}`), &k), HataBicim)
	kodBekle(t, katiCoz([]byte(`{"sira":1,"zaman":2}x`), &k), HataBicim)
	kodBekle(t, katiCoz([]byte(`{"sira":1,"zaman":2,"ZAMAN":3}`), &k), HataBilinmeyenAlan)
	kodBekle(t, katiCoz(bytes.Repeat([]byte(" "), CerceveSiniri+1), &k), HataCerceveBuyuk)
	kodBekle(t, katiCoz([]byte("{\"sira\":1,\"zaman\":2,\"x\":\"\xff\"}"), &k), HataBicim)
	if err := katiCoz([]byte(`{"sira":1,"zaman":2}`), &k); err != nil || k.Zaman != 2 {
		t.Fatal(err)
	}
}
