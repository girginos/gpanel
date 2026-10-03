package efektiflimit

import (
	"reflect"
	"testing"
)

// PHP ayar gövdesindeki iç anahtarlar TEK kaynaktan (iç alan pm_max_children) türer.
func TestPHPAyarIcAnahtarlari_IcAlandanTurer(t *testing.T) {
	if !IcAlan("pm_max_children") {
		t.Fatal("pm_max_children iç alan olmalı (bayi kararı)")
	}
	want := []string{"pm_max_children", "pm_start_servers", "pm_min_spare_servers", "pm_max_spare_servers"}
	if got := PHPAyarIcAnahtarlari(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PHPAyarIcAnahtarlari = %v, want %v", got, want)
	}
}
