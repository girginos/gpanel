package laravel

// otomatik_dosya_test.go — OtomatikHazirla'nın root dosya adımlarını (otomatikDosya),
// envOku/laravelEksikler'i ve Schedule'ın ensureLogDir'ini evKoku/kimlikBul dikişleriyle
// t.TempDir sahte evinde uçtan uca kilitler. Kaynak koruma testi (go/ast) ise handler
// gövdelerinde ve paket içinde onlardan erişilen fonksiyonlarda yol tabanlı os/exec/unix
// kullanımını yasaklar: handler'a eski os.ReadFile/os.Chmod/os.Remove/chown dönüşü ya da
// kiracı uid'sinin handler'da elle kurulması testte düşer.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const kiraciUID, kiraciGID = 4242, 4242

// sahteKok: evKoku ve kimlikBul'u sahte eve yönlendirir (test sonunda geri alınır). Sahte
// ev kullanıcısı "c_test" → (uid, gid); başka her ad çözülemez.
func sahteKok(t *testing.T, uid, gid int) sahteEv {
	t.Helper()
	e := sahteEvKur(t)
	eskiKok, eskiKimlik := evKoku, kimlikBul
	evKoku = filepath.Dir(e.home)
	sk := filepath.Base(e.home)
	kimlikBul = func(ad string) (int, int, error) {
		if ad != sk {
			return 0, 0, errors.New("kullanıcı yok")
		}
		return uid, gid, nil
	}
	t.Cleanup(func() { evKoku, kimlikBul = eskiKok, eskiKimlik })
	return e
}

// kiraciYap: yolları (symlink izlemeden) kiracıya verir.
func kiraciYap(t *testing.T, yollar ...string) {
	t.Helper()
	for _, p := range yollar {
		if err := os.Lchown(p, kiraciUID, kiraciGID); err != nil {
			t.Fatal(err)
		}
	}
}

func odKur(t *testing.T, e sahteEv) otomatikDosya {
	t.Helper()
	od, err := otomatikDosyaHazirla("c_test", e.app)
	if err != nil {
		t.Fatal(err)
	}
	return od
}

func modOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode()
}

// sikiUmask: testte umask'ı 077 yapar (geri alınır) — yeni dizin izninin umask'tan
// bağımsız sabitlendiğini (Fchmod) doğrulamak için. Paket testleri paralel koşmaz.
func sikiUmask(t *testing.T) {
	t.Helper()
	eski := unix.Umask(0o077)
	t.Cleanup(func() { unix.Umask(eski) })
}

func icerir(liste []string, s string) bool {
	for _, x := range liste {
		if x == s {
			return true
		}
	}
	return false
}

func TestOtomatikDosyaHazirla(t *testing.T) {
	e := sahteKok(t, kiraciUID, kiraciGID)
	od, err := otomatikDosyaHazirla("c_test", filepath.Join(e.app, "uyg"))
	if err != nil || od.home != e.home || od.appRel != "public_html/uyg" || od.uid != kiraciUID || od.gid != kiraciGID {
		t.Fatalf("od=%+v err=%v", od, err)
	}
	if _, err := otomatikDosyaHazirla("c_test", e.dis); err == nil {
		t.Fatal("ev dışındaki uygulama dizini kabul edildi")
	}
	if _, err := otomatikDosyaHazirla("root", filepath.Join(evKoku, "root")); err == nil {
		t.Fatal("geçersiz sistem kullanıcısı kabul edildi")
	}
	if _, err := otomatikDosyaHazirla("c_yok", filepath.Join(evKoku, "c_yok", "public_html")); !errors.Is(err, errKimlikYok) {
		t.Fatalf("çözülemeyen kimlik için errKimlikYok beklenirdi: %v", err)
	}
	// root/sistem kimliği (<=0) reddedilmeli: uid 0 ile sahiplik denetimi root dosyalarını
	// "kiracıya ait" sayardı.
	for _, k := range [][2]int{{0, kiraciGID}, {kiraciUID, 0}, {-1, -1}} {
		u, g := k[0], k[1]
		kimlikBul = func(string) (int, int, error) { return u, g, nil }
		if _, err := otomatikDosyaHazirla("c_test", e.app); !errors.Is(err, errKimlikYok) {
			t.Fatalf("uid=%d gid=%d kabul edildi (err=%v)", u, g, err)
		}
	}
}

// 1. adım: .env.example ev dışına symlink ya da kiracıya ait değilse içerik .env'e geçmemeli.
func TestOtomatikDosya_EnvUret(t *testing.T) {
	kokGerekli(t)
	durumlar := []struct {
		ad      string
		kur     func(t *testing.T, e sahteEv)
		yazmali bool
		notAlt  string
	}{
		{"ornek ev disina symlink", func(t *testing.T, e sahteEv) {
			dosyaYaz(t, filepath.Join(e.dis, "sir.env"), gizliIcerik, 0o600)
			symlinkKur(t, filepath.Join(e.dis, "sir.env"), filepath.Join(e.app, ".env.example"))
			kiraciYap(t, filepath.Join(e.app, ".env.example"))
		}, false, "okunamadı"},
		{"ornek kiraciya ait degil", func(t *testing.T, e sahteEv) {
			// root sahipli (göç kalıntısı ya da başka inode'a hardlink)
			dosyaYaz(t, filepath.Join(e.app, ".env.example"), gizliIcerik, 0o644)
		}, false, "kiracıya ait değil"},
		{"mesru", func(t *testing.T, e sahteEv) {
			dosyaYaz(t, filepath.Join(e.app, ".env.example"), "APP_NAME=x\n", 0o644)
			kiraciYap(t, filepath.Join(e.app, ".env.example"))
		}, true, "kopyalandı"},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			e := sahteKok(t, kiraciUID, kiraciGID)
			kiraciYap(t, e.home, e.app)
			d.kur(t, e)
			var y yazKaydedici
			calisti, ok, not := odKur(t, e).envUret(y.yaz)
			if !calisti || ok != d.yazmali || y.cagrildi != d.yazmali || !strings.Contains(not, d.notAlt) {
				t.Fatalf("calisti=%v ok=%v cagrildi=%v not=%q", calisti, ok, y.cagrildi, not)
			}
			if strings.Contains(y.icerik, gizliIcerik) {
				t.Fatalf("kiracıya ait olmayan içerik .env'e geçti: %q", y.icerik)
			}
		})
	}
	t.Run("env varsa dokunulmaz", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		dosyaYaz(t, filepath.Join(e.app, ".env.example"), "APP_NAME=x\n", 0o644)
		dosyaYaz(t, filepath.Join(e.app, ".env"), "APP_NAME=eski\n", 0o600)
		kiraciYap(t, e.home, e.app, filepath.Join(e.app, ".env.example"), filepath.Join(e.app, ".env"))
		var y yazKaydedici
		if calisti, _, _ := odKur(t, e).envUret(y.yaz); calisti || y.cagrildi {
			t.Fatalf("mevcut .env varken üretim çalıştı (calisti=%v)", calisti)
		}
	})
}

