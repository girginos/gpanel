package backups

// uzak_handlers_test.go — uzak kopya uçları + yedek kancası. Geçici MariaDB gerekir
// (GOSP_TEST_GECICI_DSN), yoksa DB'li testler ATLANIR. Google'a gidilmez.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"girginospanel/internal/backups/hedef/gdrive"
	"girginospanel/internal/backups/uzak"

	"github.com/go-chi/chi/v5"
)

func g140_uygula(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/0140_yedek_uzak_kopya.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			g113_exec(t, db, s)
		}
	}
}

type uzakOrtam struct {
	t        *testing.T
	db       *sql.DB
	r        http.Handler
	mu       sync.Mutex
	denetim  []string
	yanitlar []string
	hedefID  int64
}

func yeniUzakOrtam(t *testing.T) *uzakOrtam {
	t.Helper()
	db := g113_db(t)
	g113_uygula(t, db)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN alan_adi VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN sistem_kullanici VARCHAR(64) NOT NULL DEFAULT ''`,
		`ALTER TABLE backups ADD COLUMN tip VARCHAR(16) NOT NULL DEFAULT 'tam', ADD COLUMN boyut_b BIGINT NOT NULL DEFAULT 0, ADD COLUMN job_id BIGINT NULL, ADD COLUMN sha256 CHAR(64) NOT NULL DEFAULT ''`,
	} {
		g113_exec(t, db, q)
	}
	g140_uygula(t, db)
	s := &hedefSahte{}
	s.kur(t) // hedefSirSakla/Coz/Hazir sahteleri + denetim yakalama
	us0, uc0, uh0 := uzak.SirSakla, uzak.SirCoz, uzak.SirHazir
	t.Cleanup(func() { uzak.SirSakla, uzak.SirCoz, uzak.SirHazir = us0, uc0, uh0 })
	uzak.SirSakla, uzak.SirCoz, uzak.SirHazir = hedefSirSakla, hedefSirCoz, hedefSirHazir

	o := &uzakOrtam{t: t, db: db}
	hedefDenetimYakala = func(eylem, ad, detay string) {
		o.mu.Lock()
		o.denetim = append(o.denetim, eylem+"|"+ad+"|"+detay)
		o.mu.Unlock()
	}
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/uzak-kopya", h.UzakKopyaDurum)
	r.Put("/uzak-kopya/ayar", h.UzakKopyaAyarYaz)
	r.Post("/uzak-kopya/anahtar-uret", h.UzakKopyaAnahtarUret)
	r.Post("/uzak-kopya/anahtar-onayla", h.UzakKopyaAnahtarOnayla)
	r.Post("/uzak-kopya/anahtar-ice-aktar", h.UzakKopyaAnahtarIceAktar)
	r.Get("/uzak-kopya/isler", h.UzakKopyaIsler)
	r.Get("/uzak-kopya/saklama-onizle", h.UzakKopyaSaklamaOnizle)
	r.Post("/uzak-kopya/simdi-gonder/{yedek_id}", h.UzakKopyaSimdiGonder)
	r.Post("/uzak-kopya/yeniden-dene/{is_id}", h.UzakKopyaYenidenDene)
	r.Post("/uzak-kopya/iptal/{is_id}", h.UzakKopyaIptal)
	r.Post("/uzak-kopya/anahtar-yenile", h.UzakKopyaAnahtarYenile)
	r.Post("/uzak-kopya/kurtarma-anahtari", h.UzakKopyaKurtarmaAnahtari)
	r.Get("/uzak-kopya/mevcutlari-gonder", h.UzakKopyaMevcutlariOnizle)
	r.Post("/uzak-kopya/mevcutlari-gonder", h.UzakKopyaMevcutlariGonder)
	r.Get("/uzak-kopya/yedek-durumlari", h.UzakKopyaYedekDurumlari)
	r.Get("/uzak-kopya/ilerleme", h.UzakKopyaIlerleme)
	o.r = r
	// bağlı (yetkili) bir Drive hedefi
	g113_exec(t, db, `INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('Drive 1', 'drive', '{"klasor":"Y"}', '')`)
	_ = db.QueryRow(`SELECT id FROM backup_hedefler LIMIT 1`).Scan(&o.hedefID)
	if err := hedefSirlariYaz(t.Context(), db, o.hedefID, map[string]string{gdrive.SirYenileme: "R-TEST"}); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o *uzakOrtam) cagri(metod, yol, govde string) (int, string) {
	o.t.Helper()
	req := httptest.NewRequest(metod, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	o.mu.Lock()
	o.yanitlar = append(o.yanitlar, rec.Body.String())
	o.mu.Unlock()
	return rec.Code, rec.Body.String()
}

func ozelAnahtarSatiri(metin string) string {
	for _, l := range strings.Split(metin, "\n") {
		if strings.HasPrefix(l, "AGE-SECRET-KEY") {
			return l
		}
	}
	return ""
}

func TestUzakKopyaUclari_TamAkisVeSizintiYok(t *testing.T) {
	o := yeniUzakOrtam(t)
	// varsayılan KAPALI, anahtar yok
	kod, g := o.cagri("GET", "/uzak-kopya", "")
	var d struct {
		Ayar         uzak.Ayar          `json:"ayar"`
		EtkinAnahtar *uzak.AnahtarBilgi `json:"etkin_anahtar"`
		Hedefler     []HedefOzet        `json:"hedefler"`
	}
	if kod != 200 || json.Unmarshal([]byte(g), &d) != nil || d.Ayar.Aktif || d.EtkinAnahtar != nil || len(d.Hedefler) != 1 {
		t.Fatalf("başlangıç durumu: %d %s", kod, g)
	}
	// geriye uyumlu eski akış (elle anahtar-uret + onayla) hâlâ çalışır: önce kapalı ayar yaz
	// anahtar üret: kurtarma dosyası TEK KEZ döner
	kod, g = o.cagri("POST", "/uzak-kopya/anahtar-uret", `{}`)
	uretIdx := len(o.yanitlar) - 1
	if kod != 200 || !strings.Contains(g, "AGE-SECRET-KEY-1") || !strings.Contains(g, "GERİ YÜKLENEMEZ") {
		t.Fatalf("anahtar-uret: %d %.200s", kod, g)
	}
	var tam map[string]any
	_ = json.Unmarshal([]byte(g), &tam)
	ozel := ozelAnahtarSatiri(tam["kurtarma_dosyasi"].(string))
	if ozel == "" {
		t.Fatal("özel anahtar çıkarılamadı")
	}
	parmak := tam["parmak"].(string)
	// bir daha GÖSTERİLMEZ: durum + diğer yanıtlar özel anahtar içermez
	_, gd := o.cagri("GET", "/uzak-kopya", "")
	if strings.Contains(gd, ozel) || strings.Contains(gd, "AGE-SECRET-KEY") {
		t.Fatal("durum yanıtı özel anahtar içeriyor")
	}
	// onay kutusu olmadan etkinleşmez
	if kod, _ := o.cagri("POST", "/uzak-kopya/anahtar-onayla", `{"parmak":"`+parmak+`","sakladim":false}`); kod != 400 {
		t.Errorf("onaysız etkinleştirme 400: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/anahtar-onayla", `{"parmak":"`+parmak+`","sakladim":true,"indirildi":true}`); kod != 200 {
		t.Fatalf("onay: %d", kod)
	}
	// aç
	if kod, g := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"gp","saklama_n":7,"bant_kbps":512}`); kod != 200 {
		t.Fatalf("ayar: %d %s", kod, g)
	}
	// geçersiz ayar
	if kod, _ := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"../x","saklama_n":7}`); kod != 400 {
		t.Errorf("geçersiz klasör 400: %d", kod)
	}
	// bilinmeyen alan reddedilir
	if kod, _ := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"foo":1}`); kod != 400 {
		t.Errorf("bilinmeyen alan 400: %d", kod)
	}
	// ---- sızıntı taraması: HİÇBİR yanıt (anahtar-uret hariç) ve denetim özel anahtarı içermez
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, y := range o.yanitlar {
		if i == uretIdx { // anahtar-uret yanıtı: kurtarma dosyası bilerek içerir
			continue
		}
		if strings.Contains(y, ozel) || strings.Contains(y, "R-TEST") {
			t.Errorf("%d. yanıt sır içeriyor", i)
		}
	}
	if len(o.denetim) < 3 {
		t.Fatalf("denetim kayıtları: %v", o.denetim)
	}
	for _, dk := range o.denetim {
		if strings.Contains(dk, ozel) || strings.Contains(dk, "AGE-SECRET") || strings.Contains(dk, parmak) {
			t.Errorf("denetim sızdırıyor: %s", dk)
		}
	}
	var gizliKol string
	_ = o.db.QueryRow(`SELECT gizli_anahtar FROM backup_sifre_anahtarlari`).Scan(&gizliKol)
	if strings.Contains(gizliKol, ozel) {
		t.Error("özel anahtar DB'de düz")
	}
}

