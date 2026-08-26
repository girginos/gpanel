// Backup off-site destinations: FTP/SFTP üzerinden uzak depolama yükleme.
// lftp tek araç olarak hem FTP hem SFTP'yi tek komutla destekler.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Destination struct {
	ID         int64  `json:"id"`
	DomainID   int64  `json:"domain_id"`
	Tip        string `json:"tip"` // "ftp" | "sftp"
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

	cmd, temizle, err := lftpKomutu(ctx, script)
	if err != nil {
		return err
	}
	defer temizle()
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

// lftpKomutu: lftp betigini 0600 GECICI DOSYAYA yazip `lftp -f <dosya>` ile calistirir.
//
// 🔴 Neden: `lftp -c "<betik>"` betigi ARGV'ye koyar; betikte `open -u kullanici,PAROLA`
// oldugu icin parola `ps aux` ile herkese gorunur. Sunucuda /proc hidepid'siz bagli
// oldugundan bunu HERHANGI bir kiraci okuyabiliyordu. Dosya root'a ait 0600 olunca
// icerik kiracilara kapali; argv'de yalniz dosya yolu kalir.
func lftpKomutu(ctx context.Context, betik string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "gosp-lftp-*.cmd")
	if err != nil {
		return nil, func() {}, fmt.Errorf("lftp betigi: %w", err)
	}
	yol := f.Name()
	temizle := func() { _ = os.Remove(yol) }
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		temizle()
		return nil, func() {}, fmt.Errorf("lftp betigi izin: %w", err)
	}
	// lftp -f dosyayi SATIR SATIR isler; tek satirlik ";" ayrilmis betik de gecerlidir.
	if _, err := f.WriteString(betik + "\n"); err != nil {
		_ = f.Close()
		temizle()
		return nil, func() {}, fmt.Errorf("lftp betigi yazma: %w", err)
	}
	_ = f.Close()
	return exec.CommandContext(ctx, "lftp", "-f", yol), temizle, nil
}

// lftpEscape: lftp komut satırı içinde çift tırnak içine konacak değerleri escape eder.
func lftpEscape(s string) string {
	// Kontrol karakterleri (satir enjeksiyonu) — girdi katmani da reddeder, burada da temizle.
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// testConnection: kimlik bilgilerini test eder.
// SFTP için sshpass+ssh, FTP için curl — her ikisi de auth-specific exit kodu döner.
func testConnection(ctx context.Context, d *Destination) error {
	if d.Tip == "sftp" {
		// 🔴 SFTP ALTSISTEMI ile test et, kabuk komutu ile DEGIL. Eskiden
		// `ssh ... true` calistiriliyordu; Hetzner Storage Box gibi SFTP-ONLY
		// hedefler kabuk exec'ine izin VERMEZ → kimlik dogrulama BASARILI olsa
		// bile "exec request failed on channel 0" hatasi donuyor ve kullanici
		// dogru parolayi girdigi halde "baglanti kurulamadi" goruyordu.
		// `sftp -b` toplu-mod: pwd + quit → yalniz altsistem gerekir.
		// sshpass parola passwd, PreferredAuthentications=password + publickey
		// kapali → parolanin gercekten gecerli oldugu garanti edilir.
		// KRITIK: kullaniciyi `-o User=` ile ver, host `--`'den SONRA gelsin →
		// ikisi de ssh opsiyonu olarak yorumlanamaz (ProxyCommand arg-injection kapali).
		args := []string{
			"-e", // parola SSHPASS ortam degiskeninden okunur; argv'de GORUNMEZ
			"sftp",
			"-P", fmt.Sprintf("%d", d.Port),
			"-o", "User=" + d.Kullanici,
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			"-o", "BatchMode=no",
			"-b", "-",
			"--", d.Host,
		}
		cmd := exec.CommandContext(ctx, "sshpass", args...)
		// Ortam degiskeni: /proc/<pid>/environ yalniz sahibine (root) okunur,
		// kiracilar goremez — argv ise herkese aciktir.
		cmd.Env = append(os.Environ(), "SSHPASS="+d.Parola)
		// 🔴 Test, YUKLEYICININ yaptigi isin aynisini yapmali: yukleyici
		// (uploadToRemote) `mkdir -p` ile dizini kendisi yaratir. Test yalnizca
		// `cd` deneseydi, HENUZ olusturulmamis hedef dizinde "No such file"
		// dondurup calisan bir yapilandirmayi bozukmus gibi gosterirdi.
		// Bu yuzden: dizin agacini olustur (varsa hatayi yut), ic, gercek bir
		// dosya YAZ ve sil → yazma yetkisi de kanitlanir.
		gecici, err := os.CreateTemp("", "gosp-erisim-*.tmp")
		if err != nil {
			return fmt.Errorf("gecici dosya: %w", err)
		}
		defer os.Remove(gecici.Name())
		_, _ = gecici.WriteString("girginospanel erisim testi\n")
		_ = gecici.Close()

		var b strings.Builder
		hedef := strings.TrimSpace(d.UzakDizin)
		// "-" oneki: sftp toplu-modda o satirin hatasini yut (dizin zaten varsa).
		for _, seviye := range dizinSeviyeleri(hedef) {
			b.WriteString("-mkdir " + seviye + "\n")
		}
		if hedef != "" && hedef != "/" {
			b.WriteString("cd " + hedef + "\n")
		}
		b.WriteString("put " + gecici.Name() + " .girginospanel-erisim-testi\n")
		b.WriteString("-rm .girginospanel-erisim-testi\n")
		b.WriteString("pwd\n")
		cmd.Stdin = strings.NewReader(b.String())
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", sshGurultuTemizle(string(out), err))
		}
		return nil
	}
	// FTP — curl --user u:p ftp://host:port/  (NLST root)
	url := fmt.Sprintf("ftp://%s:%d/", d.Host, d.Port)
	args := []string{
		"-sS",
		"--connect-timeout", "10",
		"--max-time", "15",
		"-K", "-", // kimlik STDIN config'ten; argv'de GORUNMEZ
		"--ftp-skip-pasv-ip",
		url,
	}
	cmd := exec.CommandContext(ctx, "curl", args...)
	cmd.Stdin = strings.NewReader("user = \"" + curlConfEscape(d.Kullanici+":"+d.Parola) + "\"\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", sshGurultuTemizle(string(out), err))
	}
	return nil
}

