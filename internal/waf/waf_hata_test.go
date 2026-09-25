package waf

// waf_hata_test.go — F1 (aynı kök neden): WAF kaydında vhost render hatası ham
// `nginx -t` çıktısını (öteki kiracıların sertifika/vhost yolları) bayiye/müşteriye
// döndürmez; yönetici metni birebir aynı görür. provisioner.WAFUygula dikişle
// (wafUygula) sahteye çevrilir — ana makinenin nginx'ine dokunulmaz. DB sahtedb.

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

var wt_sir = []byte("waf-hata-test-anahtari-0123456789abcdef")

func wt_kaydet(t *testing.T, tok string, uygulaHata error) (int, string) {
	t.Helper()
	eski := wafUygula
	t.Cleanup(func() { wafUygula = eski })
	wafUygula = func(*sql.DB, int64) error { return uygulaHata }
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici FROM domains", []any{"c_bayialan"})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(wt_sir)).Put("/domains/{id}/waf", (&Handlers{DB: db}).Kaydet)
	req := httptest.NewRequest(http.MethodPut, "/domains/42/waf", strings.NewReader(`{"ayar":{"mod":"engelle","paranoya":1}}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestKaydet_VhostHatasi_BayiMusteriyeHamCiktiDonmez(t *testing.T) {
	admin, err := auth.IssueAt(wt_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(wt_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(wt_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	ham := fmt.Errorf("nginx -t başarısız: %s: %w",
		`nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/test2.girginos.app/test2.girginos.app.crt"`+
			"\nnginx: [emerg] unknown directive \"x\" in /etc/nginx/conf.d/dom_c_komsu.conf:12\nnginx: configuration file /etc/nginx/nginx.conf test failed",
		errors.New("exit status 1"))
	const onek = "WAF ayarı kaydedildi ancak vhost render başarısız (nginx değişmedi): "
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
		kod, govde := wt_kaydet(t, tok, ham)
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		for _, iz := range []string{"test2.girginos.app", "/etc/pki", "dom_c_komsu", "/etc/nginx"} {
			if strings.Contains(govde, iz) {
				t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
			}
		}
		var m map[string]string
		_ = json.Unmarshal([]byte(govde), &m)
		if !strings.HasPrefix(m["hata"], onek) || !strings.Contains(m["hata"], "günlüğüne") {
			t.Errorf("%s: önek korunmalı + genel mesaj: %q", rol, m["hata"])
		}
	}
	kod, govde := wt_kaydet(t, admin, ham)
	var m map[string]string
	_ = json.Unmarshal([]byte(govde), &m)
	if kod != http.StatusInternalServerError || m["hata"] != onek+ham.Error() {
		t.Fatalf("yönetici ham ayrıntıyı aynen görmeli: %d %s", kod, govde)
	}
}
