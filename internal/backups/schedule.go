// Backup auto-scheduler: arkaplan goroutine, saatlik tick.
// Each tick: SELECT due domains, run backup, prune old by retention.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Schedule struct {
	Freq         string `json:"freq"`            // "none" | "daily" | "weekly"
	Hour         int    `json:"hour"`            // 0-23
	Retention    int    `json:"retention"`       // keep last N
	LastBackupAt string `json:"last_backup_at"`  // RFC3339 or empty
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
	ID       int64
	AlanAdi  string
	SK       string
	Freq     string
	Hour     int
	Retention int
	IsDemo   int
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

	for _, d := range due {
		if err := runOneBackup(db, d); err != nil {
			log.Printf("backup scheduler %s: %v", d.AlanAdi, err)
			continue
		}
		if err := pruneOld(db, d.ID, d.SK, d.Retention); err != nil {
			log.Printf("backup retention %s: %v", d.AlanAdi, err)
		}
	}
}

// runOneBackup: bir domain için backup üret + DB'ye kaydet + last_backup_at güncelle.
func runOneBackup(db *sql.DB, d dueDomain) error {
	if !strings.HasPrefix(d.SK, "c_") {
		return fmt.Errorf("güvensiz sk: %s", d.SK)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(BackupRoot, d.SK)
	_ = os.MkdirAll(dir, 0700)
	dosya := fmt.Sprintf("%s-auto-%s.tar.gz", d.SK, stamp)
	abs := filepath.Join(dir, dosya)
	sqlDump := filepath.Join(dir, dosya+".sql")

	dbName := d.SK + "_main"
	_ = exec.Command("bash", "-c",
		fmt.Sprintf("mysqldump --single-transaction %s > %s 2>&1 || true", dbName, sqlDump)).Run()

	args := []string{"czf", abs, "-C", "/home", d.SK, "-C", dir, dosya + ".sql"}
	if out, err := exec.Command("tar", args...).CombinedOutput(); err != nil {
		_ = os.Remove(sqlDump)
		return fmt.Errorf("tar: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = os.Remove(sqlDump)

	st, _ := os.Stat(abs)
	var boyut int64
	if st != nil {
		boyut = st.Size()
	}

	if _, err := db.Exec(
		`INSERT INTO backups(domain_id, tip, dosya, boyut_b, notlar) VALUES(?,?,?,?,?)`,
		d.ID, "oto", dosya, boyut, "Otomatik yedek ("+d.Freq+")"); err != nil {
		return fmt.Errorf("DB kayıt: %w", err)
	}
	if _, err := db.Exec(`UPDATE domains SET last_backup_at=NOW() WHERE id=?`, d.ID); err != nil {
		log.Printf("last_backup_at güncellenemedi: %v", err)
	}
	// Uzak hedef varsa arkaplanda yükle
	pushToDestinationAsync(db, d.ID, abs, dosya)
	log.Printf("backup auto %s: dosya=%s boyut=%d", d.AlanAdi, dosya, boyut)
	return nil
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
