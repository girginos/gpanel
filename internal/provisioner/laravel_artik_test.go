package provisioner

// laravel_artik_test.go — Laravel Toolkit artıkları (bkz. laravel_artik.go): StartLimit
// [Service]→[Unit] yaması, silinen hesabın işleyici birimi / zamanlanmış görevi, açılış süpürmesi.
//
// Birim ve cron dizinleri geçici, systemctl ve kullanıcı sorgusu sahte — ana makinenin systemd'sine,
// /etc'sine ve passwd'ine dokunulmaz. (Birim dizini + systemctl sahtesi hb_dikis'ten gelir.)

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type la_ortam struct {
	hb_ortam
	cron, aski string
	mevcut     map[string]bool // sistemde var olan kullanıcılar
}

func la_dikis(t *testing.T) la_ortam {
	t.Helper()
	o := la_ortam{hb_ortam: hb_dikis(t), cron: t.TempDir(), aski: t.TempDir(), mevcut: map[string]bool{}}
	e1, e2, e3 := laravelCronDizini, laravelCronAskiDizini, laravelKullaniciBul
	t.Cleanup(func() { laravelCronDizini, laravelCronAskiDizini, laravelKullaniciBul = e1, e2, e3 })
	laravelCronDizini, laravelCronAskiDizini = o.cron, o.aski
	laravelKullaniciBul = func(ad string) (*user.User, error) {
		switch {
		case ad == "c_bozuk": // passwd okunamadı gibi "belirsiz" hata: UnknownUserError DEĞİL
			return nil, errors.New("open /etc/passwd: permission denied")
		case o.mevcut[ad]:
			return &user.User{Username: ad}, nil
		}
		return nil, user.UnknownUserError(ad)
	}
	return o
}

func (o la_ortam) komutMetni() string { return strings.Join(*o.komutlar, "\n") }

// la_birim: laravel.queueUnit'in çıktısıyla aynı iskelet. eski=true → StartLimit satırları [Service]'te
// (düzeltme öncesi biçim), false → [Unit]'te.
func la_birim(sk string, eski bool) string {
	sl := "StartLimitIntervalSec=300\nStartLimitBurst=10\n"
	unit, servis := "", ""
	if eski {
		servis = sl
	} else {
		unit = sl
	}
	return "[Unit]\nDescription=Laravel kuyruk işleyici (domain 7)\nAfter=network.target mariadb.service\n" + unit + "\n" +
		"[Service]\nType=simple\nUser=" + sk + "\nGroup=" + sk + "\nWorkingDirectory=/home/" + sk + "/app\n" +
		"ExecStart=/usr/bin/php /home/" + sk + "/app/artisan queue:work database --sleep=3 --tries=3 --timeout=60 --max-jobs=1000 --max-time=3600\n" +
		"Restart=always\nRestartSec=5\n" + servis + "Slice=girginos-" + sk + ".slice\nNoNewPrivileges=yes\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
}

// la_cron: laravel.Schedule'ın yazdığı cron.d gövdesi.
func la_cron(sk string) string {
	return "# girginos Laravel Toolkit — zamanlanmış görev (domain 5)\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin\n" +
		"* * * * * " + sk + " /usr/bin/php /home/" + sk + "/app/artisan schedule:run >> /home/" + sk + "/logs/laravel-schedule.log 2>&1\n"
}

func la_var(yol string) bool { _, err := os.Lstat(yol); return err == nil }

// ---- saf yama ----

