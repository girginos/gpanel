// Package sshaccess: host (hosting hesabı) bazlı SSH erişimi.
// Her domain = bir Linux kullanıcısı (c_<slug>). SSH erişimi, kullanıcının
// login shell'ini /bin/bash (açık) ↔ /usr/sbin/nologin (kapalı) arasında
// değiştirerek yönetilir. Sunucudaki sshd'de AllowUsers/AllowGroups OLMADIĞI
// için (doğrulandı) shell-toggle güvenli + yeterlidir; sshd_config'e HİÇ
// dokunulmaz → root veya başka hesap kilitlenmez.
package sshaccess

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const (
	shellAcik   = "/bin/bash"
	shellKapali = "/usr/sbin/nologin"
	sshPort     = 22
)

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

type durum struct {
	AlanAdi    string `json:"alan_adi"`
	Kullanici  string `json:"kullanici"`
	Aktif      bool   `json:"aktif"`
	Shell      string `json:"shell"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	AnahtarVar bool   `json:"anahtar_var"`
	IsDemo     bool   `json:"is_demo"`
}

// gecerliSK: yalnızca panel'in oluşturduğu c_<slug> kullanıcılarında işlem
// yapılmasını garanti eder (komut enjeksiyonu / yanlış hesap koruması).
func gecerliSK(sk string) bool {
	if !strings.HasPrefix(sk, "c_") || len(sk) < 3 {
		return false
	}
	return !strings.ContainsAny(sk, "/ .;|&$`\n\r\t\"'")
}

func currentShell(sk string) string {
	out, err := exec.Command("getent", "passwd", sk).Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) >= 7 {
		return parts[6]
	}
	return ""
}

func anahtarVar(sk string) bool {
	st, err := os.Stat(filepath.Join("/home", sk, ".ssh", "authorized_keys"))
	return err == nil && st.Size() > 0
}

func (h *Handlers) yukle(r *http.Request) (id int64, sk, alanAdi string, demo bool, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &alanAdi, &isDemo)
	if err != nil {
		return id, "", "", false, false
	}
	return id, sk, alanAdi, isDemo == 1, true
}

// GET /domains/{id}/ssh — mevcut SSH erişim durumu
func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	_, sk, alanAdi, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	shell := currentShell(sk)
	httpx.WriteJSON(w, http.StatusOK, durum{
		AlanAdi:    alanAdi,
		Kullanici:  sk,
		Aktif:      shell == shellAcik,
		Shell:      shell,
		SSHHost:    h.IPv4,
		SSHPort:    sshPort,
		AnahtarVar: anahtarVar(sk),
		IsDemo:     demo,
	})
}

// PUT /domains/{id}/ssh  body {"aktif": true|false} — shell'i değiştir
func (h *Handlers) Ayarla(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin SSH erişimi değiştirilemez")
		return
	}
	if !gecerliSK(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		Aktif bool `json:"aktif"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	shell := shellKapali
	if req.Aktif {
		shell = shellAcik
	}
	if out, err := exec.Command("usermod", "-s", shell, sk).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "shell değiştirilemedi: "+strings.TrimSpace(string(out)))
		return
	}
	if req.Aktif {
		// ~/.ssh iskeleti (anahtar yüklemeye hazır)
		dir := filepath.Join("/home", sk, ".ssh")
		_ = os.MkdirAll(dir, 0700)
		_ = exec.Command("chown", "-R", sk+":"+sk, dir).Run()
		_ = exec.Command("restorecon", "-R", dir).Run()
		// SSH parolasını FTP parolasıyla eşitle (parola = FTP)
		_ = hesaplar.SyncSSHPassword(h.DB, sk)
		// Chroot jail kur + gosp-ssh grubuna ekle (kendi home'una hapset)
		_ = exec.Command("/usr/local/bin/girginospanel-jail", "setup", sk).Run()
		_ = exec.Command("groupadd", "-f", "gosp-ssh").Run()
		_ = exec.Command("gpasswd", "-a", sk, "gosp-ssh").Run()
	} else {
		// SSH kapalı: gruptan çıkar + jail söktür + parolayı kilitle
		_ = exec.Command("gpasswd", "-d", sk, "gosp-ssh").Run()
		_ = exec.Command("/usr/local/bin/girginospanel-jail", "teardown", sk).Run()
		_ = hesaplar.LockSSHPassword(sk)
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET ssh_erisim=? WHERE id=?`, b2i(req.Aktif), id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "durum kaydedilemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "aktif": req.Aktif, "shell": shell, "kullanici": sk,
	})
}

