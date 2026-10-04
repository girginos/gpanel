package wordpress

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------- sürüm (dosyadan)

func sahteWP(t *testing.T, icerik string) (home string) {
	t.Helper()
	home = t.TempDir()
	d := filepath.Join(home, "public_html", "wp-includes")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "version.php"), []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestSurumOku_Gecerli(t *testing.T) {
	home := sahteWP(t, "<?php\n/**\n * x\n */\n$wp_version = '6.4.2';\n\n$wp_db_version = 56657;\n")
	if got := surumOkuRel(home, "public_html/wp-includes/version.php"); got != "6.4.2" {
		t.Fatalf("surum=%q", got)
	}
}

func TestSurumOku_Reddeder(t *testing.T) {
	for _, ic := range []string{
		"<?php\n$wp_version = '6.4.2; rm -rf /';",
		"<?php\n$wp_version = '../../etc';",
		"<?php\n$wp_version = '';",
		"<?php // yok",
		"<?php\n$wp_version = \"6.4.2\";", // çift tırnak: WP kendi dosyasında tek tırnak kullanır
	} {
		home := sahteWP(t, ic)
		if got := surumOkuRel(home, "public_html/wp-includes/version.php"); got != "" {
			t.Errorf("%q kabul edildi: %q", ic, got)
		}
	}
}

// wpGecerli: gerçek version.php biçimi ($wp_version satır başında).
const wpGecerli = "<?php\n$wp_version = '6.4.2';\n"

// Kiracı version.php'yi başka yere symlink'leyip root okumasını saptıramaz.
func TestSurumOku_SymlinkTakipEdilmez(t *testing.T) {
	home := sahteWP(t, wpGecerli)
	disari := filepath.Join(t.TempDir(), "baska.php")
	_ = os.WriteFile(disari, []byte(wpGecerli), 0o644)
	vp := filepath.Join(home, "public_html", "wp-includes", "version.php")
	_ = os.Remove(vp)
	if err := os.Symlink(disari, vp); err != nil {
		t.Skip(err)
	}
	if got := surumOkuRel(home, "public_html/wp-includes/version.php"); got != "" {
		t.Fatalf("symlink izlendi: %q", got)
	}
	// home İÇİNDE kalan symlink de izlenmez (NO_SYMLINKS; BENEATH tek başına yetmez)
	home3 := sahteWP(t, wpGecerli)
	_ = os.Rename(filepath.Join(home3, "public_html", "wp-includes", "version.php"), filepath.Join(home3, "public_html", "wp-includes", "gercek.php"))
	_ = os.Symlink("gercek.php", filepath.Join(home3, "public_html", "wp-includes", "version.php"))
	if got := surumOkuRel(home3, "public_html/wp-includes/version.php"); got != "" {
		t.Fatalf("home-içi symlink izlendi: %q", got)
	}
	// ara bileşen symlink'i
	home2 := t.TempDir()
	hedef := t.TempDir()
	_ = os.MkdirAll(filepath.Join(hedef, "wp-includes"), 0o755)
	_ = os.WriteFile(filepath.Join(hedef, "wp-includes", "version.php"), []byte(wpGecerli), 0o644)
	_ = os.Symlink(hedef, filepath.Join(home2, "public_html"))
	if got := surumOkuRel(home2, "public_html/wp-includes/version.php"); got != "" {
		t.Fatalf("ara-bileşen symlink izlendi: %q", got)
	}
}

