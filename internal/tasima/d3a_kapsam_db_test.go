package tasima

// d3a_kapsam_db_test.go — D3a: taşımanın bayiye açılması, DB'li kapsam matrisi
// (kök / bayi 7 / bayi 9). Taban şema GERÇEK göçlerden (0051+0055+0096+0109),
// bağlantı katı kipte (goc0109_test.go düzeneği). Uzak sunucuya, panelin gizli
// anahtarına ve canlı sisteme dokunulmaz: kimlik şifrelemesi, hedef çözümleme,
// anahtar dizini ve iş yürütücüsü sahte/geçicidir.
//
// GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
)

// d3Ortam — şema + sahte dikişler. Kimlikler: kök (uid 1), bayi7, bayi9, bayi0
// (kimliksiz bayi jetonu), musteri.
type d3Ortam struct {
	h        *Handlers
	db       *sql.DB
	basladi  chan int64
	baslatMu sync.Mutex
	baslayan []int64
}

func d3Kur(t *testing.T) *d3Ortam {
	t.Helper()
	db := g109Taban0109Oncesi(t)
	g109GocUygula(t, db, g109Goc)
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			role VARCHAR(16) NOT NULL DEFAULT 'user', status VARCHAR(16) NOT NULL DEFAULT 'active',
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0,
			max_trafik_mb BIGINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL UNIQUE, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			web_root VARCHAR(255) NULL, reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT NULL,
			boyut_kb BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE subdomanlar (id INT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			alt_ad VARCHAR(63) NOT NULL, tam_ad VARCHAR(253) NOT NULL, UNIQUE KEY uq_sub_tam (tam_ad))`,
		`CREATE TABLE service_plans (id BIGINT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0,
			domain_id BIGINT NULL, varsayilan TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(45) NOT NULL DEFAULT '',
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NOT NULL DEFAULT '', detail JSON NULL,
			ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (7,'bayi7','reseller'), (9,'bayi9','reseller')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES
			(10,'kok.test','c_kok',0), (20,'bayi7.test','c_bayi7',7), (30,'bayi9.test','c_bayi9',9)`,
		`INSERT INTO service_plans (id, reseller_id, domain_id, varsayilan) VALUES
			(1,0,NULL,0), (2,9,NULL,0), (3,7,NULL,1), (4,0,10,0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("d3 şema: %v\n%s", err, q)
		}
	}
	o := &d3Ortam{h: &Handlers{DB: db}, db: db, basladi: make(chan int64, 16)}

	// Sahte dikişler (hepsi Cleanup'ta geri alınır).
	eskiClaims, eskiS, eskiC := claimsAl, kimlikSifrele, kimlikCoz
	eskiY, eskiD, eskiHC, eskiKA := isYurutucu, tasimaKeyDir, hedefCozucu, hedefKendiAdresleri
	claimsAl = func(r *http.Request) *auth.Claims {
		switch r.Header.Get("X-T-Kim") {
		case "kok":
			return &auth.Claims{UserID: 1, Username: "root", Role: "admin"}
		case "bayi7":
			return &auth.Claims{UserID: 7, Username: "bayi7", Role: "reseller", ResellerID: 7}
		case "bayi9":
			return &auth.Claims{UserID: 9, Username: "bayi9", Role: "reseller", ResellerID: 9}
		case "bayi0":
			return &auth.Claims{UserID: 8, Username: "bayi0", Role: "reseller"}
		case "musteri":
			return &auth.Claims{UserID: 5, Username: "m", Role: "musteri"}
		}
		return nil
	}
	// Şifreleme sahtesi: bağlam metne gömülür; başka bağlamda ÇÖZÜLMEZ (gerçek AEAD gibi
	// girdiyi aynen döndürür).
	kimlikSifrele = func(d, b string) string { return "T|" + b + "|" + d }
	kimlikCoz = func(v, b string) string {
		if s := strings.TrimPrefix(v, "T|"+b+"|"); s != v {
			return s
		}
		return v
	}
	isYurutucu = func(h *Handlers, ctx context.Context, id int64) {
		o.baslatMu.Lock()
		o.baslayan = append(o.baslayan, id)
		o.baslatMu.Unlock()
		o.basladi <- id
	}
	tasimaKeyDir = t.TempDir()
	eskiLog := logDizin
	logDizin = t.TempDir()
	t.Cleanup(func() { logDizin = eskiLog })
	hedefCozucu = func(_ context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	hedefKendiAdresleri = func() ([]net.Addr, error) { return nil, nil }
	kilit.Lock()
	aktifIsID, aktifIptal = 0, nil
	kilit.Unlock()
	t.Cleanup(func() {
		claimsAl, kimlikSifrele, kimlikCoz = eskiClaims, eskiS, eskiC
		isYurutucu, tasimaKeyDir, hedefCozucu, hedefKendiAdresleri = eskiY, eskiD, eskiHC, eskiKA
		kilit.Lock()
		if aktifIptal != nil {
			aktifIptal()
		}
		aktifIsID, aktifIptal = 0, nil
		kilit.Unlock()
		hizMu.Lock()
		hizKovas = map[int64]*hizKova{}
		hizMu.Unlock()
	})
	return o
}

// router — rotalar.go ile AYNI yollar, ara yazılımsız (kapsam handler içinde sınanır).
func (o *d3Ortam) router() http.Handler {
	h := o.h
	r := chi.NewRouter()
	r.Get("/tasima/kaynaklar", h.Kaynaklar)
	r.Get("/tasima/anahtar", h.PanelAnahtar)
	r.Post("/tasima/test", h.Test)
	r.Post("/tasima/kesif", h.Kesif)
	r.Post("/tasima/baslat", h.Baslat)
	r.Get("/tasima", h.Durum)
	r.Get("/tasima/oturumlar", h.OturumListe)
	r.Get("/tasima/oturum/{id}", h.OturumGetir)
	r.Delete("/tasima/oturum/{id}", h.OturumSil)
	r.Get("/tasima/{id}", h.Detay)
	r.Get("/tasima/{id}/log", h.Log)
	r.Post("/tasima/{id}/iptal", h.Iptal)
	r.Put("/tasima/ayar", h.AyarGuncelle)
	r.Put("/tasima/kaynaklar/{kod}", h.KaynakGuncelle)
	return r
}

func (o *d3Ortam) istek(t *testing.T, kim, yontem, yol string, govde any) (int, string) {
	t.Helper()
	var rd io.Reader
	if govde != nil {
		b, _ := json.Marshal(govde)
		rd = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(yontem, yol, rd)
	req.Header.Set("X-T-Kim", kim)
	rec := httptest.NewRecorder()
	o.router().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (o *d3Ortam) exec(t *testing.T, q string, args ...any) sql.Result {
	t.Helper()
	res, err := o.db.Exec(q, args...)
	if err != nil {
		t.Fatalf("SQL: %v\n%s", err, q)
	}
	return res
}

func (o *d3Ortam) bayiAc(t *testing.T, acik bool) {
	t.Helper()
	kod, govde := o.istek(t, "kok", http.MethodPut, "/tasima/ayar", map[string]bool{"bayi_acik": acik})
	if kod != http.StatusOK {
		t.Fatalf("bayi anahtarı: %d %s", kod, govde)
	}
}

// isEkle — doğrudan iş satırı (kalemli).
func (o *d3Ortam) isEkle(t *testing.T, rid int64, durum, kalemHata string) int64 {
	t.Helper()
	res := o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, toplam, sahip_rid, baslatan, kaynak_parola)
		VALUES ('plesk', '93.184.216.34', ?, 1, ?, 'x', 'T|gizli')`, durum, rid)
	id, _ := res.LastInsertId()
	o.exec(t, `INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum, hata) VALUES (?, 'u', ?, 'hata', ?)`,
		id, fmt.Sprintf("is%d.test", id), kalemHata)
	return id
}

func TestD3_KaynaklarRolFarkindalikli(t *testing.T) {
	o := d3Kur(t)
	type yanit struct {
		Acik      bool          `json:"acik"`
		BayiAcik  bool          `json:"bayi_acik"`
		Kaynaklar []kaynakYanit `json:"kaynaklar"`
	}
	oku := func(kim string) yanit {
		t.Helper()
		kod, g := o.istek(t, kim, http.MethodGet, "/tasima/kaynaklar", nil)
		if kod != http.StatusOK {
			t.Fatalf("%s kaynaklar: %d %s", kim, kod, g)
		}
		var y yanit
		_ = json.Unmarshal([]byte(g), &y)
		return y
	}
	kodlar := func(y yanit) string {
		var s []string
		for _, k := range y.Kaynaklar {
			s = append(s, fmt.Sprintf("%s:%v", k.Kod, k.Etkin))
		}
		return strings.Join(s, ",")
	}
	// Varsayılan: bayide KAPALI (0109 tohumu '0').
	if y := oku("bayi7"); y.Acik || len(y.Kaynaklar) != 0 {
		t.Fatalf("bayide varsayılan kapalı olmalı: %+v", y)
	}
	if y := oku("kok"); !y.Acik || y.BayiAcik || kodlar(y) != "plesk:true,gpanel:true,cpanel:false,directadmin:false" {
		t.Fatalf("kök tüm kaynakları etkin bayrağıyla görmeli: %+v (%s)", y, kodlar(y))
	}
	o.bayiAc(t, true)
	y := oku("bayi7")
	if !y.Acik || y.BayiAcik || kodlar(y) != "plesk:true,gpanel:true" {
		t.Fatalf("bayi yalnız etkin kaynakları görmeli: %+v (%s)", y, kodlar(y))
	}
	for _, k := range y.Kaynaklar {
		if k.Yetenekler.Sahipler {
			t.Errorf("bayide sahipler yeteneği HER ZAMAN false: %s", k.Kod)
		}
	}
	// Kök yeni kaynak açınca bayi de görür (tek kayıt defteri); kapatınca kaybolur.
	if kod, g := o.istek(t, "kok", http.MethodPut, "/tasima/kaynaklar/cpanel", map[string]bool{"etkin": true}); kod != http.StatusOK {
		t.Fatalf("cpanel aç: %d %s", kod, g)
	}
	if kod, g := o.istek(t, "kok", http.MethodPut, "/tasima/kaynaklar/plesk", map[string]bool{"etkin": false}); kod != http.StatusOK {
		t.Fatalf("plesk kapat: %d %s", kod, g)
	}
	if got := kodlar(oku("bayi9")); got != "gpanel:true,cpanel:true" {
		t.Errorf("bayi kökün kaynak kararını görmeli: %s", got)
	}
	// Bayi ayar uçlarını çağıramaz; kayıtta olmayan kaynak 404.
	if kod, _ := o.istek(t, "bayi7", http.MethodPut, "/tasima/ayar", map[string]bool{"bayi_acik": true}); kod != http.StatusForbidden {
		t.Errorf("bayi genel anahtarı değiştirememeli: %d", kod)
	}
	if kod, _ := o.istek(t, "bayi7", http.MethodPut, "/tasima/kaynaklar/directadmin", map[string]bool{"etkin": true}); kod != http.StatusForbidden {
		t.Errorf("bayi kaynak anahtarını değiştirememeli: %d", kod)
	}
	if kod, _ := o.istek(t, "kok", http.MethodPut, "/tasima/kaynaklar/joomla", map[string]bool{"etkin": true}); kod != http.StatusNotFound {
		t.Errorf("kayıtta olmayan kaynak 404: %d", kod)
	}
	// Kimliksiz bayi ve müşteri hiçbir şey göremez.
	for _, kim := range []string{"bayi0", "musteri", ""} {
		if kod, _ := o.istek(t, kim, http.MethodGet, "/tasima/kaynaklar", nil); kod != http.StatusForbidden {
			t.Errorf("%q: 403 beklenir, alınan %d", kim, kod)
		}
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('tasima_bayi_ayar','tasima_kaynak_ayar')`).Scan(&n)
	if n != 3 {
		t.Errorf("kök anahtar değişiklikleri denetime yazılmalı: %d kayıt", n)
	}
}

