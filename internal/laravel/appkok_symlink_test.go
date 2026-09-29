package laravel

// appkok_symlink_test.go — OtomatikHazirla'nın root dosya işlemleri için regresyon
// testleri. Eskiden .env.example root olarak os.ReadFile ile (symlink izleyerek) okunup
// tenant'ın .env'ine yazılıyordu; .env os.Chmod, bootstrap/cache os.Remove ve chown -R ile
// yol üzerinden işleniyordu. Testler t.TempDir içinde sahte bir ev kökü ("home") ve onun
// DIŞINDA bir "dis" dizini (başka tenant / sistem dosyalarını temsil eder) kurar.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const gizliIcerik = "GIZLI_PANEL_SIRRI=cok-gizli"

type sahteEv struct {
	home string // /home/c_x karşılığı
	dis  string // ev kökünün DIŞI
	app  string // home/public_html (mutlak)
}

func sahteEvKur(t *testing.T) sahteEv {
	t.Helper()
	kok := t.TempDir()
	e := sahteEv{
		home: filepath.Join(kok, "home", "c_test"),
		dis:  filepath.Join(kok, "dis"),
	}
	e.app = filepath.Join(e.home, "public_html")
	for _, d := range []string{e.app, e.dis} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func dosyaYaz(t *testing.T, p, icerik string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(icerik), mode); err != nil {
		t.Fatal(err)
	}
}

func symlinkKur(t *testing.T, hedef, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hedef, link); err != nil {
		t.Fatal(err)
	}
}

func kokGerekli(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("chown/sahiplik testi root gerektirir")
	}
}

// yazKaydedici: envYaz yerine geçer; çağrılırsa içeriği saklar.
type yazKaydedici struct {
	cagrildi bool
	icerik   string
}

func (y *yazKaydedici) yaz(s string) error {
	y.cagrildi = true
	y.icerik = s
	return nil
}

// .env.example ev DIŞINA symlink (mutlak/göreli), ev İÇİNDE başka dosyaya symlink ya da
// uygulama dizininin ara bileşeni dışarı giden symlink ise içerik ASLA .env'e geçmemeli.
func TestEnvOrnektenUret_SymlinkSizdirmaz(t *testing.T) {
	uid := os.Getuid()
	durumlar := []struct {
		ad     string
		kur    func(t *testing.T, e sahteEv) string // appRel döner
		notAlt string
	}{
		{"mutlak symlink ev disina", func(t *testing.T, e sahteEv) string {
			dosyaYaz(t, filepath.Join(e.dis, "sir.env"), gizliIcerik, 0o600)
			symlinkKur(t, filepath.Join(e.dis, "sir.env"), filepath.Join(e.app, ".env.example"))
			return "public_html"
		}, "okunamadı"},
		{"goreli symlink ev disina", func(t *testing.T, e sahteEv) string {
			dosyaYaz(t, filepath.Join(e.dis, "sir.env"), gizliIcerik, 0o600)
			symlinkKur(t, "../../../dis/sir.env", filepath.Join(e.app, ".env.example"))
			return "public_html"
		}, "okunamadı"},
		{"ev icinde baska dosyaya symlink", func(t *testing.T, e sahteEv) string {
			dosyaYaz(t, filepath.Join(e.home, ".gpanel", "kok.conf"), gizliIcerik, 0o600)
			symlinkKur(t, "../.gpanel/kok.conf", filepath.Join(e.app, ".env.example"))
			return "public_html"
		}, "okunamadı"},
		{"ara bilesen symlink ev disina", func(t *testing.T, e sahteEv) string {
			dosyaYaz(t, filepath.Join(e.dis, "baska", ".env.example"), gizliIcerik, 0o644)
			symlinkKur(t, filepath.Join(e.dis, "baska"), filepath.Join(e.app, "uyg"))
			return "public_html/uyg"
		}, "açılamadı"},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			e := sahteEvKur(t)
			appRel := d.kur(t, e)
			var y yazKaydedici
			calisti, ok, not := envOrnektenUret(e.home, appRel, uid, y.yaz)
			if !calisti || ok {
				t.Fatalf("calisti=%v ok=%v not=%q; symlink'li .env.example reddedilmeliydi", calisti, ok, not)
			}
			if y.cagrildi {
				t.Fatalf("yaz() çağrıldı; içerik sızdı: %q", y.icerik)
			}
			if !strings.Contains(not, d.notAlt) {
				t.Fatalf("not=%q, %q içermeliydi", not, d.notAlt)
			}
		})
	}
}

