package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/kapi"
)

func TestKapiAdresURL(t *testing.T) {
	cases := []struct {
		host string
		port int
		bek  string
	}{
		{"panel.ornek.com:8443", 21001, "https://panel.ornek.com:21001"},
		{"1.2.3.4:8443", 21001, "https://1.2.3.4:21001"},
		{"[2001:db8::1]:8443", 21001, "https://[2001:db8::1]:21001"},
		{"panel.ornek.com", 21001, "https://panel.ornek.com:21001"},
		// nginx `Host $host`: varsayılan portta portsuz ama parantezli IPv6.
		{"[2001:db8::1]", 21001, "https://[2001:db8::1]:21001"},
	}
	for _, c := range cases {
		if got := kapiAdresURL(c.host, c.port); got != c.bek {
			t.Errorf("kapiAdresURL(%q,%d)=%q, beklenen %q", c.host, c.port, got, c.bek)
		}
	}
}

// cliOrtami: CLI dikişlerini geçici dizine ve sahtelere çevirir — hiçbir test gerçek
// kapi.json'a, _panel.conf'a, `ss`/firewall'a, semanage'a ya da systemctl'e dokunamaz.
// Dönen sayaç panelYenidenBaslat çağrılarını sayar; SELinux hazırlığı 0 döner (kapalı).
func cliOrtami(t *testing.T) (dir string, yenidenBaslat *int) {
	t.Helper()
	dir = t.TempDir()
	eskiYol, eskiConf, eskiUygun, eskiAta, eskiBaslat, eskiSE :=
		kapiCLIAyarYolu, kapiPanelConf, kapiPortUygunluk, kapiRolPortuAta, panelYenidenBaslat, kapiSELinuxHazirla
	t.Cleanup(func() {
		kapiCLIAyarYolu, kapiPanelConf, kapiPortUygunluk, kapiRolPortuAta, panelYenidenBaslat, kapiSELinuxHazirla =
			eskiYol, eskiConf, eskiUygun, eskiAta, eskiBaslat, eskiSE
	})
	kapiSELinuxHazirla = func() int { return 0 }
	kapiCLIAyarYolu = filepath.Join(dir, "kapi.json")
	kapiPanelConf = filepath.Join(dir, "_panel.conf")
	kapiPortUygunluk = func() func(int) bool { return func(int) bool { return true } }
	kapiRolPortuAta = func(string, int) (int, int, error) {
		t.Fatal("kapiRolPortuAta bu testte beklenmiyordu")
		return 0, 0, nil
	}
	n := 0
	panelYenidenBaslat = func() { n++ }
	return dir, &n
}

// Kullanım hataları dosyaya DOKUNMADAN 2 döner (kurulum betiği yanlış çağırırsa
// kapi.json'u bozmasın) ve paneli yeniden başlatmaz.
func TestKapiCLIKullanimHatalari(t *testing.T) {
	_, baslat := cliOrtami(t)
	for _, args := range [][]string{
		nil,
		{"bilinmeyen"},
		{"gecis", "abc"},
		{"gecis", "0"},
		{"uzat"},
		{"uzat", "0"},
		{"port"},
		{"rol-port"},
		{"rol-port", "root"},
		{"rol-port", "admin", "abc"},
		{"rol-port", "musteri", "0"},
	} {
		if kod := kapiCLI(args); kod != 2 {
			t.Errorf("kapiCLI(%v)=%d, beklenen 2 (kullanım)", args, kod)
		}
	}
	if _, err := os.Stat(kapiCLIAyarYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("kullanım hatası kapi.json yazmamalı: %v", err)
	}
	if *baslat != 0 {
		t.Fatalf("kullanım hatası paneli yeniden başlatmamalı: %d", *baslat)
	}
}

