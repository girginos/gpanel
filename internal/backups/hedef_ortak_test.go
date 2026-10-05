package backups

// hedef_ortak_test.go — ORTAK (GirginOS'a ait) Google OAuth istemcisi modeli:
// istemci verilmeden Drive hedefi eklenir, secret satıra YAZILMAZ, kimlik
// çalışma anında ortak kaynaktan çözülür; ortak yoksa 409; dosya izni gevşekse
// reddedilir; ortak kimlik değişirse "yeniden yetkilendirme gerekli"; hiçbir
// yanıt/günlük/denetimde secret yok. Geçici MariaDB gerekir (yoksa ATLANIR).

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/gdrive"
)

const tOrtakBaskaID = "baska-ortak-999.apps.googleusercontent.com"

// ortakOrtam — gömülü değerleri temizler, ortak dosyayı geçici dizine koyar.
// Not: istemci kimliği/secret testteki sahte sabitlerdir.
func ortakOrtam(t *testing.T, o *hedefOrtam, id string, mod os.FileMode) string {
	t.Helper()
	eskiID, eskiSecret, eskiYol := gdrive.GomuluClientID, gdrive.GomuluClientSecret, gdrive.OrtakDosyaYolu
	t.Cleanup(func() {
		gdrive.GomuluClientID, gdrive.GomuluClientSecret, gdrive.OrtakDosyaYolu = eskiID, eskiSecret, eskiYol
	})
	gdrive.GomuluClientID, gdrive.GomuluClientSecret = "", ""
	yol := filepath.Join(t.TempDir(), "gdrive-istemci.json")
	gdrive.OrtakDosyaYolu = yol
	if id != "" {
		ortakDosyaYaz(t, yol, id, mod)
	}
	// Ortak satırlar için kurucu gerçek çözümü (gdrive.Yeni) kullanır; ağ açılmaz.
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	driveHedefKur = func(ayar json.RawMessage, sirlar map[string]string, kaydet func(string)) (hedef.Hedef, error) {
		if _, err := gdrive.Yeni(ayar, sirlar, hedef.KurSecenek{}, gdrive.Secenek{YenilemeKaydet: kaydet}); err != nil {
			return nil, err
		}
		return hedefTestHedefi{o.s}, nil
	}
	return yol
}

func ortakDosyaYaz(t *testing.T, yol, id string, mod os.FileMode) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"client_id": id, "client_secret": tSecret})
	if err := os.WriteFile(yol, b, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(yol, mod); err != nil {
		t.Fatal(err)
	}
}

func (o *hedefOrtam) ortakEkle() (int64, HedefOzet) {
	o.t.Helper()
	kod, g := o.cagri("POST", "/hedefler/drive", `{"ad":"Google Drive"}`)
	if kod != http.StatusCreated {
		o.t.Fatalf("ortak ekle: %d %s", kod, g)
	}
	var oz HedefOzet
	_ = json.Unmarshal([]byte(g), &oz)
	return oz.ID, oz
}

func (o *hedefOrtam) listeOzet(id int64) HedefOzet {
	o.t.Helper()
	_, l := o.cagri("GET", "/hedefler", "")
	var liste []HedefOzet
	if err := json.Unmarshal([]byte(l), &liste); err != nil {
		o.t.Fatalf("liste: %s", l)
	}
	for _, x := range liste {
		if x.ID == id {
			return x
		}
	}
	o.t.Fatalf("hedef %d listede yok: %s", id, l)
	return HedefOzet{}
}

