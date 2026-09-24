package portyonetim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/kapi"
)

type kapiPYSahte struct {
	mesgul, fw map[int]bool
	selinux    map[int]string // port → başka servis türü
	conf       string         // _panel.conf
	uygula     int
	uygHata    error
	denetim    []string
	disBasladi bool
	masaustu   bool
}

func kapiPYKur(t *testing.T, a *kapi.Ayar) *kapiPYSahte {
	t.Helper()
	s := &kapiPYSahte{mesgul: map[int]bool{}, fw: map[int]bool{}, selinux: map[int]string{}}
	eskiYol, eskiSis, eskiFw, eskiUyg := kapiAyarYolu, kapiSistemPortu, kapiFwKapaliPort, KapiUygula
	eskiSE, eskiFtp, eskiConf, eskiDis := kapiSELinux, kapiFtpPasif, kapiPanelConf, disBaslat
	eskiMasa := kapiMasaustu
	t.Cleanup(func() {
		kapiAyarYolu, kapiSistemPortu, kapiFwKapaliPort, KapiUygula = eskiYol, eskiSis, eskiFw, eskiUyg
		kapiSELinux, kapiFtpPasif, kapiPanelConf, disBaslat = eskiSE, eskiFtp, eskiConf, eskiDis
		kapiMasaustu = eskiMasa
	})
	kapiMasaustu = func() bool { return s.masaustu }
	kapiAyarYolu = filepath.Join(t.TempDir(), "kapi.json")
	kapiSistemPortu = func() map[int]bool {
		c := map[int]bool{}
		for p := range s.mesgul {
			c[p] = true
		}
		return c
	}
	kapiFwKapaliPort = func() map[int]bool { return s.fw }
	kapiSELinux = func() func(int) string { return func(p int) string { return s.selinux[p] } }
	kapiFtpPasif = func() (int, int) { return 30000, 30100 }
	kapiPanelConf = func() string { return s.conf }
	// 🔴 Gerçek disYurut canlı _panel.conf'u yeniden yazar, nginx reload + firewall yapar:
	// koruma gerilese bile testte ASLA koşmamalı.
	disBaslat = func(*sql.DB, *Is, int, int, *int64) { s.disBasladi = true; TipSerbest("dis") }
	KapiUygula = func(context.Context) error { s.uygula++; return s.uygHata }
	if a != nil {
		if err := kapi.Yaz(kapiAyarYolu, a); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s *kapiPYSahte) handler() *Handler {
	return &Handler{Denetle: func(_ *http.Request, eylem, hedef, detay string, ok bool) {
		s.denetim = append(s.denetim, eylem+"|"+hedef)
	}}
}

func rolAyari() *kapi.Ayar { return &kapi.Ayar{Admin: 21001, Bayi: 22002, Musteri: 23003} }

func TestKapiRolPortuAtaDenetim(t *testing.T) {
	s := kapiPYKur(t, rolAyari())
	s.mesgul[24004] = true
	s.fw[25005] = true
	s.selinux[20048] = "mountd_port_t"
	cases := []struct {
		rol  string
		port int
		hata string
	}{
		{kapi.Musteri, 3306, "kullanılamaz"},        // yasaklı
		{kapi.Musteri, 80, "kullanılamaz"},          // <1024
		{kapi.Musteri, 24004, "başka bir servis"},   // meşgul
		{kapi.Musteri, 25005, "güvenlik duvarında"}, // firewall'da kapalı
		{kapi.Musteri, 30050, "FTP pasif"},          // pure-ftpd pasif aralığı (ss'te görünmez)
		{kapi.Musteri, 20048, "SELinux"},            // başka servise ayrılmış SELinux türü
		{kapi.Musteri, 21001, "zaten admin kapısı"}, // başka rolün portu
		{"root", 26006, "bilinmeyen rol"},
	}
	for _, c := range cases {
		if _, _, err := KapiRolPortuAta(c.rol, c.port); err == nil || !strings.Contains(err.Error(), c.hata) {
			t.Errorf("%s:%d → %q içeren hata beklenirdi, got %v", c.rol, c.port, c.hata, err)
		}
	}
	if _, _, err := KapiRolPortuAta(kapi.Musteri, 23003); !errors.Is(err, kapi.ErrAyniPort) {
		t.Errorf("aynı port ErrAyniPort, got %v", err)
	}
	if a, _ := kapi.Oku(kapiAyarYolu); a.Admin != 21001 || a.Bayi != 22002 || a.Musteri != 23003 || len(a.EskiPortlar) != 0 {
		t.Fatalf("reddedilen istekler ayarı DEĞİŞTİRMEMELİ: %+v", a)
	}
}

func TestKapiRolPortuAtaRastgele(t *testing.T) {
	kapiPYKur(t, rolAyari())
	eski, yeni, err := KapiRolPortuAta(kapi.Bayi, 0)
	if err != nil || eski != 22002 {
		t.Fatalf("eski=%d err=%v", eski, err)
	}
	if yeni < kapi.PortAltSinir || yeni > kapi.PortUstSinir || yeni == 21001 || yeni == 23003 {
		t.Fatalf("rastgele port aralıkta ve diğer rollerden farklı olmalı: %d", yeni)
	}
	if a, _ := kapi.Oku(kapiAyarYolu); a.Bayi != yeni {
		t.Fatal("ayara yazılmalı")
	}
}

// Açık eski port (nginx onu dinliyor → ss "meşgul" der) müşteri kapısı yapılabilmeli —
// ama admin yeni admin kapısını doğrulamadan DEĞİL (devralma geçiş kapısını kapatır).
func TestKapiRolPortuAtaEskiPortuDevralir(t *testing.T) {
	a := rolAyari()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 9_999_999_999
	s := kapiPYKur(t, a)
	s.mesgul[8443] = true
	if _, _, err := KapiRolPortuAta(kapi.Musteri, 8443); !errors.Is(err, kapi.ErrEskiDevralma) {
		t.Fatalf("admin doğrulamadan eski port devralınamamalı, got %v", err)
	}
	rr := httptest.NewRecorder()
	s.handler().KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"musteri","yeni_port":8443}`)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("devralma reddi → 409 (arayüz açıklamayı gösterir), got %d %s", rr.Code, rr.Body)
	}
	_ = kapi.Guncelle(kapiAyarYolu, func(x *kapi.Ayar) error { x.AdminDogrulandi = 1; return nil })
	if _, _, err := KapiRolPortuAta(kapi.Musteri, 8443); err != nil {
		t.Fatalf("panelin kendi eski portu meşgul sayılmamalı: %v", err)
	}
}

// Yalnız-loopback tünel portu (erisim masaüstü; nginx dinliyor → ss "meşgul") YALNIZ
// masaüstü modunda yönetici kapısı olabilir. Bayi/müşteri (ve web modunda yönetici) oraya
// taşınsa Heal genel listen eklemez, eski portu kaldırır → rolün kullanıcıları kilitlenir.
func TestKapiRolPortuAtaYerelPort(t *testing.T) {
	s := kapiPYKur(t, rolAyari())
	s.conf = "server {\n    listen 127.0.0.1:9443 ssl;\n    listen 21001 ssl;\n}"
	s.mesgul[9443] = true
	for _, rol := range []string{kapi.Bayi, kapi.Musteri, kapi.Admin} {
		if _, _, err := KapiRolPortuAta(rol, 9443); err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("web modunda %s kapısı tünel portuna taşınamamalı, got %v", rol, err)
		}
	}
	if a, _ := kapi.Oku(kapiAyarYolu); a.Admin != 21001 || a.Bayi != 22002 || a.Musteri != 23003 {
		t.Fatalf("reddedilen istekler ayarı değiştirmemeli: %+v", a)
	}
	s.masaustu = true
	if _, _, err := KapiRolPortuAta(kapi.Musteri, 9443); err == nil {
		t.Fatal("masaüstü modunda da müşteri kapısı tünel portuna taşınamamalı")
	}
	if _, _, err := KapiRolPortuAta(kapi.Admin, 9443); err != nil {
		t.Fatalf("masaüstü modunda yönetici kapısı tünel portu olabilmeli: %v", err)
	}
	// Rastgele seçim tünel portunu hiçbir zaman seçmez.
	s2 := kapiPYKur(t, rolAyari())
	s2.conf = "server {\n    listen 127.0.0.1:25555 ssl;\n}"
	for i := 0; i < 20; i++ {
		_, y, err := KapiRolPortuAta(kapi.Bayi, 0)
		if err != nil || y == 25555 {
			t.Fatalf("rastgele seçim tünel portunu seçmemeli: %d %v", y, err)
		}
	}
}

func TestDisPortDegistirKapiModundaReddedilir(t *testing.T) {
	s := kapiPYKur(t, rolAyari())
	if _, err := DisPortDegistir(context.Background(), nil, 7443, nil); !errors.Is(err, errKapiModu) {
		t.Fatalf("kapı modunda tek dış port değişimi reddedilmeli (rol listen'ını taşırdı), got %v", err)
	}
	rr := httptest.NewRecorder()
	(&Handler{}).DisDegistir(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"yeni_port":7443}`)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("kapı modu → 409, got %d", rr.Code)
	}
	if s.disBasladi {
		t.Fatal("kapı modunda dış port yürütmesi BAŞLAMAMALI")
	}
}

