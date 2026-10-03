package dns

import (
	"context"
	"database/sql"
	"log"
)

// Açılış onarımının dış etkili adımları (testler sahteyle değiştirir: /var/named'e
// ve named'e dokunulmadan yalnız DB tohumu doğrulanır).
var (
	onarimTohumla = SeedDefaults
	onarimZoneYaz = WriteZone
)

// EksikZoneOnar — DNS'i HİÇ tohumlanmamış aktif alan adlarını tohumlar + zone yazar.
//
// 🔴 Neden (2026-10-03, 181): hosting oluşturma DNS adımını istek bağlamıyla
// yürütüyordu; tarayıcı 30 sn'de bağlantıyı bırakınca tohum/SOA/zone "context
// canceled" ile düştü → kullanıcı/vhost/FPM'i olan ama zone'u OLMAYAN alan adı
// (girginos.cloud). Oluşturma artık kopuk bağlamla yürüyor; bu onarım o hatanın
// (ve sağlama sırasında panelin yeniden başlaması gibi benzerlerinin) bıraktığı
// yarım alan adlarını kendiliğinden tamamlar.
//
// Ölçüt "hiç tohumlanmamış": ne dns_records satırı ne dns_soa satırı var.
// SeedDefaults her zaman SOA da yazar; kaydını bilerek silen (ya da içe aktarma
// ile değiştiren) kullanıcının SOA'sı durur → onarım ONA DOKUNMAZ. Tohumlanan alan
// adının SOA'sı oluştuğu için ikinci koşu onu yeniden seçmez (idempotent).
// Demo ve pasif alan adları atlanır. ipv4 boşsa varsayilanIPv4 kullanılır.
// Dönüş: onarılan alan adı sayısı.
func EksikZoneOnar(ctx context.Context, db *sql.DB, varsayilanIPv4 string) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.id, d.alan_adi, d.ipv4 FROM domains d
		WHERE d.durum='aktif' AND d.is_demo=0
		  AND NOT EXISTS (SELECT 1 FROM dns_records r WHERE r.domain_id=d.id)
		  AND NOT EXISTS (SELECT 1 FROM dns_soa s WHERE s.domain_id=d.id)
		ORDER BY d.id`)
	if err != nil {
		return 0, err
	}
	type eksik struct {
		id         int64
		alan, ipv4 string
	}
	var liste []eksik
	for rows.Next() {
		var e eksik
		if serr := rows.Scan(&e.id, &e.alan, &e.ipv4); serr == nil {
			liste = append(liste, e)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	onarilan := 0
	for _, e := range liste {
		ip := e.ipv4
		if ip == "" {
			ip = varsayilanIPv4
		}
		n, serr := onarimTohumla(ctx, db, e.id, e.alan, ip)
		if serr != nil {
			log.Printf("dns onarım: %s (domain=%d) tohumlanamadı: %v", e.alan, e.id, serr)
			continue
		}
		if zerr := onarimZoneYaz(ctx, db, e.id); zerr != nil {
			log.Printf("dns onarım: %s (domain=%d) zone yazılamadı: %v", e.alan, e.id, zerr)
		}
		onarilan++
		log.Printf("dns onarım: %s (domain=%d) DNS'i hiç tohumlanmamıştı → %d kayıt + SOA tohumlandı", e.alan, e.id, n)
	}
	return onarilan, nil
}
