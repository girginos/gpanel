package logs

// hedef_test.go — log hedefi (ana / ek alan adı / alt alan): hedef->dosya eşlemesi,
// IDOR, sembolik bağ, geriye uyum, okuma sınırı. DB'li kısım GOSP_TEST_GECICI_DSN ister
// (yoksa atlanır); yalnız kendi açtığı gosp_test_logs_<zaman> veritabanını kullanır.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

func TestDosyaYolu_Esleme(t *testing.T) {
	eski := LogDizin
	LogDizin = "/var/log/nginx"
	defer func() { LogDizin = eski }()
	uzun := strings.Repeat("a", 250) + ".com"
	tablo := []struct {
		ad, anahtar, beklenen string
		hata                  bool
	}{
		{"girginos.cloud", "access", "/var/log/nginx/girginos.cloud.access.log", false},
		{"test5.com", "access", "/var/log/nginx/test5.com.access.log", false},
		{"test5.com", "error", "/var/log/nginx/test5.com.error.log", false},
		{"blog.girginos.cloud", "error", "/var/log/nginx/blog.girginos.cloud.error.log", false},
		{"xn--bcher-kva.example", "access", "/var/log/nginx/xn--bcher-kva.example.access.log", false},
		{"../etc/passwd", "access", "", true},
		{"..", "access", "", true},
		{"a/../../b.com", "access", "", true},
		{"a/b.com", "access", "", true},
		{`a\b.com`, "access", "", true},
		{"a..b.com", "access", "", true},
		{".gizli.com", "access", "", true},
		{"a.com.", "access", "", true},
		{"-a.com", "access", "", true},
		{"a b.com", "access", "", true},
		{"a.com\x00x", "access", "", true},
		{"a.com\n", "access", "", true},
		{"", "access", "", true},
		{uzun, "access", "", true},
		{"test5.com", "../x", "", true},
		{"test5.com", "", "", true},
	}
	for _, c := range tablo {
		p, err := DosyaYolu(c.ad, c.anahtar)
		if c.hata != (err != nil) || p != c.beklenen {
			t.Errorf("DosyaYolu(%q,%q) = %q, %v; beklenen %q hata=%v", c.ad, c.anahtar, p, err, c.beklenen, c.hata)
		}
	}
}

func TestSonNSatir_SembolikBagReddedilir(t *testing.T) {
	d := t.TempDir()
	hedef := filepath.Join(d, "gizli.txt")
	if err := os.WriteFile(hedef, []byte("parola=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bag := filepath.Join(d, "x.com.access.log")
	if err := os.Symlink(hedef, bag); err != nil {
		t.Skip("symlink yok: ", err)
	}
	if _, err := sonNSatir(bag, 10); err == nil {
		t.Fatal("sembolik bağ okundu")
	}
	if _, ok := dosyaBilgi(bag); ok {
		t.Fatal("sembolik bağ var sayıldı")
	}
}

func TestSonNSatir_SinirlarVeKesikSatir(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.com.access.log")
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "satir-%d\n", i)
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o600)
	l, err := sonNSatir(p, 3)
	if err != nil || len(l) != 3 || l[2] != "satir-4999" || l[0] != "satir-4997" {
		t.Fatalf("son 3: %v %v", l, err)
	}
	// Devasa tek satır: bellek penceresi azamiOkumaB ile sınırlı (dosya 3x büyük).
	q := filepath.Join(d, "b.com.access.log")
	_ = os.WriteFile(q, []byte(strings.Repeat("x", 3*azamiOkumaB)+"\nson\n"), 0o600)
	l, err = sonNSatir(q, 200)
	if err != nil {
		t.Fatal(err)
	}
	top := 0
	for _, s := range l {
		top += len(s)
	}
	if top > azamiOkumaB+16 {
		t.Fatalf("okuma penceresi aşıldı: %d bayt", top)
	}
	if l[len(l)-1] != "son" {
		t.Fatalf("son satır yok: %q", l[len(l)-1])
	}
	// Boş dosya
	e := filepath.Join(d, "c.com.access.log")
	_ = os.WriteFile(e, nil, 0o600)
	if l, err := sonNSatir(e, 5); err != nil || len(l) != 0 {
		t.Fatalf("boş dosya: %v %v", l, err)
	}
}

// ---- DB'li HTTP testleri ----

func logGeciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_logs_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		_ = kok.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT 'c_x', is_demo TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE ek_alan_adlari (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, alan_adi VARCHAR(253) NOT NULL)`,
		`CREATE TABLE subdomanlar (id INT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tam_ad VARCHAR(253) NOT NULL)`,
		`INSERT INTO domains (id, alan_adi, is_demo) VALUES (1,'girginos.cloud',0),(2,'baska.test',0),(3,'demo.test',1)`,
		`INSERT INTO ek_alan_adlari VALUES (10,1,'test5.com'),(20,2,'yabanci.com'),(30,1,'kotu..com')`,
		`INSERT INTO subdomanlar VALUES (100,1,'blog.girginos.cloud'),(200,2,'x.baska.test')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	return db
}

func logOrtam(t *testing.T) http.Handler {
	t.Helper()
	db := logGeciciDB(t)
	eski := LogDizin
	LogDizin = t.TempDir()
	t.Cleanup(func() { LogDizin = eski })
	yaz := func(ad, icerik string) { _ = os.WriteFile(filepath.Join(LogDizin, ad), []byte(icerik), 0o600) }
	yaz("girginos.cloud.access.log", "ANA-1\nANA-2\n")
	yaz("girginos.cloud.error.log", "ANA-ERR\n")
	yaz("test5.com.access.log", "EK-1\nEK-2\nEK-3\n")
	yaz("blog.girginos.cloud.access.log", "ALT-1\n")
	yaz("yabanci.com.access.log", "YABANCI-SIR\n")
	yaz("kotu..com.access.log", "KOTU\n")
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/domains/{id}/log-hedefleri", h.Hedefler)
	r.Get("/domains/{id}/logs", h.List)
	r.Get("/domains/{id}/logs/oku", h.Read)
	r.Get("/domains/{id}/subdomain/{sid}/logs/oku", h.Read)
	return r
}

func logGet(h http.Handler, yol string) (int, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, yol, nil))
	return rec.Code, rec.Body.String()
}

func TestHedefler_Liste(t *testing.T) {
	h := logOrtam(t)
	kod, govde := logGet(h, "/domains/1/log-hedefleri")
	if kod != 200 {
		t.Fatalf("kod %d %s", kod, govde)
	}
	var l []LogHedefi
	if err := json.Unmarshal([]byte(govde), &l); err != nil {
		t.Fatal(err)
	}
	var adlar []string
	for _, x := range l {
		adlar = append(adlar, x.Hedef+"="+x.AlanAdi)
	}
	if got := strings.Join(adlar, ","); got != "ana=girginos.cloud,ek:10=test5.com,ek:30=kotu..com,alt:100=blog.girginos.cloud" {
		t.Fatalf("liste: %s", got)
	}
	if !l[0].AccessVar || !l[0].ErrorVar || !l[1].AccessVar || l[1].ErrorVar || l[1].AccessBoyut != int64(len("EK-1\nEK-2\nEK-3\n")) {
		t.Fatalf("var/boyut: %+v", l)
	}
	if strings.Contains(govde, os.TempDir()) || strings.Contains(govde, "/var/log") || strings.Contains(govde, "YABANCI") || strings.Contains(govde, "yabanci.com") || strings.Contains(govde, "yol") {
		t.Fatalf("yol ya da yabancı hesap sızdı: %s", govde)
	}
	// log dosyası olmayan hedef: "log yok"
	_ = os.Remove(filepath.Join(LogDizin, "test5.com.access.log"))
	_, govde = logGet(h, "/domains/1/log-hedefleri")
	_ = json.Unmarshal([]byte(govde), &l)
	if l[1].AccessVar || l[1].AccessBoyut != 0 || l[1].SonDegisiklik != "" {
		t.Fatalf("log yok: %+v", l[1])
	}
	if kod, _ := logGet(h, "/domains/3/log-hedefleri"); kod != 403 {
		t.Fatalf("demo: %d", kod)
	}
	if kod, _ := logGet(h, "/domains/999/log-hedefleri"); kod != 404 {
		t.Fatalf("yok: %d", kod)
	}
}

