package antivirus

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// yeniEv — t.TempDir içinde /home/<sk> benzeri kiracı evi (.karantina + public_html).
func yeniEv(t *testing.T) (base, home string) {
	t.Helper()
	base = t.TempDir()
	home = filepath.Join(base, "home", "c_x")
	for _, d := range []string{filepath.Join(home, ".karantina"), filepath.Join(home, "public_html")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base, home
}

// kurbanYaz — ev DIŞINDA izni bilinen bir "kurban" dosya (başka kiracı/sistem).
func kurbanYaz(t *testing.T, base string, mod os.FileMode) string {
	t.Helper()
	k := filepath.Join(base, "etc", "shadow")
	if err := os.MkdirAll(filepath.Dir(k), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, []byte("gizli"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(k, mod); err != nil {
		t.Fatal(err)
	}
	return k
}

func izin(t *testing.T, yol string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(yol)
	if err != nil {
		t.Fatalf("lstat %s: %v", yol, err)
	}
	return fi.Mode().Perm()
}

func sahip(t *testing.T, yol string) (int, int) {
	t.Helper()
	fi, err := os.Lstat(yol)
	if err != nil {
		t.Fatalf("lstat %s: %v", yol, err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return int(st.Uid), int(st.Gid)
}

// yarisKur — rename ile izin işlemi arasındaki yarış kancasını test süresince kurar.
func yarisKur(t *testing.T, f func(asama string)) {
	t.Helper()
	yarisKancasi = f
	t.Cleanup(func() { yarisKancasi = nil })
}

// zamanla — FIFO gibi bloklayabilecek bir çağrıyı süre sınırıyla koşar; süre
// dolarsa FIFO'yu yazma ucundan açıp bloklanan okuyucuyu serbest bırakır.
func zamanla(t *testing.T, fifo string, f func() error) error {
	t.Helper()
	sonuc := make(chan error, 1)
	go func() { sonuc <- f() }()
	select {
	case err := <-sonuc:
		return err
	case <-time.After(5 * time.Second):
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		t.Fatalf("KRİTİK: FIFO açılışı bloklandı (O_NONBLOCK yok) — root iş parçacığı asılı kalır")
		return nil
	}
}

// ---------------------------------------------------------------- guvenliGeriTasi

// Meşru geri yükleme: gerçek dizinler → dosya orijinal yerine taşınmalı, 0644.
func TestGuvenliGeriTasi_MesruGeriYukleme(t *testing.T) {
	_, home := yeniEv(t)
	if err := os.MkdirAll(filepath.Join(home, "public_html", "stg"), 0o755); err != nil {
		t.Fatal(err)
	}
	kar := filepath.Join(home, ".karantina", "q_evil.php")
	mod := os.FileMode(0o000) // karantinadaki gerçek durum (panel root çalışır)
	if os.Geteuid() != 0 {
		mod = 0o600 // root olmayan koşucu 0000 dosyayı açamaz
	}
	if err := os.WriteFile(kar, []byte("<?php evil"), mod); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "stg", "evil.php")

	if err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("meşru geri yükleme başarısız: %v", err)
	}
	if _, err := os.Lstat(orij); err != nil {
		t.Fatalf("dosya orijinal yerde değil: %v", err)
	}
	if m := izin(t, orij); m != 0o644 {
		t.Errorf("geri yüklenen dosyanın izni %v, beklenen 0644", m)
	}
	if _, err := os.Lstat(kar); !os.IsNotExist(err) {
		t.Errorf("karantina kaynağı hâlâ var (taşınmadı)")
	}
}

// Çakışma: hedef zaten varsa ErrExist dönmeli, üzerine yazmamalı.
func TestGuvenliGeriTasi_CakismaReddedilir(t *testing.T) {
	_, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("yeni"), 0o644)
	orij := filepath.Join(home, "public_html", "evil.php")
	os.WriteFile(orij, []byte("mevcut"), 0o644) // hedef zaten var

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if !os.IsExist(err) {
		t.Fatalf("çakışma reddedilmedi, hata=%v (beklenen ErrExist)", err)
	}
	b, _ := os.ReadFile(orij)
	if string(b) != "mevcut" {
		t.Errorf("mevcut dosya EZİLDİ: %q", string(b))
	}
}

// SYMLINK SALDIRISI: ara dizin ev dışına symlink → reddedilmeli, ev dışına yazılmamalı.
func TestGuvenliGeriTasi_SymlinkSaldirisiEngellenir(t *testing.T) {
	base, home := yeniEv(t)
	attack := filepath.Join(base, "attack") // ev DIŞI
	os.MkdirAll(attack, 0o755)
	kar := filepath.Join(home, ".karantina", "q_evil.php")
	os.WriteFile(kar, []byte("<?php evil"), 0o644)
	// stg = ev dışına symlink (saldırgan kiracı kendi public_html'inde yapar)
	os.Symlink(attack, filepath.Join(home, "public_html", "stg"))
	orij := filepath.Join(home, "public_html", "stg", "evil.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("symlink saldırısı engellenmedi, hata=%v (beklenen errGuvenliYol)", err)
	}
	if _, e := os.Lstat(filepath.Join(attack, "evil.php")); !os.IsNotExist(e) {
		t.Fatalf("KRİTİK: dosya ev DIŞINA (attack) yazıldı — fix çalışmıyor!")
	}
	if _, e := os.Lstat(kar); e != nil {
		t.Errorf("kaynak dosya kaybedildi: %v", e)
	}
}

// Daha derin ara bileşen symlink (public_html/a gerçek, a/b symlink).
func TestGuvenliGeriTasi_DerinSymlinkEngellenir(t *testing.T) {
	base, home := yeniEv(t)
	attack := filepath.Join(base, "attack")
	os.MkdirAll(filepath.Join(home, "public_html", "a"), 0o755)
	os.MkdirAll(attack, 0o755)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("x"), 0o644)
	os.Symlink(attack, filepath.Join(home, "public_html", "a", "b")) // a/b -> attack
	orij := filepath.Join(home, "public_html", "a", "b", "evil.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("derin symlink engellenmedi: %v", err)
	}
	if _, e := os.Lstat(filepath.Join(attack, "evil.php")); !os.IsNotExist(e) {
		t.Fatalf("KRİTİK: derin symlink ile ev dışına yazıldı!")
	}
}

// Ev dışına doğrudan orij (leksik) → errGuvenliYol.
func TestGuvenliGeriTasi_EvDisiYolReddedilir(t *testing.T) {
	base, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("x"), 0o644)
	orij := filepath.Join(base, "baska", "evil.php") // /home/c_x dışında

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("ev dışı yol reddedilmedi: %v", err)
	}
}

