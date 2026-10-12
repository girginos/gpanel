package provisioner

// mail_yasak_havuz_test.go — paylaşılan-master / alt alan havuzlarının MEVCUT dosyalarında mail() yasağı.
//
// Neyi korur: (1) yama saf işlevi doğru durumlara ayrılır (zaten var / operatörün özel değeri / panel
// havuzu değil / çapa yok) ve eski biçimi şablonun BİREBİR çıktısına getirir; (2) tarama yalnız tenant
// havuzlarına (c_*) dokunur — dağıtım www.conf'una ve phpmyadmin/roundcube'a DOKUNMAZ; (3) php-fpm -t
// master başına bir kez koşar, başarısızsa yamalar geri alınır ama başkasının araya yazdığı içerik EZİLMEZ;
// (4) çalışmayan master başlatılmaz; (5) tarama hesabın kilidiyle sıralanır; (6) idempotent;
// (7) Init'e arka planda ve betikten sonra bağlanmıştır.

import (
	"bytes"
	"errors"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/kilit"
)

// mailHavuzIcerik: phpPoolTmpl çıktısı. satirli=false → ESKİ sürümün ürettiği biçim (yasak satırı yok).
func mailHavuzIcerik(t *testing.T, kullanici string, satirli bool) string {
	t.Helper()
	var buf bytes.Buffer
	if err := phpPoolTmpl.Execute(&buf, map[string]string{"User": kullanici, "Socket": "/run/php-fpm/" + kullanici + ".sock"}); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, MailYasakPoolSatiri()+"\n") {
		t.Fatalf("phpPoolTmpl çıktısında yasak satırı yok:\n%s", s)
	}
	if !satirli {
		s = strings.Replace(s, MailYasakPoolSatiri()+"\n", "", 1)
	}
	return s
}

