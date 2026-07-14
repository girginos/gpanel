package users

import (
	"database/sql"
	"net/http"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

type Handlers struct {
	DB *sql.DB
}

type meResp struct {
	ID      int64  `json:"id"`
	Adi     string `json:"adi"`
	Rol     string `json:"rol"`
	Eposta  string `json:"eposta"`
	AdSoyad string `json:"ad_soyad"`
	Durum   string `json:"durum"`
	TwoFA   bool   `json:"iki_fa"`
	Tema    string `json:"tercih_tema"`
	Dil     string `json:"tercih_dil"`
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	// Müşteri (FTP) oturumu — DB lookup'a gerek yok, claim'den synthetic döner.
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		httpx.WriteJSON(w, http.StatusOK, meResp{
			ID:      0,
			Adi:     mc.Kullanici,
			Rol:     "musteri",
			AdSoyad: mc.AlanAdi,
			Durum:   "active",
		})
		return
	}
	c := middleware.ClaimsFrom(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var resp meResp
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, username, role, email, full_name, status, totp_enabled, tercih_tema, tercih_dil FROM users WHERE id=?`,
		c.UserID).Scan(&resp.ID, &resp.Adi, &resp.Rol, &resp.Eposta, &resp.AdSoyad, &resp.Durum, &resp.TwoFA, &resp.Tema, &resp.Dil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kullanıcı okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
