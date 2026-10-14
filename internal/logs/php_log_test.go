package logs

// php_log_test.go — Günlükler "PHP (yazılım hataları)": hesabın PHP-FPM günlüğü
// (/var/log/php-fpm-<sk>/tenant.log). Yol eşleme, hesap sınırı (IDOR), sembolik bağ,
// hedeften bağımsızlık, dosya yokken davranış, bozuk sistem kullanıcısı.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPHPDosyaYolu_Esleme(t *testing.T) {
	eski := PHPLogKok
	PHPLogKok = "/var/log"
	defer func() { PHPLogKok = eski }()
	tablo := []struct {
		sk, beklenen string
		hata         bool
	}{
		{"c_ornek_net", "/var/log/php-fpm-c_ornek_net/tenant.log", false},
		{"c_a1", "/var/log/php-fpm-c_a1/tenant.log", false},
		{"", "", true},
		{"root", "", true},
		{"c_", "", true},
		{"C_BUYUK", "", true},
		{"c_../x", "", true},
		{"c_a/b", "", true},
		{"c_a.b", "", true},
		{"c_a\x00", "", true},
		{"c_a\n", "", true},
		{"c_" + strings.Repeat("a", 61), "", true},
		{"nginx", "", true},
	}
	for _, c := range tablo {
		p, err := PHPDosyaYolu(c.sk)
		if c.hata != (err != nil) || p != c.beklenen {
			t.Errorf("PHPDosyaYolu(%q) = %q, %v; beklenen %q hata=%v", c.sk, p, err, c.beklenen, c.hata)
		}
	}
	// "php" dışındaki anahtarlar nginx yoluna gider (eski davranış korunur)
	if p, err := dosyaYolu("a.com", "c_x", "error"); err != nil || !strings.HasSuffix(p, "a.com.error.log") {
		t.Errorf("error anahtarı: %q %v", p, err)
	}
	if _, err := dosyaYolu("a.com", "c_x", "../php"); err == nil {
		t.Errorf("geçersiz anahtar kabul edildi")
	}
}

// phpOrtam: geçici DB + geçici nginx/PHP günlük kökleri. Hesap 1 = c_x, hesap 2 = c_baska.
func phpOrtam(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db := logGeciciDB(t)
	if _, err := db.Exec(`UPDATE domains SET sistem_kullanici='c_baska' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	eskiL, eskiP := LogDizin, PHPLogKok
	LogDizin, PHPLogKok = t.TempDir(), t.TempDir()
	t.Cleanup(func() { LogDizin, PHPLogKok = eskiL, eskiP })
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/domains/{id}/logs", h.List)
	r.Get("/domains/{id}/logs/oku", h.Read)
	r.Get("/domains/{id}/subdomain/{sid}/logs/oku", h.Read)
	return r, db
}

func phpLogYaz(t *testing.T, sk, icerik string) {
	t.Helper()
	d := filepath.Join(PHPLogKok, "php-fpm-"+sk)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "tenant.log"), []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPHPLog_OkumaListeVeHesapSiniri(t *testing.T) {
	h, _ := phpOrtam(t)
	phpLogYaz(t, "c_x", "[12-Oct-2026 12:24:19] WARNING: [pool c_x] child 7 said into stderr: \"PHP message: PHP Fatal error:  Uncaught Error in /home/c_x/public_html/a.php:3\"\nPHP-SON\n")
	phpLogYaz(t, "c_baska", "BASKA-HESAP-SIR\n")

	// okuma: hedeften bağımsız (ana / ek / alt aynı hesap günlüğü)
	for _, y := range []string{
		"/domains/1/logs/oku?dosya=php",
		"/domains/1/logs/oku?hedef=ek:10&dosya=php",
		"/domains/1/logs/oku?hedef=alt:100&dosya=php",
		"/domains/1/subdomain/100/logs/oku?dosya=php",
	} {
		kod, g := logGet(h, y)
		if kod != 200 || !strings.Contains(g, "PHP Fatal error") || !strings.Contains(g, "PHP-SON") || !strings.Contains(g, `"mevcut":true`) {
			t.Errorf("%s: %d %s", y, kod, g)
		}
		if strings.Contains(g, "BASKA-HESAP") || strings.Contains(g, PHPLogKok) || strings.Contains(g, "tenant.log") {
			t.Errorf("%s: başka hesap ya da yol sızdı: %s", y, g)
		}
	}
	// hesap 2 yalnız kendi günlüğünü görür
	if _, g := logGet(h, "/domains/2/logs/oku?dosya=php"); !strings.Contains(g, "BASKA-HESAP-SIR") || strings.Contains(g, "PHP-SON") {
		t.Errorf("hesap 2: %s", g)
	}
	// başka hesabın alt alanı üzerinden PHP günlüğü okunamaz (IDOR)
	if kod, g := logGet(h, "/domains/1/logs/oku?hedef=alt:200&dosya=php"); kod != 404 || strings.Contains(g, "BASKA") {
		t.Errorf("IDOR: %d %s", kod, g)
	}
	// liste: üçüncü giriş "php", boyut gerçek, yol yok
	kod, g := logGet(h, "/domains/1/logs")
	var l []LogDosya
	if kod != 200 || json.Unmarshal([]byte(g), &l) != nil || len(l) != 3 || l[2].Anahtar != "php" || !l[2].Mevcut || l[2].BoyutB == 0 {
		t.Fatalf("liste: %d %s", kod, g)
	}
	if strings.Contains(g, PHPLogKok) {
		t.Fatalf("liste yol sızdırdı: %s", g)
	}
	// dosya yok (paylaşılan havuzdaki hesap): 200 + mevcut=false
	_ = os.RemoveAll(filepath.Join(PHPLogKok, "php-fpm-c_x"))
	if kod, g := logGet(h, "/domains/1/logs/oku?dosya=php"); kod != 200 || !strings.Contains(g, `"mevcut":false`) {
		t.Errorf("günlük yok: %d %s", kod, g)
	}
	// demo hesabın PHP günlüğü de yönetilemez
	if kod, _ := logGet(h, "/domains/3/logs/oku?dosya=php"); kod != 403 {
		t.Errorf("demo: %d", kod)
	}
}

func TestPHPLog_SembolikBagVeBozukKullanici(t *testing.T) {
	h, db := phpOrtam(t)
	disari := filepath.Join(t.TempDir(), "shadow")
	_ = os.WriteFile(disari, []byte("DISARIDAKI-SIR\n"), 0o600)
	d := filepath.Join(PHPLogKok, "php-fpm-c_x")
	_ = os.MkdirAll(d, 0o755)
	if err := os.Symlink(disari, filepath.Join(d, "tenant.log")); err != nil {
		t.Skip("symlink yok")
	}
	kod, g := logGet(h, "/domains/1/logs/oku?dosya=php")
	if kod == 200 && strings.Contains(g, "DISARIDAKI-SIR") {
		t.Fatalf("sembolik bağ üzerinden dış dosya okundu: %s", g)
	}
	if strings.Contains(g, "shadow") || strings.Contains(g, PHPLogKok) {
		t.Fatalf("yol sızdı: %s", g)
	}
	// DB'deki sistem kullanıcısı bozuksa (yol kaçışı) 400, dosyaya gidilmez
	if _, err := db.Exec(`UPDATE domains SET sistem_kullanici='c_x/../../etc' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if kod, g := logGet(h, "/domains/1/logs/oku?dosya=php"); kod != 400 || strings.Contains(g, "etc") {
		t.Errorf("bozuk sk: %d %s", kod, g)
	}
}
