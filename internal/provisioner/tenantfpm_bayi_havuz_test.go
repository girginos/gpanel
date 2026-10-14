package provisioner

// tenantfpm_bayi_havuz_test.go — bayi kaynak havuzu üyeliği php-fpm birimine `Slice=` drop-in'i
// olarak yansır; ana birim ŞABLONU değişmez (RepairTenantUnitDrift sağlıklı tenant'ları
// yeniden başlatmasın), StartLimit drop-in'i korunur, üyelik kalkınca yalnız havuz drop-in'i gider.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

func TestTenantDropinleriYaz_BayiHavuzuSliceDropin(t *testing.T) {
	sokumDikis(t)
	eU := bayidilim.UyelikDizini
	bayidilim.UyelikDizini = filepath.Join(t.TempDir(), "uyelik")
	t.Cleanup(func() { bayidilim.UyelikDizini = eU })

	if u := renderTenantUnit("c_x", "/bin/php-fpm"); strings.Contains(u, "Slice=") && strings.Contains(u, "girginos-bayi") {
		t.Fatal("havuz üyeliği ANA birime girmemeli (damga değişir → tüm tenant'lar restart)")
	}
	sk_yaz(t, tenantUnitPath("c_x"), renderTenantUnit("c_x", "/bin/php-fpm"))
	anaOnce, _ := os.ReadFile(tenantUnitPath("c_x"))
	dropin := filepath.Join(tenantUnitDropinDir("c_x"), bayidilim.DropinAd)

	// Üye değil: StartLimit drop-in'i var, havuz drop-in'i YOK.
	tenantDropinleriYaz("c_x")
	if izVar(dropin) {
		t.Fatal("üye olmayan hostinge havuz drop-in'i yazıldı")
	}
	if _, err := os.Stat(filepath.Join(tenantUnitDropinDir("c_x"), tenantStartLimitDropinAd)); err != nil {
		t.Fatalf("StartLimit drop-in'i yazılmadı: %v", err)
	}

	// Üye: Slice= iç içe plan dilimine işaret eder, StartLimit drop-in'i yerinde kalır.
	if _, err := bayidilim.UyelikYaz("c_x", 7); err != nil {
		t.Fatal(err)
	}
	tenantDropinleriYaz("c_x")
	b, err := os.ReadFile(dropin)
	if err != nil {
		t.Fatalf("üye hostinge havuz drop-in'i yazılmadı: %v", err)
	}
	if !strings.Contains(string(b), "[Service]\nSlice=girginos-bayi7-c_x.slice\n") {
		t.Errorf("drop-in içeriği:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(tenantUnitDropinDir("c_x"), tenantStartLimitDropinAd)); err != nil {
		t.Errorf("StartLimit drop-in'i kayboldu: %v", err)
	}
	// İdempotent: ikinci çağrı dosyayı değiştirmez.
	st1, _ := os.Stat(dropin)
	tenantDropinleriYaz("c_x")
	if st2, _ := os.Stat(dropin); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("aynı içerik yeniden yazıldı (idempotent değil)")
	}
	// Başka havuza taşınınca drop-in yeni dilime işaret eder.
	_, _ = bayidilim.UyelikYaz("c_x", 9)
	tenantDropinleriYaz("c_x")
	if b, _ := os.ReadFile(dropin); !strings.Contains(string(b), "Slice=girginos-bayi9-c_x.slice\n") {
		t.Errorf("devirden sonra drop-in eski havuzda kaldı:\n%s", b)
	}

	// Üyelik kalkınca YALNIZ havuz drop-in'i gider; StartLimit kalır.
	_, _ = bayidilim.UyelikSil("c_x")
	tenantDropinleriYaz("c_x")
	if izVar(dropin) {
		t.Error("üyelik kalkınca havuz drop-in'i silinmedi (hosting hâlâ eski havuz dilimini ister)")
	}
	if _, err := os.Stat(filepath.Join(tenantUnitDropinDir("c_x"), tenantStartLimitDropinAd)); err != nil {
		t.Errorf("üyelik kalkınca StartLimit drop-in'i silindi: %v", err)
	}
	// Ana birim dosyası hiçbir adımda değişmedi.
	if anaSonra, _ := os.ReadFile(tenantUnitPath("c_x")); string(anaSonra) != string(anaOnce) {
		t.Error("ana birim dosyası değişti")
	}
	// Geçersiz sk: panik/yazım yok.
	tenantDropinleriYaz("../etc")
	tenantDropinleriYaz("")
}
