package subdomain

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/php"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// rebuildVhost: alt alanın nginx vhost'unu MEVCUT duruma göre yeniden yazar —
// tek doğruluk kaynağı. Soketi çözer (alt alanın KENDİ FPM havuzu varsa onu,
// yoksa parent soketini), SSL cert varsa HTTPS bloğunu, koruma bloklarını
// uygular; nginx -t + reload, hata olursa eski conf'a geri alır.
// Part 3 bu fonksiyonu nginx ayarları + backend paritesiyle genişletir.
// rebuildVhost — Handlers uzerinden kisayol.
func (h *Handlers) rebuildVhost(ctx context.Context, sid int64, sk, altAd, tamAd, phpSurum string) error {
	return rebuildVhostDB(ctx, h.DB, sid, sk, altAd, tamAd, phpSurum)
}

// rebuildVhostDB — alt alan vhost'unun TEK ureticisi.
//
// 🔴 Onceki surumde IKI ayri uretici vardi: burasi ve koruma.go'daki
// vhostSSL/vhost. Sifre-koruma degistirildiginde ikincisi calisiyor ve
// URETTIGI DOSYADA erisim kisitlama blogu YOKTU -- yani musteri .htpasswd
// ayarina dokununca kisitlama SESSIZCE dusuyordu. (Ayni yol musterinin nginx
// ayarlarini da kaybediyordu.) Iki uretici kaçinilmaz olarak ayrisir; tek
// uretici tek davranis demektir.
func rebuildVhostDB(ctx context.Context, db *sql.DB, sid int64, sk, altAd, tamAd, phpSurum string) error {
	return rebuildVhostIc(ctx, db, sid, sk, altAd, tamAd, phpSurum)
}

func rebuildVhostIc(ctx context.Context, db *sql.DB, sid int64, sk, altAd, tamAd, phpSurum string) error {
	// 🔴 Kilit EN BASTA alinir. Onceki surumde yalniz dosya yazma bolumunu
	// sariyordu; oysa EnsureCacheZone ve subApacheYaz da conf.d/httpd
	// altinda dosya yazip silebiliyor ve GLOBAL `nginx -t`'yi etkiliyor.
	// Kapsam disi kalan her adim, kapatmaya calistigimiz yarisi geri acar.
	birak := provisioner.RenderKilidiAl()
	defer birak()

	docroot := docrootOf(sk, tamAd)
	socket, hasPool, err := php.ApplyForSub(ctx, db, sk, sid, phpSurum)
	if err != nil {
		return err
	}
	if !hasPool {
		socket, err = provisioner.PHPSocketFor(sk, phpSurum)
		if err != nil {
			return err
		}
	}
	koruma := provisioner.ProtectedBlocksForSub(db, sid, socket, webBackendGet(ctx, db, sid))
	crt, key := certYolu(sk, tamAd)
	ssl := dosyaVar(crt) && dosyaVar(key)
	ng, _ := subNginxGet(ctx, db, sid)
	backend := webBackendGet(ctx, db, sid)
	if ng.FastcgiCache && backend == "php-fpm" {
		_, _ = provisioner.EnsureCacheZone()
	}
	// apache backend: nginx'i yeniden yazmadan ÖNCE upstream'i hazırla (502'yi önle)
	if backend == "apache" {
		if e := subApacheYaz(sk, altAd, tamAd, docroot, socket); e != nil {
			return e
		}
	} else {
		subApacheSil(sk, altAd)
	}

	// 🔴 Erisim kisitlama: hata varsa vhost'a DOKUNMA. Diskteki yapilandirma
	// korumali olabilir; okunamayan bir ayar yuzunden onu ezmek, korumayi
	// SESSIZCE kaldirmak olurdu.
	kisit, kerr := provisioner.AltAlanErisimBloku(db, sid)
	if kerr != nil {
		return fmt.Errorf("erisim kisitlama okunamadi, alt alan vhost'u DEGISTIRILMEDI (%s): %w", tamAd, kerr)
	}

	o := subVhostOpts{TamAd: tamAd, DocRoot: docroot, Socket: socket, Backend: backend,
		SSL: ssl, Crt: crt, Key: key, Koruma: koruma, N: ng, Kisit: kisit}
	body := renderSubVhost(o)
	conf := confPath(sk, altAd)

	eskiB, _ := os.ReadFile(conf)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return err
	}
	_ = exec.Command("restorecon", conf).Run()
	if out, e := exec.Command("nginx", "-t").CombinedOutput(); e != nil {
		if len(eskiB) > 0 {
			_ = os.WriteFile(conf, eskiB, 0o644) // rollback
		} else {
			_ = os.Remove(conf)
		}
		_ = exec.Command("systemctl", "reload", "nginx").Run()
		return &nginxHata{strings.TrimSpace(string(out))}
	}
	if out, e := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); e != nil {
		// Ana domainde olculen ders: reload dalinda geri alma YOKSA dosya
		// diskte kalir ve BASKA bir domainin sonraki basarili reload'i, hic
		// sinanmamis bu degisikligi sessizce yururluge sokar.
		if len(eskiB) > 0 {
			_ = os.WriteFile(conf, eskiB, 0o644)
		} else {
			_ = os.Remove(conf)
		}
		return fmt.Errorf("nginx reload BASARISIZ, %s GERI ALINDI: %s", conf, strings.TrimSpace(string(out)))
	}
	return nil
}