// .env 0600: symlink hedefinin ve kiracıya ait olmayan dosyanın izni DEĞİŞMEMELİ.
func TestOtomatikDosya_EnvKilitle(t *testing.T) {
	kokGerekli(t)
	e := sahteKok(t, kiraciUID, kiraciGID)
	kiraciYap(t, e.home, e.app)
	hedef := filepath.Join(e.dis, "setuid-ikili")
	dosyaYaz(t, hedef, "x", 0o755)
	if err := os.Chmod(hedef, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	symlinkKur(t, hedef, filepath.Join(e.app, ".env"))
	kiraciYap(t, filepath.Join(e.app, ".env"))
	if err := odKur(t, e).envKilitle(); err == nil {
		t.Fatal("symlink'li .env için hata bekleniyordu")
	}
	if m := modOf(t, hedef); m&os.ModeSetuid == 0 || m.Perm() != 0o755 {
		t.Fatalf("ev dışındaki symlink hedefinin izni değişti: %v", m)
	}

	e2 := sahteKok(t, kiraciUID, kiraciGID)
	kiraciYap(t, e2.home, e2.app)
	yabanci := filepath.Join(e2.app, ".env")
	dosyaYaz(t, yabanci, "x", 0o644) // root sahipli
	if err := odKur(t, e2).envKilitle(); err == nil {
		t.Fatal("kiracıya ait olmayan .env için hata bekleniyordu")
	}
	if m := modOf(t, yabanci); m.Perm() != 0o644 {
		t.Fatalf("kiracıya ait olmayan dosyanın izni değişti: %v", m)
	}

	e3 := sahteKok(t, kiraciUID, kiraciGID)
	env := filepath.Join(e3.app, ".env")
	dosyaYaz(t, env, "APP_NAME=x\n", 0o644)
	kiraciYap(t, e3.home, e3.app, env)
	if err := odKur(t, e3).envKilitle(); err != nil {
		t.Fatal(err)
	}
	if m := modOf(t, env); m.Perm() != 0o600 {
		t.Fatalf("meşru .env izni %v, 0600 bekleniyordu", m.Perm())
	}
}

// 5. adım: ev dışı hedefler (symlink/hardlink/ara bileşen) kiracıya DEVREDİLMEMELİ, ev
// dışında dizin YARATILMAMALI; meşru göç kalıntısı kiracıya geçmeli.
func TestOtomatikDosya_Izinler(t *testing.T) {
	kokGerekli(t)
	t.Run("mesru goc kalintisi", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home, e.app)
		st := filepath.Join(e.app, "storage")
		dosyaYaz(t, filepath.Join(st, "logs", "laravel.log"), "log", 0o644) // root sahipli
		sikiUmask(t)
		ok, not := odKur(t, e).izinler()
		if !ok {
			t.Fatalf("meşru izin adımı başarısız: %q", not)
		}
		bs := filepath.Join(e.app, "bootstrap")
		for _, p := range []string{st, filepath.Join(st, "logs"), filepath.Join(st, "logs", "laravel.log"), bs, filepath.Join(bs, "cache")} {
			if u := uidOf(t, p, true); u != kiraciUID {
				t.Fatalf("%s uid=%d, %d bekleniyordu", p, u, kiraciUID)
			}
		}
		if m := modOf(t, filepath.Join(bs, "cache")); !m.IsDir() || m.Perm() != 0o755 {
			t.Fatalf("bootstrap/cache izni %v", m)
		}
	})
	t.Run("storage ici symlink ve hardlink", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home, e.app)
		st := filepath.Join(e.app, "storage")
		dosyaYaz(t, filepath.Join(st, "app", "a.txt"), "a", 0o644)
		kurban := filepath.Join(e.dis, "shadow")
		dosyaYaz(t, kurban, "root", 0o600)
		kurbanDizin := filepath.Join(e.dis, "etcdir")
		dosyaYaz(t, filepath.Join(kurbanDizin, "f"), "root", 0o600)
		symlinkKur(t, kurban, filepath.Join(st, "link-dosya"))
		symlinkKur(t, kurbanDizin, filepath.Join(st, "link-dizin"))
		hl := filepath.Join(e.dis, "hl-kaynak")
		dosyaYaz(t, hl, "root", 0o600)
		if err := os.Link(hl, filepath.Join(st, "hl")); err != nil {
			t.Fatal(err)
		}
		_, _ = odKur(t, e).izinler()
		if u := uidOf(t, filepath.Join(st, "app", "a.txt"), true); u != kiraciUID {
			t.Fatalf("meşru dosya devredilmedi: uid=%d", u)
		}
		for _, p := range []string{kurban, kurbanDizin, filepath.Join(kurbanDizin, "f"), hl} {
			if u := uidOf(t, p, false); u != 0 {
				t.Fatalf("ev dışındaki %s kiracıya devredildi (uid=%d)", p, u)
			}
		}
	})
	t.Run("bootstrap ev disina symlink", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home, e.app)
		bs := filepath.Join(e.dis, "bs")
		dosyaYaz(t, filepath.Join(bs, "cache", "x.php"), "x", 0o644)
		symlinkKur(t, bs, filepath.Join(e.app, "bootstrap"))
		kiraciYap(t, filepath.Join(e.app, "bootstrap"))
		ok, not := odKur(t, e).izinler()
		if ok || !strings.Contains(not, "bootstrap/cache") {
			t.Fatalf("ara bileşen symlink'i için başarısız adım bekleniyordu: ok=%v not=%q", ok, not)
		}
		for _, p := range []string{bs, filepath.Join(bs, "cache"), filepath.Join(bs, "cache", "x.php")} {
			if u := uidOf(t, p, false); u != 0 {
				t.Fatalf("ev dışındaki %s kiracıya devredildi (uid=%d)", p, u)
			}
		}
	})
	t.Run("bootstrap bos ev disi dizine symlink", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home, e.app)
		bs := filepath.Join(e.dis, "bos")
		if err := os.MkdirAll(bs, 0o755); err != nil {
			t.Fatal(err)
		}
		symlinkKur(t, bs, filepath.Join(e.app, "bootstrap"))
		_, _ = odKur(t, e).izinler()
		if _, err := os.Lstat(filepath.Join(bs, "cache")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ev dışında dizin yaratıldı: %v", err)
		}
	})
}

