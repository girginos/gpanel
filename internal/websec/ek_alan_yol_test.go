package websec

// ek_alan_yol_test.go — ek alan docroot yolu TEK kaynaktan (subdomain.EkDocrootYolu)
// üretilir; klasör adı websec'te sabit değildir.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/subdomain"
)

func TestEk_DocrootYoluSubdomainPaketindenGelir(t *testing.T) {
	db, ev := ekOrtam(t)
	beklenen := subdomain.EkDocrootYolu(ev, "on", "ek1.test")
	if !strings.HasSuffix(beklenen, "/on/domains/ek1.test/public_html") {
		t.Fatalf("subdomain yolu beklenmedik: %s", beklenen)
	}
	if got := ekDocrootYolu("on", "ek1.test"); got != filepath.Clean(beklenen) {
		t.Fatalf("websec yolu subdomain ile aynı olmalı: %s != %s", got, beklenen)
	}
	h, err := tarananHedefler(t.Context(), db, []int64{-1})
	if err != nil || len(h) != 1 || h[0].Docroot != filepath.Clean(beklenen) {
		t.Fatalf("keşif yeni docroot yolunu kullanmalı: %v %+v", err, h)
	}
	// Klasör yeni yerde yoksa keşif ek alanı BULMAZ (eski/başka klasöre bakmaz).
	if err := os.Rename(filepath.Join(ev, "on", "domains"), filepath.Join(ev, "on", "baska-klasor")); err != nil {
		t.Fatal(err)
	}
	if got := ekAnahtarlar(t, db, nil); !reflect.DeepEqual(got, []int64{-2, 10, 11, 12}) {
		t.Fatalf("yeni klasör yoksa ek1 atlanmalı: %v", got)
	}
}
