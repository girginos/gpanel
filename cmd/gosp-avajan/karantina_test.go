package main

// Oto-karantina (karantinaAl) symlink/hardlink/TOCTOU regresyon testleri.
//
// Testler CANLI SİSTEME DOKUNMAZ: ev kökü t.TempDir altında sahte bir "/home",
// sk→uid çözücü sahte (gerçek kullanıcı araması yok); mysql/systemctl/exec yok.
// Ajan üretimde ROOT çalışır; saldırgan kiracı kendi evindeki (public_html ara
// dizinleri, .karantina, dosyanın kendisi) her adı tarama ile işlem arasında
// değiştirebilir. "kurban" dizini evin DIŞINDA başka bir kiracıyı/sistemi temsil eder.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testAn = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// Test içeriği bilerek zararsız: gerçek imza değil, yalnız bayt sayısı ölçülür.
const zararliIcerik = "tespit-edilmis-ornek-icerik"

type karantinaOrtam struct {
	kok    string // sahte /home
	home   string // <kok>/c_test (saldırgan kiracının evi)
	pub    string // <home>/public_html
	kurban string // ev DIŞI dizin (başka kiracı / sistem)
}

func yeniKarantinaOrtam(t *testing.T) karantinaOrtam {
	t.Helper()
	d := t.TempDir()
	o := karantinaOrtam{kok: filepath.Join(d, "home"), kurban: filepath.Join(d, "kurban")}
	o.home = filepath.Join(o.kok, "c_test")
	o.pub = filepath.Join(o.home, "public_html")
	for _, p := range []string{o.pub, o.kurban} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

// buUID — dosyaları bu test süreci oluşturur; kiracı uid'i = sürecin uid'i.
func buUID(string) (int, int, bool) { return os.Getuid(), os.Getgid(), true }

func dosyaOlustur(t *testing.T, yol, icerik string, mod os.FileMode) {
	t.Helper()
	if err := os.WriteFile(yol, []byte(icerik), mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(yol, mod); err != nil { // umask'tan bağımsız
		t.Fatal(err)
	}
}

// kurbanSaglam — ev dışındaki dosya yerinde, izni ve boyutu değişmemiş olmalı.
func kurbanSaglam(t *testing.T, yol string, mod os.FileMode, boyut int64) {
	t.Helper()
	fi, err := os.Lstat(yol)
	if err != nil {
		t.Fatalf("kurban dosya yerinden oynatıldı (%s): %v", yol, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != mod || fi.Size() != boyut {
		t.Fatalf("kurban dosya değişti (%s): mod=%v boyut=%d, beklenen %v/%d", yol, fi.Mode(), fi.Size(), mod, boyut)
	}
}

// guvenlikReddi — ret, dosya-bulunamadı gibi rastlantısal bir hatadan değil
// fd-sabitli yardımcının güvenlik denetiminden gelmeli.
func guvenlikReddi(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("karantina REDDEDİLMELİYDİ (symlink/yabancı inode), err=nil")
	}
	if !strings.Contains(err.Error(), "guvenli-olmayan") {
		t.Fatalf("ret güvenlik denetiminden gelmeli, gelen: %v", err)
	}
}

// karantinaYok — reddedilen işlem .karantina'yı oluşturmamalı (ya da boş bırakmalı).
func karantinaYok(t *testing.T, home string) {
	t.Helper()
	q := filepath.Join(home, ".karantina")
	fi, err := os.Lstat(q)
	if os.IsNotExist(err) {
		return
	}
	if err != nil || !fi.IsDir() {
		t.Fatalf(".karantina beklenmedik durumda: %v %v", fi, err)
	}
	if g, _ := os.ReadDir(q); len(g) != 0 {
		t.Fatalf(".karantina boş olmalı, %d girdi var", len(g))
	}
}

// Meşru yol: kiracıya ait düzenli dosya .karantina'ya taşınır, izni 0000 olur.
func TestKarantinaAlMesru(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	src := filepath.Join(o.pub, "shell.php")
	dosyaOlustur(t, src, zararliIcerik, 0o644)

	hedef, err := karantinaAlEv(o.kok, src, buUID, testAn)
	if err != nil {
		t.Fatalf("meşru karantina başarısız: %v", err)
	}
	beklenen := filepath.Join(o.home, ".karantina", "20260929_120000_shell.php")
	if hedef != beklenen {
		t.Fatalf("hedef=%q, beklenen %q", hedef, beklenen)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("kaynak yerinde kalmamalı: %v", err)
	}
	fi, err := os.Lstat(hedef)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0 || fi.Size() != int64(len(zararliIcerik)) {
		t.Fatalf("karantina dosyası düzenli ve 0000 olmalı: fi=%v err=%v", fi, err)
	}
	qi, err := os.Lstat(filepath.Join(o.home, ".karantina"))
	if err != nil || !qi.IsDir() || qi.Mode().Perm() != 0o700 {
		t.Fatalf(".karantina 0700 dizin olmalı: %v %v", qi, err)
	}
}

// TOCTOU — ara dizin: tarama public_html/index.php'yi düzenli dosya olarak gördü;
// işlemden önce kiracı public_html'i başka bir kiracının docroot'una symlink yaptı.
// Eski os.Rename ara symlink'i izleyip KURBANIN dosyasını taşıyordu.
func TestKarantinaAlAraDizinSymlink(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	yol := filepath.Join(o.pub, "index.php")
	dosyaOlustur(t, yol, zararliIcerik, 0o644)
	if fi, err := os.Lstat(yol); err != nil || !fi.Mode().IsRegular() { // "tarama" anı
		t.Fatalf("tarama öncesi durum: %v %v", fi, err)
	}

	kurbanDoc := filepath.Join(o.kurban, "c_kurban", "public_html")
	if err := os.MkdirAll(kurbanDoc, 0o755); err != nil {
		t.Fatal(err)
	}
	kurbanDosya := filepath.Join(kurbanDoc, "index.php")
	dosyaOlustur(t, kurbanDosya, "kurbanin-sitesi", 0o644)
	// Yarış: public_html → kurbanın docroot'u.
	if err := os.Rename(o.pub, o.pub+".eski"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kurbanDoc, o.pub); err != nil {
		t.Fatal(err)
	}

	_, err := karantinaAlEv(o.kok, yol, buUID, testAn)
	guvenlikReddi(t, err)
	kurbanSaglam(t, kurbanDosya, 0o644, int64(len("kurbanin-sitesi")))
	karantinaYok(t, o.home)
}

// .karantina symlink: kiracı .karantina'yı ev dışına (başka kiracının dizinine)
// yönlendirdi. Eski os.MkdirAll/os.Rename symlink'i izleyip root olarak ev DIŞINA
// yazıyordu.
func TestKarantinaAlKarantinaDiziniSymlink(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	src := filepath.Join(o.pub, "shell.php")
	dosyaOlustur(t, src, zararliIcerik, 0o644)
	if err := os.Symlink(o.kurban, filepath.Join(o.home, ".karantina")); err != nil {
		t.Fatal(err)
	}

	_, err := karantinaAlEv(o.kok, src, buUID, testAn)
	guvenlikReddi(t, err)
	g, err := os.ReadDir(o.kurban)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 0 {
		t.Fatalf("ev dışındaki dizine yazıldı: %d girdi", len(g))
	}
	if fi, err := os.Lstat(src); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("reddedilen kaynak yerinde ve değişmeden kalmalı: %v %v", fi, err)
	}
}

// Son bileşen symlink: public_html/x.php → ev dışındaki gizli dosya. Eski kod
// symlink'i taşıyıp os.Chmod(hedef, 0) ile KURBANI 0000 yapıyordu.
func TestKarantinaAlSonBilesenSymlink(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	kurbanDosya := filepath.Join(o.kurban, "sir.conf")
	dosyaOlustur(t, kurbanDosya, "ornek-gizli-ayar", 0o640)
	link := filepath.Join(o.pub, "x.php")
	if err := os.Symlink(kurbanDosya, link); err != nil {
		t.Fatal(err)
	}

	_, err := karantinaAlEv(o.kok, link, buUID, testAn)
	guvenlikReddi(t, err)
	kurbanSaglam(t, kurbanDosya, 0o640, int64(len("ornek-gizli-ayar")))
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("reddedilen symlink yerinde kalmalı: %v %v", fi, err)
	}
	karantinaYok(t, o.home)
}

// Hardlink: kiracının evindeki ad, BAŞKA bir kullanıcıya ait inode'a hard link.
// Eski kod adı taşıyıp os.Chmod ile yabancı inode'u 0000 yapıyordu. Yeni kod
// yalnız kiracıya (uid) ait inode'u kabul eder. chown gerektirdiği için root.
func TestKarantinaAlYabanciHardlink(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("yabancı sahipli inode üretmek için root gerekir")
	}
	o := yeniKarantinaOrtam(t)
	kurbanDosya := filepath.Join(o.kurban, "baska-kiraci.php")
	dosyaOlustur(t, kurbanDosya, "baska-kiracinin-kodu", 0o640)
	if err := os.Chown(kurbanDosya, 54321, 54321); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(o.pub, "link.php")
	if err := os.Link(kurbanDosya, link); err != nil {
		t.Fatal(err)
	}

	_, err := karantinaAlEv(o.kok, link, buUID, testAn) // kiracı uid = 0 ≠ 54321
	guvenlikReddi(t, err)
	kurbanSaglam(t, kurbanDosya, 0o640, int64(len("baska-kiracinin-kodu")))
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("reddedilen hard link yerinde kalmalı: %v", err)
	}
	karantinaYok(t, o.home)
}

