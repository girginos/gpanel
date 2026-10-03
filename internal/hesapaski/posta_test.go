package hesapaski

// posta_test.go — askının posta adımı: GİRİŞ kapanır, gelen posta açık (eklenti
// tarafı). Gerçek geçici MariaDB + sahte mail eklentisi (UNIX soketinde HTTP).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

type pa_istek struct {
	Ad     string `json:"ad"`
	Askida bool   `json:"askida"`
	rol    string
}

type pa_eklenti struct {
	mu       sync.Mutex
	istekler []pa_istek
	posta    map[string]bool // posta kurulu alan adları
	kod      int             // 0 = 200
}

// pa_kur — sahte eklenti soketi + cp_eklentiler/subdomanlar tabloları.
func pa_kur(t *testing.T, db *sql.DB, aktif int) (*pa_eklenti, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "pa") // UNIX soket yolu kısa olmalı (108)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	soket := filepath.Join(dir, "m.sock")
	e := &pa_eklenti{posta: map[string]bool{}}
	ln, err := net.Listen("unix", soket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /domainler/aski", func(w http.ResponseWriter, r *http.Request) {
		var g pa_istek
		_ = json.NewDecoder(r.Body).Decode(&g)
		g.rol = r.Header.Get("X-Gosp-Rol")
		e.mu.Lock()
		e.istekler = append(e.istekler, g)
		kod := e.kod
		var pv bool
		pv = e.posta[g.Ad]
		e.mu.Unlock()
		if kod != 0 {
			w.WriteHeader(kod)
			_ = json.NewEncoder(w).Encode(map[string]string{"hata": "işlem başarısız"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ad": g.Ad, "askida": g.Askida, "posta": pv})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0, soket VARCHAR(255) NULL)`,
		`CREATE TABLE IF NOT EXISTS subdomanlar (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tam_ad VARCHAR(253) NOT NULL)`,
		`INSERT INTO subdomanlar (domain_id, tam_ad) VALUES (1,'blog.ha1.test'),(1,'Shop.HA1.test')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO cp_eklentiler (ad, aktif, soket) VALUES ('mail',?,?)`, aktif, soket); err != nil {
		t.Fatal(err)
	}
	return e, soket
}

func pa_etkin(t *testing.T, v bool) {
	t.Helper()
	eski := postaAltyapiEtkin
	postaAltyapiEtkin = func() bool { return v }
	t.Cleanup(func() { postaAltyapiEtkin = eski })
}

func (e *pa_eklenti) adlar() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var a []string
	for _, i := range e.istekler {
		a = append(a, i.Ad)
	}
	sort.Strings(a)
	return strings.Join(a, ",")
}

// Askıya alma: hesabın ana + alt alan adlarının hepsi için eklenti yönetici
// rolüyle çağrılır; dovecot askıyı uyguluyorsa nil.
func TestPostaAski_AnaVeAltAlanlar(t *testing.T) {
	db := ha_db(t)
	e, _ := pa_kur(t, db, 1)
	e.posta["ha1.test"] = true
	pa_etkin(t, true)
	if err := PostaAski(context.Background(), db, 1, true); err != nil {
		t.Fatalf("askıya alma: %v", err)
	}
	if got := e.adlar(); got != "blog.ha1.test,ha1.test,shop.ha1.test" {
		t.Fatalf("ana + alt alan adlarının hepsi: %s", got)
	}
	for _, i := range e.istekler {
		if !i.Askida || i.rol != "admin" {
			t.Fatalf("askida=true ve X-Gosp-Rol: admin olmalı: %+v", i)
		}
	}
	e.istekler = nil
	if err := PostaAski(context.Background(), db, 1, false); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	for _, i := range e.istekler {
		if i.Askida {
			t.Fatalf("askıdan almada askida=false: %+v", i)
		}
	}
}

// Bayrak yazıldı ama dovecot ona bakmıyor → "kapandı" denmez, hata.
func TestPostaAski_DovecotUygulamiyorsaHata(t *testing.T) {
	db := ha_db(t)
	e, _ := pa_kur(t, db, 1)
	e.posta["ha1.test"] = true
	pa_etkin(t, false)
	if err := PostaAski(context.Background(), db, 1, true); err == nil || !strings.Contains(err.Error(), "dovecot") {
		t.Fatalf("dovecot askıyı uygulamıyorsa hata beklenirdi: %v", err)
	}
	// Posta kurulu alan adı yoksa doğrulamaya gerek yok.
	e.posta = map[string]bool{}
	if err := PostaAski(context.Background(), db, 1, true); err != nil {
		t.Fatalf("postasız hesapta hata olmamalı: %v", err)
	}
	// Açarken doğrulama yok.
	e.posta["ha1.test"] = true
	if err := PostaAski(context.Background(), db, 1, false); err != nil {
		t.Fatalf("askıdan almada dovecot doğrulaması yok: %v", err)
	}
}

func TestPostaAski_EklentiYoksaNil(t *testing.T) {
	db := ha_db(t)
	if _, err := db.Exec(`CREATE TABLE cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0, soket VARCHAR(255) NULL)`); err != nil {
		t.Fatal(err)
	}
	pa_etkin(t, false)
	if err := PostaAski(context.Background(), db, 1, true); err != nil {
		t.Fatalf("mail eklentisi yoksa nil: %v", err)
	}
}

// Soket hatası (eklenti çökmüş) → hata: hesapaski KismiHata sayar.
func TestPostaAski_SoketHatasi(t *testing.T) {
	db := ha_db(t)
	_, soket := pa_kur(t, db, 1)
	if _, err := db.Exec(`UPDATE cp_eklentiler SET soket=? WHERE ad='mail'`, soket+".yok"); err != nil {
		t.Fatal(err)
	}
	pa_etkin(t, true)
	if err := PostaAski(context.Background(), db, 1, true); err == nil || !strings.Contains(err.Error(), "ulaşılamadı") {
		t.Fatalf("soket hatası raporlanmalı: %v", err)
	}
	// Eklenti KAPALI (aktif=0) iken soket hatası yutulur.
	if _, err := db.Exec(`UPDATE cp_eklentiler SET aktif=0 WHERE ad='mail'`); err != nil {
		t.Fatal(err)
	}
	if err := PostaAski(context.Background(), db, 1, true); err != nil {
		t.Fatalf("kapalı eklentide hata yutulmalı: %v", err)
	}
}

func TestPostaAski_EklentiHatasi(t *testing.T) {
	db := ha_db(t)
	e, _ := pa_kur(t, db, 1)
	pa_etkin(t, true)
	e.kod = http.StatusInternalServerError
	if err := PostaAski(context.Background(), db, 1, true); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("eklenti hatası raporlanmalı: %v", err)
	}
	// Eski eklenti (askı ucu yok): kapatırken hata, açarken nil.
	e.kod = http.StatusMethodNotAllowed
	if err := PostaAski(context.Background(), db, 1, true); err == nil || !strings.Contains(err.Error(), "desteklemiyor") {
		t.Fatalf("eski eklenti askıya almada raporlanmalı: %v", err)
	}
	if err := PostaAski(context.Background(), db, 1, false); err != nil {
		t.Fatalf("eski eklenti askıdan almayı engellememeli: %v", err)
	}
}

