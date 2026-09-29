// Package sshaccess: host (hosting hesabı) bazlı SSH erişimi.
// Her domain = bir Linux kullanıcısı (c_<slug>). SSH erişimi, kullanıcının
// login shell'ini /bin/bash (açık) ↔ /usr/sbin/nologin (kapalı) arasında
// değiştirerek yönetilir. Sunucudaki sshd'de AllowUsers/AllowGroups OLMADIĞI
// için (doğrulandı) shell-toggle güvenli + yeterlidir; sshd_config'e HİÇ
// dokunulmaz → root veya başka hesap kilitlenmez.
package sshaccess

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sys/unix"
)

const (
	shellAcik   = "/bin/bash"
	shellKapali = "/usr/sbin/nologin"
	sshPort     = 22
)

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

type durum struct {
	AlanAdi    string `json:"alan_adi"`
	Kullanici  string `json:"kullanici"`
	Aktif      bool   `json:"aktif"`
	Shell      string `json:"shell"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	AnahtarVar bool   `json:"anahtar_var"`
	IsDemo     bool   `json:"is_demo"`
}

// gecerliSK: yalnızca panel'in oluşturduğu c_<slug> kullanıcılarında işlem
// yapılmasını garanti eder (komut enjeksiyonu / yanlış hesap koruması).
func gecerliSK(sk string) bool {
	if !strings.HasPrefix(sk, "c_") || len(sk) < 3 {
		return false
	}
	return !strings.ContainsAny(sk, "/ .;|&$`\n\r\t\"'")
}

func currentShell(sk string) string {
	out, err := exec.Command("getent", "passwd", sk).Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) >= 7 {
		return parts[6]
	}
	return ""
}

// ---- Kiracı ~/.ssh ağacında symlink-güvenli işlemler ----
//
// /home/<sk> kiracıya ait (0710) ve ~/.ssh'i kiracı yönetir: FTP/SSH/PHP ile
// `.ssh` ya da `.ssh/authorized_keys` yerine /root/.ssh/… gösteren bir
// sembolik bağ ya da sabit bağ koyabilir. Panel root olarak sözcüksel yolla
// (MkdirAll + WriteFile + `chown -R` + `restorecon -R`) çalıştığında yazım bu
// bağın hedefine gidiyordu: yönetici kiracının anahtarını kaydettiğinde
// /root/.ssh/authorized_keys ezilir (izin root:0600 korunur, StrictModes geçer)
// → kiracı root olarak SSH ile girer.
//
// ÇÖZÜM (toplu/devir_symlink.go homeAltiAc ve files/safeio.go writeBeneath ile
// aynı teknik): ev dizini O_NOFOLLOW ile pinlenir; `.ssh` Mkdirat + Openat
// (O_DIRECTORY|O_NOFOLLOW) ile ev fd'sine göre açılır; authorized_keys pinli
// dizinde geçici dosyaya (O_EXCL) yazılıp Renameat ile yerine konur — rename
// hedefteki dizin girdisini değiştirir, bir symlink ya da sabit bağın gösterdiği
// inode'a ASLA yazmaz (denetim ile rename arasındaki yarışta bile). Sahiplik fd
// üzerinden Fchown ile verilir (`chown -R` yok), etiket yalnız pinli fd'nin
// kernel'in çözdüğü yolu ev altındaysa ve özyinelemesiz verilir.

// Test dikişleri — yalnız _test.go değiştirir; üretimde gerçek değerler.
var (
	// evKoku: kiracı ev dizinlerinin kökü (/home/<sk>).
	evKoku = "/home"
	// kimlikCoz: sk → uid/gid.
	kimlikCoz = kimlikCozGercek
	// restoreconCalistir: TEK bir yolu özyinelemesiz yeniden etiketler.
	restoreconCalistir = func(yol string) { _ = exec.Command("restorecon", yol).Run() }
	// geciciAd: authorized_keys'in yazıldığı geçici dosyanın adı (tahmin edilemez).
	geciciAd = func() string {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		return ".authorized_keys.gosp-" + hex.EncodeToString(b)
	}
)

// errGuvensizYol: ev dizini, ~/.ssh ya da authorized_keys sembolik bağ, sabit bağ
// ya da beklenmeyen türde — hedefe dokunulmadan reddedildi.
var errGuvensizYol = errors.New("güvenli olmayan yol: ev dizini, ~/.ssh ya da authorized_keys sembolik/sabit bağ ya da beklenmeyen türde")