// FIFO: O_NONBLOCK sayesinde root goroutine'i sonsuza dek takılmaz.
func TestSurumOku_FIFOTakilmaz(t *testing.T) {
	home := sahteWP(t, "x")
	vp := filepath.Join(home, "public_html", "wp-includes", "version.php")
	_ = os.Remove(vp)
	if err := syscall.Mkfifo(vp, 0o644); err != nil {
		t.Skip(err)
	}
	bitti := make(chan string, 1)
	go func() { bitti <- surumOkuRel(home, "public_html/wp-includes/version.php") }()
	select {
	case got := <-bitti:
		if got != "" {
			t.Fatalf("fifo'dan sürüm: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("FIFO okuması takıldı")
	}
}

func TestSurumOku_DizinDisi(t *testing.T) {
	if surumOku("c_x", "/etc") != "" || surumOku("c_x", "/home/c_baska/public_html") != "" {
		t.Fatal("kiracı dizini dışı kabul edildi")
	}
}

// ---------------------------------------------------------------- çekirdek güncelleme

func apiSunucu(t *testing.T, cevap func(surum string) (int, string)) (*httptest.Server, *int64) {
	t.Helper()
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&n, 1)
		time.Sleep(30 * time.Millisecond)
		kod, govde := cevap(r.URL.Query().Get("version"))
		w.WriteHeader(kod)
		_, _ = w.Write([]byte(govde))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestCekirdekKontrol_TekilVeOnbellekli(t *testing.T) {
	srv, n := apiSunucu(t, func(string) (int, string) {
		return 200, `{"offers":[{"response":"upgrade","current":"7.1.2"}]}`
	})
	c := yeniCekirdekKontrol(srv.URL)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := c.sorgu(context.Background(), "6.4.2")
			if !g.Bilinen || !g.Var || g.Hedef != "7.1.2" {
				t.Errorf("sonuç %+v", g)
			}
		}()
	}
	wg.Wait()
	_ = c.sorgu(context.Background(), "6.4.2")
	if got := atomic.LoadInt64(n); got != 1 {
		t.Fatalf("20 eşzamanlı + 1 sonraki için HTTP isteği=%d, 1 olmalı", got)
	}
}

func TestCekirdekKontrol_TTLVeHataKisaTTL(t *testing.T) {
	srv, n := apiSunucu(t, func(string) (int, string) { return 500, "x" })
	c := yeniCekirdekKontrol(srv.URL)
	simdi := time.Now()
	c.simdi = func() time.Time { return simdi }
	if g := c.sorgu(context.Background(), "6.4.2"); g.Bilinen {
		t.Fatal("hata bilinen sayıldı")
	}
	c.sorgu(context.Background(), "6.4.2")
	if atomic.LoadInt64(n) != 1 {
		t.Fatalf("hata sonucu her istekte dışarı gitti: %d", *n)
	}
	simdi = simdi.Add(c.ttlHata + time.Second)
	c.sorgu(context.Background(), "6.4.2")
	if atomic.LoadInt64(n) != 2 {
		t.Fatalf("kısa TTL sonrası yeniden denenmedi: %d", *n)
	}
}

func TestCekirdekKontrol_DisVeriDogrulanir(t *testing.T) {
	srv, _ := apiSunucu(t, func(string) (int, string) {
		return 200, `{"offers":[{"response":"upgrade","current":"<script>alert(1)</script>"}]}`
	})
	c := yeniCekirdekKontrol(srv.URL)
	if g := c.sorgu(context.Background(), "6.4.2"); g.Var || g.Hedef != "" {
		t.Fatalf("kötü hedef sürüm kabul edildi: %+v", g)
	}
}

func TestCekirdekKontrol_GuncelVeGecersizGirdi(t *testing.T) {
	srv, n := apiSunucu(t, func(string) (int, string) {
		return 200, `{"offers":[{"response":"latest","current":"7.1.2"}]}`
	})
	c := yeniCekirdekKontrol(srv.URL)
	if g := c.sorgu(context.Background(), "7.1.2"); !g.Bilinen || g.Var {
		t.Fatalf("latest: %+v", g)
	}
	// kiracı kontrollü sürüm biçimi dışı → dışarıya İSTEK GİTMEZ
	for _, s := range []string{"", "6.4.2&locale=x", "../x", "6.4.2 \n", strings.Repeat("1", 50)} {
		if g := c.sorgu(context.Background(), s); g.Bilinen {
			t.Errorf("%q bilinen", s)
		}
	}
	if atomic.LoadInt64(n) != 1 {
		t.Fatalf("geçersiz girdi dış istek üretti: %d", *n)
	}
}

