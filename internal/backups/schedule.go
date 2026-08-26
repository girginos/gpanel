// Backup auto-scheduler: arkaplan goroutine, saatlik tick.
// Each tick: SELECT due domains, run backup, prune old by retention.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/bildirim"
)

type Schedule struct {
	Freq         string `json:"freq"`           // "none" | "daily" | "weekly"
	Hour         int    `json:"hour"`           // 0-23
	Retention    int    `json:"retention"`      // keep last N
	LastBackupAt string `json:"last_backup_at"` // RFC3339 or empty
}

func gecerliFreq(f string) bool {
	return f == "none" || f == "daily" || f == "weekly"
}

// StartScheduler: panel başlangıcında çağrılır, kendi goroutine'ini başlatır.
// Her saatin başında (~ +60s offset) due olanları tarayıp yedekler.
func StartScheduler(db *sql.DB) {
	go func() {
		// İlk run: panel başladıktan 2 dakika sonra (warmup)
		time.Sleep(2 * time.Minute)
		tickOnce(db)
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			tickOnce(db)
		}
	}()
}

type dueDomain struct {
	ID        int64
	AlanAdi   string
	SK        string
	Freq      string
	Hour      int
	Retention int
	IsDemo    int
}

// TickOnce: scheduler tick'i tek seferlik manuel çağrı (test + operatör force-run için).
func TickOnce(db *sql.DB) { tickOnce(db) }

