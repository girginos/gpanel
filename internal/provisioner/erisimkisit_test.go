package provisioner

// RealIPConfYaz dogrulamasi.
//
// Neden ayri bir test: bu kod yolu AdminOnly bir API ucundan cagriliyor ve
// panelde admin girisi /etc/shadow'daki sistem root parolasina bagli — test
// icin o parolayi kullanmak dogru degil. Kod yolu yine de dogrulanmali, cunku
// erisim kisitlamasi CDN arkasinda BU yapilandirma olmadan sessizce yanlis
// calisir.
//
// Calistirma:
//   GOSP_TEST_DSN='...' go test ./internal/provisioner/ -run RealIP -v

import (
	"database/sql"
	"os"
	"os/exec"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_DSN yok — atlandi")
	}
	d, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("db acilamadi: %v", err)
	}
	if err := d.Ping(); err != nil {
		t.Fatalf("db ping: %v", err)
	}
	return d
}

func nginxSaglam(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		t.Fatalf("nginx -t BOZULDU: %s", strings.TrimSpace(string(out)))
	}
}

func TestRealIPConfYaz(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Testin kendi artigini birakmamasi icin: bastaki durumu geri yukle.
	var eskiBaslik string
	_ = db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='realip_header'`).Scan(&eskiBaslik)
	defer func() {
		_, _ = db.Exec(`DELETE FROM guvenilir_vekil WHERE aciklama LIKE 'GOSPTEST%'`)
		if eskiBaslik != "" {
			_, _ = db.Exec(`UPDATE cp_ayarlar SET deger=? WHERE anahtar='realip_header'`, eskiBaslik)
		}
		_, _ = RealIPConfYaz(db)
	}()

	_, _ = db.Exec(`DELETE FROM guvenilir_vekil`)

	// ── 1) Kayit yokken dosya OLMAMALI (negatif kontrol) ──────────────────
	if _, err := RealIPConfYaz(db); err != nil {
		t.Fatalf("bos durumda hata: %v", err)
	}
	if _, err := os.Stat(realIPConfYolu); !os.IsNotExist(err) {
		t.Fatalf("kayit yokken realip conf DURUYOR — kaldirilmis bir CDN hala guvenilir sayilirdi")
	}
	nginxSaglam(t)

	// ── 2) Cloudflare araliklari + CF-Connecting-IP ───────────────────────
	for _, c := range []string{"173.245.48.0/20", "103.21.244.0/22"} {
		if _, err := db.Exec(`INSERT IGNORE INTO guvenilir_vekil (cidr, aciklama) VALUES (?, 'GOSPTEST cloudflare')`, c); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO cp_ayarlar (anahtar,deger) VALUES ('realip_header','CF-Connecting-IP')
		ON DUPLICATE KEY UPDATE deger=VALUES(deger)`); err != nil {
		t.Fatalf("baslik: %v", err)
	}
	n, err := RealIPConfYaz(db)
	if err != nil {
		t.Fatalf("yazim hatasi: %v", err)
	}
	if n != 2 {
		t.Fatalf("beklenen 2 aralik, donen %d", n)
	}
	b, err := os.ReadFile(realIPConfYolu)
	if err != nil {
		t.Fatalf("conf okunamadi: %v", err)
	}
	icerik := string(b)
	for _, bekle := range []string{
		"set_real_ip_from 173.245.48.0/20;",
		"set_real_ip_from 103.21.244.0/22;",
		"real_ip_header CF-Connecting-IP;",
	} {
		if !strings.Contains(icerik, bekle) {
			t.Errorf("conf'ta eksik: %s", bekle)
		}
	}
	// CF basliginda zincir yok — recursive anlamsiz, yazilmamali.
	if strings.Contains(icerik, "real_ip_recursive") {
		t.Errorf("CF-Connecting-IP ile real_ip_recursive YAZILMAMALI (zincir yok)")
	}
	nginxSaglam(t)

	// ── 3) X-Forwarded-For ise recursive ACIK olmali ──────────────────────
	// Aksi halde istemcinin uydurdugu XFF degeri kabul edilir ve erisim
	// kisitlamasi sahte IP ile atlatilabilir.
	if _, err := db.Exec(`UPDATE cp_ayarlar SET deger='X-Forwarded-For' WHERE anahtar='realip_header'`); err != nil {
		t.Fatalf("baslik guncelleme: %v", err)
	}
	if _, err := RealIPConfYaz(db); err != nil {
		t.Fatalf("ikinci yazim: %v", err)
	}
	b2, _ := os.ReadFile(realIPConfYolu)
	if !strings.Contains(string(b2), "real_ip_recursive on;") {
		t.Errorf("XFF ile real_ip_recursive ACIK olmali (sahte XFF'e karsi)")
	}
	nginxSaglam(t)

	// ── 4) Gecersiz CIDR conf'a SIZMAMALI (config enjeksiyonu) ────────────
	if _, err := db.Exec(`INSERT INTO guvenilir_vekil (cidr, aciklama) VALUES ('1.2.3.4; } evil {', 'GOSPTEST kotucul')`); err != nil {
		t.Fatalf("kotucul insert: %v", err)
	}
	n2, err := RealIPConfYaz(db)
	if err != nil {
		t.Fatalf("uc yazim: %v", err)
	}
	if n2 != 2 {
		t.Errorf("gecersiz kayit ELENMELIYDI: beklenen 2, donen %d", n2)
	}
	b3, _ := os.ReadFile(realIPConfYolu)
	if strings.Contains(string(b3), "evil") {
		t.Errorf("gecersiz deger conf'a SIZDI — config enjeksiyonu")
	}
	nginxSaglam(t)

	// ── 5) Hepsi silinince dosya kaldirilmali ─────────────────────────────
	if _, err := db.Exec(`DELETE FROM guvenilir_vekil`); err != nil {
		t.Fatalf("temizlik: %v", err)
	}
	if _, err := RealIPConfYaz(db); err != nil {
		t.Fatalf("silme yazimi: %v", err)
	}
	if _, err := os.Stat(realIPConfYolu); !os.IsNotExist(err) {
		t.Errorf("tum kayitlar silindi ama conf DURUYOR")
	}
	nginxSaglam(t)
}

