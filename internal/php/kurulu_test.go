package php

// kurulu_test.go — PUT /php-settings: kurulu olmayan (desteklenen) sürüm 409 (eskiden 500 +
// iç ileti); per-tenant FPM'de alt alan kayıtları hosting sürümüne eşitlenir.

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
	"girginospanel/internal/provisioner"
	"girginospanel/internal/sahtedb"
)

func pk_istek(t *testing.T, tenantHata error, govde string) (int, string, *sahtedb.DB) {
	t.Helper()
	e1, e2 := tenantFPMAktif, tenantFPMUygula
	t.Cleanup(func() { tenantFPMAktif, tenantFPMUygula = e1, e2 })
	tenantFPMAktif = func(string) bool { return true }
	tenantFPMUygula = func(*sql.DB, int64, string, string) (string, error) {
		if tenantHata != nil {
			return "", tenantHata
		}
		return "/run/sahte-tenant.sock", nil
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
	return rec.Code, rec.Body.String(), sd
}

func TestPutAyarlar_KuruluDegil409(t *testing.T) {
	hata := fmt.Errorf("%w (8.6): php-fpm binary yok: /opt/remi/php86/root/usr/sbin/php-fpm", provisioner.ErrPHPKuruluDegil)
	// PutAyarlar kurulu olmayanı önceden 400 ile reddeder; bu dal birimin kendi denetimine karşı savunmadır.
	kod, govde, sd := pk_istek(t, hata, `{"php_surum":"8.2"}`)
	if kod != http.StatusConflict || !strings.Contains(govde, "kurulu değil") || strings.Contains(govde, "/opt/remi") {
		t.Errorf("409 + açık ileti beklenirdi: %d %s", kod, govde)
	}
	if sd.Yurutuldu("UPDATE domains") || sd.Yurutuldu("UPDATE subdomanlar") {
		t.Error("kurulu olmayan sürüm DB'ye yazıldı")
	}
}

func TestPutAyarlar_TenantFPM_AltAlanKayitlariEsitlenir(t *testing.T) {
	kod, govde, sd := pk_istek(t, nil, `{"php_surum":"8.2"}`)
	if kod != http.StatusOK {
		t.Fatalf("200 beklenirdi: %d %s", kod, govde)
	}
	if !sd.Yurutuldu("UPDATE subdomanlar SET php_surum") {
		t.Error("per-tenant FPM'de alt alan kayıtları hosting sürümüne eşitlenmeli")
	}
	// Paylaşılan havuz: alt alanların kendi havuzu olabilir, kayıtlarına dokunulmaz.
	if _, _, _, sd2 := ps_istek(t, false, `{"php_surum":"8.2"}`); sd2.Yurutuldu("UPDATE subdomanlar") {
		t.Error("paylaşılan havuzda alt alan kayıtları değişmemeli")
	}
}
