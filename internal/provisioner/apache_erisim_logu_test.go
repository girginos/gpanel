package provisioner

// apache_erisim_logu_test.go — Apache backend vhost'larında erişim günlüğü /dev/null'a gider.
//
// 🔴 `CustomLog` satırı HİÇ olmasa Apache ana sunucunun `CustomLog "logs/access_log" combined` satırını
// miras alır (tüm siteler tek dosyaya yazar) — günlük kapanmaz. Şablon bu yüzden satırı /dev/null ile yazar;
// kurulu vhost'lar açılışta aynı biçime çevrilir.
//
// Vhost dizini geçici, httpd/systemctl sahte — ana makinenin Apache'sine dokunulmaz.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ap_vhost(t *testing.T, alan string) string {
	t.Helper()
	var buf bytes.Buffer
	o := VhostOpts{AlanAdi: alan, WebRoot: "/home/c_x/public_html", PHPSocket: "/run/php-fpm-c_x/php-fpm.sock"}
	if err := apacheVhostTmpl.Execute(&buf, o); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// ap_eski: düzeltme öncesi şablonun çıktısı — yeni çıktıda yalnız bu satır farklıydı.
func ap_eski(alan, yeni string) string {
	return strings.Replace(yeni, "CustomLog "+apacheErisimLoguHedefi+" combined", "CustomLog /var/log/httpd/"+alan+".access.log combined", 1)
}

func TestApacheVhostSablonu_ErisimLoguDevNull(t *testing.T) {
	v := ap_vhost(t, "ornek.com")
	if !strings.Contains(v, "\n    CustomLog /dev/null combined\n") {
		t.Fatalf("erişim günlüğü /dev/null'a yazılmalı:\n%s", v)
	}
	if strings.Contains(v, ".access.log") {
		t.Fatalf("şablonda per-domain erişim günlüğü kalmamalı:\n%s", v)
	}
	if n := strings.Count(v, "CustomLog"); n != 1 {
		t.Fatalf("vhost'ta tam bir CustomLog olmalı (yoksa ana sunucununki miras alınır): %d", n)
	}
	if !strings.Contains(v, "\n    ErrorLog /var/log/httpd/ornek.com.error.log\n") {
		t.Fatalf("hata günlüğü korunmalı:\n%s", v)
	}
	if !strings.Contains(v, apacheVhostBaslikIzi) {
		t.Fatalf("şablon başlığı %q içermeli (süpürme panel dosyalarını bununla tanır)", apacheVhostBaslikIzi)
	}
}

func TestApacheErisimLoguYama(t *testing.T) {
	yeni := ap_vhost(t, "ornek.com")
	eski := ap_eski("ornek.com", yeni)
	if eski == yeni {
		t.Fatal("test düzeneği: eski ve yeni biçim farklı olmalı")
	}
	got, degisti := apacheErisimLoguYama(eski)
	if !degisti || got != yeni {
		t.Fatalf("eski vhost yeni şablonla BAYT BAYT aynı olmalı (değişti=%v):\n%s", degisti, got)
	}
	if g2, d2 := apacheErisimLoguYama(yeni); d2 || g2 != yeni {
		t.Fatalf("yeni biçime dokunulmamalı (değişti=%v)", d2)
	}

	// yalnız CustomLog satırını değiştiren varyantlar (ErrorLog satırı da /var/log/httpd/ içerdiği için hedefli değişim)
	satir := func(yeniSatir string) string {
		return strings.Replace(eski, "CustomLog /var/log/httpd/ornek.com.access.log combined", yeniSatir, 1)
	}
	dokunulmaz := map[string]string{
		"panel başlığı yok":           strings.Replace(eski, apacheVhostBaslikIzi, "el yapimi vhost", 1),
		"başka format":                satir("CustomLog /var/log/httpd/ornek.com.access.log common"),
		"başka dizin":                 satir("CustomLog /var/log/ozel/ornek.com.access.log combined"),
		"ana sunucu günlüğü":          satir("CustomLog logs/access_log combined"),
		"yorum satırı":                satir("#CustomLog /var/log/httpd/ornek.com.access.log combined"),
		"günlük adı access.log değil": satir("CustomLog /var/log/httpd/ornek.com.trafik.log combined"),
		"yol parçası içeren ad":       satir("CustomLog /var/log/httpd/../../tmp/x.access.log combined"),
		"satır sonunda başka argüman": satir("CustomLog /var/log/httpd/ornek.com.access.log combined env=!dontlog"),
	}
	for ad, g := range dokunulmaz {
		if g == eski {
			t.Fatalf("test düzeneği (%s): değişiklik uygulanamadı", ad)
		}
		if got, degisti := apacheErisimLoguYama(g); degisti || got != g {
			t.Errorf("%s: dokunulmamalı (değişti=%v):\n%s", ad, degisti, got)
		}
	}

	// girinti (sekme) korunur; birden çok satır hepsi çevrilir
	tab := strings.Replace(eski, "    CustomLog /var/log", "\tCustomLog /var/log", 1)
	if got, ok := apacheErisimLoguYama(tab); !ok || !strings.Contains(got, "\n\tCustomLog /dev/null combined\n") {
		t.Fatalf("girinti korunmalı:\n%s", got)
	}
	cift := strings.Replace(eski, "</VirtualHost>", "    CustomLog /var/log/httpd/diger.access.log combined\n</VirtualHost>", 1)
	if got, ok := apacheErisimLoguYama(cift); !ok || strings.Contains(got, ".access.log") || strings.Count(got, "CustomLog /dev/null combined") != 2 {
		t.Fatalf("her eski satır çevrilmeli:\n%s", got)
	}
}

