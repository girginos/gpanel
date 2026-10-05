package uzak

// otomatik_test.go — sade akış: otomatik anahtar, kurtarma indirme, toplu gönderim.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func anahtarSil(t *testing.T, o *ortam) {
	t.Helper()
	if _, err := o.db.Exec(`DELETE FROM backup_sifre_anahtarlari`); err != nil {
		t.Fatal(err)
	}
}

func anahtarSay(t *testing.T, o *ortam, where string) int {
	t.Helper()
	var n int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM backup_sifre_anahtarlari ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOtomatikAnahtar_IlkAcilistaUretirIkinciUretmez(t *testing.T) {
	o := yeniOrtam(t)
	anahtarSil(t, o)
	a := Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7}
	ctx := context.Background()
	uretildi, err := AyarYazAnahtarli(ctx, o.db, a)
	if err != nil || !uretildi {
		t.Fatalf("anahtarsızken açılış anahtar ÜRETMELİ: %v %v", uretildi, err)
	}
	k, _ := EtkinAnahtar(ctx, o.db)
	if k == nil || k.Sifreleme != SifrelemeX25519 || !k.SunucudaOzel {
		t.Fatalf("etkin klasik anahtar bekleniyordu: %+v", k)
	}
	var kapali string
	_ = o.db.QueryRow(`SELECT gizli_anahtar FROM backup_sifre_anahtarlari`).Scan(&kapali)
	if strings.Contains(kapali, "AGE-SECRET-KEY") && !strings.HasPrefix(kapali, "gos1:") {
		t.Error("özel anahtar DB'de düz")
	}
	ilk := k.Parmak
	uretildi, err = AyarYazAnahtarli(ctx, o.db, a)
	if err != nil || uretildi {
		t.Fatalf("ikinci açılış anahtar ÜRETMEMELİ: %v %v", uretildi, err)
	}
	if n := anahtarSay(t, o, ""); n != 1 {
		t.Fatalf("tek anahtar olmalı: %d", n)
	}
	if k2, _ := EtkinAnahtar(ctx, o.db); k2.Parmak != ilk {
		t.Error("etkin anahtar değişti")
	}
}

func TestOtomatikAnahtar_KapaliAyarAnahtarUretmez(t *testing.T) {
	o := yeniOrtam(t)
	anahtarSil(t, o)
	if _, err := AyarYazAnahtarli(context.Background(), o.db, Ayar{Aktif: false, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7}); err != nil {
		t.Fatal(err)
	}
	if n := anahtarSay(t, o, ""); n != 0 {
		t.Fatalf("kapalı ayar anahtar üretmemeli: %d", n)
	}
}

func TestOtomatikAnahtar_EszamanliTekAnahtar(t *testing.T) {
	o := yeniOrtam(t)
	anahtarSil(t, o)
	var wg sync.WaitGroup
	hatalar := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := EtkinKur(context.Background(), o.db, false, false); err != nil {
				hatalar <- err
			}
		}()
	}
	wg.Wait()
	close(hatalar)
	for err := range hatalar {
		t.Errorf("eşzamanlı çağrı hata verdi: %v", err)
	}
	if n := anahtarSay(t, o, ""); n != 1 {
		t.Fatalf("eşzamanlı çağrıda TEK anahtar kalmalı: %d", n)
	}
	if n := anahtarSay(t, o, "WHERE durum='etkin'"); n != 1 {
		t.Fatalf("tek etkin: %d", n)
	}
}

func TestOtomatikAnahtar_SifreHazirDegilseHicbirSeyYazilmaz(t *testing.T) {
	o := yeniOrtam(t)
	anahtarSil(t, o)
	SirHazir = func() error { return errors.New("hazır değil") }
	_, err := AyarYazAnahtarli(context.Background(), o.db, Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7})
	if !errors.Is(err, ErrSifreHazirDegil) {
		t.Fatalf("beklenen ErrSifreHazirDegil: %v", err)
	}
	if n := anahtarSay(t, o, ""); n != 0 {
		t.Error("anahtar yazılmamalı")
	}
	if a, _ := AyarOku(context.Background(), o.db); a.Aktif {
		t.Error("ayar açılmamalı")
	}
}

func TestEtkinKur_ZorlaEskiyiEmekliYapar(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	eski, _ := EtkinAnahtar(ctx, o.db)
	yeni, uretildi, err := EtkinKur(ctx, o.db, true, true)
	if err != nil || !uretildi || yeni.Sifreleme != SifrelemeHibrit || yeni.Parmak == eski.Parmak {
		t.Fatalf("zorla yenileme: %+v %v %v", yeni, uretildi, err)
	}
	if n := anahtarSay(t, o, "WHERE durum='etkin'"); n != 1 {
		t.Errorf("tek etkin: %d", n)
	}
	if n := anahtarSay(t, o, "WHERE durum='emekli'"); n != 1 {
		t.Errorf("eski emekli: %d", n)
	}
}

