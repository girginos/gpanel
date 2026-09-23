package kapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sablonConf: assets/nginx/_panel.conf'un yapısal özü (taze kurulum).
const sablonConf = `server {
    listen 8443 ssl default_server;
    ssl_certificate /etc/ssl/girginospanel/panel.crt;
    listen [::]:8443 ssl default_server;
    server_name _;

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
    location = /healthz {
        proxy_pass http://127.0.0.1:8080;
    }
}`

// canliConf: 181'in canlı yapısı — erisim masaüstü (loopback + yorumlu IPv6),
// panelhost server_name, panelhardening X-Gosp-Proxy, login hız-sınırı location'ı.
const canliConf = `server {
    listen 127.0.0.1:9443 ssl;
    listen 8443 ssl;
    listen [::]:8443 ssl;
    # [erisim:masaustu] listen [::]:9443 ssl;
    server_name lab4.girginos.app localhost 127.0.0.1;

    location ~ ^/api/v1/(auth|musteri)/login$ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Gosp-Proxy "gizli";
    }
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Gosp-Proxy "gizli";
    }
}`

func ornekAyar() *Ayar {
	return &Ayar{Admin: 21001, Bayi: 22002, Musteri: 23003}
}

func TestAdresAyristir(t *testing.T) {
	cases := []struct {
		adr      string
		port     int
		ipv6, lb bool
	}{
		{"8443", 8443, false, false},
		{"8443;", 8443, false, false},
		{"[::]:8443", 8443, true, false},
		{"127.0.0.1:9443", 9443, false, true},
		{"[::1]:9443", 9443, true, true},
		{"0.0.0.0:8443", 8443, false, false},
		{"localhost:9443", 9443, false, true},
		{"abc", 0, false, false},
		{"99999", 0, false, false},
	}
	for _, c := range cases {
		p, v6, lb := adresAyristir(c.adr)
		if p != c.port || v6 != c.ipv6 || lb != c.lb {
			t.Errorf("%q: got (%d,%v,%v) want (%d,%v,%v)", c.adr, p, v6, lb, c.port, c.ipv6, c.lb)
		}
	}
}

func TestDinlemelerVeGenelPortlar(t *testing.T) {
	d := Dinlemeler(strings.Split(canliConf, "\n"))
	if len(d) != 4 {
		t.Fatalf("4 listen bekleniyordu (loopback, v4, v6, yorumlu), %d bulundu: %+v", len(d), d)
	}
	if !d[0].Loopback || d[0].Port != 9443 {
		t.Errorf("ilk satır 127.0.0.1:9443 loopback olmalı: %+v", d[0])
	}
	if !d[3].Yorum || d[3].Port != 9443 || !d[3].IPv6 {
		t.Errorf("erisim yorumlu IPv6 9443 tanınmalı: %+v", d[3])
	}
	if g := GenelPortlar(canliConf); len(g) != 1 || g[0] != 8443 {
		t.Errorf("genel portlar yalnız [8443] olmalı (loopback/yorum hariç): %v", g)
	}
}

func TestUzlastirSablon(t *testing.T) {
	a := ornekAyar()
	yeni, err := Uzlastir(sablonConf, a, 0, testSecenek)
	if err != nil {
		t.Fatalf("Uzlastir: %v", err)
	}
	for _, p := range []string{"21001", "22002", "23003"} {
		if !strings.Contains(yeni, "listen "+p+" ssl; # gp-kapi") {
			t.Errorf("IPv4 listen eksik: %s", p)
		}
		if !strings.Contains(yeni, "listen [::]:"+p+" ssl; # gp-kapi") {
			t.Errorf("IPv6 listen eksik (şablonda etkin IPv6 var): %s", p)
		}
		if strings.Contains(yeni, p+" ssl default_server") {
			t.Errorf("rol listen'ı default_server ALMAMALI (catchall çakışması): %s", p)
		}
	}
	if !BaslikTamMi(yeni, testGizli) {
		t.Error("X-Real-IP çapasının ardında kapı başlığı olmalı")
	}
	if !strings.Contains(yeni, "listen 8443 ssl default_server;") {
		t.Error("Uzlastir EKLEYİCİDİR: mevcut 8443'e dokunmamalı (kaldırma ayrı adım)")
	}
	// Başlık healthz location'ına GİRMEMELİ (X-Real-IP çapası yok).
	hz := yeni[strings.Index(yeni, "location = /healthz"):]
	if strings.Contains(hz, Baslik) {
		t.Error("kapı başlığı healthz'e eklenmemeli")
	}
	// İdempotent.
	iki, err := Uzlastir(yeni, a, 0, testSecenek)
	if err != nil || iki != yeni {
		t.Errorf("Uzlastir idempotent olmalı (err=%v)", err)
	}
}

