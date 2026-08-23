package toplu

// devir_symlink.go — Devir sırasında tenant ev dizinine YAZAN/OKUYAN işlemler
// symlink-güvenli olmalı. Ev dizini (/home/<sk>) tenant'ın SAHİPLİĞİNDEDİR
// (provisioner: chown uid:gid, chmod 0710), yani tenant `.ssh`'ı bir symlink'e
// çevirip panelin (root) o link üzerinden /root/.ssh veya /etc altındaki
// dosyaları truncate etmesini sağlayabilir.
//
// ÇÖZÜM: openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS) — home köküne göre çözer
// ve YOL BOYUNCA hiçbir symlink'i izlemez; biri symlink ise ELOOP döner.
// (Aynı teknik internal/files/safeio.go'da web dosya yöneticisi için kullanılıyor.)

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// homeAltiAc — /home/<sk> köküne göre `rel`'i, yol boyunca HİÇBİR symlink
// izlemeden açar. flags/mode os.OpenFile ile aynı anlamda.
func homeAltiAc(sk, rel string, flags int, mode uint32) (*os.File, error) {
	home := filepath.Join("/home", sk)
	// Home kökünü aç: bu dizinin KENDİSİ symlink olmamalı (tenant onu değiştiremez;
	// üst dizin /home root'undur). O_NOFOLLOW ile yine de güvenceye alıyoruz.
	hf, err := unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(hf)

	how := &unix.OpenHow{
		Flags:   uint64(flags) | unix.O_CLOEXEC,
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(hf, rel, how)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(home, rel)), nil
}
