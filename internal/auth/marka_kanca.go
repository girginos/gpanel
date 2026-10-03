package auth

import "net/http"

// Marka kancaları (D2 whitelabel). internal/marka middleware→auth'u içe aktardığı
// için auth onu doğrudan çağıramaz; main.go bağlar. nil → eski davranış.
var (
	// TOTPIssuerKanca — 2FA kurulumunda authenticator'da görünen ad (marka adı).
	TOTPIssuerKanca func(r *http.Request) string
	// HostKiraciKanca — istek doğrulanmış (aktif) bir bayi özel panel adresine mi
	// geldi; (bayi users.id, true). Yalnız girişi REDDETMEK için kullanılır.
	HostKiraciKanca func(r *http.Request) (int64, bool)
)