func kimlikCozGercek(sk string) (uid, gid int, err error) {
	u, err := user.Lookup(sk)
	if err != nil {
		return -1, -1, err
	}
	if uid, err = strconv.Atoi(u.Uid); err != nil {
		return -1, -1, err
	}
	if gid, err = strconv.Atoi(u.Gid); err != nil {
		return -1, -1, err
	}
	return uid, gid, nil
}

// guvensizMi: O_NOFOLLOW'un symlink'e (ELOOP) ya da dizin beklenirken başka türe
// (ENOTDIR) çarpmasını errGuvensizYol'a çevirir.
func guvensizMi(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%w (%v)", errGuvensizYol, err)
	}
	return err
}

// evAc: /home/<sk>'yı symlink izlemeden dizin olarak açar (fd'yi çağıran kapatır).
func evAc(sk string) (int, error) {
	if sk == "" || sk == "." || sk == ".." || strings.ContainsRune(sk, '/') {
		return -1, errGuvensizYol
	}
	fd, err := unix.Open(filepath.Join(evKoku, sk), unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, guvensizMi(err)
	}
	return fd, nil
}

// sshDiziniAc: ~/.ssh'yi yoksa 0700 oluşturur, ev fd'sine göre symlink izlemeden
// açar ve sahipliğini fd üzerinden kiracıya verir (fd'yi çağıran kapatır).
func sshDiziniAc(evFd, uid, gid int) (int, error) {
	if err := unix.Mkdirat(evFd, ".ssh", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, err
	}
	fd, err := unix.Openat(evFd, ".ssh", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, guvensizMi(err)
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func fdYolu(fd int) (string, error) {
	return os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
}

// restoreconFd: pinli fd'nin kernel'in çözdüğü gerçek yolu hâlâ kiracının evi
// altındaysa o TEK yolu özyinelemesiz yeniden etiketler (files/safeio.go
// restoreconFd ile aynı kemer). Kiracı dizini ev dışına taşıdıysa etiketleme yok.
func restoreconFd(evFd, fd int) {
	ev, err := fdYolu(evFd)
	if err != nil {
		return
	}
	yol, err := fdYolu(fd)
	if err != nil || !strings.HasPrefix(yol, ev+"/") {
		return
	}
	restoreconCalistir(yol)
}

// sshDiziniHazirla: Ayarla(aktif=true) için ~/.ssh iskeleti (symlink-güvenli).
func sshDiziniHazirla(sk string) error {
	uid, gid, err := kimlikCoz(sk)
	if err != nil {
		return fmt.Errorf("kullanıcı kimliği: %w", err)
	}
	evFd, err := evAc(sk)
	if err != nil {
		return err
	}
	defer unix.Close(evFd)
	sshFd, err := sshDiziniAc(evFd, uid, gid)
	if err != nil {
		return err
	}
	defer unix.Close(sshFd)
	restoreconFd(evFd, sshFd)
	return nil
}

// authorizedKeysYaz: ~/.ssh/authorized_keys'i govde ile symlink/sabit bağ
// güvenli biçimde değiştirir; sahiplik kiracı, izin 0600.
func authorizedKeysYaz(sk string, govde []byte) error {
	uid, gid, err := kimlikCoz(sk)
	if err != nil {
		return fmt.Errorf("kullanıcı kimliği: %w", err)
	}
	evFd, err := evAc(sk)
	if err != nil {
		return err
	}
	defer unix.Close(evFd)
	sshFd, err := sshDiziniAc(evFd, uid, gid)
	if err != nil {
		return err
	}
	defer unix.Close(sshFd)

	// Var olan authorized_keys yalnız tek bağlantılı normal dosyaysa kabul edilir.
	// Sembolik bağ, sabit bağ (Nlink>1), dizin, FIFO → saldırı işareti: reddet.
	var st unix.Stat_t
	if err := unix.Fstatat(sshFd, "authorized_keys", &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
			return errGuvensizYol
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}

	tmp := geciciAd()
	// O_EXCL: ad önceden varsa (kiracının koyduğu symlink dahil) izlemeden EEXIST.
	tfd, err := unix.Openat(sshFd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	tf := os.NewFile(uintptr(tfd), tmp)
	defer tf.Close()
	yazErr := func() error {
		if _, e := tf.Write(govde); e != nil {
			return e
		}
		if e := unix.Fchown(tfd, uid, gid); e != nil {
			return e
		}
		return tf.Sync()
	}()
	if yazErr != nil {
		_ = unix.Unlinkat(sshFd, tmp, 0)
		return yazErr
	}
	if err := unix.Renameat(sshFd, tmp, sshFd, "authorized_keys"); err != nil {
		_ = unix.Unlinkat(sshFd, tmp, 0)
		return err
	}
	restoreconFd(evFd, sshFd)
	restoreconFd(evFd, tfd) // rename sonrası fd'nin yolu .ssh/authorized_keys
	return nil
}

// anahtarVar: ~/.ssh/authorized_keys normal ve boş olmayan bir dosya mı?
// Yol boyunca HİÇBİR symlink izlenmez: eskiden root olarak os.Stat symlink'i
// izleyip ev dışındaki bir dosyanın varlığını (GET MusteriScope) sızdırıyordu.
func anahtarVar(sk string) bool {
	evFd, err := evAc(sk)
	if err != nil {
		return false
	}
	defer unix.Close(evFd)
	fd, err := unix.Openat2(evFd, ".ssh/authorized_keys", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return false
	}
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Size > 0
}

func (h *Handlers) yukle(r *http.Request) (id int64, sk, alanAdi string, demo bool, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &alanAdi, &isDemo)
	if err != nil {
		return id, "", "", false, false
	}
	return id, sk, alanAdi, isDemo == 1, true
}

// GET /domains/{id}/ssh — mevcut SSH erişim durumu
func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	_, sk, alanAdi, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	shell := currentShell(sk)
	httpx.WriteJSON(w, http.StatusOK, durum{
		AlanAdi:    alanAdi,
		Kullanici:  sk,
		Aktif:      shell == shellAcik,
		Shell:      shell,
		SSHHost:    h.IPv4,
		SSHPort:    sshPort,
		AnahtarVar: anahtarVar(sk),
		IsDemo:     demo,
	})
}

// PUT /domains/{id}/ssh  body {"aktif": true|false} — shell'i değiştir
func (h *Handlers) Ayarla(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin SSH erişimi değiştirilemez")
		return
	}
	if !gecerliSK(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		Aktif bool `json:"aktif"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	shell := shellKapali
	if req.Aktif {
		shell = shellAcik
	}
	if out, err := exec.Command("usermod", "-s", shell, sk).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "shell değiştirilemedi: "+strings.TrimSpace(string(out)))
		return
	}
	if req.Aktif {
		// ~/.ssh iskeleti (anahtar yüklemeye hazır) — symlink-güvenli: eski
		// MkdirAll + `chown -R` + `restorecon -R` kiracının bağlarını izliyordu.
		if err := sshDiziniHazirla(sk); err != nil {
			log.Printf("SSH: %s için ~/.ssh hazırlanamadı: %v", sk, err)
		}
		// SSH parolasını FTP parolasıyla eşitle (parola = FTP)
		_ = hesaplar.SyncSSHPassword(h.DB, sk)
		// Chroot jail kur + gosp-ssh grubuna ekle (kendi home'una hapset)
		_ = exec.Command("/usr/local/bin/girginospanel-jail", "setup", sk).Run()
		_ = exec.Command("groupadd", "-f", "gosp-ssh").Run()
		_ = exec.Command("gpasswd", "-a", sk, "gosp-ssh").Run()
	} else {
		// SSH kapalı: gruptan çıkar + jail söktür + parolayı kilitle
		_ = exec.Command("gpasswd", "-d", sk, "gosp-ssh").Run()
		_ = exec.Command("/usr/local/bin/girginospanel-jail", "teardown", sk).Run()
		_ = hesaplar.LockSSHPassword(sk)
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET ssh_erisim=? WHERE id=?`, b2i(req.Aktif), id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "durum kaydedilemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "aktif": req.Aktif, "shell": shell, "kullanici": sk,
	})
}