type nginxHata struct{ msg string }

// Error — nginx ciktisi istemciye HAM verilmez.
//
// 🔴 Global `nginx -t` ciktisi KOMSU yapilandirmalarin direktif adlarini ve
// yollarini icerebiliyor; olculdu ("unknown directive X in ..."). Kiraciya
// yalniz kendi isini yapamadigi bilgisi doner; ayrinti panel loguna gider.
func (e *nginxHata) Error() string {
	log.Printf("alt alan nginx dogrulama hatasi (ayrinti): %s", e.msg)
	return "nginx yapılandırma doğrulaması başarısız — değişiklik uygulanmadı"
}

// PHPAyarGet: GET /domains/{id}/subdomain/{sid}/php-settings
// Alt alanın PHP ayarlarını döner (kendi satırı yoksa varsayılanlar).
func (h *Handlers) PHPAyarGet(w http.ResponseWriter, r *http.Request) {
	id, _, _, _, _, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var tamAd, phpSurum string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).
		Scan(&tamAd, &phpSurum); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	ayarlar, err := php.GetSub(r.Context(), h.DB, sid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayarlar okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"tam_ad":     tamAd,
		"php_surum":  phpSurum,
		"ozel_havuz": php.HasSub(r.Context(), h.DB, sid),
		"ayarlar":    ayarlar,
	})
}

// PHPAyarPut: PUT /domains/{id}/subdomain/{sid}/php-settings {php_surum?, ayarlar}
// Ayarları kaydeder → alt alana AYRI FPM havuzu render eder → vhost'u soketine yönlendirir.
func (h *Handlers) PHPAyarPut(w http.ResponseWriter, r *http.Request) {
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
	var altAd, tamAd, mevcutPHP string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).
		Scan(&altAd, &tamAd, &mevcutPHP); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "subdomain bulunamadı")
		return
	}
	var req struct {
		PHPSurum string       `json:"php_surum"`
		Ayarlar  php.Settings `json:"ayarlar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// güvenlik: skalar enjeksiyon + ek_direktifler allowlist (domain ile aynı sertleştirme)
	if err := php.ValidateSettings(req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	temiz, err := php.SanitizeEk(req.Ayarlar.EkDirektifler)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Ayarlar.EkDirektifler = temiz

	surum := strings.TrimSpace(req.PHPSurum)
	if surum == "" {
		surum = mevcutPHP
	}
	if err := php.SaveSub(r.Context(), h.DB, sid, req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi")
		return
	}
	if surum != mevcutPHP {
		if _, err := h.DB.Exec(`UPDATE subdomanlar SET php_surum=? WHERE id=? AND domain_id=?`, surum, sid, id); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "sürüm güncellenemedi")
			return
		}
	}
	if err := h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, surum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "php_surum": surum, "ozel_havuz": true})
}