func TestLaravelBirimYamala_EskiBicimYeniBicimeDonusur(t *testing.T) {
	eski, yeni := la_birim("c_a", true), la_birim("c_a", false)
	if eski == yeni {
		t.Fatal("test düzeneği: eski ve yeni biçim farklı olmalı")
	}
	got, degisti := laravelBirimYamala(eski)
	if !degisti || got != yeni {
		t.Fatalf("eski biçim yeni biçime dönmeli (değişti=%v):\n%s", degisti, got)
	}
	// idempotent: yamalı gövdede ikinci çağrı değişiklik yapmaz
	if g2, d2 := laravelBirimYamala(got); d2 || g2 != got {
		t.Fatalf("ikinci çağrı değişiklik yapmamalı (değişti=%v)", d2)
	}
	// yeni biçime hiç dokunulmaz
	if g3, d3 := laravelBirimYamala(yeni); d3 || g3 != yeni {
		t.Fatalf("yeni biçim aynen kalmalı (değişti=%v)", d3)
	}
	// [Service]'te hiç StartLimit kalmamalı, [Unit]'te tam bir kez olmalı
	if strings.Count(got, "StartLimitIntervalSec=") != 1 || strings.Count(got, "StartLimitBurst=") != 1 {
		t.Fatalf("her anahtar tam bir kez olmalı:\n%s", got)
	}
	if bas, _, ok := strings.Cut(got, "[Service]"); !ok || !strings.Contains(bas, "StartLimitBurst=10") {
		t.Fatalf("StartLimit [Service]'ten ÖNCE ([Unit]'te) olmalı:\n%s", got)
	}
}

func TestLaravelBirimYamala_Kenarlar(t *testing.T) {
	tablo := []struct {
		ad, girdi, beklenen string
		degisir             bool
	}{
		{"[Unit] yok → dokunulmaz", "[Service]\nUser=c_a\nStartLimitBurst=10\n", "", false},
		{"[Service]'te StartLimit yok → dokunulmaz", "[Unit]\nDescription=x\n\n[Service]\nUser=c_a\n", "", false},
		{"değerler ve boşluklar aynen taşınır",
			"[Unit]\nDescription=x\n\n[Service]\nUser=c_a\nStartLimitIntervalSec = 60\nStartLimitBurst=3\nRestart=always\n",
			"[Unit]\nDescription=x\nStartLimitIntervalSec = 60\nStartLimitBurst=3\n\n[Service]\nUser=c_a\nRestart=always\n", true},
		{"[Unit]'te zaten var: [Service] kopyası silinir, [Unit] değeri korunur",
			"[Unit]\nDescription=x\nStartLimitBurst=20\n\n[Service]\nUser=c_a\nStartLimitBurst=10\nStartLimitIntervalSec=300\n",
			"[Unit]\nDescription=x\nStartLimitBurst=20\nStartLimitIntervalSec=300\n\n[Service]\nUser=c_a\n", true},
		{"[Install]'daki satıra dokunulmaz", "[Unit]\nDescription=x\n\n[Service]\nUser=c_a\n\n[Install]\nStartLimitBurst=1\n", "", false},
		{"yorum satırı taşınmaz", "[Unit]\nDescription=x\n\n[Service]\nUser=c_a\n# StartLimitBurst=10\n", "", false},
		{"yalnız başlıklı [Unit]", "[Unit]\n[Service]\nUser=c_a\nStartLimitBurst=10\n", "[Unit]\nStartLimitBurst=10\n[Service]\nUser=c_a\n", true},
		{"sondaki satır sonu yoksa eklenmez",
			"[Unit]\nDescription=x\n\n[Service]\nStartLimitBurst=10", "[Unit]\nDescription=x\nStartLimitBurst=10\n\n[Service]", true},
		{"[Unit] sonunda yorum varsa satırlar ondan sonra gelir",
			"[Unit]\nDescription=x\n# not\n\n[Service]\nStartLimitBurst=10\n", "[Unit]\nDescription=x\n# not\nStartLimitBurst=10\n\n[Service]\n", true},
	}
	for _, c := range tablo {
		got, degisti := laravelBirimYamala(c.girdi)
		want := c.beklenen
		if !c.degisir {
			want = c.girdi
		}
		if degisti != c.degisir || got != want {
			t.Errorf("%s:\nbeklenen (değişir=%v):\n%q\ngelen (değişti=%v):\n%q", c.ad, c.degisir, want, degisti, got)
		}
	}
}

