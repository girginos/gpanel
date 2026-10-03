package middleware

// anahtar_test.go — API anahtarı doğrulaması (DB'siz; dikişlerle). Gerçek chi
// yönlendiricisi main.go ile AYNI biçimde kurulur: Route("/api/v1") + Group(RequireAuth)
// + r.With(rol ara yazılımı). Böylece izin tablosunun RoutePattern() dizgesiyle
// eşleştiği de kanıtlanır (TestAnahtarRoutePatternBicimi).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/kapi"

	"github.com/go-chi/chi/v5"
)

type akSahne struct {
	kayitlar  map[string]*apianahtar.Kayit // onek → kayıt
	ip        string
	hata      error
	sonClaims *auth.Claims
	sonDesen  string
	aktor     string
	redler    []string
}

func akKur(t *testing.T) (*akSahne, http.Handler) {
	t.Helper()
	s := &akSahne{kayitlar: map[string]*apianahtar.Kayit{}, ip: "203.0.113.5"}
	eG, eIP, eSK, eRD, eKapi := anahtarGetir, anahtarIstemciIP, anahtarSonKullanim, anahtarRedDenetim, kapiCoz
	anahtarGetir = func(_ context.Context, onek string) (*apianahtar.Kayit, error) {
		if s.hata != nil {
			return nil, s.hata
		}
		return s.kayitlar[onek], nil
	}
	anahtarIstemciIP = func(*http.Request) string { return s.ip }
	anahtarSonKullanim = func(int64, string) {}
	anahtarRedDenetim = func(_ *http.Request, _ *apianahtar.Kayit, neden string) { s.redler = append(s.redler, neden) }
	kapiCoz = func(*http.Request) string { return "" }
	akSifirla()
	t.Cleanup(func() {
		anahtarGetir, anahtarIstemciIP, anahtarSonKullanim, anahtarRedDenetim, kapiCoz = eG, eIP, eSK, eRD, eKapi
		akSifirla()
	})
	scopeDB = nil
	yakala := func(w http.ResponseWriter, r *http.Request) {
		s.sonClaims = ClaimsFrom(r)
		s.sonDesen = chi.RouteContext(r.Context()).RoutePattern()
		_, s.aktor = Aktor(r)
		w.WriteHeader(http.StatusOK)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", yakala)
		r.Group(func(r chi.Router) {
			r.Use(RequireAuth(mwSecret))
			r.Get("/me", yakala)
			r.With(AdminVeyaReseller).Post("/me/parola", yakala)
			r.With(AdminVeyaReseller).Get("/domains", yakala)
			r.Get("/domains/{id}", yakala)
			r.With(AdminVeyaReseller).Post("/domains", yakala)
			r.Get("/domains/{id}/files", yakala)
			r.Post("/domains/{id}/adina-oturum", yakala)
			r.Post("/databases/{dbId}/pma-token", yakala)
			r.With(AdminOnly).Get("/resellers", yakala)
			r.With(AdminVeyaReseller, YalnizOturum).Post("/api-anahtarlari", yakala)
			r.HandleFunc("/eklenti/{ad}/*", yakala)
			r.Get("/kapi/adres", yakala)
		})
	})
	return s, r
}

func akSifirla() {
	anahtarHata.mu.Lock()
	anahtarHata.kayit = map[string]*hataKayit{}
	anahtarHata.mu.Unlock()
	anahtarKova.mu.Lock()
	anahtarKova.kova = map[int64]*kova{}
	anahtarKova.mu.Unlock()
	redSeyrek.mu.Lock()
	redSeyrek.son = map[int64]time.Time{}
	redSeyrek.mu.Unlock()
}

var akID int64

