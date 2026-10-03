package provisioner

// panelkapi_test.go — kapı Heal uzlaştırması (ADR-GP-001). Gerçek dosyalar geçici
// dizinde; nginx / firewall / port / gizli sahte ve KAYIT eden dikişlerle.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/kapi"
)

const kapiTestSablon = `server {
    listen 8443 ssl default_server;
    listen [::]:8443 ssl default_server;
    server_name _;
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
    }
}`

const kapiTestCanli = `server {
    listen 127.0.0.1:9443 ssl;
    listen 8443 ssl;
    listen [::]:8443 ssl;
    # [erisim:masaustu] listen [::]:9443 ssl;
    server_name ornek.test localhost 127.0.0.1;
    location ~ ^/api/v1/(auth|musteri)/login$ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
    }
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
    }
}`

const kapiTestCatchall = `server {
    listen 8443 ssl default_server;
    listen [::]:8443 ssl default_server;
    server_name _;
    return 444;
}`

// kapiTestGizli: testlerin proxy gizlisi (üretimde 64 hex karakter).
// Çalışma anında üretilir: depoda gizli-benzeri sabit dize bulunmasın (gitleaks).
var kapiTestGizli = strings.Repeat("0123456789abcdef", 4)

type kapiSahte struct {
	dir                 string
	nginxT              func(n int) error // n: kaçıncı `nginx -t` çağrısı (1'den)
	tSayac              int
	reloadHata          func(n int) error // n: kaçıncı `systemctl reload nginx` çağrısı
	rSayac              int
	cagrilar            []string
	fwAcilan, fwKapanan []int
	ozelIzin            []string // kapiOzelIzin'e verilen yollar
	dinliyor            bool
	masaustu            bool
	gizli               string
}

func kapiTestKur(t *testing.T, conf string, ayar *kapi.Ayar) *kapiSahte {
	t.Helper()
	s := &kapiSahte{dir: t.TempDir(), dinliyor: true, gizli: kapiTestGizli}
	eskiVhost, eskiCatch, eskiMap, eskiAyar := kapiVhostYolu, kapiCatchallYolu, kapiMapYolu, kapiAyarYolu
	eskiAcme, eskiYay, eskiMarkaAcme := kapiAcmeYolu, KapiGenelPortUzlastir, kapiMarkaAcmeYolu
	eskiKomut, eskiAc, eskiKapat := kapiNginxKomut, kapiFwAc, kapiFwKapat
	eskiDinl, eskiGizli, eskiIzin, eskiAktif := kapiPortDinliyor, kapiProxyGizli, kapiVhostIzin, kapiNginxAktif
	eskiMasa, eskiOzel := kapiMasaustu, kapiOzelIzin
	eskiSEMod, eskiSEListe, eskiSEEtiket, eskiEtiketli := kapiSELinuxModu, kapiSELinuxListe, kapiSELinuxEtiket, kapiEtiketli
	eskiZDosya, eskiOnb, eskiOkundu := kapiZorlamaDosyasi, kapiSELinuxOnbellek, kapiOnbellekOkundu
	kapiKararAyarla := func(v bool) {
		kapiZorlamaMu.Lock()
		kapiAcilabilir = v
		kapiZorlamaMu.Unlock()
	}
	kapiZorlamaMu.Lock()
	eskiKarar := kapiAcilabilir
	kapiZorlamaMu.Unlock()
	// Her test "henüz uzlaştırma yok" ile başlar: ölçüm, uzlaştırma açılabilir demeden açamaz.
	kapiKararAyarla(false)
	t.Cleanup(func() {
		kapiKararAyarla(eskiKarar)
		kapiSELinuxOnbellek, kapiOnbellekOkundu = eskiOnb, eskiOkundu
		kapiVhostYolu, kapiCatchallYolu, kapiMapYolu, kapiAyarYolu = eskiVhost, eskiCatch, eskiMap, eskiAyar
		kapiAcmeYolu, KapiGenelPortUzlastir, kapiMarkaAcmeYolu = eskiAcme, eskiYay, eskiMarkaAcme
		kapiNginxKomut, kapiFwAc, kapiFwKapat = eskiKomut, eskiAc, eskiKapat
		kapiPortDinliyor, kapiProxyGizli, kapiVhostIzin, kapiNginxAktif = eskiDinl, eskiGizli, eskiIzin, eskiAktif
		kapiMasaustu, kapiOzelIzin = eskiMasa, eskiOzel
		kapiSELinuxModu, kapiSELinuxListe, kapiSELinuxEtiket, kapiEtiketli = eskiSEMod, eskiSEListe, eskiSEEtiket, eskiEtiketli
		kapiZorlamaDosyasi = eskiZDosya
		kapi.ZorlamaAyarla(false)
	})
	kapiNginxAktif = func() bool { return true }
	// SELinux kapalı varsayılır; semanage asla gerçekten çalışmaz.
	kapiSELinuxModu = func() string { return "" }
	kapiSELinuxListe = func() (string, error) { t.Error("semanage listesi beklenmiyordu"); return "", nil }
	kapiSELinuxEtiket = func(int) error { t.Error("semanage etiketi beklenmiyordu"); return nil }
	kapiEtiketli = map[int]bool{}
	kapiZorlamaDosyasi = filepath.Join(s.dir, "run", "kapi-zorlama")
	kapiSELinuxOnbellek, kapiOnbellekOkundu = filepath.Join(s.dir, "run", "kapi-selinux"), false
	// 🔴 HER yol geçici dizine: kapiNginxUygula dosya YAZAR; gerçek /etc yolunda kalan
	// tek bir dikiş testi canlı nginx dosyasını değiştirirdi.
	kapiVhostYolu = filepath.Join(s.dir, "_panel.conf")
	kapiCatchallYolu = filepath.Join(s.dir, "_panel_catchall.conf")
	kapiAcmeYolu = filepath.Join(s.dir, "_panel_acme.conf")
	kapiMarkaAcmeYolu = filepath.Join(s.dir, "_panel_marka_acme.conf")
	KapiGenelPortUzlastir = nil
	kapiMapYolu = filepath.Join(s.dir, "00-gosp-kapi.conf")
	kapiAyarYolu = filepath.Join(s.dir, "kapi.json")
	kapiNginxKomut = func(ad string, arg ...string) ([]byte, error) {
		c := ad + " " + strings.Join(arg, " ")
		s.cagrilar = append(s.cagrilar, c)
		switch c {
		case "nginx -t":
			s.tSayac++
			if s.nginxT != nil {
				if err := s.nginxT(s.tSayac); err != nil {
					return []byte("nginx: [emerg] test"), err
				}
			}
		case "systemctl reload nginx":
			s.rSayac++
			if s.reloadHata != nil {
				if err := s.reloadHata(s.rSayac); err != nil {
					return []byte("Job for nginx.service failed"), err
				}
			}
		}
		return nil, nil
	}
	kapiFwAc = func(p int) error { s.fwAcilan = append(s.fwAcilan, p); return nil }
	kapiFwKapat = func(p int) error { s.fwKapanan = append(s.fwKapanan, p); return nil }
	kapiPortDinliyor = func(int) bool { return s.dinliyor }
	kapiProxyGizli = func() string { return s.gizli }
	kapiMasaustu = func() bool { return s.masaustu }
	kapiVhostIzin = func() {}
	// Üretimdeki gibi izni gerçekten daraltır (chgrp testte yok): tekrar çağrılar ölçülebilsin.
	kapiOzelIzin = func(yol string) { s.ozelIzin = append(s.ozelIzin, yol); _ = os.Chmod(yol, 0o640) }
	if err := os.WriteFile(kapiVhostYolu, []byte(conf), 0o640); err != nil {
		t.Fatal(err)
	}
	if ayar != nil {
		if err := kapi.Yaz(kapiAyarYolu, ayar); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func oku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", yol, err)
	}
	return string(b)
}

