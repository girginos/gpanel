package uzak

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
)

func ozelSatir(metin string) string {
	for _, s := range strings.Split(metin, "\n") {
		if strings.HasPrefix(s, "AGE-SECRET-KEY") {
			return s
		}
	}
	return ""
}

func sahteIcerik(n int, tohum byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31) ^ tohum
	}
	return b
}

func TestUcTanUca_SifreliKopyaDogrulanirVeCozulur(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	duz := sahteIcerik(300_000, 7)
	yid := o.yedek(1, "c_ornek", "20260825-233538", duz, "oto", nil)
	id := o.kuyrukla(yid)
	if n := o.calis(); n != 1 {
		t.Fatalf("1 iş işlenmeliydi: %d", n)
	}
	d := o.is(id)
	if d.durum != DurumTamam {
		t.Fatalf("durum=%s hata=%s", d.durum, d.sonHata)
	}
	if d.uzakAd != "gpanel/2026-08-25/c_ornek-20260825-233538.tar.gz.age" {
		t.Errorf("uzak ad: %s", d.uzakAd)
	}
	uzakVeri, ok := o.bel.Icerik(d.uzakAd)
	if !ok {
		t.Fatal("şifreli kopya uzakta yok")
	}
	// şifreli çıktı düz yedekle EŞİT DEĞİL ve düz içeriği barındırmaz
	if bytes.Equal(uzakVeri, duz) || bytes.Contains(uzakVeri, duz[:64]) {
		t.Fatal("uzaktaki kopya DÜZ metin")
	}
	if !bytes.HasPrefix(uzakVeri, []byte("age-encryption.org/")) {
		t.Errorf("age başlığı yok")
	}
	// manifest uzakta + sha'lar doğru
	mv, ok := o.bel.Icerik(d.manifestAd)
	if !ok || d.manifestAd != d.uzakAd+".manifest.json" {
		t.Fatalf("manifest yok: %q", d.manifestAd)
	}
	var m Manifest
	if err := json.Unmarshal(mv, &m); err != nil {
		t.Fatal(err)
	}
	hs := sha256.Sum256(uzakVeri)
	hd := sha256.Sum256(duz)
	if m.Sha256Sifreli != hex.EncodeToString(hs[:]) || m.Sha256Duz != hex.EncodeToString(hd[:]) {
		t.Errorf("manifest sha'ları yanlış")
	}
	if m.BoyutDuz != int64(len(duz)) || m.BoyutSifreli != int64(len(uzakVeri)) || m.YedekID != yid || m.KaynakSurum != "test-1" ||
		m.Sifreleme != SifrelemeX25519 || m.AgeSurum == "" || len(m.AliciParmak) != 64 {
		t.Errorf("manifest alanları: %+v", m)
	}
	if d.shaS != m.Sha256Sifreli || d.shaD != m.Sha256Duz {
		t.Errorf("DB özetleri manifestle aynı olmalı")
	}
	// doğru anahtarla çözülür
	cikti := filepath.Join(t.TempDir(), "cozulen.tar.gz")
	yerel := filepath.Join(t.TempDir(), "x.age")
	_ = os.WriteFile(yerel, uzakVeri, 0o600)
	if err := DosyaCoz(yerel, o.anahtarYol, cikti); err != nil {
		t.Fatalf("doğru anahtarla çözülmeli: %v", err)
	}
	if g, _ := os.ReadFile(cikti); !bytes.Equal(g, duz) {
		t.Error("çözülen içerik özgün değil")
	}
	// yanlış anahtarla çözülmez, çıktı dosyası BIRAKILMAZ
	yu, _ := AnahtarCifti(false)
	_, yanlis := KurtarmaDosyasi(yu, time.Now())
	yy := filepath.Join(t.TempDir(), "yanlis.txt")
	_ = os.WriteFile(yy, []byte(yanlis), 0o600)
	c2 := filepath.Join(t.TempDir(), "yanlis-cikti")
	if err := DosyaCoz(yerel, yy, c2); !errors.Is(err, ErrAnahtarUymuyor) {
		t.Errorf("yanlış anahtar ErrAnahtarUymuyor vermeli: %v", err)
	}
	if _, err := os.Stat(c2); err == nil {
		t.Error("yanlış anahtarda çıktı dosyası kalmamalı")
	}
	// geçici dosyalar temizlendi
	if ents, _ := os.ReadDir(o.gecici); len(ents) != 0 {
		t.Errorf("geçici dosya kaldı: %d", len(ents))
	}
}