func TestLaravelCronKullanicilari(t *testing.T) {
	tablo := []struct {
		ad, govde string
		beklenen  []string
	}{
		{"panelin yazdığı gövde", la_cron("c_a"), []string{"c_a"}},
		{"yorum, ortam değişkeni ve boş satırlar atlanır", "# x\n\nMAILTO=\"\"\nSHELL = /bin/sh\n", nil},
		{"@reboot kısaltmasında kullanıcı 2. alan", "@reboot c_b /bin/true\n", []string{"c_b"}},
		{"eksik alanlı satır sayılmaz", "* * * * * c_c\n", nil},
		{"birden çok satır", "* * * * * c_a x\n*/5 * * * * c_b y\n", []string{"c_a", "c_b"}},
		{"boş gövde", "", nil},
	}
	for _, c := range tablo {
		got := laravelCronKullanicilari(c.govde)
		if strings.Join(got, ",") != strings.Join(c.beklenen, ",") {
			t.Errorf("%s: beklenen %v, gelen %v", c.ad, c.beklenen, got)
		}
	}
}

func TestLaravelKullaniciYok_YalnizKesinYokSayilir(t *testing.T) {
	o := la_dikis(t)
	o.mevcut["c_var"] = true
	if laravelKullaniciYok("c_var") {
		t.Fatal("var olan kullanıcı 'yok' sayılmamalı")
	}
	if !laravelKullaniciYok("c_gitti") {
		t.Fatal("UnknownUserError → kullanıcı yok")
	}
	if laravelKullaniciYok("c_bozuk") {
		t.Fatal("passwd okunamadı gibi belirsiz hata 'yok' sayılmamalı (canlı birim silinirdi)")
	}
}

// ---- açılış süpürmesi: birimler ----

func TestLaravelArtiklariniSupur_Birimler(t *testing.T) {
	o := la_dikis(t)
	o.mevcut["c_var"] = true
	diger := t.TempDir()
	dis := filepath.Join(diger, "dis.service")
	ha_yaz(t, dis, la_birim("c_gitti", false))

	yetim := "girginos-laravel-queue-1.service"
	o.birim(t, yetim, la_birim("c_gitti", true))
	if err := os.MkdirAll(filepath.Join(o.dizin, yetim+".d"), 0o755); err != nil {
		t.Fatal(err)
	}
	ha_yaz(t, filepath.Join(o.dizin, yetim+".d", birimAskiDropIn), "[Unit]\nConditionPathExists=!/yok\n")

	eskiVar := "girginos-laravel-queue-2.service" // kullanıcı var, eski biçim → yamalanır
	o.birim(t, eskiVar, la_birim("c_var", true))
	if err := os.Chmod(filepath.Join(o.dizin, eskiVar), 0o600); err != nil {
		t.Fatal(err)
	}
	yeniVar := "girginos-laravel-queue-3.service" // kullanıcı var, yeni biçim → aynen
	o.birim(t, yeniVar, la_birim("c_var", false))

	dokunulmaz := map[string]string{
		"girginos-laravel-queue-4.service":     la_birim("nginx_yok", true),                         // c_ önekli değil
		"gosp-app-c_gitti-web.service":         la_birim("c_gitti", true),                           // başka ad alanı (Uygulama Çalıştırıcı)
		"girginos-laravel-queue-x.service":     la_birim("c_gitti", true),                           // id sayı değil
		"girginos-laravel-queue-5.service":     "[Unit]\nDescription=x\n\n[Service]\nType=simple\n", // User= yok
		"girginos-laravel-queue-7.service.bak": la_birim("c_gitti", true),                           // yedek kopya
		"girginos-laravel-queue-8.service":     la_birim("c_B-gecersiz", true),                      // geçersiz hesap adı biçimi
	}
	for ad, g := range dokunulmaz {
		o.birim(t, ad, g)
	}
	// symlink: hedefe dokunulmaz, bağ da silinmez
	bag := filepath.Join(o.dizin, "girginos-laravel-queue-9.service")
	if err := os.Symlink(dis, bag); err != nil {
		t.Fatal(err)
	}

	// kullanıcı sorgusu belirsiz hata verdi (passwd okunamadı): birim SİLİNMEZ; yama zararsız olduğundan uygulanır
	belirsiz := "girginos-laravel-queue-6.service"
	o.birim(t, belirsiz, la_birim("c_bozuk", true))

	oz := laravelArtiklariniSupur()
	if oz.YetimBirim != 1 || oz.Yamalanan != 2 || oz.YetimCron != 0 || oz.Hata != 0 {
		t.Fatalf("özet beklenenden farklı: %+v", oz)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, belirsiz)); got != la_birim("c_bozuk", false) {
		t.Fatalf("kullanıcısı belirsiz birim silinmemeli (yalnız yamalanır):\n%s", got)
	}
	// yetim: birim dosyası + drop-in dizini gitti
	ha_yok(t, filepath.Join(o.dizin, yetim))
	ha_yok(t, filepath.Join(o.dizin, yetim+".d"))
	// eski biçimli ama kullanıcısı var olan: yamalandı, SİLİNMEDİ, kipi korundu
	if got := ha_oku(t, filepath.Join(o.dizin, eskiVar)); got != la_birim("c_var", false) {
		t.Fatalf("eski biçim yamalanmalıydı:\n%s", got)
	}
	if st, err := os.Stat(filepath.Join(o.dizin, eskiVar)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("dosya kipi korunmalı (0600): %v %v", st, err)
	}
	if got := ha_oku(t, filepath.Join(o.dizin, yeniVar)); got != la_birim("c_var", false) {
		t.Fatalf("yeni biçim aynen kalmalı:\n%s", got)
	}
	for ad, g := range dokunulmaz {
		if got := ha_oku(t, filepath.Join(o.dizin, ad)); got != g {
			t.Errorf("%s'e dokunulmamalıydı:\n%s", ad, got)
		}
	}
	if !la_var(bag) || !la_var(dis) {
		t.Fatal("symlink birime ve hedefine dokunulmamalı")
	}
	// komutlar: yalnız yetim birim durdurulur; tek daemon-reload; yalnız yetim için reset-failed
	want := "systemctl disable --now " + yetim + "\nsystemctl daemon-reload\nsystemctl reset-failed " + yetim
	if got := o.komutMetni(); got != want {
		t.Fatalf("komut dizisi beklenenden farklı:\n%s\n--- beklenen ---\n%s", got, want)
	}

	// idempotent: ikinci tur hiçbir şey yapmaz, hiçbir komut çalıştırmaz
	*o.komutlar = nil
	if oz2 := laravelArtiklariniSupur(); oz2 != (laravelArtikOzet{}) {
		t.Fatalf("ikinci tur sıfır özet vermeli: %+v", oz2)
	}
	if len(*o.komutlar) != 0 {
		t.Fatalf("ikinci turda komut çalışmamalı: %v", *o.komutlar)
	}
}

