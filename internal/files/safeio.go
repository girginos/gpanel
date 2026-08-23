package files

// safeio.go — TOCTOU symlink-yarışına dayanıklı dosya mutasyonları.
//
// SORUN: jailJoinStrict() yol'u KONTROL anında EvalSymlinks ile çözer ve resolved bir
// STRING döner. Mutasyon (os.Chmod/os.WriteFile/os.Rename/os.RemoveAll/os.Create/os.Chown)
// SONRADAN o string üzerinde root olarak çalışır. Tenant, kontrol ile işlem arasında
// ara-dizini bir symlink'e takas ederek (yarış) root'u jail-DIŞI bir dosyada işlem yapmaya
// kandırabilir (LPE / yerel yetki yükseltme).
//
// ÇÖZÜM: openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS) ile home'a-göreli, HİÇBİR symlink
// takip etmeden, home dışına ÇIKAMADAN, ATOMİK bir fd al; sonra fd/*at-syscall üzerinden
// işlem yap. "Çöz + işlem" tek adımda kernel'de olur; ara-bileşen symlink takası imkânsızlaşır.
// AlmaLinux 10 / kernel 6.12 openat2'yi destekler.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const dirOpenFlags = unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK

// relClean: kullanıcı-verdiği yolu home'a-GÖRELİ, '..'-temiz bir yola indirger. "/" öneki
// eklenip Clean edilerek her türlü '..' sözlüksel olarak eritilir; asıl zorlamayı yine de
// openat2'nin RESOLVE_BENEATH bayrağı yapar.
func relClean(userYol string) string {
	return strings.TrimPrefix(filepath.Clean("/"+userYol), "/")
}

// openHomeFd: home dizinini O_DIRECTORY ile açar. home (/home/c_<slug>) root tarafından
// oluşturulur; /home root'a aittir → tenant home DİZİN GİRDİSİNİ symlink'e takas edemez,
// bu yüzden home'u doğrudan açmak güvenlidir. Alt bileşenler openat2 ile korunur.
func openHomeFd(home string) (int, error) {
	return unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
}

// openAt2Beneath: rel'i home altında, hiçbir symlink takip etmeden, home dışına çıkamadan,
// ATOMİK açar ve *os.File döner (çağıran Close etmeli).
func openAt2Beneath(home, rel string, flags int, mode uint32) (*os.File, error) {
	hf, err := openHomeFd(home)
	if err != nil {
		return nil, err
	}
	defer unix.Close(hf)
	p := relClean(rel)
	if p == "" {
		p = "."
	}
	how := &unix.OpenHow{
		Flags:   uint64(flags) | unix.O_CLOEXEC,
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(hf, p, how)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(home, p)), nil
}