func TestUzlastirErisimeDokunmaz(t *testing.T) {
	// admin portu erisim'in loopback'e çektiği 9443: yeni listen EKLENMEMELİ.
	a := &Ayar{Admin: 9443, Bayi: 22002, Musteri: 23003}
	yeni, err := Uzlastir(canliConf, a, 0, testSecenek)
	if err != nil {
		t.Fatalf("Uzlastir: %v", err)
	}
	if strings.Contains(yeni, "listen 9443 ssl; # gp-kapi") || strings.Contains(yeni, "listen [::]:9443 ssl; # gp-kapi") {
		t.Error("erisim'in yönettiği 9443 için genel listen EKLENMEMELİ (operatör seçimi geri alınır)")
	}
	if !strings.Contains(yeni, "listen 127.0.0.1:9443 ssl;") || !strings.Contains(yeni, "# [erisim:masaustu] listen [::]:9443 ssl;") {
		t.Error("erisim satırları aynen korunmalı")
	}
	if !strings.Contains(yeni, "listen 22002 ssl; # gp-kapi") {
		t.Error("bayi portu eklenmeli")
	}
	// Canlı dosyada 2 çapa (login + /api/) → ikisine de başlık.
	if strings.Count(yeni, KapiSatiri(testGizli)) != 2 || !BaslikTamMi(yeni, testGizli) {
		t.Errorf("iki çapanın ikisine de başlık gerekli, sayım=%d", strings.Count(yeni, KapiSatiri(testGizli)))
	}
}

func TestUzlastirCapasizHata(t *testing.T) {
	conf := "server {\n    listen 8443 ssl;\n    location / { root /x; }\n}"
	if _, err := Uzlastir(conf, ornekAyar(), 0, testSecenek); err == nil {
		t.Fatal("X-Real-IP çapası yokken hata dönmeli (başlıksız zorlama = sessiz kilitlenme)")
	}
	if _, err := Uzlastir("server { }", ornekAyar(), 0, testSecenek); err == nil {
		t.Fatal("listen'sız dosyada hata dönmeli")
	}
}

func TestBaslikTamMiEksikCapa(t *testing.T) {
	yeni, _ := Uzlastir(canliConf, ornekAyar(), 0, testSecenek)
	// İkinci çapanın başlığını sök → eksik.
	i := strings.LastIndex(yeni, KapiSatiri(testGizli))
	bozuk := yeni[:i] + "proxy_set_header X-Baska 1;" + yeni[i+len(KapiSatiri(testGizli)):]
	if BaslikTamMi(bozuk, testGizli) {
		t.Fatal("bir çapada başlık eksikken BaslikTamMi false dönmeli")
	}
	if BaslikTamMi("server { listen 1 ssl; }", testGizli) {
		t.Fatal("hiç çapa yokken true dönmemeli")
	}
}

