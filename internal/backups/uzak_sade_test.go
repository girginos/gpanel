package backups

// uzak_sade_test.go — "Plesk gibi sade" Drive akışı: ayar kaydında otomatik anahtar,
// kurtarma anahtarı indirme, mevcut yedekleri toplu gönder, satır rozetleri, regresyon.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func (o *uzakOrtam) ayarAc() (int, string) {
	return o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"gp","saklama_n":7}`)
}

func (o *uzakOrtam) denetimSay(alt string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, d := range o.denetim {
		if strings.Contains(d, alt) {
			n++
		}
	}
	return n
}

func TestSade_HedefSecilinceOtomatikAktifVeAnahtar(t *testing.T) {
	o := yeniUzakOrtam(t)
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari`); n != 0 {
		t.Fatalf("başlangıçta anahtar yok: %d", n)
	}
	kod, g := o.ayarAc()
	if kod != 200 {
		t.Fatalf("anahtarsız açılış artık hata vermez: %d %s", kod, g)
	}
	var d struct {
		Ayar         struct{ Aktif bool } `json:"ayar"`
		EtkinAnahtar *struct {
			Sifreleme string `json:"sifreleme"`
		} `json:"etkin_anahtar"`
		Kurtarma struct {
			Var       bool   `json:"var"`
			Indirildi string `json:"indirildi"`
		} `json:"kurtarma"`
	}
	if err := json.Unmarshal([]byte(g), &d); err != nil || !d.Ayar.Aktif || d.EtkinAnahtar == nil || d.EtkinAnahtar.Sifreleme != "x25519" || !d.Kurtarma.Var || d.Kurtarma.Indirildi != "" {
		t.Fatalf("durum: %s", g)
	}
	if o.denetimSay("anahtar_otomatik") != 1 {
		t.Errorf("otomatik üretim denetime yazılmalı: %v", o.denetim)
	}
	// ikinci kayıt: yeni anahtar yok, ikinci otomatik denetim yok
	if kod, _ := o.ayarAc(); kod != 200 {
		t.Fatal(kod)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari`); n != 1 {
		t.Errorf("tek anahtar: %d", n)
	}
	if o.denetimSay("anahtar_otomatik") != 1 {
		t.Error("ikinci kayıtta tekrar üretim denetimi olmamalı")
	}
}

func TestSade_KurtarmaAnahtariIndir(t *testing.T) {
	o := yeniUzakOrtam(t)
	var logTampon bytes.Buffer
	log.SetOutput(&logTampon)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if kod, _ := o.cagri("POST", "/uzak-kopya/kurtarma-anahtari", ""); kod != http.StatusConflict {
		t.Fatalf("anahtar yokken 409: %d", kod)
	}
	if kod, g := o.ayarAc(); kod != 200 {
		t.Fatal(kod, g)
	}
	req := httptest.NewRequest("POST", "/uzak-kopya/kurtarma-anahtari", nil)
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("indirme: %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control: %q", cc)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "gosp-yedek-anahtar-") {
		t.Errorf("Content-Disposition: %q", cd)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("başlıklar: %v", rec.Header())
	}
	govde := rec.Body.String()
	ozel := ozelAnahtarSatiri(govde)
	if ozel == "" {
		t.Fatal("dosyada özel anahtar yok")
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE kurtarma_indirildi IS NOT NULL`); n != 1 {
		t.Errorf("indirme zamanı işlenmeli: %d", n)
	}
	if o.denetimSay("kurtarma_indir") != 1 {
		t.Errorf("indirme denetime yazılmalı: %v", o.denetim)
	}
	// SIZINTI: özel anahtar yalnız bu yanıtta; denetimde, günlükte, diğer yanıtlarda yok
	_, durum := o.cagri("GET", "/uzak-kopya", "")
	if strings.Contains(durum, ozel) || strings.Contains(durum, "AGE-SECRET") || !strings.Contains(durum, `"indirildi":"20`) {
		t.Errorf("durum yanıtı: %s", durum)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, y := range o.yanitlar {
		if strings.Contains(y, ozel) {
			t.Error("başka bir yanıt özel anahtarı içeriyor")
		}
	}
	for _, dk := range o.denetim {
		if strings.Contains(dk, ozel) || strings.Contains(dk, "AGE-SECRET") {
			t.Errorf("denetim sızdırıyor: %s", dk)
		}
	}
	if strings.Contains(logTampon.String(), ozel) || strings.Contains(logTampon.String(), "AGE-SECRET") {
		t.Error("günlük özel anahtar içeriyor")
	}
}