// dizinSeviyeleri: "/a/b/c" -> ["/a", "/a/b", "/a/b/c"]. sftp toplu-modunda
// `mkdir -p` yoktur; her seviye tek tek yaratilmalidir.
func dizinSeviyeleri(yol string) []string {
	yol = strings.TrimSpace(yol)
	if yol == "" || yol == "/" {
		return nil
	}
	mutlak := strings.HasPrefix(yol, "/")
	var birikim string
	var res []string
	for _, parca := range strings.Split(strings.Trim(yol, "/"), "/") {
		if parca == "" || parca == "." || parca == ".." {
			continue
		}
		if birikim == "" {
			if mutlak {
				birikim = "/" + parca
			} else {
				birikim = parca
			}
		} else {
			birikim += "/" + parca
		}
		res = append(res, birikim)
	}
	return res
}

// curlConfEscape: curl -K config dosyasinda cift tirnak icine konacak degeri kacisla.
func curlConfEscape(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

// sshGurultuTemizle: ssh/sftp/curl ciktisindan ZARARSIZ uyarilari atar ve geriye
// gercek hata satirini birakir. Ozellikle "Warning: Permanently added '<host>'
// (RSA) to the list of known hosts." satiri her ILK baglantida cikar; kullaniciya
// hata gibi gosterilmesi yanlis alarm uretiyordu.
func sshGurultuTemizle(cikti string, err error) string {
	var kalan []string
	for _, satir := range strings.Split(cikti, "\n") {
		t := strings.TrimSpace(satir)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Warning: Permanently added") ||
			strings.HasPrefix(t, "Uploading ") ||
			strings.HasPrefix(t, "Removing ") ||
			strings.HasPrefix(t, "Changing to:") ||
			strings.Contains(t, "to the list of known hosts") ||
			strings.HasPrefix(t, "Connected to ") ||
			strings.HasPrefix(t, "sftp> ") ||
			t == "Remote working directory: /" {
			continue
		}
		kalan = append(kalan, t)
	}
	if len(kalan) == 0 {
		if err != nil {
			return err.Error()
		}
		return "bilinmeyen hata"
	}
	return strings.Join(kalan, "; ")
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
