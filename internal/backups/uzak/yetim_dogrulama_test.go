package uzak

// yetim_dogrulama_test.go — DÜŞMANCA DOĞRULAMA (yetim yedek tasarımı, commit e449f18): uzak gönderim / yerel silme / işçi
// tarafında 'kurtarma' tipi ve yetim (domain_id=0) satırların davranışı. Ağa ÇIKILMAZ (hedef: hedeftest.Bellek).
// Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TopluAdaylar: 'kurtarma' satırları (job_id taşısa da, id'leri oto'lardan büyük ya da küçük olsa da) domain başına "en yeni N" sırasını
// BOZMAZ ve aday olmaz; adaylar eskiden yeniye döner.
func TestDG_U1_TopluAdaylar_KurtarmaSatirlariSiralamayiBozmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	ctx := context.Background()
	a := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	o.yedek(1, "c_a", "20260902-020000", sahteIcerik(10, 2), "kurtarma", nil)
	b := o.yedek(1, "c_a", "20260903-020000", sahteIcerik(10, 3), "oto", nil)
	c := o.yedek(1, "c_a", "20260904-020000", sahteIcerik(10, 4), "oto", nil)
	o.yedek(1, "c_a", "20260905-020000", sahteIcerik(10, 5), "kurtarma", 99) // en yeni kimlik + job_id
	d := o.yedek(2, "c_b", "20260901-020000", sahteIcerik(10, 6), "oto", nil)
	var kurtarma int64
	_ = o.db.QueryRow(`SELECT id FROM backups WHERE tip='kurtarma' ORDER BY id DESC LIMIT 1`).Scan(&kurtarma)
	if _, _, err := KuyrugaAl(ctx, o.db, kurtarma); !errors.Is(err, ErrKokDegil) {
		t.Errorf("job_id taşıyan kurtarma yedeği kuyruğa alınmamalı (ErrKokDegil): %v", err)
	}
	if _, err := SimdiGonder(ctx, o.db, kurtarma); !errors.Is(err, ErrKokDegil) {
		t.Errorf("SimdiGonder de kurtarma yedeğini reddetmeli: %v", err)
	}
	for _, v := range []struct {
		n   int
		bek []int64
	}{{1, []int64{c, d}}, {2, []int64{b, c, d}}, {5, []int64{a, b, c, d}}} {
		ids, _, err := TopluAdaylar(ctx, o.db, v.n)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ids, v.bek) {
			t.Errorf("n=%d: adaylar %v, beklenen %v (kurtarma satırı sırayı bozdu ya da aday oldu)", v.n, ids, v.bek)
		}
	}
}