// Yabancı sahip (root gerektirmeyen biçim): dosya kiracının uid'ine ait değil.
func TestKarantinaAlYabanciSahip(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	src := filepath.Join(o.pub, "yabanci.php")
	dosyaOlustur(t, src, zararliIcerik, 0o644)
	baskaUID := func(string) (int, int, bool) { return os.Getuid() + 1, os.Getgid(), true }

	_, err := karantinaAlEv(o.kok, src, baskaUID, testAn)
	guvenlikReddi(t, err)
	if fi, err := os.Lstat(src); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("yabancı dosya yerinde ve değişmeden kalmalı: %v %v", fi, err)
	}
	karantinaYok(t, o.home)
}

// Fail-closed: kiracının uid'i çözümlenemezse hiçbir şey taşınmaz. Sahte çözücü
// geçerli bir uid'i ok=false ile döner: ok denetimi atlanırsa taşıma GERÇEKLEŞİR.
func TestKarantinaAlUIDCozulemez(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	src := filepath.Join(o.pub, "shell.php")
	dosyaOlustur(t, src, zararliIcerik, 0o644)
	cozulemez := func(string) (int, int, bool) { return os.Getuid(), os.Getgid(), false }

	if _, err := karantinaAlEv(o.kok, src, cozulemez, testAn); err == nil || !strings.Contains(err.Error(), "çözümlenemedi") {
		t.Fatalf("uid çözümlenemezse fail-closed hata beklenirdi, gelen: %v", err)
	}
	if fi, err := os.Lstat(src); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("dosya yerinde ve değişmeden kalmalı: %v %v", fi, err)
	}
	if _, err := os.Lstat(filepath.Join(o.home, ".karantina")); !os.IsNotExist(err) {
		t.Fatalf(".karantina oluşturulmamalı: %v", err)
	}
}

