package musteri

// login_marka_test.go — katı giriş (D2 whitelabel): bayi özel panel adresinden YALNIZ o
// bayinin müşterisi girer; başkası (askıdaki hesap dahil) yanlış parolayla aynı 401'i alır.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/kapi"
	"girginospanel/internal/sahtedb"
)

func markaGirisi(t *testing.T, hostRID int64, hostVar bool, domainRID int64, satir []any, parola string) *httptest.ResponseRecorder {
	t.Helper()
	kapidanGelsin(t, kapi.Musteri)
	eskiK, eskiP, eskiH := kapiGirisKaydet, parolaDogrulaFn, HostKiraciKanca
	t.Cleanup(func() { kapiGirisKaydet, parolaDogrulaFn, HostKiraciKanca = eskiK, eskiP, eskiH })
	kapiGirisKaydet = func(string, string, *http.Request, int64) error { return nil }
	parolaDogrulaFn = func(duz, saklanan string) bool { return duz == "parola1" && saklanan == "parola1" }
	HostKiraciKanca = func(*http.Request) (int64, bool) { return hostRID, hostVar }
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	s.Satirlar("FROM ftp_accounts", satir)
	s.Satirlar("SELECT COALESCE(reseller_id,0) FROM domains WHERE id", []any{domainRID})
	rec := httptest.NewRecorder()
	(&Handlers{DB: db, Secret: []byte("musteri-test-anahtari-0123456789-abcdef")}).Login(rec,
		httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login",
			strings.NewReader(`{"kullanici":"c_ornek","parola":"`+parola+`"}`)))
	return rec
}

func TestMusteriLogin_MarkaHostKatiGiris(t *testing.T) {
	ref := markaGirisi(t, 7, true, 8, ftpSatiri("active", 0), "yanlis")
	if ref.Code != http.StatusUnauthorized {
		t.Fatalf("yanlış parola 401 olmalı: %d", ref.Code)
	}
	for ad, satir := range map[string][]any{"aktif": ftpSatiri("active", 0), "askida": ftpSatiri("suspended", 0)} {
		rec := markaGirisi(t, 7, true, 8, satir, "parola1")
		if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
			t.Errorf("%s: başka bayinin adresinde doğru parola ayırt edilebilir: %d %q ≠ %d %q", ad, rec.Code, rec.Body, ref.Code, ref.Body)
		}
	}
	if rec := markaGirisi(t, 7, true, 7, ftpSatiri("active", 0), "parola1"); rec.Code != http.StatusOK {
		t.Errorf("kendi bayisinin adresinden giriş başarılı olmalı: %d %s", rec.Code, rec.Body)
	}
	if rec := markaGirisi(t, 0, false, 8, ftpSatiri("active", 0), "parola1"); rec.Code != http.StatusOK {
		t.Errorf("markasız (kök) adreste kural uygulanmamalı: %d %s", rec.Code, rec.Body)
	}
}
