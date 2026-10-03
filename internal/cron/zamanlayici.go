package cron

// zamanlayici.go — kiracı cron görevlerinin KAYNAK DİLİMİNDE çalıştırılması.
//
// 🔴 NEDEN (181 canlı ölçüm, 2026-10-03): görevler eskiden /var/spool/cron/<sk>
// dosyasına HAM yazılıyordu. crond (system.slice/crond.service) işi PAM ile
// açar; system-auth → pam_systemd işi user-<UID>.slice/session-*.scope'a koyar.
// O dilimde sınır yoktur (MemoryMax/CPUQuota=infinity, TasksMax=20664): kiracı
// cron ile plan RAM/CPU/süreç tavanını (girginos-<sk>.slice) tümden aşıyordu —
// fork bombası, madencilik, komşuları OOM ile düşürme.
//
// MODEL:
//   - Depo: /var/lib/girginospanel/cron/<sk> (kök 0600; crontab biçimi, panel
//     okur/yazar). crond bu dosyayı OKUMAZ; kiracı yazamaz.
//   - Zamanlayıcı: /etc/cron.d/girginospanel-cron-<sk> (kök 0600). Satırlarda
//     kiracı girdisi olarak YALNIZ dilbilgisi denetimli zaman alanları vardır;
//     komut satırı sabittir: "root <panel> cron-calistir <sk> <görev-kimliği>".
//     Kiracı komutu kök kabuğuna HİÇ girmez.
//   - Çalıştırıcı (CalistirCLI, kök): görevi depodan kimliğiyle bulur (değişmiş/
//     silinmiş/pasif → çalıştırmaz), askıdaysa çalıştırmaz, sonra kiraci.Komut
//     (Dilim) ile: systemd-run --scope --slice=girginos-<sk>.slice → runuser -u
//     <sk> → /bin/sh -c <komut>; ortam asgari (panel sırları yok), dizin $HOME,
//     cron '%' kuralı korunur.
//   - Askı: depo /var/lib/girginospanel/cron-suspended/<sk>'ye taşınır (görev
//     yoksa boş işaret dosyası), zamanlayıcı dosyası silinir. Askı dosyası varken
//     çalıştırıcı ve uzlaştırma çalıştırmaz/üretmez.
//   - Geçiş (Uzlastir, açılış + periyodik): /var/spool/cron/c_* dosyaları depoya
//     alınır ve silinir; zamanlayıcı dosyaları depodan yeniden üretilir; artıklar
//     silinir. İdempotent.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"girginospanel/internal/kiraci"
)

// Yollar — testler geçici dizinlerle değiştirir; üretimde SABİTTİR.
var (
	depoKok     = "/var/lib/girginospanel/cron"
	askiKok     = "/var/lib/girginospanel/cron-suspended" // provisioner'ın eski askı deposuyla aynı (geriye uyum)
	spoolKok    = "/var/spool/cron"
	cronDKok    = "/etc/cron.d"
	calistirici = "/opt/girginospanel/bin/girginospanel-server"

	kullaniciVar = func(sk string) bool { _, err := user.Lookup(sk); return err == nil }
	restorecon   = func(p string) { _, _ = exec.Command("restorecon", p).CombinedOutput() }

	// kiraciIcin — görevin koşacağı kiracı: kimlik + ev + KAYNAK DİLİMİ.
	kiraciIcin = func(sk string) kiraci.Kiraci {
		return kiraci.Kiraci{Kul: sk, Ev: filepath.Join(kiraci.EvKoku, sk), Dilim: true}
	}
)

// cronDOnek — panelin /etc/cron.d dosyalarının adı öneki. cronie '.' ile
// başlayan dosyaları okumaz: geçici dosyalar o yüzden '.' ile başlar.
const cronDOnek = "girginospanel-cron-"

// CLIKomutu — panel ikilisinin çalıştırıcı alt komutu (main.go).
const CLIKomutu = "cron-calistir"

