package tasima

// aktar_config_test.go — H3 regresyonu: yapılandırma yeniden yazımı ve Laravel
// düzeltmesi ev köküne KİLİTLİ (os.Root) ve yalnız kiracının kendi düzenli
// dosyasında çalışır. Her senaryo tek bir korumayı sınar (komşu kiracı testte
// aynı uid'dir: ev kilidi ve bağ korumaları uid denetimine yaslanmaz).
// Ortam ve yardımcılar: aktar_dosya_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var yrEsleme = map[string]dbHedef{"eski_db": {Ad: "c_test_yeni", Kul: "c_test_db"}}

const yrYeniPw = "YeniParola1234"

func TestYapilandirma_BaglarKurbaniDegistirmez(t *testing.T) {
	senaryolar := []struct {
		ad string
		// kur: <ev>/public_html'i hazırlar (komsu = komşunun public_html'i);
		// döner: özeti alınacak kurban yolu.
		kur   func(t *testing.T, o yrOrtam, ph, komsu string) string
		calis func(t *testing.T, o yrOrtam, ph string)
	}{
		// Ev kilidi: ara bileşen (public_html) komşu evine mutlak bağ.
		{"public_html komşuya bağ (yapılandırma)", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.bag(t, komsu, ph)
			return komsu
		}, yrConfigYaz},
		// Son bileşen izlenmez: .env komşunun .env'ine mutlak bağ.
		{".env komşuya bağ (Laravel)", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
			o.bag(t, filepath.Join(komsu, ".env"), filepath.Join(ph, ".env"))
			return komsu
		}, yrLaravel},
		// Sabit bağ: aynı inode komşunun evinde (tek bağlantı şartı).
		{".env komşuya sabit bağ (Laravel)", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
			if err := os.Link(filepath.Join(komsu, ".env"), filepath.Join(ph, ".env")); err != nil {
				t.Fatal(err)
			}
			return komsu
		}, yrLaravel},
		// Yalnız kiracının kendi dosyası: kök sahipli .env'e dokunulmaz.
		{"kök sahipli .env (Laravel)", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
			env := filepath.Join(ph, ".env")
			yrYaz(t, env, "APP_URL=http://eski.example\nAPP_KEY=kok-sirri\n", 0o640)
			return env
		}, yrLaravel},
		// Önbellek silme de ev kilidinde: bootstrap komşuya bağ.
		{"bootstrap komşuya bağ (Laravel)", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
			o.bag(t, filepath.Join(komsu, "bootstrap"), filepath.Join(ph, "bootstrap"))
			return komsu
		}, yrLaravel},
	}
	for _, sn := range senaryolar {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			komsu := o.komsu(t)
			ph := filepath.Join(o.ev, "public_html")
			kurban := sn.kur(t, o, ph, komsu)
			once := yrOzet(t, kurban)
			sn.calis(t, o, ph)
			yrOzetKarsilastir(t, sn.ad, once, yrOzet(t, kurban))
		})
	}
}

func yrConfigYaz(t *testing.T, o yrOrtam, ph string) {
	t.Helper()
	if n := (&Handlers{}).configYenidenYaz(ph, yrEsleme, "", yrYeniPw, yrLog(t)); n != 0 {
		t.Errorf("%d yapılandırma dosyası yazıldı, beklenen 0", n)
	}
}

func yrLaravel(t *testing.T, o yrOrtam, ph string) {
	t.Helper()
	ev, err := os.OpenRoot(o.ev)
	if err != nil {
		t.Fatal(err)
	}
	defer ev.Close()
	laravelSonrasiDuzelt(ev, "public_html", yrAlan, o.uid, yrLog(t))
}

// FIFO .env: okunmaz, izni değişmez, takılmaz (eskiden os.ReadFile yazıcı
// bekleyip kalemi süresiz kilitliyordu).
func TestLaravel_FifoEnvTakilmaz(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
	env := filepath.Join(ph, ".env")
	if err := unix.Mkfifo(env, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(env, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
	once := yrOzet(t, env)
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		yrLaravel(t, o, ph)
	}()
	select {
	case <-bitti:
	case <-time.After(20 * time.Second):
		t.Fatal("FIFO .env'de takıldı")
	}
	yrOzetKarsilastir(t, "fifo", once, yrOzet(t, env))
}

// Meşru: kiracının yapılandırması yeniden yazılır; sahip, izin, tek bağlantı
// korunur, özgün içerik yedeklenir.
func TestYapilandirma_KiracininDosyasiYenilenir(t *testing.T) {
	o := yrYeniOrtam(t)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	wp := filepath.Join(ph, "wp-config.php")
	ozgun := "<?php\ndefine( 'DB_NAME', 'eski_db' );\ndefine( 'DB_USER', 'eski_kul' );\ndefine( 'DB_PASSWORD', 'eski_pw' );\n"
	o.dosya(t, wp, ozgun, 0o640)

	if n := (&Handlers{}).configYenidenYaz(ph, yrEsleme, "", yrYeniPw, yrLog(t)); n != 1 {
		t.Fatalf("%d dosya yazıldı, beklenen 1", n)
	}
	b, err := os.ReadFile(wp)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"'c_test_yeni'", "'c_test_db'", "'" + yrYeniPw + "'"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("yapılandırmada %s yok: %q", s, b)
		}
	}
	fi, err := os.Lstat(wp)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o640 || st.Uid != uint32(o.uid) || st.Nlink != 1 {
		t.Errorf("yazılan dosya: mod=%v uid=%d nlink=%d", fi.Mode(), st.Uid, st.Nlink)
	}
	if y, err := os.ReadFile(filepath.Join(tasimaYedekKok, yrSK, "wp-config.php")); err != nil || string(y) != ozgun {
		t.Errorf("özgün yapılandırma yedeklenmedi: %q (%v)", y, err)
	}
	g, _ := os.ReadDir(ph)
	for _, e := range g {
		if strings.HasPrefix(e.Name(), ".gosp_cfg_") {
			t.Errorf("geçici dosya kaldı: %s", e.Name())
		}
	}
}

// Web kökü bir kiracı evinde değilse (ev kökü dışı ya da geçersiz kullanıcı
// bileşeni) yapılandırmaya hiç dokunulmaz.
func TestYapilandirma_EvDisiWebKokuAtlanir(t *testing.T) {
	o := yrYeniOrtam(t)
	for _, kok := range []string{t.TempDir(), filepath.Join(o.base, "kurban")} {
		yrKurban(t, kok)
		once := yrOzet(t, kok)
		if n := (&Handlers{}).configYenidenYaz(kok, yrEsleme, "", yrYeniPw, yrLog(t)); n != 0 {
			t.Errorf("%s: %d dosya yazıldı", kok, n)
		}
		yrOzetKarsilastir(t, "ev dışı", once, yrOzet(t, kok))
	}
}
