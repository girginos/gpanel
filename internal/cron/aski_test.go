package cron

// aski_test.go — hesap askısı: askıdaki hesabın görev deposu askı deposuna
// taşınır (AskiUygula); panel askıda depoya yazmaz ve görev çalıştırmaz (409).
// DB: sahtedb (bellek içi).
//
// 🔴 Güvenlik: istekler YAZMA yoluna inemeyecek biçimde kurulur (var olmayan
// hesap adı → boş liste → dizin dışı/400); ayrıca TestMain tüm yolları geçici
// dizinlere çevirir — test canlı /etc/cron.d ya da depoya dokunamaz.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/sahtedb"
)

const askiTestSK = "c_askitestyok"

func askiIstek(t *testing.T, askida bool, fn func(*Handlers) http.HandlerFunc, yontem, govde string) *httptest.ResponseRecorder {
	t.Helper()
	sd, db := sahtedb.Yeni()
	t.Cleanup(func() { _ = db.Close() })
	if askida {
		sd.Satirlar("COALESCE(askida,0)=1", []any{int64(1)})
	}
	sd.Satirlar("SELECT sistem_kullanici, is_demo FROM domains", []any{askiTestSK, int64(0)})
	r := httptest.NewRequest(yontem, "/domains/7/cron/0", strings.NewReader(govde))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "7")
	rctx.URLParams.Add("idx", "0")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(&Handlers{DB: db})(w, r)
	return w
}

func TestCron_AskidakiHesapta409(t *testing.T) {
	uclar := map[string]struct {
		fn            func(*Handlers) http.HandlerFunc
		yontem, govde string
		acikKod       int // askıda değilken (kapı geçilince) beklenen kod
	}{
		"Create":   {func(h *Handlers) http.HandlerFunc { return h.Create }, http.MethodPost, "{", http.StatusBadRequest},
		"Update":   {func(h *Handlers) http.HandlerFunc { return h.Update }, http.MethodPut, "{", http.StatusBadRequest},
		"Delete":   {func(h *Handlers) http.HandlerFunc { return h.Delete }, http.MethodDelete, "", http.StatusNotFound},
		"Calistir": {func(h *Handlers) http.HandlerFunc { return h.Calistir }, http.MethodPost, "", http.StatusNotFound},
	}
	for ad, u := range uclar {
		if w := askiIstek(t, true, u.fn, u.yontem, u.govde); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "hesap askıda") {
			t.Errorf("%s: askıdaki hesapta 409 bekleniyordu, %d: %s", ad, w.Code, w.Body.String())
		}
		if w := askiIstek(t, false, u.fn, u.yontem, u.govde); w.Code != u.acikKod {
			t.Errorf("%s: askıda olmayan hesapta kapı engellememeli, %d: %s", ad, w.Code, w.Body.String())
		}
	}
	// Liste askıda da okunur (salt okuma).
	if w := askiIstek(t, true, func(h *Handlers) http.HandlerFunc { return h.List }, http.MethodGet, ""); w.Code != http.StatusOK {
		t.Errorf("List askıda da 200 olmalı: %d %s", w.Code, w.Body.String())
	}
}
