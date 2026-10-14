package antivirus

// imzadizini.go — ClamAV imza dizininin sahipliği (açılış onarımı).
//
// freshclam imzaları DatabaseOwner (clamupdate) kimliğiyle yazar. Dizin ya da içindeki dosyalar başka bir
// kullanıcıya geçerse freshclam her denemede "Can't create temporary directory" ile düşer ve imzalar
// SESSİZCE eskir: tarama çalışır ama haftalarca eski imzalarla (yarım plasebo). Canary sunucusunda
// kurulum sırasında tek bir chown -R dizini valkey'e çevirmişti; imzalar 37 gün güncellenmedi.
//
// Onarım yalnız dizinin kendisine ve DOĞRUDAN içindeki normal dosya/dizinlere uygulanır; sembolik bağ
// izlenmez, dizin sembolik bağsa ya da clamupdate kullanıcısı yoksa (ClamAV kurulu değil) hiçbir şeye
// dokunulmaz. Bir şey değiştiyse imza güncelleyici yeniden başlatılır (çalışmıyorsa başlatılmaz).

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

var (
	imzaDizini        = "/var/lib/clamav"
	imzaKullanici     = "clamupdate"
	imzaKullaniciBul  = user.Lookup
	imzaChown         = os.Lchown
	imzaYenidenBaslat = func() error { return exec.Command("systemctl", "try-restart", "clamav-freshclam").Run() }
)

// ImzaDiziniOnar: imza dizininin sahipliğini clamupdate'e onarır; onarılan girdi sayısını döner.
func ImzaDiziniOnar() (int, error) {
	u, err := imzaKullaniciBul(imzaKullanici)
	if err != nil {
		return 0, nil // ClamAV imza güncelleyicisi kurulu değil: dokunulmaz
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("antivirus: %s kimliği okunamadı (%s:%s)", imzaKullanici, u.Uid, u.Gid)
	}
	fi, err := os.Lstat(imzaDizini)
	if err != nil || !fi.IsDir() {
		return 0, nil // yok ya da sembolik bağ: dokunulmaz
	}
	onarilan := 0
	var ilkHata error
	onar := func(yol string, fi os.FileInfo) {
		if fi.Mode()&os.ModeSymlink != 0 || !(fi.Mode().IsRegular() || fi.IsDir()) {
			return
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || (int(st.Uid) == uid && int(st.Gid) == gid) {
			return
		}
		if err := imzaChown(yol, uid, gid); err != nil {
			if ilkHata == nil {
				ilkHata = fmt.Errorf("antivirus: %s sahipliği onarılamadı: %w", yol, err)
			}
			return
		}
		onarilan++
	}
	onar(imzaDizini, fi)
	girdiler, err := os.ReadDir(imzaDizini)
	if err != nil && ilkHata == nil {
		ilkHata = fmt.Errorf("antivirus: %s okunamadı: %w", imzaDizini, err)
	}
	for _, g := range girdiler {
		yol := filepath.Join(imzaDizini, g.Name())
		if gfi, err := os.Lstat(yol); err == nil {
			onar(yol, gfi)
		}
	}
	if onarilan > 0 {
		log.Printf("antivirus: %s içinde %d girdinin sahipliği %s'e onarıldı (freshclam yazamıyordu, imzalar eskiyordu); imza güncelleyici yeniden başlatılıyor",
			imzaDizini, onarilan, imzaKullanici)
		if err := imzaYenidenBaslat(); err != nil {
			log.Printf("antivirus: clamav-freshclam yeniden başlatılamadı: %v", err)
		}
	}
	return onarilan, ilkHata
}
