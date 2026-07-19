package auth

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"girginospanel/internal/httpx"
)

// claims: RequireAuth middleware zaten doğruladı; header'dan tekrar parse ederek
// (auth→middleware import cycle'ından kaçınmak için) UserID'yi alırız.
func (h *Handlers) claims(r *http.Request) *Claims {
	raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	c, err := Parse(h.Secret, raw)
	if err != nil {
		return nil
	}
	return c
}

// PUT /me — profil bilgileri (ad soyad + e-posta + tercihler)
func (h *Handlers) ProfilGuncelle(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var b struct {
		AdSoyad    string `json:"ad_soyad"`
		Eposta     string `json:"eposta"`
		TercihTema string `json:"tercih_tema"`
		TercihDil  string `json:"tercih_dil"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	b.AdSoyad = strings.TrimSpace(b.AdSoyad)
	b.Eposta = strings.TrimSpace(b.Eposta)
	if b.Eposta != "" && !strings.Contains(b.Eposta, "@") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz e-posta adresi")
		return
	}
	tema := "system"
	if b.TercihTema == "light" || b.TercihTema == "dark" || b.TercihTema == "system" {
		tema = b.TercihTema
	}
	dil := "tr"
	if b.TercihDil == "en" {
		dil = "en"
	}
	if _, err := h.DB.Exec(
		`UPDATE users SET full_name=?, email=?, tercih_tema=?, tercih_dil=?, updated_at=NOW() WHERE id=?`,
		b.AdSoyad, b.Eposta, tema, dil, c.UserID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "güncellenemedi")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /me/parola — sunucu root parolasını değiştir (mevcut parola doğrulanır → chpasswd)
func (h *Handlers) ParolaDegistir(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var b struct {
		Mevcut string `json:"mevcut"`
		Yeni   string `json:"yeni"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(b.Yeni) < 8 {
		httpx.WriteError(w, http.StatusBadRequest, "yeni parola en az 8 karakter olmalı")
		return
	}
	if !rootParolaDogrula(b.Mevcut) {
		writeAudit(h.DB, c.UserID, "root", httpx.ClientIP(r), "auth.parola", "root", false)
		httpx.WriteError(w, http.StatusUnauthorized, "mevcut parola hatalı")
		return
	}
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader("root:" + b.Yeni)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola değiştirilemedi: "+strings.TrimSpace(string(out)))
		return
	}
	writeAudit(h.DB, c.UserID, "root", httpx.ClientIP(r), "auth.parola", "root", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /me/2fa/setup — yeni secret üret (henüz aktifleştirilmez), otpauth URI döndür
func (h *Handlers) TwoFASetup(w http.ResponseWriter, r *http.Request) {
	if h.claims(r) == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	secret := TOTPGenerateSecret()
	uri := TOTPURI(secret, "root", "GirginOSPanel")
	resp := map[string]any{
		"secret":      secret,
		"otpauth":     uri, // geriye dönük uyum (elle giriş fallback)
		"otpauth_uri": uri,
	}
	// QR PNG data-URI (authenticator ile taransın). Üretilemezse elle giriş fallback kalır.
	if dataURI, err := TOTPQRDataURI(uri); err == nil {
		resp["qr_data_uri"] = dataURI
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// POST /me/2fa/enable — {secret, kod}: kod secret ile doğrulanırsa 2FA açılır
func (h *Handlers) TwoFAEnable(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var b struct {
		Secret string `json:"secret"`
		Kod    string `json:"kod"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	b.Secret = strings.TrimSpace(b.Secret)
	if !TOTPVerify(b.Secret, b.Kod) {
		httpx.WriteError(w, http.StatusBadRequest, "kod doğrulanamadı — uygulamadaki 6 haneli kodu girin")
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET totp_secret=?, totp_enabled=1 WHERE id=?`, b.Secret, c.UserID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi")
		return
	}
	writeAudit(h.DB, c.UserID, "root", httpx.ClientIP(r), "auth.2fa.enable", "root", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /me/2fa/disable — {kod}: geçerli kodla 2FA kapatılır
func (h *Handlers) TwoFADisable(w http.ResponseWriter, r *http.Request) {
	c := h.claims(r)
	if c == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	var b struct {
		Kod string `json:"kod"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	var secret string
	_ = h.DB.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, c.UserID).Scan(&secret)
	if !TOTPVerify(secret, b.Kod) {
		httpx.WriteError(w, http.StatusBadRequest, "kod doğrulanamadı")
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET totp_secret='', totp_enabled=0 WHERE id=?`, c.UserID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kapatılamadı")
		return
	}
	writeAudit(h.DB, c.UserID, "root", httpx.ClientIP(r), "auth.2fa.disable", "root", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