// 6. adım: yalnız ev içindeki bootstrap/cache'teki listelenen adlar silinir.
func TestOtomatikDosya_OnbellekSil(t *testing.T) {
	e := sahteKok(t, kiraciUID, kiraciGID)
	kurban := filepath.Join(e.dis, "bs", "cache")
	for _, a := range onbellekDosyalari {
		dosyaYaz(t, filepath.Join(kurban, a), "<?php return [];", 0o644)
	}
	symlinkKur(t, filepath.Join(e.dis, "bs"), filepath.Join(e.app, "bootstrap"))
	_ = odKur(t, e).onbellekSil()
	for _, a := range onbellekDosyalari {
		if _, err := os.Stat(filepath.Join(kurban, a)); err != nil {
			t.Fatalf("ev dışındaki %s silindi: %v", a, err)
		}
	}

	e2 := sahteKok(t, kiraciUID, kiraciGID)
	cache := filepath.Join(e2.app, "bootstrap", "cache")
	for _, a := range onbellekDosyalari[:3] {
		dosyaYaz(t, filepath.Join(cache, a), "<?php return [];", 0o644)
	}
	dosyaYaz(t, filepath.Join(cache, "korunur.php"), "x", 0o644)
	hedef := filepath.Join(e2.dis, "tek.php")
	dosyaYaz(t, hedef, "x", 0o644)
	symlinkKur(t, hedef, filepath.Join(cache, onbellekDosyalari[3]))
	if err := odKur(t, e2).onbellekSil(); err != nil {
		t.Fatal(err)
	}
	for _, a := range onbellekDosyalari {
		if _, err := os.Lstat(filepath.Join(cache, a)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("meşru önbellek dosyası %s silinmedi: %v", a, err)
		}
	}
	for _, p := range []string{filepath.Join(cache, "korunur.php"), hedef} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s silinmemeliydi: %v", p, err)
		}
	}
}

// envOku (GET /laravel/env, laravelEksikler, 3./4. adım): symlink'li .env (son ya da ara
// bileşen) ev dışını okutamaz; boyut tavanı; ev dışı uygulama dizini reddi.
func TestEnvOku_SahteEv(t *testing.T) {
	e := sahteKok(t, kiraciUID, kiraciGID)
	sir := filepath.Join(e.dis, "sir.env")
	dosyaYaz(t, sir, gizliIcerik+"\nAPP_KEY=base64:dis\n", 0o600)

	normal := filepath.Join(e.app, "normal")
	dosyaYaz(t, filepath.Join(normal, ".env"), "APP_KEY=base64:x\n", 0o600)
	if s, err := envOku("c_test", normal); err != nil || s != "APP_KEY=base64:x\n" {
		t.Fatalf("meşru .env okunamadı: %v %q", err, s)
	}
	yaprak := filepath.Join(e.app, "yaprak")
	symlinkKur(t, sir, filepath.Join(yaprak, ".env"))
	if s, err := envOku("c_test", yaprak); err == nil || strings.Contains(s, gizliIcerik) {
		t.Fatalf(".env symlink'i izlendi: err=%v %q", err, s)
	}
	dosyaYaz(t, filepath.Join(e.dis, "d2", ".env"), gizliIcerik, 0o600)
	symlinkKur(t, filepath.Join(e.dis, "d2"), filepath.Join(e.app, "ara"))
	if s, err := envOku("c_test", filepath.Join(e.app, "ara")); err == nil || strings.Contains(s, gizliIcerik) {
		t.Fatalf("ara bileşen symlink'i izlendi: err=%v %q", err, s)
	}
	buyuk := filepath.Join(e.app, "buyuk")
	dosyaYaz(t, filepath.Join(buyuk, ".env"), strings.Repeat("A", envMaxBoyut+1), 0o600)
	if _, err := envOku("c_test", buyuk); err == nil {
		t.Fatal("boyut tavanı aşıldığı halde okundu")
	}
	if _, err := envOku("c_test", e.dis); err == nil {
		t.Fatal("ev dışındaki uygulama dizini okundu")
	}
	// laravelEksikler: symlink'li .env "yok" sayılmalı (hedefte APP_KEY olsa da bit sızmaz).
	if eks := laravelEksikler("c_test", yaprak, true, true); !icerir(eks, "env") || icerir(eks, "app_key") {
		t.Fatalf("symlink'li .env için eksikler=%v", eks)
	}
	if eks := laravelEksikler("c_test", normal, true, true); icerir(eks, "env") || icerir(eks, "app_key") {
		t.Fatalf("meşru .env için eksikler=%v", eks)
	}
}

