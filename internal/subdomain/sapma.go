package subdomain

// Alt alan erişim kısıtlama sapma nöbetçisi.
//
// 🔴 NEDEN AYRI BİR DOSYA/DÖNGÜ: `provisioner.HealErisimSapmasi` yalnız
// `dom_<sk>.conf` okuyor; alt alan vhost'ları `sub_<sk>_<alt>.conf` adında ve
// yeniden render'ları bu pakette. Denetimde ölçüldü — nöbetçinin var olma
// gerekçesi olan senaryo alt alanda AYNEN yaşıyordu:
//
//   DB satırları API dışından silindi → panel "kısıt yok" diyor, site 403,
//   panel yeniden başlatıldı → nöbetçi TEK KELİME etmedi, site 403'te kaldı.
//
//   Ters yön daha kötü: DB'de aktif=1 iken blok vhost'tan elle silinince
//   panel "kısıtlı" gösterirken site HERKESE AÇIK kalıyordu.
//
// Nöbetçi DB'ye değil diskteki gerçek dosyaya bakar ve içeriği karşılaştırır.

import (
	"context"
	"database/sql"
	"log"
	"os"

	"girginospanel/internal/provisioner"
)

// HealAltAlanSapmasi — alt alan vhost'ları ile DB'yi karşılaştırır, tutmayanı
// yeniden render eder.
func HealAltAlanSapmasi(db *sql.DB) {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT s.id, s.alt_ad, s.tam_ad, COALESCE(s.php_surum,'8.3'), d.sistem_kullanici
		FROM subdomanlar s JOIN domains d ON d.id = s.domain_id
		WHERE COALESCE(d.askida,0)=0`)
	if err != nil {
		return
	}
	type alt struct {
		id                    int64
		altAd, tamAd, php, sk string
	}
	var liste []alt
	for rows.Next() {
		var a alt
		if rows.Scan(&a.id, &a.altAd, &a.tamAd, &a.php, &a.sk) == nil {
			liste = append(liste, a)
		}
	}
	rows.Close()

	duzeltilen := 0
	for _, a := range liste {
		yol := confPath(a.sk, a.altAd)
		b, e := os.ReadFile(yol)
		if e != nil {
			continue // vhost yok — deprovision edilmiş ya da henüz yazılmamış
		}
		beklenen, berr := provisioner.AltAlanErisimBloku(db, a.id)
		if berr != nil {
			// Okunamıyorsa DOKUNMA: diskteki yapılandırma korumalı olabilir.
			log.Printf("alt alan erisim sapma kontrolu atlandi (%s): %v", a.tamAd, berr)
			continue
		}
		if !provisioner.ErisimSapmasiVar(string(b), beklenen) {
			continue
		}
		log.Printf("🔴 alt alan erisim kisitlama SAPMASI: %s — dosya ile DB tutmuyor — yeniden render ediliyor", a.tamAd)
		if e := rebuildVhostDB(context.Background(), db, a.id, a.sk, a.altAd, a.tamAd, a.php); e != nil {
			log.Printf("🔴 %s yeniden render edilemedi: %v", a.tamAd, e)
			continue
		}
		duzeltilen++
	}
	if duzeltilen > 0 {
		log.Printf("✓ alt alan erisim kisitlama sapmasi duzeltildi: %d alt alan", duzeltilen)
	}
}