// Kural dogrulayicisi: nginx'e ne gecebilir, ne gecemez.
func TestErisimKuralGecerli(t *testing.T) {
	gecerli := []string{"1.2.3.4", "203.0.113.45", "198.51.100.0/24", "2001:db8::1", "2001:db8::/32"}
	gecersiz := []string{
		"", "all", "any", "localhost", "example.com",
		"1.2.3.4; } evil {", "1.2.3.4 evil", "1.2.3.4;", "1.2.3.4\nallow all",
		"999.1.1.1", "1.2.3.4/99", "#yorum",
		strings.Repeat("1", 65),
	}
	for _, g := range gecerli {
		if !erisimKuralGecerli(g) {
			t.Errorf("gecerli sayilmaliydi: %q", g)
		}
	}
	for _, g := range gecersiz {
		if erisimKuralGecerli(g) {
			t.Errorf("REDDEDILMELIYDI: %q", g)
		}
	}
}

// Blok uretimi: beyaz liste / kara liste / bozuk veri korumasi.
func TestErisimBlokUret(t *testing.T) {
	// Kisit pasifse hicbir sey yazilmaz.
	if s := erisimBlokUret(false, "red", []erisimKural{{Tip: "izin", CIDR: "1.2.3.4"}}); s != "" {
		t.Errorf("pasifken blok uretildi: %q", s)
	}
	// Beyaz liste
	s := erisimBlokUret(true, "red", []erisimKural{{Tip: "izin", CIDR: "1.2.3.4"}})
	if !strings.Contains(s, "allow 1.2.3.4;") || !strings.Contains(s, "deny all;") {
		t.Errorf("beyaz liste hatali: %q", s)
	}
	// Kara liste
	s = erisimBlokUret(true, "izin", []erisimKural{{Tip: "red", CIDR: "5.6.7.8"}})
	if !strings.Contains(s, "deny 5.6.7.8;") || !strings.Contains(s, "allow all;") {
		t.Errorf("kara liste hatali: %q", s)
	}
	// 🔴 Veri bozulmasi: kayit VAR ama hicbiri gecerli degil → blok URETILMEZ.
	// Aksi halde tek basina "deny all" kalir ve site TAMAMEN kapanir.
	s = erisimBlokUret(true, "red", []erisimKural{{Tip: "izin", CIDR: "bozuk deger"}})
	if s != "" {
		t.Errorf("tum kurallar gecersizken blok URETILMEMELI, uretilen: %q", s)
	}
	// Liste bastan bos ise bu bir NIYETTIR: "herkesi kapat" gecerli bir ayar.
	s = erisimBlokUret(true, "red", nil)
	if !strings.Contains(s, "deny all;") {
		t.Errorf("bos liste + red = tam kapatma olmaliydi: %q", s)
	}
}

