package domains

// sunucu_hatasi_test.go — bayi kapsamı (F1'in kalan uçları): alan adı PHP sürümü,
// web backend, belge kökü, SSL kapatma ve askı uçlarında vhost render hatası
// (SUNUCU GENELİ `nginx -t` çıktısı: öteki kiracıların sertifika/vhost yolları)
// bayiye ve müşteriye ham DÖNMEZ; yöneticinin gördüğü metin birebir aynıdır.
//
// Dış etkili adımlar (phpSurumUygula, vhostUygulaAlan, vhostYenidenCiz,
// webKokDogrula, sslKapat) dikişlerle sahteye çevrilir — test ana makinenin
// nginx'ine dokunmaz. DB sahtedb. Gerçek RequireAuth + imzalı jetonlar.

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

var sh_sir = []byte("domains-sunucu-hatasi-test-anahtari-0123")

func sh_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(sh_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(sh_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(sh_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// Gerçek `nginx -t` çıktısı biçiminde: sunucu genelindeki ssl_stapling her SSL'li
// alan adı için uyarı basar; küresel bozulmada komşunun vhost yolu görünür.
var sh_ham = fmt.Errorf("nginx -t başarısız: %s: %w",
	`nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [emerg] unknown directive "fastcgi_cahe" in /etc/nginx/conf.d/dom_c_komsubayi.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed`,
	errors.New("exit status 1"))

// Bayi/müşteri yanıtında GÖRÜNMEMESİ gereken yabancı / sunucu geneli izler.
var sh_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx"}

// sh_dikisler — tüm dış etkili adımları sahteye çevirir (hepsi sh_ham döner;
// belge kökü doğrulaması geçer). Ana makineye hiçbir adım ulaşmaz.
func sh_dikisler(t *testing.T) {
	t.Helper()
	e1, e2, e3, e4, e5 := phpSurumUygula, vhostUygulaAlan, vhostYenidenCiz, webKokDogrula, sslKapat
	t.Cleanup(func() { phpSurumUygula, vhostUygulaAlan, vhostYenidenCiz, webKokDogrula, sslKapat = e1, e2, e3, e4, e5 })
	phpSurumUygula = func(string, string, string, string, string, string, string) (string, error) { return "", sh_ham }
	vhostUygulaAlan = func(*sql.DB, int64, string, string) error { return sh_ham }
	vhostYenidenCiz = func(*sql.DB, int64) error { return sh_ham }
	webKokDogrula = func(sk, alt string) (string, error) { return "/home/" + sk + "/public_html/" + alt, nil }
	sslKapat = func(string, string, string, string) error { return sh_ham }
}

type sh_durum struct {
	ad, yontem, yol, desen, govde string
	isle                          func(*Handlers) http.HandlerFunc
	satirlar                      func(*sahtedb.DB)
	onek, sonek                   string // yönetici metni = onek + sh_ham + sonek
}

var sh_durumlar = []sh_durum{
	{
		ad: "SetPHP", yontem: http.MethodPut, yol: "/domains/42/php", desen: "/domains/{id}/php",
		govde: `{"php_surum":"8.3"}`,
		isle:  func(h *Handlers) http.HandlerFunc { return h.SetPHP },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(web_backend",
				[]any{"bayialan.test", "c_bayialan", int64(0), "php-fpm", "", "", ""})
		},
		onek: "değişim başarısız: ",
	},
	{
		ad: "SetWebBackend", yontem: http.MethodPut, yol: "/domains/42/web-backend", desen: "/domains/{id}/web-backend",
		govde: `{"backend":"static"}`,
		isle:  func(h *Handlers) http.HandlerFunc { return h.SetWebBackend },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, php_surum, is_demo FROM domains",
				[]any{"bayialan.test", "c_bayialan", "8.3", int64(0)})
			sd.Satirlar("SELECT COALESCE(web_backend,'php-fpm') FROM domains", []any{"php-fpm"})
		},
		onek: "vhost render: ", sonek: " (backend değişikliği geri alındı)",
	},
	{
		ad: "SetWebRoot", yontem: http.MethodPut, yol: "/domains/42/web-root", desen: "/domains/{id}/web-root",
		govde: `{"alt_dizin":"public"}`,
		isle:  func(h *Handlers) http.HandlerFunc { return h.SetWebRoot },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT sistem_kullanici, is_demo FROM domains", []any{"c_bayialan", int64(0)})
		},
		onek: "vhost render: ",
	},
	{
		ad: "SSLDisable", yontem: http.MethodDelete, yol: "/domains/42/ssl", desen: "/domains/{id}/ssl",
		isle: func(h *Handlers) http.HandlerFunc { return h.SSLDisable },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend",
				[]any{"bayialan.test", "c_bayialan", "8.3", int64(0), "php-fpm"})
		},
		onek: "SSL kapat: ",
	},
	{
		ad: "AskiyaAl", yontem: http.MethodPost, yol: "/domains/42/askiya-al", desen: "/domains/{id}/askiya-al",
		isle: func(h *Handlers) http.HandlerFunc { return h.AskiyaAl },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo FROM domains",
				[]any{"bayialan.test", "c_bayialan", int64(0)})
		},
		onek: "vhost render: ",
	},
	{
		ad: "AskidanAl", yontem: http.MethodPost, yol: "/domains/42/askidan-al", desen: "/domains/{id}/askidan-al",
		isle: func(h *Handlers) http.HandlerFunc { return h.AskidanAl },
		satirlar: func(sd *sahtedb.DB) {
			sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo FROM domains",
				[]any{"bayialan.test", "c_bayialan", int64(0)})
		},
		onek: "vhost render: ",
	},
}

func sh_cagir(t *testing.T, d sh_durum, tok string) (int, string) {
	t.Helper()
	sd, db := sahtedb.Yeni()
	d.satirlar(sd)
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sh_sir)).Method(d.yontem, d.desen, d.isle(&Handlers{DB: db}))
	req := httptest.NewRequest(d.yontem, d.yol, strings.NewReader(d.govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func sh_hataMetni(t *testing.T, govde string) string {
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

// Vhost render hatası: bayi/müşteri genel mesaj (öneki korunmuş) görür, komşu
// kiracının adı/yolu sızmaz; yönetici metni öncekiyle BİREBİR aynı.
func TestVhostHatasi_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	sh_dikisler(t)
	tok := sh_tokenlar(t)
	for _, d := range sh_durumlar {
		t.Run(d.ad, func(t *testing.T) {
			for _, rol := range []string{"bayi", "müşteri"} {
				kod, govde := sh_cagir(t, d, tok[rol])
				if kod != http.StatusInternalServerError {
					t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
				}
				for _, iz := range sh_izler {
					if strings.Contains(govde, iz) {
						t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
					}
				}
				m := sh_hataMetni(t, govde)
				if !strings.HasPrefix(m, d.onek) || !strings.HasSuffix(m, d.sonek) || !strings.Contains(m, "günlüğüne") {
					t.Errorf("%s: genel mesaj (%q … günlüğüne … %q) beklenirdi: %q", rol, d.onek, d.sonek, m)
				}
			}
			kod, govde := sh_cagir(t, d, tok["admin"])
			if kod != http.StatusInternalServerError {
				t.Fatalf("yönetici: 500 beklenirdi: %d %s", kod, govde)
			}
			if m, want := sh_hataMetni(t, govde), d.onek+sh_ham.Error()+d.sonek; m != want {
				t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, want)
			}
		})
	}
}
