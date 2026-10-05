// Package evalti: kiracı ev dizinine (<EvKoku>/<sk>) çapalı, yol boyunca symlink
// İZLEMEYEN root dosya işlemleri.
//
// Kaynak desen paket içine kapalı olduğundan dışa açık ortak yaprak olarak buraya
// alındı (o pakete bu işte dokunulmadı): laravel/runner.go evAltiAc +
// evAltiMkdirHepsi: openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS), Mkdirat + fd
// üzerinden Fchown/Fchmod, ad takası savunması.
//
// Farklar: göreli yoldaki ".." bileşeni kökte sessizce kırpılmaz, REDDEDİLİR; kiracı
// adı her giriş noktasında dar doğrulanır (kiraci.GecerliSK).
//
// Kök kiracı ağacında yol çözmesi gereken HER açmayı Ac ile yapar (taşıma
// yerleşimi, arşiv çıkarma): os.OpenRoot ev İÇİNDE kalan bağı izler; kiracının ev
// içinde kök sahipli bir alanı gösteren bağı kökün işlemini oraya yönlendirirdi.
// Kiracı kimliğinde komut bu paketin işi DEĞİLDİR: tek runuser sarmalayıcısı
// kiraci.Komut'tur.
package evalti

import (
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"girginospanel/internal/kiraci"
)

// EvKoku: kiracı ev dizinlerinin kökü. Yalnız testler t.TempDir ile değiştirir.
var EvKoku = kiraci.EvKoku

// OSKullanici: sk → komutun koşacağı ve kimliği çözülen işletim sistemi kullanıcısı.
// Üretimde birebir; testler ayrıcalıksız "nobody" ile değiştirir.
var OSKullanici = func(sk string) string { return sk }

// EvDizini: <EvKoku>/<sk>; sk geçersizse hata.
func EvDizini(sk string) (string, error) {
	if !kiraci.GecerliSK(sk) {
		return "", kiraci.ErrSK
	}
	return EvKoku + "/" + sk, nil
}

// Kimlik: kiracının uid/gid'i.
func Kimlik(sk string) (uid, gid int, err error) {
	if !kiraci.GecerliSK(sk) {
		return 0, 0, kiraci.ErrSK
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