func TestKurtarmaAnahtarlari_EtkinVeEmekliHepsiCozer(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	eski, _ := EtkinAnahtar(ctx, o.db)
	a, _ := AliciCoz(eski.Alici)
	var eskiSifreli bytes.Buffer
	if _, err := Sifrele(&eskiSifreli, strings.NewReader("eski-icerik"), a); err != nil {
		t.Fatal(err)
	}
	yeni, _, err := EtkinKur(ctx, o.db, false, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := AliciCoz(yeni.Alici)
	var yeniSifreli bytes.Buffer
	if _, err := Sifrele(&yeniSifreli, strings.NewReader("yeni-icerik"), b); err != nil {
		t.Fatal(err)
	}
	ad, metin, adet, err := KurtarmaAnahtarlari(ctx, o.db, time.Now())
	if err != nil || adet != 2 || !strings.HasPrefix(ad, "gosp-yedek-anahtar-") {
		t.Fatalf("kurtarma: %q %d %v", ad, adet, err)
	}
	for _, c := range []struct {
		veri *bytes.Buffer
		bek  string
	}{{&eskiSifreli, "eski-icerik"}, {&yeniSifreli, "yeni-icerik"}} {
		var cikti bytes.Buffer
		if err := Coz(&cikti, c.veri, strings.NewReader(metin)); err != nil || cikti.String() != c.bek {
			t.Errorf("kurtarma dosyası %q içeriğini çözemedi: %v", c.bek, err)
		}
	}
	if n := anahtarSay(t, o, "WHERE kurtarma_indirildi IS NOT NULL"); n != 2 {
		t.Errorf("indirme zamanı işlenmeli: %d", n)
	}
}

func TestKurtarmaAnahtarlari_AnahtarYoksaVeYalnizAcikAnahtar(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	anahtarSil(t, o)
	if _, _, _, err := KurtarmaAnahtarlari(ctx, o.db, time.Now()); !errors.Is(err, ErrAnahtarYok) {
		t.Fatalf("anahtarsız: %v", err)
	}
	u, _ := AnahtarCifti(false)
	if _, err := AnahtarIceAktar(ctx, o.db, u.Alici); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := KurtarmaAnahtarlari(ctx, o.db, time.Now()); !errors.Is(err, ErrAnahtarYok) {
		t.Fatalf("yalnız açık anahtar: sunucuda özel yok, indirilecek şey yok: %v", err)
	}
}

// topluKur — 2 domain × 10 oto yedek + elle (manuel) yedekler.
func topluKur(t *testing.T, o *ortam) (manuel []int64) {
	t.Helper()
	for d := int64(1); d <= 2; d++ {
		for g := 1; g <= 10; g++ {
			o.yedek(d, fmt.Sprintf("c_%d", d), fmt.Sprintf("202608%02d-020000", g), sahteIcerik(100*g, byte(g)), "oto", nil)
		}
		// müşteri/elle: tip=tam, job_id NULL
		manuel = append(manuel, o.yedek(d, fmt.Sprintf("c_%d", d), "20260899-020000", sahteIcerik(50, 9), "tam", nil))
	}
	return manuel
}

func TestTopluAdaylar_YalnizKokVeDomainBasinaSonN(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	manuel := topluKur(t, o)
	// toplu iş yedeği (job_id dolu) kökten sayılır
	jid := o.yedek(1, "c_1", "20260901-020000", sahteIcerik(10, 1), "tam", 77)
	ids, on, err := TopluAdaylar(context.Background(), o.db, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 6 || on.Adet != 6 || on.Domain != 2 || on.N != 3 || on.SaklamaN != 7 {
		t.Fatalf("domain başına 3 → 6: %v %+v", ids, on)
	}
	kume := map[int64]bool{}
	for i, id := range ids {
		kume[id] = true
		if i > 0 && ids[i-1] >= id {
			t.Errorf("eskiden yeniye sıralı olmalı: %v", ids)
		}
	}
	for _, m := range manuel {
		if kume[m] {
			t.Errorf("müşteri manuel yedeği toplu gönderime GİRMEZ: %d", m)
		}
	}
	if !kume[jid] {
		t.Error("kök toplu iş yedeği (job_id) aday olmalı")
	}
	var topla int64
	for _, id := range ids {
		var b int64
		_ = o.db.QueryRow(`SELECT boyut_b FROM backups WHERE id=?`, id).Scan(&b)
		topla += b
	}
	if topla != on.ToplamB || on.ToplamB == 0 {
		t.Errorf("boyut toplamı: %d != %d", topla, on.ToplamB)
	}
	for _, kotu := range []int{0, -1, TopluEnFazlaN + 1} {
		if _, _, err := TopluAdaylar(context.Background(), o.db, kotu); !errors.Is(err, ErrAyarGecersiz) {
			t.Errorf("n=%d reddedilmeli: %v", kotu, err)
		}
	}
}

func TestTopluKuyrugaAl_TekrarOlmazHataYenidenDenenir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	topluKur(t, o)
	ctx := context.Background()
	k, at, err := TopluKuyrugaAl(ctx, o.db, 7)
	if err != nil || k != 14 || at != 0 {
		t.Fatalf("ilk toplu: %d %d %v", k, at, err)
	}
	if n := g140say(t, o, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 14 {
		t.Fatalf("14 iş: %d", n)
	}
	// ikinci çağrı: hepsi kuyrukta → aday yok
	k, _, err = TopluKuyrugaAl(ctx, o.db, 7)
	if err != nil || k != 0 {
		t.Fatalf("tekrar: %d %v", k, err)
	}
	if n := g140say(t, o, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 14 {
		t.Fatalf("yinelenen iş açılmamalı: %d", n)
	}
	// kuyruk sırası kronolojik: en eski aday en küçük iş kimliği
	var ilkYedek, sonYedek int64
	_ = o.db.QueryRow(`SELECT backup_id FROM backup_uzak_kopyalar ORDER BY id ASC LIMIT 1`).Scan(&ilkYedek)
	_ = o.db.QueryRow(`SELECT backup_id FROM backup_uzak_kopyalar ORDER BY id DESC LIMIT 1`).Scan(&sonYedek)
	if ilkYedek >= sonYedek {
		t.Errorf("kuyruk eskiden yeniye: %d %d", ilkYedek, sonYedek)
	}
	// hata → yeniden aday ve yeniden bekliyor
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='hata', deneme=6 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, on, _ := TopluAdaylar(ctx, o.db, 7); on.Adet != 1 {
		t.Fatalf("hatalı iş yeniden aday: %+v", on)
	}
	k, _, _ = TopluKuyrugaAl(ctx, o.db, 7)
	if k != 1 || g140say(t, o, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE id=1 AND durum='bekliyor' AND deneme=0`) != 1 {
		t.Errorf("hata yeniden sıraya alınmalı: %d", k)
	}
}

func TestTopluKuyrugaAl_KapaliykenKuyrukOlusmaz(t *testing.T) {
	o := yeniOrtam(t)
	topluKur(t, o)
	if _, _, err := TopluKuyrugaAl(context.Background(), o.db, 7); !errors.Is(err, ErrHedefYok) {
		t.Fatalf("hedefsiz: %v", err)
	}
	o.ayar(func(a *Ayar) { a.Aktif = false })
	if _, _, err := TopluKuyrugaAl(context.Background(), o.db, 7); !errors.Is(err, ErrKapali) {
		t.Fatalf("kapalı: %v", err)
	}
	if n := g140say(t, o, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Errorf("kapalıyken iş oluşmamalı: %d", n)
	}
}

func TestYedekDurumlariVeGonderimOzeti(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	y1 := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(100, 1), "oto", nil)
	y2 := o.yedek(1, "c_a", "20260826-020000", sahteIcerik(100, 2), "oto", nil)
	y3 := o.yedek(1, "c_a", "20260827-020000", sahteIcerik(100, 3), "oto", nil)
	i1, i2 := o.kuyrukla(y1), o.kuyrukla(y2)
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='tamam', bitis_at=NOW() WHERE id=?`, i1); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='hata' WHERE id=?`, i2); err != nil {
		t.Fatal(err)
	}
	d, err := YedekDurumlari(context.Background(), o.db, []int64{y1, y2, y3})
	if err != nil || d[y1] != "tamam" || d[y2] != "hata" {
		t.Fatalf("durumlar: %v %v", d, err)
	}
	if _, var3 := d[y3]; var3 {
		t.Error("kuyruğa alınmamış yedek haritada olmamalı")
	}
	g, err := GonderimOzeti(context.Background(), o.db)
	if err != nil || g.Gonderilen != 1 || g.Hata != 1 || g.Bekleyen != 0 || g.SonGonderim == "" {
		t.Fatalf("özet: %+v %v", g, err)
	}
}

func g140say(t *testing.T, o *ortam, q string) int {
	t.Helper()
	var n int
	if err := o.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
