package uzak

// yerelsil_test.go — "Drive'a gönderilince yerel kopyayı sil". Her silme koşulu AYRI bir negatif
// testtir (koşul bozulunca dosya yerinde kalmalı); pozitif uçtan uca, yerel_tut, süpürme, ayar kapalı
// regresyonu, önizleme=çalıştırma, eşzamanlılık ve sızıntı testleri. Geçici MariaDB gerekir
// (GOSP_TEST_GECICI_DSN); canlı yedeğe/kuyruğa dokunulmaz, her şey t.TempDir() içindedir.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
)

func varMi(yol string) bool {
	_, err := os.Lstat(yol)
	return err == nil
}

func (o *ortam) yedekYol(sk, dosya string) string { return filepath.Join(o.kok, sk, dosya) }

func (o *ortam) dosyaAdi(yid int64) string {
	var d string
	if err := o.db.QueryRow(`SELECT dosya FROM backups WHERE id=?`, yid).Scan(&d); err != nil {
		o.t.Fatal(err)
	}
	return d
}

func (o *ortam) yerelYokMu(yid int64) bool {
	var n int
	if err := o.db.QueryRow(`SELECT yerel_yok IS NOT NULL FROM backups WHERE id=?`, yid).Scan(&n); err != nil {
		o.t.Fatal(err)
	}
	return n == 1
}

// gonder — kök yedek + kuyruk + işçi: doğrulanmış 'tamam' Drive kopyası.
func (o *ortam) gonder(domainID int64, sk, damga string) (yid, isid int64) {
	o.t.Helper()
	yid = o.yedek(domainID, sk, damga, sahteIcerik(40_000+int(domainID), byte(domainID)), "oto", nil)
	isid = o.kuyrukla(yid)
	o.calis()
	if d := o.is(isid); d.durum != DurumTamam {
		o.t.Fatalf("gönderim tamam olmalıydı: %s (%s)", d.durum, d.sonHata)
	}
	return yid, isid
}

func (o *ortam) temizle() (YerelSilSonuc, error) {
	o.t.Helper()
	return o.isci.YerelTemizle(context.Background())
}

// yerelOrtam — yerel silme KAPALI (gönderim sırasında dosyalar durur), tut=0.
func yerelOrtam(t *testing.T, f func(a *Ayar)) *ortam {
	t.Helper()
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) {
		a.YerelTut = 0
		if f != nil {
			f(a)
		}
	})
	return o
}

// ─── ayar ───────────────────────────────────────────────────────────────────

func TestYerelSilAyar_VarsayilanKapaliVeSinirlar(t *testing.T) {
	o := yeniOrtam(t)
	a, err := AyarOku(context.Background(), o.db)
	if err != nil {
		t.Fatal(err)
	}
	if a.YerelSil || a.YerelTut != 1 {
		t.Fatalf("varsayılan: yerel_sil KAPALI ve yerel_tut=1 olmalı: %+v", a)
	}
	for _, tut := range []int{-1, 11, 100} {
		a := Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "g", SaklamaN: 7, YerelTut: tut}
		if err := AyarYaz(context.Background(), o.db, a); !errors.Is(err, ErrAyarGecersiz) {
			t.Errorf("yerel_tut=%d reddedilmeliydi: %v", tut, err)
		}
	}
	for _, tut := range []int{0, 1, 10} {
		a := Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "g", SaklamaN: 7, YerelSil: true, YerelTut: tut}
		if err := AyarYaz(context.Background(), o.db, a); err != nil {
			t.Errorf("yerel_tut=%d kabul edilmeliydi: %v", tut, err)
		}
		g, _ := AyarOku(context.Background(), o.db)
		if !g.YerelSil || g.YerelTut != tut {
			t.Errorf("kayıt: %+v", g)
		}
	}
}

// ─── pozitif uçtan uca ──────────────────────────────────────────────────────