// Yalnız yamalanan birim varsa (yetim yok) tek daemon-reload yeter; reset-failed yok, birim durdurulmaz.
func TestLaravelArtiklariniSupur_YalnizYamaOlunca_DurdurmaYok(t *testing.T) {
	o := la_dikis(t)
	o.mevcut["c_var"] = true
	o.birim(t, "girginos-laravel-queue-2.service", la_birim("c_var", true))
	if oz := laravelArtiklariniSupur(); oz.Yamalanan != 1 || oz.YetimBirim != 0 || oz.Hata != 0 {
		t.Fatalf("özet: %+v", oz)
	}
	if got := o.komutMetni(); got != "systemctl daemon-reload" {
		t.Fatalf("yalnız daemon-reload beklenir (çalışan işleyici yeniden BAŞLATILMAZ): %q", got)
	}
}

// Durdurma komutu hata verse de (birim yüklenmemiş/zaten durmuş) yetim birim dosyası silinir.
func TestLaravelArtiklariniSupur_DurdurmaHatasiDosyayiKorumaz(t *testing.T) {
	o := la_dikis(t)
	ad := "girginos-laravel-queue-1.service"
	o.birim(t, ad, la_birim("c_gitti", true))
	prev := askiKomut
	askiKomut = func(a string, arg ...string) ([]byte, error) {
		*o.komutlar = append(*o.komutlar, a+" "+strings.Join(arg, " "))
		if len(arg) > 0 && arg[0] == "disable" {
			return []byte("Failed to disable unit: Unit file does not exist"), errors.New("exit status 1")
		}
		return nil, nil
	}
	t.Cleanup(func() { askiKomut = prev })
	if oz := laravelArtiklariniSupur(); oz.YetimBirim != 1 || oz.Hata != 0 {
		t.Fatalf("özet: %+v", oz)
	}
	ha_yok(t, filepath.Join(o.dizin, ad))
}