// BULGU (kritik): karantina GİRDİSİNİN KENDİSİ ev dışına symlink. Eski kod
// renameat ile symlink'i taşıyıp Fchmodat(..., 0) ile İZLİYOR ve kurbanı 0644
// yapıyordu. Artık kaynak O_NOFOLLOW ile açılıp reddedilmeli; kurban değişmemeli.
func TestGuvenliGeriTasi_SonBilesenSymlinkKurbaniDegistirmez(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o600)
	kar := filepath.Join(home, ".karantina", "q.php")
	if err := os.Symlink(kurban, kar); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "q.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if m := izin(t, kurban); m != 0o600 {
		t.Fatalf("KRİTİK: ev dışındaki kurbanın izni değişti: %v (err=%v)", m, err)
	}
	if err != errGuvenliYol {
		t.Fatalf("symlink karantina girdisi reddedilmedi: %v", err)
	}
	if _, e := os.Lstat(orij); !os.IsNotExist(e) {
		t.Errorf("symlink hedefe taşındı (reddedilmeliydi)")
	}
}

// BULGU (kritik, yarış): rename ile izin işlemi ARASINDA kiracı hedef adı ev
// dışına symlink ile değiştirir. İzin yola göre verilirse kurban 0644 olur;
// doğrulanmış fd'ye göre verilince kurban değişmez, gerçek dosya 0644 olur.
func TestGuvenliGeriTasi_YarisSonrasiSymlinkKurbaniDegistirmez(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o600)
	kar := filepath.Join(home, ".karantina", "q.php")
	if err := os.WriteFile(kar, []byte("<?php meşru"), 0o600); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "q.php")
	kacirilan := filepath.Join(home, "kacirilan.php")
	yarisKur(t, func(asama string) {
		if asama != "geri-tasi" {
			return
		}
		if err := os.Rename(orij, kacirilan); err != nil {
			t.Errorf("yarış kurulamadı: %v", err)
			return
		}
		if err := os.Symlink(kurban, orij); err != nil {
			t.Errorf("yarış kurulamadı: %v", err)
		}
	})

	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if m := izin(t, kurban); m != 0o600 {
		t.Fatalf("KRİTİK: yarışta ev dışındaki kurbanın izni değişti: %v (err=%v)", m, err)
	}
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if m := izin(t, kacirilan); m != 0o644 {
		t.Errorf("izin doğrulanmış inode'a uygulanmadı: %v", m)
	}
}

