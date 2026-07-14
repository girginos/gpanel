package domains

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type setDBPwReq struct {
	Parola string `json:"parola"`
}

// SetDatabasePassword: PUT /api/v1/databases/:dbid/password
// Body bos ise rastgele uretir. Demo abonelige reddeder.
func (h *Handlers) SetDatabasePassword(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var req setDBPwReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Parola == "" {
		req.Parola = hesaplar.RandomParola(24)
	}
	if len(req.Parola) < 6 {
		httpx.WriteError(w, http.StatusBadRequest, "parola en az 6 karakter olmalı")
		return
	}

	var dbName, dbUser string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB parolası değiştirilemez")
		return
	}

	if err := hesaplar.MySQLChangePassword(h.DB, dbUser, req.Parola); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola değişimi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"dbid":         dbid,
		"db_adi":       dbName,
		"db_kullanici": dbUser,
		"db_parola":    req.Parola,
	})
}
