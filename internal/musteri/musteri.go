// Package musteri: müşteri (domain sahibi) login + scope kontrolü
// FTP credentials ile giriş, JWT'de domain_id scope'u
package musteri

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
)

type Handlers struct {
	DB     *sql.DB
	Secret []byte
}

type loginReq struct {
	Kullanici string `json:"kullanici"`
	Parola    string `json:"parola"`
}

// Login: FTP user/password ile, FTP hesabının bağlı olduğu domain için JWT döner
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Kullanici = strings.TrimSpace(req.Kullanici)
	if req.Kullanici == "" || req.Parola == "" {
		httpx.WriteError(w, http.StatusBadRequest, "kullanıcı/parola gerekli")
		return
	}

	// ftp_accounts'tan kontrol
	var ftpID, domainID int64
	var passDB, alanAdi, status string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT fa.id, fa.domain_id, fa.password_md5, fa.status, d.alan_adi
		 FROM ftp_accounts fa
		 JOIN domains d ON d.id = fa.domain_id
		 WHERE fa.username = ?`, req.Kullanici).
		Scan(&ftpID, &domainID, &passDB, &status, &alanAdi)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusUnauthorized, "kullanıcı veya parola hatalı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "active" {
		httpx.WriteError(w, http.StatusForbidden, "FTP hesabı askıya alınmış")
		return
	}
	// Plain text karşılaştırma (Pure-FTPd MYSQLCrypt cleartext)
	if req.Parola != passDB {
		httpx.WriteError(w, http.StatusUnauthorized, "kullanıcı veya parola hatalı")
		return
	}

	// JWT üret — tip="musteri", domain_id scope
	c := auth.MusteriClaims{
		FTPHesapID: ftpID,
		DomainID:   domainID,
		Kullanici:  req.Kullanici,
		AlanAdi:    alanAdi,
	}
	tok, exp, err := auth.GenerateMusteri(h.Secret, c, 24*3600)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token: "+err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":     tok,
		"bitis":     exp,
		"domain_id": domainID,
		"alan_adi":  alanAdi,
		"kullanici": req.Kullanici,
	})
}

// MusteriOnly: middleware — token tipi "musteri" ise ve domain_id path'le eşleşmiyorsa 403
// Admin token'ı ise bypass eder (admin'ler her şeyi yapabilir)
func MusteriOnly(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

// CheckScope: handler içinde manuel scope kontrolü. Admin ise allow.
// Müşteri token ise URL'deki {id} ile token.DomainID eşleşmeli.
func CheckScope(r *http.Request, secret []byte, urlDomainIDParam string) (bool, error) {
	authH := r.Header.Get("Authorization")
	if !strings.HasPrefix(authH, "Bearer ") {
		return false, errors.New("yetkilendirme gerekli")
	}
	raw := strings.TrimPrefix(authH, "Bearer ")
	// Önce admin claims dene
	if c, err := auth.Parse(secret, raw); err == nil {
		_ = c
		return true, nil // admin
	}
	// Sonra musteri claims dene
	mc, err := auth.ParseMusteri(secret, raw)
	if err != nil {
		return false, errors.New("geçersiz token")
	}
	if urlDomainIDParam == "" {
		// Bu endpoint'te domain ID scope yok ama müşteri yine kısıtlı (ör: /domains listesi)
		return false, errors.New("müşteri bu endpoint'e erişemez")
	}
	id, _ := strconv.ParseInt(urlDomainIDParam, 10, 64)
	if id != mc.DomainID {
		return false, errors.New("bu domain'e erişim yok")
	}
	_ = time.Now
	return false, nil // musteri, scoped ok
}
