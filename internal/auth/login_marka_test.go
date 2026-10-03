package auth

// login_marka_test.go — katı giriş (D2 whitelabel): doğrulanmış bir bayi özel panel
// adresinden BAŞKA bayinin girişi, yanlış parolayla birebir aynı 401'i alır; kendi
// adresinden giriş başarılıdır. Host yalnız reddeder (HostKiraciKanca).

import (
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/kapi"
)

func TestResellerLogin_MarkaHostKatiGiris(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("dogru"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	bayi := []any{int64(5), string(hash), "Bayi Bir", "active"}
	eski := HostKiraciKanca
	t.Cleanup(func() { HostKiraciKanca = eski })

	HostKiraciKanca = func(*http.Request) (int64, bool) { return 9, true }
	s, h, _ := girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", bayi)
	ref := girisYap(h, `{"kullanici":"bayi1","parola":"yanlis"}`)
	if ref.Code != http.StatusUnauthorized {
		t.Fatalf("yanlış parola 401 olmalı: %d", ref.Code)
	}

	s, h, _ = girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", bayi)
	s.Satirlar("totp_enabled", []any{int64(0), "", int64(0)})
	rec := girisYap(h, `{"kullanici":"bayi1","parola":"dogru"}`)
	if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
		t.Fatalf("başka bayinin adresinde doğru parola ayırt edilebilir: %d %q ≠ %d %q", rec.Code, rec.Body, ref.Code, ref.Body)
	}
	if s.Sorgulandi("totp_enabled") {
		t.Fatal("reddedilen girişte 2FA durumuna inilmemeli")
	}

	HostKiraciKanca = func(*http.Request) (int64, bool) { return 5, true }
	s, h, _ = girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", bayi)
	s.Satirlar("totp_enabled", []any{int64(0), "", int64(0)})
	if rec := girisYap(h, `{"kullanici":"bayi1","parola":"dogru"}`); rec.Code != http.StatusOK {
		t.Fatalf("kendi adresinden giriş başarılı olmalı: %d %s", rec.Code, rec.Body)
	}
}
