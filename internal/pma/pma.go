// Package pma: phpMyAdmin tek-kullanimlik SSO token uretimi + redemption
package pma

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// internalAuthToken: signon.php'nin panel'i çağırırken kullandığı statik token
// Dosyadan okur (/etc/girginospanel/pma-internal.token). Yoksa erişim engellenir.
func internalAuthToken() string {
	b, err := os.ReadFile("/etc/girginospanel/pma-internal.token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// TokenIste: admin auth ile kısa-ömürlü token üret. UI -> bu endpoint -> token -> window.open('/pma-signon.php?t=...')
// URL: POST /api/v1/databases/{dbId}/pma-token
func (h *Handlers) TokenIste(w http.ResponseWriter, r *http.Request) {
	dbID, _ := strconv.ParseInt(chi.URLParam(r, "dbId"), 10, 64)

	// DB bilgileri + domain (demo kontrolü için join)
	var dbKul, dbPar, dbAdi string
	var domainID int64
	var demo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_user, db.db_pass_plain, db.db_name, db.domain_id, d.is_demo
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbID).Scan(&dbKul, &dbPar, &dbAdi, &domainID, &demo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "veritabanı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// IDOR korumasi (OWASP A01): cagiran bu veritabaninin sahibi mi?
	// Admin her db'ye erisir; musteri yalniz kendi domain'inin db'sine.
	// Degilse varligi sizdirmadan 404 (var-olmayan db ile AYNI yanit).
	if !middleware.DomainSahibiMi(r, domainID) {
		httpx.WriteError(w, http.StatusNotFound, "veritabanı bulunamadı")
		return
	}
	if demo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin phpMyAdmin'i etkin değildir")
		return
	}

	token := randomHex(24)
	expires := time.Now().Add(2 * time.Minute) // 2 dakika kısa pencere

	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO pma_tokens(token, domain_id, db_kullanici, db_parola, db_adi, son_kullanma)
		 VALUES(?,?,?,?,?,?)`,
		token, domainID, dbKul, dbPar, dbAdi, expires)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Eski token'ları temizle (her istekte birikmiş eski/kullanılmışları sil)
	_, _ = h.DB.ExecContext(r.Context(),
		`DELETE FROM pma_tokens WHERE son_kullanma < NOW() OR kullanildi=1`)

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"token":         token,
		"signon_url":    "/pma-signon.php?t=" + token,
		"son_kullanma":  expires.Format(time.RFC3339),
		"gecerlilik_sn": 120,
	})
}

// Bozdur: signon.php internal-auth header ile çağırır, credentials JSON döner, token tek-kullanim.
// URL: POST /api/v1/internal/pma-redeem  (X-Internal-Auth header)
func (h *Handlers) Bozdur(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("X-Internal-Auth")
	expected := internalAuthToken()
	if expected == "" || auth == "" || auth != expected {
		http.Error(w, "yetki yok", http.StatusUnauthorized)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		http.Error(w, "token eksik", http.StatusBadRequest)
		return
	}

	var dbKul, dbPar, dbAdi string
	var sonKul time.Time
	var kul int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db_kullanici, db_parola, db_adi, son_kullanma, kullanildi
		 FROM pma_tokens WHERE token=?`, req.Token).
		Scan(&dbKul, &dbPar, &dbAdi, &sonKul, &kul)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "token bulunamadı", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if kul == 1 {
		http.Error(w, "token zaten kullanılmış", http.StatusGone)
		return
	}
	if time.Now().After(sonKul) {
		http.Error(w, "token süresi doldu", http.StatusGone)
		return
	}
	// Tek-kullanim: işaretle
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE pma_tokens SET kullanildi=1 WHERE token=?`, req.Token)

	json.NewEncoder(w).Encode(map[string]any{
		"kullanici": dbKul,
		"parola":    dbPar,
		"db":        dbAdi,
		"host":      "127.0.0.1",
	})
}