func TestCekirdekKontrol_SahteSurumSelleHaritaSinirli(t *testing.T) {
	srv, n := apiSunucu(t, func(string) (int, string) {
		return 200, `{"offers":[{"response":"latest"}]}`
	})
	c := yeniCekirdekKontrol(srv.URL)
	c.azami = 5
	for i := 0; i < 50; i++ {
		c.sorgu(context.Background(), fmt.Sprintf("1.%d", i))
	}
	if len(c.m) > 5 || atomic.LoadInt64(n) > 5 {
		t.Fatalf("harita=%d istek=%d, ikisi de <=5 olmalı", len(c.m), *n)
	}
}

func TestCekirdekKontrol_ZamanAsimi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	c := yeniCekirdekKontrol(srv.URL)
	c.istemci.Timeout = 200 * time.Millisecond
	bas := time.Now()
	if g := c.sorgu(context.Background(), "6.4.2"); g.Bilinen {
		t.Fatal("zaman aşımı bilinen sayıldı")
	}
	if time.Since(bas) > 2*time.Second {
		t.Fatalf("zaman aşımı uygulanmadı: %v", time.Since(bas))
	}
}

// ---------------------------------------------------------------- TTL önbellek

func TestOnbellek_TTLVeTekilleme(t *testing.T) {
	o := yeniOnbellek(time.Minute, 16)
	simdi := time.Now()
	o.simdi = func() time.Time { return simdi }
	var n int64
	fn := func(context.Context) (any, error) {
		atomic.AddInt64(&n, 1)
		time.Sleep(20 * time.Millisecond)
		return "v", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); o.Yap(context.Background(), "k", time.Second, fn) }()
	}
	wg.Wait()
	o.Yap(context.Background(), "k", time.Second, fn)
	if n != 1 {
		t.Fatalf("fn %d kez çalıştı", n)
	}
	simdi = simdi.Add(2 * time.Minute)
	o.Yap(context.Background(), "k", time.Second, fn)
	if n != 2 {
		t.Fatalf("TTL sonrası yenilenmedi: %d", n)
	}
}

func TestOnbellek_HataOnbelleklenmez(t *testing.T) {
	o := yeniOnbellek(time.Minute, 16)
	var n int
	fn := func(context.Context) (any, error) { n++; return nil, fmt.Errorf("x") }
	o.Yap(context.Background(), "k", time.Second, fn)
	o.Yap(context.Background(), "k", time.Second, fn)
	if n != 2 {
		t.Fatalf("hata önbelleğe alındı: %d", n)
	}
}

// Mutasyon (Sil) sırasında uçuşta olan eski sonuç önbelleğe YAZILMAZ.
func TestOnbellek_SilUcustakiniGecersizKilar(t *testing.T) {
	o := yeniOnbellek(time.Minute, 16)
	basladi, birak := make(chan struct{}), make(chan struct{})
	go o.Yap(context.Background(), "a\x00k", time.Second, func(context.Context) (any, error) {
		close(basladi)
		<-birak
		return "ESKI", nil
	})
	<-basladi
	o.Sil("a\x00")
	close(birak)
	time.Sleep(50 * time.Millisecond)
	if d, ok := o.Bak("a\x00k"); ok {
		t.Fatalf("mutasyondan önce başlayan iş önbelleğe yazıldı: %v", d)
	}
}

func TestOnbellek_Sinirli(t *testing.T) {
	o := yeniOnbellek(time.Minute, 8)
	for i := 0; i < 100; i++ {
		k := fmt.Sprint(i)
		o.Yap(context.Background(), k, time.Second, func(context.Context) (any, error) { return k, nil })
	}
	if len(o.m) > 8 {
		t.Fatalf("önbellek sınırsız büyüdü: %d", len(o.m))
	}
}

// ---------------------------------------------------------------- wp-cli sınırı / liste

func sahteWPCli(t *testing.T, f func(ctx context.Context, sk string, args ...string) ([]byte, error)) {
	t.Helper()
	eski := wpStdoutFn
	wpStdoutFn = f
	t.Cleanup(func() { wpStdoutFn = eski })
}

