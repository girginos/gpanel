package gizli

import (
	"database/sql"
	"log"
)

// ParolalariSifrele: db_accounts icindeki HENUZ duz-metin parolalari at-rest
// sifreler. Idempotent (sifreli olana dokunmaz), Init'ten cagrilir.
// Anahtar uretilemiyorsa Sakla degeri aynen dondurur → satir BOZULMAZ.
func ParolalariSifrele(db *sql.DB) {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT id, db_user, db_pass_plain FROM db_accounts WHERE db_pass_plain<>''`)
	if err != nil {
		return
	}
	type kayit struct {
		id  int64
		kul string
		par string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.kul, &k.par) == nil && !SifreliMi(k.par) {
			liste = append(liste, k)
		}
	}
	_ = rows.Close()
	n := 0
	for _, k := range liste {
		kapali := SaklaGecis(k.par, k.kul)
		if kapali == k.par {
			continue // anahtar yok → gecis atlanir, duz metin korunur
		}
		if _, err := db.Exec(`UPDATE db_accounts SET db_pass_plain=? WHERE id=?`, kapali, k.id); err == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("gizli: %d veritabani parolasi at-rest sifrelendi", n)
	}
}
