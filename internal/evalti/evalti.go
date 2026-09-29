// Package evalti: kiracı ev dizinine (<EvKoku>/<sk>) çapalı, yol boyunca symlink
// İZLEMEYEN root dosya işlemleri ve kiracı kimliğinde komut hazırlama.
//
// Kaynak desenler paket içine kapalı olduğundan dışa açık ortak yaprak olarak buraya
// alındı (o paketlere bu işte dokunulmadı):
//   - laravel/runner.go evAltiAc + evAltiMkdirHepsi: openat2(RESOLVE_BENEATH|
//     RESOLVE_NO_SYMLINKS), Mkdirat + fd üzerinden Fchown/Fchmod, ad takası savunması;
//   - sitekopya.kiraciKomut (archivex.runuserKomut, git.kiraciCalistir ailesi): runuser,
//     shell yok, panel ortamı yok, iptalde SIGTERM.
//
// Farklar: göreli yoldaki ".." bileşeni kökte sessizce kırpılmaz, REDDEDİLİR; kiracı
// adı her giriş noktasında dar doğrulanır; boş argv reddedilir (runuser argv'siz kabuk açar).
package evalti

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// EvKoku: kiracı ev dizinlerinin kökü. Yalnız testler t.TempDir ile değiştirir.
var EvKoku = "/home"

// OSKullanici: sk → komutun koşacağı ve kimliği çözülen işletim sistemi kullanıcısı.
// Üretimde birebir; testler ayrıcalıksız "nobody" ile değiştirir (sitekopya deseni).
var OSKullanici = func(sk string) string { return sk }

// reSK: panelin açtığı sistem kullanıcıları (c_<slug>[n]; provisioner gecerliSKAdi
// alfabesi, en çok 32 karakter). Yol bileşenine ve runuser argv'sine girer → dar doğrula.
var reSK = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)

var errSK = errors.New("geçersiz kiracı kullanıcısı")

// GecerliSK: sk panelin açtığı biçimde bir kiracı sistem kullanıcısı mı?
func GecerliSK(sk string) bool { return reSK.MatchString(sk) }

// EvDizini: <EvKoku>/<sk>; sk geçersizse hata.
func EvDizini(sk string) (string, error) {
	if !GecerliSK(sk) {
		return "", errSK
	}
	return EvKoku + "/" + sk, nil
}

// Kimlik: kiracının uid/gid'i.
func Kimlik(sk string) (uid, gid int, err error) {
	if !GecerliSK(sk) {
		return 0, 0, errSK
	}
	u, err := user.Lookup(OSKullanici(sk))
	if err != nil {
		return 0, 0, err
	}
	if uid, err = strconv.Atoi(u.Uid); err != nil {
		return 0, 0, err
	}
	if gid, err = strconv.Atoi(u.Gid); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// cozum: yol boyunca hiçbir symlink izlenmez, ev kökünün dışına çıkılamaz.
const cozum = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS

// parcala: ev köküne göre göreli yolun bileşenleri ("" ve "." atlanır). ".." bileşeni
// ya da mutlak yol → hata (kökte kırpıp başka bir dizine yönelmek yerine reddet).
func parcala(rel string) ([]string, error) {
	if strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("mutlak yol kabul edilmez")
	}
	var p []string
	for _, s := range strings.Split(rel, "/") {
		switch s {
		case "", ".":
			continue
		case "..":
			return nil, fmt.Errorf("yolda '..' kabul edilmez")
		}
		p = append(p, s)
	}
	return p, nil
}

// evAc: ev dizininin kendisi. /home root'a aittir, kiracı ev girdisini takas edemez;
// O_NOFOLLOW ev yolunun son bileşeninin symlink olmasını da reddeder.
func evAc(home string) (int, error) {
	return unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
}

// Ac: rel'i home köküne göre, yol boyunca HİÇBİR symlink izlemeden ve home dışına
// çıkmadan açar; ham fd döner (çağıran kapatır). rel "" ya da "." → evin kendisi.
func Ac(home, rel string, flags int) (int, error) {
	p, err := parcala(rel)
	if err != nil {
		return -1, err
	}
	hf, err := evAc(home)
	if err != nil {
		return -1, err
	}
	defer unix.Close(hf)
	yol := strings.Join(p, "/")
	if yol == "" {
		yol = "."
	}
	return unix.Openat2(hf, yol, &unix.OpenHow{Flags: uint64(flags) | unix.O_CLOEXEC, Resolve: cozum})
}