// akEkle: sahneye geçerli bir anahtar ekler; tam anahtarı döner.
func (s *akSahne) akEkle(t *testing.T, rol string, sahip int64, izinler, ipler []string, degistir func(*apianahtar.Kayit)) string {
	t.Helper()
	u, err := apianahtar.Uret()
	if err != nil {
		t.Fatal(err)
	}
	akID++
	uyeRol := "admin"
	if rol == apianahtar.RolBayi {
		uyeRol = "reseller"
	}
	k := &apianahtar.Kayit{ID: akID, SahipID: sahip, SahipRol: rol, Ad: "WHMCS", Onek: u.Onek, Ozet: u.Ozet, Son4: u.Son4,
		Izinler: izinler, IPler: ipler, HizDk: 120, SonKullanma: time.Now().Add(time.Hour).Unix(),
		SahipAd: "bayi7", SahipDurum: "active", SahipUyeRol: uyeRol}
	if degistir != nil {
		degistir(k)
	}
	s.kayitlar[u.Onek] = k
	if rol == apianahtar.RolKok {
		akKullanici[u.Tam] = apianahtar.KokKullaniciAdi
	} else {
		akKullanici[u.Tam] = k.SahipAd
	}
	return u.Tam
}

// akKullanici: anahtar → sahibinin API kullanıcı adı (akIstek doğru biçimi kurar).
var akKullanici = map[string]string{}

// akIstekHam: Authorization başlığını olduğu gibi gönderir.
func akIstekHam(h http.Handler, metod, yol, baslik string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metod, yol, nil)
	req.Header.Set("Authorization", baslik)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func akIstek(h http.Handler, metod, yol, tok string, basliklar ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metod, yol, nil)
	if tok != "" {
		if apianahtar.AnahtarMi(tok) {
			kul, ok := akKullanici[tok]
			if !ok {
				kul = "bayi7" // sahnede olmayan (sahte/bozuk) anahtar
			}
			req.Header.Set("Authorization", "gosp "+kul+":"+tok)
		} else {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	for i := 0; i+1 < len(basliklar); i += 2 {
		req.Header.Set(basliklar[i], basliklar[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

var kokIP = []string{"203.0.113.0/24"}

func TestAnahtarRoutePatternBicimi(t *testing.T) {
	s, h := akKur(t)
	tok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	w := akIstek(h, "GET", "/api/v1/domains/42", tok)
	if w.Code != 200 {
		t.Fatalf("kök hesap:oku GET /domains/{id}: %d %s", w.Code, w.Body)
	}
	if s.sonDesen != "/api/v1/domains/{id}" {
		t.Fatalf("RoutePattern biçimi tablo anahtarıyla aynı değil: %q", s.sonDesen)
	}
	if _, ok := apianahtar.Rotalar["GET "+s.sonDesen]; !ok {
		t.Fatal("ölçülen desen izin tablosunda yok")
	}
}

func TestAnahtarKimlikBaglami(t *testing.T) {
	s, h := akKur(t)
	kok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	if w := akIstek(h, "GET", "/api/v1/domains", kok); w.Code != 200 {
		t.Fatalf("kök: %d %s", w.Code, w.Body)
	}
	c := s.sonClaims
	if c == nil || c.UserID != 1 || c.Role != "admin" || c.Username != "root" || c.ResellerID != 0 {
		t.Fatalf("kök kimliği yanlış: %+v", c)
	}
	if s.aktor != "root (API anahtarı WHMCS)" {
		t.Fatalf("denetim aktörü: %q", s.aktor)
	}
	bayi := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, nil)
	if w := akIstek(h, "GET", "/api/v1/domains", bayi); w.Code != 200 {
		t.Fatalf("bayi: %d %s", w.Code, w.Body)
	}
	c = s.sonClaims
	if c == nil || c.UserID != 7 || c.Role != "reseller" || c.ResellerID != 7 || c.Username != "bayi7" {
		t.Fatalf("bayi kimliği yanlış (rid=sahip ZORUNLU): %+v", c)
	}
	if !strings.Contains(s.aktor, "(API anahtarı WHMCS)") {
		t.Fatalf("bayi aktörü: %q", s.aktor)
	}
}

func TestAnahtarGecersizler(t *testing.T) {
	s, h := akKur(t)
	simdi := time.Now().Unix()
	iptal := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, func(k *apianahtar.Kayit) { k.IptalAt = simdi - 1 })
	gecis := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, func(k *apianahtar.Kayit) { k.IptalAt = simdi + 3600 })
	sure := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, func(k *apianahtar.Kayit) { k.SonKullanma = simdi - 1 })
	gecerli := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	bilinmeyen, _ := apianahtar.Uret()
	bozukGizli := gecerli[:len(gecerli)-1] + "A"
	if bozukGizli == gecerli {
		bozukGizli = gecerli[:len(gecerli)-1] + "B"
	}
	cases := []struct {
		ad, tok string
		kod     int
	}{
		{"iptal edilmiş", iptal, 401},
		{"geçiş süresindeki eski anahtar hâlâ geçer", gecis, 200},
		{"süresi dolmuş", sure, 401},
		{"bilinmeyen önek", bilinmeyen.Tam, 401},
		{"yanlış gizli (özet tutmuyor)", bozukGizli, 401},
		{"bozuk biçim", "gosp_kisa", 401},
		{"geçerli", gecerli, 200},
	}
	for _, c := range cases {
		if w := akIstek(h, "GET", "/api/v1/domains", c.tok); w.Code != c.kod {
			t.Errorf("%s: %d (beklenen %d) %s", c.ad, w.Code, c.kod, w.Body)
		}
	}
}

func TestAnahtarIPListesi(t *testing.T) {
	s, h := akKur(t)
	kok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, []string{"198.51.100.7/32", "2001:db8::/32"}, nil)
	bayiSerbest := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, nil)
	kokBos := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, nil, nil)
	cases := []struct {
		ad, ip, tok string
		kod         int
	}{
		{"kök: listedeki IP", "198.51.100.7", kok, 200},
		{"kök: listedeki IPv6 ağı", "2001:db8::5", kok, 200},
		{"kök: liste dışı IP", "198.51.100.8", kok, 403},
		{"kök: kanıtlanamayan IP (\"\")", "", kok, 403},
		{"kök: boş liste (bozuk satır) reddedilir", "198.51.100.7", kokBos, 403},
		{"bayi: boş liste = her IP", "192.0.2.99", bayiSerbest, 200},
	}
	for _, c := range cases {
		s.ip = c.ip
		if w := akIstek(h, "GET", "/api/v1/domains", c.tok); w.Code != c.kod {
			t.Errorf("%s: %d (beklenen %d) %s", c.ad, w.Code, c.kod, w.Body)
		}
	}
}

