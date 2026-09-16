//go:build linux

package platform

// 🔴 SURUM VE KANAL PLATFORM BASINA AYRI.
// Ortak bir surum sabiti kullansaydik Linux yayini Windows surumunu de
// oynatir, "bagimsiz guncelleme" sartini daha ilk gunde kirardi.
// Bu iki sabit BIRBIRINE BAGLANMAMALI.
const (
	Surum = "0.2.0"
	Kanal = "linux/stable"
)
