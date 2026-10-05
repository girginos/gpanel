package backups

// hedef_handlers_test.go — Google Drive hedef uçları: sırların şifreli saklanması,
// hiçbir yanıtta/denetimde sızmaması, yetkilendirme akışı (cihaz kodu), bağlantıyı
// kesme/iptal, silme kuralları. Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN),
// yoksa ATLANIR. Google'a gidilmez: cihaz akışı/iptal/Test dikişleri sahtedir.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/gdrive"
	"girginospanel/internal/denetim"

	"github.com/go-chi/chi/v5"
)

const (
	tSecret   = "HEDEF-TEST-CLIENT-SECRET-A1"
	tClientID = "test-123456.apps.googleusercontent.com"
	tYenileme = "1//HEDEF-TEST-YENILEME-JETONU-B2"
	tCihaz    = "HEDEF-TEST-CIHAZ-KODU-GIZLI-C3"
)

type hedefSahte struct {
	mu            sync.Mutex
	cihazDurum    gdrive.SorguDurumu
	cihazAralik   time.Duration
	sorguSayisi   int
	iptalEdilen   []string
	iptalHata     error
	testHata      error
	denetimler    []string
	cihazIsteHata error
}

func (s *hedefSahte) kur(t *testing.T) {
	t.Helper()
	sakla, coz, hazir, iste, sorgula, iptal, kur := hedefSirSakla, hedefSirCoz, hedefSirHazir, driveCihazIste, driveCihazSorgula, driveJetonIptal, driveHedefKur
	t.Cleanup(func() {
		hedefSirSakla, hedefSirCoz, hedefSirHazir = sakla, coz, hazir
		hedefDenetimYakala = nil
		driveCihazIste, driveCihazSorgula, driveJetonIptal, driveHedefKur = iste, sorgula, iptal, kur
		cihazBekleyenler.Lock()
		cihazBekleyenler.m = map[int64]*cihazBekleyen{}
		cihazBekleyenler.Unlock()
	})
	// Sahte "şifreleme": satıra bağlı (bağlam farklıysa çözülmez), "gos1:" önekli.
	hedefSirSakla = func(duz, baglam string) string {
		return "gos1:" + base64.RawStdEncoding.EncodeToString([]byte(baglam+"\x00"+duz))
	}
	hedefSirCoz = func(d, baglam string) string {
		if !strings.HasPrefix(d, "gos1:") {
			return d
		}
		b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(d, "gos1:"))
		if err != nil || !strings.HasPrefix(string(b), baglam+"\x00") {
			return d
		}
		return strings.TrimPrefix(string(b), baglam+"\x00")
	}
	hedefSirHazir = func() error { return nil }
	driveCihazIste = func(ctx context.Context, k gdrive.Kimlik, sec hedef.KurSecenek) (*gdrive.CihazYetkisi, error) {
		if s.cihazIsteHata != nil {
			return nil, s.cihazIsteHata
		}
		return &gdrive.CihazYetkisi{CihazKodu: tCihaz, KullaniciKodu: "ABCD-EFGH", DogrulamaURL: "https://www.google.com/device",
			Gecerlilik: 30 * time.Minute, Aralik: s.cihazAralik}, nil
	}
	driveCihazSorgula = func(ctx context.Context, k gdrive.Kimlik, sec hedef.KurSecenek, cihazKodu string) (gdrive.SorguSonucu, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sorguSayisi++
		if cihazKodu != tCihaz || k.ClientSecret != tSecret {
			return gdrive.SorguSonucu{}, errors.New("beklenmeyen kimlik")
		}
		if s.cihazDurum == gdrive.SorguTamam {
			return gdrive.SorguSonucu{Durum: gdrive.SorguTamam, YenilemeJetonu: tYenileme}, nil
		}
		return gdrive.SorguSonucu{Durum: s.cihazDurum}, nil
	}
	driveJetonIptal = func(ctx context.Context, k gdrive.Kimlik, sec hedef.KurSecenek, jeton string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.iptalEdilen = append(s.iptalEdilen, jeton)
		return s.iptalHata
	}
	driveHedefKur = func(ayar json.RawMessage, sirlar map[string]string, kaydet func(string)) (hedef.Hedef, error) {
		if sirlar[gdrive.SirClientSecret] != tSecret || sirlar[gdrive.SirYenileme] != tYenileme {
			return nil, errors.New("sırlar çözülemedi")
		}
		return hedefTestHedefi{s}, nil
	}
	hedefDenetimYakala = func(eylem, hedefAd, detay string) {
		s.mu.Lock()
		s.denetimler = append(s.denetimler, eylem+"|"+hedefAd+"|"+detay)
		s.mu.Unlock()
	}
}