// yazKilit — depo/askı/zamanlayıcı dosyalarının oku-değiştir-yaz işlemleri.
var yazKilit sync.Mutex

func depoYolu(sk string) string  { return filepath.Join(depoKok, sk) }
func askiYolu(sk string) string  { return filepath.Join(askiKok, sk) }
func spoolYolu(sk string) string { return filepath.Join(spoolKok, sk) }
func cronDYolu(sk string) string { return filepath.Join(cronDKok, cronDOnek+sk) }

func varMi(p string) bool { _, err := os.Lstat(p); return err == nil }

// askida — hesabın cron'u askıda mı (askı deposu var).
func askida(sk string) bool { return varMi(askiYolu(sk)) }

// reZaman — tek zaman alanı: virgüllü liste; her öğe * | sayı | 3 harfli ad,
// isteğe bağlı -aralık ve /adım. Boşluk ve başka karakter YOK.
var reZaman = func() *regexp.Regexp {
	oge := `(\*|[0-9]{1,2}|[A-Za-z]{3})(-([0-9]{1,2}|[A-Za-z]{3}))?(/[0-9]{1,2})?`
	return regexp.MustCompile(`^` + oge + `(,` + oge + `)*$`)
}()

func zamanGecerli(f string) bool { return len(f) <= 100 && reZaman.MatchString(f) }

func zamanlarGecerli(g Gorev) bool {
	for _, f := range []string{g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta} {
		if !zamanGecerli(f) {
			return false
		}
	}
	return true
}

// reKimlik — çalıştırıcıya verilen görev kimliği.
var reKimlik = regexp.MustCompile(`^[0-9a-f]{16}$`)

// gorevKimlik — görevin içerik özeti: zaman + tip + komut. Görev değişir ya da
// silinirse eski zamanlayıcı satırı hiçbir göreve eşleşmez (yarış penceresinde
// başka görev çalışmaz).
func gorevKimlik(sk string, g Gorev) string {
	h := sha256.Sum256([]byte(strings.Join([]string{sk, g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta, g.Tip, g.Komut}, "\x00")))
	return hex.EncodeToString(h[:8])
}

// cronDIcerik — kiracının zamanlayıcı dosyası; zamanlanacak görev yoksa nil.
// Pasif görevler ve zaman alanı dilbilgisine uymayanlar (eski kayıt) yazılmaz.
func cronDIcerik(sk string, list []Gorev) (icerik []byte, atlanan int) {
	var b strings.Builder
	n := 0
	for _, g := range list {
		if !g.Etkin {
			continue
		}
		if !zamanlarGecerli(g) {
			atlanan++
			continue
		}
		fmt.Fprintf(&b, "%s %s %s %s %s root %s %s %s %s\n",
			g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta, calistirici, CLIKomutu, sk, gorevKimlik(sk, g))
		n++
	}
	if n == 0 {
		return nil, atlanan
	}
	bas := "# girginospanel — " + sk + " zamanlanmış görevleri. Panel üretir; elle düzenlemeyin.\n" +
		"# Kaynak: " + depoYolu(sk) + ". Görevler kiracı kimliğinde, " + kiraci.DilimOnek + sk + ".slice içinde koşar.\n" +
		"SHELL=/bin/sh\nPATH=/usr/sbin:/usr/bin:/sbin:/bin\nMAILTO=\"\"\n"
	return []byte(bas + b.String()), atlanan
}

