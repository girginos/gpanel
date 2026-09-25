package sifrekoruma

// sifrekoruma_hata_test.go — bayi kapsamı (son ham hata yerleri): dizin şifre
// koruması ekleme/silme uçlarında vhost yeniden render hatası (SUNUCU GENELİ
// `nginx -t`: komşu kiracıların sertifika ve vhost yolları) bayiye ve müşteriye
// ham DÖNMEZ; yöneticinin gördüğü metin birebir aynıdır.
//
// Dış etkili adımlar (htpasswd dizini + komutu, korumaRender) dikişlerle sahte —
// test ana makinenin /etc/nginx dizinine ve nginx'ine dokunmaz. DB sahtedb.

import (
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

var sk_sir = []byte("sifrekoruma-hata-test-anahtari-0123456789")

func sk_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(sk_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(sk_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(sk_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// Gerçek `nginx -t` çıktısı biçiminde (ApplyVhostForDomain → renderAndReload).
var sk_ham = fmt.Errorf("nginx -t başarısız: %s: %w",
	`nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [emerg] unknown directive "fastcgi_cahe" in /etc/nginx/conf.d/dom_c_komsubayi.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed`,
	errors.New("exit status 1"))

var sk_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx"}

// sk_dikisler — htpasswd dizini geçici, htpasswd komutu ve vhost render sahte.
func sk_dikisler(t *testing.T) {
	t.Helper()
	e1, e2, e3 := htpasswdKok, htpasswdKomut, korumaRender
	t.Cleanup(func() { htpasswdKok, htpasswdKomut, korumaRender = e1, e2, e3 })
	htpasswdKok = t.TempDir()
	htpasswdKomut = func(...string) ([]byte, error) { return nil, nil }
	korumaRender = func(*Handlers, int64, string, string, int64) error { return sk_ham }
}

type sk_uc struct {
	ad, yontem, desen, yol, govde, onek string
	isle                                func(*Handlers) http.HandlerFunc
}

var sk_uclar = []sk_uc{
	{ad: "Ekle", yontem: http.MethodPost, desen: "/domains/{id}/koruma", yol: "/domains/42/koruma",
		govde: `{"yol":"/gizli","kullanici":"ali","parola":"parola123"}`,
		onek:  "nginx yapılandırması doğrulanamadı: ",
		isle:  func(h *Handlers) http.HandlerFunc { return h.Ekle }},
	{ad: "EkleAltAlan", yontem: http.MethodPost, desen: "/domains/{id}/subdomain/{sid}/koruma", yol: "/domains/42/subdomain/5/koruma",
		govde: `{"yol":"/gizli","kullanici":"ali","parola":"parola123"}`,
		onek:  "nginx yapılandırması doğrulanamadı: ",
		isle:  func(h *Handlers) http.HandlerFunc { return h.Ekle }},
	{ad: "Sil", yontem: http.MethodDelete, desen: "/domains/{id}/koruma/{kid}", yol: "/domains/42/koruma/3",
		onek: "nginx yeniden yüklenemedi: ",
		isle: func(h *Handlers) http.HandlerFunc { return h.Sil }},
}

func sk_cagir(t *testing.T, u sk_uc, tok string) (int, string) {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0) FROM domains",
		[]any{"c_bayialan", "8.3", int64(0)})
	sd.Satirlar("SELECT COALESCE(php_surum,'8.3') FROM subdomanlar", []any{"8.3"})
	sd.Satirlar("SELECT yol, kullanici, htpasswd_dosya, subdomain_id FROM korumali_dizinler",
		[]any{"/gizli", "ali", htpasswdKok + "/d42_gizli", int64(0)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sk_sir)).Method(u.yontem, u.desen, u.isle(&Handlers{DB: db}))
	req := httptest.NewRequest(u.yontem, u.yol, strings.NewReader(u.govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func sk_hataMetni(t *testing.T, govde string) string {
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

func TestKorumaRenderHatasi_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	tok := sk_tokenlar(t)
	for _, u := range sk_uclar {
		t.Run(u.ad, func(t *testing.T) {
			for _, rol := range []string{"bayi", "müşteri"} {
				sk_dikisler(t)
				kod, govde := sk_cagir(t, u, tok[rol])
				if kod != http.StatusInternalServerError {
					t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
				}
				for _, iz := range sk_izler {
					if strings.Contains(govde, iz) {
						t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
					}
				}
				if m := sk_hataMetni(t, govde); !strings.HasPrefix(m, u.onek) || !strings.Contains(m, "günlüğüne") {
					t.Errorf("%s: genel mesaj (%q … günlüğüne …) beklenirdi: %q", rol, u.onek, m)
				}
			}
			sk_dikisler(t)
			kod, govde := sk_cagir(t, u, tok["admin"])
			if kod != http.StatusInternalServerError {
				t.Fatalf("yönetici: 500 beklenirdi: %d %s", kod, govde)
			}
			if m, want := sk_hataMetni(t, govde), u.onek+sk_ham.Error(); m != want {
				t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, want)
			}
		})
	}
}
