package tasima

// h5_kapsam_test.go — H5 yürütme katmanında kapsam:
//   - hataMetni: kök ham hatayı görür (tip korunur); bayiye ayrıntı sızmaz.
//   - HesapAktar bayi kapsamında (RID>0): sahip Ayarlar/SahipEsle'den DEĞİL kapsamdan
//     belirlenir → site YALNIZ bayinin kendi hesabına açılır; başka kiracının ad-alanı
//     reddedilir; üzerine yazmada sahip eşitliği aranır.
//   - sahipleriHazirla bayi kapsamında reddeder.
//
// TestHataMetni ve TestSahipleriHazirlaBayiKapsam DB gerektirmez; HesapAktar testleri
// ta_db (GOSP_TEST_GECICI_DSN yoksa atlanır) + ta_kalkan (sahte sağlama) kullanır.

import (
	"context"
	"errors"
	"testing"

	"girginospanel/internal/domains"
)

func TestHataMetni(t *testing.T) {
	ham := errors.New("saglama: /home/c_komsu/public_html erişilemedi")
	ad := &domains.AdAlaniHatasi{Genel: "bu alan adı zaten kayıtlı", Ayrinti: "sahibi: bayi #9"}

	// Yönetici (kök): her hata OLDUĞU GİBİ; tip korunur.
	if got := hataMetni(true, ham); got != ham {
		t.Errorf("yönetici ham hatayı olduğu gibi görmeli: %v", got)
	}
	if got := hataMetni(true, ad); got.Error() != ad.Error() {
		t.Errorf("yönetici ad-alanı ayrıntısını görmeli: %q", got.Error())
	}
	var ah *domains.AdAlaniHatasi
	if !errors.As(hataMetni(true, ad), &ah) {
		t.Errorf("yöneticide AdAlaniHatasi tipi korunmalı (errors.As)")
	}

	// Bayi: ad-alanı → güvenli genel mesaj (ayrıntısız); tip DÜZLENİR.
	byAd := hataMetni(false, ad)
	if byAd.Error() != ad.Mesaj(false) {
		t.Errorf("bayi ad-alanı genel mesajını görmeli: %q", byAd.Error())
	}
	if errors.As(byAd, &ah) {
		t.Errorf("bayiye AdAlaniHatasi ayrıntısı (tip) sızmamalı")
	}
	// Bayi: başka her hata → sabit genel metin (sistem yolu sızmaz).
	byHam := hataMetni(false, ham)
	if byHam.Error() != tasimaHataGenel {
		t.Errorf("bayi genel metni görmeli, alınan: %q", byHam.Error())
	}
	if got := hataMetni(false, nil); got != nil {
		t.Errorf("nil hata nil dönmeli: %v", got)
	}
}

func TestSahipleriHazirlaBayiKapsam(t *testing.T) {
	// Bayi kapsamı: sahip (reseller/müşteri) oluşturma yolu DB'ye dokunmadan reddedilir.
	_, _, err := sahipleriHazirla(context.Background(), nil, []KaynakSahip{
		{Tip: "reseller", Login: "kaan", Ad: "Kaan"},
	}, Kapsam{RID: 7})
	if err == nil {
		t.Fatalf("bayi kapsamında sahip oluşturma reddedilmeli")
	}
	// Kök + boş liste: DB'ye dokunmadan sorunsuz döner (guard yalnız bayiyi keser).
	if _, _, err := sahipleriHazirla(context.Background(), nil, nil, Kapsam{}); err != nil {
		t.Fatalf("kök + boş listede hata olmamalı: %v", err)
	}
}

// h5_bayiOrtam — ta_db şemasına 9. bayi + domainlerini ekler.
func h5_bayiOrtam(t *testing.T) *Handlers {
	t.Helper()
	db := ta_db(t)
	for _, q := range []string{
		`INSERT INTO users (id, role) VALUES (9,'reseller')`,
		// bayi 9'un ad-alanı: alt adı (x.bayi9.test) başka kiracıya kapalı;
		// ez.bayi9.test = üzerine-yazma sahip-eşitliği testinin mevcut hedefi.
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (30,'bayi9.test',9), (40,'ez.bayi9.test',9)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("h5 şema: %v\n%s", err, q)
		}
	}
	return &Handlers{DB: db}
}

// HesapAktar bayi kapsamı: gövdede başka bayi (ay.ResellerID=9 + SahipEsle→9)
// gönderilse de sahip KAPSAMDAN (RID=7) belirlenir.
func TestHesapAktarBayiKapsam_SahipKapsamdan(t *testing.T) {
	h := h5_bayiOrtam(t)
	saglanan, _, gercek := ta_kalkan(t, nil)

	// Gövde bayi 9'u işaret eder; kapsam bayi 7.
	ay := Ayarlar{ResellerID: 9, SahipEsle: map[string]sahipHedef{"srcuser": {ResellerID: 9}}}

	// (A) Başka bayinin (9) alt adı → kapsam 7 için ad-alanı reddi; sağlamaya VARMAZ.
	//     Kod ay.ResellerID(9)'u kullansaydı aynı-sahip alt adı SERBEST olur, sağlamaya
	//     ulaşırdı → mutasyon burada yakalanır.
	_, errA := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "x.bayi9.test", PHPSurum: "8.3", SahipLogin: "srcuser"},
		ay, Kapsam{RID: 7}, func(string, ...any) {})
	if errA == nil {
		t.Errorf("başka bayinin alt adı bayi kapsamında reddedilmeliydi")
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("ret sağlamadan önce olmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
	// Bayi 9'un domaini dokunulmadan durmalı.
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE alan_adi='bayi9.test' AND reseller_id=9`).Scan(&n); err != nil || n != 1 {
		t.Errorf("bayi 9'un domaini değişti: n=%d err=%v", n, err)
	}

	// (B) Bayinin KENDİ (7) alt adı → serbest; sağlamaya ulaşır (INSERT şemada düşer).
	_, errB := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "x.bayi7.test", PHPSurum: "8.3", SahipLogin: "srcuser"},
		ay, Kapsam{RID: 7}, func(string, ...any) {})
	if errB == nil {
		t.Errorf("INSERT şemada düşmeli (web_backend yok) — hata beklenirdi")
	}
	if len(*saglanan) != 1 || *gercek != 0 {
		t.Errorf("kendi alt adı sağlamaya bir kez ulaşmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
}

// HesapAktar bayi üzerine-yazma: hedef başka bayinindeyse (sahip ≠ kapsam) reddedilir.
func TestHesapAktarBayiUstuneYaz_SahipEsitligi(t *testing.T) {
	h := h5_bayiOrtam(t)
	saglanan, _, gercek := ta_kalkan(t, nil)

	// ez.bayi9.test bayi 9'undur; bayi 7 üzerine yazamaz.
	_, err := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "ez.bayi9.test", PHPSurum: "8.3"},
		Ayarlar{Ustune: true}, Kapsam{RID: 7}, func(string, ...any) {})
	if err == nil {
		t.Fatalf("başka bayinin domaini üzerine yazma reddedilmeliydi")
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("üzerine yazma reddi hiçbir sağlama tetiklememeli: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
	// Kök (kapsam yok) aynı domaini üzerine yazabilir: sahip eşitliği yalnız bayiye.
	// (Ustune=true, yeniOlusturulmaz → tüm adımlar kapalı → başarı.)
	if _, err := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "ez.bayi9.test"},
		Ayarlar{Ustune: true}, Kapsam{}, func(string, ...any) {}); err != nil {
		t.Errorf("kök üzerine yazması engellenmemeli: %v", err)
	}
}