func TestGenelListenKaldir(t *testing.T) {
	a := ornekAyar()
	eklenmis, _ := Uzlastir(canliConf, a, 0, testSecenek)
	tut := map[int]bool{a.Admin: true, a.Bayi: true, a.Musteri: true}
	sonuc, silinen := GenelListenKaldir(eklenmis, tut)
	if len(silinen) != 1 || silinen[0] != 8443 {
		t.Fatalf("yalnız 8443 silinmeli, silinen=%v", silinen)
	}
	if strings.Contains(sonuc, "listen 8443 ssl;") || strings.Contains(sonuc, "listen [::]:8443 ssl;") {
		t.Error("8443'ün v4+v6 satırları silinmeli")
	}
	if !strings.Contains(sonuc, "listen 127.0.0.1:9443 ssl;") || !strings.Contains(sonuc, "# [erisim:masaustu]") {
		t.Error("loopback ve erisim-yorumlu satırlara dokunulmamalı")
	}
	if _, s := GenelListenKaldir(sonuc, tut); len(s) != 0 {
		t.Error("ikinci çağrı hiçbir şey silmemeli (idempotent)")
	}
}

func TestKapiBasliginiSil(t *testing.T) {
	yeni, _ := Uzlastir(canliConf, ornekAyar(), 0, testSecenek)
	temiz := KapiBasliginiSil(yeni)
	if strings.Contains(temiz, Baslik) {
		t.Fatal("kapı başlığı temizlenmeli")
	}
	if !strings.Contains(temiz, "X-Gosp-Proxy") {
		t.Fatal("diğer başlıklara dokunulmamalı")
	}
}

func TestEskiAcik(t *testing.T) {
	a := ornekAyar()
	if a.EskiAcik(100) {
		t.Error("eski port yokken kapalı olmalı")
	}
	a.EskiPortlar = []int{8443}
	a.EskiBitis = 1000
	if !a.EskiAcik(500) {
		t.Error("süre dolmadan açık olmalı")
	}
	if !a.EskiAcik(2000) {
		t.Error("süre dolsa bile admin doğrulamadıysa AÇIK kalmalı (dış firewall kilitlenme kilidi)")
	}
	a.AdminDogrulandi = 900
	if a.EskiAcik(2000) {
		t.Error("süre doldu + admin doğruladı → kapanmalı")
	}
	if !a.EskiAcik(500) {
		t.Error("admin doğrulasa da süre dolmadan kapanmamalı")
	}
}

func TestGecerli(t *testing.T) {
	if err := ornekAyar().Gecerli(); err != nil {
		t.Fatalf("geçerli ayar reddedildi: %v", err)
	}
	bad := []*Ayar{
		{Admin: 21001, Bayi: 21001, Musteri: 23003},                            // tekrar
		{Admin: 80, Bayi: 22002, Musteri: 23003},                               // <1024
		{Admin: 21001, Bayi: 22002, Musteri: 70000},                            // >65535
		{Admin: 0, Bayi: 22002, Musteri: 23003},                                // tanımsız
		{Admin: 21001, Bayi: 22002, Musteri: 23003, EskiPortlar: []int{21001}}, // eski=rol
	}
	for i, a := range bad {
		if a.Gecerli() == nil {
			t.Errorf("geçersiz ayar #%d kabul edildi: %+v", i, a)
		}
	}
}

func TestMapIcerigi(t *testing.T) {
	kapali := MapIcerigi(nil, 0, []int{9443})
	if !strings.Contains(kapali, `default "";`) || strings.Contains(kapali, Admin) {
		t.Errorf("mod kapalı: $gp_kapi TANIMLI ama boş olmalı:\n%s", kapali)
	}
	a := ornekAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 1000
	acik := MapIcerigi(a, 500, []int{9443})
	for _, s := range []string{"default bilinmeyen;", "21001 admin;", "22002 bayi;", "23003 musteri;", "8443 eski;", "9443 admin;"} {
		if !strings.Contains(acik, s) {
			t.Errorf("harita eksik: %q\n%s", s, acik)
		}
	}
	a.AdminDogrulandi = 1
	if strings.Contains(MapIcerigi(a, 2000, nil), "8443") {
		t.Error("eski kapandıktan sonra haritada olmamalı")
	}
	// Haritadaki bir port yerel listede de olsa İKİ kez yazılmaz (nginx -t "duplicate").
	if m := MapIcerigi(ornekAyar(), 0, []int{21001}); strings.Count(m, "21001 ") != 1 {
		t.Errorf("yinelenen map girdisi:\n%s", m)
	}
}