func mailYaz(t *testing.T, yol, icerik string, mod os.FileMode) {
	t.Helper()
	if err := os.WriteFile(yol, []byte(icerik), mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(yol, mod); err != nil {
		t.Fatal(err)
	}
}

func mailOku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- saf yama işlevi ----

func TestMailHavuzYama_Durumlar(t *testing.T) {
	eski := mailHavuzIcerik(t, "c_a", false)
	yeni := mailHavuzIcerik(t, "c_a", true)
	satir := MailYasakPoolSatiri()
	// araya: satırı disable_functions satırının hemen ardına koyar (yama işlevinden BAĞIMSIZ beklenen çıktı)
	araya := func(icerik string) string {
		i := strings.Index(icerik, "php_admin_value[disable_functions]")
		if i < 0 {
			t.Fatal("fixture'da disable_functions yok")
		}
		j := i + strings.Index(icerik[i:], "\n") + 1
		return icerik[:j] + satir + "\n" + icerik[j:]
	}

	cases := []struct {
		ad     string
		girdi  string
		durum  mailHavuzDurum
		cikti  string // "" = girdiyle aynı kalmalı
		satirN int    // çıktıda "sendmail_path" geçiş sayısı
	}{
		{"eski biçim yamalanır, şablon çıktısına BİREBİR varır", eski, mailHavuzYamalandi, yeni, 1},
		{"şablon çıktısı zaten var", yeni, mailHavuzZatenVar, "", 1},
		{"girintili + sondaki boşluklu aynı satır zaten var", strings.Replace(yeni, satir, "  "+satir+"   ", 1), mailHavuzZatenVar, "", 1},
		{"operatörün özel değeri korunur", strings.Replace(yeni, satir, "php_admin_value[sendmail_path] = /usr/bin/msmtp -t", 1), mailHavuzOzelDeger, "", 1},
		{"php_value[sendmail_path] sayılmaz (INI_SYSTEM'i değiştiremez)", eski + "php_value[sendmail_path] = /usr/sbin/sendmail -t -i\n", mailHavuzYamalandi, araya(eski + "php_value[sendmail_path] = /usr/sbin/sendmail -t -i\n"), 2},
		{";yorum satırı sayılmaz", eski + ";" + satir + "\n", mailHavuzYamalandi, araya(eski + ";" + satir + "\n"), 2},
		{"dağıtım www.conf'u (işaret yok)", "[www]\nuser = apache\ngroup = apache\n;php_admin_value[sendmail_path] = /usr/sbin/sendmail -t -i -f www@my.domain.com\n", mailHavuzTanimsiz, "", 1},
		{"işaret var ama user satırı yok", "[x]\n; ---- BEGIN_CUSTOM ----\nphp_admin_value[disable_functions] = a\n", mailHavuzTanimsiz, "", 0},
		{"işaret ve user var ama çapa yok", "[x]\nuser = c_x\n; ---- BEGIN_CUSTOM ----\n; ---- END_CUSTOM ----\n", mailHavuzCapasiz, "", 0},
		{"alt alan/poolTmpl biçimi (BEGIN_CUSTOM işareti)",
			"[c_x_s3]\nuser = c_x\nphp_admin_value[disable_functions] = a,b\n\n; ---- Common ----\n; ---- BEGIN_CUSTOM ----\n\n; ---- END_CUSTOM ----\n",
			mailHavuzYamalandi,
			"[c_x_s3]\nuser = c_x\nphp_admin_value[disable_functions] = a,b\n" + satir + "\n\n; ---- Common ----\n; ---- BEGIN_CUSTOM ----\n\n; ---- END_CUSTOM ----\n", 1},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			got, durum := mailHavuzYama(c.girdi)
			if durum != c.durum {
				t.Fatalf("durum %d, beklenen %d\n%s", durum, c.durum, got)
			}
			beklenen := c.cikti
			if beklenen == "" {
				beklenen = c.girdi
			}
			if got != beklenen {
				t.Fatalf("çıktı beklenenden farklı:\n--- got\n%s\n--- want\n%s", got, beklenen)
			}
			if n := strings.Count(got, "sendmail_path"); n != c.satirN {
				t.Fatalf("sendmail_path %d kez geçiyor, beklenen %d:\n%s", n, c.satirN, got)
			}
			// idempotent: yamalanmış çıktı ikinci kez yamalanmaz
			if durum == mailHavuzYamalandi {
				if again, d2 := mailHavuzYama(got); d2 == mailHavuzYamalandi || again != got {
					t.Fatalf("ikinci yama içeriği değiştirmemeli (durum=%d)", d2)
				}
			}
		})
	}

	// dışa açık sarmalayıcı aynı sonucu verir
	if s, ok := MailYasakHavuzuYamala(eski); !ok || s != yeni {
		t.Fatal("MailYasakHavuzuYamala eski biçimi yeni biçime getirmeli")
	}
	if s, ok := MailYasakHavuzuYamala(yeni); ok || s != yeni {
		t.Fatal("MailYasakHavuzuYamala zaten kapalı havuza dokunmamalı")
	}
}

func TestPaylasilanMasterler_PhpMapIleTutarli(t *testing.T) {
	ml := paylasilanMasterler()
	if len(ml) == 0 {
		t.Fatal("hiç master dönmedi")
	}
	gorulen := map[string]bool{}
	var ad83 bool
	for i, m := range ml {
		if m.Dizin == "" || m.Servis == "" {
			t.Fatalf("eksik alan: %+v", m)
		}
		if gorulen[m.Dizin] {
			t.Fatalf("dizin tekrarlandı: %s", m.Dizin)
		}
		gorulen[m.Dizin] = true
		if i > 0 && ml[i-1].Dizin >= m.Dizin {
			t.Fatalf("dizine göre sıralı olmalı: %s >= %s", ml[i-1].Dizin, m.Dizin)
		}
		if m.Dizin == "/etc/php-fpm.d" {
			ad83 = true
			if m.Servis != "php-fpm" || m.FpmBin != "/usr/sbin/php-fpm" {
				t.Fatalf("AppStream (8.3) master yanlış: %+v", m)
			}
		}
	}
	if !ad83 {
		t.Fatal("AppStream 8.3 master'ı (/etc/php-fpm.d) listede olmalı")
	}
	if len(ml) != len(paylasilanHavuzDizinleri()) {
		t.Fatalf("askı kodunun dizin listesiyle (%d) aynı sayıda master beklenir: %d", len(paylasilanHavuzDizinleri()), len(ml))
	}
}

