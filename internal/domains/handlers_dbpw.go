package domains

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

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
	// 🔴 Kullanici, BASKA bir satirin ciphertext'ini parola olarak yazamaz:
	// aksi halde deger sonra cozulup okunarak sifre-cozme oracle'i olusuyordu.
	if gizli.SifreliMi(req.Parola) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz parola")
		return
	}
	if len(req.Parola) < 6 {
		httpx.WriteError(w, http.StatusBadRequest, "parola en az 6 karakter olmalı")
		return
	}

	var dbName, dbUser string
	var isDemo int
	var domainID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo, d.id
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo, &domainID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	// Rota seviyesinde {id} domain param'i yok → sahiplik BURADA dogrulanir.
	if !middleware.YonetimSahibi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
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
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.parola", dbName, "kullanici="+dbUser, domainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"dbid":         dbid,
		"db_adi":       dbName,
		"db_kullanici": dbUser,
		"db_parola":    req.Parola,
	})
}