// Geçişten önce erisim'in loopback'e çektiği panel portu (181: 127.0.0.1:9443) tünel
// yoludur: haritada yönetici kapısı olur, yoksa geçiş onu sessizce öldürürdü.
func TestYerelPortlar(t *testing.T) {
	a := ornekAyar()
	if got := YerelPortlar(canliConf, a, 0); len(got) != 1 || got[0] != 9443 {
		t.Fatalf("yalnız-loopback 9443 yerel olmalı, got %v", got)
	}
	// Haritadaki port (masaüstü modunda loopback'e çekilmiş admin kapısı) yerel sayılmaz.
	a.Admin = 9443
	if got := YerelPortlar(canliConf, a, 0); len(got) != 0 {
		t.Fatalf("haritadaki port yerel listeye girmemeli, got %v", got)
	}
	// Aynı portun genel satırı da varsa yerel değildir.
	karma := strings.Replace(canliConf, "    listen 8443 ssl;", "    listen 8443 ssl;\n    listen 9443 ssl;", 1)
	if got := YerelPortlar(karma, ornekAyar(), 0); len(got) != 0 {
		t.Fatalf("genel satırı olan port yerel değil, got %v", got)
	}
}

func TestGirisIzinli(t *testing.T) {
	cases := []struct {
		kapi, rol string
		izin      bool
	}{
		{"", Admin, true}, {"", Musteri, true},
		{Eski, Admin, true}, {Eski, Bayi, true}, {Eski, Musteri, true},
		{Admin, Admin, true}, {Admin, Bayi, false}, {Admin, Musteri, false},
		{Bayi, Bayi, true}, {Bayi, Admin, false},
		{Musteri, Musteri, true}, {Musteri, Admin, false}, {Musteri, Bayi, false},
		{Bilinmeyen, Admin, false}, {Bilinmeyen, Musteri, false},
	}
	for _, c := range cases {
		if got := GirisIzinli(c.kapi, c.rol); got != c.izin {
			t.Errorf("GirisIzinli(%q,%q)=%v want %v", c.kapi, c.rol, got, c.izin)
		}
	}
}

func TestTokenUyumlu(t *testing.T) {
	cases := []struct {
		istek, tok string
		ok         bool
	}{
		{"", "", true}, {"", Admin, true}, {"", Eski, true},
		{Eski, "", true}, {Eski, Eski, true}, {Eski, Admin, false},
		{Admin, Admin, true}, {Admin, "", false}, {Admin, Musteri, false}, {Admin, Eski, false},
		{Musteri, Musteri, true}, {Musteri, Admin, false},
		{Bayi, Bayi, true}, {Bayi, Admin, false},
		{Bilinmeyen, "", false}, {Bilinmeyen, Admin, false},
	}
	for _, c := range cases {
		if got := TokenUyumlu(c.istek, c.tok); got != c.ok {
			t.Errorf("TokenUyumlu(%q,%q)=%v want %v", c.istek, c.tok, got, c.ok)
		}
	}
}

