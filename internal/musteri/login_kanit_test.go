package musteri

// login_kanit_test.go — müşteri girişinin token kapısı ve kapı kanıtı (ADR-GP-001).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/kapi"
	"girginospanel/internal/sahtedb"
)

func TestMusteriLoginTokenKapisiVeKanit(t *testing.T) {
	sir := []byte("musteri-test-anahtari-0123456789-abcdef")
	for _, k := range []string{kapi.Musteri, "", kapi.Eski} {
		t.Run("kapi="+k, func(t *testing.T) {
			kapidanGelsin(t, k)
			eski := kapiGirisKaydet
			t.Cleanup(func() { kapiGirisKaydet = eski })
			var kanit []string
			kapiGirisKaydet = func(_, rol string, _ *http.Request, _ int64) error {
				kanit = append(kanit, rol)
				return nil
			}
			s, db := sahtedb.Yeni()
			t.Cleanup(func() { db.Close() })
			// Düz-metin satır: geçişi tamamlanmamış FTP hesabı (sabit zamanlı karşılaştırma).
			s.Satirlar("FROM ftp_accounts", []any{int64(7), int64(42), "parola1", "active", "ornek.com", int64(0)})
			h := &Handlers{DB: db, Secret: sir}
			rec := httptest.NewRecorder()
			h.Login(rec, httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login",
				strings.NewReader(`{"kullanici":"c_ornek","parola":"parola1"}`)))
			if rec.Code != http.StatusOK {
				t.Fatalf("giriş başarılı olmalı: %d %s", rec.Code, rec.Body)
			}
			var v struct {
				Token string `json:"token"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &v)
			mc, err := auth.ParseMusteri(sir, v.Token)
			if err != nil || mc.Kapi != k || mc.DomainID != 42 {
				t.Fatalf("token girilen kapıya bağlanmalı: %+v err=%v", mc, err)
			}
			if strings.Join(kanit, ",") != kapi.Musteri {
				t.Fatalf("müşteri kanıtı bir kez denenmeli: %v", kanit)
			}
		})
	}
}

// Yanlış parola kanıt yazmaz.
func TestMusteriLoginYanlisParolaKanitYazmaz(t *testing.T) {
	kapidanGelsin(t, kapi.Musteri)
	eski := kapiGirisKaydet
	t.Cleanup(func() { kapiGirisKaydet = eski })
	kapiGirisKaydet = func(string, string, *http.Request, int64) error {
		t.Error("başarısız giriş kanıt yazmamalı")
		return nil
	}
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	s.Satirlar("FROM ftp_accounts", []any{int64(7), int64(42), "parola1", "active", "ornek.com", int64(0)})
	rec := httptest.NewRecorder()
	(&Handlers{DB: db, Secret: []byte("musteri-test-anahtari-0123456789-abcdef")}).Login(rec,
		httptest.NewRequest(http.MethodPost, "/api/v1/musteri/login", strings.NewReader(`{"kullanici":"c_ornek","parola":"yanlis"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yanlış parola → 401, got %d", rec.Code)
	}
}
