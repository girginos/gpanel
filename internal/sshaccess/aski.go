package sshaccess

// Hesap askısı — SSH/SFTP girişi.
//
// 🔴 Askıda giriş YOK: kabuk/grup (ssh_erisim) değiştirmek yetmez. SSH'ı kapalı
// ya da jail grubu dışındaki bir hesap, anahtarla bağlanıp `-N` ile tünel açabilir
// (AllowTcpForwarding varsayılanı), internal-sftp kabuktan bağımsızdır. Bu yüzden
// askı hesabın SÜRESİNİ doldurur (shadow expire): sshd (anahtar dahil HER yöntem)
// ve PAM hesap denetimi süresi dolmuş hesabı reddeder. Kabuk, gosp-ssh grubu,
// jail ve ssh_erisim DEĞİŞMEZ → askıdan alınınca önceki SSH ayarı aynen döner.
// Hesabın askı öncesi süre değeri saklanır ve aynen geri yazılır.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// askiGunu — askıdaki hesabın shadow süre alanı (1970-01-02 = geçmiş). Açarken
// yalnız bu değer bizimse geri alınır; askı sırasında elle konmuş bir süreye
// dokunulmaz.
const askiGunu = "1"

// reSureGun — chage -E'ye geçen değer: gün sayısı ya da -1 (süresiz).
var reSureGun = regexp.MustCompile(`^(-1|[0-9]{1,7})$`)

// Dış etkili adımlar (getent/chage, saklama dizini). Testler sahteyle değiştirir:
// ana makinenin /etc/shadow'una dokunmadan sınanır. Üretimde değerler SABİTTİR.
var (
	girisKomut = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }
	girisDepo  = "/var/lib/girginospanel/ssh-askida"
)

// hesapSuresi — shadow'daki hesap süresi alanı (gün; boş = süresiz).
func hesapSuresi(sk string) (string, error) {
	out, err := girisKomut("getent", "shadow", sk)
	if err != nil {
		return "", fmt.Errorf("hesap süresi okunamadı: %w", err)
	}
	alan := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(alan) < 8 || alan[0] != sk {
		return "", errors.New("hesap süresi okunamadı: beklenmeyen shadow satırı")
	}
	return alan[7], nil
}

// HesapGirisKilidi — askıda (kilit=true) hesabın SSH/SFTP girişini keser;
// kilit=false iken askı öncesi hâline döndürür. İdempotenttir.
func HesapGirisKilidi(sk string, kilit bool) error {
	if !gecerliSK(sk) {
		return fmt.Errorf("geçersiz sistem kullanıcısı: %q", sk)
	}
	sure, err := hesapSuresi(sk)
	if err != nil {
		return err
	}
	depo := filepath.Join(girisDepo, sk)
	if kilit {
		if sure == askiGunu {
			return nil // zaten askıda
		}
		if err := os.MkdirAll(girisDepo, 0o700); err != nil {
			return fmt.Errorf("askı öncesi SSH ayarı saklanamadı: %w", err)
		}
		if err := os.WriteFile(depo, []byte(sure), 0o600); err != nil {
			return fmt.Errorf("askı öncesi SSH ayarı saklanamadı: %w", err)
		}
		if out, err := girisKomut("chage", "-E", askiGunu, sk); err != nil {
			return fmt.Errorf("SSH girişi kapatılamadı: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	if sure != askiGunu {
		// Askı değeri bizde değil (hiç askıya alınmamış ya da süreyi yönetici
		// elle değiştirmiş): dokunma, yalnız saklanan artığı temizle.
		_ = os.Remove(depo)
		return nil
	}
	onceki := "-1" // saklanan yoksa: süresiz (panel hesap süresi kullanmaz)
	if b, err := os.ReadFile(depo); err == nil && strings.TrimSpace(string(b)) != "" {
		onceki = strings.TrimSpace(string(b))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("askı öncesi SSH ayarı okunamadı: %w", err)
	}
	if !reSureGun.MatchString(onceki) {
		return errors.New("askı öncesi SSH ayarı bozuk — hesap askıda bırakıldı")
	}
	if out, err := girisKomut("chage", "-E", onceki, sk); err != nil {
		return fmt.Errorf("SSH girişi açılamadı: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = os.Remove(depo)
	return nil
}

// AskiArtiginiSil — hesap silinirken askı öncesi SSH süresi deposunu kaldırır
// (sk yeniden kullanılabilir; bkz. provisioner.Deprovision).
func AskiArtiginiSil(sk string) {
	if !gecerliSK(sk) {
		return
	}
	_ = os.Remove(filepath.Join(girisDepo, sk))
}
