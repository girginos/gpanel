package httpx

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
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
	if err := denetimYaz(db, uid, kullanici, DenetimIP(r), eylem, hedef, detay, kapsam, basarili); err != nil {
		log.Printf("denetim: audit_log yazılamadı (eylem=%s hedef=%s): %v", eylem, hedef, err)
	}
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
	if err := denetimYaz(db, uid, kullanici, "sistem", eylem, hedef, detay, kapsam, basarili); err != nil {
		log.Printf("denetim: audit_log yazılamadı (eylem=%s hedef=%s): %v", eylem, hedef, err)
	}
}

// adinaEkOnek: müşteri ADINA oturumda aktör adının eki — "aktör (adına musteri:<domain_id>)".
// Bu biçim yalnız (a) middleware.Aktor'un döndürdüğü kimlik dizgesinde ve (b) 0114
// öncesi yazılmış eski kayıtların actor_username kolonunda bulunur. Yeni kayıtlarda
// aktör adı ve "kimin adına" AYRI kolonlarda durur (actor_username + adina_domain_id).
const adinaEkOnek = " (adına musteri:"

// AdinaAktorAdi: audit_log.actor_username (VARCHAR(64)) sığacak biçimde
// "aktör (adına musteri:<id>)". Aktör adı gerekirse rune sınırında kısaltılır;
// "adına musteri:<id>" eki HİÇ kesilmez (kaydın anlamı odur). Ayrıştırıcısı AktorAyir.
func AdinaAktorAdi(aktor string, domainID int64) string {
	ek := adinaEkOnek + strconv.FormatInt(domainID, 10) + ")"
	kalan := 64 - len([]rune(ek))
	r := []rune(aktor)
	if len(r) > kalan {
		r = r[:kalan]
	}
	return string(r) + ek
}

// AktorAyir: "aktör (adına musteri:<id>)" → ("aktör", id). Biçim tutmuyorsa (s, 0).
// Yazarken (ayrı kolona bölmek için) ve okurken (eski kayıtları aynı biçimde
// sunmak için) TEK ayrıştırıcıdır.
func AktorAyir(s string) (string, int64) {
	if !strings.HasSuffix(s, ")") {
		return s, 0
	}
	i := strings.LastIndex(s, adinaEkOnek)
	if i < 0 {
		return s, 0
	}
	n, err := strconv.ParseInt(s[i+len(adinaEkOnek):len(s)-1], 10, 64)
	if err != nil || n <= 0 {
		return s, 0
	}
	return s[:i], n
}

// denetimYaz: tek INSERT yolu. Müşteri adına yapılan işlemde aktör adı ve adına
// olunan hosting ayrı kolonlara yazılır; 0114 göçü uygulanmamış bir veritabanında
// (Unknown column) kayıt DÜŞMESİN diye eski tek-dizge biçimiyle yeniden denenir.
func denetimYaz(db *sql.DB, uid int64, kullanici, ip, eylem, hedef, detay string, kapsam int64, basarili bool) error {
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
	if aktor, adina := AktorAyir(kullanici); adina > 0 {
		_, err := db.Exec(
			`INSERT INTO audit_log(actor_user_id, actor_username, adina_domain_id, ip, action, target, detail, ok, reseller_id)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			uidVal, aktor, adina, ip, eylem, hedef, detayVal, ok, kapsam)
		if err == nil || !bilinmeyenKolon(err) {
			return err
		}
		log.Printf("denetim: audit_log.adina_domain_id yok (0114 göçü?) — kayıt eski biçimde yazılıyor")
	}
	_, err := db.Exec(
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, detail, ok, reseller_id)
		 VALUES(?,?,?,?,?,?,?,?)`,
		uidVal, kullanici, ip, eylem, hedef, detayVal, ok, kapsam)
	return err
}

// bilinmeyenKolon: MySQL/MariaDB 1054 "Unknown column".
func bilinmeyenKolon(err error) bool {
	m := err.Error()
	return strings.Contains(m, "1054") || strings.Contains(m, "Unknown column")
}
