package maske

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// tcknUret — 9 haneden geçerli 11 haneli T.C. kimlik no üretir (test yardımcısı).
func tcknUret(d9 string) string {
	var r [11]int
	for i := 0; i < 9; i++ {
		r[i] = int(d9[i] - '0')
	}
	tek := r[0] + r[2] + r[4] + r[6] + r[8]
	cift := r[1] + r[3] + r[5] + r[7]
	r[9] = ((tek*7-cift)%10 + 10) % 10
	for i := 0; i < 10; i++ {
		r[10] += r[i]
	}
	r[10] %= 10
	var b strings.Builder
	for _, v := range r {
		b.WriteByte(byte('0' + v))
	}
	return b.String()
}

// bozTckn — son hanesi değiştirilmiş (sağlaması KESİN bozuk) 11 haneli sayı.
func bozTckn(gecerli string) string {
	son := gecerli[10]
	yeni := byte('0' + (int(son-'0')+1)%10)
	return gecerli[:10] + string(yeni)
}

// bozTckn10 — 10. hanesi bozuk ama 11. hanesi buna göre TUTARLI hesaplanmış sayı:
// yalnız 10. hane sağlaması yakalar.
func bozTckn10(d9 string) string {
	g := tcknUret(d9)
	d10 := (int(g[9]-'0') + 1) % 10
	top := 0
	for i := 0; i < 9; i++ {
		top += int(g[i] - '0')
	}
	return g[:9] + string(rune('0'+d10)) + string(rune('0'+(top+d10)%10))
}

func TestTCKN_SaglamaVeYanlisPozitif(t *testing.T) {
	for _, d9 := range []string{"123456789", "987654321", "246813579"} {
		if b := bozTckn10(d9); tcknGecerli(b) {
			t.Errorf("10. hanesi bozuk sayı geçerli sayıldı: %s", b)
		}
	}
	if !tcknGecerli("10000000146") {
		t.Fatal("bilinen geçerli T.C. kimlik no reddedildi")
	}
	for _, d9 := range []string{"123456789", "987654321", "555555555", "246813579", "100000001"} {
		g := tcknUret(d9)
		if !tcknGecerli(g) {
			t.Fatalf("üretilen geçerli no reddedildi: %s", g)
		}
		if got := MaskeleSatir("tc " + g + " kayıtlı"); got != "tc "+Isaret+" kayıtlı" {
			t.Errorf("geçerli T.C. no maskelenmedi: %q", got)
		}
		b := bozTckn(g)
		if got := MaskeleSatir("sayı " + b + " sonu"); got != "sayı "+b+" sonu" {
			t.Errorf("sağlaması bozuk 11 haneli sayı maskelendi (yanlış pozitif): %q", got)
		}
	}
	// ilk hane 0 geçersiz
	if tcknGecerli("00000000000") || tcknGecerli("01234567890") {
		t.Error("0 ile başlayan kabul edildi")
	}
	// uzun rakam dizisi (zaman damgası/kimlik): içinde geçerli no olsa da dokunulmaz
	g := tcknUret("123456789")
	for _, s := range []string{"ts " + g + "7", "id 9" + g, "1696000000000", "4294967296"} {
		if got := MaskeleSatir(s); got != s {
			t.Errorf("uzun/kısa rakam dizisi değişti: %q → %q", s, got)
		}
	}
	// ayrık parçalar (boşlukla bölünmüş 11 hane) bu sürümde yakalanmaz — belgelenmiş sınır
}

