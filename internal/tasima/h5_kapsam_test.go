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
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	stdlog "log"
	"strings"
	"testing"

	"girginospanel/internal/domains"
	"girginospanel/internal/plans"
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
	// Bayi: sabit, kiracı verisi içermeyen ret nedenleri AYNEN görünür; sarmalayıcının
	// bağlamı ("plan: …") sızmaz, yalnız sabit metin döner.
	for _, acik := range []error{errBayiSandboxYok, plans.ErrPlanKapsamDisi, plans.ErrOzelPlanBaskaHosting} {
		sarili := fmt.Errorf("plan (bayi #9 'gizli-plan'): %w", acik)
		if got := hataMetni(false, sarili); got == nil || got.Error() != acik.Error() {
			t.Errorf("bayi %q nedenini aynen görmeli, alınan: %v", acik, got)
		}
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
	// 🔴 KAPSAMLI HATA: bayiye AdAlaniHatasi'nın ham tipi/ayrıntısı (komşunun adı,
	// sahibi) SIZMAMALI — genel mesaja düzlenmiş olmalı (hataMetni defer'i).
	var ahA *domains.AdAlaniHatasi
	if errors.As(errA, &ahA) {
		t.Errorf("bayiye AdAlaniHatasi tipi sızdı (hata metni süzülmedi): %v", errA)
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

// HesapAktar bayi üzerine-yazma: hedef başka bayinin YA DA kökün ise (sahip ≠ kapsam)
// reddedilir; bayinin kendi domaini üzerine yazılabilir.
func TestHesapAktarBayiUstuneYaz_SahipEsitligi(t *testing.T) {
	h := h5_bayiOrtam(t)
	saglanan, _, gercek := ta_kalkan(t, nil)

	// ez.bayi9.test bayi 9'undur, kokdomain.test kökündür (reseller_id=0); bayi 7
	// ikisinin de üzerine yazamaz. (Kök satırı ayrıca denenir: "mevcutRID != 0"
	// gibi bir gevşetme kökün sitesini bayiye açardı.)
	for _, alan := range []string{"ez.bayi9.test", "kokdomain.test"} {
		_, err := h.HesapAktar(context.Background(), &Kaynak{},
			Hesap{AlanAdi: alan, PHPSurum: "8.3"},
			Ayarlar{Ustune: true}, Kapsam{RID: 7}, func(string, ...any) {})
		if err == nil {
			t.Errorf("%s: başka sahibin domaini üzerine yazma reddedilmeliydi", alan)
		}
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("üzerine yazma reddi hiçbir sağlama tetiklememeli: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
	// Bayinin KENDİ domaini (bayi7.test, reseller_id=7): üzerine yazılır (tüm adımlar
	// kapalı → başarı). Sahip eşitliği aşırı daraltılırsa burada yakalanır.
	if _, err := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "bayi7.test"},
		Ayarlar{Ustune: true}, Kapsam{RID: 7}, func(string, ...any) {}); err != nil {
		t.Errorf("bayi kendi domaini üzerine yazabilmeli: %v", err)
	}
	// Kök (kapsam yok) aynı domaini üzerine yazabilir: sahip eşitliği yalnız bayiye.
	// (Ustune=true, yeniOlusturulmaz → tüm adımlar kapalı → başarı.)
	if _, err := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "ez.bayi9.test"},
		Ayarlar{Ustune: true}, Kapsam{}, func(string, ...any) {}); err != nil {
		t.Errorf("kök üzerine yazması engellenmemeli: %v", err)
	}
}

