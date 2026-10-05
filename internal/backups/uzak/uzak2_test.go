package uzak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
)

func TestMusteriManuelYedegiUzagaGitmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	manuel := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(1000, 1), "tam", nil) // müşteri/elle: tip=tam, job yok
	if _, _, err := KuyrugaAl(context.Background(), o.db, manuel); !errors.Is(err, ErrKokDegil) {
		t.Fatalf("manuel yedek ErrKokDegil: %v", err)
	}
	// toplu kök işi (job_id dolu) gider; eski bash 'planli' gider
	toplu := o.yedek(1, "c_a", "20260826-020000", sahteIcerik(1000, 2), "tam", 77)
	planli := o.yedek(1, "c_a", "20260827-020000", sahteIcerik(1000, 3), "planli", nil)
	o.kuyrukla(toplu)
	o.kuyrukla(planli)
	if n := say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 2 {
		t.Errorf("2 iş beklenirdi: %d", n)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, 999999); !errors.Is(err, ErrYedekYok) {
		t.Errorf("olmayan yedek (yerel başarısız) ErrYedekYok: %v", err)
	}
}

func TestKuyrugaAlIdempotent(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	y := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(1000, 1), "oto", nil)
	a, yeni1, _ := KuyrugaAl(context.Background(), o.db, y)
	b, yeni2, _ := KuyrugaAl(context.Background(), o.db, y)
	if a != b || !yeni1 || yeni2 {
		t.Errorf("aynı yedek iki satır açmamalı: %d %d %v %v", a, b, yeni1, yeni2)
	}
}

func TestAnahtarYoksaAcilamazVeKuyrukOlusmaz(t *testing.T) {
	o := yeniOrtam(t)
	if _, err := o.db.Exec(`UPDATE backup_sifre_anahtarlari SET durum='emekli'`); err != nil {
		t.Fatal(err)
	}
	a := Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7}
	if err := AyarYaz(context.Background(), o.db, a); !errors.Is(err, ErrAnahtarYok) {
		t.Fatalf("anahtarsız açılamaz: %v", err)
	}
}

func TestAyarDogrulama(t *testing.T) {
	o := yeniOrtam(t)
	kotu := []Ayar{
		{Aktif: true, HedefID: o.hedefID, Klasor: "../x", SaklamaN: 7},
		{Aktif: true, HedefID: o.hedefID, Klasor: "a/.gosp-yarim-1/b", SaklamaN: 7},
		{Aktif: true, HedefID: o.hedefID, Klasor: "ok", SaklamaN: 0},
		{Aktif: true, HedefID: o.hedefID, Klasor: "ok", SaklamaN: 7, SaklamaGun: -1},
		{Aktif: true, HedefID: o.hedefID, Klasor: "ok", SaklamaN: 7, BantKBps: -5},
	}
	for i, a := range kotu {
		if err := AyarYaz(context.Background(), o.db, a); !errors.Is(err, ErrAyarGecersiz) {
			t.Errorf("%d: geçersiz ayar reddedilmeli: %v", i, err)
		}
	}
	if err := AyarYaz(context.Background(), o.db, Ayar{Aktif: true, HedefID: 9999, Klasor: "ok", SaklamaN: 7}); !errors.Is(err, ErrHedefYok) {
		t.Errorf("olmayan hedef: %v", err)
	}
	if err := AyarYaz(context.Background(), o.db, Ayar{Aktif: true, Klasor: "ok", SaklamaN: 7}); !errors.Is(err, ErrHedefYok) {
		t.Errorf("hedefsiz açılamaz: %v", err)
	}
	if a, _ := AyarOku(context.Background(), o.db); a.Aktif {
		t.Error("varsayılan KAPALI olmalı")
	}
	// hedef silinince ayar kapanır (FK SET NULL → Aktif okunurken false)
	o.ayar(nil)
	if _, err := o.db.Exec(`DELETE FROM backup_hedefler WHERE id=?`, o.hedefID); err != nil {
		t.Fatal(err)
	}
	if a, _ := AyarOku(context.Background(), o.db); a.Aktif {
		t.Error("hedef silinince uzak kopya açık kalmamalı")
	}
}