func TestKapiRolHandler(t *testing.T) {
	s := kapiPYKur(t, rolAyari())
	h := s.handler()
	rr := httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"musteri","yeni_port":24004}`)))
	if rr.Code != 200 {
		t.Fatalf("200 beklenirdi: %d %s", rr.Code, rr.Body)
	}
	var sonuc KapiRolSonuc
	_ = json.Unmarshal(rr.Body.Bytes(), &sonuc)
	if sonuc.EskiPort != 23003 || sonuc.YeniPort != 24004 || sonuc.Uyari != "" {
		t.Fatalf("sonuç: %+v", sonuc)
	}
	if !sonuc.Uygulandi {
		t.Fatal("hatasız uygulama uygulandi=true dönmeli")
	}
	if s.uygula != 1 {
		t.Fatal("değişiklik HEMEN uygulanmalı")
	}
	if len(s.denetim) != 1 || s.denetim[0] != "kapi.rol_port|musteri" {
		t.Fatalf("denetim kaydı: %v", s.denetim)
	}

	// Uygulama başarısız: ayar kaydedilir, uyarı döner (bakım döngüsü yeniden dener).
	s.uygHata = errors.New("nginx -t")
	rr = httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"bayi","yeni_port":25005}`)))
	_ = json.Unmarshal(rr.Body.Bytes(), &sonuc)
	if rr.Code != 200 || sonuc.Uyari == "" || sonuc.Uygulandi {
		t.Fatalf("uygulanamayan değişiklik uyarı + uygulandi=false dönmeli: %d %+v", rr.Code, sonuc)
	}
	// Uygulandı ama zincir doğrulanamadı: arayüz farklı başlık gösterir.
	s.uygHata = &kapi.UygulamaHatasi{Uygulandi: true, Mesaj: "kapı zinciri ölçülemedi"}
	rr = httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"bayi","yeni_port":27007}`)))
	sonuc = KapiRolSonuc{}
	_ = json.Unmarshal(rr.Body.Bytes(), &sonuc)
	if rr.Code != 200 || sonuc.Uyari == "" || !sonuc.Uygulandi {
		t.Fatalf("uygulandı-doğrulanamadı: uyarı + uygulandi=true dönmeli: %d %+v", rr.Code, sonuc)
	}
	// Uygulayıcı "uygulanmadı" dediyse (nginx eski hâlinde) arayüz bunu ayırabilmeli.
	s.uygHata = &kapi.UygulamaHatasi{Mesaj: "nginx kapı ayarına uyumlanamadı"}
	rr = httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"bayi","yeni_port":28008}`)))
	sonuc = KapiRolSonuc{Uygulandi: true}
	_ = json.Unmarshal(rr.Body.Bytes(), &sonuc)
	if rr.Code != 200 || sonuc.Uyari == "" || sonuc.Uygulandi {
		t.Fatalf("uygulanamadı: uyarı + uygulandi=false dönmeli: %d %+v", rr.Code, sonuc)
	}

	rr = httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"musteri","port":1}`)))
	if rr.Code != 400 {
		t.Fatalf("bilinmeyen alan → 400, got %d", rr.Code)
	}
	if !TipKilitle("kapi") {
		t.Fatal("kilit serbest kalmalıydı")
	}
	rr = httptest.NewRecorder()
	h.KapiRol(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"rol":"musteri","yeni_port":26006}`)))
	TipSerbest("kapi")
	if rr.Code != http.StatusConflict {
		t.Fatalf("eşzamanlı kapı işlemi → 409, got %d", rr.Code)
	}
}