// Birim dizini hiç yoksa (systemd'siz ortam) sessizce geçer.
func TestLaravelArtiklariniSupur_DizinYoksa_Sessiz(t *testing.T) {
	o := la_dikis(t)
	hesapBirimDizini = filepath.Join(o.dizin, "yok")
	if oz := laravelArtiklariniSupur(); oz != (laravelArtikOzet{}) {
		t.Fatalf("özet sıfır olmalı: %+v", oz)
	}
}

// ---- açılış süpürmesi: zamanlanmış görev dosyaları ----

func TestLaravelArtiklariniSupur_Cron(t *testing.T) {
	o := la_dikis(t)
	o.mevcut["c_var"] = true

	yetimEtkin := filepath.Join(o.cron, "girginos-laravel-5")
	yetimAski := filepath.Join(o.aski, "girginos-laravel-6")
	ha_yaz(t, yetimEtkin, la_cron("c_gitti"))
	ha_yaz(t, yetimAski, la_cron("c_gitti"))

	kalanlar := map[string]string{
		filepath.Join(o.cron, "girginos-laravel-7"):       la_cron("c_var"),                                   // kullanıcı var
		filepath.Join(o.cron, "girginos-laravel-8"):       la_cron("c_gitti") + "* * * * * c_var /bin/true\n", // karışık: biri var
		filepath.Join(o.cron, "girginos-laravel-9"):       la_cron("c_bozuk"),                                 // belirsiz
		filepath.Join(o.cron, "girginos-laravel-10"):      la_cron("root"),                                    // c_ önekli değil
		filepath.Join(o.cron, "girginos-laravel-x"):       la_cron("c_gitti"),                                 // id sayı değil
		filepath.Join(o.cron, "girginos-laravel-queue-1"): la_cron("c_gitti"),                                 // başka ad
		filepath.Join(o.cron, "girginos-laravel-5.bak"):   la_cron("c_gitti"),                                 // yedek/editör artığı: sonek var
		filepath.Join(o.cron, "girginos-laravel-5~"):      la_cron("c_gitti"),
		filepath.Join(o.cron, "diger-is"):                 la_cron("c_gitti"),            // panelin değil
		filepath.Join(o.cron, "girginos-laravel-11"):      "# yalnız yorum\nPATH=/bin\n", // görev satırı yok
		filepath.Join(o.aski, "girginos-laravel-12"):      la_cron("c_var"),
	}
	for y, g := range kalanlar {
		ha_yaz(t, y, g)
	}
	dis := filepath.Join(t.TempDir(), "hedef")
	ha_yaz(t, dis, la_cron("c_gitti"))
	bag := filepath.Join(o.cron, "girginos-laravel-13")
	if err := os.Symlink(dis, bag); err != nil {
		t.Fatal(err)
	}

	oz := laravelArtiklariniSupur()
	if oz.YetimCron != 2 || oz.YetimBirim != 0 || oz.Yamalanan != 0 || oz.Hata != 0 {
		t.Fatalf("özet: %+v", oz)
	}
	ha_yok(t, yetimEtkin)
	ha_yok(t, yetimAski)
	for y, g := range kalanlar {
		if got := ha_oku(t, y); got != g {
			t.Errorf("%s'e dokunulmamalıydı", y)
		}
	}
	if !la_var(bag) || !la_var(dis) {
		t.Fatal("symlink cron dosyasına ve hedefine dokunulmamalı")
	}
	if len(*o.komutlar) != 0 {
		t.Fatalf("cron dosyası silmek systemctl gerektirmez: %v", *o.komutlar)
	}
	// idempotent
	if oz2 := laravelArtiklariniSupur(); oz2 != (laravelArtikOzet{}) {
		t.Fatalf("ikinci tur sıfır özet vermeli: %+v", oz2)
	}
}

// ---- hesap silme ----

