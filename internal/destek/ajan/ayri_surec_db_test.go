package ajan

// ayri_surec_db_test.go — ajan panelden AYRI süreç: panel↔ajan bağının DB üzerinden
// çalıştığının, nabzın, kilidin ve cihaz kimliği sürekliliğinin kanıtı.

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/gizli"
	"girginospanel/internal/sirdosya"
)

func TestVarsayilanYoklamaAraliklari(t *testing.T) {
	a := Yeni(nil, Yapilandirma{})
	if a.DBYoklamaAraligi <= 0 || a.DBYoklamaAraligi > 3*time.Second {
		t.Fatalf("DB yoklama aralığı ≤3 sn olmalı: %v", a.DBYoklamaAraligi)
	}
	if a.NabizAraligi <= 0 || a.NabizAraligi > 10*time.Second {
		t.Fatalf("nabız aralığı ≤10 sn olmalı: %v", a.NabizAraligi)
	}
}

// (c2) İptal yalnız DB'ye yazılır (panelde HTTP ucu YOK; iptal.Tek = eski panel ucunun DB işi)
// → ajan baglanti_iptal gönderir, damgalar, ikinci kez göndermez.
func TestDBIptaliHubaUlasir(t *testing.T) {
	o := kurOrtam(t, false, true)
	mA := o.eslestir(o.mA, 101, "musteri_a", "okuma")
	o.onayla(o.mA, mA)
	o.m.bosalt()
	o.a.mu.Lock()
	_, izVar := o.a.hesapIzi[mA]
	o.a.mu.Unlock()
	if !izVar {
		t.Fatal("ön koşul: etkin bağlantının hesap izi yok")
	}

	if !iptal.Tek(context.Background(), o.db, mustID(t, o, mA), p.IptalKullanici, "root") {
		t.Fatal("iptal yazılamadı")
	}
	g := o.m.bekle(p.TipBaglantiIptal, func(g p.Govde) bool { return g.(*p.BaglantiIptal).BaglantiID == mA }).(*p.BaglantiIptal)
	if g.Neden != p.IptalKullanici {
		t.Fatalf("neden %q", g.Neden)
	}
	var bildirildi *string
	for i := 0; i < 50; i++ {
		var s *string
		_ = o.db.QueryRow(`SELECT CAST(hub_bildirildi_at AS CHAR) FROM destek_baglantilari WHERE kimlik=?`, mA).Scan(&s)
		if s != nil {
			bildirildi = s
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if bildirildi == nil {
		t.Fatal("hub_bildirildi_at damgalanmadı")
	}
	// Tekrar gönderilmez (yoklama döngüsü çalışıyor ama damga var): hem biriken hem henüz okunmamış mesajlar.
	time.Sleep(500 * time.Millisecond)
	ikinci := func(g p.Govde) {
		if b, ok := g.(*p.BaglantiIptal); ok && b.BaglantiID == mA {
			t.Fatal("aynı iptal merkeze ikinci kez gönderildi")
		}
	}
	o.m.mu.Lock()
	for _, gm := range o.m.birikim {
		ikinci(gm.g)
	}
	o.m.mu.Unlock()
	for {
		select {
		case gm := <-o.m.gelen:
			ikinci(gm.g)
			continue
		default:
		}
		break
	}
	// İz temizliği: iptal edilen bağlantının hesap listesi özeti bellekten düşer.
	o.a.mu.Lock()
	_, kaldi := o.a.hesapIzi[mA]
	o.a.mu.Unlock()
	if kaldi {
		t.Fatal("iptal edilen bağlantının hesap izi temizlenmedi")
	}
}

// (c3) Kimlik olayı (parola/2FA/hosting silme → iptal.* tetikleri) panelde DB'ye yazılır, denetim
// kancası panelde çalışır; merkeze bildirimi ajan yapar.
func TestKimlikOlayiIptaliAjanYoluyla(t *testing.T) {
	o := kurOrtam(t, false, true)
	mA := o.eslestir(o.mA, 101, "musteri_a", "okuma")
	o.onayla(o.mA, mA)
	o.m.bosalt()
	if n := iptal.Domain(context.Background(), o.db, 42, p.IptalAlanSilme, 1, "root"); n != 1 {
		t.Fatalf("iptal %d", n)
	}
	o.m.bekle(p.TipBaglantiIptal, func(g p.Govde) bool {
		b := g.(*p.BaglantiIptal)
		return b.BaglantiID == mA && b.Neden == p.IptalAlanSilme
	})
	if o.denetimSay(dBaglantiIptal, "root") != 1 {
		t.Fatal("panel kancası denetimi yazmadı")
	}
}

// Merkeze bağlı değilken iptal damgalanmaz (bağlanınca bildirilir) ve ajan panik etmez.
func TestBagliDegilkenIptalDamgalanmaz(t *testing.T) {
	o := kurOrtam(t, false, false) // yerel acil kapatma → ajan merkeze bağlı değil
	if _, err := o.db.Exec(`INSERT INTO destek_baglantilari (kimlik, tg_user_id, rol, durum, iptal_nedeni, iptal_at)
		VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 5, 'kok', 'iptal', 'kullanici', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		o.a.iptalYokla(context.Background())
		time.Sleep(120 * time.Millisecond)
	}
	var s *string
	_ = o.db.QueryRow(`SELECT CAST(hub_bildirildi_at AS CHAR) FROM destek_baglantilari WHERE kimlik='AAAAAAAAAAAAAAAAAAAAAA'`).Scan(&s)
	if s != nil {
		t.Fatal("merkez oturumu yokken iptal bildirilmiş sayıldı")
	}
}

func mustID(t *testing.T, o *ortam, kimlik string) int64 {
	t.Helper()
	var id int64
	if err := o.db.QueryRow(`SELECT id FROM destek_baglantilari WHERE kimlik=?`, kimlik).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// (nabız) Ajan canlılığı DB nabzından okunur (ops: girginospanel-dogrula; panelde arayüz YOK).
func TestNabizOku(t *testing.T) {
	o := kurOrtam(t, false, true)
	bekleCanli := func(istenen bool) Nabiz {
		var son Nabiz
		for i := 0; i < 100; i++ {
			son = NabizOku(context.Background(), o.db)
			if son.Canli == istenen {
				return son
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("canli=%v olmadı: %+v", istenen, son)
		return son
	}
	var n Nabiz
	for i := 0; i < 100; i++ { // ilk nabız bağlanmadan önce yazılmış olabilir
		n = bekleCanli(true)
		if n.Durum.Durum == BagBagli {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n.Durum.Durum != BagBagli || !n.MerkezBagl {
		t.Fatalf("bağlı ajan nabzı: %+v", n)
	}
	// Temiz kapanış → hemen "ajan yok".
	o.a.Durdur(context.Background())
	if n = bekleCanli(false); n.MerkezBagl {
		t.Fatalf("durdurulmuş ajan merkez bağlı: %+v", n)
	}
	// Bayat nabız (ajan çöktü, Durdur çalışmadı).
	o.a.nabizYaz(context.Background())
	bekleCanli(true)
	_, _ = o.db.Exec(`UPDATE destek_ajan_nabiz SET nabiz_at = CURRENT_TIMESTAMP - INTERVAL 120 SECOND WHERE id=1`)
	bekleCanli(false)
}

// Nabız tablosu hiç yoksa (göç yok) panel patlamaz: ajan yok görünür.
func TestNabizTablosuYok(t *testing.T) {
	o := kurOrtam(t, false, false)
	_, _ = o.db.Exec(`DROP TABLE destek_ajan_nabiz`)
	if n := NabizOku(context.Background(), o.db); n.Canli {
		t.Fatal("tablo yokken canlı")
	}
}

// (e) Tek örnek kilidi.
func TestTekOrnekKilidi(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "run", "destek-ajan.lock")
	birinci, err := TekOrnekKilidi(yol)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TekOrnekKilidi(yol); err != ErrKilitTutuluyor {
		t.Fatalf("ikinci örnek reddedilmeli: %v", err)
	}
	st, _ := os.Stat(filepath.Dir(yol))
	if st.Mode().Perm() != 0o700 {
		t.Errorf("kilit dizini modu %v", st.Mode().Perm())
	}
	if fs, _ := os.Stat(yol); fs.Mode().Perm() != 0o600 {
		t.Errorf("kilit dosyası modu %v", fs.Mode().Perm())
	}
	// SÜREÇLER ARASI: başka bir süreç (util-linux flock) tutulan kilidi alamamalı.
	if _, err := exec.LookPath("flock"); err == nil {
		if err := exec.Command("flock", "-n", yol, "true").Run(); err == nil {
			t.Error("kilit tutulurken başka SÜREÇ kilidi aldı")
		}
	}
	birinci()
	üçüncü, err := TekOrnekKilidi(yol)
	if err != nil {
		t.Fatalf("kilit bırakılınca yeniden alınabilmeli: %v", err)
	}
	üçüncü()
	// Bayat (sahipsiz) kilit dosyası engel olmaz.
	if err := os.WriteFile(yol, []byte("99999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := TekOrnekKilidi(yol)
	if err != nil {
		t.Fatalf("bayat kilit dosyası engelledi: %v", err)
	}
	d()
}

func TestKilitSembolikBaglantiyiTakipEtmez(t *testing.T) {
	d := t.TempDir()
	hedef := filepath.Join(d, "hedef")
	if err := os.WriteFile(hedef, []byte("dokunma"), 0o600); err != nil {
		t.Fatal(err)
	}
	yol := filepath.Join(d, "k.lock")
	if err := os.Symlink(hedef, yol); err != nil {
		t.Skip("symlink yok")
	}
	if _, err := TekOrnekKilidi(yol); err == nil {
		t.Fatal("sembolik bağlantı üzerinden kilit alındı")
	}
	if b, _ := os.ReadFile(hedef); string(b) != "dokunma" {
		t.Fatal("hedef dosya bozuldu")
	}
}

// (d) Cihaz kimliği, ajan AYRI sürece taşınınca DEĞİŞMEZ: eski (gömülü) panel ajanının yazdığı
// biçimdeki dosya (gizli.SaklaBagli + db.key, 0600 root) yeni süreçte AYNI kimliği verir,
// dosya yeniden yazılmaz ve kenara alınmaz.
func TestCihazKimligiAyrilmadaDegismez(t *testing.T) {
	if st, err := os.Stat("/etc/girginospanel/db.key"); err != nil || st.Size() < 32 {
		t.Skip("db.key yok — gizli paketi test anahtarı üretirdi; bu test gerçek anahtarı yazmaz")
	}
	if VarsayilanCihazYolu != "/etc/girginospanel/destek-cihaz.key" || cihazBaglam != "destek-cihaz-v1" {
		t.Fatalf("cihaz anahtarı yolu/bağlamı DEĞİŞMİŞ (kimlik sürekliliği kırılır): %s %s", VarsayilanCihazYolu, cihazBaglam)
	}
	tohum := []byte("0123456789abcdef0123456789abcdef")
	beklenen, err := p.AnahtarCiftiTohumdan(tohum)
	if err != nil {
		t.Fatal(err)
	}
	yol := filepath.Join(t.TempDir(), "destek-cihaz.key")
	// ESKİ panel yazımı (cihazUret'in birebir eşi, ayrı kurulmuş): mühür + 0600.
	icerik := gizli.SaklaBagli(base64.RawURLEncoding.EncodeToString(tohum), "destek-cihaz-v1")
	if !strings.HasPrefix(icerik, "gos1:") {
		t.Skip("db.key ile mühürlenemedi")
	}
	if err := sirdosya.Yaz(yol, []byte(icerik+"\n"), 0o600, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	onceki, _ := os.ReadFile(yol)
	for tur := 0; tur < 3; tur++ { // "süreç yeniden başlatmaları"
		c, err := cihazAnahtariYukle(yol)
		if err != nil {
			t.Fatal(err)
		}
		if c.Kimlik != beklenen.Kimlik {
			t.Fatalf("tur %d: cihaz kimliği değişti: %s != %s", tur, c.Kimlik, beklenen.Kimlik)
		}
	}
	sonraki, _ := os.ReadFile(yol)
	if string(sonraki) != string(onceki) {
		t.Fatal("cihaz anahtarı dosyası yükleme sırasında yeniden yazıldı")
	}
	if _, err := os.Stat(yol + ".eski"); err == nil {
		t.Fatal("cihaz anahtarı kenara alındı (.eski) — kimlik değişirdi")
	}
}