func TestIstek(t *testing.T) {
	guven, _ := testOrtami(t)
	sinyal := 0
	ZincirKopuk = func() { sinyal++ }

	istek := func(vals ...string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/x", nil)
		for _, v := range vals {
			r.Header.Add(Baslik, v)
		}
		return r
	}

	ZorlamaAyarla(false)
	if k := Istek(istek(kapiBasligi(Admin, 21001))); k != "" {
		t.Errorf("zorlama kapalı → \"\" (başlık yok sayılır), got %q", k)
	}
	ZorlamaAyarla(true)
	if k := Istek(istek(kapiBasligi(Musteri, 23003))); k != Musteri || sinyal != 0 {
		t.Errorf("geçerli başlık → %q (sinyal yok), got %q sinyal=%d", Musteri, k, sinyal)
	}
	if k := Istek(istek(kapiBasligi("root", 21001))); k != Bilinmeyen {
		t.Errorf("tanımsız değer → bilinmeyen, got %q", k)
	}
	if k := Istek(istek(kapiBasligi(Admin, 21001), kapiBasligi(Musteri, 23003))); k != Bilinmeyen {
		t.Errorf("çoklu başlık (sahtecilik) → bilinmeyen, got %q", k)
	}
	// Yapılandırma kaymasında kapı satırı olmayan location istemcinin kendi başlığını
	// geçirir: gizlisiz değer taklittir → REDDEDİLİR (eski biçim "admin" dahil).
	for _, sahte := range []string{Admin, "admin 21001 yanlis-gizli"} {
		if k := Istek(istek(sahte)); k != Bilinmeyen {
			t.Errorf("gizlisiz/yanlış gizlili başlık %q → bilinmeyen, got %q", sahte, k)
		}
	}
	// Kanıtlı ama başlıksız = yapılandırma kayması (istemci üretemez): KİLİTLEME, eski
	// davranışa düş ve zinciri yeniden doğrulat.
	sinyal = 0
	if k := Istek(istek()); k != "" || sinyal != 1 {
		t.Errorf("kanıtlı + başlık yok → \"\" ve onarım sinyali, got %q sinyal=%d", k, sinyal)
	}
	*guven = false
	if k := Istek(istek(kapiBasligi(Admin, 21001))); k != Bilinmeyen {
		t.Errorf("nginx kanıtı yoksa (doğrudan :8080) → bilinmeyen, got %q", k)
	}
	if k := Istek(istek()); k != Bilinmeyen {
		t.Errorf("kanıtsız + başlık yok (doğrudan :8080) → bilinmeyen (fail-open YOK), got %q", k)
	}
}

// Masaüstü (erisim) modunda yeni admin kapısı yalnız loopback'te dinler; IPv6 yorumlu.
func TestUzlastirAdminLoopback(t *testing.T) {
	s := testSecenek
	s.AdminLoopback = true
	yeni, err := Uzlastir(sablonConf, ornekAyar(), 0, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yeni, "listen 127.0.0.1:21001 ssl; # gp-kapi") ||
		!strings.Contains(yeni, "# [erisim:masaustu] listen [::]:21001 ssl; # gp-kapi") {
		t.Fatalf("admin kapısı loopback + yorumlu IPv6 olmalı:\n%s", yeni)
	}
	if strings.Contains(yeni, "listen 21001 ssl;") || strings.Contains(yeni, "    listen [::]:21001 ssl; # gp-kapi\n") {
		t.Fatalf("admin kapısı GENEL dinlenmemeli (operatörün kapatması geri alınır):\n%s", yeni)
	}
	if !strings.Contains(yeni, "listen 22002 ssl; # gp-kapi") || !strings.Contains(yeni, "listen 23003 ssl; # gp-kapi") {
		t.Fatal("bayi/müşteri kapıları genel kalmalı")
	}
	if g := GenelPortlar(yeni); strings.Contains(fmt.Sprint(g), "21001") {
		t.Fatalf("admin portu genel portlarda görünmemeli (firewall'da açılmaz): %v", g)
	}
}

func TestUzlastirGizsizHata(t *testing.T) {
	if _, err := Uzlastir(sablonConf, ornekAyar(), 0, Secenek{}); err == nil {
		t.Fatal("proxy gizlisi yokken kapı başlığı yazılamaz → hata")
	}
	yeni, _ := Uzlastir(sablonConf, ornekAyar(), 0, testSecenek)
	if BaslikTamMi(yeni, "baska-gizli") || BaslikTamMi(yeni, "") {
		t.Fatal("başlık GÜNCEL gizliyle eşleşmeli (gizli değişince Heal yeniden yazar)")
	}
	iki, _ := Uzlastir(yeni, ornekAyar(), 0, Secenek{Gizli: "yeni-gizli"})
	if !BaslikTamMi(iki, "yeni-gizli") || strings.Contains(iki, testGizli) {
		t.Fatal("gizli değişince eski başlık satırları kaldırılıp yenisi yazılmalı")
	}
}

