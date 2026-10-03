package tasima

// aktar_config_test.go — H3 regresyonu: yapılandırma yeniden yazımı ve Laravel
// düzeltmesi kiracının ev dizinine çapalı, yol boyunca HİÇBİR bağı izlemeden
// (evalti.Ac: ev dışına çıkan da, ev içinde kalan da) ve yalnız kiracının kendi
// düzenli dosyasında çalışır. Her senaryo tek bir korumayı sınar (komşu kiracı
// testte aynı uid'dir: bağ korumaları uid denetimine yaslanmaz).
// Ortam ve yardımcılar: aktar_dosya_test.go.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
		// Önbellek silme de bağ izlemez: bootstrap komşuya bağ.
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
	laravelSonrasiDuzelt(o.ev, "public_html", yrAlan, o.uid, yrLog(t))
}

// yrLaravelGunluk — laravelSonrasiDuzelt'i koşar, iş günlüğünü döndürür.
func yrLaravelGunluk(o yrOrtam, dizin string) []string {
	var gunluk []string
	laravelSonrasiDuzelt(o.ev, dizin, yrAlan, o.uid, func(f string, a ...any) { gunluk = append(gunluk, fmt.Sprintf(f, a...)) })
	return gunluk
}

// Ev İÇİNDE kalan bağ da izlenmez: kiracı evinde kök sahipli bir alan (burada
// <ev>/.kok; panelin kiracı evine koyduğu kök dizinleri temsil eder) varken
// kiracının oraya kurduğu göreli bağ kökün yapılandırma okuma/yazma ve Laravel
// önbellek silme işlemini o alana YÖNLENDİREMEZ. (Önceki os.Root yalnız ev DIŞINA
// çıkan bağı reddediyor, ev içi bağı izliyordu.) Meşru karşılıkları (bağsız) diğer
// testlerde yeşildir.
func TestYapilandirma_EvIciKokAlaninaBagIzlenmez(t *testing.T) {
	senaryolar := []struct {
		ad    string
		kur   func(t *testing.T, o yrOrtam, ph, kok string)
		calis func(t *testing.T, o yrOrtam, ph string)
	}{
		{"yapılandırma dizini kök alanına bağ", func(t *testing.T, o yrOrtam, ph, kok string) {
			env := filepath.Join(kok, "app", "etc", "env.php")
			yrYaz(t, env, "<?php\ndefine( 'DB_NAME', 'eski_db' );\ndefine( 'DB_USER', 'eski_kul' );\ndefine( 'DB_PASSWORD', 'eski_pw' );\n", 0o640)
			if err := os.Lchown(env, o.uid, o.gid); err != nil {
				t.Fatal(err)
			}
			o.dizin(t, ph)
			o.bag(t, "../.kok/app", filepath.Join(ph, "app"))
		}, func(t *testing.T, o yrOrtam, ph string) {
			if b, k := configtenDBBul(ph), configtenDBKimlik(ph); len(b) != 0 || len(k) != 0 {
				t.Errorf("kök alanındaki yapılandırma okundu: bulunan=%v kimlik=%d", b, len(k))
			}
			yrConfigYaz(t, o, ph)
		}},
		{"Laravel önbelleği kök alanına bağ", func(t *testing.T, o yrOrtam, ph, kok string) {
			yrYaz(t, filepath.Join(kok, "bootstrap", "cache", "config.php"), "<?php return [];\n", 0o644)
			o.dizin(t, ph)
			o.dosya(t, filepath.Join(ph, "artisan"), "#!/usr/bin/env php\n", 0o644)
			o.bag(t, "../.kok/bootstrap", filepath.Join(ph, "bootstrap"))
		}, yrLaravel},
		{"Laravel uygulama dizini kök alanına bağ", func(t *testing.T, o yrOrtam, ph, kok string) {
			for ad, icerik := range map[string]string{"artisan": "#!/usr/bin/env php\n", ".env": "APP_URL=http://eski.example\n"} {
				yol := filepath.Join(kok, "uyg", ad)
				yrYaz(t, yol, icerik, 0o644)
				if err := os.Lchown(yol, o.uid, o.gid); err != nil {
					t.Fatal(err)
				}
			}
			o.bag(t, ".kok/uyg", filepath.Join(o.ev, "uyg"))
		}, func(t *testing.T, o yrOrtam, ph string) {
			// Bağlı dizin Laravel SAYILMAZ: günlük kök alanında artisan olup
			// olmadığını da söylemez.
			if g := yrLaravelGunluk(o, "uyg"); len(g) != 0 {
				t.Errorf("bağlı uygulama dizini Laravel sayıldı: %q", g)
			}
		}},
	}
	for _, sn := range senaryolar {
		t.Run(sn.ad, func(t *testing.T) {
			o := yrYeniOrtam(t)
			kok := filepath.Join(o.ev, ".kok")
			if err := os.Mkdir(kok, 0o755); err != nil {
				t.Fatal(err)
			}
			ph := filepath.Join(o.ev, "public_html")
			sn.kur(t, o, ph, kok)
			once := yrOzet(t, kok)
			sn.calis(t, o, ph)
			yrOzetKarsilastir(t, sn.ad, once, yrOzet(t, kok))
		})
	}
}

