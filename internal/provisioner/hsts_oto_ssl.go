package provisioner

import "strings"

// ── HTTPS-zorunlu (HSTS preload) TLD'ler ────────────────────────────────────
// 🔴 NEDEN AYRI ELE ALINIYOR: Bu uzantilar tarayicilarin HSTS preload listesinde
// TLD OLARAK yer alir. Yani tarayici bu alan adlarina ASLA http:// ile gitmez ve
// sertifika hatasinda "yine de devam et" secenegini GOSTERMEZ (Chrome: "You cannot
// visit ... because the website uses HSTS"). Sonuc: sertifika yoksa site
// TAMAMEN ERISILMEZ — kendinden imzali sertifika da kurtarmaz.
//
// Bu yuzden bu TLD'lerde domain kurulur kurulmaz Let's Encrypt DENENIR; boylece
// musteri "sitem acilmiyor" demeden once gercek sertifika yerinde olur.
var httpsZorunluTLD = map[string]bool{
	"app": true, "dev": true, "page": true, "new": true, "chrome": true,
	"google": true, "gle": true, "bank": true, "insurance": true,
	"foo": true, "zip": true, "mov": true, "boo": true, "day": true,
	"rsvp": true, "meme": true, "ing": true, "nexus": true, "prof": true,
	"esq": true, "phd": true, "dad": true, "channel": true, "search": true,
}

// HTTPSZorunluMu: alan adinin uzantisi tarayicilarca HTTPS-zorunlu mu?
// (panel UI'i de bu bilgiyi kullanip uyari gosterir)
func HTTPSZorunluMu(alanAdi string) bool {
	i := strings.LastIndex(alanAdi, ".")
	if i < 0 || i+1 >= len(alanAdi) {
		return false
	}
	return httpsZorunluTLD[strings.ToLower(alanAdi[i+1:])]
}

// OtoSSLGerekliyse: geriye donuk sarmalayici — bkz. OtoSSLDene (proxy arkasini da kapsar).
func OtoSSLGerekliyse(alanAdi, sk, phpSurum, backend string) (string, string, bool) {
	return OtoSSLDene(alanAdi, sk, phpSurum, backend)
}
