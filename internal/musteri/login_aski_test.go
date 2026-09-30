package musteri

// login_aski_test.go — müşteri girişinde askı yanıtı ve numaralandırma koruması.
//
// 🔴 Askı kodu ({"kod":"hesap_askida"}) YALNIZ doğru parolayla döner. Yanlış parolada
// askıdaki hesap, aktif hesap ve hiç olmayan kullanıcı BİREBİR aynı 401'i alır — parola
// bilmeyen biri kullanıcı adının varlığını ya da askı durumunu öğrenemez.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/sahtedb"
)

// askiGirisi: müşteri kapısında tek giriş denemesi. satir nil → kullanıcı yok.
// Dönen sayaç: parola doğrulamasının kaç kez ve hangi hash'le koştuğu.
func askiGirisi(t *testing.T, satir []any, parola string) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	kapidanGelsin(t, kapi.Musteri)
	eskiK, eskiP := kapiGirisKaydet, parolaDogrulaFn
	t.Cleanup(func() { kapiGirisKaydet, parolaDogrulaFn = eskiK, eskiP })
	kapiGirisKaydet = func(string, string, *http.Request, int64) error { return nil }
	var hashler []string
	parolaDogrulaFn = func(duz, saklanan string) bool {
		hashler = append(hashler, saklanan)
		return duz == "parola1" && saklanan == "parola1"
	}
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	if satir != nil {
		s.Satirlar("FROM ftp_accounts", satir)
	}
	rec := httptest.NewRecorder()
	(&Handlers{DB: db, Secret: []byte("musteri-test-anahtari-0123456789-abcdef")}).Login(rec,
		httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login",
			strings.NewReader(`{"kullanici":"c_ornek","parola":"`+parola+`"}`)))
	return rec, hashler
}

func ftpSatiri(durum string, domainAskida int64) []any {
	return []any{int64(7), int64(42), "parola1", durum, "ornek.com", domainAskida}
}

// Yanlış parola: askıdaki (FTP askısı / abonelik askısı), aktif ve olmayan hesap aynı yanıt.
func TestMusteriLoginYanlisParoladaAskiSizmaz(t *testing.T) {
	ref, refHash := askiGirisi(t, nil, "yanlis")
	if ref.Code != http.StatusUnauthorized {
		t.Fatalf("olmayan kullanıcı 401 olmalı: %d %s", ref.Code, ref.Body)
	}
	// Zamanlama koruması: olmayan kullanıcıda da doğrulama (sahte hash'le) koşar.
	if len(refHash) != 1 || refHash[0] != sahteFTPHash {
		t.Fatalf("olmayan kullanıcıda sahte hash'le tek doğrulama beklenir: %v", refHash)
	}
	for ad, satir := range map[string][]any{
		"aktif":           ftpSatiri("active", 0),
		"ftp-askida":      ftpSatiri("suspended", 0),
		"abonelik-askida": ftpSatiri("active", 1),
		"ikisi-askida":    ftpSatiri("suspended", 1),
	} {
		t.Run(ad, func(t *testing.T) {
			rec, _ := askiGirisi(t, satir, "yanlis")
			if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
				t.Fatalf("yanlış parolada ayırt edilebilir yanıt: %d %q ≠ %d %q", rec.Code, rec.Body, ref.Code, ref.Body)
			}
		})
	}
}

// Doğru parola + askı: 403 + makine okunur kod; token YOK. FTP ifadesi yanıtta yok.
func TestMusteriLoginAskiKodu(t *testing.T) {
	for ad, satir := range map[string][]any{
		"ftp-askida":      ftpSatiri("suspended", 0),
		"abonelik-askida": ftpSatiri("active", 1),
	} {
		t.Run(ad, func(t *testing.T) {
			rec, _ := askiGirisi(t, satir, "parola1")
			var b struct {
				Hata  string `json:"hata"`
				Kod   string `json:"kod"`
				Token string `json:"token"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &b)
			if rec.Code != http.StatusForbidden || b.Kod != httpx.KodHesapAskida || b.Hata != httpx.HesapAskidaMesaj || b.Token != "" {
				t.Fatalf("askı 403 + kod beklenir: %d %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "FTP") {
				t.Fatalf("müşteri askı mesajı FTP'den söz etmemeli: %s", rec.Body)
			}
		})
	}
	// Aktif hesap + doğru parola: giriş (kontrol grubu).
	rec, _ := askiGirisi(t, ftpSatiri("active", 0), "parola1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("aktif hesap giriş yapmalı: %d %s", rec.Code, rec.Body)
	}
}