// .env.example kiracıya ait değilse (ör. hardlink/göç kalıntısı root dosyası) okunmamalı.
func TestEnvOrnektenUret_SahibiKiraciDegil(t *testing.T) {
	kokGerekli(t)
	e := sahteEvKur(t)
	p := filepath.Join(e.app, ".env.example")
	dosyaYaz(t, p, gizliIcerik, 0o600)
	if err := os.Chown(p, 4242, 4242); err != nil {
		t.Fatal(err)
	}
	var y yazKaydedici
	calisti, ok, not := envOrnektenUret(e.home, "public_html", 0, y.yaz)
	if !calisti || ok || y.cagrildi || !strings.Contains(not, "kiracıya ait değil") {
		t.Fatalf("calisti=%v ok=%v cagrildi=%v not=%q; başka uid'nin dosyası reddedilmeliydi", calisti, ok, y.cagrildi, not)
	}
	// Aynı dosya kiracının (4242) uid'siyle meşru.
	var y2 yazKaydedici
	if _, ok, not := envOrnektenUret(e.home, "public_html", 4242, y2.yaz); !ok || y2.icerik != gizliIcerik {
		t.Fatalf("meşru sahip reddedildi: ok=%v not=%q", ok, not)
	}
}

// FIFO/dizin gibi düzenli olmayan .env.example reddedilmeli (asılı kalmamalı).
func TestEnvOrnektenUret_DuzenliDegil(t *testing.T) {
	e := sahteEvKur(t)
	if err := unix.Mkfifo(filepath.Join(e.app, ".env.example"), 0o644); err != nil {
		t.Fatal(err)
	}
	var y yazKaydedici
	_, ok, not := envOrnektenUret(e.home, "public_html", os.Getuid(), y.yaz)
	if ok || y.cagrildi || !strings.Contains(not, "düzenli bir dosya değil") {
		t.Fatalf("ok=%v cagrildi=%v not=%q; FIFO reddedilmeliydi", ok, y.cagrildi, not)
	}
}

// Meşru akış: .env yok, .env.example kiracının düzenli dosyası → içerik yazılır.
func TestEnvOrnektenUret_Mesru(t *testing.T) {
	e := sahteEvKur(t)
	ornek := "APP_NAME=Laravel\nAPP_KEY=\nAPP_URL=http://localhost\n"
	dosyaYaz(t, filepath.Join(e.app, ".env.example"), ornek, 0o644)
	var y yazKaydedici
	calisti, ok, not := envOrnektenUret(e.home, "public_html", os.Getuid(), y.yaz)
	if !calisti || !ok || y.icerik != ornek || not != ".env.example kopyalandı" {
		t.Fatalf("calisti=%v ok=%v not=%q icerik=%q", calisti, ok, not, y.icerik)
	}
}

// .env zaten varsa (symlink dahil) dokunulmamalı; .env.example yoksa açık not.
func TestEnvOrnektenUret_EnvVarVeOrnekYok(t *testing.T) {
	e := sahteEvKur(t)
	dosyaYaz(t, filepath.Join(e.app, ".env.example"), "APP_NAME=x\n", 0o644)
	symlinkKur(t, filepath.Join(e.dis, "yok"), filepath.Join(e.app, ".env")) // kırık symlink
	var y yazKaydedici
	if calisti, _, _ := envOrnektenUret(e.home, "public_html", os.Getuid(), y.yaz); calisti || y.cagrildi {
		t.Fatalf("mevcut .env varken üretim çalıştı (calisti=%v cagrildi=%v)", calisti, y.cagrildi)
	}

	e2 := sahteEvKur(t)
	var y2 yazKaydedici
	calisti, ok, not := envOrnektenUret(e2.home, "public_html", os.Getuid(), y2.yaz)
	if !calisti || ok || y2.cagrildi || not != ".env.example yok" {
		t.Fatalf("calisti=%v ok=%v not=%q", calisti, ok, not)
	}
}