// ---- açılış süpürmesi ----

type ap_ortam struct {
	dizin       string
	komutlar    *[]string
	httpdTHata  error
	aktif       bool
	reloadHata  error
	httpdTKanca func() // httpd -t anında çalışır (eşzamanlı yazıcı simülasyonu)
}

func ap_dikis(t *testing.T) *ap_ortam {
	t.Helper()
	e1, e2 := apacheVhostDizini, apacheKomut
	t.Cleanup(func() { apacheVhostDizini, apacheKomut = e1, e2 })
	o := &ap_ortam{dizin: t.TempDir(), komutlar: &[]string{}, aktif: true}
	apacheVhostDizini = o.dizin
	apacheKomut = func(ad string, arg ...string) ([]byte, error) {
		k := ad + " " + strings.Join(arg, " ")
		*o.komutlar = append(*o.komutlar, k)
		switch k {
		case "httpd -t":
			if o.httpdTKanca != nil {
				o.httpdTKanca()
			}
			if o.httpdTHata != nil {
				return []byte("AH00526: Syntax error on line 9"), o.httpdTHata
			}
		case "systemctl is-active --quiet httpd":
			if !o.aktif {
				return nil, errors.New("exit status 3")
			}
		case "systemctl reload httpd":
			if o.reloadHata != nil {
				return []byte("Job for httpd.service failed"), o.reloadHata
			}
		}
		return nil, nil
	}
	return o
}

func (o *ap_ortam) yaz(t *testing.T, ad, govde string) string {
	t.Helper()
	yol := filepath.Join(o.dizin, ad)
	ha_yaz(t, yol, govde)
	return yol
}

func (o *ap_ortam) komutMetni() string { return strings.Join(*o.komutlar, "\n") }

// ap_kur: iki eski panel vhost'u + dokunulmaması gerekenler.
func ap_kur(t *testing.T, o *ap_ortam) (eskiA, eskiB, yeniC string, dokunulmaz map[string]string) {
	t.Helper()
	yeniC = ap_vhost(t, "c.com")
	eskiA, eskiB = ap_eski("a.com", ap_vhost(t, "a.com")), ap_eski("b.com", ap_vhost(t, "b.com"))
	o.yaz(t, "dom_c_a.conf", eskiA)
	o.yaz(t, "dom_c_b.conf", eskiB)
	o.yaz(t, "dom_c_c.conf", yeniC)
	if err := os.Chmod(filepath.Join(o.dizin, "dom_c_a.conf"), 0o640); err != nil {
		t.Fatal(err)
	}
	dokunulmaz = map[string]string{
		"dom_c_a.conf.bak.1787613757": eskiA,                                                              // yedek kopya: Apache yüklemez
		"dom_c_y.conf":                strings.Replace(eskiA, apacheVhostBaslikIzi, "el yapimi vhost", 1), // panel başlığı yok
		"dom_foo.conf":                eskiA,                                                              // hesap vhost'u değil (c_ yok)
		"sub_c_x.conf":                eskiA,                                                              // alt alan vhost'u: kapsam dışı
		"00-gosp-default.conf":        "<VirtualHost 127.0.0.1:10080>\n    ErrorLog /var/log/httpd/gosp-default.error.log\n</VirtualHost>\n",
	}
	for ad, g := range dokunulmaz {
		o.yaz(t, ad, g)
	}
	return
}

