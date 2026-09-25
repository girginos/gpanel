// Package nginxset: per-domain nginx ayarlari (security header toggle + cache + ek direktifler)
package nginxset

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// Dış etkili adımlar (sunucu geneli `nginx -t`, vhost yazımı + reload). Testler
// sahteyle değiştirir: hata yolları ana makinenin nginx'ine dokunmadan sınanır.
var (
	direktifDogrula = provisioner.ValidateNginxDirectives
	vhostUygula     = provisioner.ApplyVhostForDomain
)

// ── İstemciye dönen web sunucusu hata metinleri (F1) ─────────────────────────
//
// 🔴 NEDEN: `nginx -t` TEK bir alan adını değil SUNUCUNUN TÜM yapılandırmasını
// sınar; çıktısı öteki kiracıların satırlarını taşır: sunucu genelindeki
// `ssl_stapling on` yüzünden her SSL'li alan adı için
// `"ssl_stapling" ignored ... certificate "/etc/pki/girginospanel/<başka-alan>/..."`
// uyarısı, küresel bozulmada komşunun `/etc/nginx/conf.d/dom_<sk>.conf:N` yolu.
// Bu çıktı nginx-settings / WAF / PHP kayıt uçlarından bayiye ve müşteriye
// AYNEN dönüyordu: kök hesabın ve öteki bayilerin alan adları, sertifika
// yolları ve sistem kullanıcı adları. Ayrıntı artık günlüğe yazılır; yönetici
// (sunucunun sahibi) yanıtta ayrıntıyı görmeye devam eder — o veri zaten
// onundur ve arıza ayıklamak için gereklidir (backups.Delete ile aynı ilke).

// SunucuHatasiGenel — yönetici olmayana ham ayrıntı yerine dönen metin.
const SunucuHatasiGenel = "sunucu yapılandırması uygulanamadı — ayrıntı sunucu günlüğüne yazıldı; sorun sürerse yöneticinize başvurun"

// SunucuHatasi — vhost render / `nginx -t` / reload / php-fpm uygulama hatasını
// istemciye dönecek metne çevirir. Ayrıntıyı HER ZAMAN günlüğe yazar; yanıtta
// yalnız yöneticiye verir. Çağıranın öneki (ör. "vhost: ") korunur, böylece
// yöneticinin gördüğü metin öncekiyle birebir aynı kalır.
func SunucuHatasi(r *http.Request, kaynak string, domainID int64, err error) string {
	log.Printf("%s: sunucu yapılandırması uygulanamadı (domain=%d): %v", kaynak, domainID, err)
	if middleware.RolFrom(r) == "admin" {
		return err.Error()
	}
	return SunucuHatasiGenel
}

// DirektifHatasiGenel — yönetici olmayana, kendi direktiflerine ait satır
// ayıklanamadığında dönen metin.
const DirektifHatasiGenel = "direktifler web sunucusu doğrulamasından geçmedi — sözdizimini ve kullanılan değişken adlarını kontrol edin"

// DirektifHatasi — ek direktif doğrulama (`nginx -t`) hatasını istemciye dönecek
// metne çevirir. Yönetici ham çıktıyı görür. Öteki roller yalnız KENDİ
// direktiflerine ait hata satırlarını görür (bkz. DirektifHatasiOzet); böylece
// sözdizimi hatası hâlâ düzeltilebilir kalır ama komşu yapılandırma sızmaz.
func DirektifHatasi(r *http.Request, kaynak string, domainID int64, err error, direktifler string) string {
	log.Printf("%s: ek direktif doğrulaması başarısız (domain=%d): %v", kaynak, domainID, err)
	if middleware.RolFrom(r) == "admin" {
		return err.Error()
	}
	if oz := DirektifHatasiOzet(err.Error(), direktifler); oz != "" {
		return oz
	}
	return DirektifHatasiGenel
}

// mutlakYolRe — metindeki mutlak dosya yolu adayları.
var mutlakYolRe = regexp.MustCompile(`/[^\s"'<>;,()]+`)

// direktifYeri — provisioner.ValidateNginxDirectives geçici dosyanın yolunu bu
// etiketle değiştirir; bu etiketi taşıyan satır YALNIZ kiracının kendi
// direktiflerini içeren geçici server bloğundan gelir.
const direktifYeri = "(direktifler)"