// kur: ayar yoksa yazar; varsa portlar KORUNUR (yeniden kurulum portları değiştirmez).
func TestKapiCLIKurPortlariKorur(t *testing.T) {
	cliOrtami(t)
	if kod := kapiCLI([]string{"durum"}); kod != 3 {
		t.Fatalf("ayar yokken durum 3 dönmeli, got %d", kod)
	}
	if kod := kapiCLI([]string{"kur"}); kod != 0 {
		t.Fatalf("kur 0 dönmeli, got %d", kod)
	}
	a, err := kapi.Oku(kapiCLIAyarYolu)
	if err != nil {
		t.Fatalf("kur ayarı geçici yola yazmalı: %v", err)
	}
	if kod := kapiCLI([]string{"kur"}); kod != 0 {
		t.Fatalf("ikinci kur 0 dönmeli, got %d", kod)
	}
	b, err := kapi.Oku(kapiCLIAyarYolu)
	if err != nil || b.Admin != a.Admin || b.Bayi != a.Bayi || b.Musteri != a.Musteri {
		t.Fatalf("yeniden kurulum portları korumalı: önce %+v sonra %+v err=%v", a, b, err)
	}
	if kod := kapiCLI([]string{"port", kapi.Musteri}); kod != 0 {
		t.Fatalf("port musteri 0 dönmeli, got %d", kod)
	}
}

