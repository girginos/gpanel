package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"girginospanel/internal/httpx"
)

// Anasayfa (dashboard) widget düzeni — kullanıcıya özel, sürükle-bırak sırası.
// users.dashboard_duzen kolonunda JSON metni olarak saklanır (bkz migration 0040).
// Aynı per-user tercih desenini (profile.go) izler.

// GET /dashboard-duzen — mevcut kullanıcının kayıtlı düzeni (yoksa boş string).
func (h *Handlers) DashboardDuzenGetir(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var duzen sql.NullString
	if err := h.DB.QueryRow(`SELECT dashboard_duzen FROM users WHERE id=?`, c.UserID).Scan(&duzen); err != nil && err != sql.ErrNoRows {
		httpx.WriteError(w, http.StatusInternalServerError, "okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"duzen": duzen.String})
}

// PUT /dashboard-duzen — düzeni kaydet. Gövde: {"duzen":"<json metni>"}.
func (h *Handlers) DashboardDuzenKaydet(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var b struct {
		Duzen string `json:"duzen"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(b.Duzen) > 16384 { // basit üst sınır — kötüye kullanımı engelle
		httpx.WriteError(w, http.StatusBadRequest, "düzen verisi çok büyük")
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET dashboard_duzen=?, updated_at=NOW() WHERE id=?`, b.Duzen, c.UserID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
