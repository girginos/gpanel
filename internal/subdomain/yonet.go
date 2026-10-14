package subdomain

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// Detay: GET /domains/{id}/subdomain/{sid} — tek subdomain'in yönetim paneli verisi.
func (h *Handlers) Detay(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var s Sub
	var created sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, alt_ad, tam_ad, php_surum, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		   FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).
		Scan(&s.ID, &s.AltAd, &s.TamAd, &s.PHPSurum, &created); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	s.CreatedAt = created.String
	s.DocRoot = docrootOf(sk, s.TamAd)

	// disk kullanımı (docroot du -sk) — best effort
	var diskKB int64
	if out, err := exec.Command("du", "-sk", s.DocRoot).Output(); err == nil {
		f := strings.Fields(string(out))
		if len(f) > 0 {
			diskKB, _ = strconv.ParseInt(f[0], 10, 64)
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id":         s.ID,
		"alt_ad":     s.AltAd,
		"tam_ad":     s.TamAd,
		"php_surum":  s.PHPSurum,
		"docroot":    s.DocRoot,
		"created_at": s.CreatedAt,
		"parent_ad":  alanAdi,
		"parent_id":  id,
		"disk_kb":    diskKB,
		"ipv4":       h.IPv4,
	})
}

// PHPDegistir: PUT /domains/{id}/subdomain/{sid}/php {php_surum}
// Subdomain'in PHP-FPM sürümünü değiştirir — vhost socket'ini yeniden yazar, nginx reload.
func (h *Handlers) PHPDegistir(w http.ResponseWriter, r *http.Request) {
	id, sk, _, hostingPHP, demo, ok := h.parent(r)
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
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var req struct {
		PHPSurum string `json:"php_surum"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// Hosting yolundaki kuralla aynı (PHPSurumDogrula): desteklenmeyen değer ham yazılmasın.
	php, verr := provisioner.PHPSurumDogrula(req.PHPSurum)
	if verr != nil {
		httpx.WriteError(w, http.StatusBadRequest, verr.Error())
		return
	}
	var altAd, tamAd, eskiPHP string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&altAd, &tamAd, &eskiPHP); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	// Per-tenant FPM'de alt alan hesabın TEK havuzunu (hosting sürümü) kullanır: farklı
	// sürüm eskiden DB'ye yazılıp 200 dönüyordu ama çalışan sürüm değişmiyordu.
	if tenantFPMAktif(sk) && php != strings.TrimSpace(hostingPHP) {
		httpx.WriteError(w, http.StatusConflict, tenantAltAlanPHPIletisi(hostingPHP))
		return
	}
	// sürüm sunucuda kurulu mu? (doğrulama)
	if _, err := provisioner.PHPSocketFor(sk, php); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü sunucuda kurulu değil: "+php)
		return
	}
	uid, kul := middleware.Aktor(r)
	basarili := false
	defer func() {
		httpx.DenetimDomain(h.DB, r, uid, kul, "subdomain.php_surum", tamAd, "eski="+eskiPHP+" yeni="+php, id, basarili)
	}()
	if _, err := h.DB.Exec(`UPDATE subdomanlar SET php_surum=? WHERE id=? AND domain_id=?`, php, sid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt güncellenemedi")
		return
	}
	// vhost'u yeniden yaz — alt alanın ayrı FPM havuzu varsa yeni sürüme taşınır.
	// Kopuk bağlam: DB yeni sürümdeyken vhost/havuz eskide kalmasın.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if err := h.rebuildVhost(kctx, sid, sk, altAd, tamAd, php); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "subdomain.php", id, err))
		return
	}
	basarili = true
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "php_surum": php})
}

// tenantFPMAktif — hesapta per-tenant php-fpm kurulu mu (dikiş: testler sahteyle değiştirir).
var tenantFPMAktif = provisioner.TenantFPMActive

// tenantAltAlanPHPIletisi — per-tenant FPM'de alt alana hosting'den farklı sürüm istenince.
func tenantAltAlanPHPIletisi(hostingPHP string) string {
	return "bu hesapta alt alanlar hosting'in PHP sürümünü (" + strings.TrimSpace(hostingPHP) +
		") kullanır; sürümü hosting PHP ayarlarından değiştirin"
}