func TestCatchallUzlastir(t *testing.T) {
	catchall := `server {
    listen 8443 ssl default_server;
    listen [::]:8443 ssl default_server;
    server_name _;
    return 444;
}`
	yeni := CatchallUzlastir(catchall, []int{21001, 22002}, true)
	if strings.Contains(yeni, "8443") {
		t.Error("eski port catchall'dan çıkmalı")
	}
	for _, s := range []string{"listen 21001 ssl default_server;", "listen [::]:22002 ssl default_server;", "return 444;"} {
		if !strings.Contains(yeni, s) {
			t.Errorf("catchall eksik: %q\n%s", s, yeni)
		}
	}
	if CatchallUzlastir(yeni, []int{21001, 22002}, true) != yeni {
		t.Error("CatchallUzlastir idempotent olmalı")
	}
	// IPv6 kanıtı yoksa [::] satırı YAZILMAZ (IPv6'sız çekirdekte nginx -t düşerdi).
	if v4 := CatchallUzlastir(catchall, []int{21001}, false); strings.Contains(v4, "[::]") {
		t.Errorf("ipv6=false iken [::] satırı olmamalı:\n%s", v4)
	}
}

// erisim masaüstü catchall'ı: loopback ve yorumlu satırlar KORUNUR (operatörün seçimi).
func TestCatchallUzlastirLoopbackKorunur(t *testing.T) {
	catchall := `server {
    listen 127.0.0.1:9443 ssl default_server;
    # [erisim:masaustu] listen [::]:9443 ssl default_server;
    server_name _;
    return 444;
}`
	yeni := CatchallUzlastir(catchall, []int{21001}, false)
	for _, s := range []string{"listen 127.0.0.1:9443 ssl default_server;", "# [erisim:masaustu] listen [::]:9443", "listen 21001 ssl default_server;"} {
		if !strings.Contains(yeni, s) {
			t.Errorf("eksik: %q\n%s", s, yeni)
		}
	}
	if CatchallUzlastir(catchall, nil, false) != catchall {
		t.Error("genel port yoksa loopback catchall DEĞİŞMEMELİ")
	}
}

func TestIPv6Etkin(t *testing.T) {
	if !IPv6Etkin(sablonConf) {
		t.Error("şablonda etkin IPv6 var")
	}
	if IPv6Etkin("    listen 8443 ssl;\n    # [erisim:masaustu] listen [::]:8443 ssl;") {
		t.Error("yorumlu IPv6 kanıt DEĞİLDİR")
	}
}

func TestGenelPanelPortu(t *testing.T) {
	a := ornekAyar()
	gecis := "listen 8443 ssl;\nlisten 21001 ssl; # gp-kapi\nlisten 22002 ssl; # gp-kapi\nlisten 23003 ssl; # gp-kapi"
	if p := GenelPanelPortu(gecis, a); p != 23003 {
		t.Errorf("müşteri kapısı canlıysa o dönmeli, got %d", p)
	}
	// Müşteri henüz eklenmemiş (Heal başarısız): admin/bayi DEĞİL, eski port.
	yarim := "listen 21001 ssl; # gp-kapi\nlisten 8443 ssl;\nlisten 22002 ssl; # gp-kapi"
	if p := GenelPanelPortu(yarim, a); p != 8443 {
		t.Errorf("admin/bayi ASLA genel port olmamalı, got %d", p)
	}
	if p := GenelPanelPortu("listen 21001 ssl;\nlisten 22002 ssl;", a); p != 0 {
		t.Errorf("yalnız admin/bayi varsa 0 (çağıran varsayılana düşer), got %d", p)
	}
	if p := GenelPanelPortu("listen 127.0.0.1:9443 ssl;\nlisten 7443 ssl;", nil); p != 7443 {
		t.Errorf("kapı yokken ilk GENEL listen, got %d", p)
	}
}