// h5_insertOrtam — h5_bayiOrtam + taşıma INSERT'inin sütunları (web_backend dahil):
// burada INSERT BAŞARILI olur → oluşan satırın sahibi ve geri alma doğrulanabilir.
// Ek: bayi 9'un müşterisi (customers 5) ve plan kataloğu (plans/kapsam.go modeli):
//
//	1 kökün global katalog planı · 2 bayi 9'un katalog planı · 3 bayi 7'nin katalog planı
//	4 kökün kokdomain.test'e ÖZEL planı · 5 bayi 7'nin bayi7.test'e ÖZEL planı
//
// 🔴 Kaynak limitleri (systemd slice / kiracı FPM — CANLI sistem) sahteyle değişir;
// sağlama ve geri alma ta_kalkan ile sahtedir. Dönen sayaç sahte limit çağrılarıdır.
func h5_insertOrtam(t *testing.T) (*Handlers, *int) {
	t.Helper()
	h := h5_bayiOrtam(t)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN php_surum VARCHAR(8) NULL, ADD COLUMN ftp_host VARCHAR(64) NULL,
			ADD COLUMN db_host VARCHAR(64) NULL, ADD COLUMN db_user VARCHAR(64) NULL,
			ADD COLUMN db_adi VARCHAR(64) NULL, ADD COLUMN durum VARCHAR(16) NOT NULL DEFAULT 'aktif',
			ADD COLUMN plan_id BIGINT NULL, ADD COLUMN web_backend VARCHAR(16) NULL,
			ADD COLUMN customer_id BIGINT NULL`,
		`CREATE TABLE customers (id BIGINT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO customers (id, reseller_id) VALUES (5, 9)`,
		`CREATE TABLE service_plans (id BIGINT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0,
			domain_id BIGINT NULL)`,
		`INSERT INTO service_plans (id, reseller_id, domain_id) VALUES
			(1,0,NULL), (2,9,NULL), (3,7,NULL), (4,0,10), (5,7,20)`,
	} {
		if _, err := h.DB.Exec(q); err != nil {
			t.Fatalf("h5 insert şeması: %v\n%s", err, q)
		}
	}
	eski := limitUygula
	var n int
	limitUygula = func(context.Context, *sql.DB, int64) error { n++; return nil }
	t.Cleanup(func() { limitUygula = eski })
	return h, &n
}

// Planın H5 testi: gövdede başka bayi + onun müşterisi (ay.ResellerID/CustomerID ve
// SahipEsle) gönderilse de hesap KENDİ bayisinde açılır (reseller_id=kapsam,
// customer_id=NULL); öteki bayinin kayıtları değişmez.
func TestHesapAktarBayiKapsam_SatirKendiBayisinde(t *testing.T) {
	h, limit := h5_insertOrtam(t)
	saglanan, _, gercek := ta_kalkan(t, nil)

	ay := Ayarlar{ResellerID: 9, CustomerID: 5,
		SahipEsle: map[string]sahipHedef{"srcuser": {ResellerID: 9, CustomerID: 5}}}
	sonuc, err := h.HesapAktar(context.Background(), &Kaynak{},
		Hesap{AlanAdi: "yenisite.test", PHPSurum: "8.3", SahipLogin: "srcuser"},
		ay, Kapsam{RID: 7}, func(string, ...any) {})
	if err != nil {
		t.Fatalf("bayi kendi hesabına taşıyabilmeli: %v", err)
	}
	var rid int64
	var cid sql.NullInt64
	var durum string
	if err := h.DB.QueryRow(`SELECT reseller_id, customer_id, durum FROM domains WHERE id=?`,
		sonuc.DomainID).Scan(&rid, &cid, &durum); err != nil {
		t.Fatalf("oluşan satır okunamadı: %v", err)
	}
	if rid != 7 || cid.Valid {
		t.Errorf("hesap kapsamın bayisinde açılmalı: reseller_id=%d customer_id=%v (beklenen 7, NULL)", rid, cid)
	}
	if durum != "aktif" {
		t.Errorf("başarılı taşıma aktif işaretlenmeli: %q", durum)
	}
	if len(*saglanan) != 1 || *gercek != 0 || *limit != 1 {
		t.Errorf("tek sağlama + tek limit uygulaması beklenir: sahte=%v gerçek=%d limit=%d", *saglanan, *gercek, *limit)
	}
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE reseller_id=9`).Scan(&n); err != nil || n != 2 {
		t.Errorf("bayi 9'un kayıtları değişti: n=%d err=%v", n, err)
	}
}