func iceriyor(liste []int, p int) bool {
	for _, x := range liste {
		if x == p {
			return true
		}
	}
	return false
}

func rolAyar() *kapi.Ayar { return &kapi.Ayar{Admin: 21001, Bayi: 22002, Musteri: 23003} }

func TestKapiHealTazeKurulum(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) {
		t.Fatal("taze kurulumda zorlama açılabilir olmalı")
	}
	conf := oku(t, kapiVhostYolu)
	for _, p := range []string{"21001", "22002", "23003"} {
		if !strings.Contains(conf, "listen "+p+" ssl;") {
			t.Errorf("rol listen'ı eksik: %s\n%s", p, conf)
		}
		pi, _ := strconv.Atoi(p)
		if !iceriyor(s.fwAcilan, pi) {
			t.Errorf("firewall rol portunu açmadı: %s", p)
		}
	}
	if strings.Contains(conf, "8443") {
		t.Errorf("taze kurulumda şablonun 8443'ü kaldırılmalı:\n%s", conf)
	}
	if !iceriyor(s.fwKapanan, 8443) {
		t.Error("kaldırılan 8443 firewall'da kapatılmalı")
	}
	if !kapi.BaslikTamMi(conf, kapiTestGizli) {
		t.Error("kapı başlığı eksik")
	}
	if m := oku(t, kapiMapYolu); !strings.Contains(m, "21001 admin;") || !strings.Contains(m, "default bilinmeyen;") {
		t.Errorf("map eksik:\n%s", m)
	}
}

// Kapı haritası rol etiketlerini taşır: yeni dosya 0640 doğar, var olan dosyanın izni
// de (WriteFile değiştirmez) açıkça daraltılır.
func TestKapiHealHaritaOzelIzin(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiUzlastir(1000)
	if fi, err := os.Stat(kapiMapYolu); err != nil || fi.Mode().Perm() != 0o640 {
		t.Fatalf("yeni harita 0640 doğmalı: %v %v", fi.Mode().Perm(), err)
	}
	if len(s.ozelIzin) != 1 || s.ozelIzin[0] != kapiMapYolu {
		t.Fatalf("harita yazılınca izin daraltılmalı: %v", s.ozelIzin)
	}
	// Eski sürümün 0644 yazdığı harita: içerik değişince izni de daraltılır.
	s2 := kapiTestKur(t, kapiTestSablon, rolAyar())
	if err := os.WriteFile(kapiMapYolu, []byte("# eski\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	if len(s2.ozelIzin) != 1 || s2.ozelIzin[0] != kapiMapYolu {
		t.Fatalf("var olan harita da daraltılmalı: %v", s2.ozelIzin)
	}
	// İçerik AYNI ama izin 0644 (önceki sürümün yazdığı dosya, güncellemeden sonra): dosya
	// yeniden yazılmasa da izin daraltılmalı — canlı test VPS'inde yakalanan durum.
	s3 := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiUzlastir(1000)
	if err := os.Chmod(kapiMapYolu, 0o644); err != nil {
		t.Fatal(err)
	}
	once := oku(t, kapiMapYolu)
	s3.ozelIzin = nil
	kapiUzlastir(1000)
	if oku(t, kapiMapYolu) != once {
		t.Fatal("ön koşul: harita içeriği değişmemeli")
	}
	if fi, err := os.Stat(kapiMapYolu); err != nil || fi.Mode().Perm() != 0o640 || len(s3.ozelIzin) != 1 {
		t.Fatalf("içerik aynıyken de geniş izin daraltılmalı: izin=%v çağrı=%v err=%v", fi.Mode().Perm(), s3.ozelIzin, err)
	}
	// İzin zaten doğruysa her turda tekrar chgrp/chmod koşmaz.
	s3.ozelIzin = nil
	kapiUzlastir(1000)
	if len(s3.ozelIzin) != 0 {
		t.Fatalf("izin doğruyken tekrar dokunulmamalı: %v", s3.ozelIzin)
	}
}

// Masaüstü modunda (yönetici yüzeyi internete kapalı) yeni admin kapısı da yalnız
// 127.0.0.1'de dinler ve firewall'da AÇILMAZ — admin portu değişince operatörün bilinçli
// kapatması sessizce geri alınmasın. Bayi/müşteri etkilenmez.
func TestKapiHealMasaustuAdminLoopback(t *testing.T) {
	s := kapiTestKur(t, kapiTestCanli, rolAyar())
	s.masaustu = true
	kapiUzlastir(1000)
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "listen 127.0.0.1:21001 ssl; # gp-kapi") || strings.Contains(conf, "listen 21001 ssl;") {
		t.Fatalf("admin kapısı yalnız loopback'te dinlemeli:\n%s", conf)
	}
	if strings.Contains(conf, "\n    listen [::]:21001") {
		t.Fatalf("admin IPv6 satırı etkin OLMAMALI:\n%s", conf)
	}
	if iceriyor(s.fwAcilan, 21001) {
		t.Fatal("masaüstü modunda admin portu firewall'da açılmamalı")
	}
	if !strings.Contains(conf, "listen 22002 ssl;") || !iceriyor(s.fwAcilan, 23003) {
		t.Fatal("bayi/müşteri kapıları genel kalmalı")
	}
}