// DirektifHatasiOzet — `nginx -t` çıktısından YALNIZ kiracının kendi
// direktiflerine ait hata satırlarını ayıklar; yoksa "" döner.
//
// Kural (ret varsayılan):
//   - Yalnız "[emerg]" satırları: uyarılar ([warn]) komşuların satırlarıdır
//     (ör. her SSL'li alan adının stapling uyarısı).
//   - Satır ya kiracının geçici dosyasını gösterir ("(direktifler)") ya da hiç
//     dosya konumu taşımaz VE en az bir tırnaklı değer içerir VE her tırnaklı
//     değer kiracının kendi metninde AYRI BİR SÖZCÜK olarak geçer (ör.
//     `unknown "x" variable` — x, kiracının yazdığı $x). Sözcük eşleşmesi,
//     kiracının kısa bir alt dizgiyle komşunun adlarını yoklamasını önler.
//   - Kalan satırlardaki, kiracının kendi metninde geçmeyen her mutlak yol
//     "(yol)" ile değiştirilir.
func DirektifHatasiOzet(cikti, direktifler string) string {
	var tut []string
	for _, s := range strings.Split(cikti, "\n") {
		s = strings.TrimSpace(s)
		if !strings.Contains(s, "[emerg]") {
			continue
		}
		if !strings.Contains(s, direktifYeri) {
			if strings.Contains(s, " in /") || !tirnaklilarKendi(s, direktifler) {
				continue
			}
		}
		s = mutlakYolRe.ReplaceAllStringFunc(s, func(y string) string {
			if strings.Contains(direktifler, y) {
				return y
			}
			return "(yol)"
		})
		tut = append(tut, s)
	}
	oz := strings.Join(tut, "\n")
	if len(oz) > 400 {
		oz = oz[:400] + "…"
	}
	return oz
}

// tirnaklilarKendi — satırda en az bir tırnaklı değer var ve HER biri kiracının
// metninde ayrı bir sözcük olarak geçiyor mu?
func tirnaklilarKendi(satir, direktifler string) bool {
	parca := strings.Split(satir, `"`)
	if len(parca) < 3 {
		return false
	}
	for i := 1; i < len(parca); i += 2 {
		if !sozcukVar(direktifler, parca[i]) {
			return false
		}
	}
	return true
}

// sozcukVar — metin, s'yi iki yanı harf/rakam/alt çizgi OLMAYAN bir konumda içeriyor mu?
func sozcukVar(metin, s string) bool {
	if s == "" {
		return false
	}
	kelimeHarfi := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for bas := 0; ; {
		i := strings.Index(metin[bas:], s)
		if i < 0 {
			return false
		}
		i += bas
		son := i + len(s)
		if (i == 0 || !kelimeHarfi(metin[i-1])) && (son == len(metin) || !kelimeHarfi(metin[son])) {
			return true
		}
		bas = i + 1
	}
}

type Settings struct {
	HdrXContentType bool `json:"hdr_x_content_type"`
	HdrXXSS         bool `json:"hdr_x_xss"`
	HdrReferrer     bool `json:"hdr_referrer"`
	HdrPermissions  bool `json:"hdr_permissions"`
	HdrCSPUpgrade   bool `json:"hdr_csp_upgrade"`
	HdrHSTS         bool `json:"hdr_hsts"`
	HSTSMaxAge      int  `json:"hsts_max_age"`
	HSTSSubdomains  bool `json:"hsts_subdomains"`
	HSTSPreload     bool `json:"hsts_preload"`

	// Performans onbellegi
	FastCgiCache       bool `json:"fastcgi_cache"`
	FastCgiCacheDakika int  `json:"fastcgi_cache_dakika"`
	BrowserCache       bool `json:"browser_cache"`
	BrowserCacheGun    int  `json:"browser_cache_gun"`

	EkDirektifler string `json:"ek_direktifler"`
}