// ---- tarama ----

type mailSweepOrtam struct {
	d1, d2   string
	komutlar []string
	yanit    func(satir string) ([]byte, error)
}

func mailSweepKur(t *testing.T) *mailSweepOrtam {
	t.Helper()
	o := &mailSweepOrtam{d1: t.TempDir(), d2: t.TempDir()}
	oM, oK := mailHavuzMasterleri, mailKomut
	t.Cleanup(func() { mailHavuzMasterleri, mailKomut = oM, oK })
	mailHavuzMasterleri = func() []paylasilanMaster {
		return []paylasilanMaster{
			{Dizin: o.d1, Servis: "php-fpm", FpmBin: "/sahte/php-fpm"},
			{Dizin: o.d2, Servis: "php82-php-fpm", FpmBin: "/sahte/php82-fpm"},
		}
	}
	mailKomut = func(ad string, arg ...string) ([]byte, error) {
		satir := ad + " " + strings.Join(arg, " ")
		o.komutlar = append(o.komutlar, strings.TrimSpace(satir))
		if o.yanit != nil {
			return o.yanit(strings.TrimSpace(satir))
		}
		return nil, nil
	}
	return o
}

// mailSweepDosyalari: tipik bir sunucuyu kurar. Dönen harita: yol → yamadan SONRA beklenen içerik.
func mailSweepDosyalari(t *testing.T, o *mailSweepOrtam) map[string]string {
	t.Helper()
	altAlan := func(s string) string { return strings.Replace(s, "[c_a]", "[c_a_s3]", 1) }
	beklenen := map[string]string{}
	yaz := func(dizin, ad string, icerik string, mod os.FileMode, sonra string) {
		yol := filepath.Join(dizin, ad)
		mailYaz(t, yol, icerik, mod)
		beklenen[yol] = sonra
	}
	eskiA, yeniA := mailHavuzIcerik(t, "c_a", false), mailHavuzIcerik(t, "c_a", true)
	// d1: ana master
	yaz(o.d1, "c_a.conf", eskiA, 0o640, yeniA)                                                             // canlı hesap havuzu → yamalanır (mod 0640 korunur)
	yaz(o.d1, "c_a_s3.conf", altAlan(eskiA), 0o644, altAlan(yeniA))                                        // canlı alt alan havuzu → yamalanır
	yaz(o.d1, "c_a.conf.bak", eskiA, 0o644, yeniA)                                                         // kafese geçişte saklanan yedek → yamalanır
	yaz(o.d1, "c_b.conf.askida", mailHavuzIcerik(t, "c_b", false), 0o644, mailHavuzIcerik(t, "c_b", true)) // askıdaki hesap → yamalanır
	yaz(o.d1, "c_ok.conf", mailHavuzIcerik(t, "c_ok", true), 0o644, mailHavuzIcerik(t, "c_ok", true))      // zaten kapalı
	ozel := strings.Replace(mailHavuzIcerik(t, "c_ozel", true), MailYasakPoolSatiri(), "php_admin_value[sendmail_path] = /usr/bin/msmtp -t", 1)
	yaz(o.d1, "c_ozel.conf", ozel, 0o644, ozel) // operatörün özel değeri → dokunulmaz
	www := "[www]\nuser = apache\ngroup = apache\nphp_admin_value[disable_functions] = x\n; ---- BEGIN_CUSTOM ----\n"
	yaz(o.d1, "www.conf", www, 0o644, www) // c_ öneki yok → işaretçi olsa bile dokunulmaz
	pma := mailHavuzIcerik(t, "apache", false)
	yaz(o.d1, "phpmyadmin.conf", pma, 0o644, pma) // panelin kendi havuzu: tenant kodu çalıştırmaz
	yaz(o.d1, "roundcube.conf", pma, 0o644, pma)
	yaz(o.d1, "notlar.txt", "c_a\n", 0o644, "c_a\n")
	// d2: ikinci master
	yaz(o.d2, "c_x.conf", mailHavuzIcerik(t, "c_x", false), 0o644, mailHavuzIcerik(t, "c_x", true))
	return beklenen
}

