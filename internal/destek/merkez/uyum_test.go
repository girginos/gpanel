package merkez

// uyum_test.go — merkez ↔ gerçek ajan uçtan uca koşusunda (internal/destek/e2e)
// bulunan uyumsuzlukların birim testleri.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/protokol"
)

// yoklamaOturumuAc: uzun yoklama el sıkışması; belirteç ve sunucu kimliği döner.
func yoklamaOturumuAc(t *testing.T, o *ortam) (string, string, func(yol, belirtec string, govde []byte) (int, []byte)) {
	t.Helper()
	cift, _ := protokol.AnahtarCiftiUret(nil)
	kay := yeniKayit(t, cift)
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
	_, b := post(protokol.YoklamaKayitYolu, "", zarfla(t, kay))
	_, g, err := protokol.Coz(b, protokol.YonAjana)
	md, ok := g.(*protokol.MeydanOkuma)
	if err != nil || !ok {
		t.Fatalf("kayit: %v %s", err, b)
	}
	y, _ := protokol.MeydanYanitla(cift.Ozel, md, kay)
	_, b = post(protokol.YoklamaYanitYolu, "", zarfla(t, y))
	_, g, _ = protokol.Coz(b, protokol.YonAjana)
	ks, ok := g.(*protokol.KayitSonuc)
	if !ok || !ks.Kabul {
		t.Fatalf("yanit: %s", b)
	}
	return ks.OturumBelirteci, ks.SunucuKimligi, post
}

// /al tek yanıtta en çok protokol.YoklamaAlEnCokZarf zarf döner (ajan fazlasını reddedip
// oturumu düşürür); kalanlar kaybolmadan, SIRAYLA sonraki /al'larda gelir.
func TestYoklamaAlZarfSiniri(t *testing.T) {
	o := yeniOrtam(t, nil)
	belirtec, sunucu, post := yoklamaOturumuAc(t, o)
	const toplam = 150
	for i := 0; i < toplam; i++ {
		if !o.m.ajan.BaglantiIptalGonder(sunucu, fmt.Sprintf("bag_sira_%04d", i), protokol.IptalMerkez) {
			t.Fatalf("%d. iptal kuyruğa girmedi", i)
		}
	}
	var gelen []string
	for tur := 0; len(gelen) < toplam && tur < 10; tur++ {
		k, b := post(protokol.YoklamaAlYolu, belirtec, nil)
		var dizi []json.RawMessage
		if k != http.StatusOK || json.Unmarshal(b, &dizi) != nil {
			t.Fatalf("al: %d %s", k, b)
		}
		if len(dizi) > protokol.YoklamaAlEnCokZarf || len(b) > protokol.YoklamaAlEnCokBayt {
			t.Fatalf("/al sözleşme sınırını aştı: %d zarf, %d bayt", len(dizi), len(b))
		}
		for _, z := range dizi {
			_, g, err := protokol.Coz(z, protokol.YonAjana)
			if err != nil {
				t.Fatal(err)
			}
			gelen = append(gelen, g.(*protokol.BaglantiIptal).BaglantiID)
		}
	}
	if len(gelen) != toplam {
		t.Fatalf("zarf kayboldu: %d/%d", len(gelen), toplam)
	}
	for i, id := range gelen {
		if id != fmt.Sprintf("bag_sira_%04d", i) {
			t.Fatalf("sıra bozuk: %d. zarf %s", i, id)
		}
	}
}

// Ajan her yeniden bağlanışta son iptalleri yeniden bildirir: merkezde olmayan bağlantının
// iptali sessizce kabul edilir (hata yanıtı yoklama kuyruğunu doldurmasın).
func TestBilinmeyenBaglantiIptaliSessiz(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	for i := 0; i < 5; i++ {
		ajan.gonder(&protokol.BaglantiIptal{BaglantiID: fmt.Sprintf("bag_eski_%04d", i), Neden: protokol.IptalKullanici})
	}
	// sıralama işareti: hata üreten bir mesaj
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_isaret_0001", TgUserID: 5, Durum: protokol.BaglantiEtkin, Rol: protokol.RolKok, Duzey: protokol.DuzeyOkuma})
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.hatalar) >= 1 })
	ajan.mu.Lock()
	defer ajan.mu.Unlock()
	if len(ajan.hatalar) != 1 || ajan.hatalar[0].Kod != protokol.HataEslesmeKodu {
		t.Fatalf("bilinmeyen iptale hata yanıtlandı: %+v", ajan.hatalar)
	}
}