func TestAnahtarKapiMatrisi(t *testing.T) {
	s, h := akKur(t)
	kok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	bayi := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, nil)
	cases := []struct {
		kapi, tok string
		kod       int
	}{
		{"", kok, 200}, {"", bayi, 200},
		{kapi.Eski, kok, 200}, {kapi.Eski, bayi, 200},
		{kapi.Admin, kok, 200}, {kapi.Admin, bayi, 401},
		{kapi.Bayi, kok, 401}, {kapi.Bayi, bayi, 200},
		{kapi.Musteri, kok, 401}, {kapi.Musteri, bayi, 401},
		{kapi.Bilinmeyen, kok, 401}, {kapi.Bilinmeyen, bayi, 401},
	}
	for _, c := range cases {
		k := c.kapi
		kapiCoz = func(*http.Request) string { return k }
		if w := akIstek(h, "GET", "/api/v1/domains", c.tok); w.Code != c.kod {
			t.Errorf("kapı=%q: %d (beklenen %d)", c.kapi, w.Code, c.kod)
		}
	}
}

func TestAnahtarIzinVeYasakUclar(t *testing.T) {
	s, h := akKur(t)
	// Kökün seçebileceği TÜM izinler, yazma düzeyinde.
	var hepsi []string
	for _, g := range apianahtar.Gruplar {
		if g.YalnizOku {
			hepsi = append(hepsi, g.Ad+":oku")
		} else {
			hepsi = append(hepsi, g.Ad+":yaz")
		}
	}
	tam := s.akEkle(t, apianahtar.RolKok, 1, hepsi, kokIP, nil)
	oku := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	bayi := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:yaz"}, nil, nil)
	// Bayi satırına DB'de "bayi:yaz" kurcalanmış olsa bile rol grubu reddeder.
	bayiKurcali := s.akEkle(t, apianahtar.RolBayi, 7, []string{"bayi:yaz"}, nil, nil)
	cases := []struct {
		ad, metod, yol, tok string
		kod                 int
	}{
		// Jeton/oturum basan ve kimlik değiştiren uçlar: TÜM izinlerle bile 403.
		{"/me", "GET", "/api/v1/me", tam, 403},
		{"/me/parola", "POST", "/api/v1/me/parola", tam, 403},
		{"adına oturum", "POST", "/api/v1/domains/42/adina-oturum", tam, 403},
		{"pma jetonu", "POST", "/api/v1/databases/5/pma-token", tam, 403},
		{"anahtar üretme", "POST", "/api/v1/api-anahtarlari", tam, 403},
		{"eklenti vekili", "GET", "/api/v1/eklenti/ai/sohbet", tam, 403},
		{"eklenti vekili POST", "POST", "/api/v1/eklenti/ai/sohbet", tam, 403},
		// Tabloda olmayan uç: varsayılan reddet.
		{"dosya yöneticisi (tabloda yok)", "GET", "/api/v1/domains/42/files", tam, 403},
		// Erişim düzeyi.
		{"oku izniyle yazma", "POST", "/api/v1/domains", oku, 403},
		{"yaz izniyle yazma", "POST", "/api/v1/domains", tam, 200},
		{"yaz oku'yu kapsar", "GET", "/api/v1/domains", bayi, 200},
		// Bayi kök grubuna giremez (izin + AdminOnly çift kilit).
		{"bayi kurcalanmış bayi:yaz", "GET", "/api/v1/resellers", bayiKurcali, 403},
		{"bayi hesap:yaz ile /resellers", "GET", "/api/v1/resellers", bayi, 403},
		{"kök bayi:yaz ile /resellers", "GET", "/api/v1/resellers", tam, 200},
		// Her zaman açık.
		{"kapı adresi", "GET", "/api/v1/kapi/adres", oku, 200},
	}
	for _, c := range cases {
		if w := akIstek(h, c.metod, c.yol, c.tok); w.Code != c.kod {
			t.Errorf("%s: %d (beklenen %d) %s", c.ad, w.Code, c.kod, w.Body)
		}
	}
}

