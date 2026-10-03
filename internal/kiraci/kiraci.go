// Package kiraci — panelin (kök) bir kiracının (hosting sistem kullanıcısı)
// ev ağacında çalışmasının ortak yaprak deseni.
//
// 🔴 GÜVENLİK MODELİ (CWE-59 bağ izleme / kiracılar arası):
// Ev ağacı kiracının KENDİ denetimindedir: FTP/SSH/PHP/cron ile her bileşeni
// sembolik bağa çevirebilir, denetimden sonra takas edebilir. Bu yüzden kök bu
// ağaçta YOL DİZGESİYLE dosya işlemi yapmaz:
//   - kopyalama/oluşturma KİRACI kimliğinde koşar (Komut: runuser -u <kul> --,
//     kabuk yok, panel ortamı yok); çekirdek her erişimde DAC'ı uygular →
//     kiracı yalnız zaten erişebildiğine erişir, TOCTOU penceresi yoktur;
//   - kökte kalması gereken okuma/yazma internal/evalti ile yapılır: ev dizinine
//     çapalı openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS), yol boyunca HİÇBİR bağ
//     izlenmez. os.OpenRoot YETMEZ: ev dışına çıkan bağı reddeder ama ev İÇİNDE
//     kalan bağı izler — kiracının evinde kök sahipli bir alanı gösteren bağ kökün
//     işlemini oraya yönlendirir.
//
// <EvKoku> kök sahiplidir; kiracı buradaki kendi girdisini (ev dizinini) takas
// edemez → <EvKoku>/<sk> güvenilir bir çapadır.
//
// Panelin TEK runuser sarmalayıcısı Komut'tur (evalti ve taşıma bunu kullanır).
// Öncüller: sitekopya.kiraciKomut, archivex.runuserKomut ve git.kiraciCalistir
// (ayrı işte bu pakete geçirilir). Paket yapraktır: panelin başka paketini içe
// aktarmaz.
package kiraci

import (
	"context"
	"errors"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

// EvKoku — kiracı ev dizinlerinin kökü (provisioner: useradd -d /home/<sk>).
const EvKoku = "/home"

// iptalBekleme — bağlam iptalinde SIGTERM'den sonra sürecin kapanması için süre;
// dolunca Go süreci öldürür (runuser SIGKILL'i çocuğa iletemez).
const iptalBekleme = 15 * time.Second

// reSK — panelin ürettiği sistem kullanıcıları (provisioner.BenzersizKullanici:
// "c_" + [a-z0-9_], en çok 32). Yol bileşenine ve runuser argv'sine girer.
var reSK = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)

var (
	// ErrEvDisi — yol kiracının ev dizininin İÇİNDE değil (ev'in kendisi ya da dışı).
	ErrEvDisi = errors.New("yol kiracı ev dizininin içinde değil")
	// ErrSK — sk panelin ürettiği biçimde bir kiracı sistem kullanıcısı değil.
	ErrSK = errors.New("geçersiz kiracı kullanıcısı")
	// ErrKomut — Komut'a boş argv ya da kiracı olamayacak bir kullanıcı verildi.
	ErrKomut = errors.New("geçersiz kiracı komutu")
)

// GecerliSK — sk panelin ürettiği bir sistem kullanıcısı adı mı?
func GecerliSK(sk string) bool { return reSK.MatchString(sk) }

// Kiraci — işlem yapılacak kiracı.
type Kiraci struct {
	// Kul — runuser -u değeri: kiracının işletim sistemi kullanıcısı (üretimde sk).
	Kul string
	// Ev — ev dizini (mutlak): Komut'un HOME'u, Goreli'nin ve evalti.Ac'ın çapası.
	Ev string
}

// Komut — argv'yi kiracı kimliğinde çalıştıracak komutu hazırlar: runuser -u
// <Kul> -- argv (kabuk yok; argv[0] PATH'te aranır), panel ortamı AKTARILMAZ
// (yalnız PATH/HOME/LANG — panel sırları çocuğa sızmaz), çalışma dizini "/".
// Bağlam iptalinde önce SIGTERM (runuser çocuğa iletir), takılırsa iptalBekleme
// sonunda Go süreci öldürür. Stdin/Stdout/Stderr'i çağıran bağlar.
//
// Reddedilir (ErrKomut): boş argv ya da boş komut adı — runuser argv'siz
// kiracının KABUĞUNU açar; Kul'un sistemde bir kullanıcıya çözülmemesi (boş,
// '-' ile başlayan runuser seçeneği, yol dahil) ya da uid 0'a çözülmesi — kök
// kimliğinde "kiracı" komutu DAC korumasını tümden kaldırırdı.
func (k Kiraci) Komut(ctx context.Context, argv ...string) (*exec.Cmd, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, ErrKomut
	}
	if u, err := user.Lookup(k.Kul); err != nil || u.Uid == "0" {
		return nil, ErrKomut
	}
	cmd := exec.CommandContext(ctx, "runuser", append([]string{"-u", k.Kul, "--"}, argv...)...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + k.Ev,
		"LANG=C",
	}
	cmd.Dir = "/"
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = iptalBekleme
	return cmd, nil
}

// Goreli — mutlak yolun ev dizinine göreli karşılığı (evalti.Ac'a verilecek
// biçim). Yol temizlenir; ev'in kendisi, ev dışı ya da göreli yol → ErrEvDisi.
// Yalnız DİZGE denetimidir: yolun bileşenleri bağ olabilir, onları reddetmek
// evalti.Ac'ın ya da kiracı kimliğinin işidir.
func (k Kiraci) Goreli(yol string) (string, error) {
	rel, err := filepath.Rel(k.Ev, filepath.Clean(yol))
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", ErrEvDisi
	}
	return rel, nil
}