// Oturum işlenirken bağlantının hesap listesi değişirse (ajan yeni baglanti_durum
// gönderip iş emrini kapsam_disi ile reddeder) merkez bayat Baglam'la yeni iş emri
// GÖNDERMEZ ve modeli yeniden çağırmaz; kullanıcıdan mesajı yeniden göndermesi istenir.
func TestOturumKapsamDegisinceDurur(t *testing.T) {
	o := yeniOrtam(t, nil)
	var ajan *sahteAjan
	b := Baglanti{}
	ajan = sahteAjanBagla(t, o, func(e *protokol.IsEmri) *protokol.IsSonucu {
		ajan.gonder(&protokol.BaglantiDurum{BaglantiID: b.BaglantiID, TgUserID: tTg, Durum: protokol.BaglantiEtkin,
			Rol: protokol.RolBayi, Duzey: protokol.DuzeyOkuma, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "a.com"}, {Sira: 2, Etiket: "yeni.com"}, {Sira: 3, Etiket: "b.com"}}})
		return &protokol.IsSonucu{HataKodu: protokol.HataKapsam, Ozet: "hesap listesi değişti; güncel liste gönderildi, yeniden deneyin"}
	})
	ajan.baglam = protokol.OturumBaglami{Rol: protokol.RolBayi, Duzey: protokol.DuzeyOkuma, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "a.com"}, {Sira: 2, Etiket: "b.com"}}}
	b = o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolBayi, "a.com", "b.com")
	o.c.ana = func(n int, _ ClaudeIstek) (*ClaudeYanit, error) {
		if n == 0 { // aynı turda iki araç: ilki kapsamı değiştirir, ikincisi bayat kapsamla GİTMEMELİ
			return yanit("tool_use", bArac("tu_0a", "site_yokla", `{"hesap":2}`), bArac("tu_0b", "site_yokla", `{"hesap":1}`)), nil
		}
		// model "yeniden deneyin"e uyar: aynı numara (artık başka hesap)
		return yanit("tool_use", bArac(fmt.Sprintf("tu_%d", n), "site_yokla", `{"hesap":2}`)), nil
	}
	o.mesaj(t, tTg, "b.com açılmıyor")
	if n := len(ajan.emirAdlari()); n != 1 {
		t.Fatalf("bayat kapsamla %d iş emri gönderildi", n)
	}
	if n := len(o.c.anaIstekler()); n != 1 {
		t.Fatalf("model bayat oturumla yeniden çağrıldı: %d", n)
	}
	if !strings.Contains(o.tg.son(tTg), "yeniden gönderin") {
		t.Fatalf("kullanıcı mesajı: %q", o.tg.son(tTg))
	}
	if g, _ := o.depo.BaglantiAl(b.BaglantiID); len(g.Hesaplar) != 3 || g.Hesaplar[1].Etiket != "yeni.com" {
		t.Fatalf("güncel liste kaydedilmedi: %+v", g.Hesaplar)
	}
	// yeni mesaj yeni oturum: güncel listeyle (2 = yeni.com) gider
	o.c.ana = func(n int, ist ClaudeIstek) (*ClaudeYanit, error) {
		if n == 1 {
			if !bytes.Contains(ist.Govde, []byte("2: yeni.com")) {
				t.Error("yeni oturum güncel listeyi görmüyor")
			}
		}
		return yanit("end_turn", bMetin("tamam")), nil
	}
	o.mesaj(t, tTg, "yeniden bak")
	if !strings.Contains(o.tg.son(tTg), "tamam") {
		t.Fatalf("yeni oturum: %q", o.tg.son(tTg))
	}
}