func mailDosyalariDogrula(t *testing.T, beklenen map[string]string) {
	t.Helper()
	for yol, want := range beklenen {
		if got := mailOku(t, yol); got != want {
			t.Errorf("%s içeriği beklenenden farklı:\n--- got\n%s\n--- want\n%s", filepath.Base(yol), got, want)
		}
	}
}

func TestMailPaylasilanHavuzlariKapat_YalnizTenantHavuzlarinaDokunurVeYenilerIdempotent(t *testing.T) {
	o := mailSweepKur(t)
	beklenen := mailSweepDosyalari(t, o)

	oz := mailPaylasilanHavuzlariKapat()
	mailDosyalariDogrula(t, beklenen)

	// sayaçlar: canlı 3 (c_a, c_a_s3, c_x) + yedek/askı 2; zaten 1 (c_ok); özel 1 (c_ozel)
	if oz.Yamalanan != 3 || oz.Yedek != 2 || oz.Zaten != 1 || oz.Ozel != 1 || oz.Capasiz != 0 || oz.Hata != 0 {
		t.Fatalf("özet beklenenden farklı: %+v", oz)
	}
	if want := []string{"php-fpm", "php82-php-fpm"}; !reflect.DeepEqual(oz.Yenilenen, want) || len(oz.GeriAlinan) != 0 {
		t.Fatalf("yenilenen=%v geri alınan=%v", oz.Yenilenen, oz.GeriAlinan)
	}
	// komutlar: master başına TEK -t, TEK is-active, TEK reload — sırayla; yedek/askı için hiçbiri
	want := []string{
		"/sahte/php-fpm -t", "systemctl is-active --quiet php-fpm", "systemctl reload-or-restart php-fpm",
		"/sahte/php82-fpm -t", "systemctl is-active --quiet php82-php-fpm", "systemctl reload-or-restart php82-php-fpm",
	}
	if !reflect.DeepEqual(o.komutlar, want) {
		t.Fatalf("komutlar:\n got %q\nwant %q", o.komutlar, want)
	}
	// dosya izni korunur, geride .tmp artığı yok
	if st, err := os.Stat(filepath.Join(o.d1, "c_a.conf")); err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("c_a.conf izni korunmalı (0640): %v %v", st, err)
	}
	for _, d := range []string{o.d1, o.d2} {
		if artik, _ := filepath.Glob(filepath.Join(d, "*.tmp")); len(artik) != 0 {
			t.Fatalf("geçici dosya artığı kaldı: %v", artik)
		}
	}

	// ikinci tarama: hiçbir şey değişmez, hiçbir komut koşmaz
	o.komutlar = nil
	st1, _ := os.Stat(filepath.Join(o.d1, "c_a.conf"))
	time.Sleep(20 * time.Millisecond)
	oz2 := mailPaylasilanHavuzlariKapat()
	mailDosyalariDogrula(t, beklenen)
	if oz2.Yamalanan != 0 || oz2.Yedek != 0 || oz2.Hata != 0 || len(oz2.Yenilenen) != 0 || len(o.komutlar) != 0 {
		t.Fatalf("ikinci tarama no-op olmalı: %+v komutlar=%q", oz2, o.komutlar)
	}
	if oz2.Zaten != 6 || oz2.Ozel != 1 { // c_a, c_a_s3, c_ok, c_x + .bak + .askida ; c_ozel
		t.Fatalf("ikinci taramada 6 zaten + 1 özel beklenir: %+v", oz2)
	}
	if st2, _ := os.Stat(filepath.Join(o.d1, "c_a.conf")); !st2.ModTime().Equal(st1.ModTime()) {
		t.Fatal("ikinci tarama dosyayı yeniden yazmamalı")
	}
}

