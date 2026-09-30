// Package subdomain: alt alan adı (subdomain) yönetimi — Plesk modeli.
// Subdomain, parent domain'in kullanıcısı/PHP havuzu altında; ayrı docroot + nginx server bloğu + DNS A kaydı.
package subdomain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/dns"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/php"
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

// Dış etkili adımlar (ev dizini, nginx/httpd conf.d, dış komutlar: `nginx -t`,
// `httpd -t`, systemctl, chown/chcon/restorecon, acme.sh, openssl). Testler
// sahteyle değiştirir: hata yolları ana makinenin /home, /etc/nginx, /etc/httpd
// dizinlerine ve servislerine dokunmadan sınanır. Üretimde değerler SABİTTİR.
var (
	evKoku     = "/home"
	nginxConfD = "/etc/nginx/conf.d"
	httpdConfD = "/etc/httpd/conf.d"
	komut      = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }
)

// komutHatasi — root olarak çalışan dış komutun (sunucu geneli `nginx -t`,
// acme.sh: panelin ACME hesap dizini/çıktısı, openssl) HAM çıktısının istemciye
// dönen metni: yönetici aynen görür (arıza ayıklama); öteki roller genel mesaj,
// ayrıntı günlüğe (nginxset.SunucuHatasi — bayi kapsamı).
func komutHatasi(r *http.Request, kaynak string, id int64, out []byte) string {
	return nginxset.SunucuHatasi(r, kaynak, id, errors.New(strings.TrimSpace(string(out))))
}

func docrootOf(sk, tamAd string) string { return evKoku + "/" + sk + "/subdomains/" + tamAd }
func confPath(sk, altAd string) string  { return nginxConfD + "/sub_" + sk + "_" + altAd + ".conf" }

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
	// 🔴 Hesap askıdaysa (tek kaynak) yeni alt alan adı da 503 askı vhost'uyla
	// doğar — askıdaki hesaba yayında bir alan adı eklenemez. Askıdan alınca
	// hesap askısı tüm alt alan adlarını tek üreticiden yeniden kurar.
	askida, aerr := hesaplar.HesapAskida(r.Context(), h.DB, sk)
	if aerr != nil {
		log.Printf("subdomain %s: %v", tamAd, aerr)
		httpx.WriteError(w, http.StatusInternalServerError, "hesap askı durumu okunamadı")
		return
	}
	docroot := docrootOf(sk, tamAd)
	govde := vhost(tamAd, docroot, socket, "")
	if askida {
		g, gerr := provisioner.AskiVhostGovde(tamAd, tamAd, "", "")
		if gerr != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "vhost yazılamadı")
			return
		}
		govde = g
	}
	if err := os.MkdirAll(docroot, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "docroot oluşturulamadı")
		return
	}
	// başlangıç sayfası
	if _, e := os.Stat(filepath.Join(docroot, "index.html")); e != nil {
		_ = os.WriteFile(filepath.Join(docroot, "index.html"),
			[]byte(provisioner.WelcomeHTML(tamAd)), 0o644)
	}
	_, _ = komut("chown", "-R", sk+":"+sk, evKoku+"/"+sk+"/subdomains")
	_, _ = komut("chcon", "-R", "-t", "httpd_sys_content_t", docroot)

	// nginx server bloğu
	conf := confPath(sk, altAd)
	if err := os.WriteFile(conf, []byte(govde), 0o644); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost yazılamadı")
		return
	}
	_, _ = komut("restorecon", conf)
	if out, err := komut("nginx", "-t"); err != nil {
		_ = os.Remove(conf) // rollback: bozuk conf'u kaldır (çalışan nginx etkilenmez)
		_, _ = komut("nginx", "-t")
		httpx.WriteError(w, http.StatusInternalServerError, "nginx doğrulanamadı: "+komutHatasi(r, "subdomain.olustur", id, out))
		return
	}
	_, _ = komut("systemctl", "reload", "nginx")

	if _, err := h.DB.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad, php_surum) VALUES (?,?,?,?)`,
		id, altAd, tamAd, phpSurum); err != nil {
		_ = os.Remove(conf)
		_, _ = komut("systemctl", "reload", "nginx")
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt eklenemedi")
		return
	}
	// DNS A kaydı (parent zone'a) + zone yaz
	if h.IPv4 != "" {
		if _, err := h.DB.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger, ttl, oncelik, aktif) VALUES (?,?,?,?,?,?,1)`,
			id, altAd, "A", h.IPv4, 3600, 0); err != nil {
			log.Printf("subdomain %s: DNS A kaydı eklenemedi: %v — subdomain çözümlenmeyebilir", tamAd, err)
		} else if err := dns.WriteZone(r.Context(), h.DB, id); err != nil {
			log.Printf("subdomain %s: zone dosyası yazılamadı: %v", tamAd, err)
		}
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
	// alt alanın kendi FPM havuzu varsa conf'unu tüm sürümlerden kaldır (DB satırları FK CASCADE)
	php.RemoveNamedPool(php.SubPoolName(sk, sid))
	subApacheSil(sk, altAd) // apache backend vhost'u (varsa) kaldır
	// docroot sil (guard: subdomains altında + tam_ad eşleşmeli)
	docroot := docrootOf(sk, tamAd)
	base := evKoku + "/" + sk + "/subdomains/"
	if strings.HasPrefix(docroot, base) && filepath.Clean(docroot) != filepath.Clean(base) {
		_ = os.RemoveAll(docroot)
	}
	if _, err := h.DB.Exec(`DELETE FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id); err != nil {
		log.Printf("subdomain sil: DB satırı silinemedi (sid=%d domain=%d): %v — nginx/docroot GİTTİ ama kayıt kaldı", sid, id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "subdomain kaydı silinemedi: "+err.Error())
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM dns_records WHERE domain_id=? AND ad=? AND tip='A'`, id, altAd); err != nil {
		log.Printf("subdomain sil: DNS A kaydı silinemedi (domain=%d ad=%s): %v — zone'da orphan kalabilir", id, altAd, err)
	}
	_ = dns.WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func vhost(tamAd, docroot, socket, koruma string) string {
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
        auth_basic off;
        root ` + docroot + `;
        try_files $uri =404;
    }

` + koruma + `
    error_page 404 /_gosp_404.html;
    location = /_gosp_404.html {
        root /usr/share/girginospanel/errors;
        internal;
        access_log off;
    }
    location ^~ /_gosp/ {
        alias /usr/share/girginospanel/errors/;
        access_log off;
        expires 7d;
        gzip on;
        gzip_types application/json application/javascript;
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
