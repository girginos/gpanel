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

// RedisParolalariSifrele: cp_domain_redis icindeki HENUZ duz-metin Redis ACL
// parolalarini at-rest sifreler. ParolalariSifrele ile ayni sozlesme: idempotent
// (sifreli olana dokunmaz), anahtar uretilemiyorsa satir BOZULMAZ, tablo yoksa
// sessiz cikar (eski kurulum). Baglam = satirin sk'si (bkz. redis.Durum cozumu).
func RedisParolalariSifrele(db *sql.DB) {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT domain_id, sk, redis_pass FROM cp_domain_redis WHERE redis_pass<>''`)
	if err != nil {
		return
	}
	type kayit struct {
		id  int64
		sk  string
		par string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.sk, &k.par) == nil && !SifreliMi(k.par) {
			liste = append(liste, k)
		}
	}
	_ = rows.Close()
	n := 0
	for _, k := range liste {
		kapali := SaklaGecis(k.par, k.sk)
		if kapali == k.par {
			continue // anahtar yok → gecis atlanir, duz metin korunur
		}
		if _, err := db.Exec(`UPDATE cp_domain_redis SET redis_pass=? WHERE domain_id=?`, kapali, k.id); err == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("gizli: %d redis parolasi at-rest sifrelendi", n)
	}
}
