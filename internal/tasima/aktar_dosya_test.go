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
	"fmt"
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
	for _, b := range []string{"runuser", "rsync", "mkdir", "cp", "find"} {
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

// yrYabanciUID — kaynak sunucunun seçtiği, hedefte hesabı olmayan sayısal uid.
const yrYabanciUID = 4242

// yrSahip — bir yolun (bağ izlenmeden) türü+izni ve sahibi.
type yrSahip struct {
	mod fs.FileMode
	uid uint32
}

func yrSahipAl(p string) yrSahip {
	fi, err := os.Lstat(p)
	if err != nil {
		return yrSahip{}
	}
	return yrSahip{fi.Mode(), fi.Sys().(*syscall.Stat_t).Uid}
}

// yrRsyncCagri — bir uzak rsync: sarmalayıcının (yerel yolun üst dizini)
// aktarım öncesi/sonrası durumu ve yerel yolun aktarım sonrası sahibi.
type yrRsyncCagri struct {
	uzak, yerel         string
	sarmOnce, sarmSonra yrSahip
	hedef               yrSahip
}

// yrUzak — uzakKabuk sahtesi. RsyncCek kök rsync'ini GERÇEK rsync ile taklit
// eder: kaynak ağacı geçici bir dizinde üretilir, kökü (".") kokUID'e verilir
// ve `rsync -a --numeric-ids` (tasima.go RsyncCek'in sahiplik taşıyan çekirdeği;
// S paketinden bağımsız en kötü durum) ile yerel yola çekilir — gerçek
// aktarımdaki gibi yerel yolun KENDİSİ de kaynağın seçtiği uid'e geçer.
type yrUzak struct {
	t        *testing.T
	agaclar  map[string]func(t *testing.T, dizin string)
	kokUID   int
	cagrilar []yrRsyncCagri
}

func yrYeniUzak(t *testing.T, kokUID int) *yrUzak {
	return &yrUzak{t: t, kokUID: kokUID, agaclar: map[string]func(*testing.T, string){yrUzakKok: yrKaynakDocroot, yrUzakUyg: yrKaynakUygulama}}
}

func (s *yrUzak) Calistir(context.Context, string) (string, error) {
	return "", errors.New("beklenmedik uzak komut")
}

func (s *yrUzak) AkisCalistir(context.Context, string, io.Writer) (string, error) {
	return "", errors.New("beklenmedik uzak akis")
}

func (s *yrUzak) RsyncCek(ctx context.Context, uzakYol, yerelYol string, _ ...string) (string, error) {
	uret, ok := s.agaclar[strings.TrimSuffix(uzakYol, "/")]
	if !ok {
		return "", errors.New("rsync: uzak dizin yok")
	}
	yerel := strings.TrimSuffix(yerelYol, "/")
	c := yrRsyncCagri{uzak: uzakYol, yerel: yerel, sarmOnce: yrSahipAl(filepath.Dir(yerel))}
	kaynak := s.t.TempDir()
	uret(s.t, kaynak)
	if err := os.Lchown(kaynak, s.kokUID, s.kokUID); err != nil {
		return "", err
	}
	if out, err := exec.CommandContext(ctx, "rsync", "-a", "--numeric-ids", kaynak+"/", yerelYol).CombinedOutput(); err != nil {
		return "", fmt.Errorf("rsync: %v: %s", err, out)
	}
	c.sarmSonra, c.hedef = yrSahipAl(filepath.Dir(yerel)), yrSahipAl(yerel)
	s.cagrilar = append(s.cagrilar, c)
	return "", nil
}

// yrKaynakDocroot — kaynağın belge kökü: docroot dışı bağımlılık (../uygulama/),
// uzak uid'li dosya (--numeric-ids), ağaç içi bağ, FIFO ve (yapılabiliyorsa)
// aygıt düğümü — kök rsync -a bunları hazırlık alanına yaratır.
func yrKaynakDocroot(t *testing.T, d string) {
	yrYaz(t, filepath.Join(d, "index.php"), "<?php require __DIR__.'/../uygulama/vendor/autoload.php';\n", 0o644)
	yrYaz(t, filepath.Join(d, "alt", "dosya.txt"), "icerik\n", 0o644)
	if err := os.Lchown(filepath.Join(d, "alt", "dosya.txt"), yrYabanciUID, yrYabanciUID); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alt/dosya.txt", filepath.Join(d, "bag")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alt", filepath.Join(d, "dizinbag")); err != nil {
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
	s := yrYeniUzak(t, yrYabanciUID)
	sonuc := &AktarSonuc{}

	err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
		Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, sonuc, yrLog(t))
	if err != nil {
		t.Fatalf("dosyalariAktar: %v", err)
	}

	// Uzak rsync YALNIZ kökün erişebildiği sarmalayıcının içine indi; sarmalayıcı
	// aktarımdan SONRA da kök/0700 (rsync -a yalnız kendi hedefinin sahibini değiştirir).
	if len(s.cagrilar) != 2 {
		t.Fatalf("uzak rsync çağrıları: %+v", s.cagrilar)
	}
	for _, c := range s.cagrilar {
		if filepath.Dir(filepath.Dir(c.yerel)) != hazirlikKoku || filepath.Base(c.yerel) != hazirlikIcerik {
			t.Errorf("uzak rsync hazırlık sarmalayıcısının içine inmedi: %+v", c)
		}
		for _, d := range []yrSahip{c.sarmOnce, c.sarmSonra} {
			if d.mod != fs.ModeDir|0o700 || d.uid != 0 {
				t.Errorf("sarmalayıcı aktarım boyunca kök/0700 değil: %+v", c)
			}
		}
		// Fikstür anlamlı: gerçek rsync -a hedefin KENDİSİNİ kaynağın uid'ine verdi.
		if c.hedef.uid != yrYabanciUID {
			t.Errorf("sahte kaynak hedef dizinin sahipliğini taşımadı: %+v", c)
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
	for bag, hedef := range map[string]string{"public_html/bag": "alt/dosya.txt", "public_html/dizinbag": "alt"} {
		if g := agac[bag]; g.Tur != fs.ModeSymlink || g.Icerik != hedef {
			t.Errorf("ağaç içi bağ korunmadı: %s %+v", bag, g)
		}
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
			s := yrYeniUzak(t, yrYabanciUID)
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

// Kaynak sunucu, rsync -a --numeric-ids ile hazırlık hedefinin sahipliğini
// YEREL bir hesabın uid'ine verir (burada nobody); o hesap kök rsync ile devir
// arasındaki pencerede — kökün devir yürüyüşü yolları topladıktan SONRA — ara
// dizini kurbana bağa çevirmeye çalışır. Bağ izlenseydi kökün lchown'ı KURBANI
// kiracıya verirdi. Sarmalayıcı devrin sonuna kadar yalnız kökün olduğu için
// saldırgan içeri giremez; taşıma yine de başarıyla biter.
func TestDosyalariAktar_YabanciSahipliHazirlikKurbaniDegistirmez(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	kurban := filepath.Join(o.base, "kurban")
	yrYaz(t, filepath.Join(kurban, "dosya.txt"), yrKurbanIci, 0o644)
	once := yrOzet(t, kurban)

	s := yrYeniUzak(t, o.uid)
	s.agaclar = map[string]func(*testing.T, string){yrUzakKok: func(t *testing.T, d string) {
		yrYaz(t, filepath.Join(d, "alt", "dosya.txt"), "icerik\n", 0o644)
	}}
	var saldiri error
	saldirildi := false
	onceki := lchown
	lchown = func(p string, u, g int) error {
		err := onceki(p, u, g)
		if !saldirildi {
			saldirildi = true
			hedef := s.cagrilar[len(s.cagrilar)-1].yerel
			saldiri = exec.Command("runuser", "-u", "nobody", "--", "sh", "-c",
				`mv -- "$1/alt" "$1/alt.eski" && ln -s -- "$2" "$1/alt"`, "sh", hedef, kurban).Run()
		}
		return err
	}
	t.Cleanup(func() { lchown = onceki })

	err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
		Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, &AktarSonuc{}, yrLog(t))
	if err != nil {
		t.Errorf("dosyalariAktar: %v", err)
	}
	if len(s.cagrilar) != 1 || s.cagrilar[0].hedef.uid != uint32(o.uid) {
		t.Fatalf("fikstür: rsync hedefi saldırgan uid'ine geçmedi: %+v", s.cagrilar)
	}
	if !saldirildi || saldiri == nil {
		t.Errorf("saldırgan devir sırasında hazırlık ağacına girebildi (saldırı=%v, denendi=%v)", saldiri, saldirildi)
	}
	yrOzetKarsilastir(t, "yabancı sahipli hazırlık", once, yrOzet(t, kurban))
	if b, err := os.ReadFile(filepath.Join(ph, "alt", "dosya.txt")); err != nil || string(b) != "icerik\n" {
		t.Errorf("içerik yerleşmedi: %q (%v)", b, err)
	}
	yrHazirlikBos(t)
}

// ---------------------------------------------------------------------------
// Hazırlık alanı
// ---------------------------------------------------------------------------

// yrSarmalayici — hazirlikAc ile açılmış iş dizini ve içindeki rsync hedefi.
func yrSarmalayici(t *testing.T) (sarm, icerik string) {
	t.Helper()
	ki, err := kiraciAl(yrSK)
	if err != nil {
		t.Fatal(err)
	}
	sarm, err = hazirlikAc(ki, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	icerik = filepath.Join(sarm, hazirlikIcerik)
	if err := os.Mkdir(icerik, 0o755); err != nil {
		t.Fatal(err)
	}
	return sarm, icerik
}

// yrLchownKaydet — lchown çağrılarını sırayla kaydeder (gerçek çağrı yapılır).
func yrLchownKaydet(t *testing.T) *[]string {
	var sira []string
	onceki := lchown
	lchown = func(p string, u, g int) error {
		sira = append(sira, p)
		return onceki(p, u, g)
	}
	t.Cleanup(func() { lchown = onceki })
	return &sira
}

// Devir: özel dosyalar silinir, sahiplik bağ İZLENMEDEN geçer (içerik dizininin
// kendisi dahil), sarmalayıcı (kiracıya kapıyı açan adım) EN SON devredilir.
func TestHazirlikDevret(t *testing.T) {
	o := yrYeniOrtam(t)
	kurban := filepath.Join(o.base, "kurban.txt")
	yrYaz(t, kurban, yrKurbanIci, 0o640)
	once := yrOzet(t, kurban)

	sarm, icerik := yrSarmalayici(t)
	yrKaynakDocroot(t, icerik)
	if err := os.Symlink(kurban, filepath.Join(icerik, "mutlak")); err != nil {
		t.Fatal(err)
	}
	sira := yrLchownKaydet(t)

	atlanan, err := hazirlikDevret(sarm, o.uid, o.gid)
	if err != nil {
		t.Fatal(err)
	}
	if atlanan < 1 {
		t.Errorf("özel dosya silinmedi (atlanan=%d)", atlanan)
	}
	for p, g := range yrOzet(t, sarm) {
		if g.Tur != 0 && g.Tur != fs.ModeDir && g.Tur != fs.ModeSymlink {
			t.Errorf("özel dosya kiracıya devredildi: %s (%v)", p, g.Tur)
		}
		if g.UID != uint32(o.uid) || g.GID != uint32(o.gid) {
			t.Errorf("%s kiracıya devredilmedi: %d:%d", p, g.UID, g.GID)
		}
	}
	yrOzetKarsilastir(t, "bağ hedefi", once, yrOzet(t, kurban))
	if len(*sira) == 0 || (*sira)[len(*sira)-1] != sarm {
		t.Fatalf("sarmalayıcı en son devredilmedi: %q", *sira)
	}
	for _, p := range (*sira)[:len(*sira)-1] {
		if p == sarm {
			t.Fatalf("sarmalayıcı içerik devrinden ÖNCE kiracıya açıldı: %q", *sira)
		}
	}
}

// Önkoşul kapalı hata: sarmalayıcı artık yalnız panelin 0700 dizini değilse
// (içerik yarışa açık) devir YAPILMAZ, hiçbir girdinin sahibi değişmez.
func TestHazirlikDevret_SarmalayiciAcilmissaReddeder(t *testing.T) {
	for _, sn := range []struct {
		ad  string
		boz func(t *testing.T, o yrOrtam, sarm string) error
	}{
		{"yabancı sahip", func(t *testing.T, o yrOrtam, sarm string) error {
			return os.Lchown(sarm, o.uid, o.gid)
		}},
		{"izin genişlemiş", func(t *testing.T, o yrOrtam, sarm string) error {
			return os.Chmod(sarm, 0o711)
		}},
		{"sarmalayıcı yerine bağ", func(t *testing.T, o yrOrtam, sarm string) error {
			if err := os.Rename(sarm, sarm+".gercek"); err != nil {
				return err
			}
			return os.Symlink(sarm+".gercek", sarm)
		}},
	} {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			sarm, icerik := yrSarmalayici(t)
			yrYaz(t, filepath.Join(icerik, "alt", "dosya.txt"), "icerik\n", 0o644)
			if err := sn.boz(t, o, sarm); err != nil {
				t.Fatal(err)
			}
			sira := yrLchownKaydet(t)
			if _, err := hazirlikDevret(sarm, o.uid, o.gid); err == nil {
				t.Error("yalnız panele açık olmayan sarmalayıcı devredildi")
			}
			if len(*sira) != 0 {
				t.Errorf("reddedilen devirde sahiplik değişti: %q", *sira)
			}
		})
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

// ---------------------------------------------------------------------------
// Yerleşim: kota, donmuş kiracı süreci, okunamayan modlar
// ---------------------------------------------------------------------------

// Parçalı kaynak: S = yrParcaSayi × yrParcaBoyut (8 alt dizinde).
const (
	yrParcaBoyut = 512 << 10
	yrParcaSayi  = 64
	yrParcaS     = yrParcaSayi * yrParcaBoyut
)

func yrParcaliKaynak(t *testing.T, d string) {
	icerik := strings.Repeat("k", yrParcaBoyut)
	for i := range yrParcaSayi {
		yrYaz(t, filepath.Join(d, fmt.Sprintf("p%d", i%8), fmt.Sprintf("dosya%02d.bin", i)), icerik, 0o644)
	}
}

// yrKotaSayaci — bayt sayacı (sahte kota): kok altında uid'e ait düzenli
// dosyaların toplam boyutunu disk kotasının saydığı gibi ölçer — SAHİBE göre
// (hazırlık alanı dahil) ve INODE başına (sabit bağlar bir kez) — dur çağrılana
// dek sürekli. dur: en yüksek değer, örnek sayısı.
func yrKotaSayaci(kok string, uid int) (dur func() (int64, int)) {
	bitti := make(chan struct{})
	sonuc := make(chan [2]int64, 1)
	go func() {
		var tepe, ornek int64
		for {
			var top int64
			gorulen := map[[2]uint64]bool{}
			_ = filepath.WalkDir(kok, func(_ string, d fs.DirEntry, err error) error {
				if err != nil || !d.Type().IsRegular() {
					return nil
				}
				fi, err := d.Info()
				if err != nil {
					return nil
				}
				st := fi.Sys().(*syscall.Stat_t)
				if anahtar := [2]uint64{uint64(st.Dev), st.Ino}; st.Uid == uint32(uid) && !gorulen[anahtar] {
					gorulen[anahtar] = true
					top += fi.Size()
				}
				return nil
			})
			tepe, ornek = max(tepe, top), ornek+1
			select {
			case <-bitti:
				sonuc <- [2]int64{tepe, ornek}
				return
			default:
			}
		}
	}()
	return func() (int64, int) {
		close(bitti)
		s := <-sonuc
		return s[0], int(s[1])
	}
}

// Kota tek kat: devirle hazırlık içeriği (S) kiracının kotasına yazılır.
// Yerleşim içeriği kopyalasaydı tepe mevcut+2S, rsync --remove-source-files ile
// taşısaydı mevcut+S+en büyük dosya olurdu (gerçek kotada boyutu tek bir büyük
// dosyanın belirlediği site EDQUOT → kalem düşer, hesap geri alınır). Hedefte
// olmayan girdiler sabit bağla yerleşir: tepe TAM mevcut+S. Üzerine yazma
// kipinde hedefte zaten var olan dosya kopyalanır: pay en çok o dosya kadardır;
// içeriği güncellenir, izni korunur.
func TestDosyalariAktar_KotaTekKat(t *testing.T) {
	const (
		mevcut = 1 << 20
		buyuk  = 24 << 20 // tek büyük dosya (yedek zip/video)
		ortak  = 8 << 20  // hedefte zaten var olan dosyanın eski boyutu
	)
	parca := strings.Repeat("k", yrParcaBoyut)
	for _, sn := range []struct {
		ad     string
		kaynak func(t *testing.T, d string)
		S      int64
		dosya  int   // yerleşmesi beklenen .bin sayısı
		ortak  bool  // hedefte ortak.bin (eski, 0600) önceden var
		pay    int64 // kopyaya kalan dosyalardan en büyüğü
	}{
		{"parçalı", yrParcaliKaynak, yrParcaS, yrParcaSayi, false, 0},
		{"tek büyük dosya", func(t *testing.T, d string) {
			yrYaz(t, filepath.Join(d, "yedek", "site.bin"), strings.Repeat("b", buyuk), 0o644)
			for i := range 4 {
				yrYaz(t, filepath.Join(d, fmt.Sprintf("dosya%d.bin", i)), parca, 0o644)
			}
		}, buyuk + 4*yrParcaBoyut, 5, false, 0},
		{"üzerine yazma", func(t *testing.T, d string) {
			yrParcaliKaynak(t, d)
			yrYaz(t, filepath.Join(d, "ortak.bin"), strings.Repeat("n", ortak+1), 0o644)
		}, yrParcaS + ortak + 1, yrParcaSayi + 1, true, ortak + 1},
	} {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			ph := filepath.Join(o.ev, "public_html")
			o.dizin(t, ph)
			var once int64 = mevcut
			o.dosya(t, filepath.Join(o.ev, "mevcut.bin"), strings.Repeat("m", mevcut), 0o644)
			if sn.ortak {
				o.dosya(t, filepath.Join(ph, "ortak.bin"), strings.Repeat("e", ortak), 0o600)
				once += ortak
			}
			s := yrYeniUzak(t, yrYabanciUID)
			s.agaclar = map[string]func(*testing.T, string){yrUzakKok: sn.kaynak}

			dur := yrKotaSayaci(o.base, o.uid)
			err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
				Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, &AktarSonuc{}, yrLog(t))
			tepe, ornek := dur()
			if err != nil {
				t.Fatalf("dosyalariAktar: %v", err)
			}
			t.Logf("tepe kiracı kullanımı %d bayt (önce %d + S %d + pay %d), %d örnek", tepe, once, sn.S, sn.pay, ornek)
			if tepe < once+sn.S || ornek < 2 {
				t.Fatalf("sayaç yerleşimi görmedi: tepe=%d örnek=%d", tepe, ornek)
			}
			if sinir := once + sn.S + sn.pay; tepe > sinir {
				t.Errorf("kota tek kat değil: tepe %d > %d (fazla %d bayt)", tepe, sinir, tepe-sinir)
			}
			n := 0
			for p, g := range yrOzet(t, ph) {
				if strings.HasSuffix(p, ".bin") {
					n++
					if g.UID != uint32(o.uid) {
						t.Errorf("%s kiracıya ait değil", p)
					}
				}
			}
			if n != sn.dosya {
				t.Errorf("%d dosya yerleşti, beklenen %d", n, sn.dosya)
			}
			if sn.ortak {
				b, err := os.ReadFile(filepath.Join(ph, "ortak.bin"))
				if err != nil || len(b) != ortak+1 || b[0] != 'n' {
					t.Errorf("hedefte var olan dosya güncellenmedi: %d bayt (%v)", len(b), err)
				}
				if g := yrSahipAl(filepath.Join(ph, "ortak.bin")); g.mod != 0o600 {
					t.Errorf("hedefte var olan dosyanın izni korunmadı: %v", g.mod)
				}
			}
			yrHazirlikBos(t)
		})
	}
}

// yrSurec — /proc/<pid>/status'tan ad, durum harfi, üst pid ve gerçek uid.
func yrSurec(pid int) (ad, durum string, ppid, uid int) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return "", "", 0, -1
	}
	uid = -1
	for _, s := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(s, ":")
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		switch k {
		case "Name":
			ad = f[0]
		case "State":
			durum = f[0]
		case "PPid":
			ppid, _ = strconv.Atoi(f[0])
		case "Uid":
			uid, _ = strconv.Atoi(f[0])
		}
	}
	return ad, durum, ppid, uid
}