func TestWpOku_EsZamanlilikSiniri(t *testing.T) {
	var cur, max int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		c := atomic.AddInt64(&cur, 1)
		for {
			m := atomic.LoadInt64(&max)
			if c <= m || atomic.CompareAndSwapInt64(&max, m, c) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return []byte("ok"), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); wpOku(context.Background(), "c_a", "core", "version") }()
	}
	wg.Wait()
	if max > wpEsZamanli {
		t.Fatalf("eşzamanlı wp-cli=%d, sınır %d", max, wpEsZamanli)
	}
	if max < 2 {
		t.Fatalf("paralellik yok: %d", max)
	}
}

func TestWpOku_HafifBootstrapBayraklari(t *testing.T) {
	var got []string
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	})
	wpOku(context.Background(), "c_a", "plugin", "list")
	s := strings.Join(got, " ")
	if !strings.Contains(s, "--skip-plugins") || !strings.Contains(s, "--skip-themes") {
		t.Fatalf("args=%v", got)
	}
}

func TestWpOku_SiraBeklerkenIptal(t *testing.T) {
	for i := 0; i < wpEsZamanli; i++ {
		wpOkuSem <- struct{}{}
	}
	defer func() {
		for i := 0; i < wpEsZamanli; i++ {
			<-wpOkuSem
		}
	}()
	ctx, iptal := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer iptal()
	if _, err := wpOku(ctx, "c_a", "x"); err == nil {
		t.Fatal("doluyken iptal edilmeliydi")
	}
}

func TestListeJSON_OnbellekVeMutasyonGecersiz(t *testing.T) {
	listeOnb = yeniOnbellek(time.Minute, 64)
	var n int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		atomic.AddInt64(&n, 1)
		return []byte(fmt.Sprintf(`[{"name":"p%d"}]`, atomic.LoadInt64(&n))), nil
	})
	a := listeJSON(context.Background(), "c_a", "/home/c_a/public_html", "plugin", time.Second, "plugin", "list")
	b := listeJSON(context.Background(), "c_a", "/home/c_a/public_html", "plugin", time.Second, "plugin", "list")
	if n != 1 || string(a[0]) != string(b[0]) {
		t.Fatalf("önbellek çalışmadı n=%d", n)
	}
	wpOnbellekSil("c_a", "/home/c_a/public_html")
	c := listeJSON(context.Background(), "c_a", "/home/c_a/public_html", "plugin", time.Second, "plugin", "list")
	if n != 2 || string(c[0]) == string(a[0]) {
		t.Fatalf("mutasyon sonrası bayat veri: n=%d %s", n, c[0])
	}
}