func TestHibritAnahtarSifreler(t *testing.T) {
	o := yeniOrtam(t)
	_, metin, b, err := AnahtarUret(context.Background(), o.db, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !HibritAlici(b.Alici) || !strings.Contains(metin, "ML-KEM-768") || !strings.Contains(metin, "age sürümü") {
		t.Errorf("hibrit kurtarma dosyası sürüm bilgisi taşımalı")
	}
	if err := AnahtarOnayla(context.Background(), o.db, b.Parmak, true); err != nil {
		t.Fatal(err)
	}
	o.ayar(nil)
	yid := o.yedek(1, "c_ornek", "20260825-010101", sahteIcerik(50_000, 1), "oto", nil)
	id := o.kuyrukla(yid)
	o.calis()
	d := o.is(id)
	if d.durum != DurumTamam {
		t.Fatal(d.sonHata)
	}
	var sifreleme string
	_ = o.db.QueryRow(`SELECT sifreleme FROM backup_uzak_kopyalar WHERE id=?`, id).Scan(&sifreleme)
	if sifreleme != SifrelemeHibrit {
		t.Errorf("sifreleme=%s", sifreleme)
	}
	yy := filepath.Join(t.TempDir(), "h.txt")
	_ = os.WriteFile(yy, []byte(metin), 0o600)
	v, _ := o.bel.Icerik(d.uzakAd)
	yerel := filepath.Join(t.TempDir(), "h.age")
	_ = os.WriteFile(yerel, v, 0o600)
	if err := DosyaCoz(yerel, yy, filepath.Join(t.TempDir(), "o")); err != nil {
		t.Fatalf("hibrit çözme: %v", err)
	}
	// eski (klasik) anahtar hibrit kopyayı ÇÖZMEZ
	if err := DosyaCoz(yerel, o.anahtarYol, filepath.Join(t.TempDir(), "o2")); !errors.Is(err, ErrAnahtarUymuyor) {
		t.Errorf("klasik anahtar hibriti çözmemeli: %v", err)
	}
}

func TestBozukUzakIcerikAlgilanir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	// sağlayıcı sunucu özeti yok (Drive gibi) → geri indirip kendimiz doğrularız
	o.sar.yuklemeSonrasi = func(ad string) {
		if !hedef.GeciciMi(ad) && strings.HasSuffix(ad, ".age") {
			o.bel.Boz(ad)
		}
	}
	yid := o.yedek(1, "c_ornek", "20260825-010101", sahteIcerik(120_000, 3), "oto", nil)
	id := o.kuyrukla(yid)
	o.calis()
	d := o.is(id)
	if d.durum != DurumBekliyor || !strings.Contains(d.sonHata, "bozuk") || d.kat != "dogrulama" {
		t.Fatalf("bozuk kopya yakalanmalı ve yeniden denenmeli: %+v", d)
	}
	for _, ad := range o.bel.Adlar() {
		if strings.HasSuffix(ad, ".age") {
			t.Errorf("bozuk uzak kopya silinmeliydi: %s", ad)
		}
	}
	if d.manifestAd != "" {
		t.Error("doğrulanmamış kopya için manifest YAZILMAMALI")
	}
}

func TestUzakBoyutUyusmazligi(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	o.sar.yuklemeSonrasi = func(ad string) {
		if strings.HasSuffix(ad, ".age") {
			v, _ := o.bel.Icerik(ad)
			o.bel.Koy(ad, v[:len(v)-5])
		}
	}
	yid := o.yedek(1, "c_ornek", "20260825-010101", sahteIcerik(10_000, 3), "oto", nil)
	id := o.kuyrukla(yid)
	o.calis()
	if d := o.is(id); d.durum == DurumTamam || !strings.Contains(d.sonHata, "boyut") {
		t.Fatalf("kesik kopya tamam sayılmamalı: %+v", d)
	}
}