func TestKapiHealGecisEskiKorunur(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 5000
	s := kapiTestKur(t, kapiTestCanli, a)
	if !kapiUzlastir(1000) {
		t.Fatal("geçişte zorlama açılabilir olmalı")
	}
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "listen 8443 ssl;") || !strings.Contains(conf, "listen [::]:8443 ssl;") {
		t.Error("geçiş süresince eski 8443 KORUNMALI")
	}
	if len(s.fwKapanan) != 0 {
		t.Errorf("geçişte hiçbir port kapatılmamalı, kapanan=%v", s.fwKapanan)
	}
	if !iceriyor(s.fwAcilan, 8443) {
		t.Error("açık eski port firewall'da açık tutulmalı")
	}
	if m := oku(t, kapiMapYolu); !strings.Contains(m, "8443 eski;") {
		t.Errorf("eski port haritada olmalı:\n%s", m)
	}
	// Yalnız-loopback 9443 (SSH tüneli yolu) yönetici kapısı olarak haritada kalmalı.
	if m := oku(t, kapiMapYolu); !strings.Contains(m, "9443 admin;") {
		t.Errorf("tünel portu haritada yönetici kapısı olmalı:\n%s", m)
	}
	if strings.Count(conf, kapi.Baslik) != 2 {
		t.Error("iki çapanın ikisine de başlık gerekli")
	}
	// erisim'in loopback'e çektiği 9443'e dokunulmaz.
	if !strings.Contains(conf, "listen 127.0.0.1:9443 ssl;") || !strings.Contains(conf, "# [erisim:masaustu]") {
		t.Error("erisim satırları korunmalı")
	}
}

func TestKapiHealSureDolduAdminDogrulamadi(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 500 // süre dolmuş
	kapiTestKur(t, kapiTestCanli, a)
	kapiUzlastir(1000)
	if !strings.Contains(oku(t, kapiVhostYolu), "listen 8443 ssl;") {
		t.Fatal("admin yeni kapıdan girmeden eski kapı KAPANMAMALI (dış firewall kilitlenme kilidi)")
	}
	if b, _ := kapi.Oku(kapiAyarYolu); len(b.EskiPortlar) != 1 {
		t.Fatal("eski portlar ayarda kalmalı")
	}
}

func TestKapiHealSureDolduAdminDogruladi(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis, a.AdminDogrulandi = []int{8443}, 500, 400
	s := kapiTestKur(t, kapiTestCanli, a)
	if !kapiUzlastir(1000) {
		t.Fatal("zorlama açılabilir olmalı")
	}
	conf := oku(t, kapiVhostYolu)
	if strings.Contains(conf, "listen 8443 ssl;") || strings.Contains(conf, "listen [::]:8443 ssl;") {
		t.Fatalf("süre doldu + admin doğruladı → 8443 kaldırılmalı:\n%s", conf)
	}
	if !iceriyor(s.fwKapanan, 8443) {
		t.Error("8443 firewall'da kapatılmalı")
	}
	if b, _ := kapi.Oku(kapiAyarYolu); len(b.EskiPortlar) != 0 || b.EskiBitis != 0 {
		t.Errorf("geçiş tamamlanınca eski portlar ayardan düşülmeli: %+v", b)
	}
}

// reload 0 döndü ama nginx yeni portları bağlamadı (SELinux etiketi, port çakışması):
// dosyada kalan yapılandırmayla sonraki nginx restart'ı HİÇ açılmazdı → geri alınır,
// hiçbir port kaldırılmaz, firewall'a dokunulmaz.
func TestKapiHealPortlarBaglanmazsaGeriAlinir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	yanDosyalariKur(t)
	s.dinliyor = false
	if kapiUzlastir(1000) {
		t.Fatal("portlar bağlanmadıysa zorlama açılmamalı")
	}
	if oku(t, kapiVhostYolu) != kapiTestSablon || oku(t, kapiCatchallYolu) != kapiTestCatchall {
		t.Fatal("yüklenemeyen yapılandırma ÖZGÜN haline geri alınmalı")
	}
	if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("yeni map dosyası geri almada silinmeli")
	}
	if s.rSayac != 2 {
		t.Fatalf("uygula + geri alma için 2 reload beklenir, got %d", s.rSayac)
	}
	if len(s.fwAcilan) != 0 || len(s.fwKapanan) != 0 {
		t.Fatalf("firewall'a dokunulmamalı: açılan=%v kapanan=%v", s.fwAcilan, s.fwKapanan)
	}
}

// Kaldırma aşaması admin kapısının GERÇEKTEN dinlediği ölçülmeden koşmaz (son çalışan
// portu kapatmak herkesi kilitlerdi). Burada ekleme doğrulandı ama kaldırma anında admin
// kapısı düşmüş: hiçbir port kaldırılmaz.
func TestKapiHealAdminDinlemiyorKaldirmaYok(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	n := 0
	kapiPortDinliyor = func(int) bool { n++; return n <= 3 } // 3 rol portu doğrulanır, sonra düşer
	kapiUzlastir(1000)
	if !strings.Contains(oku(t, kapiVhostYolu), "listen 8443 ssl default_server;") {
		t.Fatal("admin kapısı dinlemiyorken HİÇBİR port kaldırılmamalı (son çalışan port)")
	}
	if len(s.fwKapanan) != 0 {
		t.Fatalf("firewall'da port kapatılmamalı: %v", s.fwKapanan)
	}
	if n != 4 {
		t.Fatalf("kaldırmadan önce admin kapısı yeniden ölçülmeli (4 ölçüm), got %d", n)
	}
}

// SELinux Enforcing: etiketsiz kapı portları listen yazılmadan ÖNCE http_port_t yapılır;
// başarılı etiket sonrası sonraki uzlaştırmalar semanage'i yeniden koşturmaz.
func TestKapiHealSELinuxEtiketler(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiSELinuxModu = func() string { return "enforcing" }
	liste := 0
	kapiSELinuxListe = func() (string, error) {
		liste++
		return "http_port_t tcp 80, 443, 8443\nunreserved_port_t tcp 1024-32767\n", nil
	}
	var etiket []int
	kapiSELinuxEtiket = func(p int) error {
		if _, err := os.Stat(kapiMapYolu); err == nil {
			t.Error("etiket nginx dosyaları yazılmadan ÖNCE atılmalı")
		}
		etiket = append(etiket, p)
		return nil
	}
	if !kapiUzlastir(1000) {
		t.Fatal("etiketlenen portlarla uzlaştırma açılabilir dönmeli")
	}
	if fmt.Sprint(etiket) != "[21001 22002 23003]" {
		t.Fatalf("üç rol portu etiketlenmeli, got %v", etiket)
	}
	kapiUzlastir(1000)
	if liste != 1 || len(etiket) != 3 {
		t.Fatalf("doğrulanan etiketler önbellekte: liste=%d etiket=%v", liste, etiket)
	}
}

