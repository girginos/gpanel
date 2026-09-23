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

type kapiSahte struct {
	dir                 string
	nginxT              func(n int) error // n: kaçıncı `nginx -t` çağrısı (1'den)
	tSayac              int
	cagrilar            []string
	fwAcilan, fwKapanan []int
	dinliyor            bool
	gizli               string
}

func kapiTestKur(t *testing.T, conf string, ayar *kapi.Ayar) *kapiSahte {
	t.Helper()
	s := &kapiSahte{dir: t.TempDir(), dinliyor: true, gizli: "gizli"}
	eskiVhost, eskiCatch, eskiMap, eskiAyar := kapiVhostYolu, kapiCatchallYolu, kapiMapYolu, kapiAyarYolu
	eskiKomut, eskiAc, eskiKapat := kapiNginxKomut, kapiFwAc, kapiFwKapat
	eskiDinl, eskiGizli, eskiIzin := kapiPortDinliyor, kapiProxyGizli, kapiVhostIzin
	t.Cleanup(func() {
		kapiVhostYolu, kapiCatchallYolu, kapiMapYolu, kapiAyarYolu = eskiVhost, eskiCatch, eskiMap, eskiAyar
		kapiNginxKomut, kapiFwAc, kapiFwKapat = eskiKomut, eskiAc, eskiKapat
		kapiPortDinliyor, kapiProxyGizli, kapiVhostIzin = eskiDinl, eskiGizli, eskiIzin
		kapi.ZorlamaAyarla(false)
	})
	kapiVhostYolu = filepath.Join(s.dir, "_panel.conf")
	kapiCatchallYolu = filepath.Join(s.dir, "_panel_catchall.conf")
	kapiMapYolu = filepath.Join(s.dir, "00-gosp-kapi.conf")
	kapiAyarYolu = filepath.Join(s.dir, "kapi.json")
	kapiNginxKomut = func(ad string, arg ...string) ([]byte, error) {
		c := ad + " " + strings.Join(arg, " ")
		s.cagrilar = append(s.cagrilar, c)
		if c == "nginx -t" {
			s.tSayac++
			if s.nginxT != nil {
				if err := s.nginxT(s.tSayac); err != nil {
					return []byte("nginx: [emerg] test"), err
				}
			}
		}
		return nil, nil
	}
	kapiFwAc = func(p int) error { s.fwAcilan = append(s.fwAcilan, p); return nil }
	kapiFwKapat = func(p int) error { s.fwKapanan = append(s.fwKapanan, p); return nil }
	kapiPortDinliyor = func(int) bool { return s.dinliyor }
	kapiProxyGizli = func() string { return s.gizli }
	kapiVhostIzin = func() {}
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
	if !kapi.BaslikTamMi(conf) {
		t.Error("kapı başlığı eksik")
	}
	if m := oku(t, kapiMapYolu); !strings.Contains(m, "21001 admin;") || !strings.Contains(m, "default bilinmeyen;") {
		t.Errorf("map eksik:\n%s", m)
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

func TestKapiHealAdminDinlemiyorKaldirmaYok(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	s.dinliyor = false
	kapiUzlastir(1000)
	if !strings.Contains(oku(t, kapiVhostYolu), "listen 8443 ssl default_server;") {
		t.Fatal("admin kapısı dinlemiyorken HİÇBİR port kaldırılmamalı (son çalışan port)")
	}
	if len(s.fwKapanan) != 0 {
		t.Fatalf("firewall'da port kapatılmamalı: %v", s.fwKapanan)
	}
}

func TestKapiHealNginxTestiDuserseGeriAlinir(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
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
	if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("önceden olmayan map dosyası geri almada SİLİNMELİ")
	}
	for _, c := range s.cagrilar {
		if strings.HasPrefix(c, "systemctl reload") {
			t.Fatal("başarısız doğrulamadan sonra reload yapılmamalı")
		}
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
	izli, err := kapi.Uzlastir(kapiTestCanli, rolAyar(), 0)
	if err != nil {
		t.Fatal(err)
	}
	kapiTestKur(t, izli, nil)
	if err := os.WriteFile(kapiMapYolu, []byte(kapi.MapIcerigi(rolAyar(), 0)), 0o644); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
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

func TestKapiHealProxyGizliYoksaZorlamaYok(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	s.gizli = ""
	if kapiUzlastir(1000) {
		t.Fatal("proxy gizlisi yokken başlığa güvenilemez → zorlama açılmamalı")
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
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	yanit := map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri"}
	kapiTipOlcucu = func(_ context.Context, port, _ int) (string, error) { return yanit[port], nil }

	kapi.ZorlamaAyarla(false)
	kapiOlcVeAyarla(context.Background(), 1)
	if !kapi.ZorlamaAktif() {
		t.Fatal("üç port da doğruysa zorlama açılmalı")
	}
	yanit[23003] = "karma" // müşteri portunda başlık güvenilmiyor
	kapiOlcVeAyarla(context.Background(), 1)
	if kapi.ZorlamaAktif() {
		t.Fatal("tek port bile yanlışsa zorlama KAPANMALI (eski davranış, kilitlenme yok)")
	}
	yanit[23003] = "musteri"
	kapiTipOlcucu = func(_ context.Context, port, _ int) (string, error) {
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

// kapiTipOlc: gerçek HTTPS yolu — backend önce 502 (henüz ayakta değil) sonra 200.
func TestKapiTipOlcYenidenDener(t *testing.T) {
	var n int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kapi" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"tip":"musteri"}`)
	}))
	defer srv.Close()
	eskiAra := kapiOlcAra
	kapiOlcAra = 10 * time.Millisecond
	t.Cleanup(func() { kapiOlcAra = eskiAra })
	_, portS, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portS)
	tip, err := kapiTipOlc(context.Background(), port, 3)
	if err != nil || tip != "musteri" {
		t.Fatalf("yeniden deneme ile tip okunmalı: tip=%q err=%v", tip, err)
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("502 sonrası bir kez daha denenmeli, deneme=%d", n)
	}
}