func TestSaklama_DomainBasinaSonN_YabanciVeManifestsizeDokunulmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.SaklamaN = 3 })
	// yabancı dosyalar: aynı klasörde, bizim değil
	o.bel.Koy("gpanel/2026-08-20/baska-kisinin.txt", []byte("dokunma"))
	o.bel.Koy("gpanel/2026-08-20/eski-yedek.tar.gz.age", []byte("manifestsiz yabancı"))
	o.bel.Koy("gpanel/notlar.txt", []byte("not"))
	var ids []int64
	for g := 1; g <= 5; g++ {
		yid := o.yedek(1, "c_a", fmt.Sprintf("2026082%d-020000", g), sahteIcerik(2000+g, byte(g)), "oto", nil)
		ids = append(ids, o.kuyrukla(yid))
		o.calis()
	}
	yb := o.yedek(2, "c_b", "20260821-020000", sahteIcerik(3000, 9), "oto", nil)
	idb := o.kuyrukla(yb)
	o.calis()
	// c_a: yalnız son 3 kalır
	for k, id := range ids {
		d := o.is(id)
		_, var1 := o.bel.Icerik(d.uzakAd)
		beklenenSilindi := k < 2
		if beklenenSilindi && (d.durum != DurumSilindi || var1) {
			t.Errorf("kopya %d silinmeliydi: durum=%s uzakta=%v", k, d.durum, var1)
		}
		if !beklenenSilindi && (d.durum != DurumTamam || !var1) {
			t.Errorf("kopya %d korunmalıydı: durum=%s uzakta=%v", k, d.durum, var1)
		}
		_, man := o.bel.Icerik(d.uzakAd + ".manifest.json")
		if man == beklenenSilindi {
			t.Errorf("kopya %d manifesti tutarsız (silindi=%v manifest=%v)", k, beklenenSilindi, man)
		}
	}
	// başka domain etkilenmez (N domain başına)
	if d := o.is(idb); d.durum != DurumTamam {
		t.Errorf("c_b korunmalı: %s", d.durum)
	}
	// yabancı dosyalara DOKUNULMADI
	for _, ad := range []string{"gpanel/2026-08-20/baska-kisinin.txt", "gpanel/2026-08-20/eski-yedek.tar.gz.age", "gpanel/notlar.txt"} {
		if _, ok := o.bel.Icerik(ad); !ok {
			t.Errorf("yabancı dosya silindi: %s", ad)
		}
	}
}

func TestSaklama_ManifestsizKopyaSilinmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.SaklamaN = 1 })
	y1 := o.yedek(1, "c_a", "20260821-020000", sahteIcerik(2000, 1), "oto", nil)
	i1 := o.kuyrukla(y1)
	o.calis()
	d1 := o.is(i1)
	// uzaktaki manifest kayboldu (kullanıcı sildi): kopya artık doğrulanmış değil
	if err := o.bel.Sil(context.Background(), d1.manifestAd); err != nil {
		t.Fatal(err)
	}
	y2 := o.yedek(1, "c_a", "20260822-020000", sahteIcerik(2001, 2), "oto", nil)
	o.kuyrukla(y2)
	o.calis()
	if _, ok := o.bel.Icerik(d1.uzakAd); !ok {
		t.Fatal("manifestsiz kopya silinmemeli")
	}
	if d := o.is(i1); d.durum != DurumTamam {
		t.Errorf("durum bozulmamalı: %s", d.durum)
	}
}