// yrTestinTorunu — pid bu test sürecinden türemiş mi (en çok 8 kuşak)?
func yrTestinTorunu(pid int) bool {
	for range 8 {
		_, _, ppid, _ := yrSurec(pid)
		if ppid == os.Getpid() {
			return true
		}
		if ppid <= 1 {
			return false
		}
		pid = ppid
	}
	return false
}

// yrYerlesimSurecleri — yerleşimin süreye bağlı kiracı adımları (dosya yerleşimi).
var yrYerlesimSurecleri = map[string]bool{"cp": true, "find": true, "rsync": true}

// yrKiraciYerlesimDondur — kiracının (uid) bu testten türeyen yerleşim
// süreçlerini (cp/find/rsync) SIGSTOP ile dondurur: kiracının kendi uid'indeki
// yerleşim sürecine yapabildiği. dur: taramayı bitirir, dondurulanları SIGKILL ile kapatır,
// testten türeyen ve hâlâ durmuş (T) süreçleri SIGCONT ile sürdürür (çocuğu
// durunca kendini de durduran kök runuser dahil) ve dondurulan sayısını döner:
// test asılı süreç bırakmaz.
func yrKiraciYerlesimDondur(t *testing.T, uid int) (dur func() int) {
	bitti := make(chan struct{})
	sonuc := make(chan []int, 1)
	go func() {
		donmus := map[int]bool{}
		for {
			select {
			case <-bitti:
				var l []int
				for p := range donmus {
					l = append(l, p)
				}
				sonuc <- l
				return
			default:
			}
			girdiler, _ := os.ReadDir("/proc")
			for _, g := range girdiler {
				pid, err := strconv.Atoi(g.Name())
				if err != nil || donmus[pid] {
					continue
				}
				if fi, err := os.Stat("/proc/" + g.Name()); err != nil || fi.Sys().(*syscall.Stat_t).Uid != uint32(uid) {
					continue
				}
				if ad, _, _, u := yrSurec(pid); yrYerlesimSurecleri[ad] && u == uid && yrTestinTorunu(pid) && unix.Kill(pid, unix.SIGSTOP) == nil {
					donmus[pid] = true
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	n, durdu := 0, false
	dur = func() int {
		if durdu {
			return n
		}
		durdu = true
		close(bitti)
		l := <-sonuc
		for _, pid := range l {
			if ad, _, _, u := yrSurec(pid); yrYerlesimSurecleri[ad] && u == uid {
				_ = unix.Kill(pid, unix.SIGKILL)
			}
		}
		girdiler, _ := os.ReadDir("/proc")
		for _, g := range girdiler {
			pid, err := strconv.Atoi(g.Name())
			if err != nil {
				continue
			}
			if ad, durum, _, u := yrSurec(pid); durum == "T" && yrTestinTorunu(pid) {
				t.Logf("durmuş süreç sürdürülüyor: %s pid=%d uid=%d", ad, pid, u)
				_ = unix.Kill(pid, unix.SIGCONT)
			}
		}
		n = len(l)
		return n
	}
	t.Cleanup(func() { dur() })
	return dur
}

// Kiracı kendi uid'indeki yerleşim sürecini SIGSTOP ile dondurur: adım kendi
// süresiyle kesilir (hesap bağlamının 3 saatini beklemez), kalem hata ile biter,
// hazırlık alanı temizlenir. Kaynak kalabalıktır (4000 girdi): sabit bağla
// yerleşim hızlıdır, dondurucu adımı ancak süreç yaşarken yakalayabilir.
func TestDosyalariAktar_KiraciDondurursaAdimKesilir(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	s := yrYeniUzak(t, yrYabanciUID)
	s.agaclar = map[string]func(*testing.T, string){yrUzakKok: func(t *testing.T, d string) {
		for i := range 4000 {
			yrYaz(t, filepath.Join(d, fmt.Sprintf("p%02d", i%40), fmt.Sprintf("f%04d.txt", i)), "x\n", 0o644)
		}
	}}
	eski := yerlesimSuresi
	yerlesimSuresi = func(int64) time.Duration { return 2 * time.Second }
	t.Cleanup(func() { yerlesimSuresi = eski })

	dur := yrKiraciYerlesimDondur(t, o.uid)
	bitti := make(chan error, 1)
	go func() {
		bitti <- (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
			Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, &AktarSonuc{}, yrLog(t))
	}()
	var err error
	takildi := false
	select {
	case err = <-bitti:
	case <-time.After(60 * time.Second):
		takildi = true
	}
	donmus := dur()
	if takildi {
		// dur dondurulanları öldürdü: kalem şimdi biter (test asılı kalmaz).
		<-bitti
		t.Fatal("donmuş kiracı süreci yerleşimi süresiz bekletti (60 sn)")
	}
	if donmus == 0 {
		t.Fatalf("fikstür: kiracının yerleşim süreci dondurulamadı (hata=%v)", err)
	}
	if err == nil || !strings.Contains(err.Error(), errYerlesimSuresi.Error()) {
		t.Errorf("donmuş yerleşim süre hatasıyla bitmedi: %v", err)
	}
	yrHazirlikBos(t)
}

// Kaynakta sahibinin okuyamadığı/değiştiremediği modlar (0000 dosya, u+x'siz
// dizin, salt okunur dizin): kök rsync bunları hazırlığa aynen koyar; devirden
// önce sahibe açılmazsa kiracı rsync'i okuyamaz/taşıyamaz (23) ve kalem düşer.
// İçerik kiracıya ait ve kiracı tarafından okunur yerleşir.
func TestDosyalariAktar_OkunamayanModlarTasinir(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	s := yrYeniUzak(t, yrYabanciUID)
	s.agaclar = map[string]func(*testing.T, string){yrUzakKok: func(t *testing.T, d string) {
		yrYaz(t, filepath.Join(d, "kilitli.txt"), "kilitli\n", 0)
		yrYaz(t, filepath.Join(d, "kapali", "ic.txt"), "kapali\n", 0o644)
		yrYaz(t, filepath.Join(d, "salt", "s.txt"), "salt\n", 0o644)
		for ad, mod := range map[string]os.FileMode{"kapali": 0o600, "salt": 0o555} {
			if err := os.Chmod(filepath.Join(d, ad), mod); err != nil {
				t.Fatal(err)
			}
		}
	}}

	err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
		Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, &AktarSonuc{}, yrLog(t))
	if err != nil {
		t.Fatalf("dosyalariAktar: %v", err)
	}
	for rel, icerik := range map[string]string{"kilitli.txt": "kilitli\n", "kapali/ic.txt": "kapali\n", "salt/s.txt": "salt\n"} {
		p := filepath.Join(ph, rel)
		out, err := exec.Command("runuser", "-u", "nobody", "--", "cat", "--", p).Output()
		if err != nil || string(out) != icerik {
			t.Errorf("%s kiracı tarafından okunamıyor: %q (%v)", rel, out, err)
		}
		if g := yrSahipAl(p); g.uid != uint32(o.uid) {
			t.Errorf("%s kiracıya ait değil: %+v", rel, g)
		}
	}
	yrHazirlikBos(t)
}

// Sabit bağla yerleşen girdi kaynağın inode'unu (iznini) taşır: kaynaktan gelen
// herkese yazılabilir ve özel bitli (setuid/setgid/sticky) izinler kiracı
// ağacına AYNEN geçmemeli — rsync --no-perms'in 022 umask'la yeni dosyada
// ürettiği biçime süzülür (grup/diğer yazma ve özel bitler atılır).
func TestDosyalariAktar_IzinlerSuzulur(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	s := yrYeniUzak(t, yrYabanciUID)
	s.agaclar = map[string]func(*testing.T, string){yrUzakKok: func(t *testing.T, d string) {
		yrYaz(t, filepath.Join(d, "suid"), "#!/bin/sh\n", 0o4777)
		yrYaz(t, filepath.Join(d, "herkes.txt"), "herkes\n", 0o666)
		yrYaz(t, filepath.Join(d, "ortak", "ic.txt"), "ic\n", 0o2775)
		if err := os.Chmod(filepath.Join(d, "ortak"), 0o3777); err != nil {
			t.Fatal(err)
		}
	}}

	err := (&Handlers{}).dosyalariAktar(context.Background(), yrKaynak(s), Hesap{WebRoot: yrUzakKok},
		Ayarlar{Dosyalar: true}, yrSK, ph, yrAlan, &AktarSonuc{}, yrLog(t))
	if err != nil {
		t.Fatalf("dosyalariAktar: %v", err)
	}
	for rel, bekle := range map[string]fs.FileMode{"suid": 0o755, "herkes.txt": 0o644, "ortak/ic.txt": 0o755, "ortak": fs.ModeDir | 0o755} {
		if g := yrSahipAl(filepath.Join(ph, rel)); g.mod != bekle || g.uid != uint32(o.uid) {
			t.Errorf("%s: izin %v uid %d, beklenen %v kiracı", rel, g.mod, g.uid, bekle)
		}
	}
	yrHazirlikBos(t)
}
