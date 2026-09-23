package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID     int64  `json:"uid"`
	Username   string `json:"usr"`
	Role       string `json:"rol"`
	ResellerID int64  `json:"rid,omitempty"` // reseller token'i icin sahiplik scope-anahtari (kendi users.id)
	// Kapi: token'in alindigi panel kapisi (kapi.Admin/Bayi/Eski; "" = kapi modu
	// kapaliyken alinmis, bagsiz). RequireAuth baska kapida kullanimi reddeder.
	Kapi string `json:"kpi,omitempty"`
	jwt.RegisteredClaims
}

func Issue(secret []byte, lifetimeSec int, uid int64, username, role string) (string, error) {
	return IssueAt(secret, lifetimeSec, uid, username, role, 0, "")
}

// IssueAt: enErkenIat verilirse token'in "iat" degeri en az o degere cekilir.
// Oturum-dusurme damgasiyla ayni saniyeye denk gelen YENI girislerin kendi
// kendini gecersiz kilmasini onler (bkz. middleware.oturumGecerli).
// kapiAdi: token'in baglanacagi panel kapisi (kapi.Istek(r)); "" = bagsiz.
func IssueAt(secret []byte, lifetimeSec int, uid int64, username, role string, enErkenIat int64, kapiAdi string) (string, error) {
	now := time.Now()
	if enErkenIat > now.Unix() {
		now = time.Unix(enErkenIat, 0)
	}
	c := Claims{
		UserID:   uid,
		Username: username,
		Role:     role,
		Kapi:     kapiAdi,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(lifetimeSec) * time.Second)),
			Issuer:    "girginospanel",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString(secret)
}

// IssueReseller: reseller token'i uretir (role=reseller, ResellerID=kendi uid).
func IssueReseller(secret []byte, lifetimeSec int, uid int64, username string, resellerID int64) (string, error) {
	return IssueResellerAt(secret, lifetimeSec, uid, username, resellerID, 0, "")
}

// IssueResellerAt: bkz. IssueAt — bayi tokeni icin ayni davranis.
func IssueResellerAt(secret []byte, lifetimeSec int, uid int64, username string, resellerID int64, enErkenIat int64, kapiAdi string) (string, error) {
	now := time.Now()
	if enErkenIat > now.Unix() {
		now = time.Unix(enErkenIat, 0)
	}
	c := Claims{
		UserID:     uid,
		Username:   username,
		Role:       "reseller",
		ResellerID: resellerID,
		Kapi:       kapiAdi,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(lifetimeSec) * time.Second)),
			Issuer:    "girginospanel",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString(secret)
}

// 🔴 STRICT DECODING NEDEN: HMAC-SHA256 imzası 32 bayt = 43 base64url karakterdir ve
// son karakterin alt 2 biti DOLGUDUR. Gevşek çözücü bu bitleri yok sayar; yani aynı
// token'ın birden çok METİN gösterimi olur (token malleability). Ölçüldü: son
// karakteri değiştirilen 1600 token'dan 96'sı (≈1/16, hepsi 'Y'→'a') "kurcalanmış
// ama geçerli" kabul edildi ve kurcalama testi rastgele %6 düşüyordu. Bugün oturum
// iptali zaman damgası tabanlı olduğu için sömürülemez; ama token METNİNE bağlı bir
// iptal/önbellek/hız-sınırı eklendiği gün atlatılabilirdi. RFC 4648 §3.5.
var jwtAyristiriciSecenekleri = []jwt.ParserOption{jwt.WithStrictDecoding()}

func Parse(secret []byte, raw string) (*Claims, error) {
	if raw == "" {
		return nil, errors.New("boş token")
	}
	c := &Claims{}
	tok, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("beklenmeyen alg")
		}
		return secret, nil
	}, jwtAyristiriciSecenekleri...)
	if err != nil || !tok.Valid {
		return nil, errors.New("geçersiz token")
	}
	if c.Issuer != "girginospanel" || c.Role == "" {
		return nil, errors.New("admin token değil")
	}
	return c, nil
}

// ===== Müşteri token (domain sahibi) =====

type MusteriClaims struct {
	FTPHesapID int64  `json:"fhid"`
	DomainID   int64  `json:"did"`
	Kullanici  string `json:"usr"`
	AlanAdi    string `json:"alan"`
	Tip        string `json:"tip"`           // "musteri"
	Kapi       string `json:"kpi,omitempty"` // alindigi panel kapisi (bkz. Claims.Kapi)
	jwt.RegisteredClaims
}

func GenerateMusteri(secret []byte, c MusteriClaims, lifetimeSec int64) (string, int64, error) {
	now := time.Now()
	c.Tip = "musteri"
	c.RegisteredClaims = jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(lifetimeSec) * time.Second)),
		Issuer:    "girginospanel-musteri",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString(secret)
	return s, c.ExpiresAt.Unix(), err
}

func ParseMusteri(secret []byte, raw string) (*MusteriClaims, error) {
	c := &MusteriClaims{}
	tok, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, jwt.ErrSignatureInvalid
		}
		return secret, nil
	}, jwtAyristiriciSecenekleri...)
	if err != nil {
		return nil, err
	}
	if !tok.Valid || c.Tip != "musteri" {
		return nil, jwt.ErrTokenMalformed
	}
	return c, nil
}