func TestSaklama_GunSiniriVeEnYeniKorunur(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.SaklamaN = 10; a.SaklamaGun = 5 })
	var ids []int64
	for g := 1; g <= 3; g++ {
		yid := o.yedek(1, "c_a", fmt.Sprintf("2026082%d-020000", g), sahteIcerik(1000+g, byte(g)), "oto", nil)
		ids = append(ids, o.kuyrukla(yid))
		o.calis()
	}
	// hepsini 30 gün öncesine it: en yenisi yine korunur
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET created_at = NOW() - INTERVAL 30 DAY`); err != nil {
		t.Fatal(err)
	}
	a, _ := AyarOku(context.Background(), o.db)
	adaylar, err := SaklamaAdaylari(context.Background(), o.db, a)
	if err != nil || len(adaylar) != 2 {
		t.Fatalf("2 eski kopya aday olmalı (en yeni korunur): %v %v", adaylar, err)
	}
	for _, c := range adaylar {
		if c.ID == ids[2] {
			t.Error("en yeni kopya aday olmamalı")
		}
		if !strings.Contains(c.Neden, "gün") {
			t.Errorf("neden: %s", c.Neden)
		}
	}
}

func TestSaklamaOnizleme_UzagaDokunmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(func(a *Ayar) { a.SaklamaN = 1 })
	for g := 1; g <= 3; g++ {
		yid := o.yedek(1, "c_a", fmt.Sprintf("2026082%d-020000", g), sahteIcerik(1000+g, byte(g)), "oto", nil)
		o.kuyrukla(yid)
		o.ayar(func(a *Ayar) { a.SaklamaN = 3 })
		o.calis()
		o.ayar(func(a *Ayar) { a.SaklamaN = 1 })
	}
	// N=3 iken hiçbiri silinmedi; şimdi N=1: önizleme 2 aday gösterir ama SİLMEZ
	a, _ := AyarOku(context.Background(), o.db)
	sil0 := o.bel.Cagri("sil")
	ad, err := SaklamaUygula(context.Background(), o.db, o.sar, a, true)
	if err != nil || len(ad) != 2 {
		t.Fatalf("önizleme 2 aday vermeli: %d %v", len(ad), err)
	}
	if o.bel.Cagri("sil") != sil0 || len(o.bel.Adlar()) != 6 {
		t.Error("önizleme uzağa dokunmamalı")
	}
}

func TestYarimKalanIsDevamEder(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	yid := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(5000, 1), "oto", nil)
	id := o.kuyrukla(yid)
	// panel yükleme sırasında öldü
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum='yukleniyor' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if n := o.calis(); n != 0 {
		t.Fatal("yarım iş sıfırlanmadan alınmamalı")
	}
	n, err := o.isci.YarimlariSifirla(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("sıfırlama: %d %v", n, err)
	}
	o.calis()
	if d := o.is(id); d.durum != DurumTamam {
		t.Fatalf("devam etmeliydi: %+v", d)
	}
}

func TestKaliciHataVeUstelGeriCekilme(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	o.isci.MaksDeneme = 3
	yid := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(5000, 1), "oto", nil)
	id := o.kuyrukla(yid)
	o.bel.HataVer("yukle", hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("zaman aşımı")))
	o.calis()
	d := o.is(id)
	if d.durum != DurumBekliyor || d.deneme != 1 || d.kat != "ag" {
		t.Fatalf("geçici hata: bekliyor/1 beklenirdi: %+v", d)
	}
	var gelecek int
	_ = o.db.QueryRow(`SELECT sonraki_deneme > NOW() FROM backup_uzak_kopyalar WHERE id=?`, id).Scan(&gelecek)
	if gelecek != 1 {
		t.Error("sonraki deneme gelecekte olmalı (geri çekilme)")
	}
	if o.calis() != 0 {
		t.Error("geri çekilme bitmeden iş alınmamalı")
	}
	// iki deneme daha → kalıcı hata + bildirim
	for k := 0; k < 2; k++ {
		_, _ = o.db.Exec(`UPDATE backup_uzak_kopyalar SET sonraki_deneme = NOW() - INTERVAL 1 MINUTE WHERE id=?`, id)
		o.calis()
	}
	d = o.is(id)
	if d.durum != DurumHata || d.deneme != 3 {
		t.Fatalf("3. denemede kalıcı hata: %+v", d)
	}
	if len(o.uyarilar) != 1 || !strings.Contains(o.uyarilar[0], "gönderilemedi") {
		t.Errorf("kalıcı hatada tek bildirim: %v", o.uyarilar)
	}
	// yeniden dene → sıfırlanır, hata giderilince tamam
	o.bel.HataVer("yukle", nil)
	if err := YenidenDene(context.Background(), o.db, id); err != nil {
		t.Fatal(err)
	}
	o.calis()
	if d := o.is(id); d.durum != DurumTamam || d.deneme != 0 {
		t.Fatalf("yeniden deneme: %+v", d)
	}
	if err := YenidenDene(context.Background(), o.db, id); !errors.Is(err, ErrDurumUygunDegil) {
		t.Errorf("tamam iş yeniden denenemez: %v", err)
	}
}

func TestKimlikHatasiHemenKalici(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	yid := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(1000, 1), "oto", nil)
	id := o.kuyrukla(yid)
	o.bel.HataVer("yukle", hedef.YeniHata(hedef.KategoriKimlik, "yukle", errors.New("401")))
	o.calis()
	if d := o.is(id); d.durum != DurumHata || d.kat != "kimlik" {
		t.Fatalf("kimlik hatası beklemeden kalıcı olmalı: %+v", d)
	}
}

func TestKapaliykenHicbirSeyOlmaz(t *testing.T) {
	o := yeniOrtam(t) // ayar varsayılan: KAPALI
	yid := o.yedek(1, "c_a", "20260825-020000", sahteIcerik(1000, 1), "oto", nil)
	if _, _, err := KuyrugaAl(context.Background(), o.db, yid); !errors.Is(err, ErrKapali) {
		t.Fatalf("kapalıyken ErrKapali: %v", err)
	}
	if n := say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Errorf("kapalıyken iş oluşmamalı: %d", n)
	}
	// açıkken kuyruğa alınmış iş, kapatılınca işlenmez
	o.ayar(nil)
	id := o.kuyrukla(yid)
	o.ayar(func(a *Ayar) { a.Aktif = false })
	if o.calis() != 0 || o.is(id).durum != DurumBekliyor || len(o.bel.Adlar()) != 0 {
		t.Error("kapalıyken iş işlenmemeli")
	}
}

func say(t *testing.T, db *sql.DB, q string, a ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