// tickOnce: bu saat için due olan domainleri bul, yedekle, retention uygula.
func tickOnce(db *sql.DB) {
	now := time.Now()
	currentHour := now.Hour()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// 🔴 AT-REST BOZULMA TARAMASI: gunde 1 kez (04:00) her domainin en yeni yedeginin
	// saklanan sha256'sini diskteki dosyayla karsilastir. Olusturma-aninda saglam olan
	// bir yedek sonradan bozulabilir (disk bit-rot, bozuk uzak-kopya, elle-oynama). Bunu
	// SESSIZCE birakmak, kurtarma aninda "yedek var ama bozuk" felaketi demektir.
	if currentHour == 4 {
		verifyYedekBozulma(db)
	}

	// ANA SALTER + DISK KAPISI (yedek YAZMADAN once).
	// Eskiden ikisi de yoktu: otomatik yedegi topluca kapatmanin yolu yoktu ve bos alan
	// bakilmadan yazildigi icin yedekler kok diski doldurup paneli+siteleri dusurebiliyordu.
	genel := genelAyarOku(ctx, db)
	if !genel.Aktif {
		return
	}
	if sebep := diskKapisi(genel); sebep != "" {
		diskBildirimiVer(db, sebep)
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, alan_adi, sistem_kullanici,
		       COALESCE(backup_freq,'none'), COALESCE(backup_hour,3),
		       COALESCE(backup_retention,7), is_demo,
		       UNIX_TIMESTAMP(last_backup_at)
		FROM domains
		WHERE COALESCE(backup_freq,'none') != 'none'
		  AND COALESCE(backup_hour,3) = ?
		  AND is_demo = 0`,
		currentHour)
	if err != nil {
		log.Printf("backup scheduler tick query: %v", err)
		return
	}
	defer rows.Close()

	var due []dueDomain
	for rows.Next() {
		var d dueDomain
		var lastTs sql.NullInt64
		if err := rows.Scan(&d.ID, &d.AlanAdi, &d.SK, &d.Freq, &d.Hour, &d.Retention, &d.IsDemo, &lastTs); err != nil {
			log.Printf("backup scheduler scan: %v", err)
			continue
		}
		// Filtre: freq=daily ise 23 saat geçmiş olmalı; weekly ise 6.5 gün
		// (slack: gün/hafta sınırına denk gelirse kaçırmamak için)
		minSec := int64(23 * 3600)
		if d.Freq == "weekly" {
			minSec = int64(6*24*3600 + 12*3600)
		}
		if lastTs.Valid && (now.Unix()-lastTs.Int64) < minSec {
			continue
		}
		due = append(due, d)
	}

	if len(due) == 0 {
		return
	}
	log.Printf("backup scheduler: %d due domain bulundu", len(due))

	// Gecenin toplu yedeğini tek 'otomatik' iş olarak grupla (panelde tek satır + ilerleme).
	var jid int64
	if res, err := db.Exec(
		`INSERT INTO backup_jobs(tur, islem, durum, toplam, baslatan) VALUES('otomatik','yedek','calisiyor',?, 'sistem')`,
		len(due)); err == nil {
		jid, _ = res.LastInsertId()
	}
	var toplamB int64
	basari, hata := 0, 0
	for _, d := range due {
		db.Exec(`UPDATE backup_jobs SET aktif_domain=? WHERE id=?`, d.AlanAdi, jid)
		b, err := runOneBackup(db, d, jid, genel)
		if err != nil {
			hata++
			log.Printf("backup scheduler %s: %v", d.AlanAdi, err)
		} else {
			basari++
			toplamB += b
			if err := pruneOld(db, d.ID, d.SK, d.Retention); err != nil {
				log.Printf("backup retention %s: %v", d.AlanAdi, err)
			}
		}
		db.Exec(`UPDATE backup_jobs SET tamamlanan=?, basari=?, hata=?, boyut_b=? WHERE id=?`, basari+hata, basari, hata, toplamB, jid)
	}
	db.Exec(`UPDATE backup_jobs SET durum=?, aktif_domain='', bitis=NOW() WHERE id=?`, jobDurum(basari, hata), jid)
}

// runOneBackup: bir domain için backup üret (job_id ile) + last_backup_at. Boyut döner.
func runOneBackup(db *sql.DB, d dueDomain, jobID int64, genel *GenelAyar) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	boyut, dosya, err := birDomainYedekle(ctx, db, d.ID, d.SK, "oto", "Otomatik yedek ("+d.Freq+")", jobID)
	if err != nil {
		return 0, err
	}
	// NOT: sistem geneli uzak yukleme birDomainYedekle icinde yapilir (ortak cekirdek)
	// — manuel toplu is de ayni yoldan gecsin diye. Burada TEKRAR tetiklenmez.
	_ = genel
	_ = dosya
	db.Exec(`UPDATE domains SET last_backup_at=NOW() WHERE id=?`, d.ID)
	log.Printf("backup auto %s: boyut=%d", d.AlanAdi, boyut)
	return boyut, nil
}

// pruneOld: en yeni N yedek kalsın, geri kalan tüm 'oto' tipli yedekleri sil (manuel yedek korunur).
func pruneOld(db *sql.DB, domainID int64, sk string, retention int) error {
	if retention < 1 {
		retention = 1
	}
	rows, err := db.Query(
		`SELECT id, dosya FROM backups
		 WHERE domain_id=? AND tip='oto'
		 ORDER BY id DESC`, domainID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		ID    int64
		Dosya string
	}
	var all []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Dosya); err != nil {
			continue
		}
		all = append(all, it)
	}
	rows.Close()
	if len(all) <= retention {
		return nil
	}
	// En yeni N tut, geri kalan sil
	old := all[retention:]
	sort.Slice(old, func(i, j int) bool { return old[i].ID < old[j].ID })
	for _, it := range old {
		yol := filepath.Join(BackupRoot, sk, it.Dosya)
		_ = os.Remove(yol)
		_, _ = db.Exec(`DELETE FROM backups WHERE id=?`, it.ID)
	}
	log.Printf("backup retention domain=%d: %d eski yedek silindi (keep %d)", domainID, len(old), retention)
	return nil
}

// verifyYedekBozulma: her domainin EN YENI (sha256'li) yedeginin saklanan checksum'ini
// diskteki dosyayla karsilastirir. Uyusmazlik VEYA dosya-okunamiyor → dogrulama='bozuk'
// + KRITIK bildirim (domain sahibine yonlendirilir). Gunde 1 kez cagrilir (04:00).
//
// 🔴 NEDEN: gzip -t olusturma aninda saglamligi dogrular; ama bir yedek AYLARCA diskte/
// uzak-hedefte durur ve bit-rot/bozuk-kopya/elle-oynama ile sessizce bozulabilir. Bunu
// yalniz restore aninda kesfetmek = veri kaybi. Proaktif tarama + bildirim sarttir.
func verifyYedekBozulma(db *sql.DB) (int, int) {
	rows, err := db.Query(`
		SELECT b.id, b.domain_id, d.sistem_kullanici, b.dosya, b.sha256
		FROM backups b
		JOIN domains d ON d.id = b.domain_id
		JOIN (SELECT domain_id, MAX(id) AS mid FROM backups WHERE sha256 <> '' GROUP BY domain_id) x
		  ON x.mid = b.id`)
	if err != nil {
		log.Printf("yedek bozulma taramasi query: %v", err)
		return 0, 0
	}
	type kayit struct {
		id, domainID   int64
		sk, dosya, sha string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.domainID, &k.sk, &k.dosya, &k.sha) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()

	bozuk := 0
	uzakta := 0
	for _, k := range liste {
		if !strings.HasPrefix(k.sk, "c_") {
			continue
		}
		yol := filepath.Join(BackupRoot, k.sk, k.dosya)
		suanki, err := dosyaSha256(yol)
		if err != nil {
			// 🔴 "Yerelde yok" ile "BOZUK" ayni sey DEGILDIR. uzak_yerel_sil acikken
			// yedek TASARIM GEREGI yalniz uzak hedefte durur; eskiden tarama bunlari
			// tek tek "bozuk" damgalayip KRITIK bildirim gonderiyordu (uretimde 27
			// saglam yedek icin tetiklenecekti). Sonuc: alarm korlugu — gercek bir
			// bit-rot ayni metinle gelir ve gozden kacar.
			if os.IsNotExist(err) && uzakKopyaVar(db, k.dosya) {
				uzakta++
				db.Exec(`UPDATE backups SET dogrulama='uzakta' WHERE id=? AND dogrulama<>'bozuk'`, k.id)
				continue
			}
			db.Exec(`UPDATE backups SET dogrulama='bozuk' WHERE id=?`, k.id)
			bildirim.Yaz(db, "kritik", "yedek", "Yedek dosyası kayıp/okunamıyor",
				fmt.Sprintf("%s: en yeni yedek dosyası diskte okunamadı (%s) — kurtarma için GEÇERSİZ olabilir: %v", k.sk, k.dosya, err),
				k.domainID, "backup", k.id)
			bozuk++
			continue
		}
		if suanki != k.sha {
			db.Exec(`UPDATE backups SET dogrulama='bozuk' WHERE id=?`, k.id)
			bildirim.Yaz(db, "kritik", "yedek", "Yedek BOZULMUŞ (bit-rot)",
				fmt.Sprintf("%s: en yeni yedek (%s) checksum'ı oluşturma anındakiyle UYUŞMUYOR — dosya sonradan bozulmuş; bu yedekten geri yükleme YAPILAMAYABİLİR.", k.sk, k.dosya),
				k.domainID, "backup", k.id)
			bozuk++
		}
	}
	if bozuk > 0 {
		log.Printf("🔴 yedek bozulma taramasi: %d/%d domainin en yeni yedegi BOZUK (%d uzak hedefte, taranmadi)", bozuk, len(liste), uzakta)
	} else {
		log.Printf("yedek bozulma taramasi: %d domain temiz (%d uzak hedefte, taranmadi)", len(liste)-uzakta, uzakta)
	}
	return len(liste), bozuk
}
