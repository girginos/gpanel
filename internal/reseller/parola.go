package reseller

// parola.go — otomasyonun (WHMCS bayi ürünü "Şifre değiştir") bayi panel parolasını
// sıfırlaması:
//
//	PUT /api/v1/resellers/{id}/password   {"parola": "..."}  ("password" takma adı)
//
// Yalnız kök (AdminOnly). API anahtarında AYRI izin ister: "bayi.parola:yaz" (varsayılan
// kapalı; hosting'deki "ftp.parola" gibi). PUT /resellers/{id} gövdesindeki "parola" alanı
// anahtara yine KAPALIDIR (2026-10-03 kararı: genel bayi yazma izni parola değiştirmez);
// kullanıcı kararı 2026-10-13 ile parola sıfırlama yalnız bu uçtan ve yalnız bu izinle açıldı.
//
// Sıfırlama = ele geçirme varsayımı (panel arayüzündeki sıfırlamayla aynı): bayinin açık
// oturumları düşer, API anahtarları iptal olur, Telegram destek bağlantıları kopar.

import (
	"net/http"
	"strconv"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	destekiptal "girginospanel/internal/destek/iptal"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type parolaReq struct {
	Parola   string `json:"parola"`
	Password string `json:"password"` // takma ad (PUT /domains/{id}/ftp/password ile aynı)
}

// ParolaSifirla: PUT /resellers/{id}/password (yönetici / kök anahtarı + bayi.parola:yaz).
func (h *Handlers) ParolaSifirla(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var bayiAd string
	if id <= 0 || h.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id=? AND role='reseller'`, id).Scan(&bayiAd) != nil {
		httpx.WriteError(w, http.StatusNotFound, "reseller bulunamadı")
		return
	}
	var req parolaReq
	if err := govdeCoz(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, govdeHata(err))
		return
	}
	parola := req.Parola
	if parola == "" {
		parola = req.Password
	} else if req.Password != "" && req.Password != parola {
		httpx.WriteError(w, http.StatusBadRequest, "parola ve password farklı — yalnız birini gönderin")
		return
	}
	if len(parola) < 8 || len(parola) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "parola 8-128 karakter olmalı")
		return
	}
	if m := BcryptParolaHatasi(parola); m != "" {
		httpx.WriteError(w, http.StatusBadRequest, m)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(parola), 12)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola işlenemedi")
		return
	}
	uid, kul := middleware.Aktor(r)
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash=?, updated_at=NOW() WHERE id=? AND role='reseller'`, string(hash), id); err != nil {
		httpx.Denetim(h.DB, r, uid, kul, "bayi.parola", bayiAd, "", id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "parola kaydedilemedi")
		return
	}
	// Panel arayüzündeki sıfırlamayla AYNI sonuçlar (bkz. Guncelle).
	middleware.OturumlariDusur(h.DB, id)
	bayiAnahtarlariniIptal(r, h.DB, id, uid, kul, bayiAd, "parola_sifirlama")
	destekiptal.Bayi(r.Context(), h.DB, id, false, uid, kul)
	httpx.Denetim(h.DB, r, uid, kul, "bayi.parola", bayiAd, "parola sıfırlandı", id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "kullanici": bayiAd})
}

// BcryptParolaHatasi: bcrypt yalnız ilk 72 BAYTI kullanır ve Go'nun bcrypt'i daha uzun girdiyi
// reddeder (ErrPasswordTooLong): 73+ baytlık bayi parolası 500 "parola işlenemedi" veriyordu (VM
// doğrulaması 2026-10-13). Türkçe karakterler UTF-8'de 2 bayttır. "" = uygun.
func BcryptParolaHatasi(pw string) string {
	if len(pw) > 72 {
		return "parola en çok 72 bayt olabilir (Türkçe karakterler 2 bayt sayılır)"
	}
	return ""
}
