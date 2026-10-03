package maske

import (
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Sahte sır parçaları çalışma anında birleştirilir: kaynakta sır biçimli bir
// dizge durmasın (gitleaks bu test verisini gerçek sır sanıp commit'i durdurur).
func sahte(parcalar ...string) string { return strings.Join(parcalar, "") }

type pozitif struct{ grup, girdi, yasak string }

// pozitifler — her desen grubu için en az bir durum; ilk durum YALNIZ o grubun
// yakaladığı biçimdedir (TestDesenler_TekilKatki bunu kanıtlar).
func pozitifler() []pozitif {
	var (
		ozelBas = sahte("-----BEGIN ", "OPENSSH ", "PRIVATE ", "KEY-----")
		ozelSon = sahte("-----END ", "OPENSSH ", "PRIVATE ", "KEY-----")
		pgpBas  = sahte("-----BEGIN ", "PGP ", "PRIVATE ", "KEY BLOCK-----")
		jwt     = sahte("eyJ", "hbGciOiJIUzI1NiJ9", ".", "eyJ", "zdWIiOiJTQUhURUpXVCJ9", ".", "SAHTEimzaSAHTEimza")
		gosp    = sahte("gosp_", "abcdefghijk2", "_", "SAHTEgizliSAHTEgizliSAHTEgizliSAHTEgizli123")
		tg      = sahte("123456789", ":", "AAH", "SAHTEtelegramSAHTEtelegram123456")
		skAnt   = sahte("sk-", "ant-", "api03-", "SAHTEanthropicSAHTE")
		ghp     = sahte("ghp", "_", "SAHTEgithubSAHTEgithubSAHTEgithub12")
		aws     = sahte("AKIA", "SAHTESAHTESAHTE0")
		age     = sahte("AGE-SECRET-", "KEY-1", strings.Repeat("Q", 40))
		b64     = sahte("base64", ":", "U0FIVEVsYXJhdmVsU0FIVEVsYXJhdmVsU0FIVEVsYXI=")
		kart    = sahte("4111 ", "1111 ", "1111 ", "1111")
	)
	return []pozitif{
		{"ozel-anahtar", ozelBas + " c2FodGUtYW5haHRhcg " + ozelSon, "c2FodGUtYW5haHRhcg"},
		{"ozel-anahtar", ozelBas + "\nc2FodGUtYW5haHRhcg\n" + ozelSon, "c2FodGUtYW5haHRhcg"},
		{"ozel-anahtar", pgpBas + " c2FodGUtcGdw (kesik)", "c2FodGUtcGdw"},
		{"url-kullanici", "dial sftp://kullanici:Gizli123@depo.ornek:22 failed", "Gizli123"},
		{"url-kullanici", "baglanti mysql://wpuser:DsnGizli7@127.0.0.1/wp", "DsnGizli7"},
		{"dsn-tcp", "dsn ali:TcpGizli99@tcp(127.0.0.1:3306)/db?parseTime=true", "TcpGizli99"},
		{"authorization", "Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"cerez-baslik", "Cookie: tercih=koyu; dil=tr", "koyu"},
		{"cerez-baslik", `Set-Cookie: oturum=CerezGizli1; path=/; HttpOnly`, "CerezGizli1"},
		{"bearer", "istek başlığı Bearer sahte.jeton.degeri reddedildi", "sahte.jeton"},
		{"jwt", "jeton " + jwt + " geçersiz", "zdWIiOiJTQUhURUpXVCJ9"},
		{"gosp-api", "anahtar " + gosp + " kullanıldı", "SAHTEgizli"},
		{"telegram", "POST https://api.telegram.org/bot" + tg + "/sendMessage", "SAHTEtelegram"},
		{"telegram", "jeton " + tg, "SAHTEtelegram"},
		{"anthropic", "x-api " + skAnt + " reddedildi", "SAHTEanthropic"},
		{"saglayici-onek", "kopya " + ghp + " sızdı", "SAHTEgithub"},
		{"wp-config", `define( 'DB_PASSWORD', 'Wp-Gizli 123' );`, "Wp-Gizli"},
		{"wp-config", `define('AUTH_SALT', "salt'Gizli\"x");`, "salt'Gizli"},
		{"wp-config", `define( 'NONCE_KEY', 'kesikGizli`, "kesikGizli"},
		{"env-anahtar", "DB_PASSWORD=EnvGizli99", "EnvGizli99"},
		{"env-anahtar", `MAIL_PASSWORD="bosluklu Gizli"`, "Gizli"},
		{"env-anahtar", `'db_pass' => 'DiziGizli5',`, "DiziGizli5"},
		{"env-anahtar", "AWS_SECRET_ACCESS_KEY: YamlGizli8", "YamlGizli8"},
		{"anahtar-deger", "password=hunter2 user=x", "hunter2"},
		{"anahtar-deger", `{"client_secret":"IstemciGizli"}`, "IstemciGizli"},
		{"sifre", "şifre: çokgizli123 yanlış", "çokgizli123"},
		{"oauth-code", "GET /oauth-donus?code=4/0SahteKod&state=xyz", "0SahteKod"},
		{"sorgu", "GET /indir?dlkey=SorguGizli&x=1 HTTP/1.1", "SorguGizli"},
		{"sorgu", "GET /wp-admin/post.php?post=5&_wpnonce=NonceGizli HTTP/1.1", "NonceGizli"},
		{"cerez-ad", "ref PHPSESSID=OturumGizli1 sonu", "OturumGizli1"},
		{"cerez-ad", "c wordpress_logged_in_0123abcd=ali%7CWpOturum sonu", "WpOturum"},
		{"age", "anahtar " + age + " sızdı", "QQQQQQQQQQ"},
		{"aws", "access key " + aws + " geçersiz", aws},
		{"google-erisim", "jeton " + sahte("ya29", ".", "SahteErisim-_") + " geçersiz", "SahteErisim"},
		{"google-yenileme", "jeton " + sahte("1/", "/", "0gSAHTEsahteSAHTE") + " geçersiz", "SAHTEsahte"},
		{"dropbox", "jeton " + sahte("sl", ".", "SAHTEkisaomurlujetondegeri") + " geçersiz", "SAHTEkisaomurlu"},
		{"base64-anahtar", "anahtar " + b64 + " geçersiz", "U0FIVEVs"},
		{"pem-govde", strings.Repeat("U0FIVEVwZW0", 6), "U0FIVEVwZW0"},
		{"kart", "kart " + kart + " reddedildi", "1111 1111"},
		{"kart", "amex " + sahte("3782", "82246310005") + " ok", "82246310005"},
		{"kart", "uzun " + kart + " 22 ok", "1111 1111"},
	}
}

func TestDesenler_Pozitif(t *testing.T) {
	for _, d := range pozitifler() {
		if !strings.Contains(d.girdi, d.yasak) {
			t.Errorf("[%s] test hatası: yasak parça girdide yok: %q", d.grup, d.yasak)
			continue
		}
		if got := MaskeleSatir(d.girdi); strings.Contains(got, d.yasak) {
			t.Errorf("[%s] maskelenmedi:\n girdi: %q\n çıktı: %q", d.grup, d.girdi, got)
		} else if !strings.Contains(got, Isaret) {
			t.Errorf("[%s] işaret yok: %q", d.grup, got)
		}
	}
}

// Her desen grubunun bir pozitifi vardır ve o grup atlanınca en az bir pozitif
// SIZAR: grubun katkısı başka desenle örtülmüyor (silinmesi fark edilir).
func TestDesenler_TekilKatki(t *testing.T) {
	gruplar := map[string][]pozitif{}
	for _, p := range pozitifler() {
		gruplar[p.grup] = append(gruplar[p.grup], p)
	}
	for _, d := range desenler {
		ps := gruplar[d.ad]
		if len(ps) == 0 {
			t.Errorf("[%s] pozitif durumu yok", d.ad)
			continue
		}
		sizdi := false
		for _, p := range ps {
			if strings.Contains(desenleriUygula(p.girdi, d.ad), p.yasak) {
				sizdi = true
			}
		}
		if !sizdi {
			t.Errorf("[%s] atlanınca hiçbir pozitif sızmadı: katkısı başka desenle örtülü", d.ad)
		}
		delete(gruplar, d.ad)
	}
	for g := range gruplar {
		t.Errorf("bilinmeyen grup: %s", g)
	}
}

// normalSatirlar — sır taşımayan gerçekçi log satırları.
var normalSatirlar = []string{
	`203.0.113.7 - - [03/Oct/2026:12:34:56 +0300] "GET /wp-admin/admin-ajax.php?action=heartbeat&page=2 HTTP/2.0" 200 1532 "https://ornek.com/wp-admin/" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"`,
	`198.51.100.23 - - [03/Oct/2026:12:35:01 +0300] "GET /wp-includes/js/jquery/jquery.min.js?ver=3.7.1 HTTP/1.1" 304 0 "-" "curl/8.5.0"`,
	`2026/10/03 12:34:56 [error] 1234#1234: *5678 FastCGI sent in stderr: "PHP message: PHP Warning:  Undefined array key "foo" in /home/ali/public_html/wp-content/themes/x/functions.php on line 42" while reading response header from upstream, client: 203.0.113.7, server: ornek.com, request: "GET /?s=arama HTTP/1.1", upstream: "fastcgi://unix:/run/php-fpm/ornek.com.sock:", host: "ornek.com"`,
	`2026/10/03 12:34:56 [crit] 99#99: *1 connect() to unix:/run/php-fpm/ornek.com.sock failed (2: No such file or directory) while connecting to upstream, client: 2a01:4f8:c17:1234::1, server: ornek.com`,
	`[03-Oct-2026 12:34:56] WARNING: [pool ornek] server reached pm.max_children setting (5), consider raising it`,
	`[03-Oct-2026 12:34:56] NOTICE: [pool www] child 12345 started`,
	`[03-Oct-2026 09:34:56 UTC] PHP Fatal error:  Uncaught Error: Call to undefined function wc_get_product() in /home/ali/public_html/wp-content/plugins/x/x.php:17`,
	`[03-Oct-2026 09:34:56 UTC] PHP Deprecated:  Creation of dynamic property WP_Foo::$bar is deprecated in /home/ali/public_html/wp-includes/class-wp.php on line 9`,
	`WordPress database error Table 'wp_options' doesn't exist for query SELECT option_value FROM wp_options WHERE option_name = 'siteurl'`,
	`Oct 03 12:34:56 sunucu sshd[1234]: Failed password for invalid user admin from 198.51.100.23 port 51234 ssh2`,
	`Oct 03 12:34:56 sunucu systemd[1]: Started nginx.service - A high performance web server and a reverse proxy server.`,
	`Oct 03 12:00:01 sunucu CROND[2345]: (ali) CMD (/usr/bin/php /home/ali/public_html/artisan schedule:run >> /dev/null 2>&1)`,
	`[2026-10-03 12:34:56] production.ERROR: SQLSTATE[HY000] [2002] Connection refused (SQL: select * from users where id = 1) {"exception":"[object] (Illuminate\\Database\\QueryException(code: 2002)"}`,
	`İstek kimliği 550e8400-e29b-41d4-a716-446655440000 zaman 1696000000000 boyut 4294967296 sürüm 1.2.3.4.5`,
	`Oct 03 12:34:56 sunucu kernel: eth0: MAC 52:54:00:12:34:56 link up, saat 12:34:56`,
	"sekmeli\talan\tdeğer",
}

func TestYanlisPozitif_NormalSatirlarBozulmaz(t *testing.T) {
	for _, s := range normalSatirlar {
		if got := MaskeleSatir(s); got != s {
			t.Errorf("normal satır değişti:\n girdi: %q\n çıktı: %q", s, got)
		}
	}
	got := MaskeleSatirlar(normalSatirlar, Secenek{})
	for i := range got {
		if got[i] != normalSatirlar[i] {
			t.Errorf("Satirlar normal satırı değiştirdi: %q", got[i])
		}
	}
}

func TestKisiselVeri_TumSecenekler(t *testing.T) {
	sec := Secenek{IPKirp: true, EpostaMaskele: true, EvOnekSoy: true}
	durumlar := []struct{ girdi, beklenen string }{
		{"client: 203.0.113.77, server: x", "client: 203.0.113.0, server: x"},
		{"from 198.51.100.23 port 22", "from 198.51.100.0 port 22"},
		{"upstream 127.0.0.1:9000 ve 0.0.0.0", "upstream 127.0.0.1:9000 ve 0.0.0.0"},
		{"sürüm 1.2.3.4.5 v1.2.3.4 999.1.1.1 01.2.3.4", "sürüm 1.2.3.4.5 v1.2.3.4 999.1.1.1 01.2.3.4"},
		{"client: 2a01:4f8:c17:1234::1, server", "client: 2a01:4f8:c17::, server"},
		{"[2001:db8:abcd:12::5]:443", "[2001:db8:abcd::]:443"},
		{"tam 2001:0db8:85a3:0042:1000:8a2e:0370:7334 son", "tam 2001:db8:85a3:: son"},
		{"lo ::1 ve ::", "lo ::1 ve ::"},
		{"tam loopback 0:0:0:0:0:0:0:1 son", "tam loopback 0:0:0:0:0:0:0:1 son"},
		{"eşlemeli ::ffff:203.0.113.7", "eşlemeli ::ffff:203.0.113.0"},
		{"saat 12:34:56 MAC 52:54:00:12:34:56 A::B WP_Foo::$bar Db::fetch", "saat 12:34:56 MAC 52:54:00:12:34:56 A::B WP_Foo::$bar Db::fetch"},
		{"gönderen ali.veli@ornek.com.tr alıcı x@y.io", "gönderen a***@ornek.com.tr alıcı x***@y.io"},
		{"git@github.com jquery@3.7.1 user@localhost", "g***@github.com jquery@3.7.1 user@localhost"},
		{"in /home/ali/public_html/x.php on line 3", "in public_html/x.php on line 3"},
		{"chdir(/home/ali) ve /home/", "chdir(~) ve /home/"},
	}
	for _, d := range durumlar {
		if got := MaskeleSatir(d.girdi, sec); got != d.beklenen {
			t.Errorf("kişisel veri:\n girdi:    %q\n beklenen: %q\n çıktı:    %q", d.girdi, d.beklenen, got)
		}
	}
	// Seçenek kapalıyken kişisel veri katmanı çalışmaz.
	for _, d := range durumlar {
		if got := MaskeleSatir(d.girdi); got != d.girdi {
			t.Errorf("seçeneksiz değişti: %q → %q", d.girdi, got)
		}
	}
	// Normal satırlar: yalnız IP/e-posta/ev öneki değişir.
	a := MaskeleSatir(normalSatirlar[0], sec)
	if !strings.HasPrefix(a, "203.0.113.0 - - ") || !strings.Contains(a, "Chrome/129.0.0.0") {
		t.Errorf("access satırı: %q", a)
	}
	e := MaskeleSatir(normalSatirlar[2], sec)
	if strings.Contains(e, "/home/ali") || !strings.Contains(e, "in public_html/wp-content") || !strings.Contains(e, "client: 203.0.113.0,") {
		t.Errorf("error satırı: %q", e)
	}
}

func TestKart_LuhnVeYanlisPozitif(t *testing.T) {
	maskeli := []string{sahte("4111", "111111111111"), sahte("5555-5555", "-5555-4444"), sahte("9792", "030000000000")}
	for _, k := range maskeli {
		if got := MaskeleSatir("k " + k + " s"); got != "k "+Isaret+" s" {
			t.Errorf("kart maskelenmedi: %q → %q", k, got)
		}
	}
	dokunma := []string{
		"4111111111111112",      // Luhn geçersiz
		"1234567812345670",      // Luhn geçerli ama kart öneki değil (1)
		"1696000000000",         // ms zaman damgası
		"41111111111111111111",  // 20 hane
		"411111111111",          // 12 hane
		"x4111111111111111",     // sözcük içinde
		"3 1234 5678 1234 5670", // içteki Luhn geçerli aralık kart önekiyle başlamıyor (1…)
	}
	for _, k := range dokunma {
		if got := MaskeleSatir("k " + k + " s"); got != "k "+k+" s" {
			t.Errorf("kart olmayan maskelendi: %q → %q", k, got)
		}
	}
}

func TestBilinenSirlar(t *testing.T) {
	sir := `p@ss "w0rd"\ ğ/+&=X`
	m := Yeni(Secenek{BilinenSirlar: []string{sir, "kisa", "  ", ""}})
	for _, s := range []string{
		"düz: " + sir + " sonu",
		"GET /x?q=" + url.QueryEscape(sir),
		"yol /" + url.PathEscape(sir),
		`{"alan":"p@ss \"w0rd\"\\ ğ/+&=X"}`,
	} {
		if got := m.Satir(s); strings.Contains(got, "w0rd") {
			t.Errorf("bilinen sır sızdı: %q → %q", s, got)
		}
	}
	// MinBilinenSir altındaki sır sıradan sözcüğü bozmaz.
	if got := m.Satir("kisa bir satır"); got != "kisa bir satır" {
		t.Errorf("kısa sır değiştirildi: %q", got)
	}
	// Bir sır diğerinin öneki: uzun olan tamamen maskelenir.
	m2 := Yeni(Secenek{BilinenSirlar: []string{"abcdefgh", "abcdefghijkl"}})
	if got := m2.Satir("x abcdefghijkl y"); got != "x "+Isaret+" y" {
		t.Errorf("uzun sır önce maskelenmeliydi: %q", got)
	}
	if SirlariDegistir("a abc b", []string{"abc"}, 1) != "a "+Isaret+" b" || SirlariDegistir("a abc b", []string{"abc"}, 8) != "a abc b" {
		t.Error("SirlariDegistir min uzunluğu")
	}
}

// Kanarya: bilinen sır, .env ve wp-config satırları, kodlanmış biçimler ve
// satırlara bölünmüş özel anahtar maske + kesmeden sonra hiçbir parçasıyla geçmez.
func TestKanarya(t *testing.T) {
	kanarya := sahte("KANARYA", "-bilinen-", "7f3a9c2e1b")
	envK := sahte("KANARYAenv", "5d1c")
	wpK := sahte("KANARYAwp", "8e2f")
	keyK := sahte("S0FOQVJZQWtleQ", "KANARYAkey")
	satirlar := []string{
		"hata: bağlantı reddedildi " + kanarya,
		"GET /?x=" + url.QueryEscape(kanarya) + " HTTP/1.1",
		`{"msg":"` + kanarya + `"}`,
		"DB_PASSWORD=" + envK,
		"export REDIS_PASSWORD='" + envK + "'",
		`define( 'DB_PASSWORD', '` + wpK + `' );`,
		`define('SECURE_AUTH_SALT',  '` + wpK + `');`,
		sahte("-----BEGIN ", "RSA PRIVATE ", "KEY-----"),
		keyK,
		"abc " + keyK + " def",
		sahte("-----END ", "RSA PRIVATE ", "KEY-----") + " sonrası normal",
		"normal satır",
	}
	out := KesSatirlar(MaskeleSatirlar(satirlar, Secenek{BilinenSirlar: []string{kanarya}, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true}), 100, 80)
	tum := strings.Join(out, "\n")
	for _, yasak := range []string{"KANARYA", "7f3a9c2e1b", "S0FOQVJZQWtleQ", "5d1c", "8e2f"} {
		if strings.Contains(tum, yasak) {
			t.Errorf("kanarya sızdı (%s):\n%s", yasak, tum)
		}
	}
	if out[len(out)-1] != "normal satır" || !strings.HasSuffix(out[len(out)-2], " sonrası normal") {
		t.Errorf("blok sonrası satırlar bozuldu: %q", out[len(out)-2:])
	}
	if len(out) != len(satirlar) {
		t.Errorf("satır sayısı değişti: %d", len(out))
	}
}

func TestKesSatirlarVeKontrol(t *testing.T) {
	var s []string
	for i := 0; i < 10; i++ {
		s = append(s, strings.Repeat("ğ", i+3))
	}
	got := KesSatirlar(s, 3, 5)
	if len(got) != 3 || got[2] != "ğğğğğ…" || got[0] != "ğğğğğ…" {
		t.Errorf("KesSatirlar: %q", got)
	}
	if len(KesSatirlar(s, 0, 0)) != 10 || s[0] != "ğğğ" {
		t.Error("n<=0 hepsi tutulmalı; girdi değişmemeli")
	}
	if g := MaskeleSatir("a\tb\x00c\x1b[31m\r"); g != "a\tb c [31m " {
		t.Errorf("kontrol karakteri: %q", g)
	}
	if g := MaskeleSatir("geçersiz \xff utf8"); !utf8.ValidString(g) {
		t.Errorf("geçersiz UTF-8 onarılmadı: %q", g)
	}
	if MaskeleSatir("") != "" {
		t.Error("boş satır")
	}
}

// Aşırı uzun satır: sınırda kesilir, kesimden önce kalan sır maskelenir, süre
// doğrusal kalır (ReDoS yok).
func TestUzunSatir(t *testing.T) {
	sir := sahte("UZUN", "satir", "Gizli42")
	s := "password=" + sir + " " + strings.Repeat("a=b&c ", 200_000)
	bas := time.Now()
	got := MaskeleSatir(s, Secenek{BilinenSirlar: []string{sir}, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true})
	if time.Since(bas) > 3*time.Second {
		t.Errorf("uzun satır çok yavaş: %v", time.Since(bas))
	}
	if strings.Contains(got, "Gizli42") || !strings.HasSuffix(got, kesikEk) || len(got) > AzamiGirdi+len(kesikEk) {
		t.Errorf("uzun satır: len=%d son=%q", len(got), got[len(got)-20:])
	}
	// Boşluksuz dev satır: 4 KiB geriden kesilir, sırrın yarısı kalmaz.
	dev := strings.Repeat("Q", AzamiGirdi-10) + sir
	if got := MaskeleSatir(dev); strings.Contains(got, "UZUN") || !strings.HasSuffix(got, kesikEk) {
		t.Errorf("boşluksuz dev satır sınırda sır bıraktı")
	}
	// Kötücül desen adayları (çok sayıda iki nokta / rakam / @) doğrusal.
	kotu := strings.Repeat("1:", 30_000) + strings.Repeat("4 ", 30_000) + strings.Repeat("a@", 20_000)
	bas = time.Now()
	_ = MaskeleSatir(kotu, Secenek{IPKirp: true, EpostaMaskele: true, EvOnekSoy: true})
	if time.Since(bas) > 3*time.Second {
		t.Errorf("kötücül satır çok yavaş: %v", time.Since(bas))
	}
}

func benchSatirlar() []string {
	var out []string
	for len(out) < 2000 {
		out = append(out, normalSatirlar...)
		out = append(out, "DB_PASSWORD=x"+strings.Repeat("y", 10), `define('AUTH_KEY', 'abc');`, "user ali@ornek.com from 203.0.113.9")
	}
	return out[:2000]
}

func BenchmarkMaskeleSatirlar2000(b *testing.B) {
	s := benchSatirlar()
	sec := Secenek{BilinenSirlar: []string{"bilinen-sir-1234", "baska-sir-56789"}, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MaskeleSatirlar(s, sec)
	}
}

// 2000 satır makul sürede (yavaş CI'da da geniş pay).
func TestPerformans2000Satir(t *testing.T) {
	s := benchSatirlar()
	bas := time.Now()
	_ = MaskeleSatirlar(s, Secenek{BilinenSirlar: []string{"bilinen-sir-1234"}, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true})
	// CPU ~0.1 sn; 181 gibi yüklü 2 çekirdekte duvar saati katlanır → geniş pay.
	if d := time.Since(bas); d > 5*time.Second {
		t.Errorf("2000 satır %v sürdü", d)
	}
}
