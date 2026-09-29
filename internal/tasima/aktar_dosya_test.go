package tasima

// aktar_dosya_test.go — H3 regresyonu: taşımanın dosya adımı kiracı ağacına kök
// olarak yol üzerinden YAZMAZ.
//
// Kiracının kurduğu bağlar (sembolik/sabit) ve ev dışı hedefler varken KURBAN
// dosyaların (kök sahipli dizin, komşu kiracı evi) mod/sahip/içeriği değişmez;
// meşru taşımada dosyalar kiracıya ait oluşur, özel dosyalar (aygıt/fifo)
// taşınmaz, hazırlık alanı yalnız köke açıktır ve iş sonunda silinir.
//
// Her şey t.TempDir içinde; kiracı yerine ayrıcalıksız "nobody" (runuser ile
// GERÇEK kimlik düşürme), uzak sunucu yerine sahte uzakKabuk (ağa çıkılmaz).
// Komşu kiracı da testte "nobody"dir: böylece ev kilidi (os.Root) ve ev dışı
// denetimi uid farkına yaslanmadan TEK BAŞINA sınanır. root değilse atlanır.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	yrSK        = "c_test"
	yrUzakKok   = "/var/www/vhosts/ornek.com/httpdocs"
	yrUzakUyg   = "/var/www/vhosts/ornek.com/uygulama"
	yrAlan      = "ornek.com"
	yrKurbanIci = "KURBAN-ICERIGI-51d2"
)

type yrOrtam struct {
	base, ev string
	uid, gid int
}

// yrYeniOrtam — geçici kök: <base>/c_test (kiracı evi, nobody), hazırlık kökü,
// yapılandırma yedek kökü; taşımanın dikişleri buraya bağlanır.
func yrYeniOrtam(t *testing.T) yrOrtam {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("runuser ile kiracı kimliği için root gerekli")
	}
	for _, b := range []string{"runuser", "rsync", "mkdir"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s yok: %v", b, err)
		}
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody kullanıcısı yok: %v", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	base := t.TempDir()
	// nobody'nin geçici köke inebilmesi için (yalnız geçme izni, listeleme yok)
	for _, d := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(d, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	eskiEv, eskiKul, eskiHaz, eskiYedek := evKoku, kiraciKul, hazirlikKoku, tasimaYedekKok
	evKoku = base
	kiraciKul = func(string) string { return "nobody" }
	hazirlikKoku = filepath.Join(base, ".gosp-tasima")
	tasimaYedekKok = filepath.Join(base, "yedek")
	t.Cleanup(func() { evKoku, kiraciKul, hazirlikKoku, tasimaYedekKok = eskiEv, eskiKul, eskiHaz, eskiYedek })

	o := yrOrtam{base: base, ev: filepath.Join(base, yrSK), uid: uid, gid: gid}
	o.dizin(t, o.ev)
	if err := exec.Command("runuser", "-u", "nobody", "--", "test", "-w", o.ev).Run(); err != nil {
		t.Fatalf("nobody geçici ev dizinine erişemiyor (TMPDIR izinleri?): %v", err)
	}
	return o
}