func TestApacheErisimLoguSupur_YamalarDogrularYeniler(t *testing.T) {
	o := ap_dikis(t)
	eskiA, eskiB, yeniC, dokunulmaz := ap_kur(t, o)
	_ = eskiB

	oz := apacheErisimLoguSupur()
	if oz.Yamalanan != 2 || oz.Hata != 0 || oz.GeriAlindi || !oz.Yenilendi {
		t.Fatalf("özet: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got != ap_vhost(t, "a.com") {
		t.Fatalf("dom_c_a.conf yeni şablonla aynı olmalı:\n%s", got)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_b.conf")); got != ap_vhost(t, "b.com") {
		t.Fatalf("dom_c_b.conf yeni şablonla aynı olmalı:\n%s", got)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_c.conf")); got != yeniC {
		t.Fatal("zaten yeni olan dosyaya dokunulmamalı")
	}
	if st, err := os.Stat(filepath.Join(o.dizin, "dom_c_a.conf")); err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("dosya kipi korunmalı (0640): %v %v", st, err)
	}
	for ad, g := range dokunulmaz {
		if got := ha_oku(t, filepath.Join(o.dizin, ad)); got != g {
			t.Errorf("%s'e dokunulmamalıydı:\n%s", ad, got)
		}
	}
	_ = eskiA
	// önce doğrulama, sonra çalışıyorsa graceful reload; geçici dosya artığı yok
	want := "httpd -t\nsystemctl is-active --quiet httpd\nsystemctl reload httpd"
	if got := o.komutMetni(); got != want {
		t.Fatalf("komut dizisi:\n%s\n--- beklenen ---\n%s", got, want)
	}
	if kalinti, _ := filepath.Glob(filepath.Join(o.dizin, "*.tmp")); len(kalinti) != 0 {
		t.Fatalf("geçici dosya artığı: %v", kalinti)
	}

	// idempotent: ikinci tur hiçbir şey yapmaz, hiçbir komut çalıştırmaz
	*o.komutlar = nil
	if oz2 := apacheErisimLoguSupur(); oz2 != (apacheLogOzet{}) {
		t.Fatalf("ikinci tur sıfır özet vermeli: %+v", oz2)
	}
	if len(*o.komutlar) != 0 {
		t.Fatalf("ikinci turda komut çalışmamalı: %v", *o.komutlar)
	}
}

