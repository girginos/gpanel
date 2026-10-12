package domains

// handlers_dbmusteri_test.go — müşteri panelindeki veritabanı işlemleri.
//
// 🔴 Hata (2026-10-12, .180): müşteri ekranında "Parola Sıfırla", "Optimize Et" ve "Sil"
// düğmeleri vardı ama uçlar DBSahipligi (yalnız kök/bayi) arkasındaydı → müşteri
// kendi DB'sinde 404 "veritabanı bulunamadı" alıyordu (taşınmış ya da yerli her site).
// Artık (kullanıcı kararı): hosting sahibi KENDİ DB'sinde parola/optimize/silme yapar;
// bayi kendi hosting hesaplarının DB'lerinde; başka domainin DB'si yine 404 (varlık
// sızmaz); askıdaki hesap DB işlemi yapamaz; optimize üst üste yığılmaz.
//
// Zincir main.go ile AYNI (dp_router). Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN
// yoksa ATLANIR); MariaDB hesap/optimize adımları SAHTE (canlı sunucuya dokunulmaz).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/auth"
)

// dm_musteri: kurban.test (domain 30, FTP 900) müşterisinin olağan oturumu.
func dm_musteri(t *testing.T, fhid, domainID int64) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(dp_sir, auth.MusteriClaims{
		FTPHesapID: fhid, DomainID: domainID, Kullanici: "musteri", AlanAdi: "kurban.test",
	}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// dm_sso: WHMCS tek tık girişi = kök aktörlü adına-oturum (SSO).
func dm_sso(t *testing.T, domainID int64) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(dp_sir, auth.MusteriClaims{
		DomainID: domainID, Kullanici: "musteri", AlanAdi: "kurban.test",
		AktorUID: 1, AktorAd: "root", AktorRol: "admin", SSO: true,
	}, auth.AdinaOmurSn)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func dm_istek(t *testing.T, h *Handlers, tok, metot, yol string, govde any) (int, string) {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(metot, yol, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	dp_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// dm_optSahte: optimize adımlarının sahtesi + sayaç; durum her testte sıfırlanır.
type dm_optSahte struct {
	mu    sync.Mutex
	adlar []string
	bekle chan struct{} // doluysa optimize bu kanal kapanana dek sürer
	basla chan struct{}
}

func (s *dm_optSahte) sayi() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.adlar) }

func dm_optKur(t *testing.T) *dm_optSahte {
	t.Helper()
	s := &dm_optSahte{}
	eskiCalistir, eskiBoyut, eskiSimdi := dbOptimizeCalistir, dbBoyutFn, optSimdi
	dbOptimizeCalistir = func(_ context.Context, dbName string) ([]byte, error) {
		s.mu.Lock()
		s.adlar = append(s.adlar, dbName)
		bekle, basla := s.bekle, s.basla
		s.mu.Unlock()
		if basla != nil {
			close(basla)
		}
		if bekle != nil {
			<-bekle
		}
		return []byte("OK"), nil
	}
	dbBoyutFn = func(context.Context, string) int64 { return 0 }
	optSonMu.Lock()
	optSon = map[int64]time.Time{}
	optSonMu.Unlock()
	t.Cleanup(func() {
		dbOptimizeCalistir, dbBoyutFn, optSimdi = eskiCalistir, eskiBoyut, eskiSimdi
		optSonMu.Lock()
		optSon = map[int64]time.Time{}
		optSonMu.Unlock()
	})
	return s
}

// dm_silKur: DB silme sahtesi (DROP gönderilmez) + çağrı listesi "domain:db".
func dm_silKur(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	cagri := []string{}
	eski := dbSilSahipli
	dbSilSahipli = func(_ *sql.DB, domainID int64, dbName, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		cagri = append(cagri, fmt.Sprintf("%d:%s", domainID, dbName))
		return nil
	}
	t.Cleanup(func() { dbSilSahipli = eski })
	return &cagri
}

// dm_parolaBasarili: ALTER USER sahtesi BAŞARILI döner (müşterinin meşru sıfırlaması).
func dm_parolaBasarili(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	cagri := []string{}
	eski := dbParolaDegistir
	dbParolaDegistir = func(_ *sql.DB, domainID int64, dbUser, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		cagri = append(cagri, fmt.Sprintf("%d:%s", domainID, dbUser))
		return nil
	}
	t.Cleanup(func() { dbParolaDegistir = eski })
	return &cagri
}

// 🔴 ÇEKİRDEK: müşteri KENDİ DB'sinde parola sıfırlar ve optimize eder (eskiden 404).
func TestMusteriDB_KendiDBsindeParolaVeOptimize(t *testing.T) {
	h, _ := dp_kur(t)
	opt := dm_optKur(t)
	cagri := dm_parolaBasarili(t)
	m := dm_musteri(t, 900, 30)

	kod, govde := dm_istek(t, h, m, http.MethodPut, "/databases/300/password", map[string]string{"parola": "Yeni-Guclu-Parola-1"})
	if kod != http.StatusOK {
		t.Fatalf("müşteri kendi DB parolası: 200 beklenir, alınan %d %s", kod, govde)
	}
	if len(*cagri) != 1 || (*cagri)[0] != "30:kurban_db" {
		t.Errorf("ALTER USER yanlış hedef/sayı: %v", *cagri)
	}
	kod, govde = dm_istek(t, h, m, http.MethodPost, "/databases/300/optimize", nil)
	if kod != http.StatusOK || !strings.Contains(govde, `"db_adi":"kurban_main"`) {
		t.Fatalf("müşteri kendi DB optimize: 200 beklenir, alınan %d %s", kod, govde)
	}
	if opt.sayi() != 1 {
		t.Errorf("optimize çağrısı = %d, 1 beklenir", opt.sayi())
	}
	// Taşınmış orijinal adlı kullanıcı (wp-config DB_USER) da aynı yoldan: 302 wpu_kurban1.
	if kod, govde = dm_istek(t, h, m, http.MethodPut, "/databases/302/password", map[string]string{"parola": "Yeni-Guclu-Parola-2"}); kod != http.StatusOK {
		t.Errorf("taşınmış ad (wpu_kurban1): 200 beklenir, alınan %d %s", kod, govde)
	}
	// WHMCS SSO (kök aktörlü adına-oturum) da müşteri yetkisiyle geçer.
	if kod, govde = dm_istek(t, h, dm_sso(t, 30), http.MethodPost, "/databases/302/optimize", nil); kod != http.StatusOK {
		t.Errorf("SSO müşteri optimize: 200 beklenir, alınan %d %s", kod, govde)
	}
}

// Başka domainin DB'si müşteriye görünmez: 404, MariaDB'ye inilmez.
func TestMusteriDB_BaskaDomaininDBsi404(t *testing.T) {
	h, sahte := dp_kur(t)
	opt := dm_optKur(t)
	cagri := dm_parolaBasarili(t)
	sil := dm_silKur(t)
	m := dm_musteri(t, 900, 30)
	for _, c := range []struct{ metot, yol string }{
		{http.MethodPut, "/databases/200/password"},
		{http.MethodPost, "/databases/200/optimize"},
		{http.MethodDelete, "/databases/200"},
		{http.MethodPut, "/databases/99999/password"},
	} {
		kod, govde := dm_istek(t, h, m, c.metot, c.yol, map[string]string{"parola": "Yeni-Guclu-Parola-1", "kullanici": "kurban_x"})
		if kod != http.StatusNotFound {
			t.Errorf("%s %s: 404 beklenir, alınan %d %s", c.metot, c.yol, kod, govde)
		}
	}
	if len(*cagri) != 0 || opt.sayi() != 0 || sahte.sayi() != 0 || len(*sil) != 0 {
		t.Errorf("başka domainin DB'sine inildi: parola=%v optimize=%d ekle=%d sil=%v", *cagri, opt.sayi(), sahte.sayi(), *sil)
	}
	// SSO oturumu da başka domainin DB'sine geçemez.
	for _, c := range []struct{ metot, yol string }{
		{http.MethodPost, "/databases/200/optimize"},
		{http.MethodDelete, "/databases/200"},
	} {
		if kod, _ := dm_istek(t, h, dm_sso(t, 30), c.metot, c.yol, nil); kod != http.StatusNotFound {
			t.Errorf("SSO başka domain %s %s: 404 beklenir, alınan %d", c.metot, c.yol, kod)
		}
	}
	// Bayi yalnız KENDİ hosting hesaplarının DB'sini silebilir (200 bayi2'nin; bayi 3 değil).
	if kod, _ := dm_istek(t, h, dp_bayi(t, 3), http.MethodDelete, "/databases/200", nil); kod != http.StatusNotFound {
		t.Errorf("bayi başka bayinin DB'sini silme: 404 beklenir, alınan %d", kod)
	}
	if len(*sil) != 0 {
		t.Errorf("yabancı DB silindi: %v", *sil)
	}
}

// Kullanıcı kararı (2026-10-12): hosting sahibi KENDİ DB'sini, bayi KENDİ hosting
// hesaplarının DB'lerini siler (eskiden müşteriye 404).
func TestMusteriDB_SilmeKendiDBsinde(t *testing.T) {
	h, _ := dp_kur(t)
	sil := dm_silKur(t)
	for _, c := range []struct {
		ad, tok string
		dbid    int64
		hedef   string
	}{
		{"müşteri", dm_musteri(t, 900, 30), 300, "30:kurban_main"},
		{"SSO (WHMCS)", dm_sso(t, 30), 302, "30:wp_kurban1"},
		{"bayi (kendi hostingi)", dp_bayi(t, 2), 200, "20:bayi2_geri"},
	} {
		kod, govde := dm_istek(t, h, c.tok, http.MethodDelete, fmt.Sprintf("/databases/%d", c.dbid), nil)
		if kod != http.StatusOK || !strings.Contains(govde, `"ok":true`) {
			t.Errorf("%s silme: 200 beklenir, alınan %d %s", c.ad, kod, govde)
			continue
		}
		if n := len(*sil); n == 0 || (*sil)[n-1] != c.hedef {
			t.Errorf("%s silme yanlış hedef: %v (beklenen %s)", c.ad, *sil, c.hedef)
		}
	}
	// Demo abonelik koruması müşteri için de geçerli.
	dp_exec(t, h.DB, `UPDATE domains SET is_demo=1 WHERE id=30`)
	once := len(*sil)
	if kod, govde := dm_istek(t, h, dm_musteri(t, 900, 30), http.MethodDelete, "/databases/301", nil); kod != http.StatusForbidden {
		t.Errorf("demo abonelikte silme: 403 beklenir, alınan %d %s", kod, govde)
	}
	if len(*sil) != once {
		t.Errorf("demo abonelikte DB silindi")
	}
}

// Askıdaki hesap (domains.askida) DB işlemi yapamaz: kodlu 403 (askı sayfası).
func TestMusteriDB_AskidakiHesapReddedilir(t *testing.T) {
	h, _ := dp_kur(t)
	opt := dm_optKur(t)
	cagri := dm_parolaBasarili(t)
	sil := dm_silKur(t)
	dp_exec(t, h.DB, `UPDATE domains SET askida=1, askida_kaynak='bayi' WHERE id=30`)
	m := dm_musteri(t, 900, 30)
	for _, c := range []struct{ metot, yol string }{
		{http.MethodPut, "/databases/300/password"},
		{http.MethodPost, "/databases/300/optimize"},
		{http.MethodDelete, "/databases/300"},
	} {
		kod, govde := dm_istek(t, h, m, c.metot, c.yol, map[string]string{"parola": "Yeni-Guclu-Parola-1"})
		if kod != http.StatusForbidden || !strings.Contains(govde, "hesap_askida") {
			t.Errorf("askıda %s %s: kodlu 403 beklenir, alınan %d %s", c.metot, c.yol, kod, govde)
		}
	}
	if len(*cagri) != 0 || opt.sayi() != 0 || len(*sil) != 0 {
		t.Errorf("askıdaki hesapta işlem yapıldı: parola=%v optimize=%d sil=%v", *cagri, opt.sayi(), *sil)
	}
}

// Müşteri, boş-kullanıcılı KENDİ DB'sinde de devralma kapısından geçemez (bayiyle aynı kurallar).
func TestMusteriDB_BosKullanici_DevralmaKapisi(t *testing.T) {
	h, sahte := dp_kur(t)
	m := dm_musteri(t, 900, 30)
	for _, kul := range []string{"root", "pma", "bayi2_x", "c_bayi2_db"} {
		once := sahte.sayi()
		kod, govde := dm_istek(t, h, m, http.MethodPut, "/databases/301/password", map[string]string{"parola": "Yeni-Guclu-Parola-1", "kullanici": kul})
		if kod != http.StatusConflict || sahte.sayi() != once {
			t.Errorf("müşteri devralma (%q): 409 + MariaDB'ye inmemek beklenir, alınan %d %s", kul, kod, govde)
		}
	}
	if kod, govde := dm_istek(t, h, m, http.MethodPut, "/databases/301/password", map[string]string{"parola": "Yeni-Guclu-Parola-1", "kullanici": "kurban_blog"}); kod != http.StatusOK {
		t.Errorf("müşteri kendi ad alanında kullanıcı oluşturma: 200 beklenir, alınan %d %s", kod, govde)
	}
}

// Optimize yığılmaz: müşteri aynı DB'yi bekleme süresi dolmadan tekrar çalıştıramaz (429);
// kök/bayi beklemez; aynı anda ikinci istek 409.
func TestMusteriDB_OptimizeBeklemeVeEsZamanlilik(t *testing.T) {
	h, _ := dp_kur(t)
	opt := dm_optKur(t)
	simdi := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	optSimdi = func() time.Time { return simdi }
	m := dm_musteri(t, 900, 30)
	if kod, govde := dm_istek(t, h, m, http.MethodPost, "/databases/300/optimize", nil); kod != http.StatusOK {
		t.Fatalf("ilk optimize: 200 beklenir, alınan %d %s", kod, govde)
	}
	simdi = simdi.Add(3 * time.Minute)
	kod, govde := dm_istek(t, h, m, http.MethodPost, "/databases/300/optimize", nil)
	if kod != http.StatusTooManyRequests || !strings.Contains(govde, "7 dakika") {
		t.Errorf("bekleme içinde: 429 + kalan süre beklenir, alınan %d %s", kod, govde)
	}
	// Bayi (hesabın sahibi) beklemeye takılmaz.
	if kod, govde := dm_istek(t, h, dp_bayi(t, 3), http.MethodPost, "/databases/300/optimize", nil); kod != http.StatusOK {
		t.Errorf("bayi (kurbanın sahibi) beklemeye takılmamalı: alınan %d %s", kod, govde)
	}
	// Bekleme, KİM çalıştırırsa çalıştırsın son başarılı optimizeden sayılır (bayinin
	// az önceki optimizesi müşteriyi de bekletir): +3 dk'daki bayi → +13 dk'ya dek 429.
	simdi = simdi.Add(8 * time.Minute)
	if kod, _ := dm_istek(t, h, m, http.MethodPost, "/databases/300/optimize", nil); kod != http.StatusTooManyRequests {
		t.Errorf("bayinin optimizesinden 8 dk sonra: 429 beklenir, alınan %d", kod)
	}
	simdi = simdi.Add(3 * time.Minute)
	if kod, govde := dm_istek(t, h, m, http.MethodPost, "/databases/300/optimize", nil); kod != http.StatusOK {
		t.Errorf("süre dolunca: 200 beklenir, alınan %d %s", kod, govde)
	}
	if opt.sayi() != 3 {
		t.Errorf("optimize çağrısı = %d, 3 beklenir", opt.sayi())
	}

	// Eş zamanlı: ilk istek sürerken ikincisi 409.
	opt.mu.Lock()
	opt.bekle, opt.basla = make(chan struct{}), make(chan struct{})
	bekle, basla := opt.bekle, opt.basla
	opt.mu.Unlock()
	bitti := make(chan int, 1)
	go func() {
		k, _ := dm_istek(t, h, dp_bayi(t, 3), http.MethodPost, "/databases/302/optimize", nil)
		bitti <- k
	}()
	<-basla
	opt.mu.Lock()
	opt.bekle, opt.basla = nil, nil
	opt.mu.Unlock()
	if kod, govde := dm_istek(t, h, dp_bayi(t, 3), http.MethodPost, "/databases/302/optimize", nil); kod != http.StatusConflict {
		t.Errorf("eş zamanlı ikinci optimize: 409 beklenir, alınan %d %s", kod, govde)
	}
	close(bekle)
	if k := <-bitti; k != http.StatusOK {
		t.Errorf("ilk (bekleyen) optimize: 200 beklenir, alınan %d", k)
	}
	if kod, _ := dm_istek(t, h, dp_bayi(t, 3), http.MethodPost, "/databases/302/optimize", nil); kod != http.StatusOK {
		t.Errorf("ilk bitince kilit kalkmalı: alınan %d", kod)
	}
}
