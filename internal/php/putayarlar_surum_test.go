package php

// putayarlar_surum_test.go — PUT /domains/{id}/php-settings (UI yolu) php_surum
// doğrulaması: POST /domains ve PUT /domains/{id}/php ile AYNI doğrulayıcı
// (provisioner.PHPSurumDogrula). Geçersiz değer 400 + desteklenen liste; hiçbir yan
// etki (havuz/birim/vhost/DB yazımı) olmaz. Boş değer sürüme dokunmaz.
// Dış etkili adımlar dikişlerle sahte, DB sahtedb (bkz. putayarlar_hata_test.go).

import (
	"database/sql"
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

type psIz struct {
	havuz, vhost, tenant []string // çağrıldığı sürümler
}

// ps_istek: PutAyarlar'ı çalıştırır (domain 42, DB'deki sürüm 8.3). tenant=true → per-tenant yol.
func ps_istek(t *testing.T, tenant bool, govde string) (int, string, *psIz, *sahtedb.DB) {
	t.Helper()
	iz := &psIz{}
	e1, e2, e3, e4 := tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula
	t.Cleanup(func() { tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula = e1, e2, e3, e4 })
	tenantFPMAktif = func(string) bool { return tenant }
	tenantFPMUygula = func(_ *sql.DB, _ int64, _, surum string) (string, error) {
		iz.tenant = append(iz.tenant, surum)
		return "/run/sahte-tenant.sock", nil
	}
	havuzUygula = func(_ string, surum string, _ Settings) (string, error) {
		iz.havuz = append(iz.havuz, surum)
		return "/run/sahte.sock", nil
	}
	vhostUygula = func(_ *sql.DB, _ int64, _ string, surum string) error {
		iz.vhost = append(iz.vhost, surum)
		return nil
	}

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, php_surum, is_demo FROM domains", []any{"c_bayialan", "8.3", int64(0)})
	tok, err := auth.IssueAt(pa_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(pa_sir)).Put("/domains/{id}/php-settings", (&Handlers{DB: db}).PutAyarlar)
	req := httptest.NewRequest(http.MethodPut, "/domains/42/php-settings", strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), iz, sd
}

func TestPutAyarlar_PHPSurum_GecersizDeger400_YanEtkisiz(t *testing.T) {
	for _, tenant := range []bool{false, true} {
		for _, v := range []string{"8.30", "php8.9", "9.9", "8.33", "8,3", "8.3;rm -rf /", "../8.3", "null", "   "} {
			t.Run(fmt.Sprintf("tenant=%v/%s", tenant, v), func(t *testing.T) {
				kod, govde, iz, sd := ps_istek(t, tenant, fmt.Sprintf(`{"php_surum":%q,"ayarlar":{}}`, v))
				if kod != http.StatusBadRequest {
					t.Fatalf("400 beklenir: %d %s", kod, govde)
				}
				if !strings.Contains(govde, "7.4") || !strings.Contains(govde, "8.6") {
					t.Errorf("yanıt desteklenen sürümleri listelemeli: %s", govde)
				}
				if len(iz.havuz)+len(iz.vhost)+len(iz.tenant) != 0 {
					t.Errorf("geçersiz istekte havuz/vhost/birim çağrıldı: %+v", iz)
				}
				if sd.Yurutuldu("") { // hiçbir Exec (ayar kaydı, php_surum güncellemesi) olmamalı
					t.Errorf("geçersiz istek DB'ye yazdı")
				}
			})
		}
	}
}

// Geçerli değer kanonik (kırpılmış) hâliyle uygulanır ve domains.php_surum'a yazılır;
// boş (yok / "") değer sürüme dokunmaz (DB'deki 8.3 ile uygulanır, php_surum UPDATE'i yok).
// Yalnız boşluktan oluşan değer BOŞ DEĞİL, geçersizdir (400) — eskiden de reddedilirdi.
func TestPutAyarlar_PHPSurum_GecerliVeBos(t *testing.T) {
	for _, tenant := range []bool{false, true} {
		t.Run(fmt.Sprintf("geçerli tenant=%v", tenant), func(t *testing.T) {
			kod, govde, iz, sd := ps_istek(t, tenant, `{"php_surum":" 8.2 ","ayarlar":{}}`)
			if kod != http.StatusOK || !strings.Contains(govde, `"php_surum":"8.2"`) {
				t.Fatalf("200 + kanonik 8.2 beklenir: %d %s", kod, govde)
			}
			uygulanan := iz.havuz
			if tenant {
				uygulanan = iz.tenant
			}
			if len(uygulanan) != 1 || uygulanan[0] != "8.2" {
				t.Errorf("8.2 uygulanmalı: %+v", iz)
			}
			if !sd.Yurutuldu("UPDATE domains SET php_surum=? WHERE id=? | 8.2") {
				t.Errorf("domains.php_surum 8.2 yazılmalı")
			}
		})
		t.Run(fmt.Sprintf("boş tenant=%v", tenant), func(t *testing.T) {
			for _, g := range []string{`{"ayarlar":{}}`, `{"php_surum":"","ayarlar":{}}`} {
				kod, govde, iz, sd := ps_istek(t, tenant, g)
				if kod != http.StatusOK {
					t.Fatalf("%s: 200 beklenir: %d %s", g, kod, govde)
				}
				uygulanan := iz.havuz
				if tenant {
					uygulanan = iz.tenant
				}
				if len(uygulanan) != 1 || uygulanan[0] != "8.3" {
					t.Errorf("%s: DB'deki 8.3 ile uygulanmalı: %+v", g, iz)
				}
				if sd.Yurutuldu("UPDATE domains SET php_surum") {
					t.Errorf("%s: sürüm verilmediyse domains.php_surum yazılmamalı", g)
				}
			}
		})
	}
}
