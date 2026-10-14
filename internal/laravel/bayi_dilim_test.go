package laravel

// bayi_dilim_test.go — kuyruk işleyici unit'i bayi kaynak havuzuna üye hostingde havuzun İÇİNDEKİ
// dilime (girginos-bayi<N>-<sk>.slice) bağlanır; üyelik yoksa eski düz ad. Boş `Slice=` asla
// üretilmez (systemd'de "dilimi sıfırla" = sessizce sınırsız çalışmak).

import (
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

func TestQueueUnit_BayiHavuzuDilimi(t *testing.T) {
	eski := bayidilim.UyelikDizini
	t.Cleanup(func() { bayidilim.UyelikDizini = eski })
	bayidilim.UyelikDizini = filepath.Join(t.TempDir(), "uyelik")

	satir := func(sk string) string {
		for _, s := range strings.Split(queueUnit(3, sk, "/home/"+sk+"/app", "/usr/bin/php", "database", 60, 1000), "\n") {
			if strings.HasPrefix(s, "Slice=") {
				return s
			}
		}
		return ""
	}
	if g := satir("c_x"); g != "Slice=girginos-c_x.slice" {
		t.Errorf("üyelik yokken: %q", g)
	}
	if _, err := bayidilim.UyelikYaz("c_x", 5); err != nil {
		t.Fatal(err)
	}
	if g := satir("c_x"); g != "Slice=girginos-bayi5-c_x.slice" {
		t.Errorf("üyeyken: %q", g)
	}
	if g := satir("c_diger"); g != "Slice=girginos-c_diger.slice" {
		t.Errorf("başka hosting: %q", g)
	}
	// Katı biçime uymayan ad: eski davranış (boş değil).
	if g := satir("c_Garip-Ad"); g != "Slice=girginos-c_Garip-Ad.slice" {
		t.Errorf("biçim dışı ad: %q", g)
	}
}
