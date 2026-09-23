package kapi

import (
	"fmt"
	"net/http"
	"testing"
)

// testGizli: testlerin paylaşılan proxy gizlisi (üretimde 64 hex karakter).
const testGizli = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var testSecenek = Secenek{Gizli: testGizli}

// testOrtami: nginx kanıtı, proxy gizlisi, istemci IP ve erişim modu dikişlerini test
// değerlerine bağlar (masaüstü modu kapalı); test bitince geri yükler. guven/ip testin
// ortasında değiştirilebilir.
func testOrtami(t *testing.T) (guven *bool, ip *string) {
	t.Helper()
	g, i := true, "203.0.113.7"
	eskiN, eskiG, eskiI, eskiS, eskiM := nginxtenMi, proxyGizli, istemciIP, ZincirKopuk, masaustuModu
	t.Cleanup(func() {
		nginxtenMi, proxyGizli, istemciIP, ZincirKopuk, masaustuModu = eskiN, eskiG, eskiI, eskiS, eskiM
		ZorlamaAyarla(false)
	})
	nginxtenMi = func(*http.Request) bool { return g }
	proxyGizli = func() string { return testGizli }
	istemciIP = func(*http.Request) string { return i }
	masaustuModu = func() bool { return false }
	return &g, &i
}

// kapiBasligi: nginx'in KapiSatiri ile yazdığı biçimde başlık değeri.
func kapiBasligi(kapi string, port int) string {
	return fmt.Sprintf("%s %d %s", kapi, port, testGizli)
}
