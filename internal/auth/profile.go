package auth

import (
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"os/exec"
	"strings"

	"girginospanel/internal/httpx"
)

// oturumlariDusur: hesabin ACIK oturumlarini gecersiz kilar — middleware.oturumGecerli
// bu damgadan ONCE uretilmis tokenlari reddeder. Parola degisiminin tek amaci
// calinan/paylasilan oturumu kesmektir; damga atilmazsa eski token 8 saat daha yasar.
// (middleware paketi auth'u import ettigi icin tersine bagimlilik kurulamaz —
// tek satirlik UPDATE bilerek burada tekrarlanir.)
func (h *Handlers) oturumlariDusur(uid int64) {
	if h.DB == nil || uid <= 0 {
		return
	}
	_, _ = h.DB.Exec(`UPDATE users SET token_gecersiz_ts=UNIX_TIMESTAMP()+1 WHERE id=?`, uid)
}

// kapsamRol: denetim kaydinin kapsami — bayi ise kendi id'si, kok ise 0.
func kapsamRol(c *Claims) int64 {
	if c != nil && c.Role == "reseller" {
		return c.UserID
	}
	return 0
}

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
	// 🔴 BAYI: sistem root parolasina DOKUNMAZ — kendi bcrypt parolasini degistirir.
	if c.Role != "admin" {
		var mevcutHash string
		if err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id=? AND role='reseller'`, c.UserID).
			Scan(&mevcutHash); err != nil {
			httpx.WriteError(w, http.StatusForbidden, "hesap bulunamadı")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(mevcutHash), []byte(b.Mevcut)) != nil {
			writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.parola", c.Username, false, kapsamRol(c))
			httpx.WriteError(w, http.StatusUnauthorized, "mevcut parola hatalı")
			return
		}
		yeniHash, err := bcrypt.GenerateFromPassword([]byte(b.Yeni), 12)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "parola işlenemedi")
			return
		}
		if _, err := h.DB.Exec(`UPDATE users SET password_hash=?, updated_at=NOW() WHERE id=?`, string(yeniHash), c.UserID); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "parola kaydedilemedi")
			return
		}
		// 🔴 Parola degisiminin TEK amaci calinan/paylasilan oturumu kesmektir:
		// eski token'lar aninda dussun. Cagiranin kendi tokeni de olur → yerine
		// TAZE token doneriz, kullanici yeniden giris yapmak zorunda kalmasin.
		h.oturumlariDusur(c.UserID)
		writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.parola", c.Username, true, kapsamRol(c))
		yeniTok, _ := IssueResellerAt(h.Secret, h.LifetimeSec, c.UserID, c.Username, c.UserID, h.gecersizDamga(c.UserID))
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "token": yeniTok})
		return
	}

	if !rootParolaDogrula(b.Mevcut) {
		writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.parola", c.Username, false, kapsamRol(c))
		httpx.WriteError(w, http.StatusUnauthorized, "mevcut parola hatalı")
		return
	}
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader("root:" + b.Yeni)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola değiştirilemedi: "+strings.TrimSpace(string(out)))
		return
	}
	h.oturumlariDusur(c.UserID)
	writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.parola", c.Username, true, kapsamRol(c))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /me/2fa/setup — yeni secret üret (henüz aktifleştirilmez), otpauth URI döndür
func (h *Handlers) TwoFASetup(w http.ResponseWriter, r *http.Request) {
	if h.claims(r) == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "oturum yok")
		return
	}
	secret, err := TOTPGenerateSecret()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "secret üretilemedi")
		return
	}
	kul := "root"
	if c := h.claims(r); c != nil && c.Username != "" {
		kul = c.Username // authenticator uygulamasinda dogru hesap adi gorunsun
	}
	uri := TOTPURI(secret, kul, "GirginOSPanel")
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
	adim, ok := TOTPVerifyAdim(b.Secret, b.Kod, -1)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "kod doğrulanamadı — uygulamadaki 6 haneli kodu girin")
		return
	}
	// Etkinleştirme kodunun login'de hemen replay edilmesini önle: kullanılan adımı kaydet
	if _, err := h.DB.Exec(`UPDATE users SET totp_secret=?, totp_enabled=1, totp_last_step=? WHERE id=?`, b.Secret, adim, c.UserID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi")
		return
	}
	writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.2fa.enable", c.Username, true, kapsamRol(c))
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
	writeAudit(h.DB, c.UserID, c.Username, httpx.ClientIP(r), "auth.2fa.disable", c.Username, true, kapsamRol(c))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
