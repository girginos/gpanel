package ajan

// kapatma_db_test.go — 2026-10-04 ürün kararı: panelde Telegram Destek anahtarı YOKTUR.
// Ajan, merkez anahtarı varsa HER ZAMAN bağlanır (destek_ayar satırı gerekmez); tek yerel
// kapatma, root'un acil kapatma dosyasıdır (VarsayilanKapatmaDosyasi). Burada: şalter/tavan
// olmadan bağlanma, dosyanın bağlantıyı koparması/engellemesi, kalkınca geri gelmesi, nabızda
// raporlanması.

import (
	"context"
	"os"
	"testing"
	"time"

	p "girginospanel/internal/destek/protokol"
)

func (o *ortam) nabizDurumBekle(durum string, sn int) Nabiz {
	o.t.Helper()
	son := time.Now().Add(time.Duration(sn) * time.Second)
	var n Nabiz
	for time.Now().Before(son) {
		n = NabizOku(context.Background(), o.db)
		if n.Canli && n.Durum.Durum == durum {
			return n
		}
		time.Sleep(30 * time.Millisecond)
	}
	o.t.Fatalf("nabız durumu %q olmadı: %+v", durum, n)
	return n
}

// destek_ayar satırı HİÇ yokken ajan merkeze bağlanır, eşleştirmeyi kabul eder ve iş yürütür.
func TestAyarSatiriYokkenBaglanir(t *testing.T) {
	o := kurOrtam(t, false, true) // bagliBekle zaten bağlanmayı doğrular
	var n int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM destek_ayar`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ön koşul: destek_ayar boş olmalı: n=%d err=%v", n, err)
	}
	kim := o.eslestir(o.kok, 777, "kok_tg", "okuma")
	e := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e)
	if s := o.m.isSonucu(e.IsKodu); !s.Ok {
		t.Fatalf("ayarsız iş: %+v", s)
	}
	o.nabizDurumBekle(BagBagli, 5)
}

// Acil kapatma dosyası: bağlıyken belirirse KOPAR (≤ aralık), yeniden bağlanmaya çalışmaz,
// nabızda "yerel_kapali" görünür; dosya silinince yeniden bağlanır.
func TestAcilKapatmaDosyasiKoparirVeKalkinca(t *testing.T) {
	o := kurOrtam(t, false, true)
	kim := o.eslestir(o.kok, 778, "kok_tg", "okuma")
	n0 := o.m.baglanti.Load()

	basla := time.Now()
	if err := os.WriteFile(o.kapatma, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for o.a.Bagli() {
		if time.Since(basla) > 3*time.Second {
			t.Fatal("kapatma dosyası varken bağlantı ≤3 sn'de kopmadı")
		}
		time.Sleep(10 * time.Millisecond)
	}
	nb := o.nabizDurumBekle(BagYerelKapali, 3)
	if nb.MerkezBagl {
		t.Fatalf("kapalıyken nabız merkez_bagli: %+v", nb)
	}
	// Kapalıyken yeniden bağlanma denemesi YOK.
	time.Sleep(900 * time.Millisecond)
	if o.m.baglanti.Load() != n0 || o.a.Bagli() {
		t.Fatalf("kapalıyken yeniden bağlanıldı (%d → %d)", n0, o.m.baglanti.Load())
	}
	// Kapalıyken eşleştirme kaydı ve iş emri yürütülmez.
	if s := o.a.isYurut(context.Background(), o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("kapalıyken iş: %+v", s)
	}

	// Dosya kalkınca yeniden bağlanır; mevcut bağlantı kaydı geçerlidir.
	if err := os.Remove(o.kapatma); err != nil {
		t.Fatal(err)
	}
	o.bagliBekle()
	o.nabizDurumBekle(BagBagli, 5)
	e := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e)
	if s := o.m.isSonucu(e.IsKodu); !s.Ok {
		t.Fatalf("dosya kalkınca iş: %+v", s)
	}
}

// Ajan kapatma dosyasıyla BAŞLARSA hiç bağlanmaz; dosya kalkınca bağlanır.
func TestAcilKapatmaDosyasiVarkenBaslamaz(t *testing.T) {
	o := kurOrtam(t, false, false)
	time.Sleep(900 * time.Millisecond)
	if o.m.baglanti.Load() != 0 || o.a.Bagli() {
		t.Fatal("kapatma dosyası varken merkeze bağlanıldı")
	}
	o.nabizDurumBekle(BagYerelKapali, 3)
	if err := os.Remove(o.kapatma); err != nil {
		t.Fatal(err)
	}
	o.bagliBekle()
}

// yerelKapali: dosya, dizin ve kırık sembolik bağ KAPALI sayılır; yol boşsa özellik yok.
func TestYerelKapaliTurleri(t *testing.T) {
	d := t.TempDir()
	a := Yeni(nil, Yapilandirma{})
	a.KapatmaDosyasi = d + "/yok"
	if a.yerelKapali() {
		t.Fatal("dosya yokken kapalı")
	}
	if err := os.WriteFile(a.KapatmaDosyasi, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !a.yerelKapali() {
		t.Fatal("dosya varken açık")
	}
	_ = os.Remove(a.KapatmaDosyasi)
	if err := os.Symlink(d+"/hedef-yok", a.KapatmaDosyasi); err == nil && !a.yerelKapali() {
		t.Fatal("kırık sembolik bağ KAPALI sayılmalı")
	}
	_ = os.Remove(a.KapatmaDosyasi)
	_ = os.Mkdir(a.KapatmaDosyasi, 0o700)
	if !a.yerelKapali() {
		t.Fatal("dizin KAPALI sayılmalı")
	}
	// Varlığı belirlenemeyen yol (üst bileşen dosya → ENOTDIR; izin hatası da aynı sınıf) KAPALI sayılır.
	dosya := d + "/dosya"
	_ = os.WriteFile(dosya, nil, 0o600)
	a.KapatmaDosyasi = dosya + "/destek-kapali"
	if !a.yerelKapali() {
		t.Fatal("belirsiz durum (NotExist dışı hata) KAPALI sayılmalı (kapalı başarısız)")
	}
	if Yeni(nil, Yapilandirma{}).KapatmaDosyasi != "/etc/girginospanel/destek-kapali" {
		t.Fatal("varsayılan kapatma yolu değişti (belge: KURULUM.md)")
	}
}

// Kapatma dosyası varken (bağlantı henüz kopmadan) gelen imzalı eşleştirme onayı da KABUL
// EDİLMEZ: bağlantı satırı yazılmaz.
func TestKapaliykenEslesmeOnayiKabulEdilmez(t *testing.T) {
	o := kurOrtam(t, false, true)
	e := o.v2(901, "kok_tg", "okuma", 7)
	o.m.imzala(e)
	if err := os.WriteFile(o.kapatma, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	o.a.mu.Lock()
	oturum := o.a.aktif
	o.a.mu.Unlock()
	if oturum == nil {
		t.Fatal("ön koşul: açık oturum yok")
	}
	o.a.eslesmeOnayla(context.Background(), oturum, "zk-test", e)
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM destek_baglantilari WHERE kod_ozet=?`, e.KodOzet).Scan(&n)
	if n != 0 {
		t.Fatalf("kapalıyken eşleştirme kaydı yazıldı (%d)", n)
	}
}