type hedefTestHedefi struct{ s *hedefSahte }

func (h hedefTestHedefi) Tur() string                { return "drive" }
func (h hedefTestHedefi) Yetenekler() hedef.Yetenek  { return 0 }
func (h hedefTestHedefi) Test(context.Context) error { return h.s.testHata }
func (h hedefTestHedefi) Yukle(context.Context, string, string, hedef.YukleSecenek) error {
	return nil
}
func (h hedefTestHedefi) Listele(context.Context, string) ([]hedef.Nesne, error) { return nil, nil }
func (h hedefTestHedefi) Indir(context.Context, string, string) error            { return nil }
func (h hedefTestHedefi) Sil(context.Context, string) error                      { return nil }

type hedefOrtam struct {
	t   *testing.T
	h   *Handlers
	db  *sql.DB
	s   *hedefSahte
	r   http.Handler
	tum []string // tüm yanıt gövdeleri (sır taraması)
}

func yeniHedefOrtam(t *testing.T) *hedefOrtam {
	t.Helper()
	db := g113_db(t)
	g113_uygula(t, db)
	s := &hedefSahte{cihazDurum: gdrive.SorguBekliyor}
	s.kur(t)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/hedefler", h.HedefListe)
	r.Get("/hedefler/ortak-istemci", h.HedefOrtakIstemci)
	r.Post("/hedefler/drive", h.HedefDriveEkle)
	r.Delete("/hedefler/{id}", h.HedefSil)
	r.Post("/hedefler/{id}/yetki-baslat", h.HedefYetkiBaslat)
	r.Post("/hedefler/{id}/yetki-sorgu", h.HedefYetkiSorgu)
	r.Post("/hedefler/{id}/baglantiyi-kes", h.HedefBaglantiKes)
	r.Post("/hedefler/{id}/test", h.HedefTest)
	return &hedefOrtam{t: t, h: h, db: db, s: s, r: r}
}