func TestLaravelArtiklariniSil_YalnizHesabinKendisi(t *testing.T) {
	o := la_dikis(t)
	o.mevcut["c_a"], o.mevcut["c_a_b"] = true, true // silme userdel'den ÖNCE: kullanıcı hâlâ var

	a1, a2 := "girginos-laravel-queue-1.service", "girginos-laravel-queue-2.service"
	komsu := "girginos-laravel-queue-3.service"
	uyg := "gosp-app-c_a-web.service"
	o.birim(t, a1, la_birim("c_a", false))
	o.birim(t, a2, la_birim("c_a", true))
	o.birim(t, komsu, la_birim("c_a_b", false)) // adı c_a önekiyle başlayan komşu hesap
	o.birim(t, uyg, la_birim("c_a", false))     // Uygulama Çalıştırıcı'nın birimi: bu özelliğin işi değil
	if err := os.MkdirAll(filepath.Join(o.dizin, a1+".d"), 0o755); err != nil {
		t.Fatal(err)
	}
	ha_yaz(t, filepath.Join(o.dizin, a1+".d", birimAskiDropIn), "[Unit]\n")

	c1, c2 := filepath.Join(o.cron, "girginos-laravel-1"), filepath.Join(o.aski, "girginos-laravel-2")
	ck, cd := filepath.Join(o.cron, "girginos-laravel-3"), filepath.Join(o.cron, "diger-is")
	ha_yaz(t, c1, la_cron("c_a"))
	ha_yaz(t, c2, la_cron("c_a"))
	ha_yaz(t, ck, la_cron("c_a_b"))
	ha_yaz(t, cd, la_cron("c_a"))

	LaravelArtiklariniSil("c_a")

	ha_yok(t, filepath.Join(o.dizin, a1))
	ha_yok(t, filepath.Join(o.dizin, a1+".d"))
	ha_yok(t, filepath.Join(o.dizin, a2))
	ha_yok(t, c1)
	ha_yok(t, c2)
	for _, y := range []string{komsu, uyg} {
		if !la_var(filepath.Join(o.dizin, y)) {
			t.Errorf("%s'e dokunulmamalıydı", y)
		}
	}
	if !la_var(ck) || !la_var(cd) {
		t.Fatal("komşu hesabın ve panelin olmayan cron dosyasına dokunulmamalı")
	}
	want := "systemctl disable --now " + a1 + "\nsystemctl disable --now " + a2 + "\nsystemctl daemon-reload\n" +
		"systemctl reset-failed " + a1 + "\nsystemctl reset-failed " + a2
	if got := o.komutMetni(); got != want {
		t.Fatalf("komut dizisi:\n%s\n--- beklenen ---\n%s", got, want)
	}

	// idempotent: ikinci çağrıda komut yok
	*o.komutlar = nil
	LaravelArtiklariniSil("c_a")
	if len(*o.komutlar) != 0 {
		t.Fatalf("ikinci çağrıda komut çalışmamalı: %v", *o.komutlar)
	}
}

func TestLaravelArtiklariniSil_GecersizHesapAdiHicbirSeyeDokunmaz(t *testing.T) {
	o := la_dikis(t)
	o.birim(t, "girginos-laravel-queue-1.service", la_birim("root", false))
	ha_yaz(t, filepath.Join(o.cron, "girginos-laravel-1"), la_cron("root"))
	for _, sk := range []string{"", "root", "c_", "c_../x", "c_A", "c_a b", strings.Repeat("c", 40)} {
		LaravelArtiklariniSil(sk)
	}
	if len(*o.komutlar) != 0 || !la_var(filepath.Join(o.dizin, "girginos-laravel-queue-1.service")) ||
		!la_var(filepath.Join(o.cron, "girginos-laravel-1")) {
		t.Fatalf("geçersiz/panel dışı hesap adında hiçbir şey yapılmamalı: %v", *o.komutlar)
	}
}

// Birim kaldırıcı kendi başına da yalnız Laravel kuyruk birimi adlarını kabul eder (ikinci savunma hattı).
func TestLaravelKuyrukBiriminiKaldir_YalnizKuyrukBirimAdi(t *testing.T) {
	o := la_dikis(t)
	for _, ad := range []string{"gosp-app-c_a-web.service", "girginos-laravel-queue-x.service", "../x.service", "sshd.service", ""} {
		yol := filepath.Join(o.dizin, filepath.Base(ad))
		if ad != "" && !strings.Contains(ad, "/") {
			o.birim(t, ad, la_birim("c_a", false))
		}
		if err := laravelKuyrukBiriminiKaldir(ad); err == nil {
			t.Errorf("%q kabul edilmemeli", ad)
		}
		if ad != "" && !strings.Contains(ad, "/") && !la_var(yol) {
			t.Errorf("%q silinmemeliydi", ad)
		}
	}
	if len(*o.komutlar) != 0 {
		t.Fatalf("reddedilen adlarda hiçbir systemctl çalışmamalı: %v", *o.komutlar)
	}
}

