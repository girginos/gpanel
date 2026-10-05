package provisioner

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func pmYolKur(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	e1, e2, e3, e4 := pmWebroot, pmYolTaban, pmYolDeneme, pmYolAralik
	t.Cleanup(func() { pmWebroot, pmYolTaban, pmYolDeneme, pmYolAralik = e1, e2, e3, e4 })
	pmWebroot, pmYolDeneme, pmYolAralik = d, 8, time.Millisecond
	return d
}

// reload bitene kadar (ilk N istek) yeni vhost yok → 404 (eski yapılandırma, _default80); sonra 200.
func TestPmAcmeYoluBekle_ReloadGecikmesiniBekler(t *testing.T) {
	d := pmYolKur(t)
	var n atomic.Int32
	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "gpanel.test" || n.Add(1) <= 3 {
			http.NotFound(w, r)
			return
		}
		http.FileServer(http.Dir(d)).ServeHTTP(w, r)
	}))
	defer sunucu.Close()
	pmYolTaban = sunucu.URL
	if err := pmAcmeYoluBekleGercek("gpanel.test"); err != nil {
		t.Fatalf("reload gecikmesi beklenmeliydi: %v", err)
	}
	if n.Load() < 4 {
		t.Fatalf("ilk istekler 404'tü; en az 4 deneme beklenir, %d", n.Load())
	}
	if kalan, _ := filepath.Glob(filepath.Join(d, ".well-known", "acme-challenge", "gosp-onsinav-*")); len(kalan) != 0 {
		t.Fatalf("jeton dosyası temizlenmeliydi: %v", kalan)
	}
}

// Kalıcı 404 / yönlendirme / başka içerik: LE'ye GİDİLMEMELİ → hata döner.
func TestPmAcmeYoluBekle_KaliciHatadaVazgecer(t *testing.T) {
	for ad, h := range map[string]http.HandlerFunc{
		"404": func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"yonlendirme": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://x/", http.StatusMovedPermanently)
		},
		"baska icerik": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>SPA</html>")) },
	} {
		t.Run(ad, func(t *testing.T) {
			_ = pmYolKur(t)
			sunucu := httptest.NewServer(h)
			defer sunucu.Close()
			pmYolTaban = sunucu.URL
			if err := pmAcmeYoluBekleGercek("gpanel.test"); err == nil {
				t.Fatal("kalıcı hata beklenir")
			}
		})
	}
}