func TestIBAN_ModVeUzunluk(t *testing.T) {
	gecerli := []string{
		"TR33 0006 1005 1978 6457 8413 26", "TR330006100519786457841326",
		"DE89 3704 0044 0532 0130 00", "GB82 WEST 1234 5698 7654 32", "FR14 2004 1010 0505 0001 3M02 606",
		"tr33-0006-1005-1978-6457-8413-26",
	}
	for _, i := range gecerli {
		if got := MaskeleSatir("iban: " + i + " sonra"); got != "iban: "+Isaret+" sonra" {
			t.Errorf("IBAN maskelenmedi: %q → %q", i, got)
		}
	}
	// geçerli IBAN'ın arkasındaki sözcük/sayı yutulmaz
	if got := MaskeleSatir("TR33 0006 1005 1978 6457 8413 26 ve 5 tane dosya"); got != Isaret+" ve 5 tane dosya" {
		t.Errorf("IBAN sonrası metin bozuldu: %q", got)
	}
	gecersiz := []string{
		"TR33 0006 1005 1978 6457 8413 27", // mod-97 bozuk
		"TR33 0006 1005 1978 6457 8413 2",  // TR için kısa (25)
		"GB82 WEST 1234 5698 7654 33",      // mod-97 bozuk
		"AB12 3456 7890 1234 5678",         // biçim uyar, sağlama bozuk
		"sha256 0123456789abcdef",          // IBAN değil
	}
	for _, i := range gecersiz {
		if got := MaskeleSatir("x " + i + " y"); got != "x "+i+" y" {
			t.Errorf("geçersiz IBAN maskelendi (yanlış pozitif): %q → %q", i, got)
		}
	}
}

func TestKullaniciMetni_Pozitifler(t *testing.T) {
	tc := tcknUret("123456789")
	kart := sahte("4111 ", "1111 ", "1111 ", "1111")
	parola := sahte("Gizli", "Parola", "9!")
	durumlar := []struct{ ad, girdi, yasak string }{
		{"tckn", "T.C. kimlik numaram " + tc + " bunu kullan", tc},
		{"tckn-bitisik", "TC" + tc + "dir", tc},
		{"iban", "IBAN TR33 0006 1005 1978 6457 8413 26 hesabıma", "6457 8413"},
		{"kart", "kartım " + kart + " son kullanma 12/30", "1111 1111"},
		{"kart-tire", "kart " + sahte("4111", "-1111", "-1111", "-1111"), "1111-1111"},
		{"tel-intl-tr", "beni ara +90 532 123 45 67 lütfen", "532 123 45 67"},
		{"tel-00", "0090 532 123 45 67", "532 123 45 67"},
		{"tel-0", "05321234567 numarası", "05321234567"},
		{"tel-0-bosluklu", "0532 123 45 67", "123 45 67"},
		{"tel-0-sabit", "0212 345 67 89 sabit", "345 67 89"},
		{"tel-parantez", "(0212) 345 67 89", "345 67 89"},
		{"tel-parantez-cep", "(532) 123 45 67", "123 45 67"},
		{"tel-5xx", "532 123 45 67 ile ulaşın", "123 45 67"},
		{"tel-tire", "532-123-45-67", "123-45-67"},
		{"tel-almanya", "tel +49 170 1234567 dir", "1234567"},
		{"tel-us", "call +1 (415) 555-2671", "555-2671"},
		{"eposta", "mail: ali.veli@ornek.com.tr", "ali.veli"},
		{"ip", "sunucum 203.0.113.77 cevap vermiyor", "203.0.113.77"},
		{"parola-kv", "password=" + parola, parola},
		{"parola-sifre", "şifre: " + parola, parola},
		{"parola-sifrem", "şifrem " + parola + " yanlış mı", parola},
		{"parola-sifrem-iki-nokta", "şifrem: abcdef", "abcdef"},
		{"parola-is", "my password is " + parola, parola},
		{"parola-sifrem-sayi", "parolam 12345678", "12345678"},
		{"jwt", "jeton " + sahte("eyJ", "hbGciOiJIUzI1NiJ9", ".", "eyJ", "zdWIiOiJTQUhURUpXVCJ9", ".", "SAHTEimzaSAHTEimza"), "zdWIiOiJTQUhURUpXVCJ9"},
		{"bearer", "Authorization: Bearer abc.def.ghi", "abc.def.ghi"},
		{"dsn", "mysql://wpuser:DsnGizli7@127.0.0.1/wp", "DsnGizli7"},
		{"ozel-anahtar", sahte("-----BEGIN ", "OPENSSH ", "PRIVATE ", "KEY-----") + "\nc2FodGUtYW5haHRhcg\n" + sahte("-----END ", "OPENSSH ", "PRIVATE ", "KEY-----"), "c2FodGUtYW5haHRhcg"},
	}
	for _, d := range durumlar {
		if !strings.Contains(d.girdi, d.yasak) {
			t.Fatalf("[%s] test hatası: yasak parça girdide yok", d.ad)
		}
		got := KullaniciMetni(d.girdi, nil)
		if strings.Contains(got, d.yasak) {
			t.Errorf("[%s] maskelenmedi:\n girdi: %q\n çıktı: %q", d.ad, d.girdi, got)
		}
		if again := KullaniciMetni(got, nil); again != got {
			t.Errorf("[%s] idempotent değil:\n 1: %q\n 2: %q", d.ad, got, again)
		}
		if KullaniciMetni(d.girdi, nil) != got {
			t.Errorf("[%s] belirlenimci değil", d.ad)
		}
	}
	// e-posta biçimi: yerel kısım ilk harf + ***, alan adı kalır
	if got := KullaniciMetni("yaz: ali.veli@ornek.com.tr", nil); got != "yaz: a***@ornek.com.tr" {
		t.Errorf("e-posta biçimi: %q", got)
	}
	// bilinen sır birebir
	if got := KullaniciMetni("anahtarım "+sahte("birebir", "SirDegeri12"), []string{sahte("birebir", "SirDegeri12")}); strings.Contains(got, "SirDegeri12") {
		t.Errorf("bilinen sır maskelenmedi: %q", got)
	}
	// çok satır: satır sonları korunur, her satır maskelenir
	got := KullaniciMetni("selam\ntel 0532 123 45 67\nşifrem: abcdef\nson", nil)
	if strings.Count(got, "\n") != 3 || strings.Contains(got, "123 45 67") || strings.Contains(got, "abcdef") || !strings.HasPrefix(got, "selam\n") || !strings.HasSuffix(got, "\nson") {
		t.Errorf("çok satırlı metin: %q", got)
	}
}