// Schedule → ensureLogDir: logs yoksa kiracıya 0750 yaratılır; var olan girdiye (dizin ya
// da symlink) dokunulmaz, symlink hedefi devredilmez, ev dışında dizin yaratılmaz.
func TestEnsureLogDir(t *testing.T) {
	kokGerekli(t)
	t.Run("yok ise kiraciya 0750", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home)
		sikiUmask(t)
		if err := ensureLogDir("c_test"); err != nil {
			t.Fatal(err)
		}
		logs := filepath.Join(e.home, logAltDizin)
		if u := uidOf(t, logs, true); u != kiraciUID {
			t.Fatalf("logs uid=%d, %d bekleniyordu", u, kiraciUID)
		}
		if m := modOf(t, logs); !m.IsDir() || m.Perm() != 0o750 {
			t.Fatalf("logs izni %v, 0750 dizin bekleniyordu", m)
		}
	})
	t.Run("var olan dizine dokunulmaz", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		logs := filepath.Join(e.home, logAltDizin)
		if err := os.Mkdir(logs, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := ensureLogDir("c_test"); err != nil {
			t.Fatal(err)
		}
		if u := uidOf(t, logs, true); u != 0 {
			t.Fatalf("var olan dizinin sahipliği değişti: uid=%d", u)
		}
		if m := modOf(t, logs); m.Perm() != 0o700 {
			t.Fatalf("var olan dizinin izni değişti: %v", m)
		}
	})
	t.Run("ev disi dizine symlink", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home)
		hedef := filepath.Join(e.dis, "sistem-dizini")
		if err := os.MkdirAll(hedef, 0o755); err != nil {
			t.Fatal(err)
		}
		symlinkKur(t, hedef, filepath.Join(e.home, logAltDizin))
		kiraciYap(t, filepath.Join(e.home, logAltDizin))
		if err := ensureLogDir("c_test"); err == nil {
			t.Fatal("symlink'li logs için hata bekleniyordu")
		}
		if u := uidOf(t, hedef, false); u != 0 {
			t.Fatalf("ev dışındaki hedef kiracıya devredildi: uid=%d", u)
		}
	})
	t.Run("kirik symlink", func(t *testing.T) {
		e := sahteKok(t, kiraciUID, kiraciGID)
		kiraciYap(t, e.home)
		hedef := filepath.Join(e.dis, "yeni-dizin")
		symlinkKur(t, hedef, filepath.Join(e.home, logAltDizin))
		if err := ensureLogDir("c_test"); err == nil {
			t.Fatal("kırık symlink'li logs için hata bekleniyordu")
		}
		if _, err := os.Lstat(hedef); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ev dışında dizin yaratıldı: %v", err)
		}
	})
	t.Run("gecersiz kimlik", func(t *testing.T) {
		sahteKok(t, 0, 0)
		if err := ensureLogDir("root"); err == nil {
			t.Fatal("geçersiz sistem kullanıcısı kabul edildi")
		}
		if err := ensureLogDir("c_test"); !errors.Is(err, errKimlikYok) {
			t.Fatalf("uid 0 için errKimlikYok beklenirdi: %v", err)
		}
	})
}

// ---- Kaynak koruma (go/ast) ----

// yasakPaketYol: yol (string) ile dosya/süreç işlemi yapabilen paketler.
var yasakPaketYol = map[string]bool{"os": true, "io/ioutil": true, "os/exec": true, "golang.org/x/sys/unix": true, "syscall": true}

type kaynakGovde struct {
	fd    *ast.FuncDecl
	paket map[string]string // içe aktarma yerel adı → yol
}

type paketKaynak struct {
	fset      *token.FileSet
	dosyalar  []*ast.File            // test dışı dosyalar
	fonk      map[string]kaynakGovde // "ad" ya da "Alici.ad"
	yontem    map[string][]string    // yöntem adı → anahtarlar
	tehlikeli map[string]bool        // paket düzeyinde yasak seçiciye takma ad (var x = os.ReadFile)
	takma     map[string][]string    // paket düzeyi var → initializer'ındaki adlar (var envYazici = envYaz)
}

// paketKaynagiOku: paketin test dışı .go dosyalarını ayrıştırır. Ayrıca paket düzeyinde
// bir yasak seçiciye (os.ReadFile gibi) takma ad veren tanımlayıcıları toplar: aksi halde
// `var dosyaOku = os.ReadFile; ... dosyaOku(p)` ile handler yol tabanlı okumayı gizleyebilir
// (denetim boşluğu a). os.Stat/os.Lstat takma adı serbesttir (yalnız varlık yoklaması).
// Her paket düzeyi var'ın initializer'ındaki adlar da (takma) toplanır: geçişli tarama bir
// dikiş/takma ad çağrısında onun GÖSTERDİĞİ fonksiyona iner (`var envYazici = kokYaz` →
// kokYaz taranır); yoksa dikiş üzerinden root yazıcıya bağlanmak taramayı atlatırdı.
func paketKaynagiOku(t *testing.T) paketKaynak {
	t.Helper()
	p := paketKaynak{fset: token.NewFileSet(), fonk: map[string]kaynakGovde{}, yontem: map[string][]string{},
		tehlikeli: map[string]bool{}, takma: map[string][]string{}}
	dosyalar, err := filepath.Glob("*.go")
	if err != nil || len(dosyalar) == 0 {
		t.Fatalf("kaynak bulunamadı: %v", err)
	}
	serbestTakma := func(sel string) bool { return sel == "os.Stat" || sel == "os.Lstat" }
	for _, d := range dosyalar {
		if strings.HasSuffix(d, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(p.fset, d, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		p.dosyalar = append(p.dosyalar, f)
		paket := map[string]string{}
		for _, im := range f.Imports {
			yol := strings.Trim(im.Path.Value, `"`)
			ad := path.Base(yol)
			if im.Name != nil {
				ad = im.Name.Name
			}
			if ad == "." && yasakPaketYol[yol] {
				t.Errorf("%s: %s nokta ile içe aktarılmış (koruma testi atlatılır)", d, yol)
			}
			paket[ad] = yol
		}
		for _, dc := range f.Decls {
			// Paket düzeyi var: initializer'ında yasak seçici varsa adı "tehlikeli".
			if gd, ok := dc.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, deger := range vs.Values {
						if yasakSeciciVar(deger, paket, serbestTakma) {
							for _, ad := range vs.Names {
								p.tehlikeli[ad.Name] = true
							}
						}
					}
					for i, ad := range vs.Names {
						degerler := vs.Values
						if len(vs.Values) == len(vs.Names) {
							degerler = vs.Values[i : i+1]
						}
						for _, deger := range degerler {
							ast.Inspect(deger, func(n ast.Node) bool {
								if id, ok := n.(*ast.Ident); ok {
									p.takma[ad.Name] = append(p.takma[ad.Name], id.Name)
								}
								return true
							})
						}
					}
				}
				continue
			}
			fd, ok := dc.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			anahtar := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				tip := fd.Recv.List[0].Type
				if s, ok := tip.(*ast.StarExpr); ok {
					tip = s.X
				}
				if id, ok := tip.(*ast.Ident); ok {
					anahtar = id.Name + "." + anahtar
				}
				p.yontem[fd.Name.Name] = append(p.yontem[fd.Name.Name], anahtar)
			}
			p.fonk[anahtar] = kaynakGovde{fd, paket}
		}
	}
	return p
}

