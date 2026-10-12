package provisioner

// PHP mail() / sendmail yasağı — PHP-FPM havuzlarında düz mail çıkışı KAPALI; müşteri SMTP kullanır.
//
// Politika (operatör kararı, 2026-10-09): düz mail çıkışı istenmiyor — ele geçirilmiş / virüslü bir site
// mail() ile spam basabilir. Uygulamanın kendi SMTP ayarı (WordPress SMTP eklentisi, XenForo e-posta
// seçeneği…) etkilenmez. Her havuz php_admin_value[sendmail_path] ile hızlı-başarısız betiğe yönlendirilir:
// mail() HEMEN false döner, tek satır uygulama günlüğüne düşer. php_admin_value olduğundan kullanıcı
// ini_set / .user.ini / php_value ile EZEMEZ.
//
// 🔴 Kafesli (per-tenant) havuzlarda ayrıca bir hata vardı (180 ve 181'de ölçüldü, 2026-10-09; VPS'te
// yeniden üretildi): tenant birimi NoNewPrivileges=yes + RestrictSUIDSGID=yes ile koşar; bu, postfix'in
// setgid `postdrop`'unu etkisiz kılar. SELinux PERMISSIVE/kapalı sunucuda mail() → sendmail → postdrop
// maildrop'a yazamaz ("mail_queue_enter: create file maildrop/…: Permission denied") ve postdrop bunu
// SONSUZA DEK, 10 sn'de bir journal'a uyarı yazarak yeniden dener (180: 16 çift, en eskisi 45 gün, journal
// satırlarının %67'si = 138K/gün). PHP işçisi pclose()'da takılır; işçi reload ile öldürülse bile
// sendmail+postdrop çifti YETİM (ppid=1) kalıp cgroup'ta yaşamayı sürdürür — yalnız birimin yeniden
// başlatılması onu bitirir. (SELinux ENFORCING'de sendmail /etc/postfix/main.cf'i okuyamayıp hemen düşer:
// orada akış yok, ama her çağrıda "sendmail: fatal" satırı kalır.) Eski sürümün bıraktığı yetim postdrop'lar
// panel açılışında bir kez temizlenir (mailTakiliPostdropOldur).
//
// Kapsam — tenant kodunu çalıştıran TÜM havuz yazıcıları aynı satırı yazar (MailYasakPoolSatiri):
//   - kafesli havuz:      tenantfpm.go renderTenantPool
//   - paylaşılan master:  provisioner.go phpPoolTmpl (writePoolValidated), php.poolTmpl (ApplyToFilesystem)
//   - alt alan havuzları: php.subPoolTmpl (ApplyNamedPool) — kafesli hesabın alt alanı da paylaşılan
//     master'da, kafesin DIŞINDA koşar; orada postdrop ÇALIŞIR ve mail() gerçekten posta çıkarır.
//
// Şablonlar yalnız YENİ yazımları kapsar ve HealVhostsOnStartup tek seferliktir (sentinel) → MEVCUT
// paylaşılan-master dosyaları için her açılışta idempotent bir tarama satırı ekler
// (MailYasakHavuzlariniKapatOnStartup). Dokunulmayanlar: dağıtımın www.conf'u ile phpmyadmin.conf /
// roundcube.conf (panelin kendi havuzları, tenant kodu çalıştırmaz) ve PHP CLI/cron (ayrı yüzey).

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"girginospanel/internal/kilit"
)

//go:embed mailyasak/girginospanel-mail-yasak
var mailYasakBetik []byte

const (
	mailYasakYol = "/usr/local/bin/girginospanel-mail-yasak"
	// mailYasakSendmailPath: havuzdaki php_admin_value[sendmail_path] değeri. PHP'nin varsayılan
	// "/usr/sbin/sendmail -t -i" biçimini korur (betik argümanları yok sayar, yalnız -bs'e bakar).
	mailYasakSendmailPath = mailYasakYol + " -t -i"
)

// Dikişler (testler geçici dizin / sahte /proc / sahte komut ile koşar).
var (
	mailYasakBetikYolu = mailYasakYol
	mailYasakEtiketle  = func(yol string) { _, _ = exec.Command("restorecon", yol).CombinedOutput() }
	mailProcKok        = "/proc"
	mailSurecOldur     = func(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }

	mailHavuzMasterleri = paylasilanMasterler
	mailKomut           = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }
)