// Kullanıcı metninde teşhis için gereken sayılar bozulmamalı.
func TestKullaniciMetni_YanlisPozitifKorumasi(t *testing.T) {
	g := tcknUret("123456789")
	dokunma := []string{
		"merhaba, sitem açılmıyor",
		"hata 2026-10-04 12:30:45 civarında başladı",
		"2026-10-04T12:30:45Z",
		"pid 12345 öldü, port 8080 ve 3306 açık",
		"nginx 1.18.0 ve php 8.2.12 sürümü",
		"v10.20.30 ve 1.2.3.4.5",
		"+1 2 3 4 5 6 7 az hane", "+1234567890123456789 çok hane",
		"2147483647 değeri taştı",
		"5000000000 bayt dosya",
		"boyut 4294967296 ve 1696000000000 zaman damgası",
		"04.10.2026 tarihinde",
		"sağlaması bozuk " + bozTckn(g) + " rakamı",
		"bu bir şifre sıfırlama sorunu",
		"şifremi unuttum, panele giremiyorum",
		"password reset çalışmıyor",
		"550e8400-e29b-41d4-a716-446655440000 istek kimliği",
		"12:34:56 saat, MAC 52:54:00:12:34:56",
		"HTTP 502 hatası, 3 kez denedim",
		"/var/log/nginx/error.log dosyasında 200 satır var",
		"PHP_MEMORY_LIMIT 256M ve max_execution_time 300",
		"ornek.com.tr alan adı",
	}
	for _, s := range dokunma {
		if got := KullaniciMetni(s, nil); got != s {
			t.Errorf("yanlış pozitif:\n girdi: %q\n çıktı: %q", s, got)
		}
	}
}