func TestYerelSil_UcTanUca_YuklenincesilinirKayitKalir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 0 })
	yid := o.yedek(1, "c_ornek", "20260825-233538", sahteIcerik(120_000, 3), "oto", nil)
	yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
	isid := o.kuyrukla(yid)
	o.calis()
	d := o.is(isid)
	if d.durum != DurumTamam {
		t.Fatalf("durum=%s %s", d.durum, d.sonHata)
	}
	if varMi(yol) {
		t.Fatal("Drive kopyası doğrulandı: yerel dosya silinmeliydi")
	}
	if ents, _ := filepath.Glob(yol + "*"); len(ents) != 0 {
		t.Fatalf("geçici .silinecek dosyası kaldı: %v", ents)
	}
	if !o.yerelYokMu(yid) {
		t.Error("backups.yerel_yok işaretlenmeliydi")
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE id=?`, yid).Scan(&n)
	if n != 1 {
		t.Error("backups satırı SİLİNMEMELİ")
	}
	if _, ok := o.bel.Icerik(d.uzakAd); !ok {
		t.Error("Drive kopyası durmalı")
	}
	if c := o.isci.YerelSayac(); c.Silinen != 1 || c.Hata != 0 {
		t.Errorf("sayaç: %+v", c)
	}
	// iş hâlâ 'tamam' ve ikinci koşu hiçbir şeyi bozmaz
	o.isci.YerelSupur(context.Background())
	if o.is(isid).durum != DurumTamam {
		t.Error("iş durumu bozuldu")
	}
}

func TestYerelSil_YerelTut_0_1_2(t *testing.T) {
	for _, tut := range []int{0, 1, 2} {
		o := yeniOrtam(t)
		o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, tut })
		var yids []int64
		for _, damga := range []string{"20260801-010101", "20260802-010101", "20260803-010101"} {
			y, _ := o.gonder(1, "c_ornek", damga)
			yids = append(yids, y)
		}
		kalan := 0
		for k, y := range yids {
			var sk = "c_ornek"
			var dvar = varMi(o.yedekYol(sk, o.dosyaAdi(y)))
			// en yeni tut yedek (sondaki indisler) kalmalı
			beklenen := k >= len(yids)-tut
			if dvar != beklenen {
				t.Errorf("tut=%d yedek#%d: dosya var=%v, beklenen=%v", tut, k, dvar, beklenen)
			}
			if o.yerelYokMu(y) == dvar {
				t.Errorf("tut=%d yedek#%d: yerel_yok işareti dosya durumuyla uyumsuz", tut, k)
			}
			if dvar {
				kalan++
			}
		}
		if kalan != tut {
			t.Errorf("tut=%d: %d dosya kaldı", tut, kalan)
		}
	}
}

func TestYerelSil_TutSayisiAlanAdiBasinadir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 1 })
	a1, _ := o.gonder(1, "c_bir", "20260801-010101")
	a2, _ := o.gonder(1, "c_bir", "20260802-010101")
	b1, _ := o.gonder(2, "c_iki", "20260801-020202")
	if varMi(o.yedekYol("c_bir", o.dosyaAdi(a1))) || !varMi(o.yedekYol("c_bir", o.dosyaAdi(a2))) {
		t.Error("c_bir: yalnız en yeni kalmalı")
	}
	if !varMi(o.yedekYol("c_iki", o.dosyaAdi(b1))) {
		t.Error("c_iki'nin tek yedeği başka alan adının yedekleriyle silinmemeli")
	}
}

// ─── ayar kapalı: mevcut davranış ───────────────────────────────────────────

func TestYerelSil_AyarKapaliHicbirSeySilinmez(t *testing.T) {
	o := yerelOrtam(t, nil) // yerel_sil=false, tut=0
	var yids []int64
	for _, damga := range []string{"20260801-010101", "20260802-010101", "20260803-010101"} {
		y, _ := o.gonder(1, "c_ornek", damga)
		yids = append(yids, y)
	}
	for k := 0; k < 3; k++ {
		o.isci.YerelSupur(context.Background())
		o.isci.Tetikle()
		o.calis()
	}
	for _, y := range yids {
		if !varMi(o.yedekYol("c_ornek", o.dosyaAdi(y))) || o.yerelYokMu(y) {
			t.Fatalf("yerel_sil kapalıyken yedek %d'ye dokunuldu", y)
		}
	}
	if c := o.isci.YerelSayac(); c.Silinen != 0 {
		t.Errorf("sayaç: %+v", c)
	}
}

func TestYerelSil_UzakKopyaKapaliyken_SupurmeSilmez(t *testing.T) {
	o := yerelOrtam(t, nil)
	y, _ := o.gonder(1, "c_ornek", "20260801-010101")
	a := Ayar{Aktif: false, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7, YerelSil: true, YerelTut: 0}
	if err := AyarYaz(context.Background(), o.db, a); err != nil {
		t.Fatal(err)
	}
	o.isci.YerelSupur(context.Background())
	if !varMi(o.yedekYol("c_ornek", o.dosyaAdi(y))) {
		t.Fatal("Drive gönderimi kapalıyken otomatik silme çalışmamalı")
	}
}

// ─── negatif: her silme koşulu ayrı ─────────────────────────────────────────

// negatif — doğrulanmış tek yedek kurulur, boz() ile BİR koşul bozulur, toplu temizlik (tut=0)
// çalıştırılır: dosya yerinde kalmalı, kayıt işaretlenmemeli, hiçbir şey silinmemeli.
func negatif(t *testing.T, ad string, boz func(o *ortam, yid, isid int64, yol string)) {
	t.Helper()
	t.Run(ad, func(t *testing.T) {
		o := yerelOrtam(t, nil)
		yid, isid := o.gonder(1, "c_ornek", "20260825-233538")
		yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
		boz(o, yid, isid, yol)
		s, err := o.temizle()
		if err != nil {
			t.Fatalf("temizle: %v", err)
		}
		if s.Silinen != 0 {
			t.Fatalf("hiçbir şey silinmemeliydi: %+v", s)
		}
		if _, err := os.Lstat(yol); err != nil {
			t.Fatalf("yerel dosya YERİNDE kalmalıydı: %v", err)
		}
		if o.yerelYokMu(yid) {
			t.Fatal("yerel_yok işaretlenmemeliydi")
		}
	})
}

// Kontrol: hiçbir koşul bozulmayınca aynı akış dosyayı SİLER (negatif testlerin anlamlı olduğunu kanıtlar).
func TestYerelSilNegatif_Kontrol(t *testing.T) {
	o := yerelOrtam(t, nil)
	yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
	yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
	s, err := o.temizle()
	if err != nil || s.Silinen != 1 || varMi(yol) || !o.yerelYokMu(yid) {
		t.Fatalf("koşullar tamamken silinmeliydi: %+v %v", s, err)
	}
}

func TestYerelSilNegatif(t *testing.T) {
	negatif(t, "uzak_hata", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='hata' WHERE id=?`, isid)
	})
	negatif(t, "uzak_bekliyor", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='bekliyor' WHERE id=?`, isid)
	})
	negatif(t, "uzak_yukleniyor", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='yukleniyor' WHERE id=?`, isid)
	})
	negatif(t, "dogrulama_kaydi_yok", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET son_dogrulama=NULL WHERE id=?`, isid)
	})
	negatif(t, "manifest_yazilmamis", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET manifest_ad='' WHERE id=?`, isid)
	})
	negatif(t, "manuel_yedek", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backups SET tip='tam', job_id=NULL WHERE id=?`, yid)
	})
	negatif(t, "hedef_erisilemez", func(o *ortam, yid, isid int64, yol string) {
		o.bel.HataVer("listele", hedef.YeniHata(hedef.KategoriAg, "listele", errors.New("ag yok")))
	})
	negatif(t, "hedef_acilamiyor", func(o *ortam, yid, isid int64, yol string) {
		o.isci.HedefAc = func(ctx context.Context, id int64) (hedef.Hedef, error) {
			return nil, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("jeton"))
		}
	})
	negatif(t, "drive_nesnesi_yok", func(o *ortam, yid, isid int64, yol string) {
		_ = o.bel.Sil(context.Background(), o.is(isid).uzakAd)
	})
	negatif(t, "drive_manifesti_yok", func(o *ortam, yid, isid int64, yol string) {
		_ = o.bel.Sil(context.Background(), o.is(isid).manifestAd)
	})
	negatif(t, "drive_boyutu_farkli", func(o *ortam, yid, isid int64, yol string) {
		o.bel.Koy(o.is(isid).uzakAd, []byte("kisa"))
	})
	negatif(t, "yerel_boyut_uyusmuyor", func(o *ortam, yid, isid int64, yol string) {
		f, _ := os.OpenFile(yol, os.O_APPEND|os.O_WRONLY, 0o600)
		_, _ = f.WriteString("EK-BAYTLAR")
		_ = f.Close()
		eski := time.Now().Add(-time.Hour)
		_ = os.Chtimes(yol, eski, eski)
	})
	negatif(t, "kayitli_boyut_uyusmuyor", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backups SET boyut_b=boyut_b+1 WHERE id=?`, yid)
	})
	negatif(t, "ozet_uyusmuyor", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET sha256_duz=REPEAT('0',64) WHERE id=?`, isid)
	})
	negatif(t, "dosya_cok_yeni", func(o *ortam, yid, isid int64, yol string) {
		now := time.Now()
		_ = os.Chtimes(yol, now, now)
	})
	negatif(t, "aktif_geri_yukleme_islemi", func(o *ortam, yid, isid int64, yol string) {
		o.isci.Mesgul = func(int64) bool { return true }
	})
	negatif(t, "geri_yukleme_isi_calisiyor", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`INSERT INTO backup_jobs (islem, durum) VALUES ('geri','calisiyor')`)
	})
	negatif(t, "baska_gonderim_suruyor", func(o *ortam, yid, isid int64, yol string) {
		if _, err := o.db.Exec(`INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('ikinci', 'bellek', '{}', '')`); err != nil {
			t.Fatal(err)
		}
		var h2 int64
		_ = o.db.QueryRow(`SELECT MAX(id) FROM backup_hedefler`).Scan(&h2)
		if _, err := o.db.Exec(`INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id, durum, yedek_dosya, sifreleme, anahtar_parmak)
			SELECT backup_id, domain_id, kaynak, ?, 'yukleniyor', yedek_dosya, sifreleme, anahtar_parmak FROM backup_uzak_kopyalar WHERE id=?`, h2, isid); err != nil {
			t.Fatal(err)
		}
	})
	negatif(t, "son_n_korumasi", func(o *ortam, yid, isid int64, yol string) {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopya_ayar SET yerel_tut=1 WHERE id=1`)
	})
}