// Enforcing + etiketlenemeyen port (başka servise ayrılmış tür ya da semanage hatası):
// nginx'e HİÇ yazılmaz — restart'ta açılmayan nginx tüm siteleri düşürürdü.
func TestKapiHealSELinuxEtiketlenemezseYazilmaz(t *testing.T) {
	for ad, ayar := range map[string]struct {
		liste string
		hata  error
	}{
		"başka servisin portu": {"mountd_port_t tcp 21001\nunreserved_port_t tcp 1024-32767\n", nil},
		"semanage hatası":      {"unreserved_port_t tcp 1024-32767\n", errors.New("ValueError")},
	} {
		t.Run(ad, func(t *testing.T) {
			s := kapiTestKur(t, kapiTestSablon, rolAyar())
			kapiSELinuxModu = func() string { return "enforcing" }
			kapiSELinuxListe = func() (string, error) { return ayar.liste, nil }
			kapiSELinuxEtiket = func(int) error { return ayar.hata }
			if kapiUzlastir(1000) {
				t.Fatal("etiketlenemeyen portla zorlama açılmamalı")
			}
			if len(s.cagrilar) != 0 || oku(t, kapiVhostYolu) != kapiTestSablon {
				t.Fatalf("nginx'e dokunulmamalı: %v", s.cagrilar)
			}
		})
	}
	// Permissive'de bind serbesttir: etiket hatası uygulamayı engellemez.
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiSELinuxModu = func() string { return "permissive" }
	kapiSELinuxListe = func() (string, error) { return "", errors.New("semanage yok") }
	if !kapiUzlastir(1000) || !strings.Contains(oku(t, kapiVhostYolu), "listen 21001 ssl;") || s.rSayac == 0 {
		t.Fatal("permissive'de etiket hatası uygulamayı engellememeli")
	}
}

// Yeni etiket atıldıysa dosyalar değişmese de reload edilir: önceden bind edilemeyen
// (etiketsiz) port nginx'e ancak böyle bağlanır.
func TestKapiHealSELinuxYeniEtiketReloadEder(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiUzlastir(1000) // SELinux kapalıyken uygulandı (dosyalar yerinde)
	oncekiReload := s.rSayac
	kapiSELinuxModu = func() string { return "enforcing" }
	kapiSELinuxListe = func() (string, error) { return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(int) error { return nil }
	if !kapiUzlastir(1000) {
		t.Fatal("uzlaştırma açılabilir dönmeli")
	}
	if s.rSayac != oncekiReload+1 {
		t.Fatalf("yeni etiket sonrası dosya değişmese de reload beklenir: %d → %d", oncekiReload, s.rSayac)
	}
}

// Zorlama durumu sistem araçları için dosyaya yansır (erisim masaüstü modu bakar).
func TestKapiZorlamaDosyasi(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiZorlamaYaz(true, "test")
	if oku(t, kapiZorlamaDosyasi) != "acik\n" {
		t.Fatal("zorlama açık dosyaya yansımalı")
	}
	kapiZorlamaYaz(false, "test")
	if oku(t, kapiZorlamaDosyasi) != "kapali\n" {
		t.Fatal("zorlama kapalı dosyaya yansımalı")
	}
}

// Yarıda kesilen ölçüm (istemci koptu / kapanış) kesin değildir: zorlamayı KAPATMAZ.
func TestKapiOlcIptalZorlamayiDegistirmez(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) { // ölçüm yalnız "açılabilir" kararından sonra karar verir
		t.Fatal("ön koşul: uzlaştırma açılabilir")
	}
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	kapiTipOlcucu = func(ctx context.Context, _ int, _ string, _ int) (string, error) { return "", ctx.Err() }
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	kapi.ZorlamaAyarla(true)
	kapiOlcVeAyarla(ctx, 1)
	if !kapi.ZorlamaAktif() {
		t.Fatal("iptal edilen ölçüm zorlamayı kapatmamalı")
	}
}

const kapiTestAcme = "server {\n    listen 80;\n    location / {\n        return 301 https://$server_name:8443$request_uri;\n    }\n}"

// yanDosyalariKur: catchall + acme (panelhost) dosyalarını yazar — geri alma testleri
// yalnız _panel.conf'u değil, aynı işlemdeki yan dosyaları da denetlesin.
func yanDosyalariKur(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(kapiCatchallYolu, []byte(kapiTestCatchall), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kapiAcmeYolu, []byte(kapiTestAcme), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKapiHealNginxTestiDuserseGeriAlinir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	yanDosyalariKur(t)
	s.nginxT = func(n int) error {
		if n == 2 { // yazma SONRASI ilk test
			return errors.New("emerg")
		}
		return nil
	}
	if kapiUzlastir(1000) {
		t.Fatal("nginx -t düşünce zorlama açılmamalı")
	}
	if oku(t, kapiVhostYolu) != kapiTestSablon {
		t.Fatal("_panel.conf ÖZGÜN haline geri alınmalı")
	}
	if oku(t, kapiCatchallYolu) != kapiTestCatchall || oku(t, kapiAcmeYolu) != kapiTestAcme {
		t.Fatal("aynı işlemdeki catchall/acme de ÖZGÜN haline geri alınmalı")
	}
	if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("önceden olmayan map dosyası geri almada SİLİNMELİ")
	}
	for _, c := range s.cagrilar {
		if strings.HasPrefix(c, "systemctl reload") {
			t.Fatal("başarısız doğrulamadan sonra reload yapılmamalı")
		}
	}
	if len(s.fwAcilan) != 0 {
		t.Fatalf("uygulanamayan değişiklik için firewall'da port açılmamalı: %v", s.fwAcilan)
	}
}

// reload düşerse (ör. yeni portu başka süreç tutuyor) dosyalar geri alınır ve eski
// yapılandırma yeniden yüklenir; firewall'a dokunulmaz.
func TestKapiHealReloadDuserseGeriAlinir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	yanDosyalariKur(t)
	s.reloadHata = func(n int) error {
		if n == 1 {
			return errors.New("reload failed")
		}
		return nil
	}
	if kapiUzlastir(1000) {
		t.Fatal("reload düşünce zorlama açılmamalı")
	}
	if oku(t, kapiVhostYolu) != kapiTestSablon || oku(t, kapiCatchallYolu) != kapiTestCatchall || oku(t, kapiAcmeYolu) != kapiTestAcme {
		t.Fatal("reload hatasında tüm dosyalar ÖZGÜN haline dönmeli")
	}
	if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("önceden olmayan map dosyası silinmeli")
	}
	if s.rSayac != 2 {
		t.Fatalf("geri alınan yapılandırma yeniden yüklenmeli (2 reload), got %d", s.rSayac)
	}
	if len(s.fwAcilan) != 0 || len(s.fwKapanan) != 0 {
		t.Fatalf("firewall'a dokunulmamalı: açılan=%v kapanan=%v", s.fwAcilan, s.fwKapanan)
	}
}

// 2. aşama (kaldırma) düşerse yalnız O aşama geri alınır: 1. aşamanın eklediği rol
// kapıları kalır, kaldırılamayan port firewall'da KAPATILMAZ (nginx hâlâ dinliyor).
func TestKapiHealKaldirmaDuserseGeriAlinir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	yanDosyalariKur(t)
	s.nginxT = func(n int) error {
		if n == 4 { // 1: taban, 2: aşama 1, 3: aşama 2 tabanı, 4: aşama 2 yazımı
			return errors.New("emerg")
		}
		return nil
	}
	if !kapiUzlastir(1000) {
		t.Fatal("1. aşama uygulandı; kaldırmanın düşmesi zorlamayı engellememeli")
	}
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "listen 8443 ssl default_server;") || !strings.Contains(conf, "listen 21001 ssl;") {
		t.Fatalf("kaldırma geri alınmalı, eklenen rol kapıları KALMALI:\n%s", conf)
	}
	c := oku(t, kapiCatchallYolu)
	if !strings.Contains(c, "listen 8443 ssl default_server;") || !strings.Contains(c, "listen 21001 ssl default_server;") {
		t.Fatalf("catchall 1. aşama haline dönmeli (8443 + rol portları):\n%s", c)
	}
	if !strings.Contains(oku(t, kapiAcmeYolu), ":23003$request_uri;") {
		t.Fatal("acme 1. aşamanın yönlendirmesini korumalı")
	}
	if len(s.fwKapanan) != 0 {
		t.Fatalf("kaldırılamayan port firewall'da KAPATILMAMALI: %v", s.fwKapanan)
	}
}

