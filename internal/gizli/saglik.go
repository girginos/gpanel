package gizli

import (
	"database/sql"
	"log"
	"time"
)

// SaglikKontrol: acilista anahtarin GERCEKTEN calistigini dogrular. Anahtar dosyasi
// kaybolur/degisirse eski kayitlar cozulemez ve panel "parola" diye CIPHERTEXT
// dagitmaya baslar — bu sessizce olmasin, acik uyari birakilsin.
func SaglikKontrol(db *sql.DB) {
	if db == nil {
		return
	}
	var kul, sifreli string
	if err := db.QueryRow(`SELECT db_user, db_pass_plain FROM db_accounts
	                        WHERE db_pass_plain LIKE 'gos1:%' LIMIT 1`).Scan(&kul, &sifreli); err != nil {
		return // sifreli kayit yok → kontrol edilecek bir sey de yok
	}
	if Coz(sifreli) == sifreli && CozBagli(sifreli, kul) == sifreli {
		log.Printf("gizli: UYARI — sifreli DB parolalari COZULEMIYOR. " +
			"/etc/girginospanel/db.key kayip ya da degismis olabilir; phpMyAdmin ve " +
			"parola gosterimi hatali calisir. Anahtari yedekten geri yukleyin.")
	}
}

// PMATokenSupur: suresi gecmis / kullanilmis phpMyAdmin token satirlarini duzenli
// siler. Eskiden temizlik YALNIZ yeni token istegi geldiginde yapiliyordu; trafik
// olmayan kurulumda satirlar birikiyordu.
func PMATokenSupur(db *sql.DB, aralik time.Duration) {
	if db == nil {
		return
	}
	go func() {
		t := time.NewTicker(aralik)
		defer t.Stop()
		for {
			if _, err := db.Exec(`DELETE FROM pma_tokens WHERE son_kullanma < NOW() OR kullanildi=1`); err != nil {
				log.Printf("pma token supurme: %v", err)
			}
			<-t.C
		}
	}()
}
