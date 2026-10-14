package subdomain

// php_tenant_test.go — canlı API doğrulamasında (VM, 2026-10-13) bulunanlar: alt alan
// php_surum'u (oluştur + PUT .../php) doğrulanmadan yazılıyordu; per-tenant FPM'de alt
// alana farklı sürüm 200 dönüp çalışan sürümü değiştirmiyordu; alt alan PHP/SSL
// işlemleri denetim satırı bırakmıyordu.

import (
	"net/http"
	"strings"
	"testing"
)

func pt_tenant(t *testing.T, aktif bool) {
	t.Helper()
	eski := tenantFPMAktif
	t.Cleanup(func() { tenantFPMAktif = eski })
	tenantFPMAktif = func(string) bool { return aktif }
}

func TestPHPDegistir_GecersizSurum400VeHicbirSeyYazilmaz(t *testing.T) {
	tok := hk_tokenlar(t)
	pt_tenant(t, false)
	for _, v := range []string{"9.9", "php8.9", "", "../x", "8.30"} {
		hk_dikisler(t, nil, nil)
		u := hk_phpDegistir
		u.govde = `{"php_surum":"` + v + `"}`
		kod, govde, sd := hk_cagir(t, u, tok["admin"])
		if kod != http.StatusBadRequest || !strings.Contains(hk_hataMetni(t, govde), "desteklenen") {
			t.Errorf("php_surum=%q: 400 + desteklenen sürümler beklenirdi: %d %s", v, kod, govde)
		}
		if sd.Yurutuldu("UPDATE subdomanlar") || sd.Yurutuldu("audit_log") {
			t.Errorf("php_surum=%q: reddedilen istek DB'ye yazdı", v)
		}
	}
}

func TestPHPDegistir_TenantFPMdeFarkliSurum409(t *testing.T) {
	tok := hk_tokenlar(t)
	pt_tenant(t, true)
	hk_dikisler(t, nil, nil)
	u := hk_phpDegistir
	u.govde = `{"php_surum":"8.2"}`
	kod, govde, sd := hk_cagir(t, u, tok["admin"])
	if kod != http.StatusConflict || !strings.Contains(hk_hataMetni(t, govde), "hosting'in PHP sürümünü (8.3)") {
		t.Errorf("per-tenant FPM'de farklı sürüm 409 beklenirdi: %d %s", kod, govde)
	}
	if sd.Yurutuldu("UPDATE subdomanlar") {
		t.Error("reddedilen sürüm DB'ye yazıldı")
	}
	// Hosting ile aynı sürüm engellenmez.
	hk_dikisler(t, nil, nil)
	u.govde = `{"php_surum":"8.3"}`
	if kod, govde, _ := hk_cagir(t, u, tok["admin"]); kod != http.StatusOK {
		t.Errorf("hosting ile aynı sürüm kabul edilmeli: %d %s", kod, govde)
	}
}

func TestPHPDegistir_DenetimSatiri(t *testing.T) {
	tok := hk_tokenlar(t)
	pt_tenant(t, false)
	hk_dikisler(t, nil, nil)
	kod, govde, sd := hk_cagir(t, hk_phpDegistir, tok["admin"])
	if kod != http.StatusOK {
		t.Fatalf("200 beklenirdi: %d %s", kod, govde)
	}
	if !sd.Yurutuldu("audit_log") {
		t.Error("alt alan PHP değişimi denetim satırı yazmalı")
	}
}

func TestOlustur_PHPSurumDogrulanirVeTenantFarkliSurum409(t *testing.T) {
	tok := hk_tokenlar(t)
	for _, d := range []struct {
		tenant bool
		govde  string
		kod    int
	}{
		{false, `{"alt_ad":"yeni","php_surum":"9.9"}`, http.StatusBadRequest},
		{false, `{"alt_ad":"yeni","php_surum":"php8.9"}`, http.StatusBadRequest},
		{true, `{"alt_ad":"yeni","php_surum":"8.2"}`, http.StatusConflict},
	} {
		pt_tenant(t, d.tenant)
		hk_dikisler(t, nil, nil)
		u := hk_olustur
		u.govde = d.govde
		kod, govde, sd := hk_cagir(t, u, tok["admin"])
		if kod != d.kod {
			t.Errorf("%s (tenant=%t): %d beklenirdi: %d %s", d.govde, d.tenant, d.kod, kod, govde)
		}
		if sd.Yurutuldu("INSERT INTO subdomanlar") {
			t.Errorf("%s: reddedilen alt alan oluşturuldu", d.govde)
		}
	}
	// Arayüz sürüm göndermez → hosting sürümü; per-tenant FPM'de engellenmez.
	pt_tenant(t, true)
	hk_dikisler(t, nil, nil)
	u := hk_olustur
	u.govde = `{"alt_ad":"yeni"}`
	if kod, govde, _ := hk_cagir(t, u, tok["admin"]); kod == http.StatusConflict || kod == http.StatusBadRequest {
		t.Errorf("sürümsüz oluşturma PHP kapısına takılmamalı: %d %s", kod, govde)
	}
}

func TestSSL_KurVeKaldirDenetimSatiri(t *testing.T) {
	tok := hk_tokenlar(t)
	for _, u := range []hk_uc{hk_sslKurSelf, hk_sslKaldir} {
		hk_dikisler(t, nil, nil)
		kod, govde, sd := hk_cagir(t, u, tok["admin"])
		if kod != http.StatusOK {
			t.Fatalf("%s: 200 beklenirdi: %d %s", u.ad, kod, govde)
		}
		if !sd.Yurutuldu("audit_log") {
			t.Errorf("%s: denetim satırı yazılmalı", u.ad)
		}
	}
}
