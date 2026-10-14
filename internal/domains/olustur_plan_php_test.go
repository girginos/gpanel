package domains

// olustur_plan_php_test.go — gerçek VPS doğrulaması (2026-10-13) bulguları:
//   - kurulu olmayan PHP sürümü (8.6) → 409, yan etki/geri alma yok (eskiden 500 + iç yol +
//     yetim kullanıcı/home);
//   - kök anahtarla var olmayan / başka hostinge ÖZEL plan → 400, sağlamaya inilmez (eskiden
//     201 + kotasız hosting + sarkık plan_id);
//   - PUT /domains/{id}/plan gövdesinde plan_id yoksa 400 (eskiden plan sessizce kalkıyordu).
//
// saglama dikişle sahte; DB sahtedb (yardımcılar sunucu_hatasi_test.go'da, sh_*).

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/sahtedb"
)

var errSaglamayaIndi = errors.New("sağlamaya inildi")

// opp_olustur: POST /domains; saglama çağrılırsa hata döner ve sayılır.
func opp_olustur(t *testing.T, tok, govde string, hazirla func(*sahtedb.DB), saglamaHata error) (int, string, int) {
	t.Helper()
	eS, eG, eP := saglama, saglamaGeriAl, panelAdlariAl
	t.Cleanup(func() { saglama, saglamaGeriAl, panelAdlariAl = eS, eG, eP })
	cagri := 0
	saglama = func(string, string) (*provisioner.Result, error) {
		cagri++
		return nil, saglamaHata
	}
	saglamaGeriAl = func(string, string) error {
		t.Error("geri alma beklenmezdi (sağlama hiçbir şeye dokunmadı)")
		return nil
	}
	panelAdlariAl = func() []string { return nil }

	sd, db := sahtedb.Yeni()
	if hazirla != nil {
		hazirla(sd)
	}
	sd.Satirlar("SELECT max_domain, max_disk_mb, status FROM users", []any{int64(0), int64(0), "active"})
	sd.Satirlar("SELECT COUNT(*) FROM subdomanlar WHERE tam_ad=?", []any{int64(0)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sh_sir), middleware.AdminVeyaReseller).Post("/domains", (&Handlers{DB: db}).Create)
	req := httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if sd.Yurutuldu("INSERT INTO domains") {
		t.Errorf("domains satırı yazılmamalıydı: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Code, rec.Body.String(), cagri
}

func TestCreate_KuruluOlmayanPHP_409_YanEtkiYok(t *testing.T) {
	tok := sh_tokenlar(t)
	kurulu := fmt.Errorf("%w: PHP 8.6", provisioner.ErrPHPKuruluDegil)
	for _, rol := range []string{"admin", "bayi"} {
		kod, govde, n := opp_olustur(t, tok[rol], `{"alan_adi":"yeni86.test","php_surum":"8.6"}`, nil, kurulu)
		if kod != http.StatusConflict || n != 1 {
			t.Fatalf("%s: 409 beklenirdi: %d %s (sağlama %d)", rol, kod, govde, n)
		}
		if m := sh_hataMetni(t, govde); m != "PHP 8.6 bu sunucuda kurulu değil" {
			t.Errorf("%s: ileti %q", rol, m)
		}
	}
}

func TestCreate_KokOlmayanYaDaOzelPlan_400_SaglamayaInilmez(t *testing.T) {
	tok := sh_tokenlar(t)
	for _, pid := range []string{"99999", "0", "-1"} {
		kod, govde, n := opp_olustur(t, tok["admin"], `{"alan_adi":"plansiz.test","plan_id":`+pid+`}`, nil, errSaglamayaIndi)
		if kod != http.StatusBadRequest || n != 0 || sh_hataMetni(t, govde) != "plan bulunamadı" {
			t.Errorf("plan_id=%s: 400 'plan bulunamadı' beklenirdi: %d %s (sağlama %d)", pid, kod, govde, n)
		}
	}
	// Başka hostinge ÖZEL plan (domain_id dolu) yeni hostinge bağlanamaz.
	ozel := func(sd *sahtedb.DB) { sd.Satirlar("SELECT domain_id FROM service_plans WHERE id=?", []any{int64(42)}) }
	kod, govde, n := opp_olustur(t, tok["admin"], `{"alan_adi":"ozel.test","plan_id":5}`, ozel, errSaglamayaIndi)
	if kod != http.StatusBadRequest || n != 0 {
		t.Errorf("özel plan: 400 beklenirdi: %d %s (sağlama %d)", kod, govde, n)
	}
	// Var olan KATALOG planı (domain_id NULL): kapıdan geçer, sağlamaya iner.
	katalog := func(sd *sahtedb.DB) { sd.Satirlar("SELECT domain_id FROM service_plans WHERE id=?", []any{nil}) }
	if kod, govde, n := opp_olustur(t, tok["admin"], `{"alan_adi":"katalog.test","plan_id":3}`, katalog, errSaglamayaIndi); n != 1 {
		t.Errorf("katalog planı sağlamaya inmeliydi: %d %s", kod, govde)
	}
	// plan_id verilmezse (varsayılan plan yok) eski davranış: plansız sağlama.
	if kod, govde, n := opp_olustur(t, tok["admin"], `{"alan_adi":"varsayilan.test"}`, nil, errSaglamayaIndi); n != 1 {
		t.Errorf("plan_id'siz istek sağlamaya inmeliydi: %d %s", kod, govde)
	}
}

func TestSetPlan_PlanIDZorunlu_AcikNullYoneticide(t *testing.T) {
	tok := sh_tokenlar(t)
	kos := func(tk, govde string) (int, string, *sahtedb.DB) {
		sd, db := sahtedb.Yeni()
		r := chi.NewRouter()
		r.With(middleware.RequireAuth(sh_sir), middleware.AdminVeyaReseller).Put("/domains/{id}/plan", (&Handlers{DB: db}).SetPlan)
		req := httptest.NewRequest(http.MethodPut, "/domains/42/plan", strings.NewReader(govde))
		req.Header.Set("Authorization", "Bearer "+tk)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String(), sd
	}
	for _, g := range []string{`{}`, `{"planid":2}`, `{"plan":2}`} {
		kod, govde, sd := kos(tok["admin"], g)
		if kod != http.StatusBadRequest || !strings.Contains(govde, "plan_id zorunlu") {
			t.Errorf("%s: 400 'plan_id zorunlu' beklenirdi: %d %s", g, kod, govde)
		}
		if sd.Yurutuldu("UPDATE domains") {
			t.Errorf("%s: plan değişmemeliydi", g)
		}
	}
	for _, g := range []string{`{"plan_id":"2"}`, `[]`, `x`} {
		if kod, govde, _ := kos(tok["admin"], g); kod != http.StatusBadRequest {
			t.Errorf("%s: 400 beklenirdi: %d %s", g, kod, govde)
		}
	}
	// Açık null: bayi 403 (değişmedi); yönetici kapıdan geçer (plan kaldırma yolu).
	if kod, govde, _ := kos(tok["bayi"], `{"plan_id":null}`); kod != http.StatusForbidden {
		t.Errorf("bayi null: 403 beklenirdi: %d %s", kod, govde)
	}
	if kod, govde, _ := kos(tok["admin"], `{"plan_id":null}`); kod == http.StatusBadRequest {
		t.Errorf("yönetici açık null: zorunluluk kapısından geçmeliydi: %d %s", kod, govde)
	}
}