// Kiracıya ait OLMAYAN dosya (ör. evdeki root'a ait yapılandırmanın .karantina'ya
// taşınması) geri yüklenmemeli: chown ile kiracıya verilirdi.
func TestGuvenliGeriTasi_KiraciyaAitOlmayanReddedilir(t *testing.T) {
	_, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("root sırrı"), 0o640)
	orij := filepath.Join(home, "public_html", "q.php")

	err := guvenliGeriTasi(home, kar, orij, os.Getuid()+1, os.Getgid())
	if err != errGuvenliYol {
		t.Fatalf("kiracıya ait olmayan dosya reddedilmedi: %v", err)
	}
	if _, e := os.Lstat(orij); !os.IsNotExist(e) {
		t.Errorf("kiracıya ait olmayan dosya taşındı")
	}
}

// FIFO karantina girdisi: açılış bloklamamalı ve reddedilmeli.
func TestGuvenliGeriTasi_FIFOReddedilir(t *testing.T) {
	_, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "q.php")
	if err := unix.Mkfifo(kar, 0o600); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "q.php")
	err := zamanla(t, kar, func() error { return guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid()) })
	if err != errGuvenliYol {
		t.Fatalf("FIFO reddedilmedi: %v", err)
	}
	if _, e := os.Lstat(orij); !os.IsNotExist(e) {
		t.Errorf("FIFO taşındı")
	}
}

// Kaynak yoksa ErrNotExist dönmeli ve hedef ara dizinleri OLUŞTURULMAMALI.
func TestGuvenliGeriTasi_KaynakYok(t *testing.T) {
	_, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "yok.php")
	orij := filepath.Join(home, "public_html", "yeni", "x.php")
	err := guvenliGeriTasi(home, kar, orij, os.Getuid(), os.Getgid())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("beklenen ErrNotExist, alınan %v", err)
	}
	if _, e := os.Lstat(filepath.Join(home, "public_html", "yeni")); !os.IsNotExist(e) {
		t.Errorf("kaynak yokken hedef dizin oluşturuldu")
	}
}

// ROOT: sahiplik açık fd'ler üzerinden verilir — oluşturulan ara dizinler ve
// geri yüklenen dosya kiracı uid/gid'ine geçer.
func TestGuvenliGeriTasi_SahiplikKiraciya(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown için root gerekir")
	}
	const kuid, kgid = 12345, 12346
	_, home := yeniEv(t)
	kar := filepath.Join(home, ".karantina", "q.php")
	os.WriteFile(kar, []byte("x"), 0o000)
	if err := os.Chown(kar, kuid, 0); err != nil {
		t.Fatal(err)
	}
	orij := filepath.Join(home, "public_html", "yeni", "alt", "x.php")
	if err := guvenliGeriTasi(home, kar, orij, kuid, kgid); err != nil {
		t.Fatalf("geri yükleme başarısız: %v", err)
	}
	for _, d := range []string{filepath.Join(home, "public_html", "yeni"), filepath.Join(home, "public_html", "yeni", "alt"), orij} {
		if u, g := sahip(t, d); u != kuid || g != kgid {
			t.Errorf("%s sahibi %d:%d, beklenen %d:%d", d, u, g, kuid, kgid)
		}
	}
	if m := izin(t, orij); m != 0o644 {
		t.Errorf("izin %v, beklenen 0644", m)
	}
}

// ---------------------------------------------------------------- karantinayaTasi

var sabitAn = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// Meşru elle karantina: dosya .karantina'ya taşınır, izin 0000.
func TestKarantinayaTasi_Mesru(t *testing.T) {
	_, home := yeniEv(t)
	os.RemoveAll(filepath.Join(home, ".karantina")) // yoksa oluşturulmalı
	src := filepath.Join(home, "public_html", "evil.php")
	os.WriteFile(src, []byte("<?php evil"), 0o644)

	hedef, err := karantinayaTasi(home, src, os.Getuid(), sabitAn)
	if err != nil {
		t.Fatalf("meşru karantina başarısız: %v", err)
	}
	beklenen := filepath.Join(home, ".karantina", "20260929_120000_evil.php")
	if hedef != beklenen {
		t.Fatalf("hedef %q, beklenen %q", hedef, beklenen)
	}
	if m := izin(t, hedef); m != 0 {
		t.Errorf("karantina dosyasının izni %v, beklenen 0000", m)
	}
	if m := izin(t, filepath.Join(home, ".karantina")); m != 0o700 {
		t.Errorf(".karantina izni %v, beklenen 0700", m)
	}
	if _, e := os.Lstat(src); !os.IsNotExist(e) {
		t.Errorf("kaynak hâlâ yerinde")
	}
}