// Beklenenden büyük dosya okunmaz (bozuk/saldırgan içerik belleği şişirmesin); tam sınırdaki okunur.
func TestSinirliDosyaOku_Sinir(t *testing.T) {
	d := t.TempDir()
	tam, buyuk := filepath.Join(d, "tam"), filepath.Join(d, "buyuk")
	ha_yaz(t, tam, strings.Repeat("a", 100))
	ha_yaz(t, buyuk, strings.Repeat("a", 101))
	if b, err := sinirliDosyaOku(tam, 100); err != nil || len(b) != 100 {
		t.Fatalf("sınırdaki dosya okunmalı: %d bayt, %v", len(b), err)
	}
	if _, err := sinirliDosyaOku(buyuk, 100); err == nil {
		t.Fatal("sınırı aşan dosya reddedilmeli")
	}
	if _, err := sinirliDosyaOku(filepath.Join(d, "yok"), 100); err == nil {
		t.Fatal("olmayan dosya hata vermeli")
	}
}

// ---- atomik, karşılaştırmalı yazım ----

func TestCasYaz_BaskaYazarEzilmez(t *testing.T) {
	o := la_dikis(t)
	yol := filepath.Join(o.dizin, "girginos-laravel-queue-1.service")
	once := []byte(la_birim("c_a", true))
	ha_yaz(t, yol, string(once))

	// bu arada kuyruk uç noktası farklı ayarlarla yeni birim yazdı: yama onu ezmemeli
	yeniAyar := strings.Replace(la_birim("c_a", false), "--timeout=60", "--timeout=300", 1)
	ha_yaz(t, yol, yeniAyar)
	yazildi, err := casYaz(yol, once, []byte(la_birim("c_a", false)))
	if err != nil || yazildi {
		t.Fatalf("içerik değişmişse yazılmamalı (yazıldı=%v hata=%v)", yazildi, err)
	}
	if got := ha_oku(t, yol); got != yeniAyar {
		t.Fatalf("başkasının yazdığı içerik ezildi:\n%s", got)
	}
	// içerik hâlâ beklenen ise yazılır, geçici dosya artığı kalmaz
	ha_yaz(t, yol, string(once))
	if yazildi, err := casYaz(yol, once, []byte(la_birim("c_a", false))); err != nil || !yazildi {
		t.Fatalf("beklenen içerikte yazılmalı (yazıldı=%v hata=%v)", yazildi, err)
	}
	if got := ha_oku(t, yol); got != la_birim("c_a", false) {
		t.Fatalf("yeni içerik yazılmadı:\n%s", got)
	}
	ha_yok(t, yol+".tmp")
}

// ---- sabitler ----

// Üretim varsayılanları sabitlerle aynı (testler değiştirip geri yükler; burada hiçbiri değiştirilmemiş olmalı).
func TestLaravelArtik_VarsayilanYollar(t *testing.T) {
	if hesapBirimDizini != LaravelBirimDizini {
		t.Fatalf("hesapBirimDizini=%q, LaravelBirimDizini=%q", hesapBirimDizini, LaravelBirimDizini)
	}
	if laravelCronDizini != LaravelCronDizini || laravelCronAskiDizini != LaravelCronAskiDizini {
		t.Fatalf("cron dizinleri sabitlerden saptı: %q %q", laravelCronDizini, laravelCronAskiDizini)
	}
	for ad, re := range map[string]string{
		LaravelKuyrukBirimOnEki + "12.service": "kuyruk birimi",
		LaravelCronDosyaOnEki + "12":           "cron dosyası",
	} {
		ok := reLaravelKuyrukBirim.MatchString(ad)
		if re == "cron dosyası" {
			ok = reLaravelCronDosya.MatchString(ad)
		}
		if !ok {
			t.Errorf("%s adı (%s) kendi deseniyle eşleşmeli", ad, re)
		}
	}
	// desenler birbirinin alanına girmez
	if reLaravelCronDosya.MatchString(LaravelKuyrukBirimOnEki+"12") || reLaravelKuyrukBirim.MatchString(LaravelCronDosyaOnEki+"12") {
		t.Fatal("cron ve birim desenleri ayrışmalı")
	}
	for _, ad := range []string{"girginos-laravel-queue-.service", "girginos-laravel-queue-1.service.bak", "xgirginos-laravel-queue-1.service", "girginos-laravel-queue-1a.service"} {
		if reLaravelKuyrukBirim.MatchString(ad) {
			t.Errorf("%q birim deseniyle eşleşmemeli", ad)
		}
	}
}