// artisan bağı izlenmez: kiracının artisan'ı kökün okuyabildiği bir yolu
// gösterdiğinde Laravel tespiti (ve bayiye açılan iş günlüğü) o yolun VAR olup
// olmadığına göre değişmez — kök dosyalarının varlığı sızmaz.
func TestLaravel_ArtisanBagiVarlikSizdirmaz(t *testing.T) {
	gunlukler := map[string][]string{}
	for _, hedef := range []string{"var", "yok"} {
		o := yrYeniOrtam(t)
		gizli := filepath.Join(o.base, "kokgizli")
		yrYaz(t, filepath.Join(gizli, "var"), "KOK", 0o600)
		if err := os.Chmod(gizli, 0o700); err != nil {
			t.Fatal(err)
		}
		ph := filepath.Join(o.ev, "public_html")
		o.dizin(t, ph)
		o.bag(t, filepath.Join(gizli, hedef), filepath.Join(ph, "artisan"))
		gunlukler[hedef] = yrLaravelGunluk(o, "public_html")
	}
	if !reflect.DeepEqual(gunlukler["var"], gunlukler["yok"]) {
		t.Errorf("artisan bağının hedefinin varlığı günlüğe yansıdı:\n var: %q\n yok: %q", gunlukler["var"], gunlukler["yok"])
	}
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

// Okuyucular (configtenDBBul/configtenDBKimlik) da bağ izlemez: bağla ya da
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
	if y := yrYedekler(t); len(y) != 1 || !y[ozgun] {
		t.Errorf("özgün yapılandırma yedeklenmedi: %v", y)
	}
	g, _ := os.ReadDir(ph)
	for _, e := range g {
		if strings.HasPrefix(e.Name(), ".gosp_cfg_") {
			t.Errorf("geçici dosya kaldı: %s", e.Name())
		}
	}
}

// yrYedekler — yapılandırma yedek kökündeki her yedeğin içeriği (adlandırma
// biçiminden bağımsız).
func yrYedekler(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(tasimaYedekKok, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(p)
		out[string(b)] = true
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Yedek adı kiracı + web kökü ile benzersiz: Laravel (public_html/public) belge
// köklü iki kiracı ve aynı kiracının iki sitesi birbirinin özgün yapılandırma
// yedeğini EZMEZ (eskiden hepsi "public_html" dizinine düşüyordu).
func TestYapilandirma_YedekBenzersiz(t *testing.T) {
	o := yrYeniOrtam(t)
	webRootlar := []string{
		filepath.Join(o.ev, "public_html", "public"),
		filepath.Join(o.ev, "domains", "b.example", "public_html", "public"),
		filepath.Join(o.base, "c_ikinci", "public_html", "public"),
	}
	ozgunler := map[string]bool{}
	for i, w := range webRootlar {
		rel, err := filepath.Rel(o.base, w)
		if err != nil {
			t.Fatal(err)
		}
		d := o.base
		for _, b := range strings.Split(rel, string(filepath.Separator)) {
			if d = filepath.Join(d, b); d != o.ev {
				o.dizin(t, d)
			}
		}
		ozgun := fmt.Sprintf("<?php\ndefine( 'DB_NAME', 'eski_db' );\n// site %d\n", i)
		o.dosya(t, filepath.Join(w, "wp-config.php"), ozgun, 0o640)
		ozgunler[ozgun] = true
	}
	for _, w := range webRootlar {
		if n := (&Handlers{}).configYenidenYaz(w, yrEsleme, "", yrYeniPw, yrLog(t)); n != 1 {
			t.Fatalf("%s: %d dosya yazıldı, beklenen 1", w, n)
		}
	}
	if y := yrYedekler(t); !reflect.DeepEqual(y, ozgunler) {
		t.Errorf("yedekler birbirini ezdi: %d yedek, beklenen %d", len(y), len(ozgunler))
	}
}

// Yedek yazılamazsa dosya değiştirilmez ve iş günlüğüne (bayiye açılır) panelin
// yedek yolu sızmaz — ayrıntı yalnız sunucu logunda.
func TestYapilandirma_YedekHatasiYolSizdirmaz(t *testing.T) {
	o := yrYeniOrtam(t)
	yrYaz(t, tasimaYedekKok, "dizin degil", 0o600)
	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)
	wp := filepath.Join(ph, "wp-config.php")
	o.dosya(t, wp, "<?php\ndefine( 'DB_NAME', 'eski_db' );\n", 0o640)
	once := yrOzet(t, ph)

	var gunluk []string
	kaydet := func(f string, a ...any) { gunluk = append(gunluk, fmt.Sprintf(f, a...)) }
	if n := (&Handlers{}).configYenidenYaz(ph, yrEsleme, "", yrYeniPw, kaydet); n != 0 {
		t.Errorf("yedeksiz %d dosya yazıldı", n)
	}
	yrOzetKarsilastir(t, "yedeksiz", once, yrOzet(t, ph))
	metin := strings.Join(gunluk, "\n")
	if !strings.Contains(metin, "yedegi alinamadi") {
		t.Errorf("yedek hatası günlüğe yazılmadı: %q", metin)
	}
	if strings.Contains(metin, tasimaYedekKok) || strings.Contains(metin, o.base) {
		t.Errorf("iş günlüğüne panel yolu sızdı: %q", metin)
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