// envOku çekirdeği: .env symlink (leaf ya da ara bileşen) reddi + boyut tavanı.
func TestEvAltiDosyaOku_SymlinkVeBoyut(t *testing.T) {
	e := sahteEvKur(t)
	dosyaYaz(t, filepath.Join(e.dis, "sir.env"), gizliIcerik, 0o600)
	symlinkKur(t, filepath.Join(e.dis, "sir.env"), filepath.Join(e.app, ".env"))
	if b, err := evAltiDosyaOku(e.home, "public_html/.env", -1, envMaxBoyut); err == nil {
		t.Fatalf(".env symlink'i izlendi: %q", b)
	}
	dosyaYaz(t, filepath.Join(e.dis, "d2", ".env"), gizliIcerik, 0o600)
	symlinkKur(t, filepath.Join(e.dis, "d2"), filepath.Join(e.app, "ara"))
	if b, err := evAltiDosyaOku(e.home, "public_html/ara/.env", -1, envMaxBoyut); err == nil {
		t.Fatalf("ara bileşen symlink'i izlendi: %q", b)
	}
	dosyaYaz(t, filepath.Join(e.app, "buyuk", ".env"), strings.Repeat("A", 100), 0o600)
	if _, err := evAltiDosyaOku(e.home, "public_html/buyuk/.env", -1, 10); err == nil {
		t.Fatal("boyut tavanı aşıldığı halde okundu")
	}
	dosyaYaz(t, filepath.Join(e.app, "normal", ".env"), "APP_KEY=base64:x\n", 0o600)
	if b, err := evAltiDosyaOku(e.home, "public_html/normal/.env", -1, envMaxBoyut); err != nil || string(b) != "APP_KEY=base64:x\n" {
		t.Fatalf("meşru .env okunamadı: %v %q", err, b)
	}
}