// BULGU (kritik): public_html'deki ev dışını gösteren symlink karantinaya
// aldırılırsa eski kod os.Chmod(hedef, 0) ile kurbanı 0000 yapıyordu.
func TestKarantinayaTasi_SonBilesenSymlinkKurbaniDegistirmez(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o644)
	src := filepath.Join(home, "public_html", "x.php")
	os.Symlink(kurban, src)

	_, err := karantinayaTasi(home, src, os.Getuid(), sabitAn)
	if m := izin(t, kurban); m != 0o644 {
		t.Fatalf("KRİTİK: ev dışındaki kurbanın izni değişti: %v (err=%v)", m, err)
	}
	if err != errGuvenliYol {
		t.Fatalf("symlink reddedilmedi: %v", err)
	}
	if _, e := os.Lstat(src); e != nil {
		t.Errorf("symlink yerinden oynatıldı: %v", e)
	}
}

// Ara dizin symlink: public_html/stg → ev dışı. Ev dışındaki dosya taşınmamalı.
func TestKarantinayaTasi_AraDizinSymlinkReddedilir(t *testing.T) {
	base, home := yeniEv(t)
	attack := filepath.Join(base, "baska_kiraci")
	os.MkdirAll(attack, 0o755)
	kurban := filepath.Join(attack, "index.php")
	os.WriteFile(kurban, []byte("<?php baska"), 0o644)
	os.Symlink(attack, filepath.Join(home, "public_html", "stg"))

	_, err := karantinayaTasi(home, filepath.Join(home, "public_html", "stg", "index.php"), os.Getuid(), sabitAn)
	if err != errGuvenliYol {
		t.Fatalf("ara dizin symlink reddedilmedi: %v", err)
	}
	if m := izin(t, kurban); m != 0o644 {
		t.Fatalf("KRİTİK: başka kiracının dosyası değişti: %v", m)
	}
}

// .karantina'nın kendisi ev dışına symlink: dosya ev dışına taşınmamalı.
func TestKarantinayaTasi_KarantinaDiziniSymlinkReddedilir(t *testing.T) {
	base, home := yeniEv(t)
	attack := filepath.Join(base, "attack")
	os.MkdirAll(attack, 0o755)
	os.RemoveAll(filepath.Join(home, ".karantina"))
	os.Symlink(attack, filepath.Join(home, ".karantina"))
	src := filepath.Join(home, "public_html", "evil.php")
	os.WriteFile(src, []byte("<?php"), 0o644)

	_, err := karantinayaTasi(home, src, os.Getuid(), sabitAn)
	if err != errGuvenliYol {
		t.Fatalf(".karantina symlink reddedilmedi: %v", err)
	}
	if ents, _ := os.ReadDir(attack); len(ents) != 0 {
		t.Fatalf("KRİTİK: dosya ev DIŞINA taşındı: %d girdi", len(ents))
	}
	if _, e := os.Lstat(src); e != nil {
		t.Errorf("kaynak kayboldu: %v", e)
	}
}

// Yarış: renameat ile chmod arasında kiracı karantina adını kurbana symlink'ler.
func TestKarantinayaTasi_YarisSonrasiSymlinkKurbaniDegistirmez(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o644)
	src := filepath.Join(home, "public_html", "evil.php")
	os.WriteFile(src, []byte("<?php"), 0o644)
	hedef := filepath.Join(home, ".karantina", "20260929_120000_evil.php")
	kacirilan := filepath.Join(home, "kacirilan.php")
	yarisKur(t, func(asama string) {
		if asama != "karantina" {
			return
		}
		if err := os.Rename(hedef, kacirilan); err != nil {
			t.Errorf("yarış kurulamadı: %v", err)
			return
		}
		if err := os.Symlink(kurban, hedef); err != nil {
			t.Errorf("yarış kurulamadı: %v", err)
		}
	})

	_, err := karantinayaTasi(home, src, os.Getuid(), sabitAn)
	if m := izin(t, kurban); m != 0o644 {
		t.Fatalf("KRİTİK: yarışta kurbanın izni değişti: %v (err=%v)", m, err)
	}
	if m := izin(t, kacirilan); m != 0 {
		t.Errorf("izin 0000 doğrulanmış inode'a uygulanmadı: %v", m)
	}
}