// MailYasakPoolSatiri: havuz dosyalarına yazılan TEK satır. Kafesli, paylaşılan-master ve alt alan
// şablonlarının hepsi (ve açılış taraması) bunu kullanır → değer tek yerde durur.
func MailYasakPoolSatiri() string {
	return "php_admin_value[sendmail_path] = " + mailYasakSendmailPath
}

// MailYasakKur: hızlı-başarısız sendmail betiğini garanti eder. Idempotent (içerik/mod aynıysa
// dokunmaz); bozulmuş ya da elle değiştirilmiş dosyayı onarır. Her panel açılışında, havuzlar bu yolu
// göstermeye başlamadan ÖNCE çağrılır (provisioner.Init).
func MailYasakKur() error {
	if err := os.MkdirAll(filepath.Dir(mailYasakBetikYolu), 0o755); err != nil {
		return err
	}
	degisti, err := acmeAtomikYaz(mailYasakBetikYolu, mailYasakBetik, 0o755)
	if err != nil {
		return fmt.Errorf("mail yasağı betiği yazılamadı: %w", err)
	}
	if degisti {
		mailYasakEtiketle(mailYasakBetikYolu)
		log.Printf("mail yasağı betiği kuruldu/onarıldı: %s", mailYasakBetikYolu)
	}
	return nil
}

// mailCgroupKafeste: /proc/<pid>/cgroup içeriği bir tenant'ın kafesli FPM biriminde
// ("php-fpm-c_<tenant>.service", ya da altındaki bir alt cgroup'ta) mı? Hem cgroup v2 ("0::/yol") hem
// v1/hybrid ("N:ad:/yol", çok satır) biçimini okur. Eşleşme TAM yol bileşenidir: paylaşılan master
// birimleri (php-fpm.service, php83-php-fpm.service), "xphp-fpm-c_a.service" ve
// "php-fpm-c_a.service.scope" kafes SAYILMAZ.
func mailCgroupKafeste(cgroup string) bool {
	for _, satir := range strings.Split(cgroup, "\n") {
		parca := strings.SplitN(strings.TrimSpace(satir), ":", 3)
		if len(parca) != 3 {
			continue
		}
		for _, bilesen := range strings.Split(parca[2], "/") {
			if strings.HasPrefix(bilesen, "php-fpm-c_") && strings.HasSuffix(bilesen, ".service") {
				return true
			}
		}
	}
	return false
}