// Bayi gövdesindeki PlanID domains.Create ile aynı kuralla (plans.BaglanabilirMi)
// denetlenir: görmediği ya da bir hostinge özel plan sağlamadan ÖNCE reddedilir ve
// ret nedeni (sabit metin) bayiye aynen döner; görebildiği katalog planı bağlanır.
func TestHesapAktarBayiPlanKapsami(t *testing.T) {
	h, _ := h5_insertOrtam(t)
	saglanan, _, gercek := ta_kalkan(t, nil)

	for _, c := range []struct {
		plan     int64
		beklenen error
	}{
		{2, plans.ErrPlanKapsamDisi},       // başka bayinin katalog planı
		{4, plans.ErrPlanKapsamDisi},       // kökün hostingine özel plan
		{999, plans.ErrPlanKapsamDisi},     // olmayan plan — yabancıyla aynı yanıt
		{5, plans.ErrOzelPlanBaskaHosting}, // kendi hostingine özel plan yeni siteye bağlanamaz
	} {
		_, err := h.HesapAktar(context.Background(), &Kaynak{},
			Hesap{AlanAdi: fmt.Sprintf("plan%d.test", c.plan), PHPSurum: "8.3"},
			Ayarlar{PlanID: c.plan}, Kapsam{RID: 7}, func(string, ...any) {})
		if err == nil || err.Error() != c.beklenen.Error() {
			t.Errorf("plan %d: %q beklenirdi, alınan %v", c.plan, c.beklenen, err)
		}
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("plan reddi sağlamadan önce olmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
	// Görebildiği katalog planları: kendi (3) ve kökün global kataloğu (1).
	for _, plan := range []int64{3, 1} {
		sonuc, err := h.HesapAktar(context.Background(), &Kaynak{},
			Hesap{AlanAdi: fmt.Sprintf("plan%d.test", plan), PHPSurum: "8.3"},
			Ayarlar{PlanID: plan}, Kapsam{RID: 7}, func(string, ...any) {})
		if err != nil {
			t.Errorf("plan %d bağlanabilmeli: %v", plan, err)
			continue
		}
		var got sql.NullInt64
		if err := h.DB.QueryRow(`SELECT plan_id FROM domains WHERE id=?`, sonuc.DomainID).Scan(&got); err != nil || got.Int64 != plan {
			t.Errorf("plan %d satıra yazılmalı: %v err=%v", plan, got, err)
		}
	}
}

// 🔴 REGRESYON (geri alma paniği): sağlama + domain kaydı SONRASI bir adım düşerse
// (burada dosya adımı: göreli kaynak yolu — yan etkisiz ret) HesapAktar panik
// vermeden hatayla döner, domain satırı silinir ve sağlama geri alınır — kök ve
// bayi kapsamında. Bayi hatasının HAM hâli sunucu günlüğüne yazılır (bayiye dönen
// genel metin "ayrıntı sunucu günlüğüne yazıldı" der).
func TestHesapAktarGeriAlma_SaglamaSonrasiHata(t *testing.T) {
	h, _ := h5_insertOrtam(t)
	_, geriAlinan, gercek := ta_kalkan(t, nil)
	var gunluk bytes.Buffer
	eskiCikis := stdlog.Writer()
	stdlog.SetOutput(&gunluk)
	t.Cleanup(func() { stdlog.SetOutput(eskiCikis) })

	for _, c := range []struct {
		alan     string
		kapsam   Kapsam
		beklenen string
	}{
		{"geri-kok.test", Kapsam{}, "kaynak web kok dizini gecersiz"},
		{"geri-bayi.test", Kapsam{RID: 7}, tasimaHataGenel},
	} {
		_, err := h.HesapAktar(context.Background(), &Kaynak{},
			Hesap{AlanAdi: c.alan, PHPSurum: "8.3", WebRoot: "goreli/yol"},
			Ayarlar{Dosyalar: true}, c.kapsam, func(string, ...any) {})
		if err == nil || err.Error() != c.beklenen {
			t.Errorf("%s: %q beklenirdi, alınan %v", c.alan, c.beklenen, err)
		}
		var n int
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE alan_adi=?`, c.alan).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: geri alınan domain satırı silinmeli: n=%d err=%v", c.alan, n, err)
		}
	}
	if strings.Join(*geriAlinan, ",") != "geri-kok.test,geri-bayi.test" || *gercek != 0 {
		t.Errorf("her iki kapsamda sağlama geri alınmalı: geri=%v gerçek=%d", *geriAlinan, *gercek)
	}
	g := gunluk.String()
	if !strings.Contains(g, "bayi=7") || !strings.Contains(g, `"geri-bayi.test"`) ||
		!strings.Contains(g, "kaynak web kok dizini gecersiz") {
		t.Errorf("bayi hatasının ham hâli sunucu günlüğüne yazılmalı, günlük: %q", g)
	}
}