// Dizin ve kiracıya ait olmayan dosya karantinaya alınmamalı.
func TestKarantinayaTasi_DizinVeYabanciDosyaReddedilir(t *testing.T) {
	_, home := yeniEv(t)
	dizin := filepath.Join(home, "public_html", "wp-content")
	os.MkdirAll(dizin, 0o755)
	if _, err := karantinayaTasi(home, dizin, os.Getuid(), sabitAn); err != errGuvenliYol {
		t.Fatalf("dizin reddedilmedi: %v", err)
	}
	if _, e := os.Lstat(dizin); e != nil {
		t.Errorf("dizin taşındı")
	}
	f := filepath.Join(home, "public_html", "package.json")
	os.WriteFile(f, []byte("{}"), 0o640)
	if _, err := karantinayaTasi(home, f, os.Getuid()+1, sabitAn); err != errGuvenliYol {
		t.Fatalf("kiracıya ait olmayan dosya reddedilmedi: %v", err)
	}
	if m := izin(t, f); m != 0o640 {
		t.Errorf("yabancı dosyanın izni değişti: %v", m)
	}
}

// ---------------------------------------------------------------- karantinaBilgi / guvenliAc

// Liste: symlink girdisi, symlink .karantina ve yabancı dosya için var/boyut SIZMAMALI.
func TestKarantinaBilgi_SymlinkBoyutSizdirmaz(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o600)
	mesru := filepath.Join(home, ".karantina", "m.php")
	os.WriteFile(mesru, []byte("12345"), 0o000)
	if ok, n := karantinaBilgi(home, mesru, os.Getuid()); !ok || n != 5 {
		t.Fatalf("meşru girdi: ok=%v boyut=%d", ok, n)
	}
	if ok, _ := karantinaBilgi(home, mesru, os.Getuid()+1); ok {
		t.Errorf("kiracıya ait olmayan dosya 'mevcut' gösterildi")
	}
	sym := filepath.Join(home, ".karantina", "s.php")
	os.Symlink(kurban, sym)
	if ok, n := karantinaBilgi(home, sym, os.Getuid()); ok || n != 0 {
		t.Fatalf("KRİTİK: symlink ile ev dışı dosyanın varlığı/boyutu sızdı: ok=%v boyut=%d", ok, n)
	}
	// .karantina'nın kendisi ev dışına symlink
	os.RemoveAll(filepath.Join(home, ".karantina"))
	os.Symlink(filepath.Dir(kurban), filepath.Join(home, ".karantina"))
	if ok, n := karantinaBilgi(home, filepath.Join(home, ".karantina", "shadow"), os.Getuid()); ok || n != 0 {
		t.Fatalf("KRİTİK: symlink .karantina ile sızıntı: ok=%v boyut=%d", ok, n)
	}
	if ok, _ := karantinaBilgi(home, kurban, os.Getuid()); ok {
		t.Fatalf("ev dışı yol 'mevcut' gösterildi")
	}
}

// İncele: symlink, FIFO ve yabancı dosya okunmamalı; meşru dosya okunmalı.
func TestGuvenliAc_SymlinkFIFOYabanci(t *testing.T) {
	base, home := yeniEv(t)
	kurban := kurbanYaz(t, base, 0o600)
	mesru := filepath.Join(home, ".karantina", "m.php")
	os.WriteFile(mesru, []byte("<?php m"), 0o600)
	f, err := guvenliAc(home, mesru, os.Getuid())
	if err != nil {
		t.Fatalf("meşru açılamadı: %v", err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "<?php m" {
		t.Errorf("içerik %q", b)
	}
	if _, err := guvenliAc(home, mesru, os.Getuid()+1); err != errGuvenliYol {
		t.Errorf("kiracıya ait olmayan dosya açıldı: %v", err)
	}
	sym := filepath.Join(home, ".karantina", "s.php")
	os.Symlink(kurban, sym)
	if f, err := guvenliAc(home, sym, os.Getuid()); err != errGuvenliYol {
		if f != nil {
			f.Close()
		}
		t.Fatalf("KRİTİK: symlink izlendi: %v", err)
	}
	fifo := filepath.Join(home, ".karantina", "f.php")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	err = zamanla(t, fifo, func() error {
		f, err := guvenliAc(home, fifo, os.Getuid())
		if f != nil {
			f.Close()
		}
		return err
	})
	if err != errGuvenliYol {
		t.Fatalf("FIFO reddedilmedi: %v", err)
	}
}
