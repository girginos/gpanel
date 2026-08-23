package httpx

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// Denetim: panel islemlerini audit_log'a yazar (kok + bayi izlenebilirligi).
// Hata YUTULUR — denetim kaydi asla asil islemi bozmaz.
//
// kapsam: kaydin ait oldugu kiraci (bayi users.id; 0 = kok/panel sahibi).
// Bayi listede YALNIZ kendi kapsamini gorur; kok hepsini gorur.
// actor: middleware.Aktor cagiran tarafta cozulur (import dongusu olmasin).
func Denetim(db *sql.DB, r *http.Request, uid int64, kullanici, eylem, hedef, detay string, kapsam int64, basarili bool) {
	if db == nil {
		return
	}
	var uidVal any
	if uid > 0 {
		uidVal = uid
	}
	// detail kolonunda CHECK(json_valid) var — duz metin INSERT edilirse kayit
	// SESSIZCE dusuyordu. Serbest metni her zaman JSON nesnesine sariyoruz.
	var detayVal any
	if detay != "" {
		if b, err := json.Marshal(map[string]string{"not": detay}); err == nil {
			detayVal = string(b)
		}
	}
	ok := 0
	if basarili {
		ok = 1
	}
	_, _ = db.Exec(
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, detail, ok, reseller_id)
		 VALUES(?,?,?,?,?,?,?,?)`,
		uidVal, kullanici, DenetimIP(r), eylem, hedef, detayVal, ok, kapsam)
}

// DenetimDomain: kapsami domainin sahibinden (domains.reseller_id) cozer.
// Hosting-bazli islemlerde tercih edilen yol — kaydi dogru kiraciya yazar.
func DenetimDomain(db *sql.DB, r *http.Request, uid int64, kullanici, eylem, hedef, detay string, domainID int64, basarili bool) {
	Denetim(db, r, uid, kullanici, eylem, hedef, detay, DomainKapsam(db, domainID), basarili)
}

// DomainKapsam: domainin ait oldugu kiraci (0 = kok).
func DomainKapsam(db *sql.DB, domainID int64) int64 {
	if db == nil || domainID <= 0 {
		return 0
	}
	var rid sql.NullInt64
	if err := db.QueryRow(`SELECT reseller_id FROM domains WHERE id=?`, domainID).Scan(&rid); err != nil {
		return 0
	}
	if rid.Valid {
		return rid.Int64
	}
	return 0
}

// DenetimSistem: arka plan işleri (toplu işlem, zamanlanmış görev) için denetim
// kaydı. Denetim/DenetimDomain *http.Request ister ve DenetimIP(nil) panikler;
// arka planda istek yoktur, IP "sistem" olarak yazılır.
func DenetimSistem(db *sql.DB, uid int64, kullanici, eylem, hedef, detay string, kapsam int64, basarili bool) {
	if db == nil {
		return
	}
	var uidVal any
	if uid > 0 {
		uidVal = uid
	}
	var detayVal any
	if detay != "" {
		if b, err := json.Marshal(map[string]string{"not": detay}); err == nil {
			detayVal = string(b)
		}
	}
	ok := 0
	if basarili {
		ok = 1
	}
	_, _ = db.Exec(
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, detail, ok, reseller_id)
		 VALUES(?,?,?,?,?,?,?,?)`,
		uidVal, kullanici, "sistem", eylem, hedef, detayVal, ok, kapsam)
}
