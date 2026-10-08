package toplu

// is_disa.go — toplu iş kaydı/kilidi/ilerlemesi için DIŞA AÇIK yardımcılar.
// Domain listesinden bağımsız arka plan işleri (ör. bayi silme zinciri) AYNI
// cp_toplu_isler kaydını ve AYNI "tek aktif iş" kilidini kullanır: UI'nın mevcut
// ilerleme çubuğu (GET /domains/toplu/is?id=) ve "devam eden iş" bulma
// (GET /domains/toplu/aktif) bu işlerde de çalışır, nginx reload/ACME yarışı olmaz.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
)

// ErrMesgul — başka bir toplu iş çalışıyor.
var ErrMesgul = errors.New("zaten çalışan bir toplu işlem var")

// IsAc — tek-aktif-iş kilidini alır ve 'calisiyor' kaydı açar. İPTAL EDİLEMEZ iş
// (iptalCtx'e yazılmaz): yarım kalan bayi silme tutarsız durum bırakırdı.
// Çağıran işi bitirince MUTLAKA IsBitir çağırmalıdır (panik dahil).
func IsAc(db *sql.DB, tip string, toplam int, param, baslatan string) (int64, error) {
	kilit.Lock()
	if aktifIs != 0 {
		kilit.Unlock()
		return 0, ErrMesgul
	}
	aktifIs = -1 // rezerve
	kilit.Unlock()
	res, err := db.Exec(
		"INSERT INTO cp_toplu_isler (tip, durum, toplam, parametre, baslatan) VALUES (?, 'calisiyor', ?, ?, ?)",
		tip, toplam, param, baslatan)
	if err != nil {
		kilit.Lock()
		aktifIs = 0
		kilit.Unlock()
		return 0, err
	}
	id, _ := res.LastInsertId()
	kilit.Lock()
	aktifIs = id
	kilit.Unlock()
	return id, nil
}

// IsIlerle — ilerleme çubuğu için sayaçları yazar.
func IsIlerle(db *sql.DB, isID int64, tamamlanan, basari, hata int, aktif string) {
	if _, e := db.Exec("UPDATE cp_toplu_isler SET tamamlanan=?, basari=?, hata=?, aktif_domain=? WHERE id=?",
		tamamlanan, basari, hata, aktif, isID); e != nil {
		log.Printf("toplu: ilerleme yazılamadı (is=%d): %v", isID, e)
	}
}

// IsBitir — durum: tamam | kismi | hata. Kilidi bırakır.
func IsBitir(db *sql.DB, isID int64, durum string, detay []string) {
	dj, _ := json.Marshal(detay)
	if _, e := db.Exec("UPDATE cp_toplu_isler SET durum=?, aktif_domain='', detay=?, bitis=NOW() WHERE id=?",
		durum, string(dj), isID); e != nil {
		log.Printf("toplu: iş sonucu yazılamadı (is=%d): %v", isID, e)
	}
	kilit.Lock()
	if aktifIs == isID {
		aktifIs = 0
	}
	kilit.Unlock()
}

// AskidakileriKapat — süreç yeniden başlamadan önce 'calisiyor' kalmış işler artık
// çalışmıyor (goroutine öldü): sonsuza dek dönen ilerleme çubuğu olmasın. Açılışta bir kez.
func AskidakileriKapat(db *sql.DB) {
	if _, e := db.Exec("UPDATE cp_toplu_isler SET durum='hata', aktif_domain='', bitis=NOW(), " +
		"detay=JSON_ARRAY('panel yeniden başladı; iş yarım kaldı — sonucu kontrol edip gerekirse tekrarlayın') " +
		"WHERE durum='calisiyor'"); e != nil {
		log.Printf("toplu: askıdaki işler kapatılamadı: %v", e)
	}
}
