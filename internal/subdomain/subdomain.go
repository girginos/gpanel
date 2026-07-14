// Package subdomain: alt alan adı (subdomain) yönetimi — Plesk modeli.
// Subdomain, parent domain'in kullanıcısı/PHP havuzu altında; ayrı docroot + nginx server bloğu + DNS A kaydı.
package subdomain

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/dns"
	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

var reAlt = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type Sub struct {
	ID        int64  `json:"id"`
	AltAd     string `json:"alt_ad"`
	TamAd     string `json:"tam_ad"`
	PHPSurum  string `json:"php_surum"`
	DocRoot   string `json:"docroot"`
	CreatedAt string `json:"created_at"`
}

func (h *Handlers) parent(r *http.Request) (id int64, sk, alanAdi, phpSurum string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0) FROM domains WHERE id=?`, id).
		Scan(&sk, &alanAdi, &phpSurum, &isDemo); err != nil {
		return id, "", "", "", false, false
	}
	return id, sk, alanAdi, phpSurum, isDemo == 1, true
}

func docrootOf(sk, tamAd string) string { return "/home/" + sk + "/subdomains/" + tamAd }
func confPath(sk, altAd string) string  { return "/etc/nginx/conf.d/sub_" + sk + "_" + altAd + ".conf" }

// GET /domains/{id}/subdomain
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, alt_ad, tam_ad, php_surum, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM subdomanlar WHERE domain_id=? ORDER BY alt_ad`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	out := []Sub{}
	for rows.Next() {
		var s Sub
		if err := rows.Scan(&s.ID, &s.AltAd, &s.TamAd, &s.PHPSurum, &s.CreatedAt); err == nil {
			s.DocRoot = docrootOf(sk, s.TamAd)
			out = append(out, s)
		}
	}
	_ = rows.Err()
	httpx.WriteJSON(w, http.StatusOK, out)
}

// POST /domains/{id}/subdomain  {alt_ad, php_surum?}
func (h *Handlers) Olustur(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, parentPHP, demo, ok := h.parent(r)
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
	var req struct {
		AltAd    string `json:"alt_ad"`
		PHPSurum string `json:"php_surum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	altAd := strings.ToLower(strings.TrimSpace(req.AltAd))
	if !reAlt.MatchString(altAd) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz alt alan (küçük harf/rakam/-)")
		return
	}
	phpSurum := strings.TrimSpace(req.PHPSurum)
	if phpSurum == "" {
		phpSurum = parentPHP
	}
	tamAd := altAd + "." + alanAdi
	if err := provisioner.ValidateDomain(tamAd); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz alan adı")
		return
	}
	// çakışma kontrolü
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM subdomanlar WHERE tam_ad=?`, tamAd).Scan(&n)
	if n == 0 {
		_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE alan_adi=?`, tamAd).Scan(&n)
	}
	if n > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu alan adı zaten kullanımda")
		return
	}
	socket, err := provisioner.PHPSocketFor(sk, phpSurum)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü sunucuda kurulu değil: "+phpSurum)
		return
	}
	docroot := docrootOf(sk, tamAd)
	if err := os.MkdirAll(docroot, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "docroot oluşturulamadı")
		return
	}
	// başlangıç sayfası
	if _, e := os.Stat(filepath.Join(docroot, "index.html")); e != nil {
		_ = os.WriteFile(filepath.Join(docroot, "index.html"),
			[]byte("<!doctype html><meta charset=utf-8><title>"+tamAd+"</title>"+
				"<body style='font-family:sans-serif;text-align:center;padding:60px'>"+
				"<h1>"+tamAd+"</h1><p>Subdomain hazır. Dosyalarınızı bu dizine yükleyin.</p></body>"), 0o644)
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, "/home/"+sk+"/subdomains").Run()
	_ = exec.Command("chcon", "-R", "-t", "httpd_sys_content_t", docroot).Run()

	// nginx server bloğu
	conf := confPath(sk, altAd)
	if err := os.WriteFile(conf, []byte(vhost(tamAd, docroot, socket)), 0o644); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost yazılamadı")
		return
	}
	_ = exec.Command("restorecon", conf).Run()
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		_ = os.Remove(conf) // rollback: bozuk conf'u kaldır (çalışan nginx etkilenmez)
		_ = exec.Command("nginx", "-t").Run()
		httpx.WriteError(w, http.StatusInternalServerError, "nginx doğrulanamadı: "+strings.TrimSpace(string(out)))
		return
	}
	_ = exec.Command("systemctl", "reload", "nginx").Run()

	if _, err := h.DB.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad, php_surum) VALUES (?,?,?,?)`,
		id, altAd, tamAd, phpSurum); err != nil {
		_ = os.Remove(conf)
		_ = exec.Command("systemctl", "reload", "nginx").Run()
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt eklenemedi")
		return
	}
	// DNS A kaydı (parent zone'a) + zone yaz
	if h.IPv4 != "" {
		_, _ = h.DB.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger, ttl, oncelik, aktif) VALUES (?,?,?,?,?,?,1)`,
			id, altAd, "A", h.IPv4, 3600, 0)
		_ = dns.WriteZone(r.Context(), h.DB, id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "tam_ad": tamAd, "docroot": docroot})
}

// DELETE /domains/{id}/subdomain/{sid}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
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
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var altAd, tamAd string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&altAd, &tamAd); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	_ = os.Remove(confPath(sk, altAd))
	_ = exec.Command("systemctl", "reload", "nginx").Run()
	// docroot sil (guard: subdomains altında + tam_ad eşleşmeli)
	docroot := docrootOf(sk, tamAd)
	base := "/home/" + sk + "/subdomains/"
	if strings.HasPrefix(docroot, base) && filepath.Clean(docroot) != filepath.Clean(base) {
		_ = os.RemoveAll(docroot)
	}
	_, _ = h.DB.Exec(`DELETE FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id)
	_, _ = h.DB.Exec(`DELETE FROM dns_records WHERE domain_id=? AND ad=? AND tip='A'`, id, altAd)
	_ = dns.WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func vhost(tamAd, docroot, socket string) string {
	return `server {
    listen 80;
    listen [::]:80;
    server_name ` + tamAd + `;

    root ` + docroot + `;
    index index.php index.html index.htm;

    access_log /var/log/nginx/` + tamAd + `.access.log;
    error_log  /var/log/nginx/` + tamAd + `.error.log warn;

    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    location /.well-known/acme-challenge/ {
        root /var/www/_acme;
        try_files $uri =404;
    }

    location / { try_files $uri $uri/ /index.php?$query_string; }

    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:` + socket + `;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_read_timeout 60s;
    }

    location ~* \.(jpg|jpeg|png|gif|ico|css|js|woff2?|svg|webp|avif|pdf|zip|gz)$ {
        expires 30d;
        access_log off;
    }

    location ~ /\.(?!well-known) { deny all; }

    # GirginOSPanel subdomain — ` + tamAd + `
}
`
}
