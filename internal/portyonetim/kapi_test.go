package portyonetim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/kapi"
)

type kapiPYSahte struct {
	mesgul, fw map[int]bool
	uygula     int
	uygHata    error
	denetim    []string
}

func kapiPYKur(t *testing.T, a *kapi.Ayar) *kapiPYSahte {
	t.Helper()
	s := &kapiPYSahte{mesgul: map[int]bool{}, fw: map[int]bool{}}
	eskiYol, eskiSis, eskiFw, eskiUyg := kapiAyarYolu, kapiSistemPortu, kapiFwKapaliPort, KapiUygula
	t.Cleanup(func() {
		kapiAyarYolu, kapiSistemPortu, kapiFwKapaliPort, KapiUygula = eskiYol, eskiSis, eskiFw, eskiUyg
	})
	kapiAyarYolu = filepath.Join(t.TempDir(), "kapi.json")
	kapiSistemPortu = func() map[int]bool {
		c := map[int]bool{}
		for p := range s.mesgul {
			c[p] = true
		}
		return c
	}
	kapiFwKapaliPort = func() map[int]bool { return s.fw }
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
	cases := []struct {
		rol  string
		port int
		hata string
	}{
		{kapi.Musteri, 3306, "kullanılamaz"},        // yasaklı
		{kapi.Musteri, 80, "kullanılamaz"},          // <1024
		{kapi.Musteri, 24004, "başka bir servis"},   // meşgul
		{kapi.Musteri, 25005, "güvenlik duvarında"}, // firewall'da kapalı
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

// Açık eski port (nginx onu dinliyor → ss "meşgul" der) müşteri kapısı yapılabilmeli.
func TestKapiRolPortuAtaEskiPortuDevralir(t *testing.T) {
	a := rolAyari()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 9_999_999_999
	s := kapiPYKur(t, a)
	s.mesgul[8443] = true
	if _, _, err := KapiRolPortuAta(kapi.Musteri, 8443); err != nil {
		t.Fatalf("panelin kendi eski portu meşgul sayılmamalı: %v", err)
	}
}

func TestDisPortDegistirKapiModundaReddedilir(t *testing.T) {
	kapiPYKur(t, rolAyari())
	if _, err := DisPortDegistir(context.Background(), nil, 7443, nil); !errors.Is(err, errKapiModu) {
		t.Fatalf("kapı modunda tek dış port değişimi reddedilmeli (rol listen'ını taşırdı), got %v", err)
	}
	rr := httptest.NewRecorder()
	(&Handler{}).DisDegistir(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"yeni_port":7443}`)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("kapı modu → 409, got %d", rr.Code)
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
	if rr.Code != 200 || sonuc.Uyari == "" {
		t.Fatalf("uygulama hatası uyarı olarak dönmeli: %d %+v", rr.Code, sonuc)
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
}