func TestApacheErisimLoguSupur_HttpdTBasarisizsaHepsiGeriAlinir(t *testing.T) {
	o := ap_dikis(t)
	eskiA, eskiB, _, _ := ap_kur(t, o)
	o.httpdTHata = errors.New("exit status 1")

	oz := apacheErisimLoguSupur()
	if oz.Yamalanan != 0 || !oz.GeriAlindi || oz.Yenilendi {
		t.Fatalf("özet: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got != eskiA {
		t.Fatalf("dom_c_a.conf geri alınmalı:\n%s", got)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_b.conf")); got != eskiB {
		t.Fatalf("dom_c_b.conf geri alınmalı:\n%s", got)
	}
	if got := o.komutMetni(); got != "httpd -t" {
		t.Fatalf("doğrulama başarısızken reload denenmemeli: %q", got)
	}
}

// httpd -t sırasında başka bir yazıcı (vhost render'ı) dosyayı değiştirdiyse geri alma ONUN içeriğini ezmez.
func TestApacheErisimLoguSupur_GeriAlmaBaskaYaziciyiEzmez(t *testing.T) {
	o := ap_dikis(t)
	eskiA, _, _, _ := ap_kur(t, o)
	baska := ap_vhost(t, "b.com") + "# render yeni yazdi\n"
	o.httpdTHata = errors.New("exit status 1")
	o.httpdTKanca = func() { ha_yaz(t, filepath.Join(o.dizin, "dom_c_b.conf"), baska) }

	oz := apacheErisimLoguSupur()
	if !oz.GeriAlindi {
		t.Fatalf("özet: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got != eskiA {
		t.Fatalf("dokunulmayan dosya geri alınmalı:\n%s", got)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_b.conf")); got != baska {
		t.Fatalf("başka yazıcının içeriği ezildi:\n%s", got)
	}
}

func TestApacheErisimLoguSupur_CalismayanApacheBaslatilmaz(t *testing.T) {
	o := ap_dikis(t)
	ap_kur(t, o)
	o.aktif = false
	oz := apacheErisimLoguSupur()
	if oz.Yamalanan != 2 || oz.Yenilendi || oz.GeriAlindi {
		t.Fatalf("özet: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got != ap_vhost(t, "a.com") {
		t.Fatal("dosya yine yamalanmalı (sonraki başlatma yeni halini okur)")
	}
	if got := o.komutMetni(); got != "httpd -t\nsystemctl is-active --quiet httpd" {
		t.Fatalf("durmuş Apache'ye reload/start verilmemeli: %q", got)
	}
}

func TestApacheErisimLoguSupur_ReloadHatasiYamayiGeriAlmaz(t *testing.T) {
	o := ap_dikis(t)
	ap_kur(t, o)
	o.reloadHata = errors.New("exit status 1")
	oz := apacheErisimLoguSupur()
	if oz.Yamalanan != 2 || oz.Yenilendi || oz.GeriAlindi {
		t.Fatalf("özet: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got != ap_vhost(t, "a.com") {
		t.Fatal("doğrulanmış yama reload hatasında geri alınmamalı")
	}
}

func TestApacheErisimLoguSupur_SymlinkVeBosDizin(t *testing.T) {
	o := ap_dikis(t)
	if oz := apacheErisimLoguSupur(); oz != (apacheLogOzet{}) || len(*o.komutlar) != 0 {
		t.Fatalf("vhost yoksa hiçbir şey yapılmamalı: %+v %v", oz, *o.komutlar)
	}
	hedef := filepath.Join(t.TempDir(), "hedef.conf")
	eski := ap_eski("x.com", ap_vhost(t, "x.com"))
	ha_yaz(t, hedef, eski)
	bag := filepath.Join(o.dizin, "dom_c_link.conf")
	if err := os.Symlink(hedef, bag); err != nil {
		t.Fatal(err)
	}
	if oz := apacheErisimLoguSupur(); oz != (apacheLogOzet{}) {
		t.Fatalf("symlink vhost'a dokunulmamalı: %+v", oz)
	}
	if got := ha_oku(t, hedef); got != eski {
		t.Fatal("symlink hedefi değişmemeli")
	}
	if st, err := os.Lstat(bag); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink olduğu gibi kalmalı")
	}
}

// Süpürme vhost render kilidini alır: kilit tutulurken ilerlemez, bırakılınca biter.
func TestApacheErisimLoguSupur_RenderKilidiyleSiralanir(t *testing.T) {
	o := ap_dikis(t)
	ap_kur(t, o)
	renderKilidi.Lock()
	bitti := make(chan apacheLogOzet, 1)
	go func() { bitti <- apacheErisimLoguSupur() }()
	select {
	case <-bitti:
		renderKilidi.Unlock()
		t.Fatal("render kilidi tutulurken süpürme ilerlememeli")
	case <-time.After(150 * time.Millisecond):
	}
	if got := ha_oku(t, filepath.Join(o.dizin, "dom_c_a.conf")); got == ap_vhost(t, "a.com") {
		renderKilidi.Unlock()
		t.Fatal("kilit tutulurken dosya yazılmamalı")
	}
	renderKilidi.Unlock()
	select {
	case oz := <-bitti:
		if oz.Yamalanan != 2 {
			t.Fatalf("özet: %+v", oz)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kilit bırakılınca süpürme bitmeli")
	}
}

// ---- bağlantılar (AST) ----

func TestBaglanti_Init_ApacheErisimLogu_CagrilirArkaplandaVeKorumali(t *testing.T) {
	if n := len(la_cagrilar(la_govde(t, "provisioner.go", "Init"), "ApacheErisimLoguKapatOnStartup", "")); n != 1 {
		t.Fatalf("Init'te ApacheErisimLoguKapatOnStartup %d kez çağrılıyor; 1 olmalı", n)
	}
	var goVar, recoverVar bool
	ast_dolas(la_govde(t, "apache_erisim_logu.go", "ApacheErisimLoguKapatOnStartup"), &goVar, &recoverVar)
	if !goVar || !recoverVar {
		t.Fatalf("süpürme Init'i bloklamamalı (goroutine=%v) ve çökmeye karşı korumalı olmalı (recover=%v)", goVar, recoverVar)
	}
}