func TestMailPaylasilanHavuzlariKapat_CalismayanMasterBaslatilmaz(t *testing.T) {
	o := mailSweepKur(t)
	mailYaz(t, filepath.Join(o.d1, "c_a.conf"), mailHavuzIcerik(t, "c_a", false), 0o644)
	o.yanit = func(satir string) ([]byte, error) {
		if satir == "systemctl is-active --quiet php-fpm" {
			return nil, errors.New("exit status 3") // inactive
		}
		return nil, nil
	}
	oz := mailPaylasilanHavuzlariKapat()
	if got := mailOku(t, filepath.Join(o.d1, "c_a.conf")); got != mailHavuzIcerik(t, "c_a", true) {
		t.Fatal("çalışmayan master'ın dosyası da yamalanmalı (başlayınca okur)")
	}
	for _, k := range o.komutlar {
		if strings.Contains(k, "reload-or-restart") || strings.Contains(k, " start") {
			t.Fatalf("çalışmayan master'a reload/start verilmemeli: %q", o.komutlar)
		}
	}
	if len(oz.Yenilenen) != 0 || oz.Yamalanan != 1 {
		t.Fatalf("özet: %+v", oz)
	}
}

func TestMailPaylasilanHavuzlariKapat_FpmTestiBasarisizsaGeriAlir(t *testing.T) {
	o := mailSweepKur(t)
	beklenen := mailSweepDosyalari(t, o)
	o.yanit = func(satir string) ([]byte, error) {
		if satir == "/sahte/php-fpm -t" {
			return []byte("ERROR: [pool c_a] cannot get uid for user"), errors.New("exit status 78")
		}
		return nil, nil
	}
	oz := mailPaylasilanHavuzlariKapat()

	// d1 (test başarısız): CANLI dosyalar eski haline döner; yüklenmeyen yedek/askı kopyaları yamalı kalır
	for _, ad := range []string{"c_a.conf", "c_a_s3.conf"} {
		yol := filepath.Join(o.d1, ad)
		want := mailHavuzIcerik(t, "c_a", false)
		if ad == "c_a_s3.conf" {
			want = strings.Replace(want, "[c_a]", "[c_a_s3]", 1)
		}
		if got := mailOku(t, yol); got != want {
			t.Errorf("%s geri alınmalıydı:\n%s", ad, got)
		}
	}
	if st, _ := os.Stat(filepath.Join(o.d1, "c_a.conf")); st.Mode().Perm() != 0o640 {
		t.Errorf("geri alınan dosyanın izni korunmalı: %v", st.Mode())
	}
	for _, ad := range []string{"c_a.conf.bak", "c_b.conf.askida"} {
		yol := filepath.Join(o.d1, ad)
		if got := mailOku(t, yol); got != beklenen[yol] {
			t.Errorf("%s (yüklenmeyen kopya) yamalı kalmalı", ad)
		}
	}
	// d2 etkilenmez: yamalanır ve reload edilir
	if got, yol := mailOku(t, filepath.Join(o.d2, "c_x.conf")), filepath.Join(o.d2, "c_x.conf"); got != beklenen[yol] {
		t.Error("ikinci master başarısız ilk master'dan etkilenmemeli")
	}
	if !reflect.DeepEqual(oz.GeriAlinan, []string{"php-fpm"}) || !reflect.DeepEqual(oz.Yenilenen, []string{"php82-php-fpm"}) {
		t.Fatalf("geri alınan=%v yenilenen=%v", oz.GeriAlinan, oz.Yenilenen)
	}
	if oz.Yamalanan != 1 {
		t.Fatalf("geri alınan yamalar 'Yamalanan'dan düşülmeli (yalnız c_x): %+v", oz)
	}
	for _, k := range o.komutlar {
		if k == "systemctl reload-or-restart php-fpm" {
			t.Fatalf("doğrulaması geçmeyen master reload EDİLMEMELİ: %q", o.komutlar)
		}
	}
}