// yasakSeciciVar: ifade (ya da alt ifadesi) yasak bir paketin seçicisi mi (serbest hariç)?
func yasakSeciciVar(e ast.Expr, paket map[string]string, serbest func(string) bool) bool {
	bulundu := false
	ast.Inspect(e, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok {
				if yol := paket[id.Name]; yasakPaketYol[yol] && !serbest(path.Base(yol)+"."+se.Sel.Name) {
					bulundu = true
				}
			}
		}
		return !bulundu
	})
	return bulundu
}

// incele: gövdedeki (kapanışlar dahil) çağrı adları, kullanılan adlar (çağrı, seçici,
// bileşik literal türü) ve yasak paketlerden izinli() dışında kalan kullanımlar.
func (p paketKaynak) incele(g kaynakGovde, izinli func(string) bool) (cagri, adlar map[string]bool, ihlal []string) {
	cagri, adlar = map[string]bool{}, map[string]bool{}
	ast.Inspect(g.fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			switch f := x.Fun.(type) {
			case *ast.Ident:
				cagri[f.Name] = true
			case *ast.SelectorExpr:
				if id, ok := f.X.(*ast.Ident); !ok || g.paket[id.Name] == "" {
					cagri[f.Sel.Name] = true
				}
			}
		case *ast.SelectorExpr:
			adlar[x.Sel.Name] = true
			if id, ok := x.X.(*ast.Ident); ok {
				if yol := g.paket[id.Name]; yasakPaketYol[yol] && !izinli(path.Base(yol)+"."+x.Sel.Name) {
					ihlal = append(ihlal, fmt.Sprintf("%s %s.%s", p.fset.Position(x.Pos()), id.Name, x.Sel.Name))
				}
			}
		case *ast.CompositeLit:
			if id, ok := x.Type.(*ast.Ident); ok {
				adlar[id.Name] = true
			}
		}
		return true
	})
	for c := range cagri {
		adlar[c] = true
	}
	return cagri, adlar, ihlal
}

// referanslar: gövdede ÇAĞRILMADAN değer olarak kullanılan adlar — çıplak tanımlayıcılar
// (`f := kokYaz; f(...)`) ve içe aktarma olmayan seçiciler (`m := h.kokYaz`). Geçişli
// tarama bunlara da iner: yoksa bir fonksiyonu değer olarak alıp çağırmak taramayı
// atlatırdı. Aşırı yaklaşım zararsızdır (yalnız paket fonksiyonu/yöntemi/var adları izlenir).
func (p paketKaynak) referanslar(g kaynakGovde) map[string]bool {
	ref := map[string]bool{}
	secici := map[*ast.Ident]bool{}
	ast.Inspect(g.fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			secici[x.Sel] = true
			if id, ok := x.X.(*ast.Ident); !ok || g.paket[id.Name] == "" {
				ref[x.Sel.Name] = true
			}
		case *ast.Ident:
			if !secici[x] {
				ref[x.Name] = true
			}
		}
		return true
	})
	return ref
}

