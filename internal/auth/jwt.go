package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"usr"`
	Role     string `json:"rol"`
	jwt.RegisteredClaims
}

func Issue(secret []byte, lifetimeSec int, uid int64, username, role string) (string, error) {
	now := time.Now()
	c := Claims{
		UserID:   uid,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(lifetimeSec) * time.Second)),
			Issuer:    "girginospanel",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString(secret)
}

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
	})
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
	Tip        string `json:"tip"` // "musteri"
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
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid || c.Tip != "musteri" {
		return nil, jwt.ErrTokenMalformed
	}
	return c, nil
}

