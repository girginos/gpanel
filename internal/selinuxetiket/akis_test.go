package selinuxetiket

import (
	"os"
	"strings"
	"testing"
)

// Etiket TAŞIYAN akışların (cp -al / rename / rsync / hazırlık alanı) iş sonunda kök
// restorecon'u çağırdığını kilitler: çağrı kaldırılırsa Enforcing'de php-fpm kiracı dosyasına
// yazamaz (181, 2026-10-06: database.sqlite user_home_t). Davranış testi her akışın kendi
// DB/runuser düzeneğini gerektirir; burada çağrının VARLIĞI sabitlenir.
func TestEtiketTasiyanAkislarRestoreconCagirir(t *testing.T) {
	akislar := []struct{ dosya, desen, ne string }{
		{"../laravel/kur.go", `selinuxetiket.LoglaYol("laravel kur"`, "Laravel kurulum (cp -al hardlink)"},
		{"../tasima/aktar_dosya.go", `selinuxetiket.LoglaYol("tasima aktar"`, "taşıma dosya aktarımı (cp -l hazırlık alanı)"},
		{"../tasima/arsiv/cikar.go", `selinuxetiket.LoglaYol("arsiv cikar"`, "arşivden taşıma çıkarma"},
		{"../tasima/arsiv/dizin.go", `selinuxetiket.LoglaYol("arsiv cikar dizin"`, "arşivden taşıma (dizin)"},
		{"../uygulama/uygulama.go", `selinuxetiket.LoglaYol("uygulama kur"`, "uygulama sihirbazı"},
		{"../files/files_ext.go", `selinuxetiket.Logla("dosya yoneticisi tasi"`, "dosya yöneticisi taşı (rename)"},
		{"../files/files_ext.go", `selinuxetiket.Logla("dosya yoneticisi arsiv ac"`, "dosya yöneticisi arşiv aç"},
		{"../backups/granular.go", `selinuxetiket.LoglaYol("yedek geri yukle (secili)"`, "yedek geri yükleme (seçili)"},
		{"../backups/granular.go", `selinuxetiket.Logla("yedek geri yukle (home)"`, "yedek geri yükleme (home)"},
	}
	for _, a := range akislar {
		b, err := os.ReadFile(a.dosya)
		if err != nil {
			t.Fatalf("%s: %v", a.dosya, err)
		}
		if !strings.Contains(string(b), a.desen) {
			t.Errorf("%s: %s akışında %s çağrısı yok", a.dosya, a.ne, a.desen)
		}
	}
}
