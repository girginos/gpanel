// Package sifrekoruma: dizin bazlı .htpasswd (nginx auth_basic) yönetimi.
// Güvenlik: sıkı input doğrulama + argv-explicit exec (shell yok) + hatalı
// vhost render'ında conf yedeğinden geri-dönüş (rollback) — müşteri sitesi asla bozulmaz.
package sifrekoruma

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/subdomain"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

const htpasswdDir = "/etc/nginx/htpasswd"

var (
	reYol  = regexp.MustCompile(`^/[A-Za-z0-9._/-]{0,200}$`)
	reUser = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

type Kayit struct {
	ID        int64  `json:"id"`
	Yol       string `json:"yol"`
	Kullanici string `json:"kullanici"`
	CreatedAt string `json:"created_at"`
}

func (h *Handlers) domain(r *http.Request) (id int64, sk, surum string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0) FROM domains WHERE id=?`, id).
		Scan(&sk, &surum, &isDemo); err != nil {
		return id, "", "", false, false
	}
	return id, sk, surum, isDemo == 1, true
}

// hedefSub: {sid} varsa (subID, subSurum, true); yoksa (0, "", false).
func (h *Handlers) hedefSub(r *http.Request, id int64) (int64, string, bool) {
	sidStr := chi.URLParam(r, "sid")
	if sidStr == "" {
		return 0, "", false
	}
	sid, _ := strconv.ParseInt(sidStr, 10, 64)
	var surum string
	if err := h.DB.QueryRow(`SELECT COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&surum); err != nil {
		return 0, "", false
	}
	return sid, surum, true
}

// korumaUygula: subdomain ise subdomain vhost'unu, degilse domain vhost'unu yeniden render eder.
func (h *Handlers) korumaUygula(id int64, sk, surum string, subID int64) error {
	if subID > 0 {
		return subdomain.ReRenderKoruma(h.DB, subID)
	}
	return h.reRender(id, sk, surum)
}

// GET /domains/{id}/koruma
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	id, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	subID, _, _ := h.hedefSub(r, id)
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, yol, kullanici, created_at FROM korumali_dizinler WHERE domain_id=? AND subdomain_id=? ORDER BY yol, kullanici`, id, subID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	out := []Kayit{}
	for rows.Next() {
		var k Kayit
		if err := rows.Scan(&k.ID, &k.Yol, &k.Kullanici, &k.CreatedAt); err == nil {
			out = append(out, k)
		}
	}
	if err := rows.Err(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listeleme hatası")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// POST /domains/{id}/koruma  {yol, kullanici, parola}
func (h *Handlers) Ekle(w http.ResponseWriter, r *http.Request) {
	id, sk, surum, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	subID, _, _ := h.hedefSub(r, id)
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	var req struct {
		Yol       string `json:"yol"`
		Kullanici string `json:"kullanici"`
		Parola    string `json:"parola"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	yol := normalizeYol(req.Yol)
	if !reYol.MatchString(yol) || strings.Contains(yol, "..") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz yol (örn: /gizli)")
		return
	}
	if !reUser.MatchString(req.Kullanici) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı adı")
		return
	}
	if len(req.Parola) < 4 || len(req.Parola) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "parola 4-128 karakter olmalı")
		return
	}
	if err := os.MkdirAll(htpasswdDir, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "htpasswd dizini oluşturulamadı")
		return
	}
	ad := "d" + strconv.FormatInt(id, 10)
	if subID > 0 {
		ad += "_s" + strconv.FormatInt(subID, 10)
	}
	dosya := htpasswdDir + "/" + ad + "_" + sanitize(yol)
	flag := "-bB"
	if _, e := os.Stat(dosya); e != nil {
		flag = "-cbB" // yeni dosya oluştur
	}
	// argv EXPLICIT — parola/kullanıcı shell'e uğramaz
	if out, err := exec.Command("htpasswd", flag, dosya, req.Kullanici, req.Parola).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "htpasswd: "+strings.TrimSpace(string(out)))
		return
	}
	_ = exec.Command("restorecon", dosya).Run() // SELinux: httpd_config_t
	_ = os.Chmod(dosya, 0o644)

	if _, err := h.DB.Exec(
		`INSERT INTO korumali_dizinler (domain_id, subdomain_id, yol, kullanici, htpasswd_dosya) VALUES (?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE htpasswd_dosya=VALUES(htpasswd_dosya)`,
		id, subID, yol, req.Kullanici, dosya); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt eklenemedi")
		return
	}

	if err := h.korumaUygula(id, sk, surum, subID); err != nil {
		// vhost doğrulanamadı → eklediğimizi geri al, htpasswd'den de kaldır, tekrar render
		_, _ = h.DB.Exec(`DELETE FROM korumali_dizinler WHERE domain_id=? AND subdomain_id=? AND yol=? AND kullanici=?`, id, subID, yol, req.Kullanici)
		_ = exec.Command("htpasswd", "-D", dosya, req.Kullanici).Run()
		if kalan := h.kullaniciSayisi(id, subID, yol); kalan == 0 {
			_ = os.Remove(dosya)
		}
		_ = h.korumaUygula(id, sk, surum, subID)
		httpx.WriteError(w, http.StatusInternalServerError, "nginx yapılandırması doğrulanamadı: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /domains/{id}/koruma/{kid}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	id, sk, surum, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	kid, _ := strconv.ParseInt(chi.URLParam(r, "kid"), 10, 64)
	var yol, kullanici, dosya string
	var subID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT yol, kullanici, htpasswd_dosya, subdomain_id FROM korumali_dizinler WHERE id=? AND domain_id=?`, kid, id).
		Scan(&yol, &kullanici, &dosya, &subID); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "kayıt bulunamadı")
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM korumali_dizinler WHERE id=? AND domain_id=?`, kid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi")
		return
	}
	_ = exec.Command("htpasswd", "-D", dosya, kullanici).Run()
	if h.kullaniciSayisi(id, subID, yol) == 0 {
		_ = os.Remove(dosya) // bu yol için başka kullanıcı kalmadı → location bloğu da düşecek
	}
	if err := h.korumaUygula(id, sk, surum, subID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "nginx yeniden yüklenemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) kullaniciSayisi(id, subID int64, yol string) int {
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM korumali_dizinler WHERE domain_id=? AND subdomain_id=? AND yol=?`, id, subID, yol).Scan(&n)
	return n
}

// reRender: vhost'u yeniden üretir; nginx -t patlarsa conf'u yedekten geri yükler (site bozulmaz).
func (h *Handlers) reRender(domainID int64, sk, surum string) error {
	socket, err := provisioner.PHPSocketFor(sk, surum)
	if err != nil {
		return fmt.Errorf("php socket: %w", err)
	}
	cfg := "/etc/nginx/conf.d/dom_" + sk + ".conf"
	backup, _ := os.ReadFile(cfg) // yoksa nil
	if err := provisioner.ApplyVhostForDomain(h.DB, domainID, socket, surum); err != nil {
		if backup != nil {
			_ = os.WriteFile(cfg, backup, 0o644) // diski bilinen-iyi hale döndür
			_ = exec.Command("nginx", "-t").Run()
		}
		return err
	}
	return nil
}

func normalizeYol(y string) string {
	y = strings.TrimSpace(y)
	if y == "" {
		return "/"
	}
	if !strings.HasPrefix(y, "/") {
		y = "/" + y
	}
	if len(y) > 1 {
		y = strings.TrimRight(y, "/")
	}
	if y == "" {
		y = "/"
	}
	return y
}

var reNonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

func sanitize(s string) string {
	s = reNonAlnum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "root"
	}
	return s
}
