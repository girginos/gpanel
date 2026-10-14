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
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// Subdomain SSL: self-signed veya Let's Encrypt. Parent domain ile AYNI mantık
// (openssl / acme.sh --webroot /var/www/_acme) ama subdomain vhost'una (sub_*.conf) uygulanır.

func sslDir(sk string) string { return evKoku + "/" + sk + "/ssl" }

// acmeKoku — ACME HTTP-01 kökünü hazırlayıp döner (/var/www/acme + config home
// izinleri, restorecon). Testler sahteyle değiştirir (ana makineye dokunulmaz).
var acmeKoku = provisioner.AcmeWebroot

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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
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
	// Denetim: yan etki burada başlar; sonuç tek satırda (hosting ssl.kur/ssl.kaldir ile aynı).
	uid, kul := middleware.Aktor(r)
	basarili := false
	defer func() {
		httpx.DenetimDomain(h.DB, r, uid, kul, "subdomain.ssl.kur", tamAd, "tip="+tip, id, basarili)
	}()
	_ = os.MkdirAll(sslDir(sk), 0o750)

	switch tip {
	case "letsencrypt", "le":
		// 🔴 Challenge koku ROOT-SAHIPLI /var/www/acme -- alt alanin docroot'u
		// DEGIL. Kiracinin yazabildigi bir koke "allow all" vermek, erisim
		// kisitlamasini atlatma kapisi acar (olculdu). Vhost tarafi da AYNI
		// koku gosteriyor; ikisi birlikte degismeli.
		_ = acmeKoku()
		// Ön doğrulama (acme.go ADR-3): vhost'un challenge kökü doğru değilse
		// acme.sh'tan ÖNCE onarılır; aksi halde doğrulama 404 ile düşer.
		if err := h.acmeVhostHazir(r.Context(), sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError,
				"Doğrulama dosyasına erişilemedi (sunucu yapılandırması): "+nginxset.SunucuHatasi(r, "subdomain.ssl-acme-vhost", id, err))
			return
		}
		_, _ = komut("restorecon", "-R", filepath.Join(docroot, ".well-known"))
		if out, err := komut("/root/.acme.sh/acme.sh", "--issue", "--server", "letsencrypt",
			"--config-home", provisioner.AcmeConfigHome(), "--webroot", acmeKoku(),
			"-d", tamAd, "--keylength", "ec-256"); err != nil {
			// 🔴 acme.sh çıkış kodu 2 = RENEW_SKIP: geçerli cert ZATEN var, yenileme gerekmiyor.
			// Bu HATA DEĞİL — mevcut cert'i install-cert ile yerleştirmeye devam et. Aksi halde
			// (eski hata) ikinci kurulumda "Let's Encrypt alınamadı" yanılgısı verip panel SSL'i
			// hiç kurmuyordu. Yalnız DİĞER çıkış kodları (DNS/challenge hatası) gerçek başarısızlık.
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
				httpx.WriteError(w, http.StatusBadRequest, acmeHataMesaji("subdomain.ssl-le", id, tamAd, out))
				return
			}
		}
		if out, err := komut("/root/.acme.sh/acme.sh", "--install-cert", "--config-home", provisioner.AcmeConfigHome(), "-d", tamAd, "--ecc",
			"--key-file", key, "--fullchain-file", crt,
			"--reloadcmd", "systemctl reload nginx"); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "cert yerleştirilemedi: "+komutHatasi(r, "subdomain.ssl-kur", id, out))
			return
		}
	default: // self-signed
		if out, err := komut("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
			"-days", "365", "-keyout", key, "-out", crt,
			"-subj", "/CN="+tamAd, "-addext", "subjectAltName=DNS:"+tamAd); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "openssl: "+komutHatasi(r, "subdomain.ssl-openssl", id, out))
			return
		}
	}
	_, _ = komut("chown", "-R", sk+":"+sk, sslDir(sk))
	_, _ = komut("restorecon", "-R", sslDir(sk))

	// vhost'u yeniden yaz — cert artık var, rebuildVhost HTTPS bloğunu üretir;
	// alt alanın özel PHP havuzu + nginx/backend ayarları KORUNUR.
	// 🔴 Let's Encrypt 10-60 sn sürer; tarayıcı 30 sn'de kopunca sertifika diskte
	// ama vhost HTTP'de kalıyordu. Kopuk bağlam: vhost her durumda yazılır.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if err := h.rebuildVhost(kctx, sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "subdomain.ssl", id, err))
		return
	}
	basarili = true
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
	uid, kul := middleware.Aktor(r)
	basarili := false
	defer func() {
		httpx.DenetimDomain(h.DB, r, uid, kul, "subdomain.ssl.kaldir", tamAd, "", id, basarili)
	}()
	crt, key := certYolu(sk, tamAd)
	_ = os.Remove(crt)
	_ = os.Remove(key)
	// cert gitti → rebuildVhost HTTP bloğunu üretir; özel ayarlar KORUNUR.
	// Kopuk bağlam: cert silindikten sonra vhost var olmayan cert'e işaret etmesin.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if err := h.rebuildVhost(kctx, sslSid(r), sk, altAd, tamAd, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "subdomain.ssl-kaldir", id, err))
		return
	}
	basarili = true
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func dosyaVar(p string) bool { _, err := os.Stat(p); return err == nil }