// KİRACI İZOLASYONU: aynı tür/dizin adı farklı kiracıda ASLA aynı kayıttan sunulmaz;
// bir kiracının mutasyonu diğerinin önbelleğini silmez.
func TestListeJSON_KiraciKapsami(t *testing.T) {
	listeOnb = yeniOnbellek(time.Minute, 64)
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"name":"%s"}]`, sk)), nil
	})
	dir := "/public_html" // iki kiracıda da aynı GÖRELİ ad: anahtar sk'yı içermeli
	a := listeJSON(context.Background(), "c_a", dir, "plugin", time.Second, "plugin", "list")
	b := listeJSON(context.Background(), "c_b", dir, "plugin", time.Second, "plugin", "list")
	if string(a[0]) == string(b[0]) || !strings.Contains(string(b[0]), "c_b") {
		t.Fatalf("kiracılar arası SIZINTI: a=%s b=%s", a[0], b[0])
	}
	wpOnbellekSil("c_a", dir)
	if _, ok := listeOnb.Bak(onbAnahtar("c_b", dir, "plugin")); !ok {
		t.Fatal("c_a mutasyonu c_b önbelleğini sildi")
	}
	if _, ok := listeOnb.Bak(onbAnahtar("c_a", dir, "plugin")); ok {
		t.Fatal("c_a önbelleği silinmedi")
	}
}

func TestListeJSON_HataVeBozukCiktiOnbelleklenmez(t *testing.T) {
	listeOnb = yeniOnbellek(time.Minute, 64)
	cikti := "NOT JSON <script>"
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) { return []byte(cikti), nil })
	if r := listeJSON(context.Background(), "c_a", "/d", "plugin", time.Second, "x"); len(r) != 0 {
		t.Fatalf("bozuk çıktı döndü: %v", r)
	}
	cikti = "[]"
	if r := listeJSON(context.Background(), "c_a", "/d", "plugin", time.Second, "x"); r == nil || len(r) != 0 {
		t.Fatalf("boş dizi: %v", r)
	}
}

// ---------------------------------------------------------------- Durum

func TestDurum_DBBoyutuArkaPlanVeTemizleme(t *testing.T) {
	agirOnb = yeniOnbellek(time.Minute, 64)
	phpOnb = yeniOnbellek(time.Minute, 4)
	phpCalistir = func(context.Context) ([]byte, error) { return []byte("8.3.1\n"), nil }
	srv, _ := apiSunucu(t, func(string) (int, string) {
		return 200, `{"offers":[{"response":"upgrade","current":"7.1.2"}]}`
	})
	eskiK := cekKontrol
	cekKontrol = yeniCekirdekKontrol(srv.URL)
	defer func() { cekKontrol = eskiK }()
	var dbCagri int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		switch args[0] {
		case "core": // dosya okunamayınca yedek yol
			return []byte("6.4.2\n"), nil
		case "db":
			atomic.AddInt64(&dbCagri, 1)
			time.Sleep(100 * time.Millisecond)
			return []byte("12; DROP TABLE x\n"), nil // kötücül çıktı → boş
		}
		return nil, fmt.Errorf("beklenmeyen %v", args)
	})
	bas := time.Now()
	d := durumOlustur(context.Background(), "c_a", "/nowhere/x")
	if gecen := time.Since(bas); gecen > 80*time.Millisecond+500*time.Millisecond {
		t.Fatalf("durum ağır işi bekledi: %v", gecen)
	}
	if d["yukleniyor"] != true || d["db_mb"] != "" || d["surum"] != "6.4.2" ||
		d["guncelleme_var"] != true || d["hedef_surum"] != "7.1.2" || d["php"] != "8.3.1" {
		t.Fatalf("ilk yanıt: %v", d)
	}
	time.Sleep(300 * time.Millisecond)
	d = durumOlustur(context.Background(), "c_a", "/nowhere/x")
	if d["yukleniyor"] != false {
		t.Fatalf("hazır olmalıydı: %v", d)
	}
	if d["db_mb"] != "" { // 12; DROP… sayı değil → reddedildi ("" ama yukleniyor=false)
		t.Fatalf("kötücül db çıktısı kabul edildi: %v", d["db_mb"])
	}
	if dbCagri != 1 {
		t.Fatalf("db size %d kez çalıştı", dbCagri)
	}
}

func TestDBBoyutuTemiz(t *testing.T) {
	for s, w := range map[string]string{"12\n": "12", "0.5": "0.5", "12 MB": "", "1;2": "", "": "", "<b>": "", "-3": ""} {
		if g := dbBoyutuTemiz(s); g != w {
			t.Errorf("%q -> %q (beklenen %q)", s, g, w)
		}
	}
}

// ---------------------------------------------------------------- Tüm kurulumlar satırı

func TestIncele_DurumHaritasi(t *testing.T) {
	h := &Handlers{}
	a := wpAday{domainID: 7, sk: "c_a", alanAdi: "a.test", ssl: true, dir: "/home/c_a/public_html/blog", root: "/home/c_a/public_html"}
	k := h.incele(a, "6.4.2", cekGunc{Bilinen: true, Var: true, Hedef: "7.1.2"})
	if k.Durum != "eski" || k.SonSurum != "7.1.2" || k.Dizin != "/blog" || k.SiteURL != "https://a.test/blog" || k.Surum != "6.4.2" {
		t.Fatalf("%+v", k)
	}
	if k := h.incele(a, "7.1.2", cekGunc{Bilinen: true}); k.Durum != "guncel" {
		t.Fatalf("%+v", k)
	}
	if k := h.incele(a, "7.1.2", cekGunc{}); k.Durum != "bilinmiyor" {
		t.Fatalf("%+v", k)
	}
	if k := h.incele(a, "", cekGunc{Bilinen: true}); k.Durum != "bilinmiyor" {
		t.Fatalf("sürümsüz: %+v", k)
	}
}