func TestMailPaylasilanHavuzlariKapat_GeriAlmaBaskaYaziciyiEzmez(t *testing.T) {
	o := mailSweepKur(t)
	yolA, yolB := filepath.Join(o.d1, "c_a.conf"), filepath.Join(o.d1, "c_b.conf")
	mailYaz(t, yolA, mailHavuzIcerik(t, "c_a", false), 0o644)
	mailYaz(t, yolB, mailHavuzIcerik(t, "c_b", false), 0o644)
	baskasi := mailHavuzIcerik(t, "c_a", true) + "; PHP ayarı kaydı araya yazdı\n"
	o.yanit = func(satir string) ([]byte, error) {
		if satir == "/sahte/php-fpm -t" {
			// php-fpm -t koşarken başka bir yazıcı (PHP ayarı kaydı) c_a.conf'u yeniden yazar
			mailYaz(t, yolA, baskasi, 0o644)
			return nil, errors.New("exit status 78")
		}
		return nil, nil
	}
	mailPaylasilanHavuzlariKapat()
	if got := mailOku(t, yolA); got != baskasi {
		t.Fatalf("geri alma, araya yazılan yeni içeriği EZMEMELİ:\n%s", got)
	}
	if got := mailOku(t, yolB); got != mailHavuzIcerik(t, "c_b", false) {
		t.Fatalf("dokunulmamış dosya eski haline dönmeli:\n%s", got)
	}
}

func TestMailPaylasilanHavuzlariKapat_FpmBinBossaTestAtlanirReloadYapilir(t *testing.T) {
	o := mailSweepKur(t)
	mailHavuzMasterleri = func() []paylasilanMaster {
		return []paylasilanMaster{{Dizin: o.d1, Servis: "php-fpm", FpmBin: ""}}
	}
	mailYaz(t, filepath.Join(o.d1, "c_a.conf"), mailHavuzIcerik(t, "c_a", false), 0o644)
	mailPaylasilanHavuzlariKapat()
	if want := []string{"systemctl is-active --quiet php-fpm", "systemctl reload-or-restart php-fpm"}; !reflect.DeepEqual(o.komutlar, want) {
		t.Fatalf("komutlar %q, beklenen %q", o.komutlar, want)
	}
}