// nginx değişiklikten ÖNCE zaten bozuksa dosyaya HİÇ dokunulmaz. Son durumu
// kontrol etmek yetmez: taban kontrolü olmasa da kod yazıp sonraki `nginx -t`
// düşünce geri alır ve son durum AYNI olur (negatif kontrolle ölçüldü: yalnız son
// durumu kontrol eden sürüm kilit sökülünce de yeşil kalıyordu). Bu yüzden
// "hiç yazılmadı" doğrudan ölçülür: mtime eskiye sabitlenir, değişmemeli.
func TestKapiHealTabanZatenBozukYazilmaz(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	s.nginxT = func(n int) error { return errors.New("başka bir vhost bozuk") }
	eskiZaman := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(kapiVhostYolu, eskiZaman, eskiZaman); err != nil {
		t.Fatal(err)
	}
	if kapiUzlastir(1000) {
		t.Fatal("taban bozukken zorlama açılmamalı")
	}
	if fi, err := os.Stat(kapiVhostYolu); err != nil || !fi.ModTime().Equal(eskiZaman) {
		t.Fatalf("nginx önceden bozukken _panel.conf'a HİÇ yazılmamalı (yazıp geri almak da yazmaktır)")
	}
	if _, err := os.Stat(kapiMapYolu); err == nil {
		t.Fatal("map yazılmamalı")
	}
	if s.tSayac != 1 {
		t.Fatalf("yalnız taban `nginx -t` koşmalı, koşan=%d", s.tSayac)
	}
}

func TestKapiHealModKapaliTemizSunucuyaDokunmaz(t *testing.T) {
	s := kapiTestKur(t, kapiTestCanli, nil)
	if kapiUzlastir(1000) {
		t.Fatal("kapi.json yokken zorlama açılmamalı")
	}
	if len(s.cagrilar) != 0 {
		t.Fatalf("geçişi yapılmamış sunucuda nginx'e HİÇ dokunulmamalı, çağrılar=%v", s.cagrilar)
	}
	if _, err := os.Stat(kapiMapYolu); err == nil {
		t.Fatal("mod kapalıyken map oluşturulmamalı")
	}
	if oku(t, kapiVhostYolu) != kapiTestCanli {
		t.Fatal("_panel.conf değişmemeli")
	}
}