// PUT /domains/{id}/ssh/anahtar  body {"anahtar": "ssh-ed25519 ..."} — authorized_keys yaz
func (h *Handlers) AnahtarKaydet(w http.ResponseWriter, r *http.Request) {
	_, sk, _, demo, ok := h.yukle(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin SSH anahtarı değiştirilemez")
		return
	}
	if !gecerliSK(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		Anahtar string `json:"anahtar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	anahtar := strings.TrimSpace(req.Anahtar)
	// Her satır ssh-/ecdsa-/sk- ile başlamalı (boş = anahtarları temizle demek)
	if anahtar != "" {
		for _, line := range strings.Split(anahtar, "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			if !strings.HasPrefix(l, "ssh-") && !strings.HasPrefix(l, "ecdsa-") && !strings.HasPrefix(l, "sk-") {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz SSH anahtarı: her satır ssh-/ecdsa-/sk- ile başlamalı")
				return
			}
		}
	}
	body := ""
	if anahtar != "" {
		body = anahtar + "\n"
	}
	if err := authorizedKeysYaz(sk, []byte(body)); err != nil {
		if errors.Is(err, errGuvensizYol) {
			log.Printf("🔴 SSH anahtarı YAZILMADI (%s): %v — olası symlink/sabit bağ saldırısı", sk, err)
			httpx.WriteError(w, http.StatusConflict, "authorized_keys yazılmadı: "+err.Error()+". Hesaptaki bağ kaldırılmalı.")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "authorized_keys yazılamadı: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "anahtar_var": anahtar != ""})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EnsureInfra: panel açılışında SSH jail altyapısını hazırlar (idempotent + best-effort).
//   - girginospanel-jail script'ini /usr/local/bin'e yerleştirir
//   - gosp-ssh grubunu oluşturur
//   - sshd Match chroot config'ini yerleştirir — YALNIZCA `sshd -t` geçerse reload eder,
//     geçersizse eski haline döndürür (sshd'yi asla bozmaz).
func EnsureInfra() {
	const srcDir = "/opt/girginospanel/src/scripts"
	// 1) jail script
	if data, err := os.ReadFile(srcDir + "/girginospanel-jail"); err == nil {
		if e := os.WriteFile("/usr/local/bin/girginospanel-jail", data, 0o755); e == nil {
			_ = os.Chmod("/usr/local/bin/girginospanel-jail", 0o755)
		}
	} else {
		log.Printf("🔴 SSH IZOLASYON: girginospanel-jail betigi %s icinde YOK — jail kurulamaz (%v)", srcDir, err)
	}
	// 2) gosp-ssh grubu
	_ = exec.Command("groupadd", "-f", "gosp-ssh").Run()
	// 3) sshd Match chroot config — güvenli uygula
	dst := "/etc/ssh/sshd_config.d/50-gosp-jail.conf"
	src, err := os.ReadFile(srcDir + "/50-gosp-jail.conf")
	if err != nil {
		// 🔴 KRITIK: bu dosya deploy paketinde (assets/ops) yoksa sshd chroot
		// Match blogu HIC yazilmaz → SSH acilan kiracilar chroot'suz TAM kabuk
		// alir. Eskiden burada sessizce return vardi ve kimse fark etmiyordu.
		log.Printf("🔴 SSH IZOLASYON UYGULANMADI: %s/50-gosp-jail.conf YOK — SSH acilan kiraciyi chroot'a hapsedemem (%v). Deploy paketine (assets/ops) ekleyin.", srcDir, err)
		return
	}
	cur, _ := os.ReadFile(dst)
	if string(cur) == string(src) {
		return // zaten güncel
	}
	if e := os.WriteFile(dst, src, 0o644); e != nil {
		log.Printf("jail sshd config yazılamadı: %v", e)
		return
	}
	if out, e := exec.Command("sshd", "-t").CombinedOutput(); e != nil {
		// geçersiz → geri al, sshd'yi bozma
		if len(cur) > 0 {
			_ = os.WriteFile(dst, cur, 0o644)
		} else {
			_ = os.Remove(dst)
		}
		log.Printf("jail sshd config geçersiz, uygulanmadı: %s", strings.TrimSpace(string(out)))
		return
	}
	_ = exec.Command("systemctl", "reload", "sshd").Run()
	log.Printf("SSH jail altyapısı hazır (script + gosp-ssh + sshd chroot config)")
}