// ---- bağlantılar (AST) ----
//
// Deprovision / Init gövdesi birim testle sürülemez (nginx, userdel, DB gerekir); sıra değişmezleri
// bu yüzden kaynak ağacından doğrulanır. Biçimden (boşluk, yorum) etkilenmez.

func la_govde(t *testing.T, dosya, ad string) *ast.BlockStmt {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), dosya, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == ad {
			return fd.Body
		}
	}
	t.Fatalf("%s içinde %s bulunamadı", dosya, ad)
	return nil
}

// la_cagrilar: gövdedeki çağrıların konumları. ad "pkg.Fn" ya da "Fn"; harf doluysa çağrının
// argümanlarından biri bu string sabiti olmalı.
func la_cagrilar(g *ast.BlockStmt, ad, harf string) []token.Pos {
	var konum []token.Pos
	ast.Inspect(g, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var isim string
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			isim = fn.Name
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				isim = x.Name + "." + fn.Sel.Name
			}
		}
		if isim != ad {
			return true
		}
		if harf != "" {
			var bulundu bool
			for _, a := range c.Args {
				if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if s, err := strconv.Unquote(bl.Value); err == nil && s == harf {
						bulundu = true
					}
				}
			}
			if !bulundu {
				return true
			}
		}
		konum = append(konum, c.Pos())
		return true
	})
	return konum
}

func TestBaglanti_Deprovision_LaravelArtiklariUserdeldenOnce(t *testing.T) {
	g := la_govde(t, "provisioner.go", "Deprovision")
	laravel := la_cagrilar(g, "LaravelArtiklariniSil", "")
	userdel := la_cagrilar(g, "exec.Command", "userdel")
	if len(laravel) != 1 || len(userdel) != 1 {
		t.Fatalf("Deprovision'da LaravelArtiklariniSil=%d, userdel=%d çağrı var; her biri 1 olmalı", len(laravel), len(userdel))
	}
	if laravel[0] >= userdel[0] {
		t.Fatal("LaravelArtiklariniSil userdel'den ÖNCE çağrılmalı: çalışan işleyici varken `userdel -r` sessizce başarısız olur")
	}
}

func TestBaglanti_Init_LaravelSupurmesiCagrilir_ArkaplandaVeKorumali(t *testing.T) {
	if n := len(la_cagrilar(la_govde(t, "provisioner.go", "Init"), "LaravelArtiklariniSupurOnStartup", "")); n != 1 {
		t.Fatalf("Init'te LaravelArtiklariniSupurOnStartup %d kez çağrılıyor; 1 olmalı", n)
	}
	g := la_govde(t, "laravel_artik.go", "LaravelArtiklariniSupurOnStartup")
	var goVar, recoverVar bool
	ast_dolas(g, &goVar, &recoverVar)
	if !goVar || !recoverVar {
		t.Fatalf("süpürme Init'i bloklamamalı (goroutine=%v) ve çökmeye karşı korumalı olmalı (recover=%v)", goVar, recoverVar)
	}
}

// ast_dolas: gövdede `go` ifadesi ve recover() çağrısı var mı.
func ast_dolas(g *ast.BlockStmt, goVar, recoverVar *bool) {
	ast.Inspect(g, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			*goVar = true
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "recover" {
				*recoverVar = true
			}
		}
		return true
	})
}