func TestSade_AnahtarYenileHibritGelismis(t *testing.T) {
	o := yeniUzakOrtam(t)
	o.ayarAc()
	if kod, g := o.cagri("POST", "/uzak-kopya/anahtar-yenile", `{"hibrit":true}`); kod != 200 || !strings.Contains(g, `"sifreleme":"hibrit"`) {
		t.Fatalf("yenile: %d %s", kod, g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE durum='emekli'`); n != 1 {
		t.Errorf("eski anahtar emekli kalmalı: %d", n)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/anahtar-yenile", `{"x":1}`); kod != 400 {
		t.Errorf("bilinmeyen alan: %d", kod)
	}
}

func TestSade_MevcutlariGonder(t *testing.T) {
	o := yeniUzakOrtam(t)
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a'),(2,'b.example','c_b')`)
	for d := 1; d <= 2; d++ {
		for g := 1; g <= 9; g++ {
			g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (?, 'oto', ?, 1000)`, d, "x-2026080"+itoa(int64(g))+"-020000.tar.gz")
		}
		g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (?, 'tam', 'musteri-20260899-020000.tar.gz', 999999)`, d)
	}
	// hedef seçilmeden önizleme: hedef yok
	if kod, _ := o.cagri("GET", "/uzak-kopya/mevcutlari-gonder", ""); kod != http.StatusConflict {
		t.Errorf("hedefsiz önizleme 409: %d", kod)
	}
	// Drive kapalı kaydı (hedef seçili, aktif=false) → önizleme çalışır, gönderim kapalı
	o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":false,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"gp","saklama_n":7}`)
	kod, g := o.cagri("GET", "/uzak-kopya/mevcutlari-gonder?n=7", "")
	var on struct {
		Adet    int   `json:"adet"`
		Domain  int   `json:"domain"`
		ToplamB int64 `json:"toplam_b"`
	}
	_ = json.Unmarshal([]byte(g), &on)
	if kod != 200 || on.Adet != 14 || on.Domain != 2 || on.ToplamB != 14000 {
		t.Fatalf("önizleme (müşteri yedekleri HARİÇ, domain başına 7): %d %s", kod, g)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/mevcutlari-gonder", `{"n":7}`); kod != http.StatusConflict {
		t.Errorf("kapalıyken gönderim 409: %d", kod)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Fatalf("kapalıyken iş yok: %d", n)
	}
	o.ayarAc()
	if kod, _ := o.cagri("GET", "/uzak-kopya/mevcutlari-gonder?n=abc", ""); kod != 400 {
		t.Errorf("geçersiz n: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/mevcutlari-gonder", `{"n":101}`); kod != 400 {
		t.Errorf("n sınırı: %d", kod)
	}
	kod, g = o.cagri("POST", "/uzak-kopya/mevcutlari-gonder", `{"n":7}`)
	if kod != 202 || !strings.Contains(g, `"kuyruga":14`) {
		t.Fatalf("gönderim: %d %s", kod, g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar k JOIN backups b ON b.id=k.backup_id WHERE b.tip='tam'`); n != 0 {
		t.Errorf("müşteri yedeği kuyruğa girdi: %d", n)
	}
	if o.denetimSay("yedek.uzak.toplu") != 1 {
		t.Errorf("toplu gönderim denetimi: %v", o.denetim)
	}
	// tekrar: aday kalmaz
	if _, g := o.cagri("POST", "/uzak-kopya/mevcutlari-gonder", `{}`); !strings.Contains(g, `"kuyruga":0`) {
		t.Errorf("tekrar: %s", g)
	}
}

func TestSade_YedekDurumlariVeOzet(t *testing.T) {
	o := yeniUzakOrtam(t)
	o.ayarAc()
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a')`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, tip, dosya) VALUES (10,1,'oto','c_a-20260825-020000.tar.gz'),(11,1,'oto','c_a-20260826-020000.tar.gz'),(12,1,'tam','c_a-20260827-020000.tar.gz')`)
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != 202 {
		t.Fatal(kod)
	}
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='tamam', bitis_at=NOW()`)
	kod, g := o.cagri("GET", "/uzak-kopya/yedek-durumlari?ids=10,11,12,abc", "")
	var y struct {
		Aktif    bool              `json:"aktif"`
		Durumlar map[string]string `json:"durumlar"`
	}
	if err := json.Unmarshal([]byte(g), &y); err != nil || kod != 200 || !y.Aktif || y.Durumlar["10"] != "tamam" || len(y.Durumlar) != 1 {
		t.Fatalf("durumlar: %d %s", kod, g)
	}
	_, g = o.cagri("GET", "/uzak-kopya", "")
	if !strings.Contains(g, `"gonderilen":1`) || !strings.Contains(g, `"hata":0`) {
		t.Errorf("özet: %s", g)
	}
}

// Regresyon: Drive seçilmedikçe HİÇBİR şey değişmez.
func TestSade_DriveSecilmediysePasifKalir(t *testing.T) {
	o := yeniUzakOrtam(t)
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a')`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, tip, dosya) VALUES (10,1,'oto','c_a-20260825-020000.tar.gz')`)
	if kod, _ := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":false,"klasor":"gp","saklama_n":7}`); kod != 200 {
		t.Fatal(kod)
	}
	uzakKopyaKuyrugaAl(o.db, 10)
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari`); n != 0 {
		t.Errorf("Drive seçilmediyse anahtar üretilmez: %d", n)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Errorf("Drive seçilmediyse kuyruk oluşmaz: %d", n)
	}
	if o.denetimSay("anahtar_otomatik") != 0 {
		t.Error("denetim kaydı olmamalı")
	}
	_, g := o.cagri("GET", "/uzak-kopya/yedek-durumlari?ids=10", "")
	if !strings.Contains(g, `"aktif":false`) {
		t.Errorf("aktif false: %s", g)
	}
}