// .env symlink ise hedefin izni DEĞİŞMEMELİ; meşru .env 0600 olmalı.
func TestEvAltiChmod_SymlinkHedefiDegismez(t *testing.T) {
	e := sahteEvKur(t)
	hedef := filepath.Join(e.dis, "sistem.conf")
	dosyaYaz(t, hedef, "x", 0o644)
	if err := os.Chmod(hedef, 0o755|os.ModeSetuid); err != nil { // setuid biti: düşmemeli
		t.Fatal(err)
	}
	symlinkKur(t, hedef, filepath.Join(e.app, ".env"))
	if err := evAltiChmod(e.home, "public_html/.env", os.Getuid(), 0o600); err == nil {
		t.Fatal("symlink'li .env için chmod hata vermeliydi")
	}
	if st, _ := os.Stat(hedef); st.Mode()&os.ModeSetuid == 0 || st.Mode().Perm() != 0o755 {
		t.Fatalf("symlink hedefinin izni değişti: %v", st.Mode())
	}

	e2 := sahteEvKur(t)
	env := filepath.Join(e2.app, ".env")
	dosyaYaz(t, env, "APP_NAME=x\n", 0o644)
	if err := evAltiChmod(e2.home, "public_html/.env", os.Getuid(), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(env); st.Mode().Perm() != 0o600 {
		t.Fatalf("meşru .env izni %v, 0600 bekleniyordu", st.Mode().Perm())
	}
}

// bootstrap (ya da bootstrap/cache) dışarı giden symlink ise hedefteki dosyalar SİLİNMEMELİ;
// meşru dizinde silinmeli; cache içindeki dosya symlink ise yalnız link silinmeli.
func TestEvAltiSil_AraSymlinkIzlenmez(t *testing.T) {
	adlar := []string{"config.php", "services.php", "packages.php", "routes-v7.php"}
	for _, araAd := range []string{"bootstrap", "bootstrap/cache"} {
		t.Run(araAd, func(t *testing.T) {
			e := sahteEvKur(t)
			kurbanDir := filepath.Join(e.dis, "kurban")
			if araAd == "bootstrap" {
				kurbanDir = filepath.Join(e.dis, "kurban", "cache")
			}
			for _, a := range adlar {
				dosyaYaz(t, filepath.Join(kurbanDir, a), "<?php return [];", 0o644)
			}
			hedef := filepath.Join(e.dis, "kurban")
			if araAd == "bootstrap/cache" {
				hedef = kurbanDir
			}
			symlinkKur(t, hedef, filepath.Join(e.app, araAd))
			_ = evAltiSil(e.home, "public_html/bootstrap/cache", adlar)
			for _, a := range adlar {
				if _, err := os.Stat(filepath.Join(kurbanDir, a)); err != nil {
					t.Fatalf("ev dışındaki %s silindi: %v", a, err)
				}
			}
		})
	}

	e := sahteEvKur(t)
	cache := filepath.Join(e.app, "bootstrap", "cache")
	for _, a := range adlar[:3] {
		dosyaYaz(t, filepath.Join(cache, a), "<?php return [];", 0o644)
	}
	kurban := filepath.Join(e.dis, "tek.php")
	dosyaYaz(t, kurban, "x", 0o644)
	symlinkKur(t, kurban, filepath.Join(cache, "routes-v7.php"))
	if err := evAltiSil(e.home, "public_html/bootstrap/cache", adlar); err != nil {
		t.Fatal(err)
	}
	for _, a := range adlar {
		if _, err := os.Lstat(filepath.Join(cache, a)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("meşru önbellek dosyası %s silinmedi: %v", a, err)
		}
	}
	if _, err := os.Stat(kurban); err != nil {
		t.Fatalf("cache içi symlink'in hedefi silindi: %v", err)
	}
}

func uidOf(t *testing.T, p string, lstat bool) int {
	t.Helper()
	var st unix.Stat_t
	var err error
	if lstat {
		err = unix.Lstat(p, &st)
	} else {
		err = unix.Stat(p, &st)
	}
	if err != nil {
		t.Fatal(err)
	}
	return int(st.Uid)
}

// Özyinelemeli sahiplik: ağaç içi symlink'in HEDEFİ, ara bileşen symlink'i ve
// başka yerde bağlı (hardlink) root dosyası tenant'a DEVREDİLMEMELİ; meşru içerik devredilmeli.
func TestEvAltiSahiplikVer_SymlinkVeHardlink(t *testing.T) {
	kokGerekli(t)
	const tu, tg = 4242, 4242
	e := sahteEvKur(t)
	st := filepath.Join(e.app, "storage")
	dosyaYaz(t, filepath.Join(st, "logs", "laravel.log"), "log", 0o644)
	dosyaYaz(t, filepath.Join(st, "app", "a.txt"), "a", 0o644)
	kurbanDosya := filepath.Join(e.dis, "shadow")
	dosyaYaz(t, kurbanDosya, "root", 0o600)
	kurbanDizin := filepath.Join(e.dis, "etcdir")
	dosyaYaz(t, filepath.Join(kurbanDizin, "f"), "root", 0o600)
	symlinkKur(t, kurbanDosya, filepath.Join(st, "link-dosya"))
	symlinkKur(t, kurbanDizin, filepath.Join(st, "link-dizin"))
	hl := filepath.Join(e.dis, "hardlink-kaynak")
	dosyaYaz(t, hl, "root", 0o600)
	if err := os.Link(hl, filepath.Join(st, "hl")); err != nil {
		t.Fatal(err)
	}

	if err := evAltiSahiplikVer(e.home, "public_html/storage", tu, tg); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{st, filepath.Join(st, "logs"), filepath.Join(st, "logs", "laravel.log"), filepath.Join(st, "app", "a.txt")} {
		if u := uidOf(t, p, true); u != tu {
			t.Fatalf("meşru %s uid=%d, %d bekleniyordu", p, u, tu)
		}
	}
	if u := uidOf(t, filepath.Join(st, "link-dosya"), true); u != tu {
		t.Fatalf("symlink'in kendisi devredilmedi: uid=%d", u)
	}
	for _, p := range []string{kurbanDosya, kurbanDizin, filepath.Join(kurbanDizin, "f"), hl} {
		if u := uidOf(t, p, false); u != 0 {
			t.Fatalf("ev dışındaki %s tenant'a devredildi (uid=%d)", p, u)
		}
	}

	// Ara bileşen symlink: bootstrap → ev dışı; hedef ağaç devredilmemeli.
	e2 := sahteEvKur(t)
	bsHedef := filepath.Join(e2.dis, "bs")
	dosyaYaz(t, filepath.Join(bsHedef, "cache", "x.php"), "x", 0o644)
	symlinkKur(t, bsHedef, filepath.Join(e2.app, "bootstrap"))
	if err := evAltiSahiplikVer(e2.home, "public_html/bootstrap/cache", tu, tg); err == nil {
		t.Fatal("ara bileşen symlink'li yol için hata bekleniyordu")
	}
	for _, p := range []string{filepath.Join(bsHedef, "cache"), filepath.Join(bsHedef, "cache", "x.php")} {
		if u := uidOf(t, p, false); u != 0 {
			t.Fatalf("ev dışındaki %s tenant'a devredildi (uid=%d)", p, u)
		}
	}
}

// mkdir -p: ara bileşen dışarı giden symlink ise dışarıda dizin YARATILMAMALI; meşru
// durumda (root-sahipli göç kalıntısı bootstrap/ altında) eksik dizin yaratılıp tenant'a verilmeli.
func TestEvAltiMkdirHepsi(t *testing.T) {
	kokGerekli(t)
	const tu, tg = 4242, 4242
	e := sahteEvKur(t)
	bsHedef := filepath.Join(e.dis, "bs")
	if err := os.MkdirAll(bsHedef, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkKur(t, bsHedef, filepath.Join(e.app, "bootstrap"))
	if err := evAltiMkdirHepsi(e.home, "public_html/bootstrap/cache", tu, tg); err == nil {
		t.Fatal("ara bileşen symlink'li yol için hata bekleniyordu")
	}
	if _, err := os.Lstat(filepath.Join(bsHedef, "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ev dışında dizin yaratıldı: %v", err)
	}

	e2 := sahteEvKur(t)
	if err := os.MkdirAll(filepath.Join(e2.app, "bootstrap"), 0o755); err != nil { // root-sahipli
		t.Fatal(err)
	}
	if err := evAltiMkdirHepsi(e2.home, "public_html/bootstrap/cache", tu, tg); err != nil {
		t.Fatal(err)
	}
	if u := uidOf(t, filepath.Join(e2.app, "bootstrap", "cache"), true); u != tu {
		t.Fatalf("yeni dizin uid=%d, %d bekleniyordu", u, tu)
	}
	if u := uidOf(t, filepath.Join(e2.app, "bootstrap"), true); u != 0 {
		t.Fatalf("mevcut dizinin sahipliği değişti: uid=%d", u)
	}
}

func TestEvGoreli(t *testing.T) {
	if h, r, err := evGoreli("c_ab", "/home/c_ab/public_html/uyg"); err != nil || h != "/home/c_ab" || r != "public_html/uyg" {
		t.Fatalf("h=%q r=%q err=%v", h, r, err)
	}
	for _, kotu := range []string{"/home/c_abc/public_html", "/home/c_x/../c_ab2", "/etc"} {
		if _, _, err := evGoreli("c_ab", kotu); err == nil {
			t.Fatalf("%q kabul edildi", kotu)
		}
	}
	if _, _, err := evGoreli("root", "/home/root/x"); err == nil {
		t.Fatal("geçersiz sk kabul edildi")
	}
}