func TestD3_BayiKapaliVeKaynakKapali(t *testing.T) {
	o := d3Kur(t)
	govde := map[string]any{"tip": "plesk", "host": "93.184.216.34", "port": 22, "kullanici": "root", "parola": "pw-1234"}
	for _, yol := range []string{"/tasima/test", "/tasima/kesif", "/tasima/baslat"} {
		kod, g := o.istek(t, "bayi7", http.MethodPost, yol, govde)
		if kod != http.StatusForbidden || !strings.Contains(g, errTasimaKapali.Error()) {
			t.Errorf("%s: bayide kapalıyken 403 '%s' beklenir: %d %s", yol, errTasimaKapali, kod, g)
		}
	}
	if kod, _ := o.istek(t, "bayi7", http.MethodGet, "/tasima/anahtar", nil); kod != http.StatusForbidden {
		t.Errorf("anahtar ucu bayide kapalıyken 403: %d", kod)
	}
	o.bayiAc(t, true)
	// Deneysel (kapalı) kaynak: bayi de kök de kullanamaz (kaynağa bağlanmadan 400).
	govde["tip"] = "cpanel"
	for _, kim := range []string{"bayi7", "kok"} {
		if kod, g := o.istek(t, kim, http.MethodPost, "/tasima/test", govde); kod != http.StatusBadRequest || !strings.Contains(g, errKaynakKapali.Error()) {
			t.Errorf("%s: kapalı kaynak 400: %d %s", kim, kod, g)
		}
	}
	// Bayi: özel ağ hedefi bağlantıdan ÖNCE reddedilir.
	hedefCozucu = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}, nil
	}
	govde["tip"], govde["host"] = "plesk", "ic.example.com"
	if kod, g := o.istek(t, "bayi7", http.MethodPost, "/tasima/test", govde); kod != http.StatusBadRequest || !strings.Contains(g, "genel") {
		t.Errorf("bayi özel ağ hedefi 400: %d %s", kod, g)
	}
}