func TestUzakKopyaAnahtarUretCacheNoStore(t *testing.T) {
	o := yeniUzakOrtam(t)
	req := httptest.NewRequest("POST", "/uzak-kopya/anahtar-uret", strings.NewReader(`{"hibrit":true}`))
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), "ML-KEM-768") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
}

func TestUzakKopyaSimdiGonder(t *testing.T) {
	o := yeniUzakOrtam(t)
	_, gu := o.cagri("POST", "/uzak-kopya/anahtar-uret", `{}`)
	var tam map[string]any
	_ = json.Unmarshal([]byte(gu), &tam)
	o.cagri("POST", "/uzak-kopya/anahtar-onayla", `{"parmak":"`+tam["parmak"].(string)+`","sakladim":true}`)
	o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"gp","saklama_n":7}`)
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a')`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, tip, dosya) VALUES (10,1,'oto','c_a-20260825-020000.tar.gz'), (11,1,'tam','c_a-20260826-020000.tar.gz')`)
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/11", ""); kod != http.StatusConflict {
		t.Errorf("müşteri manuel yedeği uzağa GİTMEZ: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/999", ""); kod != 404 {
		t.Errorf("olmayan yedek: %d", kod)
	}
	if kod, g := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != 202 {
		t.Fatalf("kök yedeği: %d %s", kod, g)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != 202 { // zaten sırada: idempotent
		t.Errorf("sıradaki iş tekrar: %d", kod)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 1 {
		t.Errorf("tek iş: %d", n)
	}
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='tamam'`)
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != http.StatusConflict {
		t.Errorf("zaten gönderilmiş: %d", kod)
	}
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='hata', deneme=6, son_hata='x'`)
	if kod, _ := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != 202 {
		t.Errorf("hatalı iş yeniden kuyruğa: %d", kod)
	}
	if d := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE durum='bekliyor' AND deneme=0`); d != 1 {
		t.Error("hata işi sıfırlanıp bekliyor olmalı")
	}
	// yeniden-dene / iş listesi / önizleme
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='hata'`)
	if kod, _ := o.cagri("POST", "/uzak-kopya/yeniden-dene/1", ""); kod != 202 {
		t.Errorf("yeniden-dene: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/yeniden-dene/1", ""); kod != 409 {
		t.Errorf("bekleyen iş yeniden denenemez: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/yeniden-dene/abc", ""); kod != 400 {
		t.Errorf("geçersiz id: %d", kod)
	}
	kod, g := o.cagri("GET", "/uzak-kopya/isler?sayfa=1&adet=10", "")
	if kod != 200 || !strings.Contains(g, `"toplam":1`) || !strings.Contains(g, "a.example") {
		t.Errorf("isler: %d %s", kod, g)
	}
	if kod, g := o.cagri("GET", "/uzak-kopya/saklama-onizle", ""); kod != 200 || !strings.Contains(g, `"silinecek":[]`) {
		t.Errorf("onizle: %d %s", kod, g)
	}
	if kod, _ := o.cagri("POST", "/uzak-kopya/iptal/1", ""); kod != 503 { // işçi bu testte yok
		t.Errorf("işçisiz iptal: %d", kod)
	}
}

func TestUzakKancasi_KapaliykenDokunmazAcikkenKuyrugaAlir(t *testing.T) {
	o := yeniUzakOrtam(t)
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a')`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, tip, dosya) VALUES (10,1,'oto','c_a-20260825-020000.tar.gz')`)
	uzakKopyaKuyrugaAl(o.db, 10) // KAPALI: hiçbir iş
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Fatalf("kapalıyken iş oluşmamalı: %d", n)
	}
	uzakKopyaKuyrugaAl(o.db, 99999) // olmayan yedek: panic/hata yok
	// tablo hiç yokmuş gibi (göç koşmamış kurucu): kanca YUTMALI, panic ETMEMELİ
	g113_exec(t, o.db, `DROP TABLE backup_uzak_kopya_ayar`)
	uzakKopyaKuyrugaAl(o.db, 10)
	uzakKopyaKuyrugaAl(nil, 10)
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 0 {
		t.Errorf("iş oluşmamalı: %d", n)
	}
}

// Kanca yalnız yedek satırı yazıldıktan SONRA ve hata dönüşlerinden ÖNCE değil
// SONRA çağrılmalı: yerel yedek başarısızsa uzak iş oluşamaz.
func TestUzakKancasi_YerelBasariSonrasiTekNokta(t *testing.T) {
	b, err := os.ReadFile("jobs.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	a := strings.Index(s, "func birDomainYedekle(")
	e := strings.Index(s[a:], "\n}\n")
	f := s[a : a+e]
	if strings.Count(f, "uzakKopyaKuyrugaAl(") != 1 {
		t.Fatal("birDomainYedekle içinde tam bir kanca olmalı")
	}
	kanca := strings.Index(f, "uzakKopyaKuyrugaAl(")
	for _, onc := range []string{"arsivOlustur(", "INSERT INTO backups"} {
		if i := strings.Index(f, onc); i < 0 || i > kanca {
			t.Errorf("kanca %q çağrısından SONRA olmalı", onc)
		}
	}
	// arşiv/INSERT hata dönüşleri kancadan önce
	if strings.LastIndex(f[:kanca], "return boyut, dosya, err") < 0 || strings.LastIndex(f[:kanca], "return 0, \"\", err") < 0 {
		t.Error("hata dönüşleri kancadan önce olmalı")
	}
	// manuel (müşteri) yolunda kanca YOK
	bb, _ := os.ReadFile("backups.go")
	if strings.Contains(string(bb), "uzakKopyaKuyrugaAl") {
		t.Error("müşteri/manuel yedek yolu uzak kopya kancası içermemeli")
	}
}