// Kiracının denetlediği yollarda root olarak çalışan fonksiyonlar ve paket içinde onlardan
// erişilen HER fonksiyon yol tabanlı os/ioutil/exec/unix/syscall kullanamaz; yalnız varlık
// yoklaması (os.Stat/os.Lstat) serbesttir. Dosya işlemi ancak çapalı yardımcılar (evAlti*,
// envOrnektenUret — ayrıca aşağıdaki fd-tabanlı kuralla kilitli) ya da kiracı uid'siyle
// çalışan yardımcılar (envYaz, TenantExec) üzerinden yapılır; bunların içine inilmez.
func TestKaynakKoruma_RootDosyaIslemleriCapali(t *testing.T) {
	serbest := func(s string) bool { return s == "os.Stat" || s == "os.Lstat" }
	// guvenli: geçişli taramada İÇİNE İNİLMEYEN yardımcılar. Her biri ya kiracı uid'siyle
	// çalışır (envYaz/TenantExec: runuser — TestKaynakKoruma_KiraciCapali kilitler) ya da
	// yalnız kök-sahipli sistem yoluna yazar (cronYaz/cronSil: id türevli — aynı test
	// kilitler) ya da ev köküne çapalı fd tabanlıdır (evAlti*: fd-tabanlı test kilitler).
	// Dikişler (envYazici, kimlikBul) burada DEĞİL: tarama onların gösterdiği fonksiyona
	// iner (takma) ve değerleri TestKaynakKoruma_DikisDegerleri ile kilitlidir.
	guvenli := map[string]bool{
		"evAltiAc": true, "evAltiDosyaOku": true, "evAltiChmod": true, "evAltiSil": true,
		"evAltiMkdirHepsi": true, "evAltiSahiplikVer": true, "sahiplikAgaci": true, "sahiplikGirdi": true,
		"fdDuzenliVeSahip": true, "envOrnektenUret": true, "dizinBos": true, "yokHatasi": true,
		"envYaz": true, "TenantExec": true, "tenantExecEnv": true, "tenantKimlik": true,
		"cronYaz": true, "cronSil": true,
	}
	// Handler kiracı kimliğini/ev yolunu kendisi kurmaz, çapalı yardımcıları doğrudan
	// çağırmaz: yalnız otomatikDosya yöntemleri.
	elYasak := []string{"envOrnektenUret", "evAltiAc", "evAltiDosyaOku", "evAltiChmod", "evAltiSil",
		"evAltiMkdirHepsi", "evAltiSahiplikVer", "tenantKimlik", "kimlikBul", "otomatikDosya",
		"uid", "gid", "home", "appRel"}
	kurallar := []struct {
		kok            string
		zorunlu, yasak []string
	}{
		{"Handlers.OtomatikHazirla", []string{"otomatikDosyaHazirla", "envUret", "envKilitle", "izinler", "onbellekSil", "envOku"}, elYasak},
		{"otomatikDosyaHazirla", []string{"evGoreli", "kimlikBul"}, nil},
		{"otomatikDosya.envUret", []string{"envOrnektenUret"}, nil},
		{"otomatikDosya.envKilitle", []string{"evAltiChmod"}, nil},
		{"otomatikDosya.izinler", []string{"evAltiMkdirHepsi", "evAltiSahiplikVer"}, nil},
		{"otomatikDosya.onbellekSil", []string{"evAltiSil"}, nil},
		{"envOku", []string{"evGoreli", "evAltiDosyaOku"}, nil},
		{"envYazilabilir", []string{"envOku"}, nil},
		{"laravelEksikler", []string{"envOku"}, nil},
		{"Handlers.EnvOku", []string{"envOku"}, nil},
		// EnvYaz üzerine yazmadan ÖNCE envYazilabilir çağırmalı (güvensiz .env'i ezmeyi
		// engeller) ve yazımı envYazici dikişi üzerinden yapmalı (handler testleri onu
		// sahteler); envYaz'a doğrudan dönerse ya da envYazilabilir silinirse burada düşer.
		// Dikişin değeri (envYaz) TestKaynakKoruma_DikisDegerleri ile kilitli; geçişli
		// tarama dikişin gösterdiği fonksiyona da iner.
		{"Handlers.EnvYaz", []string{"envYazilabilir", "envYazici"}, []string{"envYaz"}},
		{"ensureLogDir", []string{"evDizini", "kimlikBul", "evAltiMkdirHepsi"}, nil},
		// Schedule: kiracının denetlediği ~/logs'a root dokunuşu YALNIZ ensureLogDir
		// üzerinden; kök dosyası YALNIZ cronYaz/cronSil (id türevli yol). Eski os.Stat/
		// os.MkdirAll/os.Chown/chown -R veya ensureLogDir'in silinmesi burada düşer.
		{"Handlers.Schedule", []string{"ensureLogDir"}, []string{"evAltiMkdirHepsi", "evAltiSahiplikVer", "kimlikBul"}},
	}
	p := paketKaynagiOku(t)
	// Paket düzeyinde yasak seçiciye takma ad (var x = os.ReadFile) HİÇ olmamalı: meşru
	// gerekçesi yok ve transitif çağrı taramasını (Ident çağrısı) atlatmak için kullanılır
	// (denetim boşluğu a). Böyle bir ad ayrıca aşağıdaki transitif taramada da yakalanır.
	if len(p.tehlikeli) > 0 {
		adlar := make([]string, 0, len(p.tehlikeli))
		for a := range p.tehlikeli {
			adlar = append(adlar, a)
		}
		sort.Strings(adlar)
		t.Errorf("paket düzeyi yasak-seçici takma adı bulundu (yol tabanlı os/exec/unix'i gizler): %v", adlar)
	}
	for _, k := range kurallar {
		g, ok := p.fonk[k.kok]
		if !ok {
			t.Errorf("%s bulunamadı (yeniden adlandırıldıysa bu koruma testini güncelle)", k.kok)
			continue
		}
		cagri, adlar, _ := p.incele(g, serbest)
		for _, z := range k.zorunlu {
			if !cagri[z] {
				t.Errorf("%s: %s çağrılmıyor", k.kok, z)
			}
		}
		for _, y := range k.yasak {
			if adlar[y] {
				t.Errorf("%s: %s doğrudan kullanılmamalı (dosya işlemi yalnız çapalı/kiracı yardımcıları üzerinden)", k.kok, y)
			}
		}
		// Geçişli tarama: kökten paket içinde erişilen her fonksiyon — çağrılan ya da değer
		// olarak alınan (referanslar) fonksiyon/yöntem ve paket düzeyi var'ın (dikiş/takma
		// ad) initializer'ında gösterilen fonksiyon.
		ziyaret := map[string]bool{}
		kuyruk := []string{k.kok}
		izle := func(ad string) {
			hedef := append([]string(nil), p.yontem[ad]...)
			if _, ok := p.fonk[ad]; ok {
				hedef = append(hedef, ad)
			} else if _, ok := p.takma[ad]; ok {
				hedef = append(hedef, ad)
			}
			for _, h := range hedef {
				if !guvenli[h] && !ziyaret[h] {
					kuyruk = append(kuyruk, h)
				}
			}
		}
		for len(kuyruk) > 0 {
			a := kuyruk[0]
			kuyruk = kuyruk[1:]
			if ziyaret[a] {
				continue
			}
			ziyaret[a] = true
			g, ok := p.fonk[a]
			if !ok {
				// Paket düzeyi var (ör. envYazici): gösterdiği adlara in.
				if p.tehlikeli[a] {
					t.Errorf("%s üzerinden paket düzeyi yasak-seçici takma adı %q erişiliyor", k.kok, a)
				}
				for _, ad := range p.takma[a] {
					izle(ad)
				}
				continue
			}
			c, adlarA, ihlal := p.incele(g, serbest)
			for _, i := range ihlal {
				t.Errorf("%s (%s üzerinden erişiliyor): yol tabanlı %s", a, k.kok, i)
			}
			// Paket düzeyi yasak-seçici takma adı (var x = os.ReadFile) çağrısı/kullanımı.
			for ad := range adlarA {
				if p.tehlikeli[ad] {
					t.Errorf("%s (%s üzerinden erişiliyor): paket düzeyi yasak-seçici takma adı %q kullanılıyor", a, k.kok, ad)
				}
			}
			for ad := range c {
				izle(ad)
			}
			for ad := range p.referanslar(g) {
				izle(ad)
			}
		}
	}
}