// Ev kökü dışı ya da c_* olmayan yol: kiracı yok → taşıma yok.
func TestKarantinaAlEvDisi(t *testing.T) {
	o := yeniKarantinaOrtam(t)
	disari := filepath.Join(o.kurban, "x.php")
	dosyaOlustur(t, disari, zararliIcerik, 0o644)
	sorgulandi := false
	izleyen := func(string) (int, int, bool) { sorgulandi = true; return os.Getuid(), os.Getgid(), true }
	if _, err := karantinaAlEv(o.kok, disari, izleyen, testAn); err == nil || !strings.Contains(err.Error(), "kiracı bulunamadı") {
		t.Fatalf("ev kökü dışındaki yol 'kiracı bulunamadı' ile reddedilmeliydi, gelen: %v", err)
	}
	if sorgulandi {
		t.Fatal("kiracı olmayan yol için uid sorgulanmamalı (işleme hiç girilmemeli)")
	}
	kurbanSaglam(t, disari, 0o644, int64(len(zararliIcerik)))
}

// Üretim bağlantısı: var olmayan bir kiracı için karantinaAl, sistem kullanıcı
// araması (salt okuma) başarısız olunca fail-closed döner; dosya sistemine
// yazma yolu açılmaz.
func TestKarantinaAlBilinmeyenKiraciFailClosed(t *testing.T) {
	_, err := karantinaAl("/home/c_gospavtest_yok_7f3a9/public_html/x.php")
	if err == nil || !strings.Contains(err.Error(), "çözümlenemedi") {
		t.Fatalf("bilinmeyen kiracı fail-closed olmalı, gelen: %v", err)
	}
}

func TestKiraciBul(t *testing.T) {
	cases := []struct {
		kok, yol, sk, home string
	}{
		{"/home", "/home/c_a/public_html/x.php", "c_a", "/home/c_a"},
		{"/home", "/home/c_a/.karantina/x", "c_a", "/home/c_a"},
		{"/home", "/home/c_a", "", ""},                      // evin kendisi dosya değil
		{"/home", "/home/c_a/", "", ""},                     // Clean → /home/c_a
		{"/home", "/home/kullanici/x.php", "", ""},          // c_* değil
		{"/home", "/homex/c_a/x.php", "", ""},               // önek sınırı
		{"/home", "/etc/passwd", "", ""},                    // ev dışı
		{"/home", "home/c_a/x.php", "", ""},                 // göreli
		{"/home", "c_a/x.php", "", ""},                      // göreli, c_* ile başlayan
		{"/home", "/home/c_a/../c_b/x", "c_b", "/home/c_b"}, // sözcüksel .. çözülür
		{"/tmp/t/home", "/tmp/t/home/c_z/p/q.php", "c_z", "/tmp/t/home/c_z"},
	}
	for _, c := range cases {
		sk, home := kiraciBul(c.kok, c.yol)
		if sk != c.sk || home != c.home {
			t.Errorf("kiraciBul(%q,%q)=(%q,%q), beklenen (%q,%q)", c.kok, c.yol, sk, home, c.sk, c.home)
		}
	}
}
