package domains

// olustur_saglama_hata_test.go — bayi kapsamı (son ham hata yerleri): POST
// /domains (AdminVeyaReseller) sağlama hatası. provisioner.Provision SUNUCU
// GENELİ `nginx -t` / `php-fpm -t` / reload çalıştırır; çıktısı (komşu
// kiracıların sertifika/vhost yolları) bayiye ham DÖNMEZ. Yöneticinin gördüğü
// metin birebir aynıdır.
//
// saglama dikişle sahte (useradd/nginx/php-fpm'e ulaşılmaz), panel adları boş,
// DB sahtedb. Yardımcılar sunucu_hatasi_test.go'da (sh_*).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/sahtedb"
)

func os_olustur(t *testing.T, tok string) (int, string) {
	t.Helper()
	eS, eG, eP := saglama, saglamaGeriAl, panelAdlariAl
	t.Cleanup(func() { saglama, saglamaGeriAl, panelAdlariAl = eS, eG, eP })
	saglama = func(string, string) (*provisioner.Result, error) { return nil, sh_ham }
	saglamaGeriAl = func(string, string) error {
		t.Error("sağlama hatasında geri alma beklenmezdi")
		return nil
	}
	panelAdlariAl = func() []string { return nil }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT max_domain, max_disk_mb, status FROM users", []any{int64(0), int64(0), "active"})
	sd.Satirlar("SELECT COUNT(*) FROM subdomanlar WHERE tam_ad=?", []any{int64(0)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sh_sir), middleware.AdminVeyaReseller).Post("/domains", (&Handlers{DB: db}).Create)
	req := httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(`{"alan_adi":"yenibayi.test"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestCreate_SaglamaHatasi_BayiyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	tok := sh_tokenlar(t)
	const onek = "sağlama başarısız: "
	kod, govde := os_olustur(t, tok["bayi"])
	if kod != http.StatusInternalServerError {
		t.Fatalf("bayi: 500 beklenirdi: %d %s", kod, govde)
	}
	for _, iz := range sh_izler {
		if strings.Contains(govde, iz) {
			t.Errorf("bayi yanıtında yabancı iz %q var: %s", iz, govde)
		}
	}
	if m := sh_hataMetni(t, govde); !strings.HasPrefix(m, onek) || !strings.Contains(m, "günlüğüne") {
		t.Errorf("bayi: genel mesaj (%q … günlüğüne …) beklenirdi: %q", onek, m)
	}
	kod, govde = os_olustur(t, tok["admin"])
	if kod != http.StatusInternalServerError {
		t.Fatalf("yönetici: 500 beklenirdi: %d %s", kod, govde)
	}
	if m, want := sh_hataMetni(t, govde), onek+sh_ham.Error(); m != want {
		t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, want)
	}
}