// dizin / dosya / bag — kiracıya (nobody) ait girdi (müşterinin FTP/SSH/PHP ile yapabildiği).
func (o yrOrtam) dizin(t *testing.T, yol string) {
	t.Helper()
	if err := os.Mkdir(yol, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(yol, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

func (o yrOrtam) dosya(t *testing.T, yol, icerik string, mod os.FileMode) {
	t.Helper()
	yrYaz(t, yol, icerik, mod)
	if err := os.Lchown(yol, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

func (o yrOrtam) bag(t *testing.T, hedef, bag string) {
	t.Helper()
	if err := os.Symlink(hedef, bag); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(bag, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

// komsu — komşu kiracının evi (testte AYNI uid: nobody) ve içinde Laravel +
// WordPress yapılandırması. APP_URL zaten hedef: yeniden yazım değil, yalnız
// izin (fchmod) yolu tetiklenir.
func (o yrOrtam) komsu(t *testing.T) string {
	t.Helper()
	ph := filepath.Join(o.base, "c_komsu", "public_html")
	o.dizin(t, filepath.Dir(ph))
	o.dizin(t, ph)
	o.dosya(t, filepath.Join(ph, "wp-config.php"), "<?php\ndefine( 'DB_NAME', 'eski_db' );\ndefine( 'DB_USER', 'eski_kul' );\ndefine( 'DB_PASSWORD', 'eski_pw' );\n", 0o644)
	o.dosya(t, filepath.Join(ph, ".env"), "APP_NAME=Komsu\nAPP_URL=https://"+yrAlan+"\n", 0o644)
	o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
	o.dizin(t, filepath.Join(ph, "bootstrap"))
	o.dizin(t, filepath.Join(ph, "bootstrap", "cache"))
	o.dosya(t, filepath.Join(ph, "bootstrap", "cache", "config.php"), "<?php return [];\n", 0o644)
	return ph
}

// yrYaz — kök olarak dosya (ara dizinlerle) yazar.
func yrYaz(t *testing.T, yol, icerik string, mod os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(yol, mod); err != nil {
		t.Fatal(err)
	}
}

// yrKurban — kiracının erişemediği kök sahipli dizin (sistem/komşu) ve içinde
// bir Laravel/WordPress görünümü.
func yrKurban(t *testing.T, yol string) {
	t.Helper()
	yrYaz(t, filepath.Join(yol, "index.php"), yrKurbanIci, 0o644)
	yrYaz(t, filepath.Join(yol, "artisan"), yrKurbanIci, 0o644)
	yrYaz(t, filepath.Join(yol, ".env"), "APP_URL=http://kurban.example\n", 0o644)
	yrYaz(t, filepath.Join(yol, "wp-config.php"), "<?php define( 'DB_NAME', 'eski_db' );\n", 0o644)
	if err := os.Chmod(yol, 0o755); err != nil {
		t.Fatal(err)
	}
}

type yrGirdi struct {
	Tur, Mod fs.FileMode
	UID, GID uint32
	Icerik   string
}

// yrOzet — ağacın bağ İZLEMEDEN özeti: tür, izin, sahip, içerik sha'sı / bağ hedefi.
func yrOzet(t *testing.T, kok string) map[string]yrGirdi {
	t.Helper()
	out := map[string]yrGirdi{}
	err := filepath.WalkDir(kok, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st := fi.Sys().(*syscall.Stat_t)
		g := yrGirdi{Tur: fi.Mode().Type(), Mod: fi.Mode().Perm() | fi.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky), UID: st.Uid, GID: st.Gid}
		switch {
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s := sha256.Sum256(b)
			g.Icerik = hex.EncodeToString(s[:])
		case fi.Mode()&fs.ModeSymlink != 0:
			g.Icerik, _ = os.Readlink(p)
		}
		rel, _ := filepath.Rel(kok, p)
		out[rel] = g
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func yrOzetKarsilastir(t *testing.T, ad string, once, sonra map[string]yrGirdi) {
	t.Helper()
	if reflect.DeepEqual(once, sonra) {
		return
	}
	for p, g := range sonra {
		if o, ok := once[p]; !ok {
			t.Errorf("%s: KURBAN ağacında yeni girdi: %s %+v", ad, p, g)
		} else if o != g {
			t.Errorf("%s: KURBAN girdisi değişti: %s\n önce %+v\nsonra %+v", ad, p, o, g)
		}
	}
	for p := range once {
		if _, ok := sonra[p]; !ok {
			t.Errorf("%s: KURBAN girdisi silindi: %s", ad, p)
		}
	}
}

// ---------------------------------------------------------------------------
// Sahte uzak sunucu
// ---------------------------------------------------------------------------

type yrRsyncCagri struct {
	uzak, yerel string
	mod         fs.FileMode
	uid         uint32
}

// yrUzak — uzakKabuk sahtesi: RsyncCek, kökün rsync'ini taklit eder (kök
// olarak, hedef yoldaki bağları izleyerek yazar); hedef dizinin o anki
// izin/sahibini kaydeder.
type yrUzak struct {
	t        *testing.T
	agaclar  map[string]func(t *testing.T, dizin string)
	cagrilar []yrRsyncCagri
}

func (s *yrUzak) Calistir(context.Context, string) (string, error) {
	return "", errors.New("beklenmedik uzak komut")
}

func (s *yrUzak) AkisCalistir(context.Context, string, io.Writer) (string, error) {
	return "", errors.New("beklenmedik uzak akis")
}

func (s *yrUzak) RsyncCek(_ context.Context, uzakYol, yerelYol string, _ ...string) (string, error) {
	c := yrRsyncCagri{uzak: uzakYol, yerel: yerelYol}
	if fi, err := os.Lstat(strings.TrimSuffix(yerelYol, "/")); err == nil {
		c.mod, c.uid = fi.Mode(), fi.Sys().(*syscall.Stat_t).Uid
	}
	s.cagrilar = append(s.cagrilar, c)
	uret, ok := s.agaclar[strings.TrimSuffix(uzakYol, "/")]
	if !ok {
		return "", errors.New("rsync: uzak dizin yok")
	}
	if err := os.MkdirAll(yerelYol, 0o755); err != nil {
		return "", err
	}
	uret(s.t, yerelYol)
	return "", nil
}

// yrKaynakDocroot — kaynağın belge kökü: docroot dışı bağımlılık (../uygulama/),
// uzak uid'li dosya (--numeric-ids), ağaç içi bağ, FIFO ve (yapılabiliyorsa)
// aygıt düğümü — kök rsync -a bunları hazırlık alanına yaratır.
func yrKaynakDocroot(t *testing.T, d string) {
	yrYaz(t, filepath.Join(d, "index.php"), "<?php require __DIR__.'/../uygulama/vendor/autoload.php';\n", 0o644)
	yrYaz(t, filepath.Join(d, "alt", "dosya.txt"), "icerik\n", 0o644)
	if err := os.Lchown(filepath.Join(d, "alt", "dosya.txt"), 4242, 4242); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alt/dosya.txt", filepath.Join(d, "bag")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(d, "boru"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mknod(filepath.Join(d, "aygit"), unix.S_IFCHR|0o666, int(unix.Mkdev(1, 3))); err != nil {
		t.Logf("aygıt düğümü yaratılamadı (yalnız FIFO sınanır): %v", err)
	}
}

// yrKaynakUygulama — belge kökü dışındaki Laravel uygulaması.
func yrKaynakUygulama(t *testing.T, d string) {
	yrYaz(t, filepath.Join(d, "artisan"), "#!/usr/bin/env php\n", 0o644)
	yrYaz(t, filepath.Join(d, ".env"), "APP_NAME=Ornek\nAPP_URL=http://eski.example\n", 0o644)
	yrYaz(t, filepath.Join(d, "bootstrap", "cache", "config.php"), "<?php return [];\n", 0o644)
	yrYaz(t, filepath.Join(d, "storage", "logs", "a.log"), "", 0o644)
}

func yrKaynak(s *yrUzak) *Kaynak {
	return &Kaynak{Tip: "plesk", Host: "192.0.2.10", Port: 22, Kullanici: "root", Parola: "uzak-parola-1234", kabuk: s}
}

func yrLog(t *testing.T) func(string, ...any) {
	return func(f string, a ...any) { t.Logf(f, a...) }
}

func yrHazirlikBos(t *testing.T) {
	t.Helper()
	g, err := os.ReadDir(hazirlikKoku)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if len(g) != 0 {
		t.Errorf("hazırlık alanı temizlenmedi: %v", g)
	}
}

// ---------------------------------------------------------------------------
// Meşru taşıma
// ---------------------------------------------------------------------------

func TestDosyalariAktar_KiraciKimligindeYerlesir(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	s := &yrUzak{t: t, agaclar: map[string]func(*testing.T, string){yrUzakKok: yrKaynakDocroot, yrUzakUyg: yrKaynakUygulama}}
	sonuc := &AktarSonuc{}

	err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
		Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, sonuc, yrLog(t))
	if err != nil {
		t.Fatalf("dosyalariAktar: %v", err)
	}

	// Uzak rsync YALNIZ kökün erişebildiği hazırlık dizinine indi.
	if len(s.cagrilar) != 2 {
		t.Fatalf("uzak rsync çağrıları: %+v", s.cagrilar)
	}
	for _, c := range s.cagrilar {
		d := strings.TrimSuffix(c.yerel, "/")
		if filepath.Dir(d) != hazirlikKoku || !c.mod.IsDir() || c.mod.Perm() != 0o700 || c.uid != 0 {
			t.Errorf("uzak rsync hazırlık alanına (kök, 0700) inmedi: %+v", c)
		}
	}
	yrHazirlikBos(t)

	// Kiracı ağacı: her girdi kiracıya ait, yalnız dizin/dosya/bağ (aygıt/fifo YOK).
	agac := yrOzet(t, o.ev)
	for _, p := range []string{"public_html/index.php", "public_html/alt/dosya.txt", "uygulama/artisan", "uygulama/storage/logs/a.log"} {
		if agac[p].Tur != 0 {
			t.Errorf("%s düzenli dosya olarak yerleşmedi: %+v", p, agac[p])
		}
	}
	for p, g := range agac {
		if g.UID != uint32(o.uid) {
			t.Errorf("%s kiracıya ait değil: uid=%d", p, g.UID)
		}
		if g.Tur != 0 && g.Tur != fs.ModeDir && g.Tur != fs.ModeSymlink {
			t.Errorf("özel dosya kiracı ağacına taşındı: %s (%v)", p, g.Tur)
		}
		if strings.Contains(p, ".gosp_cfg_") {
			t.Errorf("geçici yapılandırma dosyası kaldı: %s", p)
		}
	}
	if g := agac["public_html/bag"]; g.Tur != fs.ModeSymlink || g.Icerik != "alt/dosya.txt" {
		t.Errorf("ağaç içi bağ korunmadı: %+v", g)
	}

	// Laravel düzeltmesi (docroot dışı uygulama): APP_URL, .env 0600, önbellek.
	env := filepath.Join(o.ev, "uygulama", ".env")
	b, err := os.ReadFile(env)
	if err != nil || !strings.Contains(string(b), "APP_URL=https://"+yrAlan+"\n") || strings.Contains(string(b), "eski.example") {
		t.Errorf("APP_URL güncellenmedi: %q (%v)", b, err)
	}
	if g := agac["uygulama/.env"]; g.Mod != 0o600 {
		t.Errorf(".env izni %v, beklenen 0600", g.Mod)
	}
	if _, err := os.Lstat(filepath.Join(o.ev, "uygulama", "bootstrap", "cache", "config.php")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Laravel önbelleği silinmedi: %v", err)
	}

	if sonuc.DosyaBayt <= 0 {
		t.Errorf("DosyaBayt = %d", sonuc.DosyaBayt)
	}
	uyari := strings.Join(sonuc.Uyarilar, "|")
	if !strings.Contains(uyari, "özel dosya") {
		t.Errorf("özel dosya uyarısı yok: %q", uyari)
	}
}

// ---------------------------------------------------------------------------
// Bağlar varken kurban değişmez (dosya yerleşimi)
// ---------------------------------------------------------------------------

func TestDosyalariAktar_BaglarKurbaniDegistirmez(t *testing.T) {
	senaryolar := []struct {
		ad string
		// kur: kiracı ağacını hazırlar; döner: web kökü ve özeti alınacak kurban.
		kur    func(t *testing.T, o yrOrtam) (webRoot, kurban string)
		hataOk bool // kalem hata ile bitmeli mi
	}{
		{"public_html kök dizinine bağ", func(t *testing.T, o yrOrtam) (string, string) {
			kurban := filepath.Join(o.base, "kurban")
			yrKurban(t, kurban)
			o.bag(t, kurban, filepath.Join(o.ev, "public_html"))
			return filepath.Join(o.ev, "public_html"), kurban
		}, true},
		{"ara bileşen bağ, hedef dizin yok", func(t *testing.T, o yrOrtam) (string, string) {
			kurban := filepath.Join(o.base, "kurban")
			yrKurban(t, kurban)
			o.bag(t, kurban, filepath.Join(o.ev, "public_html"))
			return filepath.Join(o.ev, "public_html", "public"), kurban
		}, true},
		{"docroot dışı uygulama dizini bağ", func(t *testing.T, o yrOrtam) (string, string) {
			kurban := filepath.Join(o.base, "kurban")
			yrKurban(t, kurban)
			o.dizin(t, filepath.Join(o.ev, "public_html"))
			o.bag(t, kurban, filepath.Join(o.ev, "uygulama"))
			return filepath.Join(o.ev, "public_html"), kurban
		}, false},
		{"web kökü komşu kiracının evinde", func(t *testing.T, o yrOrtam) (string, string) {
			ph := o.komsu(t)
			return ph, filepath.Dir(ph)
		}, true},
	}
	for _, sn := range senaryolar {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			webRoot, kurban := sn.kur(t, o)
			once := yrOzet(t, kurban)
			s := &yrUzak{t: t, agaclar: map[string]func(*testing.T, string){yrUzakKok: yrKaynakDocroot, yrUzakUyg: yrKaynakUygulama}}
			sonuc := &AktarSonuc{}

			err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
				Ayarlar{Dosyalar: true}, yrSK, webRoot, yrAlan, sonuc, yrLog(t))
			if sn.hataOk != (err != nil) {
				t.Errorf("hata = %v (hata beklenen=%v)", err, sn.hataOk)
			}
			yrOzetKarsilastir(t, sn.ad, once, yrOzet(t, kurban))
			yrHazirlikBos(t)
		})
	}
}

// ---------------------------------------------------------------------------
// Hazırlık alanı
// ---------------------------------------------------------------------------

// Devir: özel dosyalar silinir, sahiplik bağ İZLENMEDEN geçer, hazırlık
// dizininin kendisi (kiracıya kapıyı açan adım) EN SON devredilir.
func TestHazirlikDevret(t *testing.T) {
	o := yrYeniOrtam(t)
	kurban := filepath.Join(o.base, "kurban.txt")
	yrYaz(t, kurban, yrKurbanIci, 0o640)
	once := yrOzet(t, kurban)

	dizin := filepath.Join(o.base, "hazirlik")
	if err := os.Mkdir(dizin, 0o700); err != nil {
		t.Fatal(err)
	}
	yrKaynakDocroot(t, dizin)
	if err := os.Symlink(kurban, filepath.Join(dizin, "mutlak")); err != nil {
		t.Fatal(err)
	}
	var sira []string
	onceki := lchown
	lchown = func(p string, u, g int) error {
		sira = append(sira, p)
		return onceki(p, u, g)
	}
	t.Cleanup(func() { lchown = onceki })

	atlanan, err := hazirlikDevret(dizin, o.uid, o.gid)
	if err != nil {
		t.Fatal(err)
	}
	if atlanan < 1 {
		t.Errorf("özel dosya silinmedi (atlanan=%d)", atlanan)
	}
	for p, g := range yrOzet(t, dizin) {
		if g.Tur != 0 && g.Tur != fs.ModeDir && g.Tur != fs.ModeSymlink {
			t.Errorf("özel dosya kiracıya devredildi: %s (%v)", p, g.Tur)
		}
		if g.UID != uint32(o.uid) || g.GID != uint32(o.gid) {
			t.Errorf("%s kiracıya devredilmedi: %d:%d", p, g.UID, g.GID)
		}
	}
	yrOzetKarsilastir(t, "bağ hedefi", once, yrOzet(t, kurban))
	if len(sira) == 0 || sira[len(sira)-1] != dizin {
		t.Fatalf("hazırlık dizini en son devredilmedi: %q", sira)
	}
	for _, p := range sira[:len(sira)-1] {
		if p == dizin {
			t.Fatalf("hazırlık dizini içerik devrinden ÖNCE kiracıya açıldı: %q", sira)
		}
	}
}

// Hazırlık dizini yalnız köke açık (0700) ve hazırlık kökü kiracının yalnız
// geçebileceği (0711) biçimde açılır; yarıda kalmış eski dizinler süpürülür.
func TestHazirlikAc_IzinVeSupurme(t *testing.T) {
	yrYeniOrtam(t)
	simdi := time.Now()
	eski := filepath.Join(hazirlikKoku, "c_eski-"+strconv.FormatInt(simdi.Add(-hazirlikOmru-time.Minute).Unix(), 10)+"-1")
	taze := filepath.Join(hazirlikKoku, "c_taze-"+strconv.FormatInt(simdi.Add(-time.Minute).Unix(), 10)+"-2")
	garip := filepath.Join(hazirlikKoku, "garip")
	for _, d := range []string{eski, taze, garip} {
		if err := os.MkdirAll(filepath.Join(d, "ic"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ki, err := kiraciAl(yrSK)
	if err != nil {
		t.Fatal(err)
	}
	yeni, err := hazirlikAc(ki, simdi)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(yeni)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 || fi.Sys().(*syscall.Stat_t).Uid != 0 {
		t.Fatalf("hazırlık dizini kök/0700 değil: %v %v", fi, err)
	}
	if !strings.HasPrefix(filepath.Base(yeni), yrSK+"-") || filepath.Dir(yeni) != hazirlikKoku {
		t.Errorf("hazırlık dizini adı/yeri: %s", yeni)
	}
	if fi, _ := os.Lstat(hazirlikKoku); fi.Mode().Perm() != 0o711 {
		t.Errorf("hazırlık kökü izni %v, beklenen 0711", fi.Mode().Perm())
	}
	for d, kalmali := range map[string]bool{eski: false, garip: false, taze: true, yeni: true} {
		if _, err := os.Lstat(d); (err == nil) != kalmali {
			t.Errorf("%s: kalmalı=%v, err=%v", filepath.Base(d), kalmali, err)
		}
	}
}