func TestYerelSilNegatif_YolKacisi(t *testing.T) {
	t.Run("alan_dizini_sembolik_baglanti", func(t *testing.T) {
		o := yerelOrtam(t, nil)
		yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
		dosya := o.dosyaAdi(yid)
		dis := filepath.Join(t.TempDir(), "baska-yer")
		dir := filepath.Join(o.kok, "c_ornek")
		if err := os.Rename(dir, dis); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dis, dir); err != nil {
			t.Skip("symlink yok")
		}
		s, err := o.temizle()
		if err != nil || s.Silinen != 0 {
			t.Fatalf("silmemeliydi: %+v %v", s, err)
		}
		if !varMi(filepath.Join(dis, dosya)) {
			t.Fatal("sembolik bağlantının gösterdiği dosya silindi")
		}
	})
	t.Run("dosya_sembolik_baglanti", func(t *testing.T) {
		o := yerelOrtam(t, nil)
		yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
		yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
		hedefDosya := filepath.Join(t.TempDir(), "gercek.tar.gz")
		icerik, _ := os.ReadFile(yol)
		_ = os.WriteFile(hedefDosya, icerik, 0o600)
		_ = os.Remove(yol)
		if err := os.Symlink(hedefDosya, yol); err != nil {
			t.Skip("symlink yok")
		}
		s, err := o.temizle()
		if err != nil || s.Silinen != 0 {
			t.Fatalf("silmemeliydi: %+v %v", s, err)
		}
		if !varMi(hedefDosya) {
			t.Fatal("sembolik bağlantının gösterdiği dosya silindi")
		}
	})
	t.Run("yol_kacisi_dosya_adi", func(t *testing.T) {
		o := yerelOrtam(t, nil)
		yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
		kurban := filepath.Join(filepath.Dir(o.kok), "kurban.tar.gz")
		_ = os.WriteFile(kurban, []byte("DOKUNMA"), 0o600)
		_, _ = o.db.Exec(`UPDATE backups SET dosya='../kurban.tar.gz' WHERE id=?`, yid)
		s, err := o.temizle()
		if err != nil || s.Silinen != 0 {
			t.Fatalf("silmemeliydi: %+v %v", s, err)
		}
		if !varMi(kurban) {
			t.Fatal("kök dışındaki dosya silindi")
		}
	})
	t.Run("kullanici_adi_c_degil", func(t *testing.T) {
		o := yerelOrtam(t, nil)
		yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
		yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
		_, _ = o.db.Exec(`UPDATE domains SET sistem_kullanici='root' WHERE id=1`)
		s, _ := o.temizle()
		if s.Silinen != 0 || !varMi(yol) {
			t.Fatalf("silmemeliydi: %+v", s)
		}
	})
}