func TestKapiHealModKapaliIzleriTemizler(t *testing.T) {
	izli, err := kapi.Uzlastir(kapiTestCanli, rolAyar(), 0, kapi.Secenek{Gizli: kapiTestGizli})
	if err != nil {
		t.Fatal(err)
	}
	s := kapiTestKur(t, izli, nil)
	if err := os.WriteFile(kapiMapYolu, []byte(kapi.MapIcerigi(rolAyar(), 0, nil)), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	if len(s.ozelIzin) != 1 {
		t.Error("boşaltılan harita da özel izinle yazılmalı")
	}
	conf := oku(t, kapiVhostYolu)
	if strings.Contains(conf, kapi.Baslik) {
		t.Error("mod kapalı: kapı başlığı temizlenmeli")
	}
	if !strings.Contains(conf, "listen 21001 ssl;") {
		t.Error("mod kapalı: rol listen'larına DOKUNULMAMALI (kullanıcılar o portlarda)")
	}
	if m := oku(t, kapiMapYolu); !strings.Contains(m, `default "";`) || strings.Contains(m, "admin") {
		t.Errorf("map boş varsayılana dönmeli ($gp_kapi tanımlı kalmalı):\n%s", m)
	}
}

func TestKapiHealCatchallRolPortlarinaGenisler(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	if err := os.WriteFile(kapiCatchallYolu, []byte(kapiTestCatchall), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	c := oku(t, kapiCatchallYolu)
	for _, p := range []string{"21001", "22002", "23003"} {
		if !strings.Contains(c, "listen "+p+" ssl default_server;") {
			t.Errorf("catchall rol portunu kapsamalı (yoksa Host kısıtı o portta devre dışı): %s\n%s", p, c)
		}
	}
	if strings.Contains(c, "8443") {
		t.Error("kaldırılan 8443 catchall'dan da çıkmalı")
	}
}

// Taze kurulumda panel nginx'ten önce kalkabilir: doğrulanmış (nginx -t geçen)
// yapılandırma reload edilemedi diye GERİ ALINMAZ; nginx kalkınca onunla başlar.
func TestKapiHealNginxDurukkenGeriAlinmaz(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiNginxAktif = func() bool { return false }
	s.dinliyor = false // nginx yokken admin portu da dinlemez → kaldırma aşaması atlanır
	kapiUzlastir(1000)
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "listen 21001 ssl;") || !kapi.BaslikTamMi(conf, kapiTestGizli) {
		t.Fatalf("doğrulanan yapılandırma KORUNMALI:\n%s", conf)
	}
	for _, c := range s.cagrilar {
		if strings.HasPrefix(c, "systemctl reload") {
			t.Fatal("nginx çalışmıyorken reload denenmemeli")
		}
	}
}

// IPv4-yalnız sunucu: catchall'a [::] satırı YAZILMAZ (nginx -t IPv6'sız çekirdekte düşerdi).
func TestKapiHealCatchallIPv6Kanitsiz(t *testing.T) {
	v4 := strings.ReplaceAll(kapiTestSablon, "    listen [::]:8443 ssl default_server;\n", "")
	kapiTestKur(t, v4, rolAyar())
	catch := strings.ReplaceAll(kapiTestCatchall, "    listen [::]:8443 ssl default_server;\n", "")
	if err := os.WriteFile(kapiCatchallYolu, []byte(catch), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	if c := oku(t, kapiCatchallYolu); strings.Contains(c, "[::]") || !strings.Contains(c, "listen 23003 ssl default_server;") {
		t.Fatalf("IPv6 kanıtı yokken [::] olmamalı, rol portları olmalı:\n%s", c)
	}
}

// :80 → HTTPS yönlendirmesi genel panel portuna (müşteri) gider; ASLA admin portuna
// (http://panel.alan.adi yazan herkese gizli portu söylerdi) ve kapanan eski porta değil.
func TestKapiHealAcmeYonlendirmesi(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 5000
	kapiTestKur(t, kapiTestCanli, a)
	acme := "server {\n    listen 80;\n    location / {\n        return 301 https://$server_name:8443$request_uri;\n    }\n}"
	if err := os.WriteFile(kapiAcmeYolu, []byte(acme), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	if c := oku(t, kapiAcmeYolu); !strings.Contains(c, "https://$server_name:23003$request_uri;") {
		t.Fatalf("yönlendirme müşteri kapısına gitmeli:\n%s", c)
	}
}

// Genel port bağımlıları (uygulama vhost'ları, webhook'lar) durumsuz yayınla taşınır:
// geçişte eski port canlıdır (taşınmaz), eski kapanınca canlı küme onu İÇERMEZ.
func TestKapiHealGenelPortYayilir(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 500
	kapiTestKur(t, kapiTestCanli, a)
	var genel int
	var canli map[int]bool
	KapiGenelPortUzlastir = func(g int, c map[int]bool) { genel, canli = g, c }

	kapiUzlastir(1000) // süre doldu ama admin doğrulamadı → eski açık
	if genel != 23003 || !canli[8443] || !canli[23003] {
		t.Fatalf("geçişte: genel=müşteri, 8443 canlı olmalı: genel=%d canli=%v", genel, canli)
	}
	_ = kapi.Guncelle(kapiAyarYolu, func(x *kapi.Ayar) error { x.AdminDogrulandi = 900; return nil })
	kapiUzlastir(1000)
	if genel != 23003 || canli[8443] {
		t.Fatalf("eski kapanınca 8443 canlı SAYILMAMALI (taşınsın): genel=%d canli=%v", genel, canli)
	}
	if canli[9443] {
		t.Error("loopback (erisim) portu genel canlı sayılmamalı")
	}
}

// Uzlaştırma sürerken panelden admin portu değişirse (yeni eski port + kanıt sıfır),
// eski portları düşürme kararı BAYAT kopyaya göre verilmemeli: doğrulanmamış yeni admin
// portuyla önceki admin portunu kapatmak kilitlenmedir.
func TestKapiHealEskiDusurmeTazeAyaraBakar(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis, a.AdminDogrulandi = []int{8443}, 500, 400
	kapiTestKur(t, kapiTestCanli, a)
	degisti := false
	kapiPortDinliyor = func(int) bool {
		if !degisti { // aşama 2'nin başında eşzamanlı panel isteği
			degisti = true
			if _, err := kapi.RolPortuDegistir(kapiAyarYolu, kapi.Admin, 24004, 1000); err != nil {
				t.Fatal(err)
			}
		}
		return true
	}
	kapiUzlastir(1000)
	b, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		t.Fatal(err)
	}
	if !iceriyor(b.EskiPortlar, 21001) || b.AdminDogrulandi != 0 {
		t.Fatalf("eşzamanlı eklenen eski admin portu SİLİNMEMELİ: %+v", b)
	}
}

// Kapısı kanıtlanamayan istek yağmuru onarımı YALNIZ bir kez tetikler; onarılamayan
// zincirde zorlama kapanır (15 dk kilitlenme yerine saniyeler).
func TestKapiZincirKopuk(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	eskiOlc, eskiAra := kapiTipOlcucu, kapiZincirAra
	t.Cleanup(func() { kapiTipOlcucu, kapiZincirAra = eskiOlc, eskiAra; kapiZincirTetik.Store(false) })
	kapiZincirAra = time.Hour // bu test boyunca yeniden tetiklenmesin
	var olcum int32
	kapiTipOlcucu = func(_ context.Context, port int, _ string, _ int) (string, error) {
		atomic.AddInt32(&olcum, 1)
		return map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri"}[port], nil
	}
	kapi.ZorlamaAyarla(true)
	s.nginxT = func(int) error { return errors.New("ilgisiz vhost bozuk") } // onarım uygulanamaz
	for i := 0; i < 50; i++ {
		kapiZincirKopuk()
	}
	deadline := time.Now().Add(3 * time.Second)
	for kapi.ZorlamaAktif() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if kapi.ZorlamaAktif() {
		t.Fatal("onarılamayan zincirde zorlama KAPANMALI (kilitlenme yerine eski davranış)")
	}
	if s.tSayac != 1 {
		t.Fatalf("50 sinyal tek onarım tetiklemeli, nginx -t sayısı=%d", s.tSayac)
	}
	if atomic.LoadInt32(&olcum) != 0 {
		t.Fatal("uzlaştırma düşünce ölçüme gidilmemeli")
	}
}

func TestKapiSimdiUygula(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	yanit := map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri"}
	kapiTipOlcucu = func(_ context.Context, port int, _ string, _ int) (string, error) { return yanit[port], nil }
	if err := KapiSimdiUygula(context.Background()); err != nil || !kapi.ZorlamaAktif() {
		t.Fatalf("başarılı uygulama: err=%v zorlama=%v", err, kapi.ZorlamaAktif())
	}
	yanit[22002] = "karma"
	if err := KapiSimdiUygula(context.Background()); err == nil || kapi.ZorlamaAktif() {
		t.Fatal("ölçüm düşerse hata dönmeli ve zorlama KAPANMALI")
	}
}

// Gizli yokken kapı başlığı yazılamaz: yarım kapı düzeni (listen'lar var, başlık yok)
// kurulmaz — hiçbir dosyaya dokunulmaz.
func TestKapiHealProxyGizliYoksaZorlamaYok(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	s.gizli = ""
	// Gizli denetimi SELinux etiketlemesinden de ÖNCE: uygulanmayacak kapı için sistem
	// politikası değiştirilmez (kapiTestKur'un semanage dikişleri çağrılırsa test düşer).
	kapiSELinuxModu = func() string { return "enforcing" }
	if kapiUzlastir(1000) {
		t.Fatal("proxy gizlisi yokken başlığa güvenilemez → zorlama açılmamalı")
	}
	if len(s.cagrilar) != 0 || oku(t, kapiVhostYolu) != kapiTestSablon {
		t.Fatalf("gizli yokken nginx'e dokunulmamalı: %v", s.cagrilar)
	}
	if _, err := os.Stat(kapiMapYolu); err == nil {
		t.Fatal("gizli yokken map yazılmamalı")
	}
}

// Proxy gizlisi döndürülünce (proxy.secret yenilendi) başlık satırları yeni gizliyle
// yeniden yazılır; eski gizlili satır "tam" SAYILMAZ.
func TestKapiHealGizliDegisinceBaslikYenilenir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiUzlastir(1000)
	yeniGizli := strings.Repeat("f", 64)
	if kapi.BaslikTamMi(oku(t, kapiVhostYolu), yeniGizli) {
		t.Fatal("eski gizlili başlık yeni gizliyle tam sayılmamalı")
	}
	s.gizli = yeniGizli
	if !kapiUzlastir(1000) {
		t.Fatal("yeni gizliyle uzlaştırma açılabilir dönmeli")
	}
	conf := oku(t, kapiVhostYolu)
	if !kapi.BaslikTamMi(conf, yeniGizli) || strings.Contains(conf, kapiTestGizli) {
		t.Fatalf("başlık yeni gizliyle yazılmalı, eski gizli kalmamalı:\n%s", conf)
	}
}

func TestYalnizNginxDinliyor(t *testing.T) {
	cases := map[string]bool{
		`LISTEN 0 511 0.0.0.0:21001 0.0.0.0:* users:(("nginx",pid=12,fd=6),("nginx",pid=11,fd=6))` + "\n" +
			`LISTEN 0 511 [::]:21001 [::]:* users:(("nginx",pid=12,fd=7),("nginx",pid=11,fd=7))`: true,
		`LISTEN 0 511 127.0.0.1:21001 0.0.0.0:* users:(("nginx",pid=12,fd=6))`: true,
		"": false, // dinleyen yok
		`LISTEN 0 4096 0.0.0.0:21001 0.0.0.0:* users:(("node",pid=99,fd=3))`: false,
		`LISTEN 0 511 0.0.0.0:21001 0.0.0.0:* users:(("nginx",pid=12,fd=6))` + "\n" +
			`LISTEN 0 4096 [::]:21001 [::]:* users:(("python3",pid=99,fd=3))`: false, // biri başkası
		`LISTEN 0 511 0.0.0.0:21001 0.0.0.0:*`: false, // süreç bilgisi yok (yetkisiz ss)
	}
	for ss, bek := range cases {
		if got := yalnizNginxDinliyor(ss); got != bek {
			t.Errorf("%q → %v beklenir, got %v", ss, bek, got)
		}
	}
}

func TestKapiHealIdempotent(t *testing.T) {
	s := kapiTestKur(t, kapiTestCanli, rolAyar())
	kapiUzlastir(1000)
	onceki := len(s.cagrilar)
	if !kapiUzlastir(1000) {
		t.Fatal("ikinci çalıştırma da açılabilir dönmeli")
	}
	if len(s.cagrilar) != onceki {
		t.Fatalf("değişiklik yokken nginx'e dokunulmamalı: %v", s.cagrilar[onceki:])
	}
}

func TestKapiHealErisimLoopbackPortunaDelikAcmaz(t *testing.T) {
	a := &kapi.Ayar{Admin: 9443, Bayi: 22002, Musteri: 23003} // admin = erisim'in 127.0.0.1'e çektiği port
	s := kapiTestKur(t, kapiTestCanli, a)
	kapiUzlastir(1000)
	if iceriyor(s.fwAcilan, 9443) {
		t.Fatal("operatörün dışarı kapattığı (loopback) admin portuna firewall'da delik AÇILMAMALI")
	}
	if !iceriyor(s.fwAcilan, 22002) {
		t.Fatal("genel bayi portu açılmalı")
	}
}

func TestKapiOlcVeAyarla(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) {
		t.Fatal("ön koşul: uzlaştırma açılabilir")
	}
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	yanit := map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri"}
	var kapilar []string
	kapiTipOlcucu = func(_ context.Context, port int, k string, _ int) (string, error) {
		kapilar = append(kapilar, fmt.Sprintf("%d=%s", port, k))
		return yanit[port], nil
	}

	kapi.ZorlamaAyarla(false)
	kapiOlcVeAyarla(context.Background(), 1)
	if !kapi.ZorlamaAktif() {
		t.Fatal("üç port da doğruysa zorlama açılmalı")
	}
	// Her port KENDİ kapı adıyla doğrulanır (kanıt admin/bayi etiket kaymasını yakalar).
	if strings.Join(kapilar, " ") != "21001=admin 22002=bayi 23003=musteri" {
		t.Fatalf("ölçüm beklenen kapı adlarıyla yapılmalı: %v", kapilar)
	}
	yanit[23003] = "karma" // müşteri portunda başlık güvenilmiyor
	kapiOlcVeAyarla(context.Background(), 1)
	if kapi.ZorlamaAktif() {
		t.Fatal("tek port bile yanlışsa zorlama KAPANMALI (eski davranış, kilitlenme yok)")
	}
	yanit[23003] = "musteri"
	kapiTipOlcucu = func(_ context.Context, port int, _ string, _ int) (string, error) {
		if port == 22002 {
			return "", errors.New("bağlantı reddedildi")
		}
		return yanit[port], nil
	}
	kapi.ZorlamaAyarla(true)
	kapiOlcVeAyarla(context.Background(), 1)
	if kapi.ZorlamaAktif() {
		t.Fatal("ölçüm hatasında zorlama kapanmalı")
	}
}

