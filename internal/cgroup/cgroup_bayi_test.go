package cgroup

// cgroup_bayi_test.go — bayi kaynak havuzuna üye hostingin cgroup dizini: iç içe dilim varsa o,
// yoksa (üyelik yeni yazıldı, birim henüz yeniden başlamadı) eski düz dilim. Sahte cgroup ağacı.

import (
	"os"
	"path/filepath"
	"testing"

	"girginospanel/internal/bayidilim"
)

func TestKiraciDizin_BayiHavuzuUyesi(t *testing.T) {
	eK, eU := Kok, bayidilim.UyelikDizini
	t.Cleanup(func() { Kok, bayidilim.UyelikDizini = eK, eU })
	Kok = t.TempDir()
	bayidilim.UyelikDizini = filepath.Join(t.TempDir(), "uyelik")

	duz := filepath.Join(Kok, "girginos.slice", "girginos-c_x.slice")
	ic5 := filepath.Join(Kok, "girginos.slice", "girginos-bayi5.slice", "girginos-bayi5-c_x.slice")
	ic7 := filepath.Join(Kok, "girginos.slice", "girginos-bayi7.slice", "girginos-bayi7-c_x.slice")

	// 1) Üyelik yok: eski düz dizin (iç içe dizin diskte olsa bile).
	if err := os.MkdirAll(ic5, 0o755); err != nil {
		t.Fatal(err)
	}
	if g := KiraciDizin("c_x"); g != duz {
		t.Errorf("üyelik yokken düz dizin beklenir: %q", g)
	}

	// 2) Üye + iç içe dizin var: iç içe dizin (canlı kullanım havuzun içindeki dilimden okunur).
	if _, err := bayidilim.UyelikYaz("c_x", 5); err != nil {
		t.Fatal(err)
	}
	if g := KiraciDizin("c_x"); g != ic5 {
		t.Errorf("üyeyken iç içe dizin beklenir: %q", g)
	}

	// 3) Üye ama iç içe dizin henüz yok (FPM yeniden başlamadı): süreçler eski düz dilimde → o.
	if err := os.RemoveAll(filepath.Join(Kok, "girginos.slice", "girginos-bayi5.slice")); err != nil {
		t.Fatal(err)
	}
	if g := KiraciDizin("c_x"); g != duz {
		t.Errorf("iç içe dizin yokken düz dizine düşmeli: %q", g)
	}

	// 4) İç içe yol DİZİN değil (bozuk/yabancı dosya): düz dizin.
	if err := os.MkdirAll(filepath.Dir(ic5), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ic5, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := KiraciDizin("c_x"); g != duz {
		t.Errorf("iç içe yol dosyayken düz dizin beklenir: %q", g)
	}

	// 5) Başka bayinin iç içe dizini bu hostingin dizini sayılmaz.
	if err := os.MkdirAll(ic7, 0o755); err != nil {
		t.Fatal(err)
	}
	if g := KiraciDizin("c_x"); g != duz {
		t.Errorf("başka bayinin dizini kullanıldı: %q", g)
	}

	// 6) Biçim dışı ad üyelikten bağımsız boş döner.
	if g := KiraciDizin("c_a-b"); g != "" {
		t.Errorf("biçim dışı ad boş beklenir: %q", g)
	}
}