// gecis: bugünkü genel portlar eski kapı olur; yalnız-loopback (masaüstü) sunucuda
// geçiş atlanır ve ayar YAZILMAZ.
func TestKapiCLIGecis(t *testing.T) {
	cliOrtami(t)
	conf := "server {\n    listen 8443 ssl default_server;\n    listen [::]:8443 ssl default_server;\n}\n"
	if err := os.WriteFile(kapiPanelConf, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if kod := kapiCLI([]string{"gecis", "10"}); kod != 0 {
		t.Fatalf("gecis 0 dönmeli, got %d", kod)
	}
	a, err := kapi.Oku(kapiCLIAyarYolu)
	if err != nil || len(a.EskiPortlar) != 1 || a.EskiPortlar[0] != 8443 {
		t.Fatalf("8443 eski kapı olmalı: %+v err=%v", a, err)
	}

	cliOrtami(t)
	lo := "server {\n    listen 127.0.0.1:9443 ssl;\n}\n"
	if err := os.WriteFile(kapiPanelConf, []byte(lo), 0o644); err != nil {
		t.Fatal(err)
	}
	if kod := kapiCLI([]string{"gecis"}); kod != 0 {
		t.Fatalf("masaüstü modunda gecis hata değil (0), got %d", kod)
	}
	if _, err := os.Stat(kapiCLIAyarYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("masaüstü modunda ayar yazılmamalı: %v", err)
	}

	// İç ağ/VPN adresine bağlı panel: joker rol kapıları bu kısıtlamayı kaldırırdı → atlanır.
	cliOrtami(t)
	vpn := "server {\n    listen 10.8.0.1:8443 ssl;\n}\n"
	if err := os.WriteFile(kapiPanelConf, []byte(vpn), 0o644); err != nil {
		t.Fatal(err)
	}
	if kod := kapiCLI([]string{"gecis"}); kod != 0 {
		t.Fatalf("adrese bağlı panelde gecis hata değil (0), got %d", kod)
	}
	if _, err := os.Stat(kapiCLIAyarYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adrese bağlı panelde ayar yazılmamalı: %v", err)
	}
}

// selinux: çıkış kodu hazırlığınkiyle aynı. kur/gecis/rol-port yazdıktan sonra etiketler;
// Enforcing'de etiketlenememek komutu düşürmez (panel etiketsiz porta listen yazmaz).
func TestKapiCLISELinux(t *testing.T) {
	for _, kod := range []int{0, 1, 3} {
		cliOrtami(t)
		kapiSELinuxHazirla = func() int { return kod }
		if got := kapiCLI([]string{"selinux"}); got != kod {
			t.Errorf("selinux: hazırlık %d → çıkış %d", kod, got)
		}
	}

	_, baslat := cliOrtami(t)
	var sira []string
	kapiSELinuxHazirla = func() int {
		if _, err := os.Stat(kapiCLIAyarYolu); err != nil {
			t.Error("etiketleme kapi.json yazıldıktan SONRA olmalı")
		}
		sira = append(sira, "etiket")
		return 1
	}
	if kod := kapiCLI([]string{"kur"}); kod != 0 || len(sira) != 1 {
		t.Fatalf("kur etiketlemeli ve etiket hatasında da 0 dönmeli: kod=%d etiket=%d", kod, len(sira))
	}
	if kod := kapiCLI([]string{"kur"}); kod != 0 || len(sira) != 1 {
		t.Fatalf("mevcut ayarda kur yeniden etiketlememeli: kod=%d etiket=%d", kod, len(sira))
	}
	kapiRolPortuAta = func(string, int) (int, int, error) { return 23003, 24004, nil }
	panelYenidenBaslat = func() { sira = append(sira, "baslat"); *baslat++ }
	if kod := kapiCLI([]string{"rol-port", kapi.Musteri}); kod != 0 {
		t.Fatalf("rol-port 0 dönmeli, got %d", kod)
	}
	if len(sira) != 3 || sira[1] != "etiket" || sira[2] != "baslat" {
		t.Fatalf("rol-port panel yeniden başlamadan ÖNCE etiketlemeli: %v", sira)
	}

	cliOrtami(t)
	n := 0
	kapiSELinuxHazirla = func() int { n++; return 0 }
	conf := "server {\n    listen 8443 ssl default_server;\n}\n"
	if err := os.WriteFile(kapiPanelConf, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if kod := kapiCLI([]string{"gecis"}); kod != 0 || n != 1 {
		t.Fatalf("gecis etiketlemeli: kod=%d etiket=%d", kod, n)
	}
}

// rol-port: yalnız gerçek bir değişiklikte panel yeniden başlatılır.
func TestKapiCLIRolPortYenidenBaslatma(t *testing.T) {
	cases := []struct {
		ad     string
		hata   error
		kod    int
		baslat int
	}{
		{"degisti", nil, 0, 1},
		{"ayni-port", kapi.ErrAyniPort, 0, 0},
		{"mod-yok", kapi.ErrYok, 3, 0},
		{"hata", errors.New("meşgul"), 1, 0},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			_, baslat := cliOrtami(t)
			var istenen []any
			kapiRolPortuAta = func(rol string, p int) (int, int, error) {
				istenen = append(istenen, rol, p)
				return 21001, 24004, c.hata
			}
			kod := 0
			cikti := stdoutYakala(t, func() { kod = kapiCLI([]string{"rol-port", kapi.Admin, "24004"}) })
			if kod != c.kod {
				t.Fatalf("kod=%d, beklenen %d", kod, c.kod)
			}
			// Belgeli çıktı (cli-envanter.json): "<rol> gate: ESKİ → YENİ" + açık kalan eski yönetici portu.
			if c.ad == "degisti" {
				for _, istenen := range []string{"admin gate: 21001 → 24004", "previous admin port :21001 stays OPEN"} {
					if !strings.Contains(cikti, istenen) {
						t.Errorf("çıktıda %q yok: %q", istenen, cikti)
					}
				}
			}
			if len(istenen) != 2 || istenen[0] != kapi.Admin || istenen[1] != 24004 {
				t.Fatalf("rol/port aynen iletilmeli: %v", istenen)
			}
			if *baslat != c.baslat {
				t.Fatalf("yeniden başlatma=%d, beklenen %d", *baslat, c.baslat)
			}
		})
	}
}

