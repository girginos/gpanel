package kaynaklimit

// oturum_dilimi.go — kiracının PAM oturum dilimine (user-<UID>.slice) plan tavanı.
//
// 🔴 NEDEN (181 canlı ölçüm, 2026-10-03): pam_systemd'den geçen her kiracı süreci
// (SSH/SFTP oturumu, su, crond'un kiracı spool'u, linger'lı kullanıcı yöneticisi
// user@<UID>.service ve altındaki rootless podman vb.) girginos-<sk>.slice'a DEĞİL
// user-<UID>.slice'a düşer; o dilimde sınır yoktu (MemoryMax/CPUQuota=infinity,
// TasksMax=20664). systemd (257) oturumu başka bir dilime koyamaz; user-<UID>.slice
// user.slice'ın çocuğudur. Bu yüzden oturum dilimine AYNI plan tavanı drop-in ile
// konur.
//
// BÜTÇE: kiracı cron'u ve panelden açılan "şimdi çalıştır" girginos-<sk>.slice'ta
// koşar (internal/cron) → plan bütçesini PHP ile PAYLAŞIR. Oturum dilimi AYRI bir
// bütçedir: SSH/linger açık bir kiracı en kötü durumda 2× plan kullanabilir. Bu
// bilinçli bir sınırdır (bkz. rapor/ADR); sınırsızdan (bugün) çok daha dardır.
// Plansız kiracıda (plan yok = limit yok) drop-in yazılmaz/silinir.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Testler geçici dizin/sahte komutla değiştirir; üretimde SABİTTİR.
var (
	oturumDilimKok = "/etc/systemd/system"
	uidBul         = func(sk string) (string, error) {
		u, err := user.Lookup(sk)
		if err != nil {
			return "", err
		}
		return u.Uid, nil
	}
	systemctlCalistir = func(args ...string) ([]byte, error) {
		return exec.Command("systemctl", args...).CombinedOutput()
	}
)

// minKiraciUID — sistem kullanıcılarının dilimlerine asla dokunulmaz.
const minKiraciUID = 1000

var reOturumSK = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)

func oturumDropinAdi(sk string) string { return "50-girginospanel-" + sk + ".conf" }

func oturumDilimi(uid string) string { return "user-" + uid + ".slice" }

// oturumDilimIcerik — drop-in gövdesi (girginos-<sk>.slice ile aynı değerler).
func oturumDilimIcerik(sk string, l Limitler) string {
	mem := nonzero(l.RAMMB, 512)
	return fmt.Sprintf(`# GirginOSPanel — %s oturum dilimi tavanı (SSH/su/linger). Panel üretir; elle düzenlemeyin.
[Slice]
CPUAccounting=yes
MemoryAccounting=yes
TasksAccounting=yes
IOAccounting=yes
CPUQuota=%d%%
MemoryMax=%dM
MemoryHigh=%dM
TasksMax=%d
IOWeight=%d
%s`, sk, nonzero(l.CPUYuzde, 100), mem, mem*90/100, nonzero(l.MaxProcess, 50), nonzero(l.IOAgirlik, 100), ioSliceLines(l))
}

// kiraciUID — sk'nin UID'i; kiracı olamayacak (biçim dışı, sistem kullanıcısı) → hata.
func kiraciUID(sk string) (string, error) {
	if !reOturumSK.MatchString(sk) {
		return "", fmt.Errorf("geçersiz kiracı: %q", sk)
	}
	uid, err := uidBul(sk)
	if err != nil {
		return "", err
	}
	if n, err := strconv.Atoi(uid); err != nil || n < minKiraciUID {
		return "", fmt.Errorf("%s: uid %s kiracı aralığında değil", sk, uid)
	}
	return uid, nil
}

// oturumDilimiYaz — drop-in'i yazar (daemon-reload ÇAĞIRANIN; SystemdSliceYaz yapar).
// UID döner: canlı uygulama (oturumDilimiCanli) daemon-reload'dan sonra yapılır.
func oturumDilimiYaz(sk string, l Limitler) (uid string, err error) {
	uid, err = kiraciUID(sk)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(oturumDilimKok, oturumDilimi(uid)+".d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Aynı UID'e başka (eski) kiracı adıyla yazılmış drop-in kalmasın (UID yeniden kullanımı).
	eskiler, _ := filepath.Glob(filepath.Join(dir, "50-girginospanel-*.conf"))
	for _, e := range eskiler {
		if filepath.Base(e) != oturumDropinAdi(sk) {
			_ = os.Remove(e)
		}
	}
	return uid, os.WriteFile(filepath.Join(dir, oturumDropinAdi(sk)), []byte(oturumDilimIcerik(sk, l)), 0o644)
}

// oturumDilimiCanli — dilim etkinse değerleri süreç öldürmeden anında uygular.
func oturumDilimiCanli(uid string, l Limitler) {
	birim := oturumDilimi(uid)
	if out, _ := systemctlCalistir("is-active", birim); strings.TrimSpace(string(out)) != "active" {
		return
	}
	mem := nonzero(l.RAMMB, 512)
	args := []string{"set-property", "--runtime", birim,
		fmt.Sprintf("CPUQuota=%d%%", nonzero(l.CPUYuzde, 100)),
		fmt.Sprintf("MemoryMax=%dM", mem),
		fmt.Sprintf("MemoryHigh=%dM", mem*90/100),
		fmt.Sprintf("TasksMax=%d", nonzero(l.MaxProcess, 50)),
		fmt.Sprintf("IOWeight=%d", nonzero(l.IOAgirlik, 100)),
	}
	args = append(args, ioSetPropertyArgs(l)...)
	if out, err := systemctlCalistir(args...); err != nil {
		log.Printf("oturum dilimi set-property %s: %s: %v", birim, strings.TrimSpace(string(out)), err)
	}
}

// oturumDilimiSil — sk'nin drop-in'ini (UID artık çözülmese de: hesap silinirken
// userdel'den sonra çağrılır) kaldırır; etkin dilimdeki çalışma zamanı değerlerini
// sıfırlar. Değişiklik olduysa daemon-reload yapar.
func oturumDilimiSil(sk string) {
	if !reOturumSK.MatchString(sk) {
		return
	}
	dosyalar, _ := filepath.Glob(filepath.Join(oturumDilimKok, "user-*.slice.d", oturumDropinAdi(sk)))
	if len(dosyalar) == 0 {
		return
	}
	for _, f := range dosyalar {
		if err := os.Remove(f); err != nil {
			log.Printf("oturum dilimi drop-in silinemedi %s: %v", f, err)
			continue
		}
		birim := strings.TrimSuffix(filepath.Base(filepath.Dir(f)), ".d")
		if out, _ := systemctlCalistir("is-active", birim); strings.TrimSpace(string(out)) == "active" {
			args := []string{"set-property", "--runtime", birim,
				"CPUQuota=", "MemoryMax=infinity", "MemoryHigh=infinity", "TasksMax=infinity", "IOWeight="}
			args = append(args, ioSetPropertyArgs(Limitler{})...)
			if out, err := systemctlCalistir(args...); err != nil {
				log.Printf("oturum dilimi sıfırla %s: %s: %v", birim, strings.TrimSpace(string(out)), err)
			}
		}
	}
	if out, err := systemctlCalistir("daemon-reload"); err != nil {
		log.Printf("oturum dilimi daemon-reload: %s: %v", strings.TrimSpace(string(out)), err)
	}
}
