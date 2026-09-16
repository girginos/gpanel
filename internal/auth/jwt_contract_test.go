package auth

// jwt_contract_test.go — JWT uretim/dogrulama SOZLESMESI (DB GEREKTIRMEZ).
//
// "Giris -> JWT" zincirinin kalbi: token dogru uretiliyor mu ve SAHTE / BOZUK /
// SURESI-DOLMUS / YANLIS-ALG / YANLIS-TIP token REDDEDILIYOR mu? Bu negatif
// kontroller kimlik dogrulamanin regresyona karsi kilididir (kirik surum
// musteriye gitmesin). httptest gerekmez — dogrudan paket API sozlesmesi.

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ct_secret = []byte("birim-test-jwt-anahtari-0123456789-abcdef")

// POZITIF: Issue -> Parse tur-gidis; tum claim alanlari korunmali.
func TestAdminTokenUretVeDogrula(t *testing.T) {
	tok, err := Issue(ct_secret, 3600, 42, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	c, err := Parse(ct_secret, tok)
	if err != nil {
		t.Fatalf("Parse gecerli token-i reddetti: %v", err)
	}
	if c.UserID != 42 || c.Username != "root" || c.Role != "admin" {
		t.Fatalf("claim uyumsuz: uid=%d usr=%q rol=%q", c.UserID, c.Username, c.Role)
	}
	if c.Issuer != "girginospanel" {
		t.Fatalf("issuer=%q, beklenen girginospanel", c.Issuer)
	}
}

// NEGATIF: baska bir sirla imzalanan token REDDEDILMELI (sahtecilik/imza).
func TestAdminTokenYanlisAnahtarReddedilir(t *testing.T) {
	tok, err := Issue(ct_secret, 3600, 1, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := Parse([]byte("tamamen-farkli-bir-anahtar-xxxxxxxxxxxx"), tok); err == nil {
		t.Fatal("GUVENLIK: yanlis anahtarla imzalanmis token KABUL edildi")
	}
}

// NEGATIF: son imza baytini degistir -> HMAC tutmaz -> reddedilir (kurcalama).
func TestAdminTokenKurcalanmisReddedilir(t *testing.T) {
	tok, err := Issue(ct_secret, 3600, 1, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	son := tok[len(tok)-1]
	yeni := byte(0x61)
	if son == 'a' {
		yeni = 'b'
	}
	bozuk := tok[:len(tok)-1] + string(yeni)
	if _, err := Parse(ct_secret, bozuk); err == nil {
		t.Fatal("GUVENLIK: kurcalanmis token KABUL edildi")
	}
}

// NEGATIF: bos token reddedilmeli.
func TestAdminTokenBosReddedilir(t *testing.T) {
	if _, err := Parse(ct_secret, ""); err == nil {
		t.Fatal("bos token KABUL edildi")
	}
}

// NEGATIF: alg=none (klasik JWT zafiyeti) REDDEDILMELI. Parse yalniz HS256 kabul eder.
func TestAdminTokenAlgNoneReddedilir(t *testing.T) {
	noneTok, err := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		UserID: 1, Username: "root", Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "girginospanel",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("none token uretilemedi: %v", err)
	}
	if _, err := Parse(ct_secret, noneTok); err == nil {
		t.Fatal("GUVENLIK: alg=none token KABUL edildi")
	}
}

// NEGATIF: suresi dolmus token reddedilmeli (lifetime negatif -> exp gecmiste).
func TestAdminTokenSuresiDolmusReddedilir(t *testing.T) {
	tok, err := Issue(ct_secret, -3600, 1, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := Parse(ct_secret, tok); err == nil {
		t.Fatal("suresi dolmus token KABUL edildi")
	}
}

// POZITIF: musteri token GenerateMusteri -> ParseMusteri tur-gidis.
func TestMusteriTokenUretVeDogrula(t *testing.T) {
	tok, exp, err := GenerateMusteri(ct_secret, MusteriClaims{
		FTPHesapID: 7, DomainID: 42, Kullanici: "musteri1", AlanAdi: "ornek.com",
	}, 3600)
	if err != nil {
		t.Fatalf("GenerateMusteri: %v", err)
	}
	if exp <= time.Now().Unix() {
		t.Fatalf("exp gecmiste: %d", exp)
	}
	mc, err := ParseMusteri(ct_secret, tok)
	if err != nil {
		t.Fatalf("ParseMusteri gecerli token-i reddetti: %v", err)
	}
	if mc.DomainID != 42 || mc.FTPHesapID != 7 || mc.Tip != "musteri" {
		t.Fatalf("musteri claim uyumsuz: did=%d fhid=%d tip=%q", mc.DomainID, mc.FTPHesapID, mc.Tip)
	}
}

// NEGATIF (tip karisikligi): admin token, musteri parser-inden GECMEMELI.
func TestAdminTokenMusteriOlarakReddedilir(t *testing.T) {
	tok, err := Issue(ct_secret, 3600, 1, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := ParseMusteri(ct_secret, tok); err == nil {
		t.Fatal("GUVENLIK: admin token musteri olarak KABUL edildi (tip karisikligi)")
	}
}

// NEGATIF (tip karisikligi): musteri token, admin parser-inden GECMEMELI
// (musteri issuer girginospanel-musteri; Parse yalniz girginospanel kabul eder).
func TestMusteriTokenAdminOlarakReddedilir(t *testing.T) {
	tok, _, err := GenerateMusteri(ct_secret, MusteriClaims{FTPHesapID: 7, DomainID: 42}, 3600)
	if err != nil {
		t.Fatalf("GenerateMusteri: %v", err)
	}
	if _, err := Parse(ct_secret, tok); err == nil {
		t.Fatal("GUVENLIK: musteri token admin olarak KABUL edildi (tip karisikligi)")
	}
}
