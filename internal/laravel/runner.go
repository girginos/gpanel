// Package laravel: per-domain Laravel Toolkit (Plesk paritesi).
//
// GÜVENLİK ÇEKİRDEĞİ: tüm artisan/composer/npm/git komutları tenant kullanıcısı
// (c_xxx) OLARAK, tenant ev dizini altında, panel sırları sızdırılmadan çalışır.
// kiraci.Komut (runuser; sudo DEĞİL) + sıfırdan env (panel ortamı yok) + mutlak binary
// yolları (PATH hijack yok) + context timeout + çıktı cap. Root'a geri dönüş yok.
package laravel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/kiraci"

	"golang.org/x/sys/unix"
)

const (
	composerBin = "/usr/local/bin/composer"
	ciktiCap    = 64 << 10 // 64KB çıktı tavanı
	kisaTimeout = 120 * time.Second
	// kopukSure — istemciden kopuk (httpx.KopukBaglam) komut zincirinin üst süresi;
	// tek komut yine kisaTimeout ile sınırlı.
	kopukSure   = 15 * time.Minute
	sistemPATH  = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	cronDDizin  = "/etc/cron.d"
	logAltDizin = "logs" // /home/c_x/logs
)

// badURLChars: repo URL'de yasak shell/enjeksiyon metakarakterleri (git.go ile aynı)
const badURLChars = "\"'`$();|&<>\\"

// logRootDizin: detached job (kur/deploy) logları ROOT-sahipli, tenant-YAZILAMAZ
// dizinde tutulur. Aksi halde tenant ~/logs/x.log → /etc/passwd symlink'i ile root'un
// (systemd StandardOutput=append + Go OpenFile) symlink-takibini kötüye kullanır.
const logRootDizin = "/var/log/girginos-laravel"

func ensureLogRoot() { _ = os.MkdirAll(logRootDizin, 0755) } // root:root 0755

// execSem: tenant başına eşzamanlı senkron komut sınırı (host tükenmesi DoS savunması).
var execSem sync.Map

func execGate(sk string) func() {
	v, _ := execSem.LoadOrStore(sk, make(chan struct{}, 3))
	c := v.(chan struct{})
	c <- struct{}{}
	return func() { <-c }
}

// idKilit: domain başına mutex (kur/deploy/queue check-then-act yarışını serileştirir).
var idKilit sync.Map

