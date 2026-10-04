package ajan

// ayri_surec_db_test.go — ajan panelden AYRI süreç: panel↔ajan bağının DB üzerinden
// çalıştığının, nabzın, kilidin ve cihaz kimliği sürekliliğinin kanıtı.

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/gizli"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sirdosya"

	"github.com/go-chi/chi/v5"
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

// (c1) Panelde ayar değişir → AJAN (ayrı süreç gibi: panel uçları ona hiç dokunmaz) DB yoklamasıyla
// görür ve hesap durumunu merkeze yeniden bildirir (etkin düzey düşer).
func TestAyarDegisiminiAjanYoklamasiGorur(t *testing.T) {
	o := kurOrtam(t, false, true)
	mA := o.eslestir(o.mA, 101, "musteri_a", "tam")
	o.onayla(o.mA, mA)
	o.m.bosalt()

	// Panel yalnız DB'ye yazar: çalışmayan (Baslat edilmemiş) bir "panel" örneğinden PUT.
	panel := paneliKur(t, o)
	basla := time.Now()
	if y := panel.istek("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "okuma"}); y.kod != 200 {
		t.Fatalf("panel PUT: %d %s", y.kod, y.ham)
	}
	d := o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool {
		b := g.(*p.BaglantiDurum)
		return b.BaglantiID == mA && b.Duzey == p.DuzeyOkuma
	}).(*p.BaglantiDurum)
	if gec := time.Since(basla); gec > 3*time.Second {
		t.Fatalf("ayar değişimi %v sonra bildirildi (yoklama aralığı 100 ms)", gec)
	}
	_ = d
	// Şalter KAPALI → ajan merkez bağlantısını keser (oturumlar merkezde kapanır).
	panel.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "kapali"})
	son := time.Now().Add(5 * time.Second)
	for o.a.Bagli() {
		if time.Now().After(son) {
			t.Fatal("şalter kapanınca ajan merkez bağlantısını kesmedi")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// (c2) Panel UI iptali (yalnız DB) → ajan baglanti_iptal gönderir, damgalar, ikinci kez göndermez.
func TestPanelIptaliHubaUlasir(t *testing.T) {
	o := kurOrtam(t, false, true)
	mA := o.eslestir(o.mA, 101, "musteri_a", "okuma")
	o.onayla(o.mA, mA)
	panel := paneliKur(t, o)
	o.m.bosalt()

	panel.istekOK("POST", "/destek/baglantilar/"+mA+"/iptal", o.kok, nil)
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
	// Tekrar gönderilmez (yoklama döngüsü çalışıyor ama damga var).
	time.Sleep(500 * time.Millisecond)
	o.m.mu.Lock()
	for _, gm := range o.m.birikim {
		if b, ok := gm.g.(*p.BaglantiIptal); ok && b.BaglantiID == mA {
			o.m.mu.Unlock()
			t.Fatal("aynı iptal merkeze ikinci kez gönderildi")
		}
	}
	o.m.mu.Unlock()
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
	o := kurOrtam(t, false, false) // modül kapalı → ajan merkeze bağlı değil
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

// panelRota: cmd/server'daki gibi RequireAuth altında yalnız UI uçları.
func panelRota(a *Ajan) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(dtSecret))
			a.Rotalar(r)
		})
	})
	return r
}

// panelOrtam: aynı DB'ye bakan, ajanı ÇALIŞTIRMAYAN panel örneği (cmd/server'ın yaptığı gibi).
type panelOrtam struct{ *ortam }

func paneliKur(t *testing.T, o *ortam) panelOrtam {
	t.Helper()
	panelAjan := Yeni(o.db, Yapilandirma{}) // Baslat YOK
	po := &ortam{t: t, db: o.db}
	po.h = panelRota(panelAjan)
	return panelOrtam{po}
}

// (nabız) Durum ucu ajan canlılığını DB nabzından okur.
func TestNabizVeDurumUcu(t *testing.T) {
	o := kurOrtam(t, false, true)
	panel := paneliKur(t, o)
	bekleCanli := func(istenen bool) map[string]any {
		var son yanit
		for i := 0; i < 100; i++ {
			son = panel.istek("GET", "/destek/durum", o.kok, nil)
			aj, _ := son.govde["ajan"].(map[string]any)
			if son.kod == 200 && aj != nil && aj["canli"] == istenen {
				return son.govde
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("ajan.canli=%v olmadı: %s", istenen, son.ham)
		return nil
	}
	var g map[string]any
	for i := 0; i < 100; i++ { // ilk nabız bağlanmadan önce yazılmış olabilir
		g = bekleCanli(true)
		if b, _ := g["baglanti"].(map[string]any); b != nil && b["durum"] == BagBagli {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if b, _ := g["baglanti"].(map[string]any); b == nil || b["durum"] != BagBagli {
		t.Fatalf("bağlı ajan durumu: %v", g["baglanti"])
	}
	if g["merkez_bagli"] != true {
		t.Fatal("merkez_bagli false")
	}
	// Temiz kapanış → hemen "ajan yok".
	o.a.Durdur(context.Background())
	g = bekleCanli(false)
	if b, _ := g["baglanti"].(map[string]any); b == nil || b["durum"] != BagAjanYok || g["merkez_bagli"] != false {
		t.Fatalf("durdurulmuş ajan: %v", g)
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
