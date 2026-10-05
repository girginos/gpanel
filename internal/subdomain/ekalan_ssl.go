package subdomain

// ekalan_ssl.go — ek alan adı SSL: öz-imzalı veya Let's Encrypt. Alt alan SSL'iyle
// AYNI mantık/komutlar (openssl; acme.sh --webroot /var/www/acme; --install-cert;
// reloadcmd), AYNI sertifika yolları (certYolu) ve AYNI kök-sahipli challenge kökü;
// farklar: sertifika apex + www'yi kapsar, acme hatası sınıflandırılır, hesap başına
// deneme hız sınırı + ek alan başına tek eşzamanlı kurulum vardır. Yenileme: acme.sh
// kaydı (--install-cert reloadcmd) cron ile TÜM ek alan adlarını yeniler.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"
)

// Let's Encrypt kötüye kullanım sınırı: hesap (sistem kullanıcısı) başına, kayan 1 saatte
// en çok ekSSLSaatlikDeneme deneme. Öz-imza sınırsız (LE'yi tüketmez).
const (
	ekSSLSaatlikDeneme = 5
	ekSSLPencere       = time.Hour
)

var (
	ekSSLHizMu     sync.Mutex
	ekSSLDenemeler = map[string][]time.Time{} // sk → deneme zamanları
	ekSSLSuruyor   sync.Map                   // ek alan id → struct{}
	ekSimdi        = time.Now
)

// ekSSLHizIzin — hesap için yeni bir LE denemesine izin var mı? Varsa denemeyi kaydeder.
func ekSSLHizIzin(sk string) bool {
	ekSSLHizMu.Lock()
	defer ekSSLHizMu.Unlock()
	simdi := ekSimdi()
	var kalan []time.Time
	for _, t := range ekSSLDenemeler[sk] {
		if simdi.Sub(t) < ekSSLPencere {
			kalan = append(kalan, t)
		}
	}
	if len(kalan) >= ekSSLSaatlikDeneme {
		ekSSLDenemeler[sk] = kalan
		return false
	}
	ekSSLDenemeler[sk] = append(kalan, simdi)
	return true
}

// ekAcmeHataSinifla — acme.sh çıktısını kullanıcıya sade, işlem yapılabilir mesaja çevirir
// (ham çıktı yalnız yöneticiye eklenir).
func ekAcmeHataSinifla(out string) string {
	m := strings.ToLower(out)
	switch {
	case strings.Contains(m, "ratelimited") || strings.Contains(m, "too many") || strings.Contains(m, "rate limit"):
		return "Let's Encrypt hız sınırına takıldı (aynı alan adı için çok sayıda deneme); bir süre sonra tekrar deneyin"
	case strings.Contains(m, "nxdomain") || strings.Contains(m, "no valid a records") ||
		strings.Contains(m, "could not resolve") || strings.Contains(m, "servfail") || strings.Contains(m, "dns problem"):
		return "Alan adı henüz bu sunucuya çözümlenmiyor (DNS): alan adı ve www için A kaydını bu sunucuya yönlendirin, yayılmasını bekleyip tekrar deneyin"
	case strings.Contains(m, "timeout during connect") || strings.Contains(m, "connection refused") ||
		strings.Contains(m, "invalid response") || strings.Contains(m, "fetching http") ||
		strings.Contains(m, "verify error") || strings.Contains(m, "unauthorized") || strings.Contains(m, "404"):
		return "Let's Encrypt doğrulama dosyasına erişemedi (challenge): alan adı ve www bu sunucuya yönlenmeli ve 80. port erişilebilir olmalı"
	}
	return "Let's Encrypt sertifikası alınamadı"
}