func kilitle(id int64) func() {
	v, _ := idKilit.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

var (
	reArg         = regexp.MustCompile(`^[A-Za-z0-9:_.,=/@-]+$`) // serbest argüman
	reComposerPkg = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*)/[a-z0-9]([a-z0-9._-]*)(:[\^~<>=0-9.* |,-]+)?$`)
	reNodeSurum   = regexp.MustCompile(`^[0-9]{1,2}(\.[0-9]{1,3}){0,2}$`)
)

// gecerliSK: sistem kullanıcısı c_ ile başlamalı (cross-tenant/root koruması)
func gecerliSK(sk string) bool { return strings.HasPrefix(sk, "c_") && len(sk) > 2 }

// reANSI: ANSI renk/stil kaçış dizileri. Laravel composer.json 'package:discover
// --ansi' ile renk zorlar (bizim --no-ansi'yi ezer) → çıktıyı UI için temizleriz.
var reANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

func ansiTemizle(s string) string { return reANSI.ReplaceAllString(s, "") }

// phpBin: domains.php_surum ("8.3") → CLI binary. /usr/bin/php83 vb. mevcut; yoksa
// varsayılan /usr/bin/php. Mutlak yol → tenant PATH hijack'i etkisiz.
func phpBin(surum string) string {
	kod := strings.ReplaceAll(strings.TrimSpace(surum), ".", "")
	if kod != "" {
		cand := "/usr/bin/php" + kod
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return "/usr/bin/php"
}

// guvenliAppDir: app_root ("public_html" veya "public_html/alt") → dogrulanmis MUTLAK
// dizin. public_html ALTINDA olmalı (docroot confinement + operatör kararı). '..'/
// mutlak/garip-karakter reddi + EvalSymlinks ile ara-symlink kaçışı kapalı.
func guvenliAppDir(sk, appRoot string) (string, error) {
	if !gecerliSK(sk) {
		return "", fmt.Errorf("geçersiz kullanıcı")
	}
	ar := strings.Trim(strings.TrimSpace(appRoot), "/")
	if ar == "" {
		ar = "public_html"
	}
	if strings.Contains(ar, "..") || !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(ar) {
		return "", fmt.Errorf("geçersiz uygulama dizini")
	}
	// 🔴 Gizli dizin (.ssh/.cron) secilemez; kapsam TENANT HOME (Laravel kodu
	// belge kokunun DISINDA olabilir — public_html kardesi laravel_11 gibi).
	if strings.HasPrefix(ar, ".") || strings.Contains(ar, "/.") {
		return "", fmt.Errorf("gizli dizin seçilemez")
	}
	base := evKoku + "/" + sk
	abs := filepath.Clean(base + "/" + ar)
	if abs != base && !strings.HasPrefix(abs, base+"/") {
		return "", fmt.Errorf("dizin ev dizini dışına çıkamaz")
	}
	// Symlink kaçışını kapat — leaf henüz yoksa (kur öncesi) EN DERİN MEVCUT atayı çöz
	// ve base altında olduğunu doğrula. Aksi halde public_html→/etc symlink'iyle mkdir
	// root'un keyfi konumda dizin yaratması sömürülür (denetim bulgusu #4).
	kontrol := abs
	for kontrol == base || strings.HasPrefix(kontrol, base+"/") {
		if real, err := filepath.EvalSymlinks(kontrol); err == nil {
			if real != base && !strings.HasPrefix(real, base+"/") {
				return "", fmt.Errorf("dizin (symlink dahil) ev dizini dışına çıkamaz")
			}
			break // en derin mevcut ata base altında — güvenli
		}
		if kontrol == base {
			break // public_html yok — tenant olarak oluşturulacak (root değil)
		}
		kontrol = filepath.Dir(kontrol)
	}
	return abs, nil
}

// tenantEnv: alt-sürece verilecek SIFIRDAN ortam — panel sırları YOK.
func tenantEnv(sk string) []string {
	return []string{
		"HOME=/home/" + sk,
		"USER=" + sk, "LOGNAME=" + sk,
		"PATH=" + sistemPATH,
		"COMPOSER_HOME=/home/" + sk + "/.composer",
		"COMPOSER_ALLOW_SUPERUSER=0",
		"NPM_CONFIG_CACHE=/home/" + sk + "/.npm",
		"LANG=C.UTF-8",
	}
}

// TenantExec: kısa/senkron komut. runuser -u sk -- bin args... ; cwd tenant home
// altında (EvalSymlinks confine); timeout + çıktı cap. bin MUTLAK yol olmalı.
func TenantExec(ctx context.Context, sk, cwd, bin string, args ...string) (string, bool) {
	return tenantExecEnv(ctx, sk, cwd, tenantEnv(sk), bin, args...)
}

// tenantExecEnv: TenantExec çekirdeği + özel env (npm için PATH prefix vb.).
func tenantExecEnv(ctx context.Context, sk, cwd string, env []string, bin string, args ...string) (string, bool) {
	if !gecerliSK(sk) {
		return "geçersiz kullanıcı", false
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil || !(real == "/home/"+sk || strings.HasPrefix(real, "/home/"+sk+"/")) {
		return "çalışma dizini tenant ev dizini dışında", false
	}
	release := execGate(sk) // tenant başına eşzamanlılık sınırı (DoS savunması)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, kisaTimeout)
	defer cancel()
	ham, runErr := tenantCalistir(ctx, sk, real, env, append([]string{bin}, args...)...)
	if errors.Is(runErr, kiraci.ErrKomut) {
		return "geçersiz kullanıcı", false
	}
	out := ansiTemizle(ham) // ANSI renk kodlarını temizle (Laravel --ansi zorlar)
	if len(out) > ciktiCap {
		out = out[len(out)-ciktiCap:]
	}
	if ctx.Err() == context.DeadlineExceeded {
		return out + "\n\n[zaman aşımı: komut 120sn'de tamamlanmadı, sonlandırıldı]", false
	}
	return out, runErr == nil
}

// tenantCalistir: tenantExecEnv'in süreç çekirdeği — argv'yi kiracı (sk) kimliğinde
// dir'de çalıştırır, birleşik çıktı döner. kiraci.Komut: runuser -u sk -- argv
// (kabuk yok), temel ortam PATH/HOME/LANG — panel ortamı (sırlar) ASLA geçmez; env
// yalnız bunun üzerine eklenir. artisan/composer/npm kiracının KODUNU yükler →
// ortam burada genişletilmez. Kiracı olamayacak kullanıcı → kiraci.ErrKomut.
// Doğrulama (gecerliSK, dir kiracı evinde) çağıranın işidir. Kilit: sir_cocuk_test.go.
func tenantCalistir(ctx context.Context, sk, dir string, env []string, argv ...string) (string, error) {
	// Dilim: artisan/composer/npm kiracı kodu — plan dilimine girer (RAM/CPU/süreç tavanı).
	cmd, err := kiraci.Kiraci{Kul: sk, Ev: "/home/" + sk, Dilim: true}.Komut(ctx, argv...)
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, env...)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err = cmd.Run()
	return buf.String(), err
}

// systemdRunDetached: uzun tek-seferlik iş (create-project, deploy). Transient unit
// olarak tenant uid/gid + cgroup slice içinde başlatır; çıktı logPath'e append edilir.
// Panel goroutine'i beklemez → frontend log-tail poll eder. Unit adı döner.
func systemdRunDetached(sk, cwd, unit, logPath string, argv ...string) error {
	if !gecerliSK(sk) {
		return fmt.Errorf("geçersiz kullanıcı")
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil || !(real == "/home/"+sk || strings.HasPrefix(real, "/home/"+sk+"/")) {
		return fmt.Errorf("çalışma dizini tenant ev dizini dışında")
	}
	// log ROOT-sahipli, tenant-YAZILAMAZ dizinde (symlink-takip felaketi önlenir —
	// denetim #1). Tenant burada symlink yaratamaz; chown YOK. Taze başla.
	ensureLogRoot()
	_ = os.Remove(logPath)
	sr := []string{
		"--unit=" + unit,
		"--uid=" + sk, "--gid=" + sk,
		"--working-directory=" + real,
		"--slice=girginos-" + sk + ".slice",
		"-p", "RuntimeMaxSec=1800",
		"-p", "StandardOutput=append:" + logPath,
		"-p", "StandardError=append:" + logPath,
		"-E", "HOME=/home/" + sk,
		"-E", "PATH=" + sistemPATH,
		"-E", "COMPOSER_HOME=/home/" + sk + "/.composer",
		"-E", "COMPOSER_ALLOW_SUPERUSER=0",
		"-E", "NPM_CONFIG_CACHE=/home/" + sk + "/.npm",
		"-E", "LANG=C.UTF-8",
		"--",
	}
	sr = append(sr, argv...)
	out, err := exec.Command("systemd-run", sr...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, string(out))
	}
	return nil
}

// unitDurum: transient/servis unit ActiveState (tek -p → --value güvenli).
func unitDurum(unit string) string {
	out, _ := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", unit).Output()
	return strings.TrimSpace(string(out))
}

// dosyaTail: dosyanın son max byte'ı (log kuyruğu). O_NOFOLLOW + düzenli-dosya +
// LimitReader → symlink takibi (/etc/shadow) ve /dev/zero OOM'u reddedilir (denetim #3/#5).
func dosyaTail(path string, max int64) string {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi.Size() > max {
		_, _ = f.Seek(-max, io.SeekEnd)
	}
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return ansiTemizle(string(b)) // deploy/kur logları da ANSI içerebilir
}

// gecerliRepoURL: git.go ile aynı katılık (shell metakarakter reddi + şema).
func gecerliRepoURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" || len(u) > 512 {
		return false
	}
	for _, c := range u {
		if c <= ' ' || strings.ContainsRune(badURLChars, c) {
			return false
		}
	}
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://")
}

// envMaxBoyut: .env / .env.example okuma tavanı (OOM savunması).
const envMaxBoyut = 2 << 20

// errEnvYok: .env (ya da uygulama dizini) gerçekten YOK. envOku'nun diğer hataları
// (yolda symlink → ELOOP, düzenli olmayan dosya, boyut tavanı) "yok" DEĞİLDİR: dosya
// büyük olasılıkla vardır ama root onu güvenle okuyamaz. Bunları "boş .env" gibi
// göstermek düzenleyicide kaydedilince gerçek .env'in ezilmesine yol açardı.
var errEnvYok = errors.New(".env yok")

// errEnvGuvensiz: .env var olabilir ama güvenle okunamadı (EnvOku/EnvYaz 409 döner).
var errEnvGuvensiz = errors.New(".env güvenle okunamadı (yolda sembolik bağlantı, düzenli olmayan dosya ya da boyut tavanı); panel düzenleyicisi bu dosyayı göstermez ve üzerine yazmaz — dosyayı FTP/SSH ile düzenleyin")

// envOku: appRoot/.env oku. Yol /home/<sk> köküne çapalı openat2 ile, HİÇBİR bileşende
// symlink izlenmeden açılır (evAltiDosyaOku) → tenant'ın .env'i ya da uygulama dizininin
// bir ara bileşenini /etc/girginospanel/env veya başka tenant dizinine symlink'lemesiyle
// (kontrol-kullanım yarışı dahil) root'un panel sırlarını/cross-tenant okuması VE
// /dev/zero OOM'u reddedilir (denetim #2/#5). Sahiplik denetlenmez (eski davranış).
// Dönüş hatası: yalnız gerçekten yoksa errEnvYok; diğer her durumda errEnvGuvensiz'i
// saran hata (çağıran "yok" ile "okunamadı"yı ayırt eder).
// Davranış otomatik_dosya_test.go'da (evKoku sahte ev) ve kaynak koruma testinde kilitli.
func envOku(sk, appDir string) (string, error) {
	home, rel, err := evGoreli(sk, appDir)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errEnvGuvensiz, err)
	}
	b, err := evAltiDosyaOku(home, path.Join(rel, ".env"), -1, envMaxBoyut)
	if err != nil {
		if yokHatasi(err) {
			return "", errEnvYok
		}
		return "", fmt.Errorf("%w: %v", errEnvGuvensiz, err)
	}
	return string(b), nil
}

// envYazilabilir: PUT /laravel/env öncesi denetim. Mevcut .env root tarafından güvenle
// okunamıyorsa (errEnvGuvensiz) yazım REDDEDİLİR: düzenleyici onu boş göstermiş olabilir
// ve kiracı-tee symlink'i izleyerek gerçek dosyanın üzerine yazardı. Yoksa ya da
// okunabiliyorsa nil.
func envYazilabilir(sk, appDir string) error {
	if _, err := envOku(sk, appDir); err != nil && !errors.Is(err, errEnvYok) {
		return err
	}
	return nil
}

// ---- Ev köküne çapalı, symlink İZLEMEYEN root dosya işlemleri ----
//
// guvenliAppDir yalnız uygulama DİZİNİNİN string'ini kontrol anında doğrular. Root'un
// sonradan o dizindeki DOSYALAR üzerinde yol (string) ile yaptığı os.ReadFile/os.Chmod/
// os.Remove/chown işlemleri hem son bileşendeki hem ara bileşenlerdeki symlink'leri izler;
// tenant bu dizinde symlink oluşturabildiği (ve kontrolden sonra takas edebildiği) için
// root başka tenant'ın ya da sistemin dosyasına yönlendirilebilir. Aşağıdaki yardımcılar
// yolu ev köküne göre openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS) ile TEK atomik adımda
// çözer ve işlemi fd/*at-syscall üzerinden yapar (aynı teknik: internal/files/safeio.go,
// internal/toplu/devir_symlink.go).

// evAltiResolve: yol boyunca hiçbir symlink izlenmez, ev kökünün dışına çıkılamaz.
const evAltiResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS

// sahiplikMaxDerinlik: özyinelemeli chown'da en çok dizin derinliği (fd tükenmesi savunması).
const sahiplikMaxDerinlik = 64

// evKoku: kiracı ev dizinlerinin kökü. Yalnız testler değiştirir (t.TempDir sahte evi);
// üretimde daima /home. evAlti* çağıran her yol ev kökünü evGoreli/evDizini'den alır.
// Bu dosyadaki ve gorevkuyruk.go'daki (cronDizini) dikişlerin üretim değeri ve test dışı
// kodda yeniden bağlanmaması TestKaynakKoruma_DikisDegerleri ile kilitli.
var evKoku = "/home"

// kimlikBul: sistem kullanıcısı → uid/gid. Test dikişi (üretimde tenantKimlik).
var kimlikBul = tenantKimlik

// yarisKancasi: YALNIZ testler atar (üretimde nil). Denetim ile işlem arasındaki pencerede
// çağrılır; testler burada adı takas ederek (renameat2 RENAME_EXCHANGE) yarışı
// deterministik yeniden üretir. asama: "mkdir" (Mkdirat → Openat arası), "sahiplik"
// (girdi açılıp denetlendikten sonra, chown'dan önce).
var yarisKancasi func(asama string, dfd int, ad string)

func yarisNoktasi(asama string, dfd int, ad string) {
	if yarisKancasi != nil {
		yarisKancasi(asama, dfd, ad)
	}
}

// yokHatasi: hata "yol yok" mu (ENOENT; ara bileşen dizin değil → ENOTDIR)?
func yokHatasi(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR)
}

// evDizini: <evKoku>/<sk> (sk c_ ile başlamalı).
func evDizini(sk string) (string, error) {
	if !gecerliSK(sk) || strings.Contains(sk, "/") {
		return "", fmt.Errorf("geçersiz kullanıcı")
	}
	return evKoku + "/" + sk, nil
}

// evGoreli: <evKoku>/<sk>/... mutlak uygulama dizinini (ev kökü, köke göre yol) ikilisine ayırır.
func evGoreli(sk, appDir string) (home, rel string, err error) {
	if home, err = evDizini(sk); err != nil {
		return "", "", err
	}
	abs := filepath.Clean(appDir)
	if abs == home {
		return home, ".", nil
	}
	if !strings.HasPrefix(abs, home+"/") {
		return "", "", fmt.Errorf("dizin ev dizini dışında")
	}
	return home, strings.TrimPrefix(abs, home+"/"), nil
}

// tenantKimlik: sistem kullanıcısının uid/gid'i.
func tenantKimlik(sk string) (uid, gid int, err error) {
	u, err := user.Lookup(sk)
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

// evAltiAc: rel'i home köküne göre, yol boyunca HİÇBİR symlink izlemeden ve home dışına
// çıkmadan açar; ham fd döner (çağıran kapatmalı). Home'un kendisi O_NOFOLLOW ile açılır
// (/home root'a aittir; tenant ev girdisini takas edemez).
func evAltiAc(home, rel string, flags int) (int, error) {
	hf, err := unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(hf)
	p := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if p == "" {
		p = "."
	}
	how := &unix.OpenHow{Flags: uint64(flags) | unix.O_CLOEXEC, Resolve: evAltiResolve}
	return unix.Openat2(hf, p, how)
}

// fdDuzenliVeSahip: açılmış fd DÜZENLİ dosya mı ve (sahipUID >= 0 ise) sahibi sahipUID mi?
// Denetim yol üzerinden değil, açılmış inode üzerinden yapılır (yarışa bağışık).
func fdDuzenliVeSahip(fd int, ad string, sahipUID int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%s düzenli bir dosya değil", ad)
	}
	if sahipUID >= 0 && int(st.Uid) != sahipUID {
		return fmt.Errorf("%s kiracıya ait değil", ad)
	}
	return nil
}

// evAltiDosyaOku: home altındaki rel dosyasını symlink izlemeden okur. Yalnız düzenli
// dosya (FIFO/dizin/aygıt reddi), sahipUID >= 0 ise yalnız o uid'ye ait dosya, en çok max
// bayt (fazlası hata — sessiz kesme geri-yazımda veriyi bozardı).
func evAltiDosyaOku(home, rel string, sahipUID int, max int64) ([]byte, error) {
	fd, err := evAltiAc(home, rel, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path.Base(rel))
	defer f.Close()
	if err := fdDuzenliVeSahip(fd, path.Base(rel), sahipUID); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s çok büyük", path.Base(rel))
	}
	return b, nil
}

// evAltiChmod: symlink izlemeden, yalnız sahipUID'ye ait düzenli dosyada Fchmod.
func evAltiChmod(home, rel string, sahipUID int, mode uint32) error {
	fd, err := evAltiAc(home, rel, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := fdDuzenliVeSahip(fd, path.Base(rel), sahipUID); err != nil {
		return err
	}
	return unix.Fchmod(fd, mode)
}

// evAltiSil: relDir dizinini symlink izlemeden açıp içindeki adları Unlinkat ile siler
// (Unlinkat son bileşen symlink'ini izlemez; dizinleri silmez). Olmayan dosya hata değil.
func evAltiSil(home, relDir string, adlar []string) error {
	dfd, err := evAltiAc(home, relDir, unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return err
	}
	defer unix.Close(dfd)
	var ilk error
	for _, ad := range adlar {
		if ad == "" || ad == "." || ad == ".." || strings.Contains(ad, "/") {
			continue
		}
		if err := unix.Unlinkat(dfd, ad, 0); err != nil && !errors.Is(err, unix.ENOENT) && ilk == nil {
			ilk = err
		}
	}
	return ilk
}

// dizinBos: fd'nin gösterdiği dizinde "." ve ".." dışında girdi var mı? fd'nin okuma
// konumunu ilerletir (çağıran fd'yi yalnız *at çağrılarının kökü olarak kullanır).
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

// evAltiMkdirHepsi: `mkdir -p home/rel`in symlink-güvenli karşılığı. Her bileşen home'dan
// başlayarak O_NOFOLLOW ile açılır (bileşen symlink ise ELOOP → durur, dışarıda dizin
// yaratılmaz); eksik bileşen Mkdirat ile yaratılıp fd üzerinden tenant'a verilir ve izni
// (umask'tan bağımsız) mode yapılır. Var olan bileşenin sahipliğine/iznine dokunulmaz.
// Göç kalıntısı root-sahipli üst dizinde (ör. bootstrap/) de çalışır.
// Yarış: Mkdirat ile Openat arasında kiracı adı aynı üst dizindeki başka (ör. root
// sahipli, dolu) bir dizinle takas edebilir; yeni yarattığımız dizin BOŞ olmak zorunda
// olduğundan açılan dizin boş değilse sahiplik VERİLMEZ, hata döner.
func evAltiMkdirHepsi(home, rel string, uid, gid int, mode uint32) error {
	fd, err := unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	p := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if p == "" {
		unix.Close(fd)
		return nil
	}
	for _, parca := range strings.Split(p, "/") {
		yeni := false
		if err := unix.Mkdirat(fd, parca, mode); err == nil {
			yeni = true
		} else if !errors.Is(err, unix.EEXIST) {
			unix.Close(fd)
			return err
		}
		if yeni {
			yarisNoktasi("mkdir", fd, parca)
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
			if berr != nil {
				unix.Close(nfd)
				return berr
			}
			err = unix.Fchown(nfd, uid, gid)
			if err == nil {
				err = unix.Fchmod(nfd, mode)
			}
			if err != nil {
				unix.Close(nfd)
				return err
			}
		}
		fd = nfd
	}
	unix.Close(fd)
	return nil
}

// evAltiSahiplikVer: `chown -R uid:gid home/rel`in symlink-güvenli karşılığı. Kök dizin
// openat2 ile çapalanır; alt ağaç fd üzerinden yürünür: dizinler O_NOFOLLOW ile açılır,
// symlink'lerin YALNIZ kendisi (lchown) değiştirilir, hedefleri asla. Başka yerde de
// bağlı (Nlink>1) ve kiracıya ait olmayan dosyalar atlanır (hardlink ile başka inode'u
// kiracıya devretme savunması).
func evAltiSahiplikVer(home, rel string, uid, gid int) error {
	fd, err := evAltiAc(home, rel, unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return err
	}
	d := os.NewFile(uintptr(fd), path.Base(rel))
	defer d.Close()
	return sahiplikAgaci(d, uid, gid, 0)
}

func sahiplikAgaci(d *os.File, uid, gid, derinlik int) error {
	if derinlik > sahiplikMaxDerinlik {
		return fmt.Errorf("dizin ağacı çok derin")
	}
	dfd := int(d.Fd())
	if err := unix.Fchown(dfd, uid, gid); err != nil {
		return err
	}
	adlar, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	var ilk error
	not := func(e error) {
		if e != nil && ilk == nil {
			ilk = e
		}
	}
	for _, ad := range adlar {
		not(sahiplikGirdi(dfd, ad, uid, gid, derinlik))
	}
	return ilk
}

// sahiplikGirdi: dfd altındaki TEK girdi. Ad yalnız BİR KEZ çözülür: O_PATH|O_NOFOLLOW
// ile inode'a fd alınır (symlink ise linkin kendisi); tür/Nlink/sahip denetimi ve chown
// AYNI fd üzerinden yapılır (Fstatat/Fchownat AT_EMPTY_PATH). Eskiden ad Fstatat ile
// denetlenip Fchownat ile yeniden çözülüyordu; arada ad takasıyla (RENAME_EXCHANGE)
// denetlenmemiş bir hardlink'in sahipliği devredilebiliyordu.
func sahiplikGirdi(dfd int, ad string, uid, gid, derinlik int) error {
	fd, err := unix.Openat(dfd, ad, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil // yarışta kaybolmuş olabilir
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstatat(fd, "", &st, unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil
	}
	if st.Mode&unix.S_IFMT == unix.S_IFDIR {
		// Aynı inode'u okumak için yeniden aç ("." — ad yeniden çözülmez).
		cfd, err := unix.Openat(fd, ".", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil
		}
		c := os.NewFile(uintptr(cfd), ad)
		defer c.Close()
		return sahiplikAgaci(c, uid, gid, derinlik+1)
	}
	// Başka yerde de bağlı (Nlink>1) ve kiracıya ait olmayan inode (dosya ya da symlink)
	// devredilmez: hardlink ile yabancı inode'un sahipliğini alma savunması.
	if st.Nlink > 1 && int(st.Uid) != uid {
		return nil
	}
	yarisNoktasi("sahiplik", dfd, ad)
	return unix.Fchownat(fd, "", uid, gid, unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW)
}

// envOrnektenUret: uygulama dizininde .env YOKSA .env.example'dan üretir.
// .env.example root tarafından yalnız ev köküne çapalı, symlink izlenmeden ve yalnız
// KİRACIYA AİT (uid) düzenli dosya ise okunur; içerik yaz() ile yazılır (üretimde envYaz:
// tenant uid'siyle tee). Eskiden düz os.ReadFile(.env.example) root olarak symlink izlerdi
// → root'un okuyabildiği her dosya (başka tenant .env'i, /etc/girginospanel/env) tenant'ın
// .env'ine kopyalanıp GET /laravel/env ile okunabilirdi.
// Dönüş: calisti=false → .env zaten var (adım kaydı yok).
func envOrnektenUret(home, appRel string, uid int, yaz func(string) error) (calisti, ok bool, not string) {
	dfd, err := evAltiAc(home, appRel, unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return true, false, "uygulama dizini açılamadı: " + err.Error()
	}
	var st unix.Stat_t
	e := unix.Fstatat(dfd, ".env", &st, unix.AT_SYMLINK_NOFOLLOW)
	unix.Close(dfd)
	if e == nil {
		return false, true, "" // .env (symlink dahil) zaten var → dokunma
	}
	b, err := evAltiDosyaOku(home, path.Join(appRel, ".env.example"), uid, envMaxBoyut)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return true, false, ".env.example yok"
		}
		return true, false, ".env.example okunamadı: " + err.Error()
	}
	if err := yaz(string(b)); err != nil {
		return true, false, err.Error()
	}
	return true, true, ".env.example kopyalandı"
}

// envYazici: PUT /laravel/env'in yazıcısı. Test dikişi: handler testleri
// (handler_davranis_test.go) onu sahteyle değiştirir ki runuser çalışmasın; üretimde daima
// envYaz. Kilitler: TestKaynakKoruma_DikisDegerleri değerin tam `envYaz` olduğunu ve test
// dışı kodda hiçbir yolla (atama/init/&/gölge/linkname) yeniden bağlanmadığını;
// TestKaynakKoruma_RootDosyaIslemleriCapali Handlers.EnvYaz'ın buradan yazdığını ve
// dikişin gösterdiği fonksiyona inerek yol tabanlı root işlemi olmadığını denetler.
var envYazici = envYaz

// envYaz: .env'i TENANT OLARAK yaz (runuser tee) → doğru sahiplik + uid-confined
// (tenant kendi izinleri dışına yazamaz; root symlink-follow felaketi YOK).
// Kaynak koruma testi: yalnız kiraci.Kiraci{...}.Komut (runuser -u sk, panel ortamı yok) —
// doğrudan exec ve root yazımı yasak.
func envYaz(sk, appDir, icerik string) error {
	if len(icerik) > 5<<20 {
		return fmt.Errorf(".env çok büyük")
	}
	dst := filepath.Join(appDir, ".env")
	cmd, err := kiraci.Kiraci{Kul: sk, Ev: "/home/" + sk}.Komut(context.Background(), "/usr/bin/tee", dst)
	if err != nil {
		return fmt.Errorf(".env yazılamadı: %v", err)
	}
	cmd.Env = append(cmd.Env, tenantEnv(sk)...)
	cmd.Stdin = strings.NewReader(icerik)
	var errBuf bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(".env yazılamadı: %v: %s", err, errBuf.String())
	}
	return nil
}
