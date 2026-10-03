package cron

// Zamanlayıcı modeli: kök cron.d satırı biçimi + enjeksiyon reddi, cron '%'
// kuralı, eski spool geçişi (idempotent), askı, silme, çalıştırıcı. Tüm yollar
// geçici dizinlerde (TestMain): canlı /etc/cron.d, /var/spool/cron ve
// /var/lib/girginospanel'e HİÇBİR test dokunamaz.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "cron-test-")
	if err != nil {
		panic(err)
	}
	depoKok = filepath.Join(tmp, "depo")
	askiKok = filepath.Join(tmp, "aski")
	spoolKok = filepath.Join(tmp, "spool")
	cronDKok = filepath.Join(tmp, "cron.d")
	restorecon = func(string) {}
	kullaniciVar = func(sk string) bool { return strings.HasPrefix(sk, "c_var") }
	kod := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(kod)
}

// temizle: test başına boş dizinler.
func temizle(t *testing.T) {
	t.Helper()
	for _, d := range []string{depoKok, askiKok, spoolKok, cronDKok} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func dosya(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gorev(dk, komut string) Gorev {
	return Gorev{Dakika: dk, Saat: "*", Gun: "*", Ay: "*", Hafta: "*", Komut: komut, Etkin: true, Tip: "komut"}
}

func TestZamanGecerli(t *testing.T) {
	for _, f := range []string{"*", "*/5", "0", "59", "1-5", "mon-fri", "MON", "1,15,30", "0-59/10", "jan", "*/15,7"} {
		if !zamanGecerli(f) {
			t.Errorf("%q geçerli olmalı", f)
		}
	}
	for _, f := range []string{"", "* * * * * root id", "1 2", "1;id", "$(id)", "`id`", "*\n", "@reboot", "1-", "-1",
		"1//2", "a b", "1\t2", "root", "*,", ",1", "1/", "100", "%", "1|id", "../x"} {
		if zamanGecerli(f) {
			t.Errorf("%q REDDEDİLMELİ (kök zamanlayıcı satırına girer)", f)
		}
	}
}

// Kiracı zaman alanına boşluklu "* * * * * root <komut> #" yazıp kök satırına
// komut enjekte edemez; komut alanı hiçbir zaman zamanlayıcı dosyasına girmez.
func TestValidate_ZamanEnjeksiyonuReddedilir(t *testing.T) {
	for _, g := range []Gorev{
		{Dakika: "* * * * * root id #", Saat: "*", Gun: "*", Ay: "*", Hafta: "*", Komut: "true"},
		{Dakika: "*", Saat: "*", Gun: "*", Ay: "*", Hafta: "* root id", Komut: "true"},
		{Dakika: "*", Saat: "*\nroot", Gun: "*", Ay: "*", Hafta: "*", Komut: "true"},
	} {
		if err := validate(g); err == nil {
			t.Errorf("validate(%+v) reddetmeliydi", g)
		}
	}
	if err := validate(gorev("*/5", "php artisan schedule:run; rm -rf ~/tmp | tee x")); err != nil {
		t.Errorf("geçerli görev reddedildi: %v", err)
	}
}

func TestCronDIcerik_Bicim(t *testing.T) {
	kotu := gorev("*", "x")
	kotu.Dakika = "1 2" // eski kayıt: boşluklu alan
	pasif := gorev("5", "pasif-komut")
	pasif.Etkin = false
	list := []Gorev{gorev("*/5", "curl 'http://x/?a=1;b' && echo $(id) # root"), pasif, kotu, gorev("0", "true")}
	ic, atlanan := cronDIcerik("c_var_a", list)
	if atlanan != 1 {
		t.Errorf("atlanan = %d, beklenen 1", atlanan)
	}
	s := string(ic)
	for _, yasak := range []string{"curl", "pasif-komut", "$(id)", "true\n"} {
		if strings.Contains(s, yasak) {
			t.Errorf("zamanlayıcı dosyasında kiracı komutu var (%q):\n%s", yasak, s)
		}
	}
	if !strings.Contains(s, "MAILTO=\"\"\n") || !strings.Contains(s, "SHELL=/bin/sh\n") {
		t.Errorf("ortam satırları eksik:\n%s", s)
	}
	var satirlar [][]string
	for _, ln := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.HasPrefix(ln, "#") || strings.Contains(ln, "=") {
			continue
		}
		satirlar = append(satirlar, strings.Fields(ln))
	}
	if len(satirlar) != 2 {
		t.Fatalf("2 görev satırı bekleniyordu:\n%s", s)
	}
	for i, f := range satirlar {
		if len(f) != 10 || f[5] != "root" || f[6] != calistirici || f[7] != CLIKomutu || f[8] != "c_var_a" || !reKimlik.MatchString(f[9]) {
			t.Errorf("satır %d biçimi bozuk: %q", i, f)
		}
		if f[9] != gorevKimlik("c_var_a", list[[]int{0, 3}[i]]) {
			t.Errorf("satır %d kimliği görevle eşleşmiyor", i)
		}
	}
	if ic, _ := cronDIcerik("c_var_a", []Gorev{pasif}); ic != nil {
		t.Errorf("zamanlanacak görev yokken dosya üretilmemeli: %q", ic)
	}
}

func TestYuzdeAyir(t *testing.T) {
	for _, c := range []struct{ in, komut, girdi string }{
		{"date +%Y", "date +", "Y\n"},
		{`date +\%Y`, "date +%Y", ""},
		{"cat%a%b", "cat", "a\nb\n"},
		{`a\b`, `a\b`, ""},
		{"x%", "x", ""},
		{`x%a\%b`, "x", "a%b\n"},
		{"duz komut", "duz komut", ""},
	} {
		k, g := yuzdeAyir(c.in)
		if k != c.komut || g != c.girdi {
			t.Errorf("yuzdeAyir(%q) = (%q,%q), beklenen (%q,%q)", c.in, k, g, c.komut, c.girdi)
		}
	}
}

const eskiSpool = bannerLine + "\n# son güncelleme: c_var_a\n\n" +
	"# gosp-meta: bildirim=hata\n# yedek\n0 3 * * * /home/c_var_a/yedek.sh\n" +
	"# gosp-meta: etkin=0\n#*/5 * * * * pasif-is\n" +
	"*/10 * * * * php /home/c_var_a/cron.php\n"

func TestUzlastir_SpoolGecisiIdempotent(t *testing.T) {
	temizle(t)
	if err := os.WriteFile(filepath.Join(spoolKok, "c_var_a"), []byte(eskiSpool), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ad := range []string{"c_yok_kullanici", "root"} { // kullanıcısı yok / kiracı değil → dokunulmaz
		if err := os.WriteFile(filepath.Join(spoolKok, ad), []byte("* * * * * x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Artık: deposu olmayan hesabın zamanlayıcısı silinir; başka cron.d dosyası kalır.
	_ = os.WriteFile(filepath.Join(cronDKok, cronDOnek+"c_var_artik"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(cronDKok, "0hourly"), []byte("x"), 0o644)

	Uzlastir()
	if varMi(spoolYolu("c_var_a")) {
		t.Fatal("eski spool silinmedi (crond onu sınırsız oturum diliminde koşturur)")
	}
	for _, ad := range []string{"c_yok_kullanici", "root"} {
		if !varMi(filepath.Join(spoolKok, ad)) {
			t.Errorf("%s spool'una dokunulmamalı", ad)
		}
	}
	list, err := read("c_var_a")
	if err != nil || len(list) != 3 {
		t.Fatalf("depo: %d görev, %v (3 bekleniyordu)", len(list), err)
	}
	if list[0].Bildirim != "hata" || list[0].Yorum != "yedek" || list[1].Etkin {
		t.Errorf("meta/yorum/pasiflik korunmadı: %+v", list)
	}
	fi, err := os.Stat(depoYolu("c_var_a"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("depo izni 0600 olmalı: %v %v", fi, err)
	}
	cd := dosya(t, cronDYolu("c_var_a"))
	if strings.Count(cd, " root "+calistirici) != 2 {
		t.Errorf("2 etkin görev satırı bekleniyordu:\n%s", cd)
	}
	if varMi(filepath.Join(cronDKok, cronDOnek+"c_var_artik")) || !varMi(filepath.Join(cronDKok, "0hourly")) {
		t.Error("artık temizliği yanlış")
	}
	depo1 := dosya(t, depoYolu("c_var_a"))
	Uzlastir()
	if dosya(t, depoYolu("c_var_a")) != depo1 || dosya(t, cronDYolu("c_var_a")) != cd {
		t.Error("ikinci uzlaştırma değişiklik yaptı (idempotent değil)")
	}
	// Kiracı crontab(1) ile yeniden spool yaratırsa: yeni görev eklenir, kopya eklenmez.
	_ = os.WriteFile(spoolYolu("c_var_a"), []byte("0 3 * * * /home/c_var_a/yedek.sh\n1 1 * * * yeni-is\n"), 0o600)
	Uzlastir()
	if list, _ := read("c_var_a"); len(list) != 4 || list[3].Komut != "yeni-is" {
		t.Fatalf("birleştirme yanlış: %+v", list)
	}
}

func TestAskiUygula(t *testing.T) {
	temizle(t)
	yazKilit.Lock()
	err := write("c_var_a", []Gorev{gorev("*/5", "is-a")})
	yazKilit.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	once := dosya(t, cronDYolu("c_var_a"))

	for i := 0; i < 2; i++ { // idempotent
		if err := AskiUygula("c_var_a", true); err != nil {
			t.Fatal(err)
		}
	}
	if varMi(cronDYolu("c_var_a")) || varMi(depoYolu("c_var_a")) || !askida("c_var_a") {
		t.Fatal("askıda zamanlayıcı/depo kalmamalı, askı deposu olmalı")
	}
	Uzlastir()
	if varMi(cronDYolu("c_var_a")) {
		t.Fatal("uzlaştırma askıdaki hesabın zamanlayıcısını geri üretti")
	}
	yazKilit.Lock()
	err = write("c_var_a", []Gorev{gorev("*", "x")})
	yazKilit.Unlock()
	if err != errAskida {
		t.Fatalf("askıda yazma reddedilmeli: %v", err)
	}
	// Askıda kiracının yarattığı spool askı deposuna alınır, zamanlanmaz.
	_ = os.WriteFile(spoolYolu("c_var_a"), []byte("2 2 * * * askida-yazilan\n"), 0o600)
	Uzlastir()
	if varMi(cronDYolu("c_var_a")) || varMi(depoYolu("c_var_a")) || varMi(spoolYolu("c_var_a")) {
		t.Fatal("askıdaki spool zamanlandı")
	}
	if err := AskiUygula("c_var_a", false); err != nil {
		t.Fatal(err)
	}
	if askida("c_var_a") {
		t.Fatal("askıdan alınmadı")
	}
	list, _ := read("c_var_a")
	if len(list) != 2 || list[0].Komut != "is-a" || list[1].Komut != "askida-yazilan" {
		t.Fatalf("görevler geri gelmedi: %+v", list)
	}
	if s := dosya(t, cronDYolu("c_var_a")); !strings.HasPrefix(s, strings.SplitN(once, "*/5", 2)[0]) || strings.Count(s, CLIKomutu) != 2 {
		t.Fatalf("zamanlayıcı yeniden üretilmedi:\n%s", s)
	}
	// Görevsiz hesap askıya alınınca boş işaret yazılır (çalıştırıcı reddeder).
	if err := AskiUygula("c_var_b", true); err != nil || !askida("c_var_b") {
		t.Fatalf("görevsiz askı işareti: %v", err)
	}
	if err := AskiUygula("c_var_b", false); err != nil || askida("c_var_b") {
		t.Fatalf("görevsiz askıdan alma: %v", err)
	}
	if varMi(cronDYolu("c_var_b")) {
		t.Fatal("görevsiz hesaba zamanlayıcı yazıldı")
	}
	if AskiUygula("root", true) == nil || AskiUygula("../c_x", true) == nil {
		t.Fatal("kiracı olmayan ad reddedilmeli")
	}
}

func TestSil(t *testing.T) {
	temizle(t)
	yazKilit.Lock()
	_ = write("c_var_a", []Gorev{gorev("*", "x")})
	yazKilit.Unlock()
	_ = os.WriteFile(askiYolu("c_var_a"), nil, 0o600)
	_ = os.WriteFile(spoolYolu("c_var_a"), nil, 0o600)
	Sil("c_var_a")
	for _, p := range []string{cronDYolu("c_var_a"), depoYolu("c_var_a"), askiYolu("c_var_a"), spoolYolu("c_var_a")} {
		if varMi(p) {
			t.Errorf("%s silinmedi", p)
		}
	}
}

func TestCalistirCLI(t *testing.T) {
	temizle(t)
	pasif := gorev("1", "pasif-is")
	pasif.Etkin = false
	list := []Gorev{gorev("*/5", "echo merhaba%girdi"), pasif}
	yazKilit.Lock()
	if err := write("c_var_a", list); err != nil {
		t.Fatal(err)
	}
	yazKilit.Unlock()

	k := kiracitest.Kur(t)
	sahteKiraci(t, "nobody")
	if kod := CalistirCLI([]string{"c_var_a", gorevKimlik("c_var_a", list[0])}); kod != 0 {
		t.Fatalf("çıkış %d", kod)
	}
	// '%' kuralı: komut "echo merhaba", "girdi\n" standart girdi.
	cs := k.SizintiYok(t, "runuser", "-u", "nobody", "--", "/bin/sh", "-c", evdeCalistir, "gosp-cron", "echo merhaba")
	if len(cs) != 1 {
		t.Fatalf("1 çağrı bekleniyordu: %d", len(cs))
	}
	// Pasif / bilinmeyen / değişmiş görev çalışmaz; biçim dışı argüman reddedilir.
	for _, args := range [][]string{
		{"c_var_a", gorevKimlik("c_var_a", pasif)},
		{"c_var_a", "0123456789abcdef"},
		{"c_var_b", gorevKimlik("c_var_b", list[0])},
	} {
		if kod := CalistirCLI(args); kod != 0 {
			t.Errorf("%q: çıkış %d", args, kod)
		}
	}
	for _, args := range [][]string{nil, {"root", "0123456789abcdef"}, {"c_var_a", "x;id"}, {"c_var_a"}, {"../c", "0123456789abcdef"}} {
		if kod := CalistirCLI(args); kod != 2 {
			t.Errorf("%q: biçim dışı argüman 2 dönmeli, %d", args, kod)
		}
	}
	// Askıda: çalıştırıcı çalıştırmaz.
	if err := AskiUygula("c_var_a", true); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(depoYolu("c_var_a"), metin("c_var_a", list), 0o600) // tutarsız durum bile
	if kod := CalistirCLI([]string{"c_var_a", gorevKimlik("c_var_a", list[0])}); kod != 0 {
		t.Fatalf("askıda çıkış %d", kod)
	}
	if n := len(k.Cagrilar(t, "runuser")); n != 1 {
		t.Fatalf("askıda/pasif/bilinmeyen görev çalıştırıldı: %d çağrı", n)
	}
}

// Tutarsız durum (yarım kalmış işlem: hem depo hem askı deposu var): hesabın
// zamanlayıcısı ÜRETİLMEZ — askı her zaman kazanır (derinlemesine savunma;
// Uzlastir'in artık temizliğinden bağımsız).
func TestCronDUzlastir_AskiKazanir(t *testing.T) {
	temizle(t)
	_ = os.WriteFile(depoYolu("c_var_a"), metin("c_var_a", []Gorev{gorev("*", "x")}), 0o600)
	_ = os.WriteFile(askiYolu("c_var_a"), nil, 0o600)
	_ = os.WriteFile(cronDYolu("c_var_a"), []byte("eski"), 0o600)
	if err := cronDUzlastir("c_var_a"); err != nil {
		t.Fatal(err)
	}
	if varMi(cronDYolu("c_var_a")) {
		t.Fatal("askıdaki hesabın zamanlayıcısı kaldı/üretildi")
	}
	// Kullanıcısı olmayan hesap da zamanlanmaz.
	_ = os.WriteFile(depoYolu("c_yok"), metin("c_yok", []Gorev{gorev("*", "x")}), 0o600)
	if err := cronDUzlastir("c_yok"); err != nil || varMi(cronDYolu("c_yok")) {
		t.Fatalf("kullanıcısız hesap zamanlandı: %v", err)
	}
}
