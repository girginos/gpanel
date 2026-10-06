// Package selinuxetiket — kiracı ev dizinine dosya koyan akışların iş bittikten sonra
// SELinux etiketini politika varsayılanına döndürmesi için TEK nokta.
//
// 🔴 Neden: `cp -al`/`mv`/`rsync`/`cp -a` kaynak inode'un etiketini TAŞIR (hardlink aynı
// inode'dur). Hazırlık/geçici alandan (user_home_t) public_html altına gelen dosya, hedefin
// httpd_user_rw_content_t kuralını almaz; Enforcing'de php-fpm (httpd_t) SQLite/log/cache
// yazamaz (create/write/unlink reddi). Yeni dosyayı hedef dizinde YARATAN akışlar (tar -x,
// wp-cli, composer) etiketi ebeveynden miras alır; sorun yalnız "taşıyan" akışlardadır.
//
// Güvenlik: kök kimliğiyle çalışır. Kiracı ev içinde bir bileşeni symlink yapabilir; bu
// yüzden yol bileşenlerinin HİÇBİRİ symlink olmamalı (Lstat) — aksi hâlde restorecon
// çalıştırılmaz (ErrSymlink). restorecon -R alt ağaçta symlink'leri izlemez.
//
// SELinux kapalıysa (ya da restorecon yoksa) sessizce geçer. Mod değiştirmez.
package selinuxetiket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrSymlink: yolun bir bileşeni symlink — kök restorecon'u çalıştırılmadı.
var ErrSymlink = errors.New("selinuxetiket: yol symlink bileşeni içeriyor")

// ErrYol: ev dizini dışına çıkan ya da geçersiz yol.
var ErrYol = errors.New("selinuxetiket: geçersiz yol")

const restoreconSure = 15 * time.Minute

// evKoku: kiracı evlerinin kökü; yalnız bunun altındaki yollar etiketlenir (savunma
// derinliği: kök restorecon'u sistem yollarına yönlendirilemez). Testte değiştirilir.
var evKoku = "/home"

// Dikişler (testte değiştirilir).
var (
	// selinuxAcik: çekirdekte SELinux var mı (kapalıysa hiçbir şey yapılmaz).
	selinuxAcik = func() bool {
		_, err := os.Stat("/sys/fs/selinux/enforce")
		return err == nil
	}
	aracVar = func(ad string) bool {
		_, err := exec.LookPath(ad)
		return err == nil
	}
	komut = func(ctx context.Context, ad string, arg ...string) ([]byte, error) {
		return exec.CommandContext(ctx, ad, arg...).CombinedOutput()
	}
)

// Etiketle: home/parcalar... yolunu (alt ağaç dahil) `restorecon -RF` ile politika
// varsayılanına döndürür. -F: customizable tipler (httpd_sys_content_t vb. başka
// kaynaktan gelmiş) de sıfırlanır — `-R` tek başına onları bırakır.
// Var olmayan yol hatasızdır (etiketlenecek şey yok). Hata YUTULMAZ: çağıran loglar.
func Etiketle(home string, parcalar ...string) error {
	if !selinuxAcik() || !aracVar("restorecon") {
		return nil
	}
	yol, err := guvenliYol(home, parcalar...)
	if err != nil {
		return err
	}
	if yol == "" {
		return nil
	}
	ctx, iptal := context.WithTimeout(context.Background(), restoreconSure)
	defer iptal()
	if out, err := komut(ctx, "restorecon", "-RF", yol); err != nil {
		return fmt.Errorf("restorecon -RF %s: %v: %s", yol, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Mutlak: mutlak yolu (home altında olmalı) Etiketle'ye çevirir.
func Mutlak(home, yol string) error {
	rel, err := filepath.Rel(filepath.Clean(home), filepath.Clean(yol))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrYol
	}
	if rel == "." {
		return Etiketle(home)
	}
	return Etiketle(home, rel)
}

// Yol: /home/<sk>/... biçimli mutlak yolu (ev dizini yoldan çıkarılır) etiketler.
func Yol(yol string) error {
	yol = filepath.Clean(yol)
	kok := filepath.Clean(evKoku) + string(filepath.Separator)
	if !filepath.IsAbs(yol) || !strings.HasPrefix(yol, kok) {
		return ErrYol
	}
	ad := strings.SplitN(strings.TrimPrefix(yol, kok), string(filepath.Separator), 2)[0]
	if ad == "" {
		return ErrYol
	}
	return Mutlak(filepath.Join(evKoku, ad), yol)
}

// LoglaYol: Yol + log.
func LoglaYol(baglam, yol string) {
	if err := Yol(yol); err != nil {
		log.Printf("selinux etiket (%s): %v", baglam, err)
	}
}

// Logla: Etiketle'yi çağırır, hatayı bağlamla birlikte loglar (akışı bozmaz).
func Logla(baglam, home string, parcalar ...string) {
	if err := Etiketle(home, parcalar...); err != nil {
		log.Printf("selinux etiket (%s): %v", baglam, err)
	}
}

// LoglaMutlak: Mutlak + log.
func LoglaMutlak(baglam, home, yol string) {
	if err := Mutlak(home, yol); err != nil {
		log.Printf("selinux etiket (%s): %v", baglam, err)
	}
}

// guvenliYol: home ve parcalar bileşenlerinin hiçbiri symlink değilse tam yolu döner.
// Var olmayan ilk bileşende "" (nil hata) döner.
func guvenliYol(home string, parcalar ...string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", ErrYol
	}
	home = filepath.Clean(home)
	if !strings.HasPrefix(home, filepath.Clean(evKoku)+string(filepath.Separator)) {
		return "", ErrYol
	}
	yol := home
	for _, p := range parcalar {
		yol = filepath.Join(yol, p)
	}
	if yol != home && !strings.HasPrefix(yol, home+string(filepath.Separator)) {
		return "", ErrYol
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(yol, home), string(filepath.Separator))
	adimlar := []string{home}
	cur := home
	if rel != "" {
		for _, b := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, b)
			adimlar = append(adimlar, cur)
		}
	}
	for _, a := range adimlar {
		fi, err := os.Lstat(a)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", ErrSymlink
		}
	}
	return yol, nil
}