// olcumSunucusu: /api/v1/kapi'yi taklit eden TLS sunucusu. yanitla(deneme, nonce, port)
// yanıtı belirler; port sunucunun kendi portudur.
func olcumSunucusu(t *testing.T, yanitla func(n int32, nonce string, port int, w http.ResponseWriter)) int {
	t.Helper()
	var n int32
	var port int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kapi" {
			http.NotFound(w, r)
			return
		}
		yanitla(atomic.AddInt32(&n, 1), r.Header.Get(kapi.OlcumBaslik), port, w)
	}))
	t.Cleanup(srv.Close)
	_, portS, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ = strconv.Atoi(portS)
	eskiAra, eskiGizli := kapiOlcAra, kapiProxyGizli
	kapiOlcAra = 10 * time.Millisecond
	kapiProxyGizli = func() string { return kapiTestGizli }
	t.Cleanup(func() { kapiOlcAra, kapiProxyGizli = eskiAra, eskiGizli })
	return port
}

// kapiTipOlc: gerçek HTTPS yolu — backend önce 502 (henüz ayakta değil) sonra 200.
func TestKapiTipOlcYenidenDener(t *testing.T) {
	var deneme int32
	port := olcumSunucusu(t, func(n int32, nonce string, port int, w http.ResponseWriter) {
		deneme = n
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprintf(w, `{"tip":"musteri","kanit":%q}`, kapi.OlcumKaniti(kapiTestGizli, nonce, kapi.Musteri, port))
	})
	tip, err := kapiTipOlc(context.Background(), port, kapi.Musteri, 3)
	if err != nil || tip != "musteri" {
		t.Fatalf("yeniden deneme ile tip okunmalı: tip=%q err=%v", tip, err)
	}
	if deneme != 2 {
		t.Fatalf("502 sonrası bir kez daha denenmeli, deneme=%d", deneme)
	}
}