// ekSSLUygula — sertifikayı kurar + vhost'u yeniden yazar + DB bilgisini günceller.
// Dönüş: kullanıcıya gösterilecek hata ("" = başarılı) ve HTTP kodu (hata varsa).
func (h *Handlers) ekSSLUygula(ctx context.Context, r *http.Request, e ekSatir, tip string) (string, int) {
	if !reEkSK.MatchString(e.SK) || !ekAdGuvenli(e.Ad) {
		return "geçersiz kayıt", http.StatusBadRequest
	}
	if _, yuk := ekSSLSuruyor.LoadOrStore(e.ID, struct{}{}); yuk {
		return "Bu alan adı için SSL kurulumu zaten sürüyor", http.StatusConflict
	}
	defer ekSSLSuruyor.Delete(e.ID)

	crt, key := certYolu(e.SK, e.Ad)
	if err := os.MkdirAll(sslDir(e.SK), 0o750); err != nil {
		return "SSL dizini oluşturulamadı", http.StatusInternalServerError
	}
	switch tip {
	case "letsencrypt":
		if !ekSSLHizIzin(e.SK) {
			return "Bu hesap için saatlik Let's Encrypt deneme sınırı doldu; bir süre sonra tekrar deneyin", http.StatusTooManyRequests
		}
		// Challenge kökü ROOT-SAHİPLİ /var/www/acme (alt alanla aynı): kiracının yazabildiği
		// docroot'a "allow all" konumu bırakmak erişim kısıtlamasını atlatma kapısı olurdu.
		_ = acmeKoku()
		if out, err := komut("/root/.acme.sh/acme.sh", "--issue", "--server", "letsencrypt",
			"--config-home", provisioner.AcmeConfigHome(), "--webroot", acmeKoku(),
			"-d", e.Ad, "-d", "www."+e.Ad, "--keylength", "ec-256"); err != nil {
			// acme.sh çıkış kodu 2 = RENEW_SKIP: geçerli sertifika zaten var → hata DEĞİL.
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 2 {
				msg := ekAcmeHataSinifla(string(out))
				if middleware.RolFrom(r) == "admin" {
					msg += " — " + komutHatasi(r, "ekalan.ssl-le", e.DomainID, out)
				} else {
					_ = komutHatasi(r, "ekalan.ssl-le", e.DomainID, out) // ayrıntı günlüğe
				}
				return msg, http.StatusBadRequest
			}
		}
		if out, err := komut("/root/.acme.sh/acme.sh", "--install-cert", "--config-home", provisioner.AcmeConfigHome(),
			"-d", e.Ad, "--ecc", "--key-file", key, "--fullchain-file", crt,
			"--reloadcmd", "systemctl reload nginx"); err != nil {
			return "sertifika yerleştirilemedi: " + komutHatasi(r, "ekalan.ssl-kur", e.DomainID, out), http.StatusInternalServerError
		}
	default: // self-signed
		if out, err := komut("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
			"-days", "365", "-keyout", key, "-out", crt,
			"-subj", "/CN="+e.Ad, "-addext", "subjectAltName=DNS:"+e.Ad+",DNS:www."+e.Ad); err != nil {
			return "openssl: " + komutHatasi(r, "ekalan.ssl-openssl", e.DomainID, out), http.StatusInternalServerError
		}
	}
	_, _ = komut("chown", "-R", "-h", e.SK+":"+e.SK, sslDir(e.SK))
	_, _ = komut("restorecon", "-R", sslDir(e.SK))

	// Sertifika diskte → vhost HTTPS bloğuyla yeniden yazılır. Kopuk bağlam: LE 10-60 sn
	// sürer, istemci kopsa da vhost yazılır (sertifika var, vhost HTTP'de kalmasın).
	if err := ekVhostYenidenKur(context.WithoutCancel(ctx), h.DB, e.ID); err != nil {
		return nginxset.SunucuHatasi(r, "ekalan.ssl", e.DomainID, err), http.StatusInternalServerError
	}
	h.ekSSLKaydet(context.WithoutCancel(ctx), e.ID, e.SK, e.Ad)
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "ek_alan.ssl_kur", e.Ad, "tip="+tip, e.Kiraci, true)
	return "", 0
}

// ekSSLKaydet — ek_alan_adlari.ssl_aktif/ssl_bitis (liste önbelleği; dosya doğruluk kaynağı).
func (h *Handlers) ekSSLKaydet(ctx context.Context, id int64, sk, ad string) {
	crt, key := certYolu(sk, ad)
	aktif := dosyaVar(crt) && dosyaVar(key)
	var bitis any
	if aktif {
		if b := certBitis(crt); b != "" {
			bitis = b
		}
	}
	a := 0
	if aktif {
		a = 1
	}
	_, _ = h.DB.ExecContext(ctx, `UPDATE ek_alan_adlari SET ssl_aktif=?, ssl_bitis=? WHERE id=?`, a, bitis, id)
}

// GET /domains/{id}/ek-alan-adlari/{eid}/ssl
func (h *Handlers) EkSSLDurum(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	crt, key := certYolu(s.SK, s.Ad)
	aktif := dosyaVar(crt) && dosyaVar(key)
	bitis := ""
	if aktif {
		bitis = certBitis(crt)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"aktif": aktif, "bitis": bitis})
}

// POST /domains/{id}/ek-alan-adlari/{eid}/ssl {tip}
func (h *Handlers) EkSSLKur(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	if s.Demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	var req struct {
		Tip string `json:"tip"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tip := strings.ToLower(strings.TrimSpace(req.Tip))
	switch tip {
	case "", "self-signed":
		tip = "self-signed"
	case "le", "letsencrypt":
		tip = "letsencrypt"
	default:
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz tip (self-signed|letsencrypt)")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if hata, kod := h.ekSSLUygula(kctx, r, s, tip); hata != "" {
		httpx.WriteError(w, kod, hata)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "tam_ad": s.Ad, "tip": tip})
}

// DELETE /domains/{id}/ek-alan-adlari/{eid}/ssl
func (h *Handlers) EkSSLKaldir(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	if s.Demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !reEkSK.MatchString(s.SK) || !ekAdGuvenli(s.Ad) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kayıt")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	ekAcmeSil(s.SK, s.Ad) // acme kaydı + sertifika dosyaları (yenileme cron'u kalmasın)
	if err := ekVhostYenidenKur(kctx, h.DB, s.ID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "ekalan.ssl-kaldir", s.DomainID, err))
		return
	}
	h.ekSSLKaydet(kctx, s.ID, s.SK, s.Ad)
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "ek_alan.ssl_kaldir", s.Ad, "", s.Kiraci, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