// TestKaynakKoruma_KiraciCapali: guvenli listesindeki "içine inilmeyen" yazıcıların KENDİSİ
// güvenli mi? İki sınıf: (1) kiracı uid'siyle çalışanlar (envYaz/envYazici) — yol tabanlı
// root os yazımı (os.WriteFile/os.Create/os.OpenFile/os.Chmod/os.Chown/os.Remove/os.Mkdir*)
// YASAK, doğrudan exec YASAK — süreç yalnız kiraci.Kiraci{...}.Komut ile (runuser, panel
// ortamı/sırları çocuğa geçmez) başlatılır; (2) yalnız kök yoluna yazanlar (cronYaz/cronSil) —
// her os yol argümanı cronDPath(...) çağrısı olmalı. Denetim boşluğu (b): envYaz gizlice
// root os.WriteFile(dst,...) yaparsa bir üst test onu (guvenli, taranmıyor) yakalamıyordu.
func TestKaynakKoruma_KiraciCapali(t *testing.T) {
	p := paketKaynagiOku(t)
	yasakYazSel := map[string]bool{
		"os.WriteFile": true, "os.Create": true, "os.OpenFile": true, "os.Chmod": true,
		"os.Chown": true, "os.Lchown": true, "os.Remove": true, "os.RemoveAll": true,
		"os.Mkdir": true, "os.MkdirAll": true, "os.Rename": true, "os.Symlink": true, "os.Link": true,
		"os.ReadFile": true, "ioutil.WriteFile": true, "ioutil.ReadFile": true,
	}
	// (1) kiraci.Komut ile kiracı olarak çalışanlar.
	for _, ad := range []string{"envYaz", "TenantExec", "tenantExecEnv", "mkdirTenant"} {
		g, ok := p.fonk[ad]
		if !ok {
			t.Errorf("%s bulunamadı", ad)
			continue
		}
		var komutCagri int
		ast.Inspect(g.fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if kiraciKomutCagrisi(ce, g.paket) {
				komutCagri++
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := se.X.(*ast.Ident)
			if !ok {
				return true
			}
			sel := path.Base(g.paket[id.Name]) + "." + se.Sel.Name
			if yasakYazSel[sel] {
				t.Errorf("%s: kiracı-çapalı yazıcı yol tabanlı root %s yapıyor", ad, sel)
			}
			if sel == "exec.Command" || sel == "exec.CommandContext" {
				t.Errorf("%s: doğrudan %s — kiracı süreci yalnız kiraci.Kiraci{...}.Komut ile başlatılır (kimlik düşürme + panel ortamı yok)", ad, sel)
			}
			return true
		})
		// TenantExec, tenantExecEnv'e devreder; diğerleri süreci kendisi başlatır.
		if ad != "TenantExec" && komutCagri == 0 {
			t.Errorf("%s: kiraci.Kiraci{...}.Komut çağrısı yok (kiracıya düşürme atlanmış olabilir)", ad)
		}
	}
	// (2) yalnız kök (id türevli) yola yazanlar: her os çağrısının yol argümanı cronDPath(...).
	for _, ad := range []string{"cronYaz", "cronSil"} {
		g, ok := p.fonk[ad]
		if !ok {
			t.Errorf("%s bulunamadı", ad)
			continue
		}
		ast.Inspect(g.fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := se.X.(*ast.Ident)
			if !ok || g.paket[id.Name] == "" {
				return true
			}
			sel := path.Base(g.paket[id.Name]) + "." + se.Sel.Name
			if !yasakPaketYol[g.paket[id.Name]] {
				return true
			}
			if serbest := sel == "os.Stat" || sel == "os.Lstat"; serbest {
				return true
			}
			if len(ce.Args) == 0 || !cagriAdi(ce.Args[0], "cronDPath") {
				t.Errorf("%s: %s yol argümanı cronDPath(...) değil (kök dışına yazma riski)", ad, sel)
			}
			return true
		})
	}
}

