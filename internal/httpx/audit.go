package httpx

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// Denetim: panel islemlerini audit_log'a yazar (admin + bayi izlenebilirligi).
// Hata YUTULUR — denetim kaydi asla asil islemi bozmaz.
// actor: middleware.RolFrom/ClaimsFrom cagiran tarafta cozulur (import dongusu olmasin).
func Denetim(db *sql.DB, r *http.Request, uid int64, kullanici, eylem, hedef, detay string, basarili bool) {
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
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, detail, ok)
		 VALUES(?,?,?,?,?,?,?)`,
		uidVal, kullanici, ClientIP(r), eylem, hedef, detayVal, ok)
}