func TestRead_HedefOkuma(t *testing.T) {
	h := logOrtam(t)
	tablo := []struct{ yol, icerir string }{
		{"/domains/1/logs/oku", "ANA-2"}, // geriye uyum: hedef yok = ana
		{"/domains/1/logs/oku?hedef=ana", "ANA-2"},
		{"/domains/1/logs/oku?hedef=ek:10", "EK-3"},
		{"/domains/1/logs/oku?hedef=alt:100", "ALT-1"},
		{"/domains/1/subdomain/100/logs/oku", "ALT-1"}, // eski alt alan yolu
		{"/domains/1/logs/oku?hedef=ana&dosya=error", "ANA-ERR"},
	}
	for _, c := range tablo {
		kod, g := logGet(h, c.yol)
		if kod != 200 || !strings.Contains(g, c.icerir) {
			t.Errorf("%s: %d %s", c.yol, kod, g)
		}
		if strings.Contains(g, `"yol"`) || strings.Contains(g, LogDizin) {
			t.Errorf("%s: yol sızdı: %s", c.yol, g)
		}
	}
	// ek alanın access'i ana log içeriğini içermemeli
	if _, g := logGet(h, "/domains/1/logs/oku?hedef=ek:10"); strings.Contains(g, "ANA-") {
		t.Errorf("ek hedefi ana log döndü: %s", g)
	}
	// log dosyası yok: 200 + mevcut=false
	if kod, g := logGet(h, "/domains/1/logs/oku?hedef=ek:10&dosya=error"); kod != 200 || !strings.Contains(g, `"mevcut":false`) {
		t.Errorf("log yok: %d %s", kod, g)
	}
	// son sınırı
	if _, g := logGet(h, "/domains/1/logs/oku?hedef=ek:10&son=1"); strings.Contains(g, "EK-1") || !strings.Contains(g, "EK-3") {
		t.Errorf("son=1: %s", g)
	}
}

func TestRead_IDOR(t *testing.T) {
	h := logOrtam(t)
	yabanci := []string{
		"/domains/1/logs/oku?hedef=ek:20",      // başka hesabın ek alanı
		"/domains/1/logs/oku?hedef=alt:200",    // başka hesabın alt alanı
		"/domains/1/subdomain/200/logs/oku",    // eski yol, başka hesabın alt alanı
		"/domains/1/logs/oku?hedef=ek:999",     // olmayan
		"/domains/1/log-hedefleri?hedef=ek:20", // (parametre yok sayılır, sızıntı yok)
	}
	_, olmayan := logGet(h, "/domains/1/logs/oku?hedef=ek:999")
	for _, y := range yabanci[:4] {
		kod, g := logGet(h, y)
		if kod != 404 || strings.Contains(g, "YABANCI") || g != olmayan {
			t.Errorf("%s: kod=%d (404 ve olmayanla aynı yanıt bekleniyor) %s", y, kod, g)
		}
	}
	if _, g := logGet(h, yabanci[4]); strings.Contains(g, "yabanci") {
		t.Errorf("hedef listesinde yabancı: %s", g)
	}
	// ana alan adı başka hesabın id'siyle de karışmaz
	if _, g := logGet(h, "/domains/2/logs/oku?hedef=ek:10"); strings.Contains(g, "EK-") {
		t.Errorf("hesap 2 hesap 1'in ek logunu okudu: %s", g)
	}
}

func TestRead_GecersizHedefVeYolKacisi(t *testing.T) {
	h := logOrtam(t)
	for _, y := range []string{
		"/domains/1/logs/oku?hedef=ek:abc", "/domains/1/logs/oku?hedef=ek:-1", "/domains/1/logs/oku?hedef=x:1",
		"/domains/1/logs/oku?hedef=../../etc/passwd", "/domains/1/logs/oku?hedef=ek:10:1&x=1",
		"/domains/1/logs/oku?hedef=alt:1%00",
		"/domains/1/subdomain/100/logs/oku?hedef=ek:10", // hem sid hem hedef
	} {
		if kod, g := logGet(h, y); kod != 400 {
			t.Errorf("%s: %d %s", y, kod, g)
		}
	}
	// DB'deki alan adı bozuksa (kotu..com) dosya adı doğrulaması reddeder, yol sızmaz
	if kod, g := logGet(h, "/domains/1/logs/oku?hedef=ek:30"); kod != 400 || strings.Contains(g, "KOTU") {
		t.Errorf("bozuk ad okundu: %d %s", kod, g)
	}
	if kod, _ := logGet(h, "/domains/1/logs/oku?hedef=ek:10&dosya=../../etc/passwd"); kod != 400 {
		t.Errorf("dosya anahtarı kaçışı: %d", kod)
	}
}

func TestRead_SembolikBagHedefte(t *testing.T) {
	h := logOrtam(t)
	disari := filepath.Join(t.TempDir(), "sir.txt")
	_ = os.WriteFile(disari, []byte("DISARIDAKI-SIR\n"), 0o600)
	p := filepath.Join(LogDizin, "test5.com.access.log")
	_ = os.Remove(p)
	if err := os.Symlink(disari, p); err != nil {
		t.Skip("symlink yok")
	}
	kod, g := logGet(h, "/domains/1/logs/oku?hedef=ek:10")
	if kod == 200 && strings.Contains(g, "DISARIDAKI-SIR") {
		t.Fatalf("sembolik bağ üzerinden dış dosya okundu: %s", g)
	}
	if strings.Contains(g, "sir.txt") || strings.Contains(g, LogDizin) {
		t.Fatalf("yol sızdı: %s", g)
	}
}
