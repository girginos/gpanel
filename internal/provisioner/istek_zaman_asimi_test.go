package provisioner

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestIstekZamanAsimiSn(t *testing.T) {
	durumlar := []struct{ maxExec, bek int }{
		{0, 3600},    // sınırsız → tavan
		{-5, 3600},   // geçersiz → tavan
		{30, 600},    // taban
		{300, 600},   // 2×300 = 600 (plan tavanı 300)
		{301, 602},   // 2×
		{1000, 2000}, // 2×
		{1800, 3600}, // tam tavan
		{5000, 3600}, // tavana kırpılır
	}
	for _, d := range durumlar {
		if g := IstekZamanAsimiSn(d.maxExec); g != d.bek {
			t.Errorf("IstekZamanAsimiSn(%d) = %d, beklenen %d", d.maxExec, g, d.bek)
		}
	}
}

// Havuzda request_terminate_timeout TAM bir kez, [sk] bölümünde, havuzun kendi
// max_execution_time'ından türetilmiş ve ondan BÜYÜK (CPU sınırı önce devreye girsin).
func TestRenderTenantPool_IstekZamanAsimi(t *testing.T) {
	pool := renderTenantPool(nil, "c_zamanasimi", 0)
	if n := strings.Count(pool, "request_terminate_timeout"); n != 1 {
		t.Fatalf("request_terminate_timeout %d kez geçiyor, 1 olmalı:\n%s", n, pool)
	}
	m := regexp.MustCompile(`(?m)^php_admin_value\[max_execution_time\] = (\d+)$`).FindStringSubmatch(pool)
	if m == nil {
		t.Fatalf("max_execution_time satırı yok:\n%s", pool)
	}
	me, _ := strconv.Atoi(m[1])
	want := "request_terminate_timeout = " + strconv.Itoa(IstekZamanAsimiSn(me)) + "s\n"
	if !strings.Contains(pool, "\n"+want) {
		t.Fatalf("havuz %q içermeli (max_execution_time=%d):\n%s", strings.TrimSpace(want), me, pool)
	}
	if IstekZamanAsimiSn(me) <= me && me > 0 {
		t.Fatalf("duvar saati sınırı (%d) CPU sınırından (%d) büyük olmalı", IstekZamanAsimiSn(me), me)
	}
	// php_admin_value/php_value DEĞİL havuz yönergesi; [sk] başlığından sonra.
	if strings.Contains(pool, "php_admin_value[request_terminate_timeout]") || strings.Index(pool, want) < strings.Index(pool, "[c_zamanasimi]") {
		t.Fatalf("yönerge yeri/biçimi yanlış:\n%s", pool)
	}
}