// Silme izi yazılamamış satır (domain_id>0 ama domains'te karşılığı yok) uzak gönderime/yerel silmeye/kuyruğa GİRMEZ.
func TestDG_U2_IzYazilamamisSatir_DomainYok_AdayDegilKuyrugaAlinmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	ctx := context.Background()
	canli := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	sahipsiz := o.yedek(2, "c_b", "20260901-020000", sahteIcerik(10, 2), "oto", nil)
	_, _ = o.db.Exec(`ALTER TABLE backups DROP FOREIGN KEY fk_backup_domain`)
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=2`); err != nil { // satır domain_id=2'de KALIR (iz yazılamadı)
		t.Fatal(err)
	}
	ids, _, err := TopluAdaylar(ctx, o.db, 5)
	if err != nil || !reflect.DeepEqual(ids, []int64{canli}) {
		t.Errorf("toplu aday yalnız canlı domainin yedeği olmalı: %v %v", ids, err)
	}
	if _, _, err := KuyrugaAl(ctx, o.db, sahipsiz); !errors.Is(err, ErrYedekYok) {
		t.Errorf("sahipsiz satır kuyruğa alınmamalı: %v", err)
	}
	if _, err := SimdiGonder(ctx, o.db, sahipsiz); !errors.Is(err, ErrYedekYok) {
		t.Errorf("SimdiGonder da reddetmeli: %v", err)
	}
	kayitlar, err := yerelKayitlari(ctx, o.db, o.kok, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kayitlar {
		if k.yedekID == sahipsiz {
			t.Error("yerel-silme taraması sahipsiz satırı içermemeli")
		}
	}
}

// Yerel-silme sıralaması: 'kurtarma' satırı "son N" sayacına GİRMEZ (sira=0, kök değil, elle sınıfı); oto satırları kendi aralarında sıralanır.
func TestDG_U3_YerelKayitlari_KurtarmaSiraSayacinaGirmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	oto1 := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	oto2 := o.yedek(1, "c_a", "20260902-020000", sahteIcerik(10, 2), "oto", nil)
	kur := o.yedek(1, "c_a", "20260903-020000", sahteIcerik(10, 3), "kurtarma", 7)
	kayitlar, err := yerelKayitlari(context.Background(), o.db, o.kok, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	sira := map[int64]int{}
	kok := map[int64]bool{}
	sinif := map[int64]string{}
	for _, k := range kayitlar {
		sira[k.yedekID], kok[k.yedekID], sinif[k.yedekID] = k.sira, k.kok, k.sinif
	}
	if kok[kur] || sira[kur] != 0 || sinif[kur] != sinifManuel {
		t.Errorf("kurtarma: kök=%v sira=%d sınıf=%s (kök değil, sıra 0, manuel sınıfı beklenir)", kok[kur], sira[kur], sinif[kur])
	}
	if sira[oto2] != 1 || sira[oto1] != 2 || !kok[oto1] || !kok[oto2] {
		t.Errorf("oto sırası: oto2=%d oto1=%d (1,2 beklenir; kurtarma sayacı kaydırmamalı)", sira[oto2], sira[oto1])
	}
}

// Kuyrukta/bekleyen iş varken hosting silinip yedek yetim olursa: işçi KALICI hata verir, hiçbir şey yüklemez, yerel dosyaya dokunmaz;
// "yeniden dene" de aynı sonucu verir (yetim yedek başka bir yoldan uzağa sızmaz).
func TestDG_U4_BekleyenIs_YedekYetimOlunca_KaliciHata_YuklemeYapilmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	ctx := context.Background()
	y := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(1000, 1), "oto", nil)
	is := o.kuyrukla(y)
	var dosya string
	_ = o.db.QueryRow(`SELECT dosya FROM backups WHERE id=?`, y).Scan(&dosya)
	yerel := filepath.Join(o.kok, "c_a", dosya)

	o.yetimYap(y) // hosting silindi: satır domain_id=0
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// kimlik yeniden kullanıldı: BAŞKA müşteri (c_baska) aynı kimlikle açıldı ve dizininde AYNI adlı bir dosya var
	if _, err := o.db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'baska.example','c_baska')`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(o.kok, "c_baska"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.kok, "c_baska", dosya), []byte("BASKA-MUSTERI-VERISI"), 0o600); err != nil {
		t.Fatal(err)
	}
	o.calis()
	d := o.is(is)
	if d.durum != DurumHata || !strings.Contains(d.sonHata, "yetim") {
		t.Errorf("yetim yedek için iş KALICI hata ('yetim' açıklamalı) vermeli: %+v", d)
	}
	if n := len(o.bel.Adlar()); n != 0 {
		t.Errorf("🔴 yetim yedek uzağa YÜKLENDİ: %v", o.bel.Adlar())
	}
	if _, err := os.Stat(yerel); err != nil {
		t.Errorf("yerel yedek dosyasına dokunulmamalı: %v", err)
	}
	if err := YenidenDene(ctx, o.db, is); err != nil {
		t.Fatal(err)
	}
	o.calis()
	if d := o.is(is); d.durum != DurumHata || len(o.bel.Adlar()) != 0 {
		t.Errorf("yeniden dene de yetim yedeği göndermemeli: %+v yüklenen=%v", d, o.bel.Adlar())
	}
}

// Yetim yedek başka bir hosting olarak yeniden bağlanırsa (domain_id=2, tip 'kurtarma'): eski domainin iş kaydı yetim sayılır, kalıcı hata verir.
func TestDG_U5_BekleyenIs_YedekBaskaDomaineBaglaninca_KaliciHata(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	y := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(1000, 1), "oto", nil)
	is := o.kuyrukla(y)
	o.domain(2, "c_a2")
	_, _ = o.db.Exec(`ALTER TABLE backups DROP FOREIGN KEY fk_backup_domain`)
	if _, err := o.db.Exec(`UPDATE backups SET domain_id=2, tip='kurtarma' WHERE id=?`, y); err != nil {
		t.Fatal(err)
	}
	o.calis()
	if d := o.is(is); d.durum != DurumHata || len(o.bel.Adlar()) != 0 {
		t.Errorf("başka domaine bağlanan yedek için eski iş yükleme yapmamalı: %+v yüklenen=%v", d, o.bel.Adlar())
	}
}
