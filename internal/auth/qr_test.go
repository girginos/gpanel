package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

func mask(s string) string {
	if len(s) <= 6 {
		return "****"
	}
	return s[:3] + "..." + s[len(s)-3:]
}

// TestTwoFASetupQR: /me/2fa/setup gercek handler'ini (DB gerektirmez, sadece JWT claims
// okur) httptest ile cagirir ve QR/otpauth zincirini uctan uca kanitlar.
func TestTwoFASetupQR(t *testing.T) {
	key := []byte("test-jwt-secret-0123456789-abcdef")
	h := &Handlers{Secret: key, LifetimeSec: 3600}

	tok, err := Issue(key, 3600, 1, "root", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/me/2fa/setup", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.TwoFASetup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Secret     string `json:"secret"`
		Otpauth    string `json:"otpauth"`
		OtpauthURI string `json:"otpauth_uri"`
		QRDataURI  string `json:"qr_data_uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Secret == "" {
		t.Fatal("secret bos")
	}
	if resp.OtpauthURI == "" || resp.OtpauthURI != resp.Otpauth {
		t.Fatalf("otpauth_uri/otpauth uyumsuz: %q vs %q", resp.OtpauthURI, resp.Otpauth)
	}

	// otpauth_uri gecerli ve DOGRU secret'i icermeli
	u, err := url.Parse(resp.OtpauthURI)
	if err != nil {
		t.Fatalf("otpauth parse: %v", err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("otpauth scheme/host hatali: %q", resp.OtpauthURI)
	}
	if got := u.Query().Get("secret"); got != resp.Secret {
		t.Fatalf("otpauth secret %q != response secret %q", got, resp.Secret)
	}

	// qr_data_uri: gecerli data-URI + PNG magic + 256x256 boyut
	const pfx = "data:image/png;base64,"
	if !strings.HasPrefix(resp.QRDataURI, pfx) {
		t.Fatalf("qr_data_uri onek hatali: %.40s", resp.QRDataURI)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(resp.QRDataURI, pfx))
	if err != nil {
		t.Fatalf("qr base64 decode: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		t.Fatalf("PNG magic yok: % x", raw[:8])
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("qr boyut %dx%d, beklenen 256x256", b.Dx(), b.Dy())
	}

	// QR ISPATI: otpauth_uri'yi yeniden encode et; byte-ozdes olmali => QR tam olarak
	// bu otpauth_uri'yi kodluyor.
	want, err := qrcode.Encode(resp.OtpauthURI, qrcode.Medium, 256)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("QR PNG, otpauth_uri QR'ina esit degil")
	}

	// Enrollment zinciri: QR'daki secret'ten uretilen TOTP kodu /enable gecidinden
	// (TOTPVerify) gecmeli.
	counter := uint64(time.Now().Unix()) / 30
	code, ok := hotp(resp.Secret, counter)
	if !ok {
		t.Fatal("hotp uretemedi")
	}
	if !TOTPVerify(resp.Secret, code) {
		t.Fatal("TOTPVerify gecerli kodu reddetti")
	}

	t.Logf("KANIT setup response: secret=%s otpauth_uri=%s qr_data_uri=%s<%d byte PNG 256x256> totp=%s -> verify OK",
		mask(resp.Secret),
		strings.Replace(resp.OtpauthURI, resp.Secret, mask(resp.Secret), 1),
		pfx, len(raw), code)
}