func TestKapiEskiHandlerlari(t *testing.T) {
	a := rolAyari()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 1000
	s := kapiPYKur(t, a)
	h := s.handler()

	rr := httptest.NewRecorder()
	h.KapiEskiKapat(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"zorla":false}`)))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "henüz hiç giriş yapmadı") {
		t.Fatalf("admin doğrulamadan kapatma 409 + açıklama: %d %s", rr.Code, rr.Body)
	}
	if s.uygula != 0 {
		t.Fatal("reddedilen işlem uygulanmamalı")
	}

	rr = httptest.NewRecorder()
	h.KapiUzat(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"gun":3}`)))
	if rr.Code != 200 || s.uygula != 1 {
		t.Fatalf("uzat: %d uygulama=%d", rr.Code, s.uygula)
	}
	var yanit struct {
		Kapi KapiDurum `json:"kapi"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &yanit)
	if !yanit.Kapi.EskiAcik || yanit.Kapi.EskiBitis < 3*86400 {
		t.Fatalf("yanıt güncel kapı durumunu taşımalı: %+v", yanit.Kapi)
	}

	rr = httptest.NewRecorder()
	h.KapiEskiKapat(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"zorla":true}`)))
	if rr.Code != 200 {
		t.Fatalf("zorla kapatma: %d %s", rr.Code, rr.Body)
	}
	if d := KapiDurumGetir(); d == nil || len(d.EskiPortlar) != 0 {
		t.Fatalf("zorla sonrası eski port kalmamalı: %+v", d)
	}
	if strings.Join(s.denetim, ",") != "kapi.eski_uzat|eski,kapi.eski_kapat|eski" {
		t.Fatalf("denetim: %v", s.denetim)
	}
}

