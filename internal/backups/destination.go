// Backup off-site destinations: FTP/SFTP üzerinden uzak depolama yükleme.
// lftp tek araç olarak hem FTP hem SFTP'yi tek komutla destekler.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

type Destination struct {
	ID         int64  `json:"id"`
	DomainID   int64  `json:"domain_id"`
	Tip        string `json:"tip"`    // "ftp" | "sftp"
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Kullanici  string `json:"kullanici"`
	Parola     string `json:"parola,omitempty"` // write-only: GET'te boş döner
	UzakDizin  string `json:"uzak_dizin"`
	Aktif      bool   `json:"aktif"`
	SonYukleme string `json:"son_yukleme,omitempty"`
	SonDurum   string `json:"son_durum,omitempty"`
	SonHata    string `json:"son_hata,omitempty"`
}

func gecerliTip(t string) bool { return t == "ftp" || t == "sftp" }

// readDestination: bir domain'in destinasyon kaydını döner (yoksa nil, nil).
func readDestination(ctx context.Context, db *sql.DB, domainID int64) (*Destination, error) {
	d := &Destination{DomainID: domainID}
	var aktif int
	var sonYuk sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, tip, host, port, kullanici, parola, uzak_dizin, aktif,
		        DATE_FORMAT(son_yukleme,'%Y-%m-%d %H:%i'), son_durum, son_hata
		 FROM backup_destinations WHERE domain_id=?`, domainID).
		Scan(&d.ID, &d.Tip, &d.Host, &d.Port, &d.Kullanici, &d.Parola, &d.UzakDizin,
			&aktif, &sonYuk, &d.SonDurum, &d.SonHata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Aktif = aktif == 1
	if sonYuk.Valid {
		d.SonYukleme = sonYuk.String
	}
	return d, nil
}

// lftpURL: tip + host + port'tan lftp URL'i kurar.
func lftpURL(d *Destination) string {
	if d.Tip == "sftp" {
		return fmt.Sprintf("sftp://%s:%d", d.Host, d.Port)
	}
	return fmt.Sprintf("ftp://%s:%d", d.Host, d.Port)
}

// uploadToRemote: lokal tar.gz'yi uzak hedefe yükler.
// lftp ile: connect → cd → put. SFTP için auto-confirm host key.
func uploadToRemote(ctx context.Context, d *Destination, localPath, dosyaAdi string) error {
	if !d.Aktif {
		return nil // disable: sessizce skip
	}
	url := lftpURL(d)
	// cmd:fail-exit ile herhangi bir komut başarısız olursa lftp non-zero exit eder
	script := fmt.Sprintf(
		`set cmd:fail-exit yes; `+
			`set sftp:auto-confirm yes; `+
			`set ssl:verify-certificate no; `+
			`set ftp:ssl-allow no; `+
			`set net:max-retries 1; `+
			`set net:timeout 15; `+
			`set net:reconnect-interval-base 2; `+
			`open -u "%s","%s" %s; `+
			`mkdir -p -f "%s"; `+
			`cd "%s"; `+
			`put -O . "%s"; `+
			`bye`,
		lftpEscape(d.Kullanici), lftpEscape(d.Parola), url,
		lftpEscape(d.UzakDizin), lftpEscape(d.UzakDizin), localPath)

	cmd := exec.CommandContext(ctx, "lftp", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lftp: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Output'ta dahi hata izi varsa fail say (defense in depth)
	bad := []string{"Login failed", "Access failed", "Connection refused", "Permission denied",
		"Could not resolve", "Host key verification failed", "No route to host"}
	for _, p := range bad {
		if strings.Contains(string(out), p) {
			return fmt.Errorf("lftp: %s", strings.TrimSpace(string(out)))
		}
	}
	_ = dosyaAdi
	return nil
}

// lftpEscape: lftp komut satırı içinde çift tırnak içine konacak değerleri escape eder.
func lftpEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// testConnection: kimlik bilgilerini test eder.
// SFTP için sshpass+ssh, FTP için curl — her ikisi de auth-specific exit kodu döner.
func testConnection(ctx context.Context, d *Destination) error {
	if d.Tip == "sftp" {
		// sshpass parola passwd, ssh BatchMode=no + PreferredAuthentications=password
		// publickey by-pass — kullanıcı parolasının gerçekten geçerli olduğunu garanti eder.
		host := fmt.Sprintf("%s@%s", d.Kullanici, d.Host)
		args := []string{
			"-p", d.Parola,
			"ssh",
			"-p", fmt.Sprintf("%d", d.Port),
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			"-o", "BatchMode=no",
			host, "true",
		}
		cmd := exec.CommandContext(ctx, "sshpass", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			short := strings.TrimSpace(string(out))
			if short == "" {
				short = err.Error()
			}
			return fmt.Errorf("%s", short)
		}
		return nil
	}
	// FTP — curl --user u:p ftp://host:port/  (NLST root)
	url := fmt.Sprintf("ftp://%s:%d/", d.Host, d.Port)
	args := []string{
		"-sS",
		"--connect-timeout", "10",
		"--max-time", "15",
		"--user", d.Kullanici + ":" + d.Parola,
		"--ftp-skip-pasv-ip",
		url,
	}
	cmd := exec.CommandContext(ctx, "curl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		short := strings.TrimSpace(string(out))
		if short == "" {
			short = err.Error()
		}
		return fmt.Errorf("%s", short)
	}
	return nil
}

// pushToDestinationAsync: yedek başarıyla oluştuktan sonra arkaplanda upload tetikler.
// Hata olsa bile API cevabını bloke etmez; son_durum/son_hata DB'ye yazılır.
func pushToDestinationAsync(db *sql.DB, domainID int64, localPath, dosyaAdi string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		d, err := readDestination(ctx, db, domainID)
		if err != nil || d == nil || !d.Aktif {
			return
		}
		if err := uploadToRemote(ctx, d, localPath, dosyaAdi); err != nil {
			short := err.Error()
			if len(short) > 500 {
				short = short[:500]
			}
			_, _ = db.Exec(`UPDATE backup_destinations
				SET son_durum='hata', son_hata=?, son_yukleme=NOW() WHERE domain_id=?`,
				short, domainID)
			log.Printf("backup destination upload domain=%d: %v", domainID, err)
			return
		}
		_, _ = db.Exec(`UPDATE backup_destinations
			SET son_durum='basarili', son_hata='', son_yukleme=NOW() WHERE domain_id=?`,
			domainID)
		log.Printf("backup destination upload domain=%d başarılı: %s", domainID, dosyaAdi)
	}()
}
