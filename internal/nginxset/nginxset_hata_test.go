package nginxset

// nginxset_hata_test.go — F1: nginx-settings kaydında `nginx -t` çıktısı
// (SUNUCU GENELİ: öteki kiracıların sertifika/vhost yolları) bayiye/müşteriye
// ham dönmez; yönetici ham ayrıntıyı görmeye devam eder (metin birebir aynı).
//
// Sunucu geneli `nginx -t` ve vhost yazımı dikişlerle (direktifDogrula,
// vhostUygula) sahteye çevrilir — test ana makinenin nginx'ine dokunmaz. DB sahtedb.

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

var nt_sir = []byte("nginxset-hata-test-anahtari-0123456789ab")

func nt_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(nt_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(nt_sir, 3600, 7, "bayi7", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(nt_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

// Gerçek `nginx -t` çıktısı biçiminde (181 error.log'daki satırların eşi):
// sunucu genelindeki ssl_stapling her SSL'li alan adı için uyarı basar.
const nt_komsuUyarilar = `nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/test2.girginos.app/test2.girginos.app.crt"
nginx: [warn] "ssl_stapling" ignored, issuer certificate not found for certificate "/home/c_girginos_app/ssl/girginos.app.crt"`

const nt_testBasarisiz = "nginx: configuration file /etc/nginx/nginx.conf test failed"

// Yanıtta GÖRÜNMEMESİ gereken yabancı / sunucu geneli izler.
var nt_yabanciIzler = []string{"test2.girginos.app", "c_girginos_app", "/etc/pki", "dom_c_komsu", "/etc/nginx"}

func nt_yabanciYok(t *testing.T, rol, govde string) {
	t.Helper()
	for _, iz := range nt_yabanciIzler {
		if strings.Contains(govde, iz) {
			t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
		}
	}
}

// nt_kaydet — PUT /domains/42/nginx-settings. Dikişler: doğrulama dogrulaHata'yı,
// vhost uygulama uygulaHata'yı döner.
func nt_kaydet(t *testing.T, tok, ek string, dogrulaHata, uygulaHata error) (int, string) {
	t.Helper()
	eskiD, eskiU := direktifDogrula, vhostUygula
	t.Cleanup(func() { direktifDogrula, vhostUygula = eskiD, eskiU })
	direktifDogrula = func(string) error { return dogrulaHata }
	vhostUygula = func(*sql.DB, int64, string, string) error { return uygulaHata }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT php_surum, sistem_kullanici FROM domains", []any{"8.3", "c_bayialan"})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(nt_sir)).Put("/domains/{id}/nginx-settings", (&Handlers{DB: db}).Kaydet)
	govde, _ := json.Marshal(map[string]any{"ayarlar": map[string]any{"ek_direktifler": ek}})
	req := httptest.NewRequest(http.MethodPut, "/domains/42/nginx-settings", strings.NewReader(string(govde)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func nt_hataMetni(t *testing.T, govde string) string {
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

// Vhost uygulama (renderAndReload) hatası: ham `nginx -t` çıktısı bayiye/müşteriye dönmez.
func TestKaydet_VhostHatasi_BayiMusteriyeHamCiktiDonmez(t *testing.T) {
	admin, bayi, musteri := nt_tokenlar(t)
	ham := fmt.Errorf("nginx -t başarısız: %s: %w",
		nt_komsuUyarilar+"\nnginx: [emerg] unknown directive \"fastcgi_cahe\" in /etc/nginx/conf.d/dom_c_komsu.conf:12\n"+nt_testBasarisiz,
		errors.New("exit status 1"))
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
		kod, govde := nt_kaydet(t, tok, "", nil, ham)
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		nt_yabanciYok(t, rol, govde)
		if m := nt_hataMetni(t, govde); !strings.HasPrefix(m, "vhost: ") || !strings.Contains(m, "günlüğüne") {
			t.Errorf("%s: genel mesaj ('vhost: … ayrıntı sunucu günlüğüne …') beklenirdi: %q", rol, m)
		}
	}
	// Yönetici: metin öncekiyle BİREBİR aynı (ayrıntı dahil).
	kod, govde := nt_kaydet(t, admin, "", nil, ham)
	if kod != http.StatusInternalServerError || nt_hataMetni(t, govde) != "vhost: "+ham.Error() {
		t.Fatalf("yönetici ham ayrıntıyı aynen görmeli: %d %s", kod, govde)
	}
}

// Ek direktif doğrulaması: bayinin KENDİ hatası (bilinmeyen değişken) görünür kalır,
// aynı çıktıdaki komşu satırları görünmez. (Bayi bunu İSTEDİĞİ AN tetikleyebilir:
// `add_header X $tanimsiz;` → sunucu geneli çıktı.)
func TestKaydet_DirektifHatasi_KendiHatasiKalirKomsuSizmaz(t *testing.T) {
	admin, bayi, musteri := nt_tokenlar(t)
	ek := "add_header X-T $tanimsiz_degisken;"
	ham := errors.New(nt_komsuUyarilar + "\nnginx: [emerg] unknown \"tanimsiz_degisken\" variable\n" + nt_testBasarisiz)
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
		kod, govde := nt_kaydet(t, tok, ek, ham, nil)
		if kod != http.StatusBadRequest {
			t.Fatalf("%s: 400 beklenirdi: %d %s", rol, kod, govde)
		}
		nt_yabanciYok(t, rol, govde)
		if m := nt_hataMetni(t, govde); !strings.Contains(m, `unknown \"tanimsiz_degisken\" variable`) &&
			!strings.Contains(m, `unknown "tanimsiz_degisken" variable`) {
			t.Errorf("%s: kendi direktif hatası görünür kalmalı: %q", rol, m)
		}
	}
	kod, govde := nt_kaydet(t, admin, ek, ham, nil)
	if kod != http.StatusBadRequest || nt_hataMetni(t, govde) != "geçersiz nginx direktifi: "+ham.Error() {
		t.Fatalf("yönetici ham ayrıntıyı aynen görmeli: %d %s", kod, govde)
	}
}

// Konumlu kendi hatası ("(direktifler):N") görünür; komşunun küresel bozulması yalnız
// genel mesaj üretir (komşunun dosya yolu/adı sızmaz).
func TestKaydet_DirektifHatasi_KonumluVeKomsuBozuklugu(t *testing.T) {
	_, bayi, _ := nt_tokenlar(t)
	kendi := errors.New(nt_komsuUyarilar + "\nnginx: [emerg] unknown directive \"add_heade\" in (direktifler):7\n" + nt_testBasarisiz)
	kod, govde := nt_kaydet(t, bayi, "add_heade X 1;", kendi, nil)
	if kod != http.StatusBadRequest {
		t.Fatalf("400 beklenirdi: %d %s", kod, govde)
	}
	nt_yabanciYok(t, "bayi", govde)
	if m := nt_hataMetni(t, govde); !strings.Contains(m, "(direktifler):7") {
		t.Errorf("kendi konumlu hatası görünmeli: %q", m)
	}

	komsu := errors.New(nt_komsuUyarilar + "\nnginx: [emerg] unknown directive \"fastcgi_cahe\" in /etc/nginx/conf.d/dom_c_komsu.conf:12\n" + nt_testBasarisiz)
	kod, govde = nt_kaydet(t, bayi, "add_header X-A 1;", komsu, nil)
	if kod != http.StatusBadRequest {
		t.Fatalf("400 beklenirdi: %d %s", kod, govde)
	}
	nt_yabanciYok(t, "bayi", govde)
	if strings.Contains(govde, "fastcgi_cahe") {
		t.Errorf("komşunun direktif adı sızmamalı: %s", govde)
	}
}