// ─── süpürme / yarım kalan ──────────────────────────────────────────────────

func TestYerelSil_SupurmeErtelenmisSilmeyiTamamlar(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 0 })
	o.isci.Mesgul = func(int64) bool { return true } // geri yükleme sürüyor: silme ertelenir
	yid, isid := o.gonder(1, "c_ornek", "20260825-233538")
	yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
	if o.is(isid).durum != DurumTamam {
		t.Fatal("silme ertelenince iş durumu BOZULMAMALI (tamam kalır)")
	}
	if !varMi(yol) || o.yerelYokMu(yid) {
		t.Fatal("kullanımdayken silinmemeli")
	}
	o.isci.Mesgul = func(int64) bool { return false }
	o.isci.YerelSupur(context.Background())
	if varMi(yol) || !o.yerelYokMu(yid) {
		t.Fatal("süpürme ertelenmiş silmeyi tamamlamalıydı")
	}
}

func TestYerelSil_SupurmeYokOlanDosyayiIsaretler(t *testing.T) {
	o := yerelOrtam(t, nil)
	yid, _ := o.gonder(1, "c_ornek", "20260825-233538")
	yol := o.yedekYol("c_ornek", o.dosyaAdi(yid))
	_ = os.Remove(yol) // dosya gitti ama işaret yazılamamıştı (yarım kalan silme)
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 0 })
	o.isci.YerelSupur(context.Background())
	if !o.yerelYokMu(yid) {
		t.Fatal("yarım kalan silme işaretlenmeliydi")
	}
}