// Ajan eslesme_onayla'yı reddederse (kod tüketilmiş / destek kapalı / kota) kullanıcıya
// "panelde onay verin" denip sessiz kalınmaz; merkezdeki bekleyen eşleştirme silinir.
func TestEslesmeReddiKullaniciyaBildirilir(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod := "ZYXWVUTSRQPNMKJH"
	ozet, _ := protokol.EslesmeKoduOzet(kod)
	ajan.gonder(&protokol.KodOzet{KodOzet: ozet, SonGecerlilik: time.Now().Add(5 * time.Minute).Unix()})
	bekle(t, func() bool { o.m.ajan.mu.Lock(); defer o.m.ajan.mu.Unlock(); _, ok := o.m.ajan.kodlar[ozet]; return ok })
	o.mesaj(t, tTg, "/start "+kod)
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.eslesmeZrf) == 1 })
	ajan.mu.Lock()
	zarf := ajan.eslesmeZrf[0]
	ajan.mu.Unlock()
	// başka bir zarfa yanıt: eşleştirme reddi sayılmaz
	ajan.gonder(&protokol.HataMesaji{Kod: protokol.HataDevreDisi, Ilgili: "baska_zarf_0001"})
	ajan.gonder(&protokol.HataMesaji{Kod: protokol.HataDevreDisi, Ilgili: zarf})
	bekle(t, func() bool { return strings.Contains(o.tg.son(tTg), "eşleştirmeyi reddetti (destek_kapali)") })
	if n := o.m.sayacAl("eslesme_red"); n != 1 {
		t.Fatalf("eslesme_red sayacı %d", n)
	}
	o.m.ajan.mu.Lock()
	_, kaldi := o.m.ajan.eslesmeler[ozet]
	o.m.ajan.mu.Unlock()
	if kaldi {
		t.Fatal("reddedilen eşleştirme merkezde kaldı")
	}
	// reddedilen eşleştirmeden bağlantı doğamaz
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_red_00001", KodOzet: ozet, TgUserID: tTg, Durum: protokol.BaglantiOnayBekliyor,
		Rol: protokol.RolMusteri, Duzey: protokol.DuzeyKapali})
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.hatalar) >= 1 })
	if _, var_ := o.depo.BaglantiAl("bag_red_00001"); var_ {
		t.Fatal("reddedilmiş eşleştirmeden bağlantı kaydedildi")
	}
}

// /start kodu protokol kuralıyla normalleştirilir: boşluklu yazım da eşleşir; kural dışı
// kod ajana hiç sorulmaz ve hız sınırını tüketmez.
func TestStartKoduNormallestirilir(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod, _ := protokol.EslesmeKoduUret(nil)
	ozet, _ := protokol.EslesmeKoduOzet(kod)
	ajan.gonder(&protokol.KodOzet{KodOzet: ozet, SonGecerlilik: time.Now().Add(5 * time.Minute).Unix()})
	bekle(t, func() bool { o.m.ajan.mu.Lock(); defer o.m.ajan.mu.Unlock(); _, ok := o.m.ajan.kodlar[ozet]; return ok })
	for _, bozuk := range []string{"ABC", strings.Repeat("A", 17), "ABCD-EFGH-IJKL-MNOP"} { // I/L/O alfabede yok
		o.mesaj(t, tTg, "/start "+bozuk)
	}
	ajan.mu.Lock()
	n := len(ajan.eslesmeler)
	ajan.mu.Unlock()
	if n != 0 {
		t.Fatalf("bozuk kod ajana gitti: %d", n)
	}
	if !strings.Contains(o.tg.son(tTg), "biçimi") {
		t.Fatalf("bozuk kod yanıtı: %q", o.tg.son(tTg))
	}
	g := protokol.EslesmeKoduGorunen(kod)
	o.mesaj(t, tTg, "/start "+strings.ToLower(strings.ReplaceAll(g, "-", " ")))
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.eslesmeler) == 1 })
	ajan.mu.Lock()
	defer ajan.mu.Unlock()
	if e := ajan.eslesmeler[0]; e.KodOzet != ozet {
		t.Fatalf("özet: %s", e.KodOzet)
	}
}
