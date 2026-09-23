// Package musteri: müşteri (domain sahibi) login + scope kontrolü
// FTP credentials ile giriş, JWT'de domain_id scope'u
package musteri

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
)

type Handlers struct {
	DB     *sql.DB
	Secret []byte
}

type loginReq struct {
	Kullanici string `json:"kullanici"`
	Parola    string `json:"parola"`
}

// Dikişler (testler): isteğin panel kapısı, kapı giriş kanıtı.
var (
	kapiCoz         = kapi.Istek
	kapiGirisKaydet = kapi.GirisiKaydet
)

// Login: FTP user/password ile, FTP hesabının bağlı olduğu domain için JWT döner
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	// 🔴 KAPI: müşteri girişi yalnız müşteri kapısında (ve geçişteki eski kapıda) vardır.
	// Yönetici/bayi portunda kimlik hiç işlenmeden var-olmayan rotayla aynı yanıt döner.
	istekKapi := kapiCoz(r)
	if !kapi.GirisIzinli(istekKapi, kapi.Musteri) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64KB üstü login gövdesi = kötüye kullanım (DoS)
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
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if status != "active" {
		httpx.WriteError(w, http.StatusForbidden, "FTP hesabı askıya alınmış")
		return
	}
	// FTP parolasi at-rest $6$ crypt hash'i; sabit-zamanli crypt dogrulama
	// (eski duz-metin satir icin geriye-uyumlu). Bkz hesaplar.FTPParolaDogrula.
	if !hesaplar.FTPParolaDogrula(req.Parola, passDB) {
		httpx.WriteError(w, http.StatusUnauthorized, "kullanıcı veya parola hatalı")
		return
	}

	// JWT üret — tip="musteri", domain_id scope
	c := auth.MusteriClaims{
		FTPHesapID: ftpID,
		DomainID:   domainID,
		Kullanici:  req.Kullanici,
		AlanAdi:    alanAdi,
		Kapi:       istekKapi,
	}
	tok, exp, err := auth.GenerateMusteri(h.Secret, c, 24*3600)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token: "+err.Error())
		return
	}
	// Müşteri kapısından ilk dış giriş: arayüz eski kapıyı kapatmadan önce bu kapının
	// gerçekten kullanıldığını gösterir. Hata girişi engellemez.
	if err := kapiGirisKaydet(kapi.AyarYolu, kapi.Musteri, r, time.Now().Unix()); err != nil {
		log.Printf("kapi: müşteri kapısı giriş kanıtı kaydedilemedi: %v", err)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":     tok,
		"bitis":     exp,
		"domain_id": domainID,
		"alan_adi":  alanAdi,
		"kullanici": req.Kullanici,
	})
}