// dosyaYaz — kök 0600 dosyayı atomik yazar (aynı dizinde '.'-önekli geçici dosya).
func dosyaYaz(p string, veri []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(p), "."+filepath.Base(p)+".tmp")
	if err := os.WriteFile(tmp, veri, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// cronDYaz — zamanlayıcı dosyasını listeden üretir; içerik aynıysa dokunmaz,
// zamanlanacak görev yoksa dosyayı siler.
func cronDYaz(sk string, list []Gorev) error {
	p := cronDYolu(sk)
	icerik, atlanan := cronDIcerik(sk, list)
	if atlanan > 0 {
		log.Printf("cron: %s: %d görevin zaman alanı geçersiz — zamanlanmadı", sk, atlanan)
	}
	if icerik == nil {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if eski, err := os.ReadFile(p); err == nil && string(eski) == string(icerik) {
		return nil
	}
	if err := os.MkdirAll(cronDKok, 0o755); err != nil {
		return err
	}
	if err := dosyaYaz(p, icerik); err != nil {
		return err
	}
	restorecon(p)
	return nil
}

// cronDUzlastir — tek hesabın zamanlayıcı dosyasını duruma göre üretir/siler.
func cronDUzlastir(sk string) error {
	if askida(sk) || !kullaniciVar(sk) {
		return cronDSil(sk)
	}
	list, err := okuDosya(depoYolu(sk))
	if err != nil {
		return err
	}
	return cronDYaz(sk, list)
}

func cronDSil(sk string) error {
	if err := os.Remove(cronDYolu(sk)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// anahtar — birleştirmede aynı görev ölçütü (zaman + komut).
func anahtar(g Gorev) string {
	return strings.Join([]string{g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta, g.Komut}, "\x00")
}

// birlestir — a'ya b'de olup a'da olmayan görevleri ekler (en çok maxGorev).
func birlestir(a, b []Gorev) []Gorev {
	var gor = map[string]bool{}
	for _, g := range a {
		gor[anahtar(g)] = true
	}
	for _, g := range b {
		if len(a) >= maxGorev {
			break
		}
		if !gor[anahtar(g)] {
			gor[anahtar(g)] = true
			a = append(a, g)
		}
	}
	return a
}

// tasiVeyaBirlestir — kaynak dosyadaki görevleri hedefe taşır (hedef yoksa
// rename, varsa birleştirip kaynağı siler).
func tasiVeyaBirlestir(sk, kaynak, hedef string) error {
	if !varMi(kaynak) {
		return nil
	}
	if !varMi(hedef) {
		if err := os.MkdirAll(filepath.Dir(hedef), 0o700); err != nil {
			return err
		}
		return os.Rename(kaynak, hedef)
	}
	k, err := okuDosya(kaynak)
	if err != nil {
		return err
	}
	h, err := okuDosya(hedef)
	if err != nil {
		return err
	}
	if err := dosyaYaz(hedef, metin(sk, birlestir(h, k))); err != nil {
		return err
	}
	return os.Remove(kaynak)
}

// spoolIceAl — eski modelin /var/spool/cron/<sk> dosyasını (panelin yazdığı ya da
// kiracının crontab(1) ile yarattığı) depoya — askıdaysa askı deposuna — alır
// ve siler: crond onu kiracı oturumunda (sınırsız user-<UID>.slice) koşturmasın.
// Görevler dosyadan AYRIŞTIRILARAK alınır (biçim dışı satırlar düşer); zaman
// alanı dilbilgisine uymayan görev depoda kalır ama zamanlanmaz.
func spoolIceAl(sk string) (int, error) {
	p := spoolYolu(sk)
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s düzenli dosya değil", p)
	}
	gelen, err := okuDosya(p)
	if err != nil {
		return 0, err
	}
	hedef := depoYolu(sk)
	if askida(sk) {
		hedef = askiYolu(sk)
	}
	var mevcut []Gorev
	if varMi(hedef) {
		if mevcut, err = okuDosya(hedef); err != nil {
			return 0, err
		}
	}
	once := len(mevcut)
	sonra := birlestir(mevcut, gelen)
	if err := dosyaYaz(hedef, metin(sk, sonra)); err != nil {
		return 0, err
	}
	if err := os.Remove(p); err != nil {
		return 0, err
	}
	return len(sonra) - once, nil
}

// Uzlastir — açılış/bakım uzlaştırması (idempotent): eski spool dosyalarını
// depoya alır, zamanlayıcı dosyalarını depodan yeniden üretir, artıkları siler.
func Uzlastir() {
	yazKilit.Lock()
	defer yazKilit.Unlock()
	for _, d := range []string{depoKok, askiKok} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			log.Printf("cron uzlaştırma: %s: %v", d, err)
			return
		}
	}
	// 1) Eski model: /var/spool/cron/c_* (kullanıcısı olmayan artığa dokunulmaz;
	// crond da onu çalıştırmaz).
	if ents, err := os.ReadDir(spoolKok); err == nil {
		for _, e := range ents {
			sk := e.Name()
			if !kiraci.GecerliSK(sk) || !kullaniciVar(sk) {
				continue
			}
			n, err := spoolIceAl(sk)
			if err != nil {
				log.Printf("cron uzlaştırma: %s: spool alınamadı: %v", sk, err)
				continue
			}
			log.Printf("cron uzlaştırma: %s: spool panel deposuna alındı (%d görev), kiracı dilimine taşındı", sk, n)
		}
	}
	// 2) Depodaki her hesabın zamanlayıcı dosyası.
	if ents, err := os.ReadDir(depoKok); err == nil {
		for _, e := range ents {
			sk := e.Name()
			if !kiraci.GecerliSK(sk) || !e.Type().IsRegular() {
				continue
			}
			if err := cronDUzlastir(sk); err != nil {
				log.Printf("cron uzlaştırma: %s: %v", sk, err)
			}
		}
	}
	// 3) Artık zamanlayıcı dosyaları: deposu yok / askıda / kullanıcısı yok.
	if ents, err := os.ReadDir(cronDKok); err == nil {
		for _, e := range ents {
			sk, ok := strings.CutPrefix(e.Name(), cronDOnek)
			if !ok {
				continue
			}
			if !kiraci.GecerliSK(sk) || !varMi(depoYolu(sk)) || askida(sk) || !kullaniciVar(sk) {
				if err := os.Remove(filepath.Join(cronDKok, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
					log.Printf("cron uzlaştırma: artık %s silinemedi: %v", e.Name(), err)
				}
			}
		}
	}
}

// AskiUygula — hesap askısının cron adımı (idempotent). askida=true: depo askı
// deposuna taşınır (görev yoksa boş işaret yazılır), zamanlayıcı dosyası silinir.
// askida=false: askı deposu depoya döner, zamanlayıcı yeniden üretilir.
func AskiUygula(sk string, askiyaAl bool) error {
	if !kiraci.GecerliSK(sk) {
		return errBad
	}
	yazKilit.Lock()
	defer yazKilit.Unlock()
	if _, err := spoolIceAl(sk); err != nil {
		log.Printf("cron askı: %s: spool alınamadı: %v", sk, err)
	}
	if askiyaAl {
		if err := cronDSil(sk); err != nil {
			return err
		}
		if err := tasiVeyaBirlestir(sk, depoYolu(sk), askiYolu(sk)); err != nil {
			return err
		}
		if !varMi(askiYolu(sk)) {
			return dosyaYaz(askiYolu(sk), metin(sk, nil))
		}
		return nil
	}
	if err := tasiVeyaBirlestir(sk, askiYolu(sk), depoYolu(sk)); err != nil {
		return err
	}
	return cronDUzlastir(sk)
}

// Sil — hesap silinirken cron izleri (zamanlayıcı, depo, askı, eski spool).
func Sil(sk string) {
	if !kiraci.GecerliSK(sk) {
		return
	}
	yazKilit.Lock()
	defer yazKilit.Unlock()
	for _, p := range []string{cronDYolu(sk), depoYolu(sk), askiYolu(sk), spoolYolu(sk)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("cron sil: %s: %v", p, err)
		}
	}
}

// yuzdeAyir — cron(5) '%' kuralı (cronie do_command.c ile aynı): ilk kaçışsız
// '%' komutu bitirir, kalanı standart girdi olur ('%' → satır sonu); "\%" → '%'.
func yuzdeAyir(s string) (komut, girdi string) {
	var b strings.Builder
	kacis := false
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if kacis {
			if c != '%' {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
			kacis = false
			continue
		}
		if c == '\\' {
			kacis = true
			continue
		}
		if c == '%' {
			break
		}
		b.WriteByte(c)
	}
	if kacis {
		b.WriteByte('\\')
	}
	if i >= len(s) {
		return b.String(), ""
	}
	var g strings.Builder
	kacis, satirGerek := false, false
	for _, c := range []byte(s[i+1:]) {
		if kacis {
			if c != '%' {
				g.WriteByte('\\')
			}
		} else if c == '%' {
			c = '\n'
		}
		kacis = c == '\\'
		if !kacis {
			g.WriteByte(c)
		}
		satirGerek = c != '\n'
	}
	if kacis {
		g.WriteByte('\\')
	}
	if satirGerek {
		g.WriteByte('\n')
	}
	return b.String(), g.String()
}

// evdeCalistir — kiracı kabuğu önce $HOME'a geçer (crond davranışı), sonra komutu
// AYRI argüman olarak ($1) yeni kabukta çalıştırır: komut dizgeye eklenmez.
const evdeCalistir = `cd "$HOME" || exit 1; exec /bin/sh -c "$1"`

// gorevKomutu — görevin çalıştırılacak komutu: kiracı kimliği + kaynak dilimi
// (kiraciIcin) + asgari ortam; tip=komut için cron '%' kuralı.
func gorevKomutu(ctx context.Context, sk string, g Gorev) (*exec.Cmd, error) {
	komut, girdi := g.Komut, ""
	if g.Tip == "" || g.Tip == "komut" {
		komut, girdi = yuzdeAyir(g.Komut)
	}
	cmd, err := kiraciIcin(sk).Komut(ctx, "/bin/sh", "-c", evdeCalistir, "gosp-cron", komut)
	if err != nil {
		return nil, err
	}
	if girdi != "" {
		cmd.Stdin = strings.NewReader(girdi)
	}
	return cmd, nil
}

// CalistirCLI — "girginospanel-server cron-calistir <sk> <kimlik>" (kök, crond'dan).
// config.Load'dan ÖNCE çağrılır: panel sırlarına gerek yok. Çıkış kodu görevinkidir;
// görev bulunamaz/askıdaysa 0 (crond'a gürültü yok, stderr'e not).
func CalistirCLI(args []string) int {
	if len(args) != 2 || !kiraci.GecerliSK(args[0]) || !reKimlik.MatchString(args[1]) {
		fmt.Fprintln(os.Stderr, "kullanım: cron-calistir <c_kullanici> <görev-kimliği>")
		return 2
	}
	sk, kimlik := args[0], args[1]
	if askida(sk) {
		fmt.Fprintf(os.Stderr, "cron: %s askıda — görev çalıştırılmadı\n", sk)
		return 0
	}
	list, err := okuDosya(depoYolu(sk))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cron: %s: depo okunamadı: %v\n", sk, err)
		return 1
	}
	for _, g := range list {
		if !g.Etkin || !zamanlarGecerli(g) || gorevKimlik(sk, g) != kimlik {
			continue
		}
		cmd, err := gorevKomutu(context.Background(), sk, g)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cron: %s: %v\n", sk, err)
			return 1
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && ee.ExitCode() > 0 {
				return ee.ExitCode()
			}
			fmt.Fprintf(os.Stderr, "cron: %s: %v\n", sk, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "cron: %s: %s kimlikli etkin görev yok (değişmiş/silinmiş)\n", sk, kimlik)
	return 0
}