// isDirBeneath: rel home altında bir DİZİN mi? (symlink-güvenli; ara-bileşen symlink ise hata).
func isDirBeneath(home, rel string) (bool, error) {
	f, err := openAt2Beneath(home, rel, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

// safeParentFd: rel'in PARENT dizinini home altında symlink-takipsiz açar (raw fd) ve
// tek-bileşen leaf adını döner. Çağıran unix.Close(parentFd) etmeli. Parent fd pinlenir →
// leaf üstündeki ara-bileşenler artık takas edilemez; yalnız tek leaf işleme konu olur.
func safeParentFd(home, rel string) (parentFd int, leaf string, err error) {
	p := relClean(rel)
	parent := filepath.Dir(p) // "a/b" -> "a", "f" -> "."
	leaf = filepath.Base(p)
	f, err := openAt2Beneath(home, parent, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, "", err
	}
	fd, err := unix.Dup(int(f.Fd()))
	f.Close()
	if err != nil {
		return -1, "", err
	}
	return fd, leaf, nil
}

// tenantIDs: sk (c_<slug>) sistem kullanıcısının uid/gid'i.
func tenantIDs(sk string) (uid, gid int, ok bool) {
	uu, err := userLookup(sk)
	if err != nil {
		return 0, 0, false
	}
	return uu.UID, uu.GID, true
}

// withinHome: p, home'un (symlink-çözülmüş) altında mı? restorecon-by-path gibi
// artık-kalan path işlemlerini jail'e sınırlamak için son bir emniyet kemeri.
func withinHome(home, p string) bool {
	hr, err := filepath.EvalSymlinks(home)
	if err != nil {
		hr = home
	}
	pr, err := filepath.EvalSymlinks(p)
	if err != nil {
		pr = p
	}
	return pr == hr || strings.HasPrefix(pr, hr+string(filepath.Separator))
}

// restoreconFd: fd'nin PİNLENMİŞ gerçek yolunu (/proc/self/fd/N → kernel çözer, saldırgan
// symlink'ine bağışık) alıp, hâlâ home altındaysa restorecon çalıştırır. Enforcing SELinux
// sunucularda root'un oluşturduğu dosya doğru context (httpd_sys_content_t) almazsa
// nginx/php-fpm erişemez; bu yüzden ŞART. within-home kontrolü relabel'ı jail'e sınırlar.
func restoreconFd(home string, f *os.File) {
	real, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	if err != nil || !withinHome(home, real) {
		return
	}
	_, _ = exec.Command("restorecon", real).CombinedOutput()
}

// fchownRestoreFd: fd'yi tenant'a chown (symlink-güvenli: fd üzerinden Fchown) + SELinux
// context'i düzelt. Eski path-tabanlı chown(abs, sk) os.Chown symlink TAKİP EDERDİ →
// /etc/shadow'u tenant'a devretme (LPE) riski; Fchown pinlenmiş inode'da çalışır.
func fchownRestoreFd(home string, f *os.File, sk string) {
	if uid, gid, ok := tenantIDs(sk); ok {
		_ = unix.Fchown(int(f.Fd()), uid, gid)
	}
	restoreconFd(home, f)
}

// ---- Yüksek seviye, symlink-güvenli mutasyonlar ----

// chmodBeneath: symlink-güvenli chmod. Leaf'i openat2 ile (symlink ise REDDEDİLİR) açıp
// Fchmod uygular; ara-bileşen takası kernel tarafından engellenir.
func chmodBeneath(home, rel string, mode uint32) error {
	f, err := openAt2Beneath(home, rel, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Fchmod(int(f.Fd()), mode)
}

// writeBeneath: symlink-güvenli dosya yazma (oluştur/truncate). Mevcut dosyanın izinleri
// korunur (open, create-dışında mode'a dokunmaz); yeni dosya createMode alır. Ardından fd
// üzerinden tenant'a chown + restorecon.
func writeBeneath(home, rel string, data []byte, createMode uint32, sk string) error {
	// ── ATOMIK YAZMA (veri-kaybı guard) ────────────────────────────────────
	// Eski O_TRUNC yolu, disk/kota dolduğunda dosyayı önce sıfırlar sonra
	// yazamaz → içerik kaybı. Artık: aynı dizinde geçici dosyaya yaz + fsync,
	// sonra Renameat ile hedefe ata (rename atomiktir). Temp yazımı başarısızsa
	// (ENOSPC/EDQUOT) hedef dosyaya HİÇ dokunulmaz → orijinal korunur.
	pfd, leaf, err := safeParentFd(home, rel)
	if err != nil {
		return err
	}
	defer unix.Close(pfd)

	// Mevcut dosyanın izinlerini koru (varsa); yoksa createMode.
	mode := createMode
	if st, serr := fstatatLeaf(pfd, leaf); serr == nil {
		mode = st.Mode & 0o7777
	}

	tmp := "." + leaf + ".gosp-tmp"
	// Kalıntı temp varsa temizle (önceki yarım yazım).
	_ = unix.Unlinkat(pfd, tmp, 0)
	tfd, err := unix.Openat(pfd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return err
	}
	tf := os.NewFile(uintptr(tfd), tmp)
	yaz := func() error {
		if _, e := tf.Write(data); e != nil {
			return e // ENOSPC/EDQUOT burada yakalanır — hedef henüz DOKUNULMADI
		}
		if e := tf.Sync(); e != nil { // fsync: rename öncesi veri diske insin
			return e
		}
		// Yeni dosya için tenant chown + SELinux etiketi (mevcut dosya rename ile
		// zaten kendi etiketini korur; yine de tutarlılık için uygula).
		fchownRestoreFd(home, tf, sk)
		return nil
	}
	if e := yaz(); e != nil {
		tf.Close()
		_ = unix.Unlinkat(pfd, tmp, 0) // yarım temp'i sil, hedef bozulmadı
		return e
	}
	tf.Close()
	// Atomik geçiş: temp → hedef (aynı dizin fd'si içinde, symlink takip etmez).
	if e := unix.Renameat(pfd, tmp, pfd, leaf); e != nil {
		_ = unix.Unlinkat(pfd, tmp, 0)
		return e
	}
	return nil
}

// fstatatLeaf: parent fd içindeki leaf'in Stat_t'si (symlink TAKİP EDİLMEZ).
func fstatatLeaf(pfd int, leaf string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(pfd, leaf, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

// createExclBeneath: symlink-güvenli yeni-boş-dosya (O_EXCL). Zaten varsa unix.EEXIST.
func createExclBeneath(home, rel, sk string) error {
	f, err := openAt2Beneath(home, rel, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0644)
	if err != nil {
		return err
	}
	fchownRestoreFd(home, f, sk)
	return f.Close()
}

// copyStreamBeneath: symlink-güvenli akışlı yazma (upload). src'den fd'ye kopyalar.
func copyStreamBeneath(home, rel string, src io.Reader, sk string) (int64, error) {
	f, err := openAt2Beneath(home, rel, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, src)
	if err != nil {
		return n, err
	}
	fchownRestoreFd(home, f, sk)
	return n, nil
}

// mkdirAllBeneath: symlink-güvenli `mkdir -p`. Her bileşeni Mkdirat + O_NOFOLLOW openat ile
// yürür; herhangi bir bileşen symlink ise O_NOFOLLOW REDDEDER. Yeni oluşturulan dizinler
// (sk != "") tenant'a chown edilir.
func mkdirAllBeneath(home, rel, sk string) error {
	p := relClean(rel)
	hf, err := openHomeFd(home)
	if err != nil {
		return err
	}
	if p == "" || p == "." {
		unix.Close(hf)
		return nil
	}
	dirfd := hf
	uid, gid, haveIDs := tenantIDs(sk)
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." {
			continue
		}
		created := false
		if err := unix.Mkdirat(dirfd, part, 0755); err == nil {
			created = true
		} else if err != unix.EEXIST {
			unix.Close(dirfd)
			return err
		}
		nfd, err := unix.Openat(dirfd, part, dirOpenFlags, 0)
		unix.Close(dirfd)
		if err != nil {
			return err
		}
		dirfd = nfd
		if created && haveIDs {
			_ = unix.Fchown(dirfd, uid, gid)
		}
	}
	unix.Close(dirfd)
	return nil
}

// renameBeneath: symlink-güvenli rename/move. Kaynak ve hedef PARENT'ları openat2 ile
// pinler, Renameat ile taşır (rename final-bileşen symlink'ini TAKİP ETMEZ, girdiyi taşır).
func renameBeneath(home, oldRel, newRel, sk string) error {
	if err := mkdirAllBeneath(home, filepath.Dir(relClean(newRel)), sk); err != nil {
		return err
	}
	of, oleaf, err := safeParentFd(home, oldRel)
	if err != nil {
		return err
	}
	defer unix.Close(of)
	nf, nleaf, err := safeParentFd(home, newRel)
	if err != nil {
		return err
	}
	defer unix.Close(nf)
	return unix.Renameat(of, oleaf, nf, nleaf)
}

// removeAllBeneath: symlink-güvenli `rm -rf`. Parent'ı pinler, leaf'i (dosya/symlink ise
// unlink; dizin ise fd-özyinelemeli unlinkat) siler. Hiçbir adımda symlink takip edilmez.
func removeAllBeneath(home, rel string) error {
	pfd, leaf, err := safeParentFd(home, rel)
	if err != nil {
		return err
	}
	defer unix.Close(pfd)
	return removeAt(pfd, leaf)
}

// removeAt: dirfd'ye göre name'i özyinelemeli sil (tüm işlemler pinlenmiş fd'lere göreli,
// O_NOFOLLOW → symlink asla takip edilmez, jail-dışı silme imkânsız).
func removeAt(dirfd int, name string) error {
	if err := unix.Unlinkat(dirfd, name, 0); err == nil {
		return nil
	} else if err == unix.ENOENT {
		return nil
	} else if err != unix.EISDIR && err != unix.EPERM && err != unix.ENOTEMPTY {
		return err
	}
	cfd, err := unix.Openat(dirfd, name, dirOpenFlags, 0)
	if err != nil {
		return err
	}
	names, rerr := readdirnamesFd(cfd)
	if rerr != nil {
		unix.Close(cfd)
		return rerr
	}
	for _, n := range names {
		if n == "." || n == ".." {
			continue
		}
		if e := removeAt(cfd, n); e != nil {
			unix.Close(cfd)
			return e
		}
	}
	unix.Close(cfd)
	return unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
}

// readdirnamesFd: raw dir fd'yi (sahipliğini almadan) listeler. Dup + os.File ile okuyup
// dup'ı kapatır; asıl fd çağırana kalır.
func readdirnamesFd(dirfd int) ([]string, error) {
	dup, err := unix.Dup(dirfd)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(dup), "dir")
	names, err := f.Readdirnames(-1)
	f.Close()
	return names, err
}

// chownTreeBeneath: dst alt-ağacını symlink-güvenli özyinelemeli tenant'a chown eder
// (Fchownat AT_SYMLINK_NOFOLLOW → symlink'in KENDİSİ chown edilir, hedefi değil).
func chownTreeBeneath(home, rel, sk string) error {
	uid, gid, ok := tenantIDs(sk)
	if !ok {
		return nil // kullanıcı yok → sessiz atla (test/kenar durum)
	}
	pfd, leaf, err := safeParentFd(home, rel)
	if err != nil {
		return err
	}
	defer unix.Close(pfd)
	return chownAt(pfd, leaf, uid, gid)
}

func chownAt(dirfd int, name string, uid, gid int) error {
	if err := unix.Fchownat(dirfd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	cfd, err := unix.Openat(dirfd, name, dirOpenFlags, 0)
	if err != nil {
		return nil // dosya/symlink → alt yok
	}
	names, rerr := readdirnamesFd(cfd)
	if rerr != nil {
		unix.Close(cfd)
		return rerr
	}
	for _, n := range names {
		if n == "." || n == ".." {
			continue
		}
		if e := chownAt(cfd, n, uid, gid); e != nil {
			unix.Close(cfd)
			return e
		}
	}
	unix.Close(cfd)
	return nil
}

// copyTreeBeneath: symlink-güvenli özyinelemeli kopya. Kaynak ve hedef PARENT'ları pinler;
// dosyaları O_NOFOLLOW ile kopyalar (jail-dışı symlink İÇERİĞİ okunmaz → bilgi sızması yok),
// symlink'leri (readlink+symlinkat ile) OLDUĞU gibi yeniden kurar, dizinleri özyineler.
func copyTreeBeneath(home, srcRel, dstRel, sk string) error {
	if err := mkdirAllBeneath(home, filepath.Dir(relClean(dstRel)), sk); err != nil {
		return err
	}
	sfd, sleaf, err := safeParentFd(home, srcRel)
	if err != nil {
		return err
	}
	defer unix.Close(sfd)
	dfd, dleaf, err := safeParentFd(home, dstRel)
	if err != nil {
		return err
	}
	defer unix.Close(dfd)
	uid, gid, haveIDs := tenantIDs(sk)
	return copyEntryAt(sfd, sleaf, dfd, dleaf, uid, gid, haveIDs)
}

func copyEntryAt(sdir int, sname string, ddir int, dname string, uid, gid int, haveIDs bool) error {
	var st unix.Stat_t
	if err := unix.Fstatat(sdir, sname, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if err := unix.Mkdirat(ddir, dname, st.Mode&0o777); err != nil && err != unix.EEXIST {
			return err
		}
		ncd, err := unix.Openat(ddir, dname, dirOpenFlags, 0)
		if err != nil {
			return err
		}
		defer unix.Close(ncd)
		if haveIDs {
			_ = unix.Fchown(ncd, uid, gid)
		}
		nsd, err := unix.Openat(sdir, sname, dirOpenFlags, 0)
		if err != nil {
			return err
		}
		defer unix.Close(nsd)
		names, rerr := readdirnamesFd(nsd)
		if rerr != nil {
			return rerr
		}
		for _, n := range names {
			if n == "." || n == ".." {
				continue
			}
			if e := copyEntryAt(nsd, n, ncd, n, uid, gid, haveIDs); e != nil {
				return e
			}
		}
		return nil
	case unix.S_IFLNK:
		target, err := readlinkAt(sdir, sname)
		if err != nil {
			return err
		}
		_ = unix.Unlinkat(ddir, dname, 0)
		return unix.Symlinkat(target, ddir, dname)
	case unix.S_IFREG:
		return copyRegAt(sdir, sname, ddir, dname, st.Mode&0o777, uid, gid, haveIDs)
	default:
		return nil // özel dosyaları atla
	}
}

func readlinkAt(dirfd int, name string) (string, error) {
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(dirfd, name, buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

func copyRegAt(sdir int, sname string, ddir int, dname string, perm uint32, uid, gid int, haveIDs bool) error {
	sf, err := unix.Openat(sdir, sname, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(sf), sname)
	defer in.Close()
	df, err := unix.Openat(ddir, dname, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, perm)
	if err != nil {
		return err
	}
	out := os.NewFile(uintptr(df), dname)
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if haveIDs {
		_ = unix.Fchown(df, uid, gid)
	}
	return nil
}
