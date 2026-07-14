package domains

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type sslIssueReq struct {
	Tip string `json:"tip"` // "self-signed" | "letsencrypt"
}

type sslDurumResp struct {
	Aktif    bool   `json:"aktif"`
	Kaynak   string `json:"kaynak"`
	BitisISO string `json:"bitis_iso,omitempty"`
	CertYol  string `json:"cert_yol,omitempty"`
	KeyYol   string `json:"key_yol,omitempty"`
}

func (h *Handlers) SSLDurum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var aktif int
	var kaynak, certYol, keyYol, bitis string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT ssl_aktif, ssl_kaynak, cert_path, key_path,
		   COALESCE(DATE_FORMAT(ssl_bitis,'%Y-%m-%dT%H:%i:%sZ'),'')
		 FROM domains WHERE id=?`, id).
		Scan(&aktif, &kaynak, &certYol, &keyYol, &bitis)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sslDurumResp{
		Aktif:    aktif == 1,
		Kaynak:   kaynak,
		BitisISO: bitis,
		CertYol:  certYol,
		KeyYol:   keyYol,
	})
}

func (h *Handlers) SSLIssue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req sslIssueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Tip == "" {
		req.Tip = "self-signed"
	}
	if req.Tip != "self-signed" && req.Tip != "letsencrypt" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz tip (self-signed|letsencrypt)")
		return
	}
	var alanAdi, sk, phpSurum, backend string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo, &backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe SSL kurulamaz")
		return
	}

	var certYol, keyYol string
	switch req.Tip {
	case "self-signed":
		certYol, keyYol, err = provisioner.EnableSelfSigned(alanAdi, sk, phpSurum, backend)
	case "letsencrypt":
		certYol, keyYol, err = provisioner.EnableLetsEncrypt(alanAdi, sk, phpSurum, backend)
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSL kurulum: "+err.Error())
		return
	}

	bitis := time.Now().Add(365 * 24 * time.Hour)
	if req.Tip == "letsencrypt" {
		bitis = time.Now().Add(90 * 24 * time.Hour)
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET ssl_aktif=1, ssl_kaynak=?, cert_path=?, key_path=?, ssl_bitis=?
		 WHERE id=?`, req.Tip, certYol, keyYol, bitis, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"id":     id,
		"tip":    req.Tip,
		"cert":   certYol,
		"key":    keyYol,
		"bitis":  bitis.Format("2006-01-02"),
	})
}

func (h *Handlers) SSLDisable(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, sk, phpSurum, backend string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo, &backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo abonelik dokunulamaz")
		return
	}
	if err := provisioner.DisableSSL(alanAdi, sk, phpSurum, backend); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSL kapat: "+err.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET ssl_aktif=0, ssl_kaynak='', cert_path='', key_path='', ssl_bitis=NULL
		 WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
