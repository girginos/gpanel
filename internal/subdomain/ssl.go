package subdomain

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// Subdomain SSL: self-signed veya Let's Encrypt. Parent domain ile AYNI mantık
// (openssl / acme.sh --webroot /var/www/_acme) ama subdomain vhost'una (sub_*.conf) uygulanır.

func sslDir(sk string) string { return "/home/" + sk + "/ssl" }
func sslSid(r *http.Request) int64 {
	v, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	return v
}

func certYolu(sk, tamAd string) (string, string) {
	d := sslDir(sk)
	return filepath.Join(d, tamAd+".crt"), filepath.Join(d, tamAd+".key")
}

// subInfo: sid + parent'tan alt_ad/tam_ad/php_surum çöz.
func (h *Handlers) subInfo(r *http.Request, id int64) (altAd, tamAd, phpSurum string, ok bool) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`,
		sid, id).Scan(&altAd, &tamAd, &phpSurum); err != nil {
		return "", "", "", false
	}
	return altAd, tamAd, phpSurum, true
}

// GET /domains/{id}/subdomain/{sid}/ssl — durum
func (h *Handlers) SSLDurum(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, tamAd, _, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	crt, key := certYolu(sk, tamAd)
	aktif := dosyaVar(crt) && dosyaVar(key)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"aktif": aktif})
}

// POST /domains/{id}/subdomain/{sid}/ssl  {tip:"self-signed"|"letsencrypt"}
func (h *Handlers) SSLKur(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, demo, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	altAd, tamAd, phpSurum, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	var req struct {
		Tip string `json:"tip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tip := strings.ToLower(strings.TrimSpace(req.Tip))
	if tip == "" {
		tip = "self-signed"
	}

	if _, err := provisioner.PHPSocketFor(sk, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü kurulu değil: "+phpSurum)
		return
	}
	docroot := docrootOf(sk, tamAd)
	crt, key := certYolu(sk, tamAd)
	_ = os.MkdirAll(sslDir(sk), 0o750)

	switch tip {
	case "letsencrypt", "le":
		// 🔴 Challenge koku ROOT-SAHIPLI /var/www/acme -- alt alanin docroot'u
		// DEGIL. Kiracinin yazabildigi bir koke "allow all" vermek, erisim
		// kisitlamasini atlatma kapisi acar (olculdu). Vhost tarafi da AYNI
		// koku gosteriyor; ikisi birlikte degismeli.
		_ = provisioner.AcmeWebroot()
		_, _ = exec.Command("restorecon", "-R", filepath.Join(docroot, ".well-known")).CombinedOutput()
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--issue", "--server", "letsencrypt",
			"--config-home", provisioner.AcmeConfigHome(), "--webroot", provisioner.AcmeWebroot(),
			"-d", tamAd, "--keylength", "ec-256").CombinedOutput(); err != nil {
			// 🔴 acme.sh çıkış kodu 2 = RENEW_SKIP: geçerli cert ZATEN var, yenileme gerekmiyor.
			// Bu HATA DEĞİL — mevcut cert'i install-cert ile yerleştirmeye devam et. Aksi halde
			// (eski hata) ikinci kurulumda "Let's Encrypt alınamadı" yanılgısı verip panel SSL'i
			// hiç kurmuyordu. Yalnız DİĞER çıkış kodları (DNS/challenge hatası) gerçek başarısızlık.
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
				httpx.WriteError(w, http.StatusBadRequest,
					"Let's Encrypt alınamadı (subdomain DNS'i bu sunucuya A kaydıyla yönlendirilmeli): "+strings.TrimSpace(string(out)))
				return
			}
		}
		if out, err := exec.Command("/root/.acme.sh/acme.sh", "--install-cert", "--config-home", provisioner.AcmeConfigHome(), "-d", tamAd, "--ecc",
			"--key-file", key, "--fullchain-file", crt,
			"--reloadcmd", "systemctl reload nginx").CombinedOutput(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "cert yerleştirilemedi: "+strings.TrimSpace(string(out)))
			return
		}
	default: // self-signed
		if out, err := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
			"-days", "365", "-keyout", key, "-out", crt,
			"-subj", "/CN="+tamAd, "-addext", "subjectAltName=DNS:"+tamAd).CombinedOutput(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "openssl: "+strings.TrimSpace(string(out)))
			return
		}
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, sslDir(sk)).Run()
	_ = exec.Command("restorecon", "-R", sslDir(sk)).Run()

	// vhost'u yeniden yaz — cert artık var, rebuildVhost HTTPS bloğunu üretir;
	// alt alanın özel PHP havuzu + nginx/backend ayarları KORUNUR.
	if err := h.rebuildVhost(r.Context(), sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "tam_ad": tamAd, "tip": tip})
}

// DELETE /domains/{id}/subdomain/{sid}/ssl — SSL'i kaldır, HTTP'ye dön
func (h *Handlers) SSLKaldir(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, demo, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	altAd, tamAd, phpSurum, ok := h.subInfo(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	crt, key := certYolu(sk, tamAd)
	_ = os.Remove(crt)
	_ = os.Remove(key)
	// cert gitti → rebuildVhost HTTP bloğunu üretir; özel ayarlar KORUNUR.
	if err := h.rebuildVhost(r.Context(), sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func dosyaVar(p string) bool { _, err := os.Stat(p); return err == nil }