func TestMailPaylasilanHavuzlariKapat_ReloadHatasiYamayiGeriAlmaz(t *testing.T) {
	o := mailSweepKur(t)
	yol := filepath.Join(o.d1, "c_a.conf")
	mailYaz(t, yol, mailHavuzIcerik(t, "c_a", false), 0o644)
	o.yanit = func(satir string) ([]byte, error) {
		if strings.HasPrefix(satir, "systemctl reload-or-restart") {
			return []byte("Job failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	oz := mailPaylasilanHavuzlariKapat()
	// -t geçti → dosya geçerli; reload bir sonraki fırsatta (PHP ayarı kaydı / açılış) devreye girer
	if got := mailOku(t, yol); got != mailHavuzIcerik(t, "c_a", true) {
		t.Fatal("reload hatası dosyadaki geçerli yamayı geri almamalı")
	}
	if len(oz.Yenilenen) != 0 || len(oz.GeriAlinan) != 0 {
		t.Fatalf("özet: %+v", oz)
	}
}

func TestMailPaylasilanHavuzlariKapat_DizinYoksaSessizGecer(t *testing.T) {
	o := mailSweepKur(t)
	mailHavuzMasterleri = func() []paylasilanMaster {
		return []paylasilanMaster{{Dizin: filepath.Join(o.d1, "yok"), Servis: "php99-php-fpm", FpmBin: "/sahte/php99"}}
	}
	oz := mailPaylasilanHavuzlariKapat()
	if oz.Yamalanan+oz.Yedek+oz.Hata != 0 || len(o.komutlar) != 0 {
		t.Fatalf("kurulu olmayan sürüm hiçbir şey yapmamalı: %+v %q", oz, o.komutlar)
	}
}

func TestMailPaylasilanHavuzlariKapat_HesabinKilidiyleSiralanir(t *testing.T) {
	o := mailSweepKur(t)
	yol := filepath.Join(o.d1, "c_kilit.conf")
	mailYaz(t, yol, mailHavuzIcerik(t, "c_kilit", false), 0o644)

	hk := kilit.Hesap("c_kilit") // PHP ayarı kaydı / askıya alma bu kilidi tutar
	hk.Lock()
	bitti := make(chan struct{})
	go func() {
		mailPaylasilanHavuzlariKapat()
		close(bitti)
	}()
	select {
	case <-bitti:
		hk.Unlock()
		t.Fatal("tarama, hesabın kilidi başkasındayken dosyayı yamalamamalı")
	case <-time.After(200 * time.Millisecond):
	}
	if got := mailOku(t, yol); got != mailHavuzIcerik(t, "c_kilit", false) {
		hk.Unlock()
		t.Fatal("kilit tutulurken dosyaya dokunulmamalı")
	}
	hk.Unlock()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("kilit bırakılınca tarama tamamlanmalı")
	}
	if got := mailOku(t, yol); got != mailHavuzIcerik(t, "c_kilit", true) {
		t.Fatal("kilit bırakılınca dosya yamalanmalı")
	}
}

// ---- bağlantılar (AST) ----

func TestBaglanti_Init_MailHavuzTaramasiBetiktenSonraVeArkaplanda(t *testing.T) {
	_, g := fonksiyonGovdesi(t, "provisioner.go", "Init")
	kur := cagriKonumlari(g, "MailYasakKur", "")
	tara := cagriKonumlari(g, "MailYasakHavuzlariniKapatOnStartup", "")
	sweep := cagriKonumlari(g, "EnsureTenantFPMOnStartup", "")
	if len(kur) != 1 || len(tara) != 1 || len(sweep) != 1 {
		t.Fatalf("Init'te MailYasakKur=%d, MailYasakHavuzlariniKapatOnStartup=%d, EnsureTenantFPMOnStartup=%d çağrı var; her biri 1 olmalı",
			len(kur), len(tara), len(sweep))
	}
	if tara[0] <= kur[0] {
		t.Fatal("havuz taraması, hızlı-başarısız betiği kuran MailYasakKur'dan SONRA çağrılmalı")
	}
	if tara[0] >= sweep[0] {
		t.Fatal("havuz taraması EnsureTenantFPMOnStartup'tan ÖNCE başlamalı (yedekten geri dönen havuzlar yamalı gelsin)")
	}

	// OnStartup işlevi Init'i bloklamamalı: gövdesi bir goroutine başlatır, o da recover ile korunur
	// ve senkron taramayı çağırır.
	_, ob := fonksiyonGovdesi(t, "mail_yasak.go", "MailYasakHavuzlariniKapatOnStartup")
	var goStmt *ast.GoStmt
	ast.Inspect(ob, func(n ast.Node) bool {
		if gs, ok := n.(*ast.GoStmt); ok && goStmt == nil {
			goStmt = gs
		}
		return true
	})
	if goStmt == nil {
		t.Fatal("MailYasakHavuzlariniKapatOnStartup bir goroutine başlatmalı (Init'i bloklamamalı)")
	}
	var recoverVar bool
	ast.Inspect(goStmt, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "recover" {
				recoverVar = true
			}
		}
		return true
	})
	if !recoverVar {
		t.Fatal("goroutine recover ile korunmalı: tarama çökerse panel düşmemeli")
	}
	if len(cagriKonumlari(ob, "mailPaylasilanHavuzlariKapat", "")) != 1 {
		t.Fatal("OnStartup işlevi senkron taramayı (mailPaylasilanHavuzlariKapat) tam bir kez çağırmalı")
	}
}
