package hostuyg

// Per-app sistem user yönetimi.
//
// Her uygulama için: `gpanel-app-<kod-ornek>` sistem hesabı (login yok,
// home = kurulum dizini, shell = /sbin/nologin).
//
// Silme: `userdel -r` home dizinini de siler.

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"time"
)

const KullaniciPrefix = "gpanel-app-"

// kullaniciAdiTemizle — recipe kod'undan güvenli username üret.
// max 32 char (Linux limit), sadece a-z0-9-.
var kullaniciAdiSafe = regexp.MustCompile(`[^a-z0-9-]`)

func KullaniciAdi(kod, ornek string) string {
	slug := strings.ToLower(kod)
	if ornek != "" && ornek != kod {
		slug = slug + "-" + strings.ToLower(ornek)
	}
	slug = kullaniciAdiSafe.ReplaceAllString(slug, "-")
	tam := KullaniciPrefix + slug
	if len(tam) > 32 {
		tam = tam[:32]
	}
	// trailing tire olmasın (useradd sevmez)
	return strings.TrimRight(tam, "-")
}

// KullaniciVarMi — sistemde bu user tanımlı mı?
func KullaniciVarMi(ad string) bool {
	_, err := user.Lookup(ad)
	return err == nil
}

// KullaniciYarat — sistem user oluştur (login yok, home = homeYolu).
// İdempotent: zaten varsa hata dönmez, sadece home'u kontrol eder.
func KullaniciYarat(ad, homeYolu string) error {
	if !strings.HasPrefix(ad, KullaniciPrefix) {
		return fmt.Errorf("güvenlik: user adı %q %s prefix'i taşımıyor", ad, KullaniciPrefix)
	}
	if KullaniciVarMi(ad) {
		return nil
	}
	// useradd --system --shell /sbin/nologin --home-dir <home> --create-home <ad>
	cmd := exec.Command("useradd",
		"--system",
		"--shell", "/sbin/nologin",
		"--home-dir", homeYolu,
		"--create-home",
		ad)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("useradd fail: %s (%v)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// KullaniciSil — userdel -r (home dahil). Yoksa hata dönmez.
func KullaniciSil(ad string) error {
	if !strings.HasPrefix(ad, KullaniciPrefix) {
		return fmt.Errorf("güvenlik: user adı %q %s prefix'i taşımıyor — silme reddedildi", ad, KullaniciPrefix)
	}
	if !KullaniciVarMi(ad) {
		return nil
	}
	// Önce process'lerini öldür (systemd unit stop bunu zaten yapmış olmalı)
	_ = exec.Command("pkill", "-9", "-u", ad).Run()
	// Fix: pgrep polling ile kill'in tamamlandığını doğrula (max 2s)
	for i := 0; i < 10; i++ {
		if err := exec.Command("pgrep", "-u", ad).Run(); err != nil {
			// exit code 1 = process yok
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	cmd := exec.Command("userdel", "--remove", "--force", ad)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("userdel fail: %s (%v)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// KullaniciUID — user'ın UID/GID'i (dosya ownership için).
func KullaniciUID(ad string) (uid, gid int, err error) {
	u, err := user.Lookup(ad)
	if err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(u.Uid, "%d", &uid); err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(u.Gid, "%d", &gid); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// Sanity — kullanıcı adı boş ya da güvenlik prefix'siz olmasın.
func kullaniciAdiDogrula(ad string) error {
	if ad == "" {
		return errors.New("kullanıcı adı boş")
	}
	if !strings.HasPrefix(ad, KullaniciPrefix) {
		return fmt.Errorf("prefix zorunlu (%s...)", KullaniciPrefix)
	}
	if len(ad) > 32 {
		return errors.New("kullanıcı adı 32 karakteri aşamaz")
	}
	return nil
}
