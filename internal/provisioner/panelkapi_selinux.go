package provisioner

// SELinux port etiketleri — kapı portları (ADR-GP-001).
//
// 🔴 nginx (httpd_t) yalnız http_port_t etiketli portlara bind edebilir; rastgele kapı
// portları varsayılan politikada unreserved_port_t'dir. Enforcing sunucuda (AlmaLinux 10
// varsayılanı) etiketsiz bir porta yazılan listen'da reload sessizce düşer, sonraki nginx
// restart'ı ise HİÇ açılmaz → tüm siteler kapanır. `nginx -t` bunu yakalamaz. Bu yüzden
// portlar listen'a yazılmadan ÖNCE etiketlenir (ayrıntı: portyonetim/selinux.go).

import (
	"log"

	"girginospanel/internal/portyonetim"
)

// Dikişler (testler SELinux'u kapalı varsayar).
var (
	kapiSELinuxModu   = portyonetim.SELinuxModu
	kapiSELinuxListe  = portyonetim.SELinuxPortListesi
	kapiSELinuxEtiket = portyonetim.SELinuxHttpEtiketle
	// kapiEtiketli: bu süreçte http_port_t olduğu doğrulanan portlar. kapiMu altında
	// kullanılır (yalnız kapiUzlastir'dan çağrılır); her uzlaştırmada semanage koşmasın.
	kapiEtiketli = map[int]bool{}
)

// kapiPortlariEtiketle: nginx'in bind edeceği kapı portlarını SELinux'ta http_port_t yapar.
//   - tamam=false → Enforcing'de etiketlenemeyen port var: listen YAZILMAMALI.
//     Permissive'de etiketleme hatası engellemez (bind serbest), yalnız günlüğe düşer.
//   - yeni=true → bu çağrıda etiket eklendi: dosyalar değişmese de reload gerekir.
//
// Başka bir servise ayrılmış portun türü DEĞİŞTİRİLMEZ (o servisi bozabilirdi); seçim
// zaten böyle portları eler (portyonetim.KapiPortDenetleyici), elle yazılan ayar için
// günlük operatöre portu değiştirmesini söyler.
func kapiPortlariEtiketle(portlar []int) (tamam, yeni bool) {
	mod := kapiSELinuxModu()
	if mod == "" {
		return true, false
	}
	var eksik []int
	for _, p := range portlar {
		if !kapiEtiketli[p] {
			eksik = append(eksik, p)
		}
	}
	if len(eksik) == 0 {
		return true, false
	}
	zorunlu := mod == "enforcing"
	liste, err := kapiSELinuxListe()
	if err != nil {
		log.Printf("🔴 kapi: SELinux %s ama port etiketleri okunamadı: %v", mod, err)
		return !zorunlu, false
	}
	tamam = true
	for _, p := range eksik {
		http, baska := portyonetim.SELinuxPortDurumu(liste, p)
		switch {
		case http:
			kapiEtiketli[p] = true
		case baska != "":
			log.Printf("🔴 kapi: :%d SELinux'ta %s servisine ayrılmış — http_port_t yapılmadı; bu kapı portunu değiştirin", p, baska)
			tamam = tamam && !zorunlu
		default:
			if e := kapiSELinuxEtiket(p); e != nil {
				log.Printf("🔴 kapi: :%d SELinux http_port_t yapılamadı: %v", p, e)
				tamam = tamam && !zorunlu
				continue
			}
			log.Printf("kapi: :%d SELinux'ta http_port_t olarak etiketlendi (nginx bind edebilsin)", p)
			kapiEtiketli[p] = true
			yeni = true
		}
	}
	return tamam, yeni
}
