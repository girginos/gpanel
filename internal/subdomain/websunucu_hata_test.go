package subdomain

// websunucu_hata_test.go — bayi kapsamı: PUT /domains/{id}/subdomain/{sid}/web-sunucu
// ek direktif doğrulaması ve vhost yeniden kurulumu hatalarında SUNUCU GENELİ
// `nginx -t` çıktısı (öteki kiracıların sertifika/vhost yolları) bayiye ve
// müşteriye ham dönmez. Bayi kendi direktif hatasını görmeye devam eder;
// yöneticinin metni birebir aynıdır.
//
// Dış etkili adımlar (webDirektifDogrula, webVhostYenidenKur) dikişlerle sahte —
// ana makinenin nginx'ine dokunulmaz. DB sahtedb. Gerçek RequireAuth + jetonlar.

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

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var ws_sir = []byte("subdomain-websunucu-test-anahtari-0123456")

func ws_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(ws_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(ws_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(ws_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

const ws_komsu = `nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [warn] "ssl_stapling" ignored, issuer certificate not found for certificate "/home/c_komsubayi/ssl/komsu.crt"`

const ws_basarisiz = "nginx: configuration file /etc/nginx/nginx.conf test failed"

var ws_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx", "dom_c_"}

func ws_cagir(t *testing.T, tok, ek string, dogrula, kur error) (int, string) {
	t.Helper()
	e1, e2 := webDirektifDogrula, webVhostYenidenKur
	t.Cleanup(func() { webDirektifDogrula, webVhostYenidenKur = e1, e2 })
	webDirektifDogrula = func(string) error { return dogrula }
	webVhostYenidenKur = func(context.Context, *sql.DB, int64, string, string, string, string) error { return kur }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, alan_adi, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0) FROM domains",
		[]any{"c_bayialan", "bayialan.test", "8.3", int64(0)})
	sd.Satirlar("SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar",
		[]any{"blog", "blog.bayialan.test", "8.3"})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(ws_sir)).Put("/domains/{id}/subdomain/{sid}/web-sunucu", (&Handlers{DB: db}).WebPut)
	govde, _ := json.Marshal(map[string]any{"backend": "static", "nginx": map[string]any{"ek_direktifler": ek}})
	req := httptest.NewRequest(http.MethodPut, "/domains/42/subdomain/5/web-sunucu", strings.NewReader(string(govde)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func ws_hataMetni(t *testing.T, govde string) string {
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

func ws_yabanciYok(t *testing.T, rol, govde string) {
	t.Helper()
	for _, iz := range ws_izler {
		if strings.Contains(govde, iz) {
			t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
		}
	}
}

// Ek direktif doğrulaması: bayinin KENDİ hatası görünür, aynı çıktıdaki komşu
// satırları görünmez; yönetici ham çıktıyı aynen görür.
func TestWebPut_DirektifHatasi_KendiHatasiKalirKomsuSizmaz(t *testing.T) {
	tok := ws_tokenlar(t)
	ek := "add_header X-T $tanimsiz_degisken;"
	ham := errors.New(ws_komsu + "\nnginx: [emerg] unknown \"tanimsiz_degisken\" variable\n" + ws_basarisiz)
	for _, rol := range []string{"bayi", "müşteri"} {
		kod, govde := ws_cagir(t, tok[rol], ek, ham, nil)
		if kod != http.StatusBadRequest {
			t.Fatalf("%s: 400 beklenirdi: %d %s", rol, kod, govde)
		}
		ws_yabanciYok(t, rol, govde)
		if m := ws_hataMetni(t, govde); !strings.HasPrefix(m, "nginx direktif hatası: ") ||
			!strings.Contains(m, `unknown "tanimsiz_degisken" variable`) {
			t.Errorf("%s: kendi direktif hatası görünür kalmalı: %q", rol, m)
		}
	}
	kod, govde := ws_cagir(t, tok["admin"], ek, ham, nil)
	if kod != http.StatusBadRequest || ws_hataMetni(t, govde) != "nginx direktif hatası: "+ham.Error() {
		t.Fatalf("yönetici ham ayrıntıyı aynen görmeli: %d %s", kod, govde)
	}
}

// Vhost yeniden kurulumu (render + `nginx -t`/`httpd -t` + reload) hatası: bayi ve
// müşteri genel mesaj görür; yönetici metni birebir aynı.
func TestWebPut_VhostHatasi_BayiMusteriyeHamCiktiDonmez(t *testing.T) {
	tok := ws_tokenlar(t)
	ham := fmt.Errorf("nginx -t başarısız: %s: %w",
		ws_komsu+"\nnginx: [emerg] unknown directive \"fastcgi_cahe\" in /etc/nginx/conf.d/dom_c_komsubayi.conf:12\n"+ws_basarisiz,
		errors.New("exit status 1"))
	for _, rol := range []string{"bayi", "müşteri"} {
		kod, govde := ws_cagir(t, tok[rol], "", nil, ham)
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		ws_yabanciYok(t, rol, govde)
		if m := ws_hataMetni(t, govde); !strings.Contains(m, "günlüğüne") {
			t.Errorf("%s: genel mesaj beklenirdi: %q", rol, m)
		}
	}
	kod, govde := ws_cagir(t, tok["admin"], "", nil, ham)
	if kod != http.StatusInternalServerError || ws_hataMetni(t, govde) != ham.Error() {
		t.Fatalf("yönetici ham ayrıntıyı aynen görmeli: %d %s", kod, govde)
	}
}