func TestYerelSil_DriveKopyasiOlmayanYokDosyaIsaretlenmez(t *testing.T) {
	o := yerelOrtam(t, nil)
	y := o.yedek(1, "c_ornek", "20260825-233538", sahteIcerik(1000, 1), "oto", nil) // hiç gönderilmedi
	_ = os.Remove(o.yedekYol("c_ornek", o.dosyaAdi(y)))
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 0 })
	o.isci.YerelSupur(context.Background())
	if o.yerelYokMu(y) {
		t.Fatal("Drive kopyası olmayan kayıt 'yalnız Drive'da' diye işaretlenemez")
	}
}

func TestYerelSil_GonderilmemisYedekDokunulmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 0 })
	y := o.yedek(1, "c_ornek", "20260801-010101", sahteIcerik(2000, 2), "oto", nil) // eski planlı yedek, Drive'a hiç gitmedi
	o.isci.YerelSupur(context.Background())
	s, _ := o.temizle()
	if s.Silinen != 0 || !varMi(o.yedekYol("c_ornek", o.dosyaAdi(y))) {
		t.Fatalf("Drive'da olmayan yedek silinemez: %+v", s)
	}
}

// ─── önizleme = çalıştırma ──────────────────────────────────────────────────

func TestYerelTemizle_OnizlemeCalistirmaTutarli(t *testing.T) {
	o := yerelOrtam(t, func(a *Ayar) { a.YerelTut = 1 })
	// alan adı 1: 3 doğrulanmış → 2 silinecek, 1 tutulacak
	for _, d := range []string{"20260801-010101", "20260802-010101", "20260803-010101"} {
		o.gonder(1, "c_bir", d)
	}
	// alan adı 2: eski doğrulanmış + en yeni gönderilmemiş → eski silinecek, yeni Drive'da yok
	o.gonder(2, "c_iki", "20260801-020202")
	yeniGonderilmemis := o.yedek(2, "c_iki", "20260802-020202", sahteIcerik(3000, 9), "oto", nil)
	// alan adı 3: yalnız manuel yedek
	manuel := o.yedek(3, "c_uc", "20260801-030303", sahteIcerik(2500, 4), "tam", nil)
	// alan adı 4: Ağustos'tan kalma, Drive'a hiç gitmemiş planlı yedek
	eski := o.yedek(4, "c_dort", "20260801-040404", sahteIcerik(4000, 5), "planli", nil)

	on, err := o.isci.YerelOnizle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if on.Silinecek != 3 || on.Tutulan != 1 || on.DriveYok != 2 || on.Manuel != 1 || on.Domain != 2 || on.YerelTut != 1 {
		t.Fatalf("önizleme: %+v", on)
	}
	if on.SilinecekB <= 0 || on.DriveYokB <= 0 || len(on.AlanAdlari) != 2 {
		t.Fatalf("önizleme boyut/alan adı: %+v", on)
	}
	// önizleme hiçbir şeyi değiştirmedi
	var yy int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE yerel_yok IS NOT NULL`).Scan(&yy)
	if yy != 0 {
		t.Fatal("önizleme kayıt değiştirdi")
	}
	s, err := o.temizle()
	if err != nil {
		t.Fatal(err)
	}
	if s.Silinen != on.Silinecek || s.SilinenB != on.SilinecekB || s.Atlanan != 0 || s.Hata != 0 {
		t.Fatalf("çalıştırma önizlemeyle tutarsız: %+v vs %+v", s, on)
	}
	for _, y := range []int64{yeniGonderilmemis, manuel, eski} {
		if !varMi(o.yedekYol(map[int64]string{yeniGonderilmemis: "c_iki", manuel: "c_uc", eski: "c_dort"}[y], o.dosyaAdi(y))) {
			t.Errorf("yedek %d korunmalıydı", y)
		}
	}
	on2, _ := o.isci.YerelOnizle(context.Background())
	if on2.Silinecek != 0 {
		t.Errorf("ikinci önizleme boş olmalı: %+v", on2)
	}
	s2, _ := o.temizle()
	if s2.Silinen != 0 {
		t.Errorf("ikinci çalıştırma silmemeli: %+v", s2)
	}
}

// ─── eşzamanlılık ───────────────────────────────────────────────────────────

func TestYerelSil_EszamanliCiftSilmeYok(t *testing.T) {
	o := yerelOrtam(t, nil)
	for _, d := range []string{"20260801-010101", "20260802-010101", "20260803-010101"} {
		o.gonder(1, "c_bir", d)
		o.gonder(2, "c_iki", d)
	}
	o.ayar(func(a *Ayar) { a.YerelSil, a.YerelTut = true, 1 })
	var wg sync.WaitGroup
	var mu sync.Mutex
	toplam := 0
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := o.temizle()
			if err != nil {
				t.Errorf("temizle: %v", err)
				return
			}
			mu.Lock()
			toplam += s.Silinen
			mu.Unlock()
		}()
	}
	for k := 0; k < 3; k++ {
		wg.Add(2)
		go func() { defer wg.Done(); o.isci.YerelSupur(context.Background()) }()
		go func() { defer wg.Done(); o.isci.yerelOtomatik(context.Background(), 1) }()
	}
	wg.Wait()
	if c := o.isci.YerelSayac(); c.Silinen != 4 || c.Hata != 0 {
		t.Fatalf("her dosya TAM BİR kez silinmeliydi (4): sayaç %+v", c)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE yerel_yok IS NOT NULL`).Scan(&n)
	if n != 4 {
		t.Errorf("4 işaret beklenirdi: %d", n)
	}
	say := 0
	_ = filepath.Walk(o.kok, func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			say++
			if strings.Contains(p, silinecekEki) {
				t.Errorf("geçici dosya kaldı: %s", p)
			}
		}
		return nil
	})
	if say != 2 {
		t.Errorf("her alan adının en yeni yedeği kalmalıydı (2 dosya): %d", say)
	}
	_ = toplam
}

