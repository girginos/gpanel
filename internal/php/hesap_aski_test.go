package php

// hesap_aski_test.go — askıdaki hesapta PHP ayarı KAYDEDİLMEZ ve havuz
// BAŞLATILMAZ (409 "hesap askıda"); askı durumu tek kaynaktan (hesaplar.HesapAskida)
// okunur. Dış etkili adımlar sahte; DB sahtedb.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

func TestPutAyarlar_HesapAskida_KaydetmezBaslatmaz(t *testing.T) {
	e1, e2, e3, e4 := tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula
	t.Cleanup(func() { tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula = e1, e2, e3, e4 })
	uygulandi := 0
	tenantFPMAktif = func(string) bool { return false }
	tenantFPMUygula = func(*sql.DB, int64, string, string) (string, error) { uygulandi++; return "", nil }
	havuzUygula = func(string, string, Settings) (string, error) { uygulandi++; return "", nil }
	vhostUygula = func(*sql.DB, int64, string, string) error { uygulandi++; return nil }
	tok, err := auth.IssueAt(pa_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, askida := range []bool{true, false} {
		sd, db := sahtedb.Yeni()
		sd.Satirlar("SELECT sistem_kullanici, php_surum, is_demo FROM domains", []any{"c_bayialan", "8.3", int64(0)})
		if askida {
			sd.Satirlar("FROM domains WHERE sistem_kullanici=? AND COALESCE(askida,0)=1", []any{int64(1)})
		}
		r := chi.NewRouter()
		r.With(middleware.RequireAuth(pa_sir)).Put("/domains/{id}/php-settings", (&Handlers{DB: db}).PutAyarlar)
		req := httptest.NewRequest(http.MethodPut, "/domains/42/php-settings", strings.NewReader(`{"ayarlar":{}}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		uygulandi = 0
		r.ServeHTTP(rec, req)
		if askida {
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "hesap askıda") {
				t.Fatalf("askıda 409 'hesap askıda' beklenirdi: %d %s", rec.Code, rec.Body.String())
			}
			if uygulandi != 0 || sd.Yurutuldu("php_settings") {
				t.Fatalf("askıda ayar kaydedilmemeli / havuz başlatılmamalı (uygulandı=%d)", uygulandi)
			}
			continue
		}
		if rec.Code != http.StatusOK || uygulandi == 0 || !sd.Yurutuldu("php_settings") {
			t.Fatalf("askıda olmayan hesapta ayar kaydedilip uygulanmalı: %d %s", rec.Code, rec.Body.String())
		}
	}
}