// Araç çıktısı yolunda (MaskeleSatir/DesenleMaskele) telefon ve doğal dil parola KULLANILMAZ.
func TestAracCiktisi_TelefonVeDogalDilDokunulmaz(t *testing.T) {
	dokunma := []string{
		"tel 0532 123 45 67", "+90 532 123 45 67", "532-123-45-67", "şifrem 12345678", "my password is x1",
		"listen 5432 pid 2123456789 worker 0212 345 67 89",
	}
	for _, s := range dokunma {
		if got := MaskeleSatir(s, Secenek{IPKirp: true, EpostaMaskele: true}); got != s {
			t.Errorf("araç yolu kullanıcıya özgü kuralı uyguladı: %q → %q", s, got)
		}
		if got := DesenleMaskele(s); got != s {
			t.Errorf("DesenleMaskele kullanıcıya özgü kuralı uyguladı: %q → %q", s, got)
		}
	}
	// checksum'lı tanımlayıcılar ise araç yolunda DA maskelenir
	g := tcknUret("246813579")
	for _, s := range []string{"owner " + g, "iban TR33 0006 1005 1978 6457 8413 26"} {
		if got := DesenleMaskele(s); !strings.Contains(got, Isaret) || strings.Contains(got, g) || strings.Contains(got, "6457") {
			t.Errorf("araç yolunda sağlamalı tanımlayıcı maskelenmedi: %q → %q", s, got)
		}
	}
}

func rastgeleMetin(r *rand.Rand) string {
	alfabe := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "5", "9", " ", " ", "-", "+", "(", ")", ".", ":", "=", "@",
		"a", "b", "T", "R", "D", "E", "e", "n", "ş", "ı", "\n", "\t", "password", "şifrem", "iban ", "TR33", "eyJ", "***", "tel ", "+90", "0532"}
	n := r.Intn(60)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(alfabe[r.Intn(len(alfabe))])
	}
	return b.String()
}

func TestKullaniciMetni_RastgeleCokme_Idempotent(t *testing.T) {
	r := rand.New(rand.NewSource(20261004))
	for i := 0; i < 30000; i++ {
		s := rastgeleMetin(r)
		got := KullaniciMetni(s, nil)
		if !utf8.ValidString(got) {
			t.Fatalf("geçersiz UTF-8: %q → %q", s, got)
		}
		if again := KullaniciMetni(got, nil); again != got {
			t.Fatalf("idempotent değil:\n girdi: %q\n 1: %q\n 2: %q", s, got, again)
		}
	}
	// rastgele geçerli T.C. no / IBAN yerleştirilen metinde asla sızmaz
	for i := 0; i < 2000; i++ {
		d9 := strconv.Itoa(100000000 + r.Intn(899999999))
		g := tcknUret(d9)
		s := rastgeleMetin(r) + " " + g + " " + rastgeleMetin(r)
		// komşu rakam bulaşırsa (uzun dizi) bu test dışı
		if i := strings.Index(s, g); (i > 0 && rakam(s[i-1])) || (i+11 < len(s) && rakam(s[i+11])) {
			continue
		}
		if got := KullaniciMetni(s, nil); strings.Contains(got, g) {
			t.Fatalf("geçerli T.C. no sızdı: %q → %q", s, got)
		}
	}
}

func FuzzKullaniciMetni(f *testing.F) {
	for _, s := range []string{"", "tel +90 532 123 45 67", "şifrem: x", "TR33 0006 1005 1978 6457 8413 26", "10000000146", "a@b.cd", "\x00\xff", strings.Repeat("5", 70)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a := KullaniciMetni(s, nil)
		if !utf8.ValidString(a) {
			t.Fatalf("geçersiz UTF-8")
		}
		if b := KullaniciMetni(a, nil); b != a {
			t.Fatalf("idempotent değil: %q → %q → %q", s, a, b)
		}
	})
}