// TestKaynakKoruma_DikisDegerleri: test dikişleri (testlerin sahtelediği paket düzeyi
// değişkenler) üretimde DAİMA güvenli değerlerinde kalmalı. Her biri test dışı kodda TAM
// BİR KEZ, beklenen initializer ile tanımlanır ve test dışı kodda hiçbir yolla yeniden
// bağlanamaz: atama (init() ve paket düzeyi kapanışlar dahil), ++/--, adres alma (&x →
// *p = ...), range/select/type-switch hedefi, yerel gölgeleme (x := / var x / parametre /
// alıcı adı) ya da go:linkname. Aksi halde ör. `var envYazici = kokYaz` veya
// `func init() { envYazici = kokYaz }` ile PUT /laravel/env root olarak symlink izleyen bir
// yazıcıya bağlanırdı; davranış testleri dikişi sahteyle değiştirdiği için bunu göremez.
func TestKaynakKoruma_DikisDegerleri(t *testing.T) {
	beklenen := map[string]string{
		"envYazici":    "envYaz",       // kiracı uid'siyle kiraci.Komut tee (KiraciCapali kilitler)
		"kimlikBul":    "tenantKimlik", // user.Lookup
		"evKoku":       `"/home"`,
		"cronDizini":   "cronDDizin",
		"cronDDizin":   `"/etc/cron.d"`,
		"yarisKancasi": "", // üretimde nil
	}
	p := paketKaynagiOku(t)
	tanim := map[string]int{}
	ustDuzey := map[*ast.Ident]bool{}
	for _, f := range p.dosyalar {
		for _, dc := range f.Decls {
			gd, ok := dc.(*ast.GenDecl)
			if !ok || (gd.Tok != token.VAR && gd.Tok != token.CONST) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ad := range vs.Names {
					b, izlenen := beklenen[ad.Name]
					if !izlenen {
						continue
					}
					ustDuzey[ad] = true
					tanim[ad.Name]++
					deger := ""
					switch {
					case len(vs.Values) == len(vs.Names):
						deger = types.ExprString(vs.Values[i])
					case len(vs.Values) != 0:
						deger = "<çoklu değer>"
					}
					if deger != b {
						t.Errorf("%s: dikiş %s = %q, üretimde %q olmalı", p.fset.Position(ad.Pos()), ad.Name, deger, b)
					}
				}
			}
		}
	}
	for ad := range beklenen {
		if tanim[ad] != 1 {
			t.Errorf("dikiş %s test dışı kodda paket düzeyinde %d kez tanımlı; tam 1 olmalı", ad, tanim[ad])
		}
	}
	bagla := func(e ast.Expr, nasil string) {
		for {
			pe, ok := e.(*ast.ParenExpr)
			if !ok {
				break
			}
			e = pe.X
		}
		if id, ok := e.(*ast.Ident); ok && !ustDuzey[id] {
			if _, izlenen := beklenen[id.Name]; izlenen {
				t.Errorf("%s: dikiş %s test dışı kodda %s (yalnız testler değiştirebilir)", p.fset.Position(id.Pos()), id.Name, nasil)
			}
		}
	}
	alanlar := func(fl *ast.FieldList, nasil string) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, ad := range f.Names {
				bagla(ad, nasil)
			}
		}
	}
	for _, f := range p.dosyalar {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:linkname") {
					t.Errorf("%s: go:linkname test dışı kodda yasak (dikişler dışarıdan bağlanabilir)", p.fset.Position(c.Pos()))
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					bagla(l, "atanıyor/gölgeleniyor")
				}
			case *ast.IncDecStmt:
				bagla(x.X, "değiştiriliyor")
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					bagla(x.X, "adresi alınıyor")
				}
			case *ast.RangeStmt:
				if x.Key != nil {
					bagla(x.Key, "range hedefi")
				}
				if x.Value != nil {
					bagla(x.Value, "range hedefi")
				}
			case *ast.ValueSpec:
				for _, ad := range x.Names {
					bagla(ad, "yeniden tanımlanıyor (gölge)")
				}
			case *ast.FuncDecl:
				alanlar(x.Recv, "alıcı adıyla gölgeleniyor")
			case *ast.FuncType:
				alanlar(x.Params, "parametreyle gölgeleniyor")
				alanlar(x.Results, "sonuç adıyla gölgeleniyor")
			}
			return true
		})
	}
}

// kiraciKomutCagrisi: çağrı `kiraci.Kiraci{...}.Komut(...)` mı (internal/kiraci içe
// aktarılmış, alıcı bileşik değişmez)?
func kiraciKomutCagrisi(ce *ast.CallExpr, paket map[string]string) bool {
	se, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || se.Sel.Name != "Komut" {
		return false
	}
	cl, ok := se.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	tip, ok := cl.Type.(*ast.SelectorExpr)
	if !ok || tip.Sel.Name != "Kiraci" {
		return false
	}
	id, ok := tip.X.(*ast.Ident)
	return ok && paket[id.Name] == "girginospanel/internal/kiraci"
}

// cagriAdi: ifade `ad(...)` biçiminde bir çağrı mı?
func cagriAdi(e ast.Expr, ad string) bool {
	ce, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ce.Fun.(*ast.Ident)
	return ok && id.Name == ad
}

// Çapalı yardımcılar YALNIZ fd/*at tabanlı syscall kullanır: açılıştan sonra yol ile
// (os.Remove/os.Chmod/os.ReadFile/unix.Unlink/...) işlem yapmak, açma ile işlem arasında
// ara bileşenin symlink'e takası yarışını geri getirir — bu fark statik testte görünmez,
// bu yüzden kaynak düzeyinde kilitlenir. unix.Open yalnız ev kökünü O_NOFOLLOW ile açmak
// için serbesttir.
func TestKaynakKoruma_CapaliYardimcilarFdTabanli(t *testing.T) {
	izinli := map[string]bool{
		"os.NewFile": true,
		"unix.Open":  true, "unix.Openat2": true, "unix.Openat": true, "unix.Close": true,
		"unix.Fstat": true, "unix.Fstatat": true, "unix.Fchmod": true, "unix.Fchown": true,
		"unix.Fchownat": true, "unix.Unlinkat": true, "unix.Mkdirat": true,
		"unix.Getdents": true, "unix.ParseDirent": true,
		"unix.OpenHow": true, "unix.Stat_t": true, "unix.ENOENT": true, "unix.EEXIST": true,
	}
	sabit := []string{"unix.O_", "unix.RESOLVE_", "unix.S_IF", "unix.AT_"}
	izin := func(s string) bool {
		if izinli[s] {
			return true
		}
		for _, o := range sabit {
			if strings.HasPrefix(s, o) {
				return true
			}
		}
		return false
	}
	p := paketKaynagiOku(t)
	for _, ad := range []string{"evAltiAc", "evAltiDosyaOku", "evAltiChmod", "evAltiSil", "evAltiMkdirHepsi",
		"evAltiSahiplikVer", "sahiplikAgaci", "sahiplikGirdi", "dizinBos", "fdDuzenliVeSahip", "envOrnektenUret"} {
		g, ok := p.fonk[ad]
		if !ok {
			t.Errorf("%s bulunamadı (yeniden adlandırıldıysa bu koruma testini güncelle)", ad)
			continue
		}
		_, _, ihlal := p.incele(g, izin)
		for _, i := range ihlal {
			t.Errorf("%s: fd/*at tabanlı olmayan %s", ad, i)
		}
	}
}
