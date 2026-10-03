package pano

// esik.go — kenar-tetikli eşik kovaları (saf, DB'siz; esik_test.go).
//
// Kalıcı bir durum (disk %92) her turda "aşıldı" demez: bildirim YALNIZ kova
// yukarı geçtiğinde üretilir. Aşağı iniş histerezisle olur (eşiğin hemen
// altında salınan kullanım bildirim yağdırmaz). Okundu durumuna bakılmaz.

// Kota kovaları: %85 (uyarı) ve %100 (kritik). Plan: "yakında dolacak = %85+".
var kotaEsikleri = []int{85, 100}

// kotaHisterezis — aşağı inişte kovanın altına bu kadar puan düşmek gerekir
// (100 → 85 için < %95, 85 → 0 için < %80).
const kotaHisterezis = 5.0

// kotaKovasi — önceki kova ve güncel yüzdeye göre yeni kova.
func kotaKovasi(onceki int, yuzde float64) int {
	ham := 0
	for _, e := range kotaEsikleri {
		if yuzde >= float64(e) {
			ham = e
		}
	}
	if ham >= onceki {
		return ham
	}
	// Aşağı: önceki kovanın histerezis sınırının altına inmedikçe kal.
	if yuzde >= float64(onceki)-kotaHisterezis {
		return onceki
	}
	return ham
}

// SSL seviyeleri (yüksek = kötü). Let's Encrypt sertifikası bitişe 30 gün kala
// yenilenir; 14 günün altına inmişse yenileme başarısız demektir.
const (
	sslNormal   = 0
	sslYakin    = 1 // ≤ 14 gün
	sslKritik   = 2 // ≤ 3 gün
	sslDoldu    = 3 // süresi geçmiş
	sslSifirGun = 20
)

// sslHam — kalan güne göre ham seviye.
func sslHam(kalanGun int) int {
	switch {
	case kalanGun < 0:
		return sslDoldu
	case kalanGun <= 3:
		return sslKritik
	case kalanGun <= 14:
		return sslYakin
	}
	return sslNormal
}

// sslSeviyesi — önceki seviye ve kalan güne göre yeni seviye. Aşağı iniş
// (yenilendi) yalnız kalan gün sslSifirGun'ü aşınca: 15. günde geri sayıp 14'te
// yeniden bildirim yağdırmaz.
func sslSeviyesi(onceki, kalanGun int) int {
	ham := sslHam(kalanGun)
	if ham >= onceki {
		return ham
	}
	if kalanGun > sslSifirGun {
		return sslNormal
	}
	return onceki
}

// yukariGecti — bildirim üretilmeli mi.
func yukariGecti(onceki, yeni int) bool { return yeni > onceki }
