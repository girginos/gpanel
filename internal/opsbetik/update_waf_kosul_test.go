package opsbetik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// girginospanel-update, WAF'ı yalnız ZATEN kuruluysa (modül ya da ABI damgası) senkronlamalı.
// Koşulsuz çağrı, WAF istememiş her sunucuda güncelleme sırasında libmodsecurity'yi kaynaktan
// derletiyordu (2 çekirdekte ~6 dk). Kurulum betiği WAF'ı varsayılan kurmaz.
func TestUpdateWafSetupKosulludur(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "assets", "ops", "girginospanel-update"))
	if err != nil {
		t.Fatal(err)
	}
	satirlar := strings.Split(string(b), "\n")
	cagri := -1
	for i, s := range satirlar {
		ts := strings.TrimSpace(s)
		if strings.HasPrefix(ts, "#") {
			continue
		}
		if strings.HasPrefix(ts, "girginospanel-waf-setup >") {
			cagri = i
			break
		}
	}
	if cagri < 0 {
		t.Fatal("girginospanel-update içinde waf-setup çağrısı bulunamadı")
	}
	// Çağrıdan önceki 3 satır içinde modül/damga koşulu olmalı.
	pencere := strings.Join(satirlar[max0(cagri-3):cagri], "\n")
	if !strings.Contains(pencere, "ngx_http_modsecurity_module.so") || !strings.Contains(pencere, ".built_for_nginx") {
		t.Fatalf("waf-setup çağrısı 'WAF zaten kurulu' koşuluna bağlı değil:\n%s", pencere)
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
