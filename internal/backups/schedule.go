// Backup auto-scheduler: arkaplan goroutine, saatlik tick.
// Each tick: SELECT due domains, run backup, prune old by retention.
package backups

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
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
		b, err := runOneBackup(db, d, jid)
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
func runOneBackup(db *sql.DB, d dueDomain, jobID int64) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	boyut, _, err := birDomainYedekle(ctx, db, d.ID, d.SK, "oto", "Otomatik yedek ("+d.Freq+")", jobID)
	if err != nil {
		return 0, err
	}
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