// yarisKancasi: YALNIZ testler atar (üretimde nil). Mkdirat ile Openat arasındaki
// pencerede çağrılır; test adı takas ederek yarışı deterministik yeniden üretir.
var yarisKancasi func(dfd int, ad string)

// dizinBos: fd'nin gösterdiği dizinde "." ve ".." dışında girdi var mı? (fd'nin okuma
// konumunu ilerletir; fd yalnız *at çağrılarının kökü olarak kullanılır.)
func dizinBos(fd int) (bool, error) {
	buf := make([]byte, 4096)
	for {
		n, err := unix.Getdents(fd, buf)
		if err != nil {
			return false, err
		}
		if n <= 0 {
			return true, nil
		}
		if _, sayi, _ := unix.ParseDirent(buf[:n], 1, nil); sayi > 0 {
			return false, nil
		}
	}
}

// MkdirHepsi: `mkdir -p home/rel`in symlink-güvenli karşılığı. Her bileşen home'dan
// başlayarak O_NOFOLLOW ile açılır (bileşen symlink ise durur, dışarıda dizin
// yaratılmaz); eksik bileşen Mkdirat ile yaratılıp fd üzerinden uid:gid'e verilir ve izni
// (umask'tan bağımsız) mode yapılır. Var olan bileşenin sahipliğine/iznine dokunulmaz.
// Yarış: Mkdirat ile Openat arasında kiracı adı aynı üst dizindeki başka (dolu) bir
// dizinle takas edebilir; yeni yaratılan dizin BOŞ olmak zorunda olduğundan açılan dizin
// boş değilse sahiplik VERİLMEZ, hata döner.
func MkdirHepsi(home, rel string, uid, gid int, mode uint32) error {
	parcalar, err := parcala(rel)
	if err != nil {
		return err
	}
	fd, err := evAc(home)
	if err != nil {
		return err
	}
	for _, parca := range parcalar {
		yeni := false
		if err := unix.Mkdirat(fd, parca, mode); err == nil {
			yeni = true
		} else if !errors.Is(err, unix.EEXIST) {
			unix.Close(fd)
			return err
		}
		if yeni && yarisKancasi != nil {
			yarisKancasi(fd, parca)
		}
		nfd, err := unix.Openat(fd, parca, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return err
		}
		if yeni {
			bos, berr := dizinBos(nfd)
			if berr == nil && !bos {
				berr = fmt.Errorf("%s: yeni yaratılan dizin yerinde değil (ad takası)", parca)
			}
			if berr == nil {
				berr = unix.Fchown(nfd, uid, gid)
			}
			if berr == nil {
				berr = unix.Fchmod(nfd, mode)
			}
			if berr != nil {
				unix.Close(nfd)
				return berr
			}
		}
		fd = nfd
	}
	unix.Close(fd)
	return nil
}

// kiraciPATH: kiracı komutlarının arama yolu (panelin PATH'i ve ortamı taşınmaz).
const kiraciPATH = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// KiraciKomut: argv'yi KİRACI kimliğinde (runuser -u sk; shell yok, panel ortamı ve
// sırları yok) çalıştıracak komutu hazırlar; Stdin/Stdout/Stderr'i çağıran bağlar.
// Kiracı olarak koşmak symlink hilelerini yapısal olarak kapatır: çözülen bağ komşu
// evi ya da /etc'yi gösterse bile yazım EACCES alır; dosyalar doğrudan kiracıya ait
// oluşur ve disk kotası kiracıya işler. runuser SIGTERM'i çocuğa iletir (SIGKILL'i
// iletemez) → iptalde önce TERM; çocuk takılırsa WaitDelay sonunda Go süreci öldürür.
func KiraciKomut(ctx context.Context, sk string, argv ...string) (*exec.Cmd, error) {
	ev, err := EvDizini(sk)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("boş komut")
	}
	cmd := exec.CommandContext(ctx, "runuser", append([]string{"-u", OSKullanici(sk), "--"}, argv...)...)
	cmd.Env = []string{kiraciPATH, "HOME=" + ev, "LANG=C"}
	cmd.Dir = "/"
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	return cmd, nil
}
