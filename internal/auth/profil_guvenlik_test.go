package auth

// profil_guvenlik_test.go — oturumlu kimlik uçlarının (2FA etkinleştir/kapat, parola
// değiştir) çalınmış-jeton senaryoları:
//   - 2FA etkinken /me/2fa/enable secret'ı ÜZERİNE YAZAMAZ (2FA devralma);
//   - /me/2fa/disable ve /me/parola hatalı denemeleri hesap başına sınırlıdır.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/sahtedb"
)

const testSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

func profilIstek(t *testing.T, h *Handlers, tok, yol, govde string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, yol, strings.NewReader(govde))
	r.Header.Set("Authorization", "Bearer "+tok)
	return r
}

func profilOrtami(t *testing.T) (*sahtedb.DB, *Handlers) {
	t.Helper()
	denemeSifirla()
	t.Cleanup(denemeSifirla)
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	return s, &Handlers{DB: db, Secret: ct_secret, LifetimeSec: 60}
}

func gecerliKod(t *testing.T, secret string) string {
	t.Helper()
	kod, ok := hotp(secret, uint64(time.Now().Unix()/30))
	if !ok {
		t.Fatal("hotp üretilemedi")
	}
	return kod
}

// 2FA zaten etkinken enable yeni secret yazamaz; etkin değilken yazar.
func TestTwoFAEnableEtkinkenSecretDegismez(t *testing.T) {
	tok, err := Issue(ct_secret, 3600, 1, "root", "admin")
	if err != nil {
		t.Fatal(err)
	}
	govde := `{"secret":"` + testSecret + `","kod":"` + gecerliKod(t, testSecret) + `"}`

	s, h := profilOrtami(t)
	s.Satirlar("SELECT totp_enabled FROM users", []any{int64(1)})
	rec := httptest.NewRecorder()
	h.TwoFAEnable(rec, profilIstek(t, h, tok, "/me/2fa/enable", govde))
	if rec.Code != http.StatusConflict {
		t.Fatalf("2FA etkinken enable 409 vermeli: %d %s", rec.Code, rec.Body)
	}
	if s.Yurutuldu("totp_secret=?") {
		t.Fatal("2FA etkinken totp_secret ÜZERİNE YAZILDI (2FA devralma)")
	}

	s, h = profilOrtami(t)
	s.Satirlar("SELECT totp_enabled FROM users", []any{int64(0)})
	rec = httptest.NewRecorder()
	h.TwoFAEnable(rec, profilIstek(t, h, tok, "/me/2fa/enable", govde))
	if rec.Code != http.StatusOK || !s.Yurutuldu("totp_secret=?") {
		t.Fatalf("2FA kapalıyken enable çalışmalı: %d %s", rec.Code, rec.Body)
	}
}

// Hatalı kodlar sınırlanır: eşikten sonra DOĞRU kod bile 429 alır ve 2FA kapanmaz.
func TestTwoFADisableHataliDenemeSiniri(t *testing.T) {
	tok, _ := Issue(ct_secret, 3600, 1, "root", "admin")
	s, h := profilOrtami(t)
	s.Satirlar("SELECT totp_secret FROM users", []any{testSecret})
	yanlis := `{"kod":"000000"}`
	if gecerliKod(t, testSecret) == "000000" {
		yanlis = `{"kod":"000001"}`
	}
	for i := 0; i < denemeMaks; i++ {
		rec := httptest.NewRecorder()
		h.TwoFADisable(rec, profilIstek(t, h, tok, "/me/2fa/disable", yanlis))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("deneme %d: 400 beklenir: %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.TwoFADisable(rec, profilIstek(t, h, tok, "/me/2fa/disable", `{"kod":"`+gecerliKod(t, testSecret)+`"}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("eşik aşıldı: doğru kod bile 429 almalı: %d %s", rec.Code, rec.Body)
	}
	if s.Yurutuldu("totp_enabled=0") {
		t.Fatal("kilitliyken 2FA kapatıldı")
	}
	// Pencere/kilit geçince tekrar denenebilir.
	eski := denemeSaat
	defer func() { denemeSaat = eski }()
	denemeSaat = func() time.Time { return time.Now().Add(denemeKilit + time.Minute) }
	rec = httptest.NewRecorder()
	h.TwoFADisable(rec, profilIstek(t, h, tok, "/me/2fa/disable", `{"kod":"`+gecerliKod(t, testSecret)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("kilit süresi dolunca doğru kod geçmeli: %d %s", rec.Code, rec.Body)
	}
}

// Bayi mevcut-parola doğrulaması da sınırlıdır.
func TestParolaDegistirHataliDenemeSiniri(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("dogru-parola"), bcrypt.MinCost)
	tok, err := IssueResellerAt(ct_secret, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	s, h := profilOrtami(t)
	s.Satirlar("SELECT password_hash FROM users", []any{string(hash)})
	for i := 0; i < denemeMaks; i++ {
		rec := httptest.NewRecorder()
		h.ParolaDegistir(rec, profilIstek(t, h, tok, "/me/parola", `{"mevcut":"yanlis","yeni":"yeni-parola-1"}`))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("deneme %d: 401 beklenir: %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ParolaDegistir(rec, profilIstek(t, h, tok, "/me/parola", `{"mevcut":"dogru-parola","yeni":"yeni-parola-1"}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("eşik aşıldı: doğru parola bile 429 almalı: %d %s", rec.Code, rec.Body)
	}
	if s.Yurutuldu("SET password_hash") {
		t.Fatal("kilitliyken parola değişti")
	}
}
