package plans

// direktif_hata_test.go — bayi kapsamı (son ham hata yerleri): plan oluşturma /
// güncellemede nginx ek direktif doğrulaması SUNUCU GENELİ `nginx -t` çalıştırır;
// çıktısı (öteki kiracıların sertifika/vhost yolları) bayiye ham DÖNMEZ. Bayi
// yalnız KENDİ direktiflerine ait [emerg] satırını görür (düzeltebilsin diye);
// yöneticinin gördüğü metin birebir aynıdır.
//
// direktifDogrula dikişle sahte — test /etc/nginx/conf.d'ye geçici dosya yazmaz,
// nginx çalıştırmaz. DB sahtedb (doğrulama DB'den ÖNCE ya da kapı sorgusundan
// sonra döner).

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var dh_sir = []byte("plans-direktif-hata-test-anahtari-012345")

// ValidateNginxDirectives çıktısı biçiminde: sunucu geneli uyarılar + kiracının
// geçici bloğundaki kendi hatası ("(direktifler)" konumlu).
const dh_ham = `nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [warn] "ssl_stapling" ignored, issuer certificate not found for certificate "/home/c_komsubayi/ssl/komsu.crt"
nginx: [emerg] unknown directive "fastcgi_cahe" in (direktifler):7
nginx: configuration file /etc/nginx/nginx.conf test failed`

var dh_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx"}

func dh_cagir(t *testing.T, kim, yontem, yol string) (int, string) {
	t.Helper()
	eski := direktifDogrula
	t.Cleanup(func() { direktifDogrula = eski })
	direktifDogrula = func(string) error { return errors.New(dh_ham) }

	var tok string
	var err error
	if kim == "admin" {
		tok, err = auth.IssueAt(dh_sir, 3600, 1, "root", "admin", 0, "")
	} else {
		tok, err = auth.IssueResellerAt(dh_sir, 3600, 7, "bayi7", 7, 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	sd, db := sahtedb.Yeni()
	// Update kapısı (bayi): plan 3 bayinin, yabancı hostinge bağlı değil.
	sd.Satirlar("SELECT reseller_id, domain_id FROM service_plans", []any{int64(7), nil})
	sd.Satirlar("SELECT COUNT(*) FROM domains WHERE plan_id=? AND reseller_id<>?", []any{int64(0)})
	// Update mevcut satırı okur (kök alanları korunur — bayi_plan.go); bayi havuzu sınırsız.
	sd.Satirlar("reseller_id, domain_id FROM service_plans WHERE id=?", []any{int64(3), "bayi-plan", "", int64(1024), int64(1024),
		int64(1), int64(1), int64(0), int64(0), int64(0), int64(1), int64(100), int64(512), int64(50), int64(50000), int64(100), int64(25),
		int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), "8.3", int64(0), int64(64), "",
		int64(0), "on", int64(1), int64(0), "2026-10-03", int64(7), nil})
	sd.Satirlar("SELECT max_disk_mb, max_trafik_mb, fazla_satis FROM users", []any{int64(0), int64(0), int64(0)})
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(dh_sir), middleware.AdminVeyaReseller).Post("/plans", h.Create)
	r.With(middleware.RequireAuth(dh_sir), middleware.AdminVeyaReseller).Put("/plans/{id}", h.Update)
	govde, _ := json.Marshal(map[string]any{"ad": "bayi-plan", "disk_kota_mb": 1024, "trafik_kota_mb": 1024,
		"nginx_ek_direktifler": "fastcgi_cahe on;"})
	req := httptest.NewRequest(yontem, yol, strings.NewReader(string(govde)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func dh_hataMetni(t *testing.T, govde string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(govde), &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, govde)
	}
	for _, k := range []string{"error", "hata", "mesaj"} {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	t.Fatalf("yanıtta hata metni yok: %s", govde)
	return ""
}

func TestPlanDirektifHatasi_BayiKendiSatiriniGorur_KomsuSizmaz_YoneticiAyni(t *testing.T) {
	const onek = "nginx direktif doğrulaması başarısız:\n"
	for _, u := range []struct{ ad, yontem, yol string }{
		{"Create", http.MethodPost, "/plans"},
		{"Update", http.MethodPut, "/plans/3"},
	} {
		t.Run(u.ad, func(t *testing.T) {
			kod, govde := dh_cagir(t, "bayi", u.yontem, u.yol)
			if kod != http.StatusBadRequest {
				t.Fatalf("bayi: 400 beklenirdi: %d %s", kod, govde)
			}
			for _, iz := range dh_izler {
				if strings.Contains(govde, iz) {
					t.Errorf("bayi yanıtında yabancı iz %q var: %s", iz, govde)
				}
			}
			if m := dh_hataMetni(t, govde); !strings.HasPrefix(m, onek) ||
				!strings.Contains(m, `unknown directive "fastcgi_cahe" in (direktifler):7`) {
				t.Errorf("bayi kendi direktif hatasını görmeli: %q", m)
			}
			kod, govde = dh_cagir(t, "admin", u.yontem, u.yol)
			if kod != http.StatusBadRequest {
				t.Fatalf("yönetici: 400 beklenirdi: %d %s", kod, govde)
			}
			if m := dh_hataMetni(t, govde); m != onek+dh_ham {
				t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, onek+dh_ham)
			}
		})
	}
}