// mailTakiliPostdropOldur: kafesli tenant FPM birimlerinin cgroup'unda duran `postdrop` süreçlerini
// sonlandırır; sonlandırılan sayıyı döner. Panel açılışında bir kez çağrılır (provisioner.Init).
//
// Kafeste postdrop tanım gereği DOĞRUDAN BAŞARISIZDIR (NoNewPrivileges setgid'i iptal eder) ve
// sonsuza dek yeniden denediği için asla kendiliğinden ölmez. Yalnız `postdrop` öldürülür: ana
// süreç sendmail kendiliğinden çıkar ("unable to execute /usr/sbin/postdrop"), bekleyen PHP işçisinin
// pclose()'u döner (VPS provasında doğrulandı). Böylece SMTP istemcisi olarak çalışan bir
// sendmail yerine-koyması (msmtp gibi — kafeste çalışır, postdrop'u YOKTUR) ve kafesin DIŞINDAKİ
// meşru postdrop'lar (cron, SSH, paylaşılan master havuzları) etkilenmez: eşleşme komut adı VE cgroup'tur.
func mailTakiliPostdropOldur() int {
	girdiler, err := os.ReadDir(mailProcKok)
	if err != nil {
		return 0
	}
	n := 0
	for _, g := range girdiler {
		pid, err := strconv.Atoi(g.Name())
		if err != nil || pid <= 1 {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(mailProcKok, g.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "postdrop" {
			continue
		}
		cg, err := os.ReadFile(filepath.Join(mailProcKok, g.Name(), "cgroup"))
		if err != nil || !mailCgroupKafeste(string(cg)) {
			continue
		}
		if mailSurecOldur(pid) == nil {
			n++
		}
	}
	return n
}

// ---- Paylaşılan master / alt alan havuzlarının MEVCUT dosyaları ----

// paylasilanMaster: bir php-fpm master'ının havuz dizini, servisi ve `-t` doğrulayıcısı.
type paylasilanMaster struct{ Dizin, Servis, FpmBin string }

// paylasilanMasterler: phpMap'teki her master (aynı dizin bir kez), dizin sırasıyla.
func paylasilanMasterler() []paylasilanMaster {
	var out []paylasilanMaster
	gorulen := map[string]bool{}
	for _, ay := range phpMap {
		if ay.PoolDir == "" || ay.Service == "" || gorulen[ay.PoolDir] {
			continue
		}
		gorulen[ay.PoolDir] = true
		out = append(out, paylasilanMaster{Dizin: ay.PoolDir, Servis: ay.Service, FpmBin: ay.FpmBin})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dizin < out[j].Dizin })
	return out
}

var (
	// Panelin şablonlarından birinin ürettiği havuz mu? phpPoolTmpl "Guvenlik sertlestirmesi", php.poolTmpl
	// ve php.subPoolTmpl "BEGIN_CUSTOM" taşır. Dağıtımın www.conf'u ve panelin kendi havuzları taşımaz.
	mailHavuzIsaretRe = regexp.MustCompile(`(?m)^; ---- (?:BEGIN_CUSTOM|Guvenlik sertlestirmesi)`)
	// Satırın eklendiği çapa: üç şablonda da disable_functions'ın hemen arkası (şablonlar bununla uyumlu
	// kalmalı — php.TestSablonlar_MailYasakYamasiylaUyumlu).
	mailHavuzCapaRe = regexp.MustCompile(`(?m)^php_admin_value\[disable_functions\][^\n]*\n`)
	// Havuzda ZATEN bir php_admin_value[sendmail_path] var mı? (php_value / ";" yorum satırı sayılmaz:
	// php_value INI_SYSTEM anahtarını zaten değiştiremez.)
	mailHavuzMevcutRe    = regexp.MustCompile(`(?m)^[ \t]*php_admin_value\[sendmail_path\][ \t]*=[ \t]*([^\n]*?)[ \t]*$`)
	mailHavuzKullaniciRe = regexp.MustCompile(`(?m)^user[ \t]*=[ \t]*(\S+)[ \t]*$`)
)

type mailHavuzDurum int

const (
	mailHavuzYamalandi mailHavuzDurum = iota // satır eklendi
	mailHavuzZatenVar                        // satır bizim değerle zaten var
	mailHavuzOzelDeger                       // başka bir php_admin_value[sendmail_path] var: operatör seçimi, dokunulmaz
	mailHavuzTanimsiz                        // panel şablonu değil ya da user satırı yok: dokunulmaz
	mailHavuzCapasiz                         // disable_functions satırı yok: yeri tahmin edilmez
)

// mailHavuzYama: havuz içeriğine yasak satırını ekler (saf işlev).
func mailHavuzYama(icerik string) (string, mailHavuzDurum) {
	if !mailHavuzIsaretRe.MatchString(icerik) || !mailHavuzKullaniciRe.MatchString(icerik) {
		return icerik, mailHavuzTanimsiz
	}
	if m := mailHavuzMevcutRe.FindStringSubmatch(icerik); m != nil {
		if m[1] == mailYasakSendmailPath {
			return icerik, mailHavuzZatenVar
		}
		return icerik, mailHavuzOzelDeger
	}
	loc := mailHavuzCapaRe.FindStringIndex(icerik)
	if loc == nil {
		return icerik, mailHavuzCapasiz
	}
	return icerik[:loc[1]] + MailYasakPoolSatiri() + "\n" + icerik[loc[1]:], mailHavuzYamalandi
}

// MailYasakHavuzuYamala: mevcut bir paylaşılan-master havuz dosyasının içeriğine yasak satırını ekler.
// Döner: yeni içerik, satır eklendi mi. (Şablon uyumluluk testleri ve açılış taraması aynı işlevi kullanır.)
func MailYasakHavuzuYamala(icerik string) (string, bool) {
	yeni, d := mailHavuzYama(icerik)
	return yeni, d == mailHavuzYamalandi
}

// mailHavuzDosya: yamalanmış bir dosya (geri alma için önceki içerik).
type mailHavuzDosya struct {
	yol        string
	once, yeni []byte
	mod        os.FileMode
}

// mailHavuzDosyasiniYamala: tek dosyayı hesabın kilidi altında okur-yamalar-yazar. Kilit, aynı dosyayı
// yazan öteki yolları (PHP ayarı kaydı, askıya alma, alt alan üreticisi: hepsi kilit.Hesap(sk) altında)
// sıralar; yoksa tarama onların yeni yazdığı içeriği eski içerikle ezebilirdi. Yazma temp+rename: php-fpm
// reload'u yarım dosya okumaz.
func mailHavuzDosyasiniYamala(yol string) (*mailHavuzDosya, mailHavuzDurum, error) {
	ilk, err := os.ReadFile(yol)
	if err != nil {
		return nil, mailHavuzTanimsiz, err
	}
	m := mailHavuzKullaniciRe.FindStringSubmatch(string(ilk))
	if m == nil {
		return nil, mailHavuzTanimsiz, nil
	}
	hk := kilit.Hesap(m[1])
	hk.Lock()
	defer hk.Unlock()
	b, err := os.ReadFile(yol) // kilit altında yeniden oku: ilk okuma ile kilit arasında biri yazmış olabilir
	if err != nil {
		return nil, mailHavuzTanimsiz, err
	}
	yeni, d := mailHavuzYama(string(b))
	if d != mailHavuzYamalandi {
		return nil, d, nil
	}
	mod := os.FileMode(0o644)
	if st, e := os.Stat(yol); e == nil {
		mod = st.Mode().Perm()
	}
	if _, err := acmeAtomikYaz(yol, []byte(yeni), mod); err != nil {
		return nil, d, err
	}
	return &mailHavuzDosya{yol: yol, once: b, yeni: []byte(yeni), mod: mod}, d, nil
}

// mailHavuzGeriAl: yamayı geri alır — YALNIZ dosya hâlâ bizim yazdığımız içerikteyse. Bu arada başka bir
// yazıcı (PHP ayarı kaydı vb.) dosyayı değiştirdiyse onun içeriği korunur.
func mailHavuzGeriAl(d *mailHavuzDosya) {
	m := mailHavuzKullaniciRe.FindStringSubmatch(string(d.yeni))
	if m == nil {
		return
	}
	hk := kilit.Hesap(m[1])
	hk.Lock()
	defer hk.Unlock()
	if cur, err := os.ReadFile(d.yol); err == nil && bytes.Equal(cur, d.yeni) {
		_, _ = acmeAtomikYaz(d.yol, d.once, d.mod)
	}
}

// mailHavuzOzet: bir taramanın sonucu (günlük ve testler için).
type mailHavuzOzet struct {
	Yamalanan  int      // canlı (*.conf) dosyada satır eklendi
	Yedek      int      // .bak / .askida kopyasında satır eklendi (yüklenmezler; geri dönünce kapalı gelsin)
	Zaten      int      // satır zaten vardı
	Ozel       int      // başka bir sendmail_path vardı: dokunulmadı
	Capasiz    int      // disable_functions satırı bulunamadı: dokunulmadı
	Hata       int      // okunamadı / yazılamadı
	Yenilenen  []string // reload edilen servisler
	GeriAlinan []string // php-fpm -t başarısız → yamaları geri alınan servisler
}

// mailPaylasilanHavuzlariKapat: her paylaşılan master'ın tenant havuzlarına (c_*.conf: hesap ve alt alan
// havuzları) yasak satırını ekler. Kural dizisi, php-fpm'in yalnız "*.conf" içerdiği gerçeğine dayanır:
//   - canlı *.conf: yazıldıktan sonra master başına BİR KEZ `php-fpm -t`; geçerse çalışan master reload
//     edilir (çalışmayan master BAŞLATILMAZ), geçmezse o master'ın yamaları geri alınır;
//   - *.conf.bak (kafese geçişte saklanan yedek) ve *.conf.askida (askıdaki hesap): yüklenmezler; yalnız
//     metin yamalanır ki yedek geri dönünce / hesap askıdan alınınca mail() yine kapalı gelsin.
func mailPaylasilanHavuzlariKapat() mailHavuzOzet {
	var oz mailHavuzOzet
	kaydet := func(durum mailHavuzDurum, err error, canli bool) {
		switch {
		case err != nil:
			oz.Hata++
		case durum == mailHavuzYamalandi && canli:
			oz.Yamalanan++
		case durum == mailHavuzYamalandi:
			oz.Yedek++
		case durum == mailHavuzZatenVar:
			oz.Zaten++
		case durum == mailHavuzOzelDeger:
			oz.Ozel++
		case durum == mailHavuzCapasiz:
			oz.Capasiz++
		}
	}
	for _, m := range mailHavuzMasterleri() {
		var degisen []*mailHavuzDosya
		canli, _ := filepath.Glob(filepath.Join(m.Dizin, "c_*.conf"))
		sort.Strings(canli)
		for _, yol := range canli {
			d, durum, err := mailHavuzDosyasiniYamala(yol)
			if err != nil {
				log.Printf("mail yasağı: %s yamalanamadı: %v", yol, err)
			}
			kaydet(durum, err, true)
			if d != nil {
				degisen = append(degisen, d)
			}
		}
		for _, ek := range []string{".conf.bak", ".conf" + AskiHavuzEki} {
			yedekler, _ := filepath.Glob(filepath.Join(m.Dizin, "c_*"+ek))
			sort.Strings(yedekler)
			for _, yol := range yedekler {
				_, durum, err := mailHavuzDosyasiniYamala(yol)
				if err != nil {
					log.Printf("mail yasağı: %s yamalanamadı: %v", yol, err)
				}
				kaydet(durum, err, false)
			}
		}
		if len(degisen) == 0 {
			continue
		}
		if m.FpmBin != "" {
			if out, err := mailKomut(m.FpmBin, "-t"); err != nil {
				for _, d := range degisen {
					mailHavuzGeriAl(d)
				}
				oz.Yamalanan -= len(degisen)
				oz.GeriAlinan = append(oz.GeriAlinan, m.Servis)
				log.Printf("mail yasağı: %s için php-fpm -t BAŞARISIZ, %d havuz yaması GERİ ALINDI (mail() bu master'da hâlâ açık): %s",
					m.Servis, len(degisen), strings.TrimSpace(string(out)))
				continue
			}
		}
		if _, err := mailKomut("systemctl", "is-active", "--quiet", m.Servis); err != nil {
			continue // çalışmayan master başlatılmaz; sonraki başlatma dosyaları zaten yeni haliyle okur
		}
		if out, err := mailKomut("systemctl", "reload-or-restart", m.Servis); err != nil {
			log.Printf("mail yasağı: %s reload başarısız (yama dosyada, ilk reload'da devreye girer): %s: %v",
				m.Servis, strings.TrimSpace(string(out)), err)
			continue
		}
		oz.Yenilenen = append(oz.Yenilenen, m.Servis)
	}
	if oz.Yamalanan+oz.Yedek+oz.Hata+len(oz.GeriAlinan) > 0 {
		log.Printf("mail yasağı (paylaşılan havuzlar): %d canlı + %d yedek/askı havuza satır eklendi, %d zaten vardı, %d özel değer, %d çapasız, %d hata; reload: %v, geri alınan: %v",
			oz.Yamalanan, oz.Yedek, oz.Zaten, oz.Ozel, oz.Capasiz, oz.Hata, oz.Yenilenen, oz.GeriAlinan)
	}
	return oz
}

// MailYasakHavuzlariniKapatOnStartup: mevcut paylaşılan-master / alt alan havuzlarında mail() yasağını
// uygular. Arka planda koşar (provisioner.Init'i BLOKLAMAZ: php-fpm -t + reload saniyeler sürebilir) ve
// çökmeye karşı korumalıdır. Idempotent: her açılışta güvenle tekrarlanır.
func MailYasakHavuzlariniKapatOnStartup() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("mail yasağı havuz taraması çöktü: %v", r)
			}
		}()
		mailPaylasilanHavuzlariKapat()
	}()
}