func TestD3_IsIDOR(t *testing.T) {
	o := d3Kur(t)
	kokIs := o.isEkle(t, 0, "tamam", "saglama: nginx -t /etc/nginx/conf.d/kok.conf")
	a := o.isEkle(t, 7, "hata", "bayi7 kendi notu")
	b := o.isEkle(t, 9, "tamam", "bayi9 gizli notu")
	if err := os.MkdirAll(logDizin, 0o750); err == nil {
		_ = os.WriteFile(logYolu(b), []byte("bayi9 gunlugu\n"), 0o640)
		t.Cleanup(func() { _ = os.Remove(logYolu(b)) })
	}

	// Liste: bayi yalnız kendi işini görür; sahip alanı bayiye gitmez.
	_, g := o.istek(t, "bayi7", http.MethodGet, "/tasima", nil)
	var liste struct {
		Isler []struct {
			ID    int64  `json:"id"`
			Sahip string `json:"sahip"`
		} `json:"isler"`
		AktifIs int64 `json:"aktif_is"`
	}
	_ = json.Unmarshal([]byte(g), &liste)
	if len(liste.Isler) != 1 || liste.Isler[0].ID != a || liste.Isler[0].Sahip != "" {
		t.Errorf("bayi 7 yalnız kendi işini görmeli: %s", g)
	}
	_, g = o.istek(t, "kok", http.MethodGet, "/tasima", nil)
	_ = json.Unmarshal([]byte(g), &liste)
	if len(liste.Isler) != 3 || !strings.Contains(g, `"sahip":"bayi9"`) {
		t.Errorf("kök tüm işleri sahibiyle görmeli: %s", g)
	}

	// Detay/günlük/iptal: başkasının iş kimliği = olmayan kimlik (kod + gövde BAYT BAYT).
	for _, uc := range []struct{ yontem, sonek string }{
		{http.MethodGet, ""}, {http.MethodGet, "/log"}, {http.MethodPost, "/iptal"},
	} {
		yokKod, yokGovde := o.istek(t, "bayi7", uc.yontem, "/tasima/987654"+uc.sonek, nil)
		for _, yabanci := range []int64{b, kokIs} {
			kod, govde := o.istek(t, "bayi7", uc.yontem, fmt.Sprintf("/tasima/%d%s", yabanci, uc.sonek), nil)
			if kod != yokKod || govde != yokGovde || kod != http.StatusNotFound {
				t.Errorf("%s %d%s: yabancı iş (%d %q) ≠ olmayan iş (%d %q)", uc.yontem, yabanci, uc.sonek, kod, govde, yokKod, yokGovde)
			}
		}
	}
	if kod, g := o.istek(t, "bayi7", http.MethodGet, fmt.Sprintf("/tasima/%d", a), nil); kod != http.StatusOK || !strings.Contains(g, "bayi7 kendi notu") {
		t.Errorf("bayi kendi işinin detayını görmeli: %d %s", kod, g)
	}
	if kod, g := o.istek(t, "kok", http.MethodGet, fmt.Sprintf("/tasima/%d/log", b), nil); kod != http.StatusOK || !strings.Contains(g, "bayi9 gunlugu") {
		t.Errorf("kök bayi işinin günlüğünü görmeli: %d %s", kod, g)
	}

	// aktif_is: kökün çalışan işi bayiye görünmez.
	kilit.Lock()
	aktifIsID = kokIs
	kilit.Unlock()
	_, g = o.istek(t, "bayi7", http.MethodGet, "/tasima", nil)
	if strings.Contains(g, fmt.Sprintf(`"aktif_is":%d`, kokIs)) {
		t.Errorf("bayi kökün aktif işini öğrenmemeli: %s", g)
	}
	_, g = o.istek(t, "kok", http.MethodGet, "/tasima", nil)
	if !strings.Contains(g, fmt.Sprintf(`"aktif_is":%d`, kokIs)) {
		t.Errorf("kök aktif işi görmeli: %s", g)
	}
	kilit.Lock()
	aktifIsID = 0
	kilit.Unlock()
}

