package hostuyg

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// SHA256 uyumsuzluğu / HTTP hata durumunda yarım indirilen (en çok 5 GB) geçici
// dosya önbellek dizininde KALMAMALI (disk doldurma).
func TestIndir_HataDurumundaGeciciDosyaSilinir(t *testing.T) {
	dizin := t.TempDir()
	eski := IndirmeCacheDir
	IndirmeCacheDir = dizin
	t.Cleanup(func() { IndirmeCacheDir = eski })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/yok" {
			http.Error(w, "yok", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("icerik"))
	}))
	defer srv.Close()

	for ad, url := range map[string]string{"sha-uyumsuz": srv.URL + "/ok", "http-404": srv.URL + "/yok"} {
		if _, err := Indir(url, "00"); err == nil {
			t.Fatalf("%s: hata bekleniyordu", ad)
		}
		kalan, _ := os.ReadDir(dizin)
		if len(kalan) != 0 {
			t.Errorf("%s: geçici dosya kaldı: %d", ad, len(kalan))
		}
	}
}
