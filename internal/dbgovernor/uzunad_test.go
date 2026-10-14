package dbgovernor

// uzunad_test.go — 33..64 karakterli kiracı DB kullanıcısı regresyonu (inceleme 2026-10-13).

import (
	"strings"
	"testing"
)

// 33..64 karakterli kiracı DB kullanıcısı (panel `<önek>_<sonek<=32>` üretir) ölçülmez/kısılmazdı:
// regexp {1,32} onu "geçersiz" sayıp governor'dan tamamen muaf bırakıyordu.
func TestUzunAdliKullaniciKisilir(t *testing.T) {
	uzun := "girgin_" + strings.Repeat("x", 30) // 37
	ucUc := "a" + strings.Repeat("b", 63)       // 64
	for _, u := range []string{uzun, ucUc} {
		if !kullaniciGecerli(u) {
			t.Fatalf("%d karakterli kiracı kullanıcısı geçerli sayılmalı", len(u))
		}
	}
	for _, u := range []string{"a" + strings.Repeat("b", 64), "", "a'b", "a b", "a\n", "root", "mysql.sys", "mariadb.sys", "PANEL", "pma"} {
		if kullaniciGecerli(u) {
			t.Fatalf("%q geçersiz olmalıydı", u)
		}
	}
	o := yeniOrtam(t)
	o.hosting(3, 0, "uzun.test", uzun)
	o.taban()
	o.calis(4, hizlar(uzun, 2.0))
	if !esitInts(o.yon.baglanti(uzun), []int{2, 2}) {
		t.Fatalf("uzun adlı kullanıcı kısılmadı: %v (alterler=%v)", o.yon.baglanti(uzun), o.yon.alterler)
	}
}
