package laravel

// laravel3_regresyon_test.go — 3. tur onarımının davranış testleri (t.TempDir, canlı
// sisteme dokunmaz):
//   1) envOku/envYazilabilir: uygulama dizini EV İÇİ symlink üzerinden gidiyorsa .env
//      "boş/yok" gibi GÖRÜNMEMELİ (ELOOP → açık hata) — yoksa düzenleyici boş kaydedip
//      gerçek .env'i ezerdi.
//   2) sahiplikGirdi: denetim ile chown arasında ad takası (renameat2 RENAME_EXCHANGE) ile
//      ev dışı bir hardlink'in sahipliği devredilememeli (fd tabanlı: tek çözüm).
//   3) evAltiMkdirHepsi: yeni yaratılan dizin, dolu bir kök dizinle takas edilirse sahiplik
//      verilmemeli.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- 1) ev içi symlink'li uygulama dizininde .env açık hata (ELOOP) ----

func TestEnvOku_EvIciSymlink_AcikHata(t *testing.T) {
	e := sahteKok(t, kiraciUID, kiraciGID)

	// home/surumler/v1/.env (gerçek, sırlı) ; home/public_html/current -> ../surumler/v1
	gercek := filepath.Join(e.home, "surumler", "v1")
	dosyaYaz(t, filepath.Join(gercek, ".env"), "APP_KEY=base64:gercek\n"+gizliIcerik+"\n", 0o600)
	current := filepath.Join(e.app, "current")
	symlinkKur(t, filepath.Join("..", "..", "surumler", "v1"), current)

	// envOku: ELOOP → errEnvGuvensiz (yok DEĞİL); içerik sızmamalı.
	s, err := envOku("c_test", current)
	if err == nil {
		t.Fatalf("ev içi symlink'li uygulama dizini için hata bekleniyordu; içerik=%q", s)
	}
	if errors.Is(err, errEnvYok) {
		t.Fatal("ELOOP errEnvYok olarak sınıflandı — düzenleyici boş .env gösterir ve kaydedince gerçeği ezerdi")
	}
	if !errors.Is(err, errEnvGuvensiz) {
		t.Fatalf("errEnvGuvensiz bekleniyordu: %v", err)
	}
	if strings.Contains(s, gizliIcerik) {
		t.Fatalf("içerik sızdı: %q", s)
	}

	// envYazilabilir: güvensiz .env üzerine yazım REDDEDİLMELİ (düzenleyici kaydetmesin).
	if err := envYazilabilir("c_test", current); err == nil || errors.Is(err, errEnvYok) {
		t.Fatalf("güvensiz .env için envYazilabilir hata döndürmeliydi: %v", err)
	}

	// Karşılaştırma: gerçekten yok → errEnvYok; yazım serbest.
	bos := filepath.Join(e.app, "bosapp")
	if err := os.MkdirAll(bos, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := envOku("c_test", bos); !errors.Is(err, errEnvYok) {
		t.Fatalf(".env yokken errEnvYok bekleniyordu: %v", err)
	}
	if err := envYazilabilir("c_test", bos); err != nil {
		t.Fatalf(".env yokken yazım serbest olmalı: %v", err)
	}

	// Karşılaştırma: meşru düzenli .env → okunur; yazım serbest.
	normal := filepath.Join(e.app, "normal")
	dosyaYaz(t, filepath.Join(normal, ".env"), "APP_KEY=base64:x\n", 0o600)
	if s, err := envOku("c_test", normal); err != nil || s != "APP_KEY=base64:x\n" {
		t.Fatalf("meşru .env okunamadı: %v %q", err, s)
	}
	if err := envYazilabilir("c_test", normal); err != nil {
		t.Fatalf("meşru .env için yazım serbest olmalı: %v", err)
	}
}

// ---- 2) sahiplik chown'unda ad takası ile hardlink devri ----

// renameDestekli: RENAME_EXCHANGE bu fs'de destekleniyor mu?
func renameDestekli(t *testing.T, dir string) {
	t.Helper()
	a := filepath.Join(dir, ".rx_a")
	b := filepath.Join(dir, ".rx_b")
	if err := os.WriteFile(a, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE); err != nil {
		t.Skipf("RENAME_EXCHANGE desteklenmiyor: %v", err)
	}
	_ = os.Remove(a)
	_ = os.Remove(b)
}

func TestSahiplikGirdi_AdTakasiHardlinkDevriEngellenir(t *testing.T) {
	kokGerekli(t)
	e := sahteKok(t, kiraciUID, kiraciGID)
	kiraciYap(t, e.home, e.app)
	st := filepath.Join(e.app, "storage")
	if err := os.MkdirAll(st, 0o755); err != nil {
		t.Fatal(err)
	}
	renameDestekli(t, st)

	// Ev dışı, root sahipli kurban. storage içinde ona bir hardlink ("attack").
	kurban := filepath.Join(e.dis, "gizli-kok-dosya")
	dosyaYaz(t, kurban, "root-only", 0o600)
	attack := filepath.Join(st, "attack")
	if err := os.Link(kurban, attack); err != nil {
		t.Fatal(err)
	}
	// Meşru, root sahipli göç kalıntısı ("victimslot", Nlink==1): denetimi geçer.
	victimslot := filepath.Join(st, "victimslot")
	dosyaYaz(t, victimslot, "migrasyon", 0o600) // root sahipli, Nlink 1

	// Yarış kancası: "victimslot" denetlendikten SONRA, chown'dan ÖNCE adları takas et →
	// isim tabanlı bir chown artık kurbanın hardlink'ine ("attack") çarpar.
	var birKez bool
	yarisKancasi = func(asama string, dfd int, ad string) {
		if asama != "sahiplik" || ad != "victimslot" || birKez {
			return
		}
		birKez = true
		if err := unix.Renameat2(dfd, "victimslot", dfd, "attack", unix.RENAME_EXCHANGE); err != nil {
			t.Fatalf("takas başarısız: %v", err)
		}
	}
	t.Cleanup(func() { yarisKancasi = nil })

	if err := evAltiSahiplikVer(e.home, "public_html/storage", kiraciUID, kiraciGID); err != nil {
		t.Fatalf("evAltiSahiplikVer: %v", err)
	}
	if !birKez {
		t.Fatal("yarış kancası hiç tetiklenmedi (test kurulumu bozuk)")
	}
	// fd tabanlı chown pinlenmiş inode'u değiştirir; kurban (ev dışı) root kalmalı.
	if u := uidOf(t, kurban, false); u != 0 {
		t.Fatalf("ad takası ile ev dışı kurban kiracıya devredildi (uid=%d)", u)
	}
}

// ---- 3) mkdir'de yeni dizinin dolu kök diziniyle takası ----

func TestEvAltiMkdirHepsi_AdTakasiDoluDizinReddi(t *testing.T) {
	kokGerekli(t)
	e := sahteKok(t, kiraciUID, kiraciGID)
	kiraciYap(t, e.home, e.app)
	bs := filepath.Join(e.app, "bootstrap")
	if err := os.MkdirAll(bs, 0o755); err != nil { // root sahipli ebeveyn (göç kalıntısı)
		t.Fatal(err)
	}
	renameDestekli(t, bs)

	// bootstrap/dolu: root sahipli, DOLU dizin (takas hedefi).
	dolu := filepath.Join(bs, "dolu")
	dosyaYaz(t, filepath.Join(dolu, "gizli.php"), "<?php // root", 0o600)

	var birKez bool
	yarisKancasi = func(asama string, dfd int, ad string) {
		if asama != "mkdir" || ad != "cache" || birKez {
			return
		}
		birKez = true
		if err := unix.Renameat2(dfd, "cache", dfd, "dolu", unix.RENAME_EXCHANGE); err != nil {
			t.Fatalf("takas başarısız: %v", err)
		}
	}
	t.Cleanup(func() { yarisKancasi = nil })

	err := evAltiMkdirHepsi(e.home, "public_html/bootstrap/cache", kiraciUID, kiraciGID, 0o755)
	if !birKez {
		t.Fatal("yarış kancası tetiklenmedi (kurulum bozuk)")
	}
	if err == nil {
		t.Fatal("dolu dizinle takas sonrası sahiplik verildi (hata bekleniyordu)")
	}
	// Takas sonrası dolu inode bootstrap/cache adında; kök kalmalı (chown edilmemeli).
	if u := uidOf(t, filepath.Join(bs, "cache"), true); u != 0 {
		t.Fatalf("dolu dizin (ad takası) kiracıya devredildi (uid=%d)", u)
	}
	if u := uidOf(t, filepath.Join(bs, "cache", "gizli.php"), false); u != 0 {
		t.Fatalf("dolu dizinin içeriği kiracıya devredildi (uid=%d)", u)
	}
}
