package domains

// hesap_aski_test.go — hesap askısı HTTP katmanı: tekil uç ve TOPLU işlem aynı
// tek yoldan (hesapaski.Uygula) geçer; kısmi başarısızlıkta "hesap askıda
// bırakıldı" bildirilir (ham adım çıktısı yalnız yöneticiye); bayi askıdayken
// tek tek askı/açma yok (toplu dahil); askıdaki hesapta PHP sürümü değişmez.
// Dış etkili adımlar sahte (askiAdimlari, phpSurumUygula) — DB sahtedb.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/hesapaski"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

// ha_adimlar — askı adımlarını sahteye çevirir; php adımı verilen hatayı döner.
func ha_adimlar(t *testing.T, phpHata error) *[]string {
	t.Helper()
	eski := askiAdimlari
	t.Cleanup(func() { askiAdimlari = eski })
	cagri := &[]string{}
	askiAdimlari = func() hesapaski.Adimlar {
		not := func(s string) { *cagri = append(*cagri, s) }
		return hesapaski.Adimlar{
			AnaVhost:    func(*sql.DB, int64) error { not("ana"); return nil },
			AltVhostlar: func(context.Context, *sql.DB, int64) error { not("alt"); return nil },
			AltHavuzlar: func(context.Context, *sql.DB, int64, string) ([]string, error) { return nil, nil },
			PHPHavuz: func(_ *sql.DB, _ int64, _, _ string, _ []string, askida bool) error {
				not(fmt.Sprintf("php(%v)", askida))
				return phpHata
			},
			SSHGiris:      func(string, bool) error { not("ssh"); return nil },
			DBKullanici:   func(*sql.DB, int64, bool) error { not("db"); return nil },
			CalismaZamani: func(string, bool) { not("cron") },
		}
	}
	return cagri
}

func ha_askiSatiri(sd *sahtedb.DB) {
	sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(php_surum",
		[]any{"bayialan.test", "c_bayialan", int64(0), "8.3", int64(0), ""})
}

var ha_askiyaAl = sh_durum{
	ad: "AskiyaAl", yontem: http.MethodPost, yol: "/domains/42/askiya-al", desen: "/domains/{id}/askiya-al",
	isle: func(h *Handlers) http.HandlerFunc { return h.AskiyaAl }, satirlar: ha_askiSatiri,
}

// Tekil uç hesabın TÜM adımlarını uygular (alt alan adları dahil).
func TestAskiyaAl_HesapDuzeyindeTekYol(t *testing.T) {
	cagri := ha_adimlar(t, nil)
	kod, govde := sh_cagir(t, ha_askiyaAl, sh_tokenlar(t)["admin"])
	if kod != http.StatusOK {
		t.Fatalf("200 beklenirdi: %d %s", kod, govde)
	}
	if got := strings.Join(*cagri, " "); got != "ana alt db php(true) ssh cron" {
		t.Fatalf("hesabın tüm adımları uygulanmalı: %s", got)
	}
}

// Kısmi başarısızlık: hesap askıda bırakıldığı söylenir; ham çıktı (komşu
// kiracı yolu) yalnız yöneticiye.
func TestAskiyaAl_KismiHata_AskidaBildirilir_HamCiktiYalnizYoneticiye(t *testing.T) {
	ha_adimlar(t, errors.New("php-fpm (php-fpm) yeniden yüklenemedi: /etc/nginx/conf.d/dom_c_komsubayi.conf"))
	tok := sh_tokenlar(t)
	for _, rol := range []string{"bayi", "admin"} {
		kod, govde := sh_cagir(t, ha_askiyaAl, tok[rol])
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		m := sh_hataMetni(t, govde)
		if !strings.HasPrefix(m, "işlem kısmen uygulandı, hesap askıda bırakıldı: ") {
			t.Fatalf("%s: hesabın askıda bırakıldığı bildirilmeli: %q", rol, m)
		}
		if ham := strings.Contains(m, "c_komsubayi"); ham != (rol == "admin") {
			t.Fatalf("%s: ham çıktı yalnız yöneticiye (ham=%v): %q", rol, ham, m)
		}
	}
}

// Bayi askıdayken hosting tek tek askıya alınamaz / açılamaz — TOPLU dahil
// (eskiden toplu işlem bu kuralı atlıyordu).
func TestAski_BayiAskidayken_TekilVeTopluReddedilir(t *testing.T) {
	cagri := ha_adimlar(t, nil)
	tok := sh_tokenlar(t)
	bayiAskida := func(sd *sahtedb.DB) {
		ha_askiSatiri(sd)
		sd.Satirlar("SELECT u.status FROM domains d JOIN users u", []any{"suspended"})
	}
	for _, d := range []sh_durum{
		{ad: "AskiyaAl", yontem: http.MethodPost, yol: "/domains/42/askiya-al", desen: "/domains/{id}/askiya-al",
			isle: func(h *Handlers) http.HandlerFunc { return h.AskiyaAl }, satirlar: bayiAskida},
		{ad: "AskidanAl", yontem: http.MethodPost, yol: "/domains/42/askidan-al", desen: "/domains/{id}/askidan-al",
			isle: func(h *Handlers) http.HandlerFunc { return h.AskidanAl }, satirlar: bayiAskida},
	} {
		if kod, govde := sh_cagir(t, d, tok["admin"]); kod != http.StatusConflict || !strings.Contains(govde, "bayi") {
			t.Fatalf("%s: 409 beklenirdi: %d %s", d.ad, kod, govde)
		}
	}
	sd, db := sahtedb.Yeni()
	bayiAskida(sd)
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sh_sir)).Post("/domains/toplu/durum", (&Handlers{DB: db}).TopluDurum)
	req := httptest.NewRequest(http.MethodPost, "/domains/toplu/durum", strings.NewReader(`{"ids":[42],"durum":"aktif"}`))
	req.Header.Set("Authorization", "Bearer "+tok["admin"])
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["atlanan"] != float64(1) || m["guncellenen"] != float64(0) {
		t.Fatalf("toplu açma bayi askıdayken atlanmalı: %s", rec.Body.String())
	}
	if len(*cagri) != 0 || sd.Yurutuldu("UPDATE domains SET askida") {
		t.Fatalf("reddedilen istekte hiçbir adım/yazma olmamalı: %v", *cagri)
	}
}

// Askıdaki hesapta PHP sürümü değişmez: havuz kapısı reddeder → 409 "hesap askıda".
func TestSetPHP_HesapAskida_Reddedilir(t *testing.T) {
	eski := phpSurumUygula
	t.Cleanup(func() { phpSurumUygula = eski })
	phpSurumUygula = func(string, string, string, string, string, string, string) (string, error) {
		return "", fmt.Errorf("%w — PHP havuzu askıdan alınınca yeniden başlar", hesaplar.ErrHesapAskida)
	}
	d := sh_durum{
		ad: "SetPHP", yontem: http.MethodPut, yol: "/domains/42/php", desen: "/domains/{id}/php", govde: `{"php_surum":"8.2"}`,
		isle: func(h *Handlers) http.HandlerFunc { return h.SetPHP },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(web_backend",
				[]any{"bayialan.test", "c_bayialan", int64(0), "php-fpm", "", "", ""})
		},
	}
	kod, govde := sh_cagir(t, d, sh_tokenlar(t)["bayi"])
	if kod != http.StatusConflict || !strings.Contains(sh_hataMetni(t, govde), "hesap askıda") {
		t.Fatalf("409 'hesap askıda' beklenirdi: %d %s", kod, govde)
	}
}
