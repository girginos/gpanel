package sitekopya

// Regresyon: site kopyalama ucunda kiracı-kontrollü symlink ile kök
// ayrıcalığının kötüye kullanımı (CWE-59). Tüm senaryolar t.TempDir içinde,
// kiracı yerine ayrıcalıksız "nobody" kullanıcısıyla kurulur; canlı ev
// dizinlerine dokunulmaz. runuser root gerektirdiğinden root değilse atlanır.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	testSK      = "c_test"
	gizliIcerik = "GIZLI-KOK-ICERIGI-7f3a9c"
)

var testZaman = time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)

type ortam struct {
	base, home string
	uid, gid   int
}

func yeniOrtam(t *testing.T) ortam {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("runuser ile kiracı kimliği için root gerekli")
	}
	for _, b := range []string{"runuser", "rsync", "du", "mkdir", "test"} {
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
	eskiKok, eskiKul := evKoku, osKullanici
	evKoku = base
	osKullanici = func(string) string { return "nobody" }
	t.Cleanup(func() { evKoku, osKullanici = eskiKok, eskiKul })

	home := filepath.Join(base, testSK)
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(home, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("runuser", "-u", "nobody", "--", "test", "-w", home).Run(); err != nil {
		t.Fatalf("nobody geçici ev dizinine erişemiyor (TMPDIR izinleri?): %v", err)
	}
	return ortam{base: base, home: home, uid: uid, gid: gid}
}

// kiraciDosya: kiracıya (nobody) ait dosya yazar.
func (o ortam) kiraciDosya(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(yol, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

// kiraciDizin: kiracıya (nobody) ait dizin açar.
func (o ortam) kiraciDizin(t *testing.T, yol string) {
	t.Helper()
	if err := os.Mkdir(yol, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(yol, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

// kiraciBag: kiracıya (nobody) ait symlink kurar (müşterinin FTP/SSH/cron ile
// yapabildiği şey).
func (o ortam) kiraciBag(t *testing.T, hedef, bag string) {
	t.Helper()
	if err := os.Symlink(hedef, bag); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(bag, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
}

// kokDizin: kiracının erişemediği kök sahipli dizin (komşu kiracının evi,
// /root gibi) ve içinde gizli dosya.
func kokDizin(t *testing.T, yol string, mod os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(yol, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(yol, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yol, "gizli.txt"), []byte(gizliIcerik), 0o600); err != nil {
		t.Fatal(err)
	}
}

// gizliSizdiMi: kök altında (bağ izlemeden) gizli içerikli düzenli dosya var mı.
func gizliSizdiMi(t *testing.T, kok string) string {
	t.Helper()
	var bulunan string
	_ = filepath.WalkDir(kok, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), gizliIcerik) {
			bulunan = p
		}
		return nil
	})
	return bulunan
}

func sahipUID(t *testing.T, yol string) int {
	t.Helper()
	fi, err := os.Lstat(yol)
	if err != nil {
		t.Fatal(err)
	}
	return int(fi.Sys().(*syscall.Stat_t).Uid)
}

func girdiler(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ad []string
	for _, e := range es {
		ad = append(ad, e.Name())
	}
	return ad
}

// VEKTÖR A: public_html → kiracının erişemediği kök dizine symlink. Eskiden
// kök rsync kök bağı çözüp içeriği gerçek dosya olarak kopyalıyor, chown -R
// müşteriye devrediyordu.
func TestOlustur_PublicHtmlSymlinkKokIcerigiSizdirmaz(t *testing.T) {
	o := yeniOrtam(t)
	kurban := filepath.Join(o.base, "kurban")
	kokDizin(t, kurban, 0o700)
	o.kiraciBag(t, kurban, filepath.Join(o.home, "public_html"))

	ad, err := kopyaOlustur(context.Background(), testSK, testZaman)
	if err == nil {
		t.Errorf("erişilemez hedefe bağlı public_html kopyalanmamalıydı (ad=%s)", ad)
	}
	if p := gizliSizdiMi(t, o.home); p != "" {
		t.Fatalf("SIZINTI: kök-okunur dosya içeriği kiracı evine kopyalandı: %s", p)
	}
	if got := girdiler(t, kurban); len(got) != 1 || got[0] != "gizli.txt" {
		t.Fatalf("kurban dizini değişti: %v", got)
	}
	if uid := sahipUID(t, filepath.Join(kurban, "gizli.txt")); uid != 0 {
		t.Fatalf("kurban dosyasının sahipliği değişti: uid=%d", uid)
	}
}

// VEKTÖR B: ~/kopyalar → komşu (kök sahipli) dizine symlink. Eskiden
// `chown sk:sk kopyalar` (-h yok) bağın HEDEFİNİN sahipliğini müşteriye
// devrediyor ve kopya komşu dizine yazılıyordu.
func TestOlustur_KopyalarSymlinkKomsuyaYazmazSahiplikDegismez(t *testing.T) {
	o := yeniOrtam(t)
	ph := filepath.Join(o.home, "public_html")
	o.kiraciDizin(t, ph)
	o.kiraciDosya(t, filepath.Join(ph, "index.html"), "merhaba")
	komsu := filepath.Join(o.base, "komsu")
	kokDizin(t, komsu, 0o755)
	o.kiraciBag(t, komsu, filepath.Join(o.home, "kopyalar"))

	if ad, err := kopyaOlustur(context.Background(), testSK, testZaman); err == nil {
		t.Errorf("komşu dizine kopya yazılmamalıydı (ad=%s)", ad)
	}
	if uid := sahipUID(t, komsu); uid != 0 {
		t.Fatalf("KOMŞU DİZİNİN SAHİPLİĞİ DEĞİŞTİ: uid=%d", uid)
	}
	if got := girdiler(t, komsu); len(got) != 1 || got[0] != "gizli.txt" {
		t.Fatalf("komşu dizine yazıldı: %v", got)
	}
}

// Liste: ~/kopyalar → komşunun kopyalar dizinine symlink; kök olarak
// ReadDir+du komşunun kopya adlarını/boyutlarını sızdırıyordu.
func TestListe_KopyalarSymlinkKomsuKopyalariniGostermez(t *testing.T) {
	o := yeniOrtam(t)
	komsu := filepath.Join(o.base, "komsu_kopyalar")
	kokDizin(t, filepath.Join(komsu, "kopya_20260101_000000"), 0o755)
	o.kiraciBag(t, komsu, filepath.Join(o.home, "kopyalar"))

	if l := kopyaListe(context.Background(), testSK); len(l) != 0 {
		t.Fatalf("komşunun kopyaları listelendi: %+v", l)
	}
}

// Sil: ~/kopyalar → komşunun kopyalar dizinine symlink; kök os.RemoveAll ara
// bileşeni izleyip komşunun kopyasını siliyordu.
func TestSil_KopyalarSymlinkKomsuKopyasiniSilmez(t *testing.T) {
	o := yeniOrtam(t)
	komsu := filepath.Join(o.base, "komsu_kopyalar")
	hedef := filepath.Join(komsu, "kopya_20260101_000000")
	kokDizin(t, hedef, 0o755)
	o.kiraciBag(t, komsu, filepath.Join(o.home, "kopyalar"))

	if err := kopyaKaldir(testSK, "kopya_20260101_000000"); err == nil {
		t.Error("komşu kopya için silme başarılı dönmemeliydi")
	}
	if _, err := os.Stat(filepath.Join(hedef, "gizli.txt")); err != nil {
		t.Fatalf("KOMŞUNUN KOPYASI SİLİNDİ: %v", err)
	}
}

// Sil: son bileşen (kopya_...) bir symlink ise kopya sayılmaz; bağ ve hedefi
// yerinde kalır.
func TestSil_KopyaAdiSymlinkIseReddedilir(t *testing.T) {
	o := yeniOrtam(t)
	o.kiraciDizin(t, filepath.Join(o.home, "kopyalar"))
	ph := filepath.Join(o.home, "public_html")
	o.kiraciDizin(t, ph)
	o.kiraciDosya(t, filepath.Join(ph, "index.html"), "merhaba")
	bag := filepath.Join(o.home, "kopyalar", "kopya_20260101_000000")
	o.kiraciBag(t, ph, bag)

	err := kopyaKaldir(testSK, "kopya_20260101_000000")
	var ih *islemHatasi
	if !errors.As(err, &ih) || ih.kod != http.StatusNotFound {
		t.Fatalf("symlink kopya adı 404 ile reddedilmeliydi, gelen: %v", err)
	}
	if _, err := os.Lstat(bag); err != nil {
		t.Fatalf("bağ silindi: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ph, "index.html")); err != nil {
		t.Fatalf("bağın hedefi silindi: %v", err)
	}
}

// Girdi doğrulama: sk ve kopya adı yol bileşenine/argv'ye girer.
func TestGirdiDogrulama(t *testing.T) {
	o := yeniOrtam(t)
	// sk="" doğrulanmazsa evDizini("") = evKoku+"/" → kök düzeyindeki kopyalar/
	// hedef alınırdı: tuzak kopya (silinmemeli/listelenmemeli).
	tuzak := filepath.Join(o.base, "kopyalar", "kopya_20260101_000000")
	kokDizin(t, tuzak, 0o755)
	for _, sk := range []string{"", "c_", "root", "c_../kurban", "c_a/b", "c_A", "c_-rf", strings.Repeat("c_x", 20)} {
		_, err := kopyaOlustur(context.Background(), sk, testZaman)
		var ih *islemHatasi
		if !errors.As(err, &ih) || ih.kod != http.StatusBadRequest || ih.mesaj != "geçersiz kullanıcı" {
			t.Errorf("kopyaOlustur(%q) geçersiz kullanıcı ile reddedilmeliydi: %v", sk, err)
		}
		if err := kopyaKaldir(sk, "kopya_20260101_000000"); !errors.As(err, &ih) || ih.kod != http.StatusBadRequest {
			t.Errorf("kopyaKaldir(%q) 400 ile reddedilmeliydi: %v", sk, err)
		}
		if l := kopyaListe(context.Background(), sk); len(l) != 0 {
			t.Errorf("kopyaListe(%q) boş dönmeliydi: %v", sk, l)
		}
	}
	for _, ad := range []string{"", "..", "../public_html", "kopya_20260101_000000/..", "kopya_1_2", "kopyalar"} {
		err := kopyaKaldir(testSK, ad)
		var ih *islemHatasi
		if !errors.As(err, &ih) || ih.kod != http.StatusBadRequest {
			t.Errorf("kopyaKaldir(ad=%q) 400 ile reddedilmeliydi: %v", ad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tuzak, "gizli.txt")); err != nil {
		t.Fatalf("doğrulanmamış girdiyle kiracı dışı kopya silindi: %v", err)
	}
}

// Meşru kullanım bozulmamalı: kopya oluşur, kiracıya aittir, ağaç içi bağ
// bağ olarak taşınır, listelenir ve silinir.
func TestMesruKopyaOlusturListeSil(t *testing.T) {
	o := yeniOrtam(t)
	ph := filepath.Join(o.home, "public_html")
	o.kiraciDizin(t, ph)
	o.kiraciDosya(t, filepath.Join(ph, "index.php"), "<?php echo 1;")
	o.kiraciDizin(t, filepath.Join(ph, "alt"))
	o.kiraciDosya(t, filepath.Join(ph, "alt", "a.txt"), "alt-icerik")
	o.kiraciBag(t, "alt/a.txt", filepath.Join(ph, "baglanti"))

	ad, err := kopyaOlustur(context.Background(), testSK, testZaman)
	if err != nil {
		t.Fatalf("meşru kopya başarısız: %v", err)
	}
	if ad != "kopya_20260929_120000" {
		t.Fatalf("beklenmeyen kopya adı: %s", ad)
	}
	kp := filepath.Join(o.home, "kopyalar", ad, "public_html")
	if b, err := os.ReadFile(filepath.Join(kp, "index.php")); err != nil || string(b) != "<?php echo 1;" {
		t.Fatalf("index.php kopyalanmadı: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(kp, "alt", "a.txt")); err != nil || string(b) != "alt-icerik" {
		t.Fatalf("alt/a.txt kopyalanmadı: %v %q", err, b)
	}
	if hedef, err := os.Readlink(filepath.Join(kp, "baglanti")); err != nil || hedef != "alt/a.txt" {
		t.Fatalf("ağaç içi bağ bağ olarak taşınmadı: %v %q", err, hedef)
	}
	for _, p := range []string{filepath.Join(o.home, "kopyalar"), filepath.Join(o.home, "kopyalar", ad), kp, filepath.Join(kp, "index.php")} {
		if uid := sahipUID(t, p); uid != o.uid {
			t.Fatalf("%s kiracıya ait değil: uid=%d", p, uid)
		}
	}
	if fi, err := os.Stat(filepath.Join(o.home, "kopyalar")); err != nil || fi.Mode().Perm() != 0o711 {
		t.Fatalf("kopyalar izni 0711 olmalı: %v %v", err, fi.Mode())
	}

	l := kopyaListe(context.Background(), testSK)
	if len(l) != 1 || l[0].Ad != ad || l[0].Tarih == "" {
		t.Fatalf("liste beklenmedik: %+v", l)
	}

	if err := kopyaKaldir(testSK, ad); err != nil {
		t.Fatalf("meşru silme başarısız: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(o.home, "kopyalar", ad)); !os.IsNotExist(err) {
		t.Fatalf("kopya silinmedi: %v", err)
	}
	if err := kopyaKaldir(testSK, ad); err == nil {
		t.Fatal("olmayan kopya için 404 beklenirdi")
	}
	if _, err := os.Stat(filepath.Join(ph, "index.php")); err != nil {
		t.Fatalf("kaynak etkilendi: %v", err)
	}
}

// Handler kapsamı: kimliksiz (ya da kapsam dışı) istek DB'ye ulaşmadan
// "bulunamadı" almalı — rota ara katmanı unutulsa bile.
func TestHandler_KapsamDisiIstekDBYeUlasmaz(t *testing.T) {
	h := &Handlers{DB: nil} // DB'ye dokunulursa nil işaretçi paniği
	durumlar := []struct {
		ad string
		m  string
		fn http.HandlerFunc
	}{
		{"Liste", http.MethodGet, h.Liste},
		{"Olustur", http.MethodPost, h.Olustur},
		{"Sil", http.MethodDelete, h.Sil},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			req := httptest.NewRequest(d.m, "/domains/7/kopya", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", "7")
			rctx.URLParams.Add("ad", "kopya_20260101_000000")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("kapsam denetimi yok: kimliksiz istek DB'ye ulaştı (%v)", r)
					}
				}()
				d.fn(rec, req)
			}()
			if rec.Code != http.StatusNotFound {
				t.Fatalf("kimliksiz istek 404 almalıydı, gelen %d", rec.Code)
			}
		})
	}
}