// ─── sızıntı ────────────────────────────────────────────────────────────────

func TestYerelSil_CiktilardaSirVeYolYok(t *testing.T) {
	o := yerelOrtam(t, func(a *Ayar) { a.YerelTut = 1 })
	o.gonder(1, "c_bir", "20260801-010101")
	o.gonder(1, "c_bir", "20260802-010101")
	on, _ := o.isci.YerelOnizle(context.Background())
	o.bel.HataVer("listele", hedef.YeniHata(hedef.KategoriAg, "listele", errors.New("ham sağlayıcı metni https://gizli.example/x")))
	s, _ := o.temizle() // hedef erişilemez: nedenli atlama
	if len(s.Nedenler) == 0 {
		t.Fatal("neden bekleniyordu")
	}
	b1, _ := json.Marshal(on)
	b2, _ := json.Marshal(s)
	b3, _ := json.Marshal(o.isci.YerelSayac())
	metin := string(b1) + string(b2) + string(b3)
	for _, yasak := range []string{o.kok, "/var/", ".tar.gz", "sha256", "manifest", "AGE-", ".age", "gizli.example", "ham sağlayıcı", o.ozel} {
		if yasak != "" && strings.Contains(metin, yasak) {
			t.Errorf("çıktıda yasak ifade: %q\n%s", yasak, metin)
		}
	}
}
