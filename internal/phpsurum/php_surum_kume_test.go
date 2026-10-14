package phpsurum

import (
	"sort"
	"strings"
	"testing"

	"girginospanel/internal/provisioner"
)

// provisioner.phpMap (panelin YÖNETTİĞİ, API doğrulayıcısının kaynağı) ile
// DesteklenenSurumler (kurulum/EOL kaynağı) AYNI sürüm kümesini taşımalı — biri eksikse
// split-brain: sürüm kurulabilir ama atanamaz (API 400), ya da atanır ama havuzu yazılamaz.
func TestPHPSurumleri_ProvisionerIleAyniKume(t *testing.T) {
	set := map[string]bool{}
	for _, m := range DesteklenenSurumler {
		set[m.Surum] = true
	}
	var beklenen []string
	for v := range set {
		beklenen = append(beklenen, v)
	}
	sort.Strings(beklenen)
	got := provisioner.PHPSurumleri()
	if strings.Join(got, ",") != strings.Join(beklenen, ",") {
		t.Fatalf("provisioner.phpMap %v ≠ phpsurum.DesteklenenSurumler %v", got, beklenen)
	}
	for _, v := range beklenen {
		if _, err := provisioner.PHPSurumDogrula(v); err != nil {
			t.Errorf("desteklenen sürüm %s doğrulayıcıda reddedildi: %v", v, err)
		}
	}
}