// Uygula düzeyi: posta adımı düşerse hesap askıda kalır (KismiHata), askıdan
// alma yarım kalırsa yeniden askıya alınır.
func TestUygula_PostaHatasi_GuvenliTaraf(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"posta(true)": errors.New("mail eklentisine ulaşılamadı")}}
	_, err := k.adimlar(t).Uygula(context.Background(), db, 1, true, KaynakKok)
	var kh *KismiHata
	if !errors.As(err, &kh) || !kh.Askida || !strings.Contains(err.Error(), "posta girişi") {
		t.Fatalf("askıda kalan KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d.askida != 1 {
		t.Fatalf("hesap askıda kalmalı: %+v", d)
	}
	k2 := &ha_kayit{db: db, hata: map[string]error{"posta(false)": errors.New("HTTP 500")}}
	_, err = k2.adimlar(t).Uygula(context.Background(), db, 1, false, KaynakKok)
	if !errors.As(err, &kh) || !kh.Askida {
		t.Fatalf("askıdan alma yarım: yeniden askıya alınmış KismiHata beklenirdi: %v", err)
	}
	if !strings.Contains(strings.Join(k2.cagri, " "), "posta(true)") {
		t.Fatalf("posta yeniden kapatılmalı: %v", k2.cagri)
	}
}

// Üretim adımları posta adımını gerçekten bağlar (sahte değil).
func TestVarsayilan_PostaAdimiBagli(t *testing.T) {
	if reflect.ValueOf(Varsayilan().Posta).Pointer() != reflect.ValueOf(PostaAski).Pointer() {
		t.Fatal("Varsayilan().Posta üretimde PostaAski olmalı")
	}
}
