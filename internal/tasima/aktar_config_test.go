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

// yrFifoSerbest — FIFO'yu okuyucu olarak açarken takılmış çağrıyı, sonuç gelene
// dek yazıcı açarak serbest bırakır (bekleyen okuyucu varken O_NONBLOCK yazıcı
// açılır): takılan test süresiz asılı kalmaz, hata olarak raporlanır.
func yrFifoSerbest[T any](fifo string, bitti <-chan T) T {
	for {
		if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			unix.Close(fd)
		}
		select {
		case v := <-bitti:
			return v
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Yapılandırma adayının DİZİN bileşeni FIFO (doğrudan ya da ev içi bağla):
// açılışı takılmaz (eskiden os.Root.Open son bileşeni O_DIRECTORY'siz açıp
// yazıcı bekliyordu → tek taşıma yuvası panel yeniden başlayana dek dolu),
// aday atlanır ve sonraki meşru aday yine işlenir (okuyucular dahil).
func TestYapilandirma_FifoDizinTakilmaz(t *testing.T) {
	for _, sn := range []string{"doğrudan", "ev içi bağ"} {
		t.Run(sn, func(t *testing.T) {
			o := yrYeniOrtam(t)
			ph := filepath.Join(o.ev, "public_html")
			for _, d := range []string{ph, filepath.Join(ph, "app"), filepath.Join(ph, "sites"), filepath.Join(ph, "sites", "default")} {
				o.dizin(t, d)
			}
			// app/etc/env.php'den SONRAKİ aday: meşru, kiracının kendi dosyası.
			o.dosya(t, filepath.Join(ph, "sites", "default", "settings.php"), "<?php\ndefine( 'DB_NAME', 'eski_db' );\n", 0o640)
			fifo := filepath.Join(ph, "app", "etc")
			if sn != "doğrudan" {
				fifo = filepath.Join(o.ev, "boru")
				o.bag(t, "../../boru", filepath.Join(ph, "app", "etc"))
			}
			if err := unix.Mkfifo(fifo, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Lchown(fifo, o.uid, o.gid); err != nil {
				t.Fatal(err)
			}

			type sonuc struct {
				bulunan []string
				yazilan int
			}
			bitti := make(chan sonuc, 1)
			go func() {
				b := configtenDBBul(ph)
				bitti <- sonuc{b, (&Handlers{}).configYenidenYaz(ph, yrEsleme, "", yrYeniPw, yrLog(t))}
			}()
			var s sonuc
			select {
			case s = <-bitti:
			case <-time.After(20 * time.Second):
				t.Error("FIFO dizin bileşeninde takıldı (20 sn)")
				s = yrFifoSerbest(fifo, bitti)
			}
			if len(s.bulunan) != 1 || s.bulunan[0] != "eski_db" || s.yazilan != 1 {
				t.Errorf("FIFO adayından sonraki meşru aday işlenmedi: bulunan=%v yazılan=%d", s.bulunan, s.yazilan)
			}
		})
	}
}

// Okuyucular (configtenDBBul/configtenDBKimlik) da ev kilidinde: bağla ya da
// sabit bağla komşunun yapılandırmasındaki DB adı/kullanıcı/parola bu hesaba
// OKUNMAZ; ev dışı web kökünde hiçbir şey okunmaz. Meşru durumda kiracının
// kendi yapılandırması okunur (test boşuna geçmiyor).
func TestYapilandirma_OkuyucularKomsuyuOkumaz(t *testing.T) {
	senaryolar := []struct {
		ad string
		// kur: web kökünü döndürür (komsu = komşunun public_html'i).
		kur   func(t *testing.T, o yrOrtam, ph, komsu string) string
		mesru bool
	}{
		{"public_html komşuya bağ", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.bag(t, komsu, ph)
			return ph
		}, false},
		{"wp-config komşuya bağ", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.bag(t, filepath.Join(komsu, "wp-config.php"), filepath.Join(ph, "wp-config.php"))
			return ph
		}, false},
		{"wp-config komşuya sabit bağ", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			if err := os.Link(filepath.Join(komsu, "wp-config.php"), filepath.Join(ph, "wp-config.php")); err != nil {
				t.Fatal(err)
			}
			return ph
		}, false},
		{"ev kökü dışı web kökü", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			d := t.TempDir()
			o.dosya(t, filepath.Join(d, "wp-config.php"), "<?php\ndefine( 'DB_NAME', 'eski_db' );\ndefine( 'DB_USER', 'eski_kul' );\ndefine( 'DB_PASSWORD', 'eski_pw' );\n", 0o644)
			return d
		}, false},
		{"meşru: kiracının kendi yapılandırması", func(t *testing.T, o yrOrtam, ph, komsu string) string {
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "wp-config.php"), "<?php\ndefine( 'DB_NAME', 'eski_db' );\ndefine( 'DB_USER', 'eski_kul' );\ndefine( 'DB_PASSWORD', 'eski_pw' );\n", 0o640)
			return ph
		}, true},
	}
	for _, sn := range senaryolar {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			webRoot := sn.kur(t, o, filepath.Join(o.ev, "public_html"), o.komsu(t))
			bulunan := configtenDBBul(webRoot)
			kimlik := configtenDBKimlik(webRoot)
			okundu := len(bulunan) == 1 && bulunan[0] == "eski_db" && kimlik["eski_db"] == dbKimlik{Kul: "eski_kul", Pw: "eski_pw"}
			if sn.mesru && !okundu {
				t.Errorf("kiracının kendi yapılandırması okunmadı: bulunan=%v kimlik=%d", bulunan, len(kimlik))
			}
			if !sn.mesru && (len(bulunan) != 0 || len(kimlik) != 0) {
				t.Errorf("başkasının yapılandırması okundu: bulunan=%v kimlik=%d", bulunan, len(kimlik))
			}
		})
	}
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
