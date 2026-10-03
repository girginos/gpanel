package kapi

// AnahtarUyumlu: API anahtarı (sahip rolü rol: "admin" | "reseller") bu kapıdan
// kabul edilir mi? JWT'nin aksine anahtar belirli bir kapıda ÜRETİLMEZ; kapı
// rolden türetilir — port değişince anahtar yenilenmek zorunda kalmaz.
//
//	istekKapi      kök anahtarı  bayi anahtarı
//	"" (kapalı)        ✓             ✓        tek port, her rol (bugünkü davranış)
//	eski               ✓             ✓        geçiş dönemi ortak port
//	admin              ✓             ✗
//	bayi               ✗             ✓
//	musteri            ✗             ✗
//	bilinmeyen         ✗             ✗        kanıtsız istek
func AnahtarUyumlu(istekKapi, rol string) bool {
	switch istekKapi {
	case "", Eski:
		return rol == "admin" || rol == "reseller"
	case Admin:
		return rol == "admin"
	case Bayi:
		return rol == "reseller"
	}
	return false
}