// eski-kapat: admin yeni kapıdan girmeden reddedilir (kilitlenme kilidi); --zorla kapatır.
// uzat: geçici ayarı günceller ve paneli yeniden başlatır.
func TestKapiCLIEskiKapi(t *testing.T) {
	_, baslat := cliOrtami(t)
	conf := "server {\n    listen 8443 ssl default_server;\n}\n"
	if err := os.WriteFile(kapiPanelConf, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if kod := kapiCLI([]string{"gecis", "5"}); kod != 0 {
		t.Fatalf("gecis 0 dönmeli, got %d", kod)
	}
	once, _ := kapi.Oku(kapiCLIAyarYolu)
	if kod := kapiCLI([]string{"uzat", "3"}); kod != 0 || *baslat != 1 {
		t.Fatalf("uzat 0 dönmeli ve bir kez yeniden başlatmalı: kod=%d baslat=%d", kod, *baslat)
	}
	sonra, _ := kapi.Oku(kapiCLIAyarYolu)
	if sonra.EskiBitis <= once.EskiBitis {
		t.Fatalf("uzat bitişi ileri almalı: %d → %d", once.EskiBitis, sonra.EskiBitis)
	}
	if kod := kapiCLI([]string{"eski-kapat"}); kod != 1 || *baslat != 1 {
		t.Fatalf("admin doğrulanmadan eski-kapat reddedilmeli: kod=%d baslat=%d", kod, *baslat)
	}
	if a, _ := kapi.Oku(kapiCLIAyarYolu); len(a.EskiPortlar) == 0 {
		t.Fatal("reddedilen eski-kapat eski portları silmemeli")
	}
	if kod := kapiCLI([]string{"eski-kapat", "--zorla"}); kod != 0 || *baslat != 2 {
		t.Fatalf("--zorla kapatmalı ve yeniden başlatmalı: kod=%d baslat=%d", kod, *baslat)
	}
}

// stdoutYakala: f çalışırken os.Stdout'a yazılanları döndürür (CLI çıktısı sözleşme testleri için).
func stdoutYakala(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	eski := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = eski }()
	f()
	w.Close()
	b, _ := io.ReadAll(r)
	r.Close()
	return string(b)
}

// Betik sözleşmesi: girginospanel-install.sh ve girginospanel-update, `kapi durum` çıktısındaki
// eski-kapı satırını `grep -E '^  (eski|old) '` ile bulur (kurulum/güncelleme özetindeki "eski
// adres açık" uyarısı). kapiYazdir bu satırı üretmeyi bırakırsa uyarı SESSİZCE kaybolur.
func TestKapiYazdir_EskiKapiSatiriBetiklerleUyumlu(t *testing.T) {
	a := &kapi.Ayar{Admin: 21001, Bayi: 21002, Musteri: 21003, EskiPortlar: []int{8443},
		EskiBitis: time.Now().Add(7 * 24 * time.Hour).Unix()}
	cikti := stdoutYakala(t, func() { kapiYazdir(a) })
	if !regexp.MustCompile(`(?m)^  (eski|old) `).MatchString(cikti) {
		t.Fatalf("kapi durum çıktısında '  old ' satırı yok (betikler bunu grep'ler):\n%s", cikti)
	}
	if !strings.Contains(cikti, "8443") {
		t.Errorf("eski kapı satırı portu içermeli:\n%s", cikti)
	}
	for _, yol := range []string{"../../girginospanel-install.sh", "../../assets/ops/girginospanel-update"} {
		b, err := os.ReadFile(yol)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `grep -E '^  (eski|old) '`) {
			t.Errorf("%s: eski-kapı grep kalıbı '^  (eski|old) ' değil — kapi durum çıktısıyla uyumsuz", yol)
		}
	}
}

// kapiHata: bilinen kütüphane hataları CLI'da İngilizce basılır; sarılmış olsalar da çevrilir,
// bilinmeyenler olduğu gibi geçer.
func TestKapiHata_SentinelCevirisi(t *testing.T) {
	for _, c := range []struct {
		err error
		bek string
	}{
		{kapi.ErrYok, "gate mode is not configured"},
		{fmt.Errorf("sarılmış: %w", kapi.ErrAyniPort), "no change"},
		{kapi.ErrEskiYok, "no open old gate"},
		{&kapi.AdminDogrulanmadiHatasi{AdminPort: 21001}, ":21001"},
		{errors.New("bilinmeyen kütüphane hatası"), "bilinmeyen kütüphane hatası"},
	} {
		if g := kapiHata(c.err); !strings.Contains(g, c.bek) {
			t.Errorf("kapiHata(%v)=%q, %q içermeli", c.err, g, c.bek)
		}
	}
}
