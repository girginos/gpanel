package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

type Handlers struct {
	DB          *sql.DB
	Secret      []byte
	LifetimeSec int
}

type loginReq struct {
	Kullanici string `json:"kullanici"`
	Parola    string `json:"parola"`
	Kod       string `json:"kod"`
}

type loginResp struct {
	Token     string `json:"token"`
	Bitis     int64  `json:"bitis"`
	Kullanici struct {
		ID      int64  `json:"id"`
		Adi     string `json:"adi"`
		Rol     string `json:"rol"`
		AdSoyad string `json:"ad_soyad"`
	} `json:"kullanici"`
}

// rootParolaDogrula: /etc/shadow'dan root hash okur, Python crypt subprocess ile karşılaştırır.
// yescrypt ($y$), sha512crypt ($6$), sha256crypt ($5$), MD5crypt ($1$) hepsini destekler.
func rootParolaDogrula(parola string) bool {
	data, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return false
	}
	var hash string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "root:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				hash = parts[1]
			}
			break
		}
	}
	if hash == "" || strings.HasPrefix(hash, "!") || strings.HasPrefix(hash, "*") || !strings.HasPrefix(hash, "$") {
		return false
	}
	cmd := exec.Command("python3", "-c",
		"import sys, crypt; p = sys.stdin.read(); sys.stdout.write(crypt.crypt(p, sys.argv[1]))",
		hash)
	cmd.Stdin = strings.NewReader(parola)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	computed := strings.TrimSpace(string(out))
	return computed == hash
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	req.Kullanici = strings.TrimSpace(req.Kullanici)
	if req.Kullanici == "" || req.Parola == "" {
		httpx.WriteError(w, http.StatusBadRequest, "kullanıcı adı ve parola zorunlu")
		return
	}
	if req.Kullanici != "root" {
		writeAudit(h.DB, 0, req.Kullanici, httpx.ClientIP(r), "auth.login", req.Kullanici, false)
		httpx.WriteError(w, http.StatusUnauthorized, "yalnızca sunucu root kullanıcısı admin paneline giriş yapabilir")
		return
	}
	if !rootParolaDogrula(req.Parola) {
		writeAudit(h.DB, 0, req.Kullanici, httpx.ClientIP(r), "auth.login", req.Kullanici, false)
		httpx.WriteError(w, http.StatusUnauthorized, "kullanıcı adı veya parola hatalı")
		return
	}

	// 2FA — parola doğru; 2FA açıksa TOTP kodu da gerekir
	{
		var en int
		var sec string
		_ = h.DB.QueryRow(`SELECT totp_enabled, totp_secret FROM users WHERE id=1`).Scan(&en, &sec)
		if en == 1 {
			if strings.TrimSpace(req.Kod) == "" {
				httpx.WriteJSON(w, http.StatusOK, map[string]any{"iki_fa_gerekli": true})
				return
			}
			if !TOTPVerify(sec, req.Kod) {
				writeAudit(h.DB, 1, "root", httpx.ClientIP(r), "auth.2fa", "root", false)
				httpx.WriteError(w, http.StatusUnauthorized, "2FA kodu hatalı")
				return
			}
		}
	}

	const adminUID = int64(1)
	tok, err := Issue(h.Secret, h.LifetimeSec, adminUID, "root", "admin")
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token üretilemedi")
		return
	}
	writeAudit(h.DB, adminUID, "root", httpx.ClientIP(r), "auth.login", "root", true)

	resp := loginResp{Token: tok, Bitis: time.Now().Add(time.Duration(h.LifetimeSec) * time.Second).Unix()}
	resp.Kullanici.ID = adminUID
	resp.Kullanici.Adi = "root"
	resp.Kullanici.Rol = "admin"
	var adSoyad string
	_ = h.DB.QueryRow(`SELECT full_name FROM users WHERE id=1`).Scan(&adSoyad)
	resp.Kullanici.AdSoyad = adSoyad
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func writeAudit(db *sql.DB, uid int64, username, ip, action, target string, ok bool) {
	var uidVal any
	if uid > 0 {
		uidVal = uid
	}
	okv := 0
	if ok {
		okv = 1
	}
	_, _ = db.Exec(
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, ok)
		 VALUES(?,?,?,?,?,?)`,
		uidVal, username, ip, action, target, okv)
}