func TestAcmeYonlendirmeUzlastir(t *testing.T) {
	acme := "    location / {\n        return 301 https://$server_name:8443$request_uri;\n    }"
	yeni := AcmeYonlendirmeUzlastir(acme, 23003)
	if !strings.Contains(yeni, "return 301 https://$server_name:23003$request_uri;") {
		t.Errorf("yönlendirme portu güncellenmeli:\n%s", yeni)
	}
	if AcmeYonlendirmeUzlastir(yeni, 23003) != yeni || AcmeYonlendirmeUzlastir(acme, 0) != acme {
		t.Error("idempotent olmalı; port 0 → dokunma")
	}
}

func TestRastgelePortSec(t *testing.T) {
	eski := geciciPortBaslangici
	t.Cleanup(func() { geciciPortBaslangici = eski })
	geciciPortBaslangici = func() int { return 32768 }

	for i := 0; i < 200; i++ {
		p, err := RastgelePortSec(nil, nil)
		if err != nil || p < PortAltSinir || p > PortUstSinir {
			t.Fatalf("aralık dışı/hata: p=%d err=%v", p, err)
		}
	}
	// Yalnız tek port uygun → her seferinde o (tarama yedeği çalışıyor).
	tek := 25000
	p, err := RastgelePortSec(func(x int) bool { return x == tek }, nil)
	if err != nil || p != tek {
		t.Fatalf("tek uygun port bulunmalı: p=%d err=%v", p, err)
	}
	if _, err := RastgelePortSec(func(x int) bool { return x == tek }, map[int]bool{tek: true}); err == nil {
		t.Fatal("tek uygun port haric'teyse hata dönmeli (sonsuz döngü YOK)")
	}
	// Çekirdek geçici aralığı aşağı çekilmişse üst sınır iner.
	geciciPortBaslangici = func() int { return 25001 }
	for i := 0; i < 200; i++ {
		if p, _ := RastgelePortSec(nil, nil); p > 25000 {
			t.Fatalf("geçici aralığa taşan port: %d", p)
		}
	}
	a, b, m, err := RolPortlariSec(nil, nil)
	if err != nil || a == b || b == m || a == m {
		t.Fatalf("üç rol farklı port almalı: %d %d %d err=%v", a, b, m, err)
	}
}

func TestOkuYaz(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kapi.json")
	if _, err := Oku(yol); !errors.Is(err, ErrYok) {
		t.Fatalf("dosya yokken ErrYok beklenir, got %v", err)
	}
	a := ornekAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 1234
	if err := Yaz(yol, a); err != nil {
		t.Fatalf("Yaz: %v", err)
	}
	if fi, _ := os.Stat(yol); fi.Mode().Perm() != 0o600 {
		t.Errorf("izin 0600 olmalı, %v", fi.Mode().Perm())
	}
	b, err := Oku(yol)
	if err != nil || b.Admin != a.Admin || b.EskiBitis != 1234 || len(b.EskiPortlar) != 1 || b.Surum != 1 {
		t.Fatalf("tur-gidiş bozuk: %+v err=%v", b, err)
	}
	if err := Guncelle(yol, func(x *Ayar) error { x.AdminDogrulandi = 99; return nil }); err != nil {
		t.Fatalf("Guncelle: %v", err)
	}
	if c, _ := Oku(yol); c.AdminDogrulandi != 99 {
		t.Fatal("Guncelle kalıcı olmalı")
	}
	if err := Yaz(yol, &Ayar{Admin: 1, Bayi: 1, Musteri: 1}); err == nil {
		t.Fatal("geçersiz ayar yazılmamalı")
	}
	_ = os.WriteFile(yol, []byte("{bozuk"), 0o600)
	if _, err := Oku(yol); err == nil || errors.Is(err, ErrYok) {
		t.Fatalf("bozuk JSON hata vermeli (ErrYok değil), got %v", err)
	}
}