func (o *hedefOrtam) cagri(metod, yol, govde string) (int, string) {
	o.t.Helper()
	req := httptest.NewRequest(metod, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	o.tum = append(o.tum, rec.Body.String())
	return rec.Code, rec.Body.String()
}

func (o *hedefOrtam) ekle() int64 {
	o.t.Helper()
	kod, g := o.cagri("POST", "/hedefler/drive", `{"ad":"Drive 1","client_id":"`+tClientID+`","client_secret":"`+tSecret+`","klasor":"Yedekler"}`)
	if kod != http.StatusCreated {
		o.t.Fatalf("ekle: %d %s", kod, g)
	}
	var oz HedefOzet
	_ = json.Unmarshal([]byte(g), &oz)
	return oz.ID
}

func (o *hedefOrtam) sirYok(ek ...string) {
	o.t.Helper()
	sirlar := append([]string{tSecret, tYenileme, tCihaz}, ek...)
	for i, g := range o.tum {
		for _, s := range sirlar {
			if strings.Contains(g, s) {
				o.t.Errorf("%d. yanıt gövdesine SIR sızdı: %.80s", i, g)
			}
		}
	}
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	for _, d := range o.s.denetimler {
		for _, s := range sirlar {
			if strings.Contains(d, s) {
				o.t.Errorf("denetim kaydına SIR sızdı: %s", d)
			}
		}
	}
}

func (o *hedefOrtam) dbSirlar(id int64) string {
	var s string
	if err := o.db.QueryRow(`SELECT sirlar FROM backup_hedefler WHERE id=?`, id).Scan(&s); err != nil {
		o.t.Fatal(err)
	}
	return s
}

func TestHedefDriveTamAkis(t *testing.T) {
	o := yeniHedefOrtam(t)
	id := o.ekle()
	yol := "/hedefler/" + itoa(id)

	// Sırlar DB'de ŞİFRELİ: düz client_secret yok.
	if s := o.dbSirlar(id); !strings.HasPrefix(s, "gos1:") || strings.Contains(s, tSecret) {
		t.Fatalf("sırlar şifreli saklanmadı: %.20s", s)
	}
	var ayar string
	_ = o.db.QueryRow(`SELECT ayar FROM backup_hedefler WHERE id=?`, id).Scan(&ayar)
	if strings.Contains(ayar, tSecret) {
		t.Fatal("ayar JSON'unda sır var")
	}

	// Liste: durum yetki-yok, sır yok.
	_, l := o.cagri("GET", "/hedefler", "")
	var liste []HedefOzet
	if err := json.Unmarshal([]byte(l), &liste); err != nil || len(liste) != 1 || liste[0].Durum != hedefDurumYetkiYok || liste[0].YetkiVar || liste[0].ClientID != tClientID || liste[0].Klasor != "Yedekler" {
		t.Fatalf("liste: %s", l)
	}
	// Yetkisiz hedefin testi: yetki gerekli.
	if _, g := o.cagri("POST", yol+"/test", ""); !strings.Contains(g, `"yetki_gerekli":true`) {
		t.Errorf("yetkisiz test yetki_gerekli demeli: %s", g)
	}

	// Yetkilendirme: kod verilir, cihaz kodu yanıtta YOK.
	kod, g := o.cagri("POST", yol+"/yetki-baslat", "")
	if kod != 200 || !strings.Contains(g, "ABCD-EFGH") || !strings.Contains(g, "https://www.google.com/device") {
		t.Fatalf("yetki-baslat: %d %s", kod, g)
	}
	if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"durum":"bekliyor"`) {
		t.Errorf("bekleyen sorgu: %s", g)
	}
	o.s.mu.Lock()
	o.s.cihazDurum = gdrive.SorguTamam
	o.s.mu.Unlock()
	if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"durum":"tamam"`) {
		t.Fatalf("tamam sorgu: %s", g)
	}
	// Jeton şifreli yazıldı; client_secret korundu.
	s, _ := hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	m, err := s.sirlariAc()
	if err != nil || m[gdrive.SirYenileme] != tYenileme || m[gdrive.SirClientSecret] != tSecret {
		t.Fatalf("sırlar: %v %v", m, err)
	}
	if strings.Contains(o.dbSirlar(id), tYenileme) {
		t.Fatal("yenileme jetonu DB'de düz")
	}
	// Bekleyen oturum kapandı.
	if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"durum":"yok"`) {
		t.Errorf("tamamlanınca bekleyen kalmamalı: %s", g)
	}

	// Liste: bağlı.
	_, l = o.cagri("GET", "/hedefler", "")
	_ = json.Unmarshal([]byte(l), &liste)
	if liste[0].Durum != hedefDurumBagli || !liste[0].YetkiVar {
		t.Errorf("yetkilendirilen hedef bağlı olmalı: %s", l)
	}

	// Test başarılı → son_basari dolu.
	if _, g := o.cagri("POST", yol+"/test", ""); !strings.Contains(g, `"ok":true`) {
		t.Fatalf("test: %s", g)
	}
	var sb sql.NullString
	_ = o.db.QueryRow(`SELECT son_basari FROM backup_hedefler WHERE id=?`, id).Scan(&sb)
	if !sb.Valid {
		t.Error("son_basari yazılmadı")
	}
	// Test: jeton geçersiz → yetki-gerekli durumu.
	o.s.testHata = hedef.YeniHata(hedef.KategoriKimlik, "jeton", errors.New("invalid_grant "+tYenileme), tYenileme)
	_, g = o.cagri("POST", yol+"/test", "")
	if !strings.Contains(g, `"kategori":"kimlik"`) || !strings.Contains(g, `"yetki_gerekli":true`) {
		t.Errorf("kimlik hatası yanıtı: %s", g)
	}
	_, l = o.cagri("GET", "/hedefler", "")
	_ = json.Unmarshal([]byte(l), &liste)
	if liste[0].Durum != hedefDurumYetkiGerekli || liste[0].HataKategori != "kimlik" || liste[0].HataAciklama == "" {
		t.Errorf("yetki-gerekli durumu bekleniyordu: %s", l)
	}
	// Kota hatası: durum hata.
	o.s.testHata = hedef.YeniHata(hedef.KategoriKota, "yukle", errors.New("x"))
	o.cagri("POST", yol+"/test", "")
	_, l = o.cagri("GET", "/hedefler", "")
	_ = json.Unmarshal([]byte(l), &liste)
	if liste[0].Durum != hedefDurumHata || liste[0].HataKategori != "kota" {
		t.Errorf("hata durumu bekleniyordu: %s", l)
	}

	// Bağlantıyı kes: Google'da iptal + sunucudan jeton silindi, client_secret kaldı.
	_, g = o.cagri("POST", yol+"/baglantiyi-kes", "")
	if !strings.Contains(g, `"google_iptal":true`) {
		t.Errorf("kes: %s", g)
	}
	if len(o.s.iptalEdilen) != 1 || o.s.iptalEdilen[0] != tYenileme {
		t.Errorf("jeton iptal edilmedi: %v", o.s.iptalEdilen)
	}
	s, _ = hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	m, _ = s.sirlariAc()
	if m[gdrive.SirYenileme] != "" || m[gdrive.SirClientSecret] != tSecret {
		t.Errorf("kesildikten sonra sırlar: yenileme=%q secret_var=%v", m[gdrive.SirYenileme], m[gdrive.SirClientSecret] != "")
	}
	// Google iptali başarısız olsa da sunucudaki jeton silinir, yanıt bunu söyler.
	o.s.iptalHata = errors.New("ağ")
	m[gdrive.SirYenileme] = tYenileme
	if err := hedefSirlariYaz(context.Background(), o.db, id, m); err != nil {
		t.Fatal(err)
	}
	_, g = o.cagri("POST", yol+"/baglantiyi-kes", "")
	if !strings.Contains(g, `"google_iptal":false`) {
		t.Errorf("google_iptal false beklenirdi: %s", g)
	}
	s, _ = hedefTara(o.db.QueryRow(hedefSecim+` AND id=?`, id))
	if m2, _ := s.sirlariAc(); m2[gdrive.SirYenileme] != "" {
		t.Error("Google iptali başarısız olsa da sunucudaki jeton silinmeli")
	}

	// Sil.
	if kod, g := o.cagri("DELETE", yol, ""); kod != 200 {
		t.Fatalf("sil: %d %s", kod, g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 0 {
		t.Errorf("hedef silinmedi")
	}
	o.sirYok()

	// Denetim kodlarının hepsi katalogda etiketli ve yalnız sade metin taşır.
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if len(o.s.denetimler) < 5 {
		t.Errorf("denetim kaydı eksik: %v", o.s.denetimler)
	}
	for _, d := range o.s.denetimler {
		if kod := strings.SplitN(d, "|", 2)[0]; !denetim.EylemEtiketiVar(kod) {
			t.Errorf("denetim kodu katalogda yok: %s", kod)
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestHedefEkleGecersizVeCift(t *testing.T) {
	o := yeniHedefOrtam(t)
	iyi := func(ad, cid, sec, kl string) string {
		return `{"ad":"` + ad + `","client_id":"` + cid + `","client_secret":"` + sec + `","klasor":"` + kl + `"}`
	}
	for _, c := range []struct{ ad, kotu string }{
		{"client_id", iyi("A", "kotu-id", tSecret, "K")},
		{"secret", iyi("A", tClientID, "kisa", "K")},
		{"klasor", iyi("A", tClientID, tSecret, "a/b")},
		{"ad bos", iyi("", tClientID, tSecret, "K")},
		{"json", `{`},
	} {
		if kod, g := o.cagri("POST", "/hedefler/drive", c.kotu); kod != http.StatusBadRequest {
			t.Errorf("%s: %d %s (400 beklenirdi)", c.ad, kod, g)
		}
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 0 {
		t.Fatalf("geçersiz girdi satır oluşturdu: %d", n)
	}
	o.ekle()
	if kod, _ := o.cagri("POST", "/hedefler/drive", iyi("Drive 1", tClientID, tSecret, "K")); kod != http.StatusConflict {
		t.Errorf("aynı ad 409 olmalı: %d", kod)
	}
	// Klasör boşsa varsayılan.
	_, g := o.cagri("POST", "/hedefler/drive", `{"ad":"B","client_id":"`+tClientID+`","client_secret":"`+tSecret+`"}`)
	if !strings.Contains(g, gdrive.VarsayilanKlasor) {
		t.Errorf("varsayılan klasör: %s", g)
	}
	o.sirYok()
}

func TestHedefSifrelemeHazirDegilseEklenmez(t *testing.T) {
	o := yeniHedefOrtam(t)
	body := `{"ad":"A","client_id":"` + tClientID + `","client_secret":"` + tSecret + `"}`
	// 1) anahtar hazır değil
	hedefSirHazir = func() error { return errors.New("anahtar yok") }
	if kod, _ := o.cagri("POST", "/hedefler/drive", body); kod != http.StatusServiceUnavailable {
		t.Errorf("anahtar yokken 503 beklenirdi: %d", kod)
	}
	// 2) gizli.SaklaBagli düz metne düşerse (şifrelenmedi) satır GERİ ALINIR
	hedefSirHazir = func() error { return nil }
	hedefSirSakla = func(duz, baglam string) string { return duz }
	if kod, _ := o.cagri("POST", "/hedefler/drive", body); kod != http.StatusServiceUnavailable {
		t.Errorf("düz metne düşen saklama 503 olmalı: %d", kod)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 0 {
		t.Fatalf("şifrelenemeyen sırlarla hedef OLUŞTU (%d satır, düz metin riski)", n)
	}
}

func TestHedefSilUzakKopyaVarsa409(t *testing.T) {
	o := yeniHedefOrtam(t)
	id := o.ekle()
	g113_exec(t, o.db, `INSERT INTO domains (id) VALUES (1)`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, dosya) VALUES (10, 1, 'x.tar.gz')`)
	g113_exec(t, o.db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (10, 1, 'hedef', ?)`, id)
	if kod, _ := o.cagri("DELETE", "/hedefler/"+itoa(id), ""); kod != http.StatusConflict {
		t.Errorf("uzak kopya kaydı varken 409 beklenirdi: %d", kod)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_hedefler`); n != 1 {
		t.Errorf("hedef silindi")
	}
	if len(o.s.iptalEdilen) != 0 {
		t.Errorf("silinmeyen hedefin jetonu iptal edilmemeliydi")
	}
}

