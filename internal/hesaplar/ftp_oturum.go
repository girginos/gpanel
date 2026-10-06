package hesaplar

// ftp_oturum.go — FTP parolası değişince o hesabın AÇIK pure-ftpd oturumlarını düşürme
// (kullanıcı kararı 2026-10-06: "Düşsün"). Parola yalnız YENİ girişi etkiler; açık
// oturum eski parolayla sürerdi.
//
// Yöntem: `pure-ftpwho -s -n` (betik çıktısı: pid|hesap|süre|durum|...) → hesabı BİREBİR
// eşleşen satırların PID'si → SIGTERM (pure-ftpd oturum çocuğu temiz kapanır).
//
// 🔴 Güvenlik: kabuk YOK, argümanlar sabit liste; kullanıcı adı katı doğrulanır
// (FTPOturumAdiGecerli) — geçersiz ad hiçbir komut çalıştırmadan reddedilir; yalnız
// hesap adı BİREBİR eşleşen (ya da hosting dalında sk + "-" öneki: ek hesaplar; başka
// kiracının sk'sı "-" içeremez) satırlara sinyal gider; PID'in gerçekten pure-ftpd
// olduğu sinyalden hemen önce /proc'tan doğrulanır (PID yeniden kullanım koruması).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// reFTPOturumAdi: ana hesap (c_<slug>) ya da ek hesap (c_<slug>-<ad>); en çok 64.
var reFTPOturumAdi = regexp.MustCompile(`^c_[a-z0-9_]{1,30}(-[a-z0-9][a-z0-9_]*)?$`)

// reFTPOturumSK: yalnız ana hesap / hosting sistem kullanıcısı.
var reFTPOturumSK = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)

// FTPOturumAdiGecerli: oturum düşürmeye verilebilecek FTP hesap adı biçimi.
func FTPOturumAdiGecerli(ad string) bool {
	return len(ad) <= 64 && reFTPOturumAdi.MatchString(ad)
}

// Dikişler (testler gerçek pure-ftpd'ye dokunmaz).
var (
	// ftpWhoKomut: oturum listesi (kabuksuz, sabit argüman).
	ftpWhoKomut = func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "pure-ftpwho", "-s", "-n")
	}
	// ftpOturumSonlandir: tek oturum sürecine SIGTERM.
	ftpOturumSonlandir = ftpGercekSonlandir
)

func ftpGercekSonlandir(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("geçersiz pid")
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return nil // süreç zaten bitmiş
	}
	if strings.TrimSpace(string(b)) != "pure-ftpd" {
		return nil // PID başka sürece geçmiş: dokunma
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// FTPOturumlariDusur: bu FTP hesabının (ana ya da ek; BİREBİR ad) açık oturumlarını
// sonlandırır. Dönen sayı sinyal gönderilen oturum sayısıdır.
func FTPOturumlariDusur(kullanici string) (int, error) {
	if !FTPOturumAdiGecerli(kullanici) {
		return 0, fmt.Errorf("güvenlik: geçersiz FTP kullanıcı adı")
	}
	return ftpOturumlariDusur(func(acct string) bool { return acct == kullanici })
}

// FTPHostingOturumlariDusur: hostingin (sistem kullanıcısı sk) ana + TÜM ek hesaplarının
// açık oturumları (sahip devri gibi tüm erişimin kesildiği durumlar).
func FTPHostingOturumlariDusur(sk string) (int, error) {
	if !reFTPOturumSK.MatchString(sk) {
		return 0, fmt.Errorf("güvenlik: geçersiz sistem kullanıcısı")
	}
	return ftpOturumlariDusur(func(acct string) bool {
		return acct == sk || strings.HasPrefix(acct, sk+"-")
	})
}

func ftpOturumlariDusur(eslesir func(string) bool) (int, error) {
	ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
	defer iptal()
	out, err := ftpWhoKomut(ctx).Output()
	if err != nil {
		return 0, fmt.Errorf("pure-ftpwho: %w", err)
	}
	var hatalar []error
	n := 0
	for _, satir := range strings.Split(string(out), "\n") {
		alan := strings.Split(strings.TrimSpace(satir), "|")
		if len(alan) < 2 {
			continue
		}
		acct := strings.TrimSpace(alan[1])
		// Kaçışlı ("\|") ad ya da biçimsiz satır: kendi ad doğrulamamızı geçemez → atla.
		if !FTPOturumAdiGecerli(acct) || !eslesir(acct) {
			continue
		}
		pid, e := strconv.Atoi(strings.TrimSpace(alan[0]))
		if e != nil || pid <= 1 {
			continue
		}
		if e := ftpOturumSonlandir(pid); e != nil {
			hatalar = append(hatalar, fmt.Errorf("pid %d: %w", pid, e))
			continue
		}
		n++
	}
	return n, errors.Join(hatalar...)
}
