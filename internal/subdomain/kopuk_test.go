package subdomain

// kopuk_test.go — müşteri (MusteriScope) uçları: istemci (tarayıcı 30 sn sınırı)
// iş SÜRERKEN bağlantıyı bırakır → r.Context() iptal olur. Yan etki başladıktan
// sonraki adımlar (askı okuması + vhost + kayıt; SSL sonrası HTTPS vhost'u)
// iptal edilmiş bağlamla düşmemeli: alt alan adı / SSL YARIM kalmaz.
// Dikişler hesap_aski_test.go'dan (geçici dizin + sahte komut + geçici MariaDB).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// kp_cagir: ilk `tetik` komutunda istek bağlamını iptal eden (istemcinin gidişi) çağrı.
func kp_cagir(t *testing.T, tetik, yontem, desen, yol, govde string, isle func(*Handlers) http.HandlerFunc, h *Handlers) (int, string, bool) {
	t.Helper()
	ctx, iptal := context.WithCancel(context.Background())
	defer iptal()
	onceki := komut
	t.Cleanup(func() { komut = onceki })
	iptalEdildi := false
	komut = func(ad string, arg ...string) ([]byte, error) {
		if ad == tetik && !iptalEdildi {
			iptal()
			iptalEdildi = true
		}
		return onceki(ad, arg...)
	}
	r := chi.NewRouter()
	r.Method(yontem, desen, isle(h))
	req := httptest.NewRequest(yontem, yol, strings.NewReader(govde)).WithContext(ctx)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), iptalEdildi
}

func TestOlustur_IstemciKoparsaAltAlanTamamlanir(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	kod, govde, iptal := kp_cagir(t, "chown", http.MethodPost, "/domains/{id}/subdomain",
		fmt.Sprintf("/domains/%d/subdomain", ha_domID), `{"alt_ad":"kopuk"}`,
		func(h *Handlers) http.HandlerFunc { return h.Olustur }, &Handlers{DB: db})
	if !iptal {
		t.Fatal("kurgu: istek bağlamı sağlama sırasında iptal edilmeliydi")
	}
	if kod != http.StatusOK {
		t.Fatalf("istemci koptu diye oluşturma yarıda kaldı: %d %s", kod, govde)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subdomanlar WHERE alt_ad='kopuk'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alt alan kaydı yazılmadı: n=%d err=%v", n, err)
	}
	if !strings.Contains(ha_conf(t, "kopuk"), "server_name kopuk."+ha_alan+";") {
		t.Fatal("alt alan vhost'u yazılmadı")
	}
}

func TestSSLKur_IstemciKoparsaHTTPSVhostYazilir(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "guvenli")
	if err := os.MkdirAll(sslDir(ha_sk), 0o750); err != nil {
		t.Fatal(err)
	}
	kod, govde, iptal := kp_cagir(t, "openssl", http.MethodPost, "/domains/{id}/subdomain/{sid}/ssl",
		fmt.Sprintf("/domains/%d/subdomain/%d/ssl", ha_domID, sid), `{"tip":"self-signed"}`,
		func(h *Handlers) http.HandlerFunc { return h.SSLKur }, &Handlers{DB: db})
	if !iptal {
		t.Fatal("kurgu: istek bağlamı sertifika üretimi sırasında iptal edilmeliydi")
	}
	if kod != http.StatusOK {
		t.Fatalf("sertifika diskte ama vhost yazılmadı (yarım SSL): %d %s", kod, govde)
	}
	crt, _ := certYolu(ha_sk, "guvenli."+ha_alan)
	if conf := ha_conf(t, "guvenli"); !strings.Contains(conf, "listen 443 ssl;") || !strings.Contains(conf, crt) {
		t.Fatalf("HTTPS vhost'u bekleniyordu:\n%s", conf)
	}
}