func TestD3_OturumKapsami(t *testing.T) {
	o := d3Kur(t)
	o.bayiAc(t, true)
	g := kaynakGirdi{Tip: "plesk", Host: "93.184.216.34", Port: 22, Kullanici: "root", Parola: "kok-parolasi"}
	kokOt := o.h.oturumKaydet(istekKapsami{}, g, []Hesap{{AlanAdi: "a.test"}})
	g.Parola = "bayi7-parolasi"
	b7Ot := o.h.oturumKaydet(istekKapsami{Kapsam: Kapsam{RID: 7}, UID: 7}, g, []Hesap{{AlanAdi: "b.test"}})
	if kokOt <= 0 || b7Ot <= 0 {
		t.Fatalf("oturumlar yazılamadı: %d %d", kokOt, b7Ot)
	}
	// Bayi aynı host için oturum açınca kökün oturumu SİLİNMEZ.
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE id=? AND durum='oturum'`, kokOt).Scan(&n)
	if n != 1 {
		t.Fatalf("bayi oturumu kökün oturumunu sildi")
	}
	// Kimlik kapsam bağlamıyla şifreli.
	var p7 string
	_ = o.db.QueryRow(`SELECT kaynak_parola FROM tasima_isleri WHERE id=?`, b7Ot).Scan(&p7)
	if p7 != "T|r7|93.184.216.34|bayi7-parolasi" {
		t.Errorf("bayi kimliği kiracı bağlamıyla şifrelenmeli: %q", p7)
	}

	yokKod, yokGovde := o.istek(t, "bayi7", http.MethodGet, "/tasima/oturum/987654", nil)
	kod, govde := o.istek(t, "bayi7", http.MethodGet, fmt.Sprintf("/tasima/oturum/%d", kokOt), nil)
	if kod != yokKod || govde != yokGovde || kod != http.StatusNotFound {
		t.Errorf("başkasının oturumu = olmayan oturum: (%d %q) vs (%d %q)", kod, govde, yokKod, yokGovde)
	}
	if kod, govde := o.istek(t, "bayi9", http.MethodDelete, fmt.Sprintf("/tasima/oturum/%d", b7Ot), nil); kod != yokKod || govde != yokGovde {
		t.Errorf("bayi 9 bayi 7'nin oturumunu silememeli: %d %s", kod, govde)
	}
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE id=?`, b7Ot).Scan(&n)
	if n != 1 {
		t.Errorf("çapraz silme satırı sildi")
	}
	_, govde = o.istek(t, "bayi7", http.MethodGet, "/tasima/oturumlar", nil)
	if !strings.Contains(govde, fmt.Sprintf(`"id":%d`, b7Ot)) || strings.Contains(govde, fmt.Sprintf(`"id":%d`, kokOt)) {
		t.Errorf("bayi yalnız kendi oturumlarını listelemeli: %s", govde)
	}
	if strings.Contains(govde, "parolasi") {
		t.Errorf("oturum listesi sır döndürmemeli: %s", govde)
	}
	// Kökün oturum_id'siyle başlatma: oturum "yok" sayılır → kimlik çözülmez, keşif istenir.
	kod, govde = o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", map[string]any{
		"tip": "plesk", "host": "93.184.216.34", "port": 22, "kullanici": "root", "parola": "",
		"oturum_id": kokOt, "secilen": []Hesap{{AlanAdi: "a.test"}},
	})
	if kod != http.StatusBadRequest {
		t.Errorf("başka kapsamın oturumu kullanılamamalı: %d %s", kod, govde)
	}
}

// bayiOturum — bayi 7 için sunucu keşif kopyalı oturum.
func (o *d3Ortam) bayiOturum(t *testing.T, rid int64, hesaplar []Hesap) int64 {
	t.Helper()
	g := kaynakGirdi{Tip: "plesk", Host: "93.184.216.34", Port: 22, Kullanici: "root", Parola: "kaynak-pw"}
	id := o.h.oturumKaydet(istekKapsami{Kapsam: Kapsam{RID: rid}, UID: rid}, g, hesaplar)
	if id <= 0 {
		t.Fatalf("oturum yazılamadı")
	}
	return id
}

