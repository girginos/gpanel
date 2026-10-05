package subdomain

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/nginxset"
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
	return vhostYenidenKur(ctx, h.DB, sid, sk, altAd, tamAd, phpSurum)
}

// vhostYenidenKur — alt alan vhost'unu yeniden kurar (php-fpm havuzu +
// `httpd -t` + sunucu geneli `nginx -t` + reload). Testler sahteyle değiştirir:
// hata metninin kime ne döndüğü ana makineye dokunmadan sınanır.
//
// 🔴 Hatası istemciye nginxset.SunucuHatasi ile döner: `nginx -t` çıktısı
// nginxHata ile gizli olsa da `httpd -t` (apache backend'li alt alan: komşu
// kiracıların /etc/httpd/conf.d/sub_apache_<sk>_… yolları), php-fpm ve reload
// çıktıları ham taşınır.
var vhostYenidenKur = rebuildVhostDB

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

	// 🔴 HESAP askıdaysa (tek kaynak: hesaplar.HesapAskida) alt alan adı da ana
	// vhost'la AYNI 503 askı şablonuyla yazılır. Bu fonksiyon alt alan vhost'unun
	// tek üreticisi olduğundan SSL verme/yenileme, PHP sürüm/ayar değişimi, web
	// sunucu değişimi, koruma/erişim yeniden çizimi ve sapma iyileştirici askıyı
	// DELEMEZ. Askıdayken alt alanın PHP havuzu KURULMAZ. Durum okunamazsa vhost'a
	// DOKUNMA (ne açar ne kapatır).
	askida, aerr := hesaplar.HesapAskida(ctx, db, sk)
	if aerr != nil {
		return fmt.Errorf("alt alan vhost'u DEGISTIRILMEDI (%s): %w", tamAd, aerr)
	}
	var body string
	if askida {
		crt, key := certYolu(sk, tamAd)
		if !dosyaVar(crt) || !dosyaVar(key) {
			crt, key = "", ""
		}
		b, err := provisioner.AskiVhostGovde(tamAd, tamAd, crt, key)
		if err != nil {
			return err
		}
		body = b
	} else {
		b, err := altVhostGovde(ctx, db, sid, sk, altAd, tamAd, phpSurum)
		if err != nil {
			return err
		}
		body = b
	}
	conf := confPath(sk, altAd)

	eskiB, _ := os.ReadFile(conf)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return err
	}
	_, _ = komut("restorecon", conf)
	if out, e := komut("nginx", "-t"); e != nil {
		if len(eskiB) > 0 {
			_ = os.WriteFile(conf, eskiB, 0o644) // rollback
		} else {
			_ = os.Remove(conf)
		}
		_, _ = komut("systemctl", "reload", "nginx")
		return &nginxHata{strings.TrimSpace(string(out))}
	}
	if out, e := komut("systemctl", "reload", "nginx"); e != nil {
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
	if askida {
		// nginx artık 503 döndürüyor: alt alanın apache arka ucu (varsa) kaldırılır;
		// askıdan alınca altVhostGovde ayardan yeniden yazar.
		subApacheSil(sk, altAd)
	}
	return nil
}

// altVhostGovde — askıda OLMAYAN hesabın alt alan vhost gövdesi: PHP havuzu
// (alt alanın kendi havuzu ya da parent soketi), apache arka ucu, koruma ve erişim
// kısıtlama blokları, SSL, nginx ayarları.
func altVhostGovde(ctx context.Context, db *sql.DB, sid int64, sk, altAd, tamAd, phpSurum string) (string, error) {
	docroot := docrootOf(sk, tamAd)
	socket, hasPool, err := php.ApplyForSub(ctx, db, sk, sid, phpSurum)
	if err != nil {
		return "", err
	}
	if !hasPool {
		socket, err = provisioner.PHPSocketFor(sk, phpSurum)
		if err != nil {
			return "", err
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
			return "", e
		}
	} else {
		subApacheSil(sk, altAd)
	}

	// 🔴 Erisim kisitlama: hata varsa vhost'a DOKUNMA. Diskteki yapilandirma
	// korumali olabilir; okunamayan bir ayar yuzunden onu ezmek, korumayi
	// SESSIZCE kaldirmak olurdu.
	kisit, kerr := provisioner.AltAlanErisimBloku(db, sid)
	if kerr != nil {
		return "", fmt.Errorf("erisim kisitlama okunamadi, alt alan vhost'u DEGISTIRILMEDI (%s): %w", tamAd, kerr)
	}

	o := subVhostOpts{TamAd: tamAd, DocRoot: docroot, Socket: socket, Backend: backend,
		SSL: ssl, Crt: crt, Key: key, Koruma: koruma, N: ng, Kisit: kisit}
	return renderSubVhost(o), nil
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
		"ayarlar":    php.IstemciAyarlari(r, ayarlar), // yönetici dışı: kökün iç alanları çıkarılır
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
	// 🔴 KÖK SINIRI (php.IstegiCoz — domain ucuyla AYNI kapı): yönetici dışı oturum
	// kökün iç alanlarını (pm_max_children + türevleri) yazamaz, disable_functions /
	// open_basedir / session_save_path / mail_force_extra_parameters'ı değiştiremez
	// → 403, hiçbir şey yazılmaz; değerler alt alanın kaydından korunur. Eskiden alt
	// alan havuzu bunları AYNEN yazıyordu: müşteri disable_functions="" ile alt
	// alanda exec/system'i açabiliyor, pm_max_children ile planın işçi sayısını
	// slice dışında aşabiliyordu.
	mevcut, err := php.GetSub(r.Context(), h.DB, sid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayarlar okunamadı")
		return
	}
	surumIstek, ayarlar, kod, mesaj := php.IstegiCoz(r, mevcut)
	if kod != 0 {
		if kod == http.StatusBadRequest {
			mesaj = "geçersiz gövde"
		}
		httpx.WriteError(w, kod, mesaj)
		return
	}
	req := struct {
		PHPSurum string
		Ayarlar  php.Settings
	}{surumIstek, ayarlar}
	// güvenlik: skalar enjeksiyon + ek_direktifler allowlist (domain ile aynı sertleştirme)
	if err := php.ValidateSettings(req.Ayarlar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	temiz, err := php.SanitizeEk(req.Ayarlar.EkDirektifler)
	if err == nil {
		err = php.EkDirektifKapisi(r, req.Ayarlar.EkDirektifler) // müşteriye özgü ek yasaklar
	}
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
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "subdomain.php-ayar", id, err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "php_surum": surum, "ozel_havuz": true})
}