func Defaults() Settings {
	return Settings{
		HdrXContentType: true, HdrXXSS: true, HdrReferrer: true,
		HdrPermissions: true, HdrCSPUpgrade: true, HdrHSTS: true,
		HSTSMaxAge: 31536000, HSTSSubdomains: true, HSTSPreload: false,
		FastCgiCache: false, FastCgiCacheDakika: 60,
		BrowserCache: true, BrowserCacheGun: 30,
		EkDirektifler: "",
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func Get(ctx context.Context, db *sql.DB, domainID int64) (Settings, error) {
	s := Defaults()
	var b1, b2, b3, b4, b5, b6, b7, b8, bFC, bBC int
	err := db.QueryRowContext(ctx,
		`SELECT hdr_x_content_type, hdr_x_xss, hdr_referrer, hdr_permissions,
		        hdr_csp_upgrade, hdr_hsts, hsts_max_age, hsts_subdomains, hsts_preload,
		        ek_direktifler, fastcgi_cache, fastcgi_cache_dakika,
		        browser_cache, browser_cache_gun
		 FROM nginx_settings WHERE domain_id=?`, domainID).
		Scan(&b1, &b2, &b3, &b4, &b5, &b6, &s.HSTSMaxAge, &b7, &b8,
			&s.EkDirektifler, &bFC, &s.FastCgiCacheDakika, &bBC, &s.BrowserCacheGun)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.HdrXContentType = b1 == 1
	s.HdrXXSS = b2 == 1
	s.HdrReferrer = b3 == 1
	s.HdrPermissions = b4 == 1
	s.HdrCSPUpgrade = b5 == 1
	s.HdrHSTS = b6 == 1
	s.HSTSSubdomains = b7 == 1
	s.HSTSPreload = b8 == 1
	s.FastCgiCache = bFC == 1
	s.BrowserCache = bBC == 1
	return s, nil
}

func Save(ctx context.Context, db *sql.DB, domainID int64, s Settings) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO nginx_settings(domain_id, hdr_x_content_type, hdr_x_xss, hdr_referrer,
		    hdr_permissions, hdr_csp_upgrade, hdr_hsts, hsts_max_age, hsts_subdomains, hsts_preload,
		    ek_direktifler, fastcgi_cache, fastcgi_cache_dakika, browser_cache, browser_cache_gun)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
		    hdr_x_content_type=VALUES(hdr_x_content_type),
		    hdr_x_xss=VALUES(hdr_x_xss),
		    hdr_referrer=VALUES(hdr_referrer),
		    hdr_permissions=VALUES(hdr_permissions),
		    hdr_csp_upgrade=VALUES(hdr_csp_upgrade),
		    hdr_hsts=VALUES(hdr_hsts),
		    hsts_max_age=VALUES(hsts_max_age),
		    hsts_subdomains=VALUES(hsts_subdomains),
		    hsts_preload=VALUES(hsts_preload),
		    ek_direktifler=VALUES(ek_direktifler),
		    fastcgi_cache=VALUES(fastcgi_cache),
		    fastcgi_cache_dakika=VALUES(fastcgi_cache_dakika),
		    browser_cache=VALUES(browser_cache),
		    browser_cache_gun=VALUES(browser_cache_gun)`,
		domainID, b2i(s.HdrXContentType), b2i(s.HdrXXSS), b2i(s.HdrReferrer),
		b2i(s.HdrPermissions), b2i(s.HdrCSPUpgrade), b2i(s.HdrHSTS),
		s.HSTSMaxAge, b2i(s.HSTSSubdomains), b2i(s.HSTSPreload),
		s.EkDirektifler, b2i(s.FastCgiCache), s.FastCgiCacheDakika,
		b2i(s.BrowserCache), s.BrowserCacheGun)
	return err
}

type Handlers struct {
	DB *sql.DB
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	s, err := Get(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alan_adi": alanAdi,
		"ayarlar":  s,
	})
}

func (h *Handlers) Kaydet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		Ayarlar Settings `json:"ayarlar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	var php, sk string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT php_surum, sistem_kullanici FROM domains WHERE id=?`, id).
		Scan(&php, &sk); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	// Guvenlik: tenant ek_direktifler LFD/SSRF/RCE denylist + nginx -t sozdizim dogrulama
	if bad := provisioner.TehlikeliNginxDirektifi(req.Ayarlar.EkDirektifler); bad != "" {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik: nginx '"+bad+"' direktifine izin verilmiyor")
		return
	}
	if err := direktifDogrula(req.Ayarlar.EkDirektifler); err != nil {
		httpx.WriteError(w, http.StatusBadRequest,
			"geçersiz nginx direktifi: "+DirektifHatasi(r, "nginxset", id, err, req.Ayarlar.EkDirektifler))
		return
	}
	if err := Save(r.Context(), h.DB, id, req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydet: "+err.Error())
		return
	}
	socket, err := provisioner.PHPSocketFor(sk, php)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "socket: "+err.Error())
		return
	}
	if err := vhostUygula(h.DB, id, socket, php); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost: "+SunucuHatasi(r, "nginxset", id, err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