// Guvenilir vekil kapisi — YEREL adres asla guvenilir olamaz.
//
// Olculdu: guvenilir vekil listesinde 127.0.0.0/8 varken, bir kiracinin PHP'si
// kendi kutusundan 127.0.0.1'e baglanip "X-Forwarded-For: <izinli>" gondererek
// KOMSUSUNUN beyaz listesini atladi (403 -> 200). Kutunun ICINDEN uretilebilen
// hicbir adres kimlik kaniti olamaz.
func TestVekilAraligiGecerli(t *testing.T) {
	red := []string{
		// yerel / ozel — kiraci bunlari kendi kutusundan uretebilir
		"127.0.0.1", "127.0.0.0/8", "::1",
		"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12",
		"100.64.0.0/10", "100.127.255.255", // CGNAT
		"169.254.0.0/16", "fe80::1", // link-local
		"0.0.0.0", "0.0.0.0/0", "::/0",
		// cok genis
		"10.0.0.0/7", "128.0.0.0/1", "8.0.0.0/7",
		// gecersiz
		"", "all", "1.2.3.4; } evil {", "255.255.255.255",
	}
	kabul := []string{
		"173.245.48.0/20", "103.21.244.0/22", "104.16.0.0/13", // Cloudflare
		"2400:cb00::/32", "2606:4700::/32",
		"8.8.8.8", "1.1.1.1",
	}
	for _, c := range red {
		if vekilAraligiGecerli(c) {
			t.Errorf("guvenilir vekil olarak REDDEDILMELIYDI: %q", c)
		}
	}
	for _, c := range kabul {
		if !vekilAraligiGecerli(c) {
			t.Errorf("guvenilir vekil olarak kabul edilmeliydi: %q", c)
		}
	}
}

// CIDR normalizasyonu — host bitli yazim sessizce her seyi aciyordu.
//
// nginx bir CIDR'in host bitlerini yok sayar: `allow 1.2.3.4/0` TUM interneti
// kapsar ama panel "sadece 1.2.3.4" gibi gosteriyordu. Kanoniklestirme, panel
// ile nginx'in AYNI seyi soylemesini saglar.
func TestErisimKuralNormalize(t *testing.T) {
	tablo := map[string]string{
		"1.2.3.4/0":         "0.0.0.0/0",
		"192.168.1.55/8":    "192.0.0.0/8",
		"198.51.100.77/24":  "198.51.100.0/24",
		"10.9.8.7/16":       "10.9.0.0/16",
		"203.0.113.45":      "203.0.113.45", // tek adres degismez
		"2a01:4f8:1c::5":    "2a01:4f8:1c::5",
		"2a01:4f8:1c::5/48": "2a01:4f8:1c::/48",
		" 1.2.3.4/24 ":      "1.2.3.0/24", // bosluk kirpilir
	}
	for girdi, bekle := range tablo {
		if got := erisimKuralNormalize(girdi); got != bekle {
			t.Errorf("normalize(%q) = %q, beklenen %q", girdi, got, bekle)
		}
	}
}

// nginx'in reddettigi degerler API'de de reddedilmeli.
//
// `net.ParseIP("255.255.255.255")` gecerli der, nginx demez. Kabul edilseydi o
// domainin HER render'i duserdi — SSL yenilemesi dahil — ve ariza 90 gun sonra
// "TLS hatasi" olarak, sebebinden cok uzakta gorunurdu.
func TestNginxReddettigiDeger(t *testing.T) {
	if erisimKuralGecerli("255.255.255.255") {
		t.Error("255.255.255.255 reddedilmeliydi (nginx: invalid parameter)")
	}
	if !erisimKuralGecerli("255.255.255.254") {
		t.Error("255.255.255.254 gecerli olmaliydi")
	}
}