// PUT /domains/{id}/ssh/anahtar  body {"anahtar": "ssh-ed25519 ..."} — authorized_keys yaz
func (h *Handlers) AnahtarKaydet(w http.ResponseWriter, r *http.Request) {
	_, sk, _, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin SSH anahtarı değiştirilemez")
		return
	}
	if !gecerliSK(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		Anahtar string `json:"anahtar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	anahtar := strings.TrimSpace(req.Anahtar)
	// Her satır ssh-/ecdsa-/sk- ile başlamalı (boş = anahtarları temizle demek)
	if anahtar != "" {
		for _, line := range strings.Split(anahtar, "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			if !strings.HasPrefix(l, "ssh-") && !strings.HasPrefix(l, "ecdsa-") && !strings.HasPrefix(l, "sk-") {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz SSH anahtarı: her satır ssh-/ecdsa-/sk- ile başlamalı")
				return
			}
		}
	}
	dir := filepath.Join("/home", sk, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ".ssh dizini: "+err.Error())
		return
	}
	ak := filepath.Join(dir, "authorized_keys")
	body := ""
	if anahtar != "" {
		body = anahtar + "\n"
	}
	if err := os.WriteFile(ak, []byte(body), 0600); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "authorized_keys yazılamadı: "+err.Error())
		return
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, dir).Run()
	_ = exec.Command("restorecon", "-R", dir).Run()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "anahtar_var": anahtar != ""})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EnsureInfra: panel açılışında SSH jail altyapısını hazırlar (idempotent + best-effort).
//   - girginospanel-jail script'ini /usr/local/bin'e yerleştirir
//   - gosp-ssh grubunu oluşturur
//   - sshd Match chroot config'ini yerleştirir — YALNIZCA `sshd -t` geçerse reload eder,
//     geçersizse eski haline döndürür (sshd'yi asla bozmaz).
func EnsureInfra() {
	const srcDir = "/opt/girginospanel/src/scripts"
	// 1) jail script
	if data, err := os.ReadFile(srcDir + "/girginospanel-jail"); err == nil {
		if e := os.WriteFile("/usr/local/bin/girginospanel-jail", data, 0o755); e == nil {
			_ = os.Chmod("/usr/local/bin/girginospanel-jail", 0o755)
		}
	}
	// 2) gosp-ssh grubu
	_ = exec.Command("groupadd", "-f", "gosp-ssh").Run()
	// 3) sshd Match chroot config — güvenli uygula
	dst := "/etc/ssh/sshd_config.d/50-gosp-jail.conf"
	src, err := os.ReadFile(srcDir + "/50-gosp-jail.conf")
	if err != nil {
		return
	}
	cur, _ := os.ReadFile(dst)
	if string(cur) == string(src) {
		return // zaten güncel
	}
	if e := os.WriteFile(dst, src, 0o644); e != nil {
		log.Printf("jail sshd config yazılamadı: %v", e)
		return
	}
	if out, e := exec.Command("sshd", "-t").CombinedOutput(); e != nil {
		// geçersiz → geri al, sshd'yi bozma
		if len(cur) > 0 {
			_ = os.WriteFile(dst, cur, 0o644)
		} else {
			_ = os.Remove(dst)
		}
		log.Printf("jail sshd config geçersiz, uygulanmadı: %s", strings.TrimSpace(string(out)))
		return
	}
	_ = exec.Command("systemctl", "reload", "sshd").Run()
	log.Printf("SSH jail altyapısı hazır (script + gosp-ssh + sshd chroot config)")
}