func TestAnahtarHesapVeRolTutarliligi(t *testing.T) {
	s, h := akKur(t)
	askida := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, func(k *apianahtar.Kayit) { k.SahipDurum = "suspended" })
	rolDegisti := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, func(k *apianahtar.Kayit) { k.SahipUyeRol = "user" })
	sahteKok := s.akEkle(t, apianahtar.RolKok, 2, []string{"hesap:oku"}, kokIP, nil)
	silinmis := s.akEkle(t, apianahtar.RolBayi, 9, []string{"hesap:oku"}, nil, func(k *apianahtar.Kayit) { k.SahipUyeRol = ""; k.SahipDurum = "" })
	ridsiz := s.akEkle(t, apianahtar.RolBayi, 0, []string{"hesap:oku"}, nil, nil)
	w := akIstek(h, "GET", "/api/v1/domains", askida)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"kod":"bayi_askida"`) {
		t.Errorf("askıdaki bayi: %d %s", w.Code, w.Body)
	}
	for ad, tok := range map[string]string{"rol değişti": rolDegisti, "uid≠1 kök": sahteKok, "silinmiş sahip": silinmis, "rid=0 bayi": ridsiz} {
		if w := akIstek(h, "GET", "/api/v1/domains", tok); w.Code != 401 {
			t.Errorf("%s: %d (beklenen 401)", ad, w.Code)
		}
	}
}

func TestAnahtarOriginVeVT(t *testing.T) {
	s, h := akKur(t)
	tok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, nil)
	if w := akIstek(h, "GET", "/api/v1/domains", tok, "Origin", "https://kotu.example"); w.Code != 403 {
		t.Errorf("Origin başlıklı istek: %d", w.Code)
	}
	s.hata = errors.New("bağlantı koptu")
	if w := akIstek(h, "GET", "/api/v1/domains", tok); w.Code != 503 {
		t.Errorf("DB hatası fail-closed 503 olmalı: %d", w.Code)
	}
}

func TestAnahtarHizSiniri(t *testing.T) {
	s, h := akKur(t)
	tok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, func(k *apianahtar.Kayit) { k.HizDk = 10 })
	for i := 0; i < 10; i++ {
		if w := akIstek(h, "GET", "/api/v1/domains", tok); w.Code != 200 {
			t.Fatalf("istek %d: %d", i, w.Code)
		}
	}
	w := akIstek(h, "GET", "/api/v1/domains", tok)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || w.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("11. istek 429 + Retry-After olmalı: %d %v", w.Code, w.Header())
	}
}

func TestAnahtarHataKilidi(t *testing.T) {
	_, h := akKur(t)
	sahte, _ := apianahtar.Uret()
	for i := 0; i < 20; i++ {
		_ = akIstek(h, "GET", "/api/v1/domains", sahte.Tam)
	}
	if w := akIstek(h, "GET", "/api/v1/domains", sahte.Tam); w.Code != 429 {
		t.Fatalf("20 hatalı denemeden sonra IP kilitlenmeli: %d", w.Code)
	}
}

func TestAnahtarJWTYoluDegismedi(t *testing.T) {
	_, h := akKur(t)
	tok, err := auth.IssueAt(mwSecret, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// JWT ile /me (anahtara kapalı uç) normal çalışır.
	if w := akIstek(h, "GET", "/api/v1/me", tok); w.Code != 200 {
		t.Fatalf("JWT /me: %d", w.Code)
	}
	if w := akIstek(h, "POST", "/api/v1/api-anahtarlari", tok); w.Code != 200 {
		t.Fatalf("JWT anahtar yönetimi (YalnizOturum geçer): %d", w.Code)
	}
}

func TestAnahtarAktorAdiSigar(t *testing.T) {
	uzun := strings.Repeat("ç", 80)
	a := AnahtarAktorAdi(uzun, uzun)
	if n := len([]rune(a)); n > 64 {
		t.Fatalf("aktör adı 64'ü aştı: %d", n)
	}
	if !strings.Contains(a, "(API anahtarı ") || !strings.HasSuffix(a, ")") {
		t.Fatalf("biçim bozuldu: %q", a)
	}
}

// TestYalnizOturumDogrudan: izin tablosundan BAĞIMSIZ ikinci kilit — anahtar
// kimliğiyle gelen istek YalnizOturum uçlarında 403, JWT kimliğiyle geçer.
func TestYalnizOturumDogrudan(t *testing.T) {
	h := YalnizOturum(http.HandlerFunc(mwOK))
	req := httptest.NewRequest("POST", "/api/v1/api-anahtarlari", nil)
	ctx := context.WithValue(req.Context(), claimsKey, &auth.Claims{UserID: 1, Role: "admin"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(context.WithValue(ctx, anahtarKey, &AnahtarBilgi{ID: 1})))
	if w.Code != 403 {
		t.Fatalf("anahtar kimliği YalnizOturum'u geçti: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != 200 {
		t.Fatalf("JWT kimliği YalnizOturum'da: %d", w.Code)
	}
}

// TestAnahtarKullaniciAdiBagi: anahtar YALNIZ sahibinin kullanıcı adıyla geçer
// ("gosp <kullanıcı>:<anahtar>"); yanlış ad geçersiz anahtarla AYNI 401'i alır ve
// hatalı deneme sayılır; kullanıcı adısız eski "Bearer gosp_…" reddedilir.
func TestAnahtarKullaniciAdiBagi(t *testing.T) {
	s, h := akKur(t)
	bayi := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, nil)
	kok := s.akEkle(t, apianahtar.RolKok, 1, []string{"hesap:oku"}, kokIP, func(k *apianahtar.Kayit) { k.SahipAd = "yonetici" })
	gecersiz, _ := apianahtar.Uret()
	cases := []struct {
		ad, baslik string
		kod        int
	}{
		{"bayi doğru ad", "gosp bayi7:" + bayi, 200},
		{"bayi ad harf duyarsız", "GOSP BAYI7:" + bayi, 200},
		{"bayi yanlış ad", "gosp bayi8:" + bayi, 401},
		{"bayi kökün adıyla", "gosp root:" + bayi, 401},
		{"bayi boş ad", "gosp :" + bayi, 401},
		{"bayi adsız", "gosp " + bayi, 401},
		{"kök 'root'", "gosp root:" + kok, 200},
		{"kök users.username", "gosp yonetici:" + kok, 200},
		{"kök bayinin adıyla", "gosp bayi7:" + kok, 401},
		{"geçersiz anahtar doğru ad", "gosp bayi7:" + gecersiz.Tam, 401},
	}
	for _, c := range cases {
		if w := akIstekHam(h, "GET", "/api/v1/domains", c.baslik); w.Code != c.kod {
			t.Errorf("%s: %d (beklenen %d) %s", c.ad, w.Code, c.kod, w.Body)
		}
	}
	// Yanlış ad ile geçersiz anahtar istemciye AYIRT EDİLEMEZ (aynı gövde).
	a := akIstekHam(h, "GET", "/api/v1/domains", "gosp bayi8:"+bayi).Body.String()
	b := akIstekHam(h, "GET", "/api/v1/domains", "gosp bayi7:"+gecersiz.Tam).Body.String()
	if a != b {
		t.Errorf("yanlış ad yanıtı geçersiz anahtardan ayırt edilebiliyor: %q / %q", a, b)
	}
	if !strings.Contains(strings.Join(s.redler, ","), "kullanıcı adı uyuşmuyor") {
		t.Errorf("yanlış ad denetime ret olarak düşmeli: %v", s.redler)
	}
	// Eski biçim: reddedilir, doğru biçimi söyler.
	w := akIstekHam(h, "GET", "/api/v1/domains", "Bearer "+bayi)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Authorization: gosp ") {
		t.Fatalf("Bearer gosp_ reddedilmeli ve biçimi söylemeli: %d %s", w.Code, w.Body)
	}
}

// Yanlış kullanıcı adı ve eski biçim hatalı deneme sayılır (IP kilidi).
func TestAnahtarKullaniciAdiKilit(t *testing.T) {
	for _, eski := range []bool{false, true} {
		s, h := akKur(t)
		bayi := s.akEkle(t, apianahtar.RolBayi, 7, []string{"hesap:oku"}, nil, nil)
		baslik := "gosp bayi8:" + bayi
		if eski {
			baslik = "Bearer " + bayi
		}
		for i := 0; i < 20; i++ {
			_ = akIstekHam(h, "GET", "/api/v1/domains", baslik)
		}
		if w := akIstekHam(h, "GET", "/api/v1/domains", "gosp bayi7:"+bayi); w.Code != 429 {
			t.Errorf("eski=%v: 20 hatalı denemeden sonra IP kilitlenmeli: %d", eski, w.Code)
		}
	}
}
