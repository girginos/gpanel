package git

// webhook_aski_test.go — kimlik doğrulamasız git webhook'u askıdaki hesapta
// HİÇBİR komut çalıştırmaz (pull kiracı kimliğiyle git çalıştırır; kiracının
// .git/config / hooks'u kod yürütebilir). Yanıt eşleşmeyen sırla birebir aynıdır:
// hesabın askı durumu sızmaz. Komut çalıştırıcı sahte, DB sahtedb.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/sahtedb"
)

func TestWebhook_HesapAskida_KomutCalismaz_DurumSizmaz(t *testing.T) {
	eskiK, eskiEv := kiraciCalistir, evKoku
	t.Cleanup(func() { kiraciCalistir, evKoku = eskiK, eskiEv })
	evKoku = t.TempDir()
	var calisan []string
	kiraciCalistir = func(sk, cwd string, stdin []byte, argv ...string) (string, string, error) {
		calisan = append(calisan, strings.Join(argv, " "))
		return "", "", errors.New("sahte")
	}
	cagir := func(repo bool, askida bool) (*sahtedb.DB, *httptest.ResponseRecorder) {
		sd, db := sahtedb.Yeni()
		if repo {
			sd.Satirlar("FROM git_repos g JOIN domains d", []any{int64(3), int64(42), "c_gitaski", "main", "public_html"})
		}
		if askida {
			sd.Satirlar("FROM domains WHERE sistem_kullanici=? AND COALESCE(askida,0)=1", []any{int64(1)})
		}
		r := chi.NewRouter()
		r.Post("/api/v1/git-webhook/{secret}", (&Handlers{DB: db}).Webhook)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/git-webhook/0123456789abcdef0123", nil))
		return sd, rec
	}

	_, eslesmedi := cagir(false, false)
	sd, rec := cagir(true, true)
	if rec.Code != eslesmedi.Code || rec.Body.String() != eslesmedi.Body.String() {
		t.Fatalf("askıdaki hesabın yanıtı eşleşmeyen sırla aynı olmalı: %d %q / %d %q",
			rec.Code, rec.Body.String(), eslesmedi.Code, eslesmedi.Body.String())
	}
	if len(calisan) != 0 || sd.Yurutuldu("UPDATE git_repos") {
		t.Fatalf("askıdaki hesapta hiçbir komut çalışmamalı: %v", calisan)
	}

	sd, _ = cagir(true, false)
	if !sd.Yurutuldu("UPDATE git_repos") {
		t.Fatal("askıda olmayan hesapta pull denenmeli (kapı yalnız askıya özgü)")
	}
}