func TestHedefYetkiSorguAraligaUyar(t *testing.T) {
	o := yeniHedefOrtam(t)
	o.s.cihazAralik = time.Hour
	id := o.ekle()
	yol := "/hedefler/" + itoa(id)
	o.cagri("POST", yol+"/yetki-baslat", "")
	o.cagri("POST", yol+"/yetki-sorgu", "") // ilk sorgu Google'a gider
	for i := 0; i < 5; i++ {
		if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"bekliyor"`) {
			t.Fatalf("erken sorgu bekliyor demeli: %s", g)
		}
	}
	if o.s.sorguSayisi != 1 {
		t.Errorf("Google'a %d sorgu gitti; aralık dolmadan tekrar SORGULANMAMALI (slow_down)", o.s.sorguSayisi)
	}
}

func TestHedefYetkiReddedildiVeSuresiDoldu(t *testing.T) {
	for _, d := range []gdrive.SorguDurumu{gdrive.SorguReddedildi, gdrive.SorguSuresiDoldu} {
		o := yeniHedefOrtam(t)
		id := o.ekle()
		yol := "/hedefler/" + itoa(id)
		o.cagri("POST", yol+"/yetki-baslat", "")
		o.s.mu.Lock()
		o.s.cihazDurum = d
		o.s.mu.Unlock()
		if _, g := o.cagri("POST", yol+"/yetki-sorgu", ""); !strings.Contains(g, `"durum":"`+string(d)+`"`) {
			t.Errorf("%s: %s", d, g)
		}
		if cihazBekliyorMu(id) {
			t.Errorf("%s sonrası bekleyen oturum kalmamalı", d)
		}
		if m, _ := (&hedefSatir{id: id, sirlar: o.dbSirlar(id)}).sirlariAc(); m[gdrive.SirYenileme] != "" {
			t.Errorf("%s: jeton yazılmamalı", d)
		}
	}
}

func TestHedefYetkiBaslatHatasiSirsiz(t *testing.T) {
	o := yeniHedefOrtam(t)
	id := o.ekle()
	o.s.cihazIsteHata = hedef.YeniHata(hedef.KategoriKimlik, "yetki", errors.New("invalid_client "+tSecret), tSecret)
	kod, g := o.cagri("POST", "/hedefler/"+itoa(id)+"/yetki-baslat", "")
	if kod != 200 || !strings.Contains(g, `"kategori":"kimlik"`) || strings.Contains(g, "invalid_client") {
		t.Errorf("hata yanıtı yalnız kategori+sabit açıklama olmalı: %s", g)
	}
	o.sirYok()
}

func TestHedefOlmayanVeYanlisTur(t *testing.T) {
	o := yeniHedefOrtam(t)
	if kod, _ := o.cagri("POST", "/hedefler/999/test", ""); kod != http.StatusNotFound {
		t.Errorf("olmayan hedef 404: %d", kod)
	}
	g113_exec(t, o.db, `INSERT INTO backup_hedefler (id, ad, tur, ayar, sirlar) VALUES (50, 'minio', 's3', '{}', '')`)
	if kod, _ := o.cagri("POST", "/hedefler/50/yetki-baslat", ""); kod != http.StatusBadRequest {
		t.Errorf("drive olmayan hedef 400: %d", kod)
	}
	if kod, _ := o.cagri("POST", "/hedefler/abc/test", ""); kod != http.StatusBadRequest {
		t.Errorf("geçersiz kimlik 400: %d", kod)
	}
}
