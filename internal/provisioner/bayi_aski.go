package provisioner

import (
	"database/sql"
	"fmt"
	"log"
)

// BayiAskiUygula: bir bayinin TUM hosting hesaplarina askiyi zincirleme uygular
// ya da kaldirir.
//
// 🔴 GERI ACARKEN yalniz askida_kaynak='bayi' olanlar acilir: tek tek (manuel)
// askiya alinmis hesaplar, bayi geri acilinca yanlislikla yayina donmez.
// Demo abonelikler zincire GIRMEZ (askiToggle de reddediyor).
// Doner: (basarili, basarisiz, hata). Basarisiz > 0 ise DB ile nginx'in durumu
// AYRISMIS demektir — cagiran bunu kullaniciya ve denetim kaydina yansitmali.
func BayiAskiUygula(db *sql.DB, resellerID int64, askida bool) (int, int, error) {
	if db == nil || resellerID <= 0 {
		return 0, 0, nil
	}
	sorgu := `SELECT id, sistem_kullanici FROM domains
	           WHERE reseller_id=? AND COALESCE(is_demo,0)=0 AND COALESCE(askida,0)=0`
	if !askida {
		sorgu = `SELECT id, sistem_kullanici FROM domains
		          WHERE reseller_id=? AND COALESCE(askida,0)=1 AND askida_kaynak='bayi'`
	}
	rows, err := db.Query(sorgu, resellerID)
	if err != nil {
		return 0, 0, err
	}
	type hedef struct {
		id int64
		sk string
	}
	var liste []hedef
	for rows.Next() {
		var t hedef
		if err := rows.Scan(&t.id, &t.sk); err == nil {
			liste = append(liste, t)
		}
	}
	okumaHatasi := rows.Err()
	_ = rows.Close()
	if okumaHatasi != nil {
		// Liste yarim okunduysa devam etmek "yarisi askiya alindi, hepsi tamam"
		// yanilgisi uretir — hic dokunmadan hata don.
		return 0, 0, fmt.Errorf("bayi %d hosting listesi eksik okundu: %w", resellerID, okumaHatasi)
	}

	ak, durum, kaynak, ftpDurum := 0, "aktif", "", "active"
	if askida {
		ak, durum, kaynak, ftpDurum = 1, "pasif", "bayi", "suspended"
	}
	basarili, basarisiz := 0, 0
	for _, t := range liste {
		// Liste anlik-goruntu: domain bu arada SILINMIS olabilir. RowsAffected=0
		// ise satir yok → vhost'u yeniden yazma (silinen domainin conf'unu geri
		// getirip yetim server_name birakiyordu) ve basarili/basarisiz sayma.
		res, err := db.Exec(`UPDATE domains SET askida=?, durum=?, askida_kaynak=? WHERE id=?`,
			ak, durum, kaynak, t.id)
		if err != nil {
			log.Printf("bayi-aski: domain %d DB guncelleme: %v", t.id, err)
			basarisiz++
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			log.Printf("bayi-aski: domain %d islem sirasinda silinmis — atlandi", t.id)
			continue
		}
		if err := RerenderVhost(db, t.id); err != nil {
			// DB "askida" diyor ama nginx hala siteyi servis ediyor → BASARILI SAYMA.
			log.Printf("bayi-aski: domain %d vhost render: %v", t.id, err)
			basarisiz++
			continue
		}
		if _, err := db.Exec(`UPDATE ftp_accounts SET status=? WHERE domain_id=?`, ftpDurum, t.id); err != nil {
			log.Printf("bayi-aski: domain %d ftp durum: %v", t.id, err)
		}
		if t.sk != "" {
			SuspendUserRuntime(t.sk, askida)
		}
		basarili++
	}
	return basarili, basarisiz, nil
}