func TestKapiDurumGetir(t *testing.T) {
	kapiPYKur(t, nil)
	if KapiDurumGetir() != nil {
		t.Fatal("kapı modu kapalıyken nil")
	}
	if kapiModu() {
		t.Fatal("kapi.json yokken kapı modu değil")
	}
	a := rolAyari()
	a.IlkGiris = map[string]int64{kapi.Musteri: 77}
	if err := kapi.Yaz(kapiAyarYolu, a); err != nil {
		t.Fatal(err)
	}
	d := KapiDurumGetir()
	if d == nil || d.IlkGiris[kapi.Musteri] != 77 || d.IlkGiris[kapi.Bayi] != 0 {
		t.Fatalf("rol başına ilk giriş durumu dönmeli: %+v", d)
	}
	if b, _ := json.Marshal(d); !strings.Contains(string(b), `"ilk_giris":{"bayi":0,"musteri":77}`) {
		t.Fatalf("arayüz her iki rolün anahtarını da görmeli: %s", b)
	}
}

func TestFtpPasifAraligi(t *testing.T) {
	eski := ftpConfYolu
	t.Cleanup(func() { ftpConfYolu = eski })
	ftpConfYolu = filepath.Join(t.TempDir(), "yok.conf")
	if alt, ust := ftpPasifAraligi(); alt != 30000 || ust != 30100 {
		t.Fatalf("FTP henüz kurulmamışsa ftp-setup varsayılanı: %d-%d", alt, ust)
	}
	ftpConfYolu = filepath.Join(t.TempDir(), "pure-ftpd.conf")
	// ftp-setup'ın set_directive biçimi; yorumlu satır dikkate alınmaz.
	icerik := "# PassivePortRange 1 2\nPassivePortRange                  40000 40100\n"
	if err := os.WriteFile(ftpConfYolu, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if alt, ust := ftpPasifAraligi(); alt != 40000 || ust != 40100 {
		t.Fatalf("yapılandırılmış aralık okunmalı: %d-%d", alt, ust)
	}
}