func (o *d3Ortam) baslatGovde(oturum int64, secilen []Hesap, ay Ayarlar) map[string]any {
	return map[string]any{"tip": "plesk", "host": "93.184.216.34", "port": 22, "kullanici": "root",
		"oturum_id": oturum, "secilen": secilen, "ayarlar": ay}
}

func TestD3_BaslatBayiZorlananAlanlar(t *testing.T) {
	o := d3Kur(t)
	o.bayiAc(t, true)
	ot := o.bayiOturum(t, 7, []Hesap{{KaynakHesap: "ahmet", AlanAdi: "a.test", WebRoot: "/var/www/vhosts/a.test/httpdocs", DBler: []string{"ahmet_wp"}}})
	// Gövde: başka bayi + müşteri, sahip taşıma, üzerine yazma, keşifte olmayan site ve
	// sahte docroot/DB. Hepsi yok sayılmalı.
	kod, govde := o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot,
		[]Hesap{{AlanAdi: "A.test", WebRoot: "/etc", DBler: []string{"mysql"}}, {AlanAdi: "baska.test", WebRoot: "/root"}},
		Ayarlar{ResellerID: 9, CustomerID: 5, SahipleriTasi: true, Ustune: true, Dosyalar: true}))
	if kod != http.StatusAccepted {
		t.Fatalf("bayi başlatma: %d %s", kod, govde)
	}
	var y struct {
		IsID   int64  `json:"is_id"`
		Toplam int    `json:"toplam"`
		Durum  string `json:"durum"`
	}
	_ = json.Unmarshal([]byte(govde), &y)
	if y.Toplam != 1 || y.Durum != "calisiyor" {
		t.Errorf("tek kalem (kesişim) ve boş yuvada hemen başlamalı: %s", govde)
	}
	select {
	case id := <-o.basladi:
		if id != y.IsID {
			t.Errorf("başlayan iş %d, beklenen %d", id, y.IsID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dağıtıcı işi başlatmadı")
	}
	var rid int64
	var uid sql.NullInt64
	var ayarJSON, parola string
	if err := o.db.QueryRow(`SELECT sahip_rid, baslatan_uid, ayarlar, kaynak_parola FROM tasima_isleri WHERE id=?`, y.IsID).
		Scan(&rid, &uid, &ayarJSON, &parola); err != nil {
		t.Fatal(err)
	}
	if rid != 7 || uid.Int64 != 7 {
		t.Errorf("iş kapsamı sunucudan: sahip_rid=%d baslatan_uid=%v", rid, uid)
	}
	var kayit isAyarKayit
	_ = json.Unmarshal([]byte(ayarJSON), &kayit)
	if kayit.ResellerID != 7 || kayit.CustomerID != 0 || kayit.SahipleriTasi || kayit.Ustune || kayit.PlanID != 3 || len(kayit.SahipEsleK) != 0 {
		t.Errorf("bayi için zorlanan alanlar: %s", ayarJSON)
	}
	if parola != "T|r7|93.184.216.34|kaynak-pw" {
		t.Errorf("oturumdaki kimlik bayi bağlamıyla çözülüp yeniden şifrelenmeli: %q", parola)
	}
	var alan, hj string
	if err := o.db.QueryRow(`SELECT alan_adi, hesap_json FROM tasima_kalemleri WHERE is_id=?`, y.IsID).Scan(&alan, &hj); err != nil {
		t.Fatal(err)
	}
	var hs Hesap
	_ = json.Unmarshal([]byte(hj), &hs)
	if alan != "a.test" || hs.WebRoot != "/var/www/vhosts/a.test/httpdocs" || strings.Join(hs.DBler, ",") != "ahmet_wp" {
		t.Errorf("kalem SUNUCU keşif kopyasından olmalı: %s %s", alan, hj)
	}
	// Aynı kiracının ikinci başlatması: 409 (kendi verisi).
	if kod, g := o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, []Hesap{{AlanAdi: "a.test"}}, Ayarlar{})); kod != http.StatusConflict {
		t.Errorf("aynı bayinin ikinci işi 409: %d %s", kod, g)
	}
	var dn int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='tasima_baslat' AND reseller_id=7 AND actor_user_id=7`).Scan(&dn)
	if dn != 1 {
		t.Errorf("başlatma denetimi bayi kapsamıyla yazılmalı: %d", dn)
	}
}

func TestD3_BaslatBayiPlanVeKota(t *testing.T) {
	o := d3Kur(t)
	o.bayiAc(t, true)
	ot := o.bayiOturum(t, 9, []Hesap{{KaynakHesap: "u", AlanAdi: "x1.test", WebRoot: "/home/u/x1"}, {KaynakHesap: "u", AlanAdi: "x2.test", WebRoot: "/home/u/x2"}})
	iki := []Hesap{{AlanAdi: "x1.test"}, {AlanAdi: "x2.test"}}
	// Bayi 9'un varsayılan planı yok (plan 3 bayi 7'nin) → plan zorunlu.
	if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, iki, Ayarlar{})); kod != http.StatusBadRequest || !strings.Contains(g, "hosting planı") {
		t.Errorf("plansız bayi işi reddedilmeli: %d %s", kod, g)
	}
	// Başkasının planı / hostinge özel plan → 403.
	for _, p := range []int64{3, 4} {
		if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, iki, Ayarlar{PlanID: p})); kod != http.StatusForbidden {
			t.Errorf("plan %d bayi 9'a kapalı: %d %s", p, kod, g)
		}
	}
	// Kalan hosting hakkı: max_domain=2, mevcut 1 → 2 site seçilemez.
	o.exec(t, `UPDATE users SET max_domain=2 WHERE id=9`)
	if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, iki, Ayarlar{PlanID: 2})); kod != http.StatusForbidden || !strings.Contains(g, "kalan hak 1") {
		t.Errorf("kota ön denetimi: %d %s", kod, g)
	}
	// Askıdaki bayi başlatamaz.
	o.exec(t, `UPDATE users SET max_domain=0, status='suspended' WHERE id=9`)
	if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, iki, Ayarlar{PlanID: 2})); kod != http.StatusForbidden || !strings.Contains(g, "askıda") {
		t.Errorf("askıdaki bayi: %d %s", kod, g)
	}
	o.exec(t, `UPDATE users SET status='active' WHERE id=9`)
	if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, iki, Ayarlar{PlanID: 1})); kod != http.StatusAccepted {
		t.Errorf("kökün global katalog planıyla başlatabilmeli: %d %s", kod, g)
	}
	// Oturumsuz bayi başlatması (istemci listesi kabul edilmez).
	if kod, _ := o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", o.baslatGovde(0, iki, Ayarlar{})); kod != http.StatusBadRequest {
		t.Errorf("oturumsuz bayi başlatması reddedilmeli: %d", kod)
	}
}

func TestD3_KuyrukTekYuva(t *testing.T) {
	o := d3Kur(t)
	o.bayiAc(t, true)
	kokOt := o.h.oturumKaydet(istekKapsami{}, kaynakGirdi{Tip: "plesk", Host: "93.184.216.34", Port: 22, Kullanici: "root", Parola: "p"}, nil)
	_ = kokOt
	kod, g := o.istek(t, "kok", http.MethodPost, "/tasima/baslat", map[string]any{"tip": "plesk", "host": "93.184.216.34",
		"port": 22, "kullanici": "root", "parola": "kok-pw", "secilen": []Hesap{{AlanAdi: "k1.test", WebRoot: "/var/www/k1"}}})
	if kod != http.StatusAccepted || !strings.Contains(g, `"durum":"calisiyor"`) {
		t.Fatalf("kök işi hemen başlamalı: %d %s", kod, g)
	}
	kokID := <-o.basladi
	// Bayi: kökün işi sürerken 409 DEĞİL kuyruk (sunucu geneli bilgi sızmaz).
	ot := o.bayiOturum(t, 7, []Hesap{{KaynakHesap: "u", AlanAdi: "q.test", WebRoot: "/home/u/q"}})
	kod, g = o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, []Hesap{{AlanAdi: "q.test"}}, Ayarlar{}))
	if kod != http.StatusAccepted || !strings.Contains(g, `"durum":"kuyrukta"`) {
		t.Fatalf("bayi işi kuyruğa girmeli: %d %s", kod, g)
	}
	var y struct {
		IsID int64 `json:"is_id"`
	}
	_ = json.Unmarshal([]byte(g), &y)
	// Bayi 9 da sıraya girer; kök anahtarı kapatınca bayilerin SIRADAKİ işleri iptal.
	ot9 := o.bayiOturum(t, 9, []Hesap{{KaynakHesap: "u", AlanAdi: "r.test", WebRoot: "/home/u/r"}})
	if kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot9, []Hesap{{AlanAdi: "r.test"}}, Ayarlar{PlanID: 1})); kod != http.StatusAccepted {
		t.Fatalf("bayi 9 kuyruk: %d %s", kod, g)
	}
	// Bayi 7 kendi sıradaki işini iptal edebilir; bayi 9'unkini edemez (404).
	var b9 int64
	_ = o.db.QueryRow(`SELECT id FROM tasima_isleri WHERE sahip_rid=9 AND durum='kuyrukta'`).Scan(&b9)
	if kod, _ := o.istek(t, "bayi7", http.MethodPost, fmt.Sprintf("/tasima/%d/iptal", b9), nil); kod != http.StatusNotFound {
		t.Errorf("başkasının sıradaki işi iptal edilememeli: %d", kod)
	}
	o.bayiAc(t, false)
	var d7, d9, p7 sql.NullString
	_ = o.db.QueryRow(`SELECT durum, kaynak_parola FROM tasima_isleri WHERE id=?`, y.IsID).Scan(&d7, &p7)
	_ = o.db.QueryRow(`SELECT durum FROM tasima_isleri WHERE id=?`, b9).Scan(&d9)
	if d7.String != "iptal" || d9.String != "iptal" || p7.Valid {
		t.Errorf("anahtar kapanınca bayi kuyruğu iptal + kimlik silinmeli: %v %v %v", d7, d9, p7)
	}
	var dk string
	_ = o.db.QueryRow(`SELECT durum FROM tasima_isleri WHERE id=?`, kokID).Scan(&dk)
	if dk != "calisiyor" {
		t.Errorf("kökün çalışan işi etkilenmemeli: %s", dk)
	}
	// Kök sıraya bir iş daha koyar; çalışan bitince sıradaki başlar.
	o.bayiAc(t, true)
	ot2 := o.bayiOturum(t, 7, []Hesap{{KaynakHesap: "u", AlanAdi: "s.test", WebRoot: "/home/u/s"}})
	if kod, g := o.istek(t, "bayi7", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot2, []Hesap{{AlanAdi: "s.test"}}, Ayarlar{})); kod != http.StatusAccepted || !strings.Contains(g, "kuyrukta") {
		t.Fatalf("yeniden kuyruk: %d %s", kod, g)
	}
	o.h.yuvaBirak(kokID)
	select {
	case id := <-o.basladi:
		var rid int64
		_ = o.db.QueryRow(`SELECT sahip_rid FROM tasima_isleri WHERE id=?`, id).Scan(&rid)
		if rid != 7 {
			t.Errorf("yuva boşalınca sıradaki bayi işi başlamalı: is=%d rid=%d", id, rid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sıradaki iş başlamadı")
	}
	var temiz sql.NullString
	_ = o.db.QueryRow(`SELECT kaynak_parola FROM tasima_isleri WHERE id=?`, kokID).Scan(&temiz)
	if temiz.Valid {
		t.Errorf("biten işin kimliği silinmeli")
	}
}

// Dağıtıcı başlamadan izni YENİDEN okur: sıradaki bayi işi, kök anahtarı kapattıysa
// kaynağa bağlanmadan iptal edilir.
func TestD3_IsYurut_IzinYenidenOkunur(t *testing.T) {
	o := d3Kur(t)
	id := o.isEkle(t, 7, "calisiyor", "")
	kilit.Lock()
	aktifIsID = id
	kilit.Unlock()
	o.h.isYurut(context.Background(), id) // bayi anahtarı kapalı (varsayılan)
	var durum, hata string
	var parola sql.NullString
	_ = o.db.QueryRow(`SELECT durum, COALESCE(hata,''), kaynak_parola FROM tasima_isleri WHERE id=?`, id).Scan(&durum, &hata, &parola)
	if durum != "iptal" || hata != errTasimaKapali.Error() || parola.Valid {
		t.Errorf("kapalı anahtarda iş iptal + kimlik silinmeli: %s %q %v", durum, hata, parola)
	}
	kilit.Lock()
	bos := aktifIsID == 0
	kilit.Unlock()
	if !bos {
		t.Error("yuva boşalmalı")
	}
}

func TestD3_ArtakalanBayiTemizligi(t *testing.T) {
	o := d3Kur(t)
	yetim := o.isEkle(t, 55, "tamam", "")
	kendi := o.isEkle(t, 7, "tamam", "")
	for _, d := range []string{"55", "7", "abc"} {
		if err := os.MkdirAll(filepath.Join(tasimaKeyDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	o.h.artakalanBayileriTemizle()
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE id IN (?,?)`, yetim, kendi).Scan(&n)
	if n != 1 {
		t.Errorf("yalnız silinmiş bayinin satırları silinmeli: kalan %d", n)
	}
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_kalemleri WHERE is_id=?`, yetim).Scan(&n)
	if n != 0 {
		t.Errorf("kalemler CASCADE silinmeli")
	}
	if _, err := os.Stat(filepath.Join(tasimaKeyDir, "55")); !os.IsNotExist(err) {
		t.Error("silinmiş bayinin anahtar dizini silinmeli")
	}
	for _, d := range []string{"7", "abc"} {
		if _, err := os.Stat(filepath.Join(tasimaKeyDir, d)); err != nil {
			t.Errorf("%s dizini korunmalı: %v", d, err)
		}
	}
}

// Kiracıya özel panel anahtarı: bayinin çifti kökünkünden ve öteki bayininkinden ayrı.
func TestD3_PanelAnahtariKiraciyaOzel(t *testing.T) {
	o := d3Kur(t)
	o.bayiAc(t, true)
	eski := sshKeygen
	sayac := 0
	sshKeygen = func(yol, yorum string) ([]byte, error) {
		sayac++
		if err := os.WriteFile(yol, []byte(fmt.Sprintf("OZEL-%d-%s", sayac, yorum)), 0o600); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(yol+".pub", []byte(fmt.Sprintf("ssh-ed25519 PUB%d %s", sayac, yorum)), 0o644)
	}
	t.Cleanup(func() { sshKeygen = eski })
	pub := map[string]string{}
	for _, kim := range []string{"kok", "bayi7", "bayi9", "bayi7"} {
		kod, g := o.istek(t, kim, http.MethodGet, "/tasima/anahtar", nil)
		if kod != http.StatusOK {
			t.Fatalf("%s anahtar: %d %s", kim, kod, g)
		}
		var y struct {
			Pubkey string `json:"pubkey"`
		}
		_ = json.Unmarshal([]byte(g), &y)
		if onceki, ok := pub[kim]; ok && onceki != y.Pubkey {
			t.Errorf("%s: anahtar her çağrıda yeniden üretilmemeli", kim)
		}
		pub[kim] = y.Pubkey
	}
	if pub["kok"] == pub["bayi7"] || pub["bayi7"] == pub["bayi9"] || !strings.Contains(pub["bayi7"], "-r7") {
		t.Errorf("anahtarlar kiracıya özel olmalı: %v", pub)
	}
	// Bayinin "panel anahtarı" kaynağı kendi özel anahtarını kullanır.
	k := kaynakGirdi{PanelAnahtari: true, Parola: "x"}.kaynak(Kapsam{RID: 7})
	if !strings.Contains(k.Anahtar, "-r7") || k.Parola != "" {
		t.Errorf("bayi panel anahtarı kiracının çifti olmalı: %q", k.Anahtar)
	}
}

// Bayi işinin günlüğü ve kalem notları süzülür (panel yolu, komşu işareti, sır);
// kökünkü ham kalır. Kota hatası kalemi 'atlandi' yapar.
func TestD3_CalistirBayiGunlukSuzgeci(t *testing.T) {
	o := d3Kur(t)
	eski := hesapAktarici
	t.Cleanup(func() { hesapAktarici = eski })
	hesapAktarici = func(_ *Handlers, _ context.Context, k *Kaynak, hs Hesap, _ Ayarlar, kp Kapsam, log func(string, ...any)) (*AktarSonuc, error) {
		log("dosyalar aktariliyor: /var/www/vhosts/%s/httpdocs → /home/c_komsu/public_html", hs.AlanAdi)
		log("ad alanı: sahibi: bayi #9 parola=%s", k.Parola)
		if hs.AlanAdi == "kota.test" {
			return nil, &kotaHatasi{mesaj: "hosting hesabı limitiniz dolu (2/2)"}
		}
		return &AktarSonuc{DomainID: 1, Uyarilar: []string{"SSL: nginx -t /etc/nginx/conf.d/komsu.conf", "Veritabanı yok — taşınmadı"}}, nil
	}
	for _, c := range []struct {
		rid  int64
		ham  bool
		etik string
	}{{7, false, "bayi"}, {0, true, "kök"}} {
		id := o.isEkle(t, c.rid, "calisiyor", "")
		o.exec(t, `DELETE FROM tasima_kalemleri WHERE is_id=?`, id)
		for _, a := range []string{"ok.test", "kota.test"} {
			o.exec(t, `INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum) VALUES (?, 'u', ?, 'bekliyor')`, id, a)
		}
		o.h.calistir(context.Background(), id, &Kaynak{Host: "93.184.216.34", Tip: "plesk", Parola: "gizli-kaynak-pw"},
			[]Hesap{{AlanAdi: "ok.test"}, {AlanAdi: "kota.test"}}, Ayarlar{}, Kapsam{RID: c.rid})
		gunluk := dosyaSonu(logYolu(id), 1<<20)
		var notlar, kotaDurum string
		_ = o.db.QueryRow(`SELECT COALESCE(hata,'') FROM tasima_kalemleri WHERE is_id=? AND alan_adi='ok.test'`, id).Scan(&notlar)
		_ = o.db.QueryRow(`SELECT durum FROM tasima_kalemleri WHERE is_id=? AND alan_adi='kota.test'`, id).Scan(&kotaDurum)
		hepsi := gunluk + "\n" + notlar
		for _, iz := range []string{"/home/c_komsu", "bayi #9", "/etc/nginx", "komsu.conf", "gizli-kaynak-pw", "/var/www"} {
			if c.ham {
				continue
			}
			if strings.Contains(hepsi, iz) {
				t.Errorf("%s: %q bayiye sızdı:\n%s", c.etik, iz, hepsi)
			}
		}
		if c.ham && (!strings.Contains(gunluk, "/home/c_komsu") || !strings.Contains(notlar, "/etc/nginx")) {
			t.Errorf("kök ham ayrıntıyı görmeli:\n%s\n%s", gunluk, notlar)
		}
		if strings.Contains(hepsi, "gizli-kaynak-pw") {
			t.Errorf("%s: kaynak parolası günlüğe yazılmamalı", c.etik)
		}
		if !strings.Contains(notlar, "Veritabanı yok") || kotaDurum != "atlandi" {
			t.Errorf("%s: zararsız not korunmalı, kota kalemi atlandi: %q %q", c.etik, notlar, kotaDurum)
		}
		if !strings.Contains(gunluk, "limitiniz dolu (2/2)") {
			t.Errorf("%s: bayinin kendi kota metni günlükte görünmeli:\n%s", c.etik, gunluk)
		}
	}
}

// isYukle: satır bozulsa/ele geçirilse de bayi işi bayinin kendi hesabına yürür
// (sahip eşlemesi yalnız kök işinde); kök işinin sunucu eşlemesi kuyrukta korunur.
func TestD3_IsYukle_KapsamSatirdan(t *testing.T) {
	o := d3Kur(t)
	bozuk := `{"reseller_id":9,"customer_id":5,"sahipleri_tasi":true,"ustune":true,"plan_id":3,"sahip_esle":{"src":{"r":9,"c":5}}}`
	for _, c := range []struct {
		rid int64
	}{{7}, {0}} {
		res := o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, sahip_rid, ayarlar, kaynak_parola)
			VALUES ('plesk','93.184.216.34','kuyrukta',?,?,?)`, c.rid, bozuk, kimlikSifrele("pw-x", kimlikBaglam(Kapsam{RID: c.rid}, "93.184.216.34")))
		id, _ := res.LastInsertId()
		k, ay, kp, err := o.h.isYukle(id)
		if err != nil {
			t.Fatalf("rid %d: %v", c.rid, err)
		}
		if k.Parola != "pw-x" || kp.RID != c.rid {
			t.Errorf("rid %d: kimlik kapsam bağlamıyla çözülmeli: %q %+v", c.rid, k.Parola, kp)
		}
		if c.rid > 0 {
			if ay.ResellerID != 7 || ay.CustomerID != 0 || ay.SahipleriTasi || ay.Ustune || ay.SahipEsle != nil {
				t.Errorf("bayi işi satırdaki sahip alanlarını yok saymalı: %+v", ay)
			}
		} else if ay.SahipEsle["src"].ResellerID != 9 || ay.ResellerID != 9 {
			t.Errorf("kök işinin sunucu eşlemesi korunmalı: %+v", ay)
		}
	}
}