func TestEszamanlilikBir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	o.sar.bekle = 15 * time.Millisecond
	for g := 1; g <= 6; g++ {
		o.kuyrukla(o.yedek(int64(g), "c_d"+string(rune('a'+g)), "20260825-020000", sahteIcerik(3000, byte(g)), "oto", nil))
	}
	var wg sync.WaitGroup
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				ok, err := o.isci.Bir(context.Background())
				if err != nil || !ok {
					return
				}
			}
		}()
	}
	wg.Wait()
	if o.sar.enCok != 1 {
		t.Fatalf("eşzamanlılık 1 olmalı, en çok %d", o.sar.enCok)
	}
	if n := say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE durum='tamam'`); n != 6 {
		t.Errorf("6 iş tamamlanmalı: %d", n)
	}
}

func TestBantSiniriHedefeIletilir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.BantKBps = 2048 })
	o.kuyrukla(o.yedek(1, "c_a", "20260825-020000", sahteIcerik(4000, 1), "oto", nil))
	o.calis()
	if len(o.sar.secimler) < 2 {
		t.Fatalf("yükleme çağrıları: %d", len(o.sar.secimler))
	}
	if s := o.sar.secimler[0]; s.BantKBps != 2048 || s.Boyut <= 0 || len(s.Sha256) != 64 {
		t.Errorf("veri yüklemesine bant sınırı + boyut + sha iletilmeli: %+v", s)
	}
}

func TestYerelBozukYedekGonderilmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	y := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(4000, 1), "oto", nil)
	id := o.kuyrukla(y)
	// yerel dosya kayıtlı özetten sonra bozuldu
	var dosya string
	_ = o.db.QueryRow(`SELECT dosya FROM backups WHERE id=?`, y).Scan(&dosya)
	if err := os.WriteFile(filepath.Join(o.kok, "c_a", dosya), sahteIcerik(4000, 99), 0o600); err != nil {
		t.Fatal(err)
	}
	o.calis()
	d := o.is(id)
	if d.durum != DurumHata || !strings.Contains(d.sonHata, "uyuşmuyor") {
		t.Fatalf("bozuk yerel yedek kalıcı hata: %+v", d)
	}
	if len(o.bel.Adlar()) != 0 {
		t.Error("bozuk yerel yedek uzağa YÜKLENMEMELİ")
	}
}

func TestYerelDosyaYoksaKaliciHata(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	y := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(4000, 1), "oto", nil)
	id := o.kuyrukla(y)
	_ = os.RemoveAll(filepath.Join(o.kok, "c_a"))
	o.calis()
	if d := o.is(id); d.durum != DurumHata || !strings.Contains(d.sonHata, "bulunamadı") {
		t.Fatalf("%+v", d)
	}
}

func TestIptal(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	id := o.kuyrukla(o.yedek(1, "c_a", "20260825-020000", sahteIcerik(4000, 1), "oto", nil))
	if err := o.isci.Iptal(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if o.is(id).durum != DurumIptal || o.calis() != 0 {
		t.Error("bekleyen iş iptal edilmeli ve işlenmemeli")
	}
	if err := YenidenDene(context.Background(), o.db, id); err != nil {
		t.Fatal(err)
	}
	// koşan işi iptal: yükleme sırasında
	o.sar.bekle = 300 * time.Millisecond
	bitti := make(chan struct{})
	go func() { o.calis(); close(bitti) }()
	time.Sleep(100 * time.Millisecond)
	if err := o.isci.Iptal(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	<-bitti
	if d := o.is(id); d.durum != DurumIptal {
		t.Fatalf("koşan iş iptal edilmeli: %+v", d)
	}
}

func TestIndirVeCoz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	duz := sahteIcerik(77_777, 5)
	id := o.kuyrukla(o.yedek(1, "c_a", "20260825-020000", duz, "oto", nil))
	o.calis()
	cikti := filepath.Join(t.TempDir(), "geri.tar.gz")
	yol, err := o.isci.IndirVeCoz(context.Background(), id, o.anahtarYol, cikti)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := os.ReadFile(yol); !bytes.Equal(g, duz) {
		t.Error("indirilip çözülen içerik özgün değil")
	}
	if _, err := o.isci.IndirVeCoz(context.Background(), id, o.anahtarYol, cikti); !errors.Is(err, ErrCiktiVar) {
		t.Errorf("var olan çıktı ezilmemeli: %v", err)
	}
	// uzak kopya bozulursa manifest özetiyle yakalanır
	d := o.is(id)
	o.bel.Boz(d.uzakAd)
	if _, err := o.isci.IndirVeCoz(context.Background(), id, o.anahtarYol, filepath.Join(t.TempDir(), "x")); err == nil || !strings.Contains(err.Error(), "bozuk") {
		t.Errorf("bozuk uzak kopya yakalanmalı: %v", err)
	}
}

func TestAnahtarAkislari(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	_, metin, b, err := AnahtarUret(ctx, o.db, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ozel := ozelSatir(metin)
	var gizliKol string
	_ = o.db.QueryRow(`SELECT gizli_anahtar FROM backup_sifre_anahtarlari WHERE parmak=?`, b.Parmak).Scan(&gizliKol)
	if gizliKol == "" || strings.Contains(gizliKol, ozel) || !strings.HasPrefix(gizliKol, "gos1:") {
		t.Errorf("özel anahtar DB'de şifreli olmalı")
	}
	if g := SirCoz(gizliKol, b.Parmak); g != ozel {
		t.Errorf("şifreli özel anahtar geri çözülmeli")
	}
	// onaylanmamış anahtar şifrelemede kullanılmaz
	if k, _ := EtkinAnahtar(ctx, o.db); k == nil || k.Parmak == b.Parmak {
		t.Error("onaylanmamış anahtar etkin sayılmamalı")
	}
	// ikinci üretim bekleyeni değiştirir
	_, _, b2, _ := AnahtarUret(ctx, o.db, false, time.Now())
	if n := say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE durum='bekliyor'`); n != 1 {
		t.Errorf("tek bekleyen anahtar: %d", n)
	}
	if err := AnahtarOnayla(ctx, o.db, b.Parmak, true); !errors.Is(err, ErrAnahtarBulunamadi) {
		t.Errorf("silinmiş bekleyen onaylanamaz: %v", err)
	}
	if err := AnahtarOnayla(ctx, o.db, b2.Parmak, false); err != nil {
		t.Fatal(err)
	}
	if k, _ := EtkinAnahtar(ctx, o.db); k == nil || k.Parmak != b2.Parmak {
		t.Error("yeni anahtar etkin olmalı")
	}
	if n := say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE durum='emekli'`); n != 1 {
		t.Errorf("eski etkin emekli olmalı: %d", n)
	}
	// içe aktar: geçersiz / geçerli / yinelenen
	if _, err := AnahtarIceAktar(ctx, o.db, "age1bozuk"); !errors.Is(err, ErrAliciGecersiz) {
		t.Errorf("geçersiz alıcı: %v", err)
	}
	u, _ := AnahtarCifti(true)
	ib, err := AnahtarIceAktar(ctx, o.db, u.Alici)
	if err != nil || ib.SunucudaOzel || ib.Sifreleme != SifrelemeHibrit {
		t.Fatalf("içe aktarma: %+v %v", ib, err)
	}
	if _, err := AnahtarIceAktar(ctx, o.db, u.Alici); !errors.Is(err, ErrAnahtarVar) {
		t.Errorf("yinelenen: %v", err)
	}
	// panel şifreleme anahtarı hazır değilse üretim reddedilir, hiçbir şey yazılmaz
	n0 := say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari`)
	SirHazir = func() error { return errors.New("yok") }
	if _, _, _, err := AnahtarUret(ctx, o.db, false, time.Now()); !errors.Is(err, ErrSifreHazirDegil) {
		t.Errorf("hazır değil: %v", err)
	}
	if say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari`) != n0 {
		t.Error("hazır değilken satır yazılmamalı")
	}
	SirHazir = func() error { return nil }
	SirSakla = func(d, _ string) string { return d } // düz metne düşen gerçek davranışın benzetimi
	if _, _, _, err := AnahtarUret(ctx, o.db, false, time.Now()); !errors.Is(err, ErrSifreHazirDegil) {
		t.Errorf("şifrelenmemiş saklama reddedilmeli: %v", err)
	}
}

func TestSizintiYokGunlukSatirManifestUzak(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	var gunluk bytes.Buffer
	log.SetOutput(&gunluk)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ozel := ozelSatir(o.ozel)
	if !strings.HasPrefix(ozel, "AGE-SECRET-KEY-1") {
		t.Fatal("test önkoşulu: özel anahtar bulunamadı")
	}
	// başarılı + başarısız + kalıcı hata yolları
	o.isci.MaksDeneme = 1
	o.kuyrukla(o.yedek(1, "c_a", "20260825-020000", sahteIcerik(3000, 1), "oto", nil))
	o.calis()
	o.bel.HataVer("yukle", hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("boom")))
	o.kuyrukla(o.yedek(1, "c_a", "20260826-020000", sahteIcerik(3000, 2), "oto", nil))
	o.calis()
	tara := func(ad, metin string) {
		if strings.Contains(metin, ozel) || strings.Contains(metin, strings.TrimPrefix(ozel, "AGE-SECRET-KEY-1")) {
			t.Errorf("%s ÖZEL ANAHTAR içeriyor", ad)
		}
	}
	tara("günlük", gunluk.String())
	rows, err := o.db.Query(`SELECT CONCAT_WS('|', durum, son_hata, hata_kategori, uzak_ad, manifest_ad, yedek_dosya, anahtar_parmak) FROM backup_uzak_kopyalar`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		tara("iş satırı", s)
	}
	rows.Close()
	for _, ad := range o.bel.Adlar() {
		v, _ := o.bel.Icerik(ad)
		tara("uzak:"+ad, string(v))
	}
	for _, u := range o.uyarilar {
		tara("bildirim", u)
	}
	liste, _, _ := IsListe(context.Background(), o.db, 1, 50, "")
	jb, _ := json.Marshal(liste)
	tara("iş listesi JSON", string(jb))
	ab, _ := Anahtarlar(context.Background(), o.db)
	jb, _ = json.Marshal(ab)
	tara("anahtar listesi JSON", string(jb))
}

func TestIsListeSayfalama(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	for g := 1; g <= 5; g++ {
		o.kuyrukla(o.yedek(int64(g), "c_s"+string(rune('a'+g)), "20260825-020000", sahteIcerik(100, byte(g)), "oto", nil))
	}
	l, top, err := IsListe(context.Background(), o.db, 2, 2, "")
	if err != nil || top != 5 || len(l) != 2 {
		t.Fatalf("sayfalama: %d %d %v", top, len(l), err)
	}
	if l[0].ID < l[1].ID {
		t.Error("yeniden eskiye sıralı olmalı")
	}
	if _, top, _ := IsListe(context.Background(), o.db, 1, 10, "tamam"); top != 0 {
		t.Errorf("durum süzgeci: %d", top)
	}
}

func TestSifreleHataDagitimiVeAkis(t *testing.T) {
	u, _ := AnahtarCifti(false)
	r, _ := AliciCoz(u.Alici)
	var dst bytes.Buffer
	duz := sahteIcerik(1<<20+17, 4)
	s, err := Sifrele(&dst, bytes.NewReader(duz), r)
	if err != nil || s.DuzBoyut != int64(len(duz)) || s.SifreliBoyut != int64(dst.Len()) {
		t.Fatalf("sonuç: %+v %v", s, err)
	}
	var o bytes.Buffer
	_, metin := KurtarmaDosyasi(u, time.Now())
	if err := Coz(&o, &dst, strings.NewReader(metin)); err != nil || !bytes.Equal(o.Bytes(), duz) {
		t.Fatalf("geri çözme: %v", err)
	}
	if err := Coz(&o, bytes.NewReader([]byte("age değil")), strings.NewReader(metin)); err == nil {
		t.Error("age olmayan girdi reddedilmeli")
	}
	if err := Coz(&o, bytes.NewReader(nil), strings.NewReader("anahtar yok")); !errors.Is(err, ErrAnahtarOkunamadi) {
		t.Errorf("anahtarsız dosya: %v", err)
	}
}

func TestManifestYazilamazsaTamamSayilmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	o.sar.manifestHata = hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("kesildi"))
	id := o.kuyrukla(o.yedek(1, "c_a", "20260825-020000", sahteIcerik(4000, 1), "oto", nil))
	o.calis()
	d := o.is(id)
	if d.durum != DurumBekliyor || d.manifestAd != "" || !strings.Contains(d.sonHata, "manifest") {
		t.Fatalf("manifestsiz kopya TAMAM sayılmamalı: %+v", d)
	}
	// manifest düzelince yeniden deneme aynı adı üzerine yazar ve tamamlar
	o.sar.manifestHata = nil
	_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET sonraki_deneme=NULL WHERE id=?`, id)
	o.calis()
	if d := o.is(id); d.durum != DurumTamam || d.manifestAd == "" {
		t.Fatalf("yeniden denemede tamamlanmalı: %+v", d)
	}
}