// Kanıtı tutmayan yanıt (portu başka süreç tutuyor / yanlış etiket / başka porta
// aktarılmış) tip ne derse desin ölçümü GEÇEMEZ; her deneme taze nonce kullanır.
func TestKapiTipOlcKanitsizYanitGecmez(t *testing.T) {
	nonceler := map[string]bool{}
	for ad, uret := range map[string]func(nonce string, port int) string{
		"kanıt yok":      func(string, int) string { return "" },
		"yanlış kapı":    func(n string, p int) string { return kapi.OlcumKaniti(kapiTestGizli, n, kapi.Bayi, p) },
		"başka port":     func(n string, p int) string { return kapi.OlcumKaniti(kapiTestGizli, n, kapi.Admin, p+1) },
		"yanlış gizli":   func(n string, p int) string { return kapi.OlcumKaniti("baska", n, kapi.Admin, p) },
		"bayat (tekrar)": func(string, int) string { return kapi.OlcumKaniti(kapiTestGizli, "sabit", kapi.Admin, 0) },
	} {
		t.Run(ad, func(t *testing.T) {
			port := olcumSunucusu(t, func(_ int32, nonce string, port int, w http.ResponseWriter) {
				nonceler[nonce] = true
				fmt.Fprintf(w, `{"tip":"yonetim","kanit":%q}`, uret(nonce, port))
			})
			if tip, err := kapiTipOlc(context.Background(), port, kapi.Admin, 2); err == nil {
				t.Fatalf("kanıtsız yanıt kabul edildi: tip=%q", tip)
			}
		})
	}
	if len(nonceler) != 10 {
		t.Fatalf("her deneme taze nonce göndermeli: %d farklı nonce / 10 deneme", len(nonceler))
	}
}

func TestKapiTipOlcGizliYoksaOlcmez(t *testing.T) {
	port := olcumSunucusu(t, func(_ int32, _ string, _ int, w http.ResponseWriter) {
		t.Error("gizli yokken istek atılmamalı")
	})
	kapiProxyGizli = func() string { return "" }
	if _, err := kapiTipOlc(context.Background(), port, kapi.Admin, 1); err == nil {
		t.Fatal("gizli yokken ölçüm hata dönmeli")
	}
}

// Geçici hata (limit_conn 503, aşırı yük, backend henüz ayakta değil) sonuç DEĞİLDİR:
// yerel bir süreç IP-başı bağlantı sınırını doldurup ölçümü düşürerek rol ayrımını
// kapattıramasın. Bağlantı reddi (portta dinleyen yok) ise kesindir.
func TestKapiTipOlcGeciciHataBelirsiz(t *testing.T) {
	for _, kod := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		port := olcumSunucusu(t, func(_ int32, _ string, _ int, w http.ResponseWriter) {
			w.WriteHeader(kod)
		})
		if _, err := kapiTipOlc(context.Background(), port, kapi.Admin, 2); !errors.Is(err, errOlcumBelirsiz) {
			t.Errorf("HTTP %d belirsiz sayılmalı, got %v", kod, err)
		}
	}
	// Kanıtsız 200 kesin başarısızlıktır (belirsiz DEĞİL).
	port := olcumSunucusu(t, func(_ int32, _ string, _ int, w http.ResponseWriter) {
		fmt.Fprint(w, `{"tip":"yonetim"}`)
	})
	if _, err := kapiTipOlc(context.Background(), port, kapi.Admin, 1); err == nil || errors.Is(err, errOlcumBelirsiz) {
		t.Fatalf("kanıtsız yanıt kesin başarısızlık olmalı, got %v", err)
	}
	// Bağlantı reddi: dinleyen yok → kesin.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bos := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if _, err := kapiTipOlc(context.Background(), bos, kapi.Admin, 1); err == nil || errors.Is(err, errOlcumBelirsiz) {
		t.Fatalf("bağlantı reddi kesin başarısızlık olmalı, got %v", err)
	}
	// Bağlantı kabul edilip kesilirse (yük altında nginx/çekirdek): sonuç yok → belirsiz.
	kes, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kes.Close() })
	go func() {
		for {
			c, err := kes.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if _, err := kapiTipOlc(context.Background(), kes.Addr().(*net.TCPAddr).Port, kapi.Admin, 1); !errors.Is(err, errOlcumBelirsiz) {
		t.Fatalf("kopan bağlantı belirsiz sayılmalı, got %v", err)
	}
}

// Belirsiz ölçüm zorlamayı hiçbir yönde DEĞİŞTİRMEZ; kesin başarısızlık kapatır.
func TestKapiOlcBelirsizZorlamayiDegistirmez(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) {
		t.Fatal("ön koşul: uzlaştırma açılabilir")
	}
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	kapiTipOlcucu = func(context.Context, int, string, int) (string, error) {
		return "", fmt.Errorf("%w: HTTP 503", errOlcumBelirsiz)
	}
	for _, bas := range []bool{true, false} {
		kapi.ZorlamaAyarla(bas)
		if s := kapiOlcVeAyarla(context.Background(), 1); s != olcumBelirsiz || kapi.ZorlamaAktif() != bas {
			t.Fatalf("belirsiz ölçüm (başlangıç %v): sonuç=%v zorlama=%v", bas, s, kapi.ZorlamaAktif())
		}
	}
	kapiTipOlcucu = func(context.Context, int, string, int) (string, error) { return "", errors.New("connection refused") }
	kapi.ZorlamaAyarla(true)
	if s := kapiOlcVeAyarla(context.Background(), 1); s != olcumKopuk || kapi.ZorlamaAktif() {
		t.Fatalf("kesin başarısızlık zorlamayı kapatmalı: sonuç=%v zorlama=%v", s, kapi.ZorlamaAktif())
	}
}

// Panelden uygulanan değişikliğin ölçümü belirsizse: hata "önceki durumunda" der ve
// bakım döngüsü kısa aralıkla yeniden ölçsün diye dürtülür.
func TestKapiSimdiUygulaBelirsiz(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	for len(kapiYenidenOlc) > 0 {
		<-kapiYenidenOlc
	}
	kapiTipOlcucu = func(context.Context, int, string, int) (string, error) {
		return "", fmt.Errorf("%w: zaman aşımı", errOlcumBelirsiz)
	}
	kapi.ZorlamaAyarla(true)
	err := KapiSimdiUygula(context.Background())
	if err == nil || !strings.Contains(err.Error(), "önceki durumunda") || !kapi.ZorlamaAktif() {
		t.Fatalf("belirsiz: hata=%v zorlama=%v", err, kapi.ZorlamaAktif())
	}
	select {
	case <-kapiYenidenOlc:
	default:
		t.Fatal("bakım döngüsü kısa aralıkla yeniden ölçmeye dürtülmeli")
	}
}

func TestKapiGeriCekilme(t *testing.T) {
	ar := 15 * time.Minute
	for n, bek := range map[int]time.Duration{0: time.Minute, 1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute,
		4: 8 * time.Minute, 5: 15 * time.Minute, 60: 15 * time.Minute} {
		if got := kapiGeriCekilme(n, ar); got != bek {
			t.Errorf("ardışık=%d → %v beklenir, got %v", n, bek, got)
		}
	}
	if got := kapiGeriCekilme(3, 30*time.Second); got != 30*time.Second {
		t.Errorf("aralık bir dakikadan kısaysa aralığı aşmamalı, got %v", got)
	}
}
