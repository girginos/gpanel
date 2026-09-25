package php

// putayarlar_hata_test.go — F1 (aynı kök neden): PHP ayar kaydında pool/vhost
// uygulama hataları ham `nginx -t` / `systemctl` çıktısını (öteki kiracıların
// sertifika/vhost yolları, paylaşılan servis durumu) bayiye/müşteriye döndürmez;
// yönetici metni birebir aynı görür. Dış etkili adımlar dikişlerle sahteye
// çevrilir (tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula) — test
// ana makinenin php-fpm/nginx yapılandırmasına dokunmaz. DB sahtedb.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var pa_sir = []byte("php-putayarlar-hata-test-anahtari-012345")

// pa_nginxT — sunucu geneli `nginx -t` çıktısı (komşu kiracıların izleriyle).
var pa_nginxT = fmt.Errorf("nginx -t başarısız: %s: %w",
	`nginx: [warn] "ssl_stapling" ignored, issuer certificate not found for certificate "/home/c_girginos_app/ssl/girginos.app.crt"`+
		"\nnginx: [emerg] unknown directive \"x\" in /etc/nginx/conf.d/dom_c_komsu.conf:12\nnginx: configuration file /etc/nginx/nginx.conf test failed",
	errors.New("exit status 1"))

// pa_fpm — paylaşılan php-fpm servisinin reload hatası (sunucu geneli servis durumu).
var pa_fpm = fmt.Errorf("php-fpm reload (%s): %s: %w", "php-fpm",
	`Job for php-fpm.service failed because the control process exited with error code. See "systemctl status php-fpm.service" and "journalctl -xeu php-fpm.service" for details.`,
	errors.New("exit status 1"))

var pa_yabanciIzler = []string{"c_girginos_app", "dom_c_komsu", "/etc/nginx", "php-fpm.service", "journalctl"}

type pa_senaryo struct {
	tenant                        bool
	tenantHata, havuzHata, vhHata error
}

func pa_kaydet(t *testing.T, tok string, s pa_senaryo) (int, string) {
	t.Helper()
	e1, e2, e3, e4 := tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula
	t.Cleanup(func() { tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula = e1, e2, e3, e4 })
	tenantFPMAktif = func(string) bool { return s.tenant }
	tenantFPMUygula = func(*sql.DB, int64, string, string) (string, error) { return "/run/sahte.sock", s.tenantHata }
	havuzUygula = func(string, string, Settings) (string, error) { return "/run/sahte.sock", s.havuzHata }
	vhostUygula = func(*sql.DB, int64, string, string) error { return s.vhHata }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, php_surum, is_demo FROM domains", []any{"c_bayialan", "8.3", int64(0)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(pa_sir)).Put("/domains/{id}/php-settings", (&Handlers{DB: db}).PutAyarlar)
	req := httptest.NewRequest(http.MethodPut, "/domains/42/php-settings", strings.NewReader(`{"ayarlar":{}}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestPutAyarlar_UygulamaHatasi_BayiMusteriyeHamCiktiDonmez(t *testing.T) {
	admin, err := auth.IssueAt(pa_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(pa_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(pa_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	senaryolar := []struct {
		ad   string
		s    pa_senaryo
		onek string
		ham  error
	}{
		{"paylaşılan pool → vhost (nginx -t)", pa_senaryo{vhHata: pa_nginxT}, "nginx vhost: ", pa_nginxT},
		{"paylaşılan pool yazımı (php-fpm reload)", pa_senaryo{havuzHata: pa_fpm}, "pool yaz: ", pa_fpm},
		{"per-tenant fpm (nginx -t)", pa_senaryo{tenant: true, tenantHata: pa_nginxT}, "per-tenant fpm: ", pa_nginxT},
	}
	for _, sn := range senaryolar {
		for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
			kod, govde := pa_kaydet(t, tok, sn.s)
			if kod != http.StatusInternalServerError {
				t.Fatalf("%s / %s: 500 beklenirdi: %d %s", sn.ad, rol, kod, govde)
			}
			for _, iz := range pa_yabanciIzler {
				if strings.Contains(govde, iz) {
					t.Errorf("%s / %s: yanıtta sunucu geneli iz %q var: %s", sn.ad, rol, iz, govde)
				}
			}
			var m map[string]string
			_ = json.Unmarshal([]byte(govde), &m)
			if !strings.HasPrefix(m["hata"], sn.onek) || !strings.Contains(m["hata"], "günlüğüne") {
				t.Errorf("%s / %s: önek korunmalı + genel mesaj: %q", sn.ad, rol, m["hata"])
			}
		}
		kod, govde := pa_kaydet(t, admin, sn.s)
		var m map[string]string
		_ = json.Unmarshal([]byte(govde), &m)
		if kod != http.StatusInternalServerError || m["hata"] != sn.onek+sn.ham.Error() {
			t.Errorf("%s: yönetici ham ayrıntıyı aynen görmeli: %d %s", sn.ad, kod, govde)
		}
	}
}
