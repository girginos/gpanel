package kiraci

// dilim_bayi_test.go — bayi kaynak havuzuna üye hosting, cron/kabuk işlerini havuzun İÇİNDEKİ
// iç içe dilimde (girginos-bayi<N>-<sk>.slice) çalıştırır; üyelik yoksa eski düz ad.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"girginospanel/internal/bayidilim"
)

func bayiUyelikSahte(t *testing.T) {
	t.Helper()
	eski := bayidilim.UyelikDizini
	t.Cleanup(func() { bayidilim.UyelikDizini = eski })
	bayidilim.UyelikDizini = filepath.Join(t.TempDir(), "uyelik")
}

func TestDilimAdi_BayiHavuzuUyesi(t *testing.T) {
	bayiUyelikSahte(t)
	if d, err := DilimAdi("c_ornek_com"); err != nil || d != "girginos-c_ornek_com.slice" {
		t.Fatalf("üyelik yokken DilimAdi = %q, %v", d, err)
	}
	if _, err := bayidilim.UyelikYaz("c_ornek_com", 5); err != nil {
		t.Fatal(err)
	}
	if d, err := DilimAdi("c_ornek_com"); err != nil || d != "girginos-bayi5-c_ornek_com.slice" {
		t.Fatalf("üyeyken DilimAdi = %q, %v", d, err)
	}
	// Başka hostingin dilimi etkilenmez.
	if d, err := DilimAdi("c_diger"); err != nil || d != "girginos-c_diger.slice" {
		t.Fatalf("üyesiz hosting DilimAdi = %q, %v", d, err)
	}
	// Biçim dışı ad üyelik dosyası olsa bile reddedilir (dilim adı boş kalıp sınırsız çalışmasın).
	for _, sk := range []string{"", "root", "c_x --slice=-.slice", "c_a/b"} {
		if _, err := DilimAdi(sk); !errors.Is(err, ErrSK) {
			t.Errorf("DilimAdi(%q) reddedilmeliydi: %v", sk, err)
		}
	}
}

func TestKomut_Dilim_BayiHavuzuUyesi(t *testing.T) {
	bayiUyelikSahte(t)
	sahteKiraciKullanici(t)
	if _, err := bayidilim.UyelikYaz("c_ornek_com", 12); err != nil {
		t.Fatal(err)
	}
	k := Kiraci{Kul: "c_ornek_com", Ev: "/home/c_ornek_com", Dilim: true}
	cmd, err := k.Komut(context.Background(), "/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	var bulundu bool
	for _, a := range cmd.Args {
		if a == "--slice=girginos-bayi12-c_ornek_com.slice" {
			bulundu = true
		}
		if a == "--slice=girginos-c_ornek_com.slice" {
			t.Fatalf("üye hosting eski düz dilimde çalıştırıldı: %q", cmd.Args)
		}
	}
	if !bulundu {
		t.Fatalf("iç içe dilim argümanı yok: %q", cmd.Args)
	}
}
