package auth

// login_aski_test.go — bayi girişinde askı yanıtı: makine okunur kod, YALNIZ doğru parolayla.

import (
	"encoding/json"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
)

func TestResellerLoginAskiKodu(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("dogru"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	satir := func(durum string) []any { return []any{int64(5), string(hash), "Bayi Bir", durum} }

	// Yanlış parola: askıdaki bayi, aktif bayiyle BİREBİR aynı 401'i alır (askı sızmaz).
	s, h, _ := girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", satir("active"))
	ref := girisYap(h, `{"kullanici":"bayi1","parola":"yanlis"}`)
	s, h, _ = girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", satir("suspended"))
	rec := girisYap(h, `{"kullanici":"bayi1","parola":"yanlis"}`)
	if ref.Code != http.StatusUnauthorized || rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
		t.Fatalf("yanlış parolada askı ayırt edilebilir: %d %q ≠ %d %q", rec.Code, rec.Body, ref.Code, ref.Body)
	}

	// Doğru parola + askı: 403 + kod; 2FA'ya inilmez, token ve kanıt yok.
	s, h, kanit := girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", satir("suspended"))
	rec = girisYap(h, `{"kullanici":"bayi1","parola":"dogru"}`)
	var b httpx.ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusForbidden || b.Kod != httpx.KodHesapAskida || b.Hata != httpx.HesapAskidaMesaj {
		t.Fatalf("askı 403 + kod beklenir: %d %s", rec.Code, rec.Body)
	}
	if s.Sorgulandi("totp_enabled") || len(*kanit) != 0 {
		t.Fatal("askıdaki bayide 2FA'ya inilmemeli, kanıt yazılmamalı")
	}
}