func ortakYetkilendir(t *testing.T, o *hedefOrtam, id int64) {
	t.Helper()
	yol := "/hedefler/" + itoa(id)
	if kod, g := o.cagri("POST", yol+"/yetki-baslat", ""); kod != 200 || !strings.Contains(g, "ABCD-EFGH") {
		t.Fatalf("yetki-baslat: %d %s", kod, g)
	}
	o.s.mu.Lock()
	o.s.cihazDurum = gdrive.SorguTamam
	o.s.mu.Unlock()
	if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"durum":"tamam"`) {
		t.Fatalf("yetki-sorgu: %s", g)
	}
}

func TestOrtakIstemciYokken409(t *testing.T) {
	o := yeniHedefOrtam(t)
	ortakOrtam(t, o, "", 0)
	if httpGetOrtak(o) {
		t.Fatal("ortak yokken ortak_istemci true")
	}
	kod, g := o.cagri("POST", "/hedefler/drive", `{"ad":"Google Drive"}`)
	if kod != http.StatusConflict || !strings.Contains(g, "Ortak Google istemcisi bu sürümde yapılandırılmamış") || !strings.Contains(g, "Gelişmiş") {
		t.Fatalf("ortak yoksa 409 + net mesaj bekleniyordu: %d %s", kod, g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 0 {
		t.Fatalf("ortak yokken satır oluştu: %d", n)
	}
	// Yalnız biri verilirse 400 (yarım istemci).
	if kod, _ := o.cagri("POST", "/hedefler/drive", `{"ad":"X","client_id":"`+tClientID+`"}`); kod != http.StatusBadRequest {
		t.Errorf("yalnız client_id 400 olmalı: %d", kod)
	}
	// Kendi istemcisi hâlâ çalışır (ortak olmasa da).
	if id := o.ekle(); id == 0 {
		t.Fatal("kendi istemci akışı bozuldu")
	}
}

func httpGetOrtak(o *hedefOrtam) bool {
	_, g := o.cagri("GET", "/hedefler/ortak-istemci", "")
	var m struct {
		Ortak bool `json:"ortak_istemci"`
	}
	_ = json.Unmarshal([]byte(g), &m)
	return m.Ortak
}

func TestOrtakEkleSecretSatiraYazilmazVeUcUcaAkis(t *testing.T) {
	var logTampon bytes.Buffer
	eskiOut := log.Writer()
	log.SetOutput(&logTampon)
	t.Cleanup(func() { log.SetOutput(eskiOut) })

	o := yeniHedefOrtam(t)
	ortakOrtam(t, o, tClientID, 0o600)
	if !httpGetOrtak(o) {
		t.Fatal("ortak dosya varken ortak_istemci false")
	}
	id, oz := o.ortakEkle()
	if oz.Istemci != "ortak" || oz.ClientID == tClientID || !strings.HasPrefix(tClientID, strings.TrimSuffix(oz.ClientID, "…")) {
		t.Fatalf("ortak özet: istemci=%q client_id=%q (maskeli olmalı)", oz.Istemci, oz.ClientID)
	}
	// Satır: ayar'da secret yok + istemci=ortak; sirlar'da client_secret YOK.
	var ayar string
	_ = o.db.QueryRow(`SELECT ayar FROM backup_hedefler WHERE id=?`, id).Scan(&ayar)
	if strings.Contains(ayar, tSecret) || !strings.Contains(ayar, `"istemci":"ortak"`) {
		t.Fatalf("ayar: %s", ayar)
	}
	s, _ := hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	m, err := s.sirlariAc()
	if err != nil || m[gdrive.SirClientSecret] != "" || strings.Contains(o.dbSirlar(id), tSecret) {
		t.Fatalf("ortak secret satıra yazıldı: %v %v", m, err)
	}
	if g := o.listeOzet(id); g.Durum != hedefDurumYetkiYok || g.YetkiVar {
		t.Fatalf("yeni ortak hedef yetki-yok olmalı: %+v", g)
	}

	// Uçtan uca yetki: sahte OAuth, kimlik ortak kaynaktan (stub secret'ı doğrular).
	ortakYetkilendir(t, o, id)
	if g := o.listeOzet(id); g.Durum != hedefDurumBagli || !g.YetkiVar {
		t.Fatalf("yetkilenince bağlı olmalı: %+v", g)
	}
	s, _ = hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	if m, _ := s.sirlariAc(); m[gdrive.SirYenileme] != tYenileme || m[gdrive.SirClientSecret] != "" {
		t.Fatalf("yalnız refresh token saklanmalı: %v", m)
	}
	if _, g := o.cagri("POST", "/hedefler/"+itoa(id)+"/test", ""); !strings.Contains(g, `"ok":true`) {
		t.Fatalf("test: %s", g)
	}
	// Bağlantıyı kes: Google iptali ortak secret'la, sonra jeton silinir.
	if _, g := o.cagri("POST", "/hedefler/"+itoa(id)+"/baglantiyi-kes", ""); !strings.Contains(g, `"google_iptal":true`) {
		t.Fatalf("kes: %s", g)
	}
	if len(o.s.iptalEdilen) != 1 {
		t.Fatalf("jeton iptal edilmedi: %v", o.s.iptalEdilen)
	}
	// SIZINTI: yanıt + denetim + günlükte ne secret ne tam client_id.
	o.sirYok(tClientID)
	if l := logTampon.String(); strings.Contains(l, tSecret) || strings.Contains(l, tYenileme) || strings.Contains(l, tClientID) {
		t.Errorf("günlüğe sır sızdı: %.200s", l)
	}
}

func TestOrtakDosyaIzni0644Reddedilir(t *testing.T) {
	o := yeniHedefOrtam(t)
	ortakOrtam(t, o, tClientID, 0o644)
	if httpGetOrtak(o) {
		t.Fatal("güvensiz izinli dosya ortak_istemci=true döndürdü")
	}
	kod, g := o.cagri("POST", "/hedefler/drive", `{"ad":"Google Drive"}`)
	if kod != http.StatusConflict || !strings.Contains(g, "izni güvensiz") {
		t.Fatalf("0644 reddedilip açık hata verilmeli: %d %s", kod, g)
	}
	if strings.Contains(g, tSecret) || strings.Contains(g, tClientID) {
		t.Errorf("hata yanıtına değer sızdı: %s", g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 0 {
		t.Fatalf("satır oluştu: %d", n)
	}
}

func TestOrtakKimlikDegisirseYenidenYetkilendirme(t *testing.T) {
	o := yeniHedefOrtam(t)
	yol := ortakOrtam(t, o, tClientID, 0o600)
	id, _ := o.ortakEkle()
	ortakYetkilendir(t, o, id)

	// Ortak secret değişti (aynı client_id): jeton çalışmaya devam eder.
	if g := o.listeOzet(id); g.Durum != hedefDurumBagli {
		t.Fatalf("bağlı olmalı: %+v", g)
	}
	// Ortak client_id değişti: sessiz hata değil, yetki-gerekli + kimlik.
	ortakDosyaYaz(t, yol, tOrtakBaskaID, 0o600)
	g := o.listeOzet(id)
	if g.Durum != hedefDurumYetkiGerekli || g.HataKategori != "kimlik" || g.HataAciklama == "" {
		t.Fatalf("client_id değişince yetki-gerekli beklenirdi: %+v", g)
	}
	if _, y := o.cagri("POST", "/hedefler/"+itoa(id)+"/test", ""); !strings.Contains(y, `"yetki_gerekli":true`) || !strings.Contains(y, `"kategori":"kimlik"`) {
		t.Fatalf("test yetki_gerekli demeli: %s", y)
	}
	// Yeniden yetkilendirme: yeni kimlik satıra sabitlenir, bağlı olur.
	ortakYetkilendir(t, o, id)
	if g := o.listeOzet(id); g.Durum != hedefDurumBagli || g.HataKategori != "" {
		t.Fatalf("yeniden yetkiden sonra bağlı olmalı: %+v", g)
	}
	var ayar string
	_ = o.db.QueryRow(`SELECT ayar FROM backup_hedefler WHERE id=?`, id).Scan(&ayar)
	if !strings.Contains(ayar, tOrtakBaskaID) || !strings.Contains(ayar, `"istemci":"ortak"`) {
		t.Fatalf("yeni ortak kimlik satıra sabitlenmedi: %s", ayar)
	}
	o.sirYok()
}

func TestOrtakKaybolursaMevcutHedefAcikHata(t *testing.T) {
	o := yeniHedefOrtam(t)
	yol := ortakOrtam(t, o, tClientID, 0o600)
	id, _ := o.ortakEkle()
	ortakYetkilendir(t, o, id)
	_ = os.Remove(yol)
	if g := o.listeOzet(id); g.Durum != hedefDurumHata || g.HataAciklama == "" {
		t.Fatalf("ortak yok olunca açık hata beklenirdi: %+v", g)
	}
	if kod, g := o.cagri("POST", "/hedefler/"+itoa(id)+"/yetki-baslat", ""); kod != http.StatusConflict || !strings.Contains(g, "Ortak Google istemcisi") {
		t.Fatalf("yetki-baslat 409 + ortak mesajı: %d %s", kod, g)
	}
	// Bağlantıyı kes yine de yerel jetonu siler.
	if kod, g := o.cagri("POST", "/hedefler/"+itoa(id)+"/baglantiyi-kes", ""); kod != 200 || !strings.Contains(g, `"google_iptal":false`) {
		t.Fatalf("kes: %d %s", kod, g)
	}
	s, _ := hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	if m, _ := s.sirlariAc(); m[gdrive.SirYenileme] != "" {
		t.Fatal("jeton silinmedi")
	}
}
