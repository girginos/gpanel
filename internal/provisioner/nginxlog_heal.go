package provisioner

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// HealNginxLogPerms — nginx erisim/hata loglarini KIRACIYA KAPATIR.
//
// 🔴 Kok sorun: nginx `/var/log/nginx/<domain>.access.log|.error.log` dosyalarini
// 0644 (dunya-okunur) + dizini 0711 (other=x) olarak birakiyor. Shell erisimi olan
// bir kiraci, dosya adi domain'den deterministik oldugu icin KOMSUSUNUN HTTP
// loglarini `cat /var/log/nginx/<komsu>.access.log` ile okuyabiliyordu — ziyaretci
// IP'leri, query-string'deki session/token/API-anahtari sizabilir.
//
// Cozum: dizini 0700 yap (other=x kaldir → kiraci ISME GORE bile dosyaya
// erisemez, cunku dizin gecisi engellenir) + dosyalari 0640'a indir (derinlemesine
// savunma). Panel ve logrotate root oldugu icin (DAC bypass) etkilenmez.
// Acilista + idempotent; nginx/logrotate/paket guncellemesi izni sifirlarsa bir
// sonraki panel yeniden baslatmada tekrar uygulanir.
func HealNginxLogPerms() {
	const dir = "/var/log/nginx"
	fi, err := os.Stat(dir)
	if err != nil {
		return
	}
	// Dizin: yalnizca root gezebilsin (0700). Bu, kiraci ISME-GORE erisimini de keser.
	if fi.Mode().Perm() != 0o700 {
		if e := os.Chmod(dir, 0o700); e != nil {
			log.Printf("nginx log dizini izni sertlestirilemedi: %v", e)
		} else {
			log.Printf("nginx log dizini 0700 yapildi (kiraci cross-tenant log okumasi kapatildi)")
		}
	}
	// Dosyalar: dunya-okunur bitini kaldir (0640). Derinlemesine savunma.
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ad := e.Name()
		if !strings.HasSuffix(ad, ".log") && !strings.Contains(ad, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode().Perm()&0o007 == 0 {
			continue // zaten other-erisimi yok
		}
		_ = os.Chmod(filepath.Join(dir, ad), 0o640)
	}
}
