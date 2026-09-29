package provisioner

// mail_varsayilan.go — Mail sunucusunun VARSAYILAN (SNI-siz) TLS sertifikası.
//
// 🔴 SORUN (üretimde ölçüldü): mail eklentisi per-domain sertifikayı SNI ile
// sunuyor (mail.<d> → Let's Encrypt). Ama istemci TLS el sıkışmasında SNI
// GÖNDERMEZSE (Outlook sık sık göndermez, bazı Android istemcileri hiç) veya
// sunucuya farklı bir adla bağlanırsa, dovecot/postfix VARSAYILAN sertifikayı
// sunar — ve o, kurulumda üretilen KENDİNDEN İMZALI sertifikadır. İstemci
// güvenmez, bağlantıyı kabul etmez ve KULLANICIYA SÜREKLİ ŞİFRE SORAR.
//
// ÇÖZÜM: varsayılan /etc/pki/mail/mail.crt+key, panel host (sunucu adı) Let's
// Encrypt sertifikasıyla doldurulur ve onunla EŞİT tutulur.
//
// 🔴 GÜVENLİK — varsayılanın TEK meşru kaynağı panel host sertifikasıdır.
// Varsayılan sertifika SUNUCU GENELİ bir kimliktir: dovecot/postfix'in SNI'sız
// bütün bağlantıları ve paylaşımlı webmail 443 vhost'u (kendi LE'si olmayan
// TÜM kiracıların webmail/autodiscover adları) onu sunar. Eskiden domain SSL
// akışı (müşteri/bayi kapsamlı uç) "varsayılan self-signed/süresi dolmuşsa"
// kiracının mail.<alan> sertifikasını buraya yazıyordu: kendi alanına LE posta
// SSL'i isteyen İLK kiracı sunucunun kimliğini ele geçiriyor, sertifika artık
// "geçerli" sayıldığından da kalıcılaşıyordu (panel host LE'si sonradan
// alınsa bile geçmiyordu). Kurulumda konan panel kopyası ise yenilenmiyordu
// (panel acme reloadcmd yalnız nginx'i yeniler) → ≤90 günde süresi dolup
// kapıyı her sunucuda kendiliğinden açıyordu. Artık:
//   - MailVarsayilanKur (kurulum): kaynak panel host sertifikası DEĞİLSE reddeder.
//   - MailVarsayilanPaneldenEsitle (panel host SSL akışı — AdminOnly — ve
//     MailVarsayilanNobetci): varsayılan panel sertifikasından farklıysa (süresi
//     dolan eski kopya, oraya yazılmış bir kiracı sertifikası) panel
//     sertifikasıyla değiştirir.
// Kiracı kapsamlı hiçbir akış bu dosyadaki işlevleri çağırmaz (domains paketi
// yapısal testi: TestPostaSSL_KiraciYolu_SunucuVarsayilaninaBasvurmaz).

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	// Varsayılan (SNI'sız) posta sertifikası — dovecot 99-gosp-mail.conf
	// ssl_cert, postfix smtpd_tls_cert_file, paylaşımlı webmail vhost'u.
	mailVarsayilanCrt = "/etc/pki/mail/mail.crt"
	mailVarsayilanKey = "/etc/pki/mail/mail.key"

	// Varsayılanın TEK meşru kaynağı: panel host sertifikası. panelhost.CertPath
	// / KeyPath ile AYNI yol (panelhost bu paketi içe aktarır; döngü olmasın
	// diye burada tekrarlanır). Root sahipli; yalnız panel host SSL akışı
	// (AdminOnly) ve acme yenilemesi yazar.
	mailVarsayilanKaynakCrt = "/etc/ssl/girginospanel/panel.crt"
	mailVarsayilanKaynakKey = "/etc/ssl/girginospanel/panel.key"

	// mailVarsayilanYenile — dovecot + postfix reload. Testler sahteyle
	// değiştirir (ana makinedeki servislere dokunulmaz).
	mailVarsayilanYenile = mailVarsayilanServisYenile

	// mailVarsayilanMu — kurulum, panel host SSL akışı ve nöbetçi aynı
	// dosyaları yazabilir; kontrol+yazma tek kritik bölgede.
	mailVarsayilanMu sync.Mutex
)

// ErrMailVarsayilanKaynak — varsayılan posta sertifikası için panel host
// sertifikası DIŞINDA bir kaynak istendi (ör. bir kiracının mail.<alan>
// sertifikası). Sunucu geneli kimlik kiracı sertifikasından beslenemez.
var ErrMailVarsayilanKaynak = errors.New("varsayılan posta sertifikasının kaynağı yalnız panel host sertifikası olabilir")

// mailVarsayilanKaynagiMi — verilen yollar panel host sertifikası mı?
func mailVarsayilanKaynagiMi(certPath, keyPath string) bool {
	return filepath.Clean(certPath) == mailVarsayilanKaynakCrt &&
		filepath.Clean(keyPath) == mailVarsayilanKaynakKey
}

// MailVarsayilanKur — kurulum akışı: varsayılan mail sertifikası HÂLÂ
// self-signed (ya da yok/bozuk/süresi dolmuş) ise panel host LE cert+key ile
// değiştirir ve dovecot+postfix'i reload eder. Döndürdüğü bool: değişiklik
// yapıldı mı. Zaten geçerli (CA imzalı) bir varsayılan varsa dokunmaz.
//
// 🔴 Kaynak YALNIZ panel host sertifikası olabilir (bkz. dosya başı); başka
// bir yol (kiracı sertifikası) ErrMailVarsayilanKaynak ile reddedilir.
func MailVarsayilanKur(certPath, keyPath string) (bool, error) {
	if !mailVarsayilanKaynagiMi(certPath, keyPath) {
		return false, ErrMailVarsayilanKaynak
	}
	mailVarsayilanMu.Lock()
	defer mailVarsayilanMu.Unlock()
	if !mailVarsayilanSelfSigned() {
		return false, nil // varsayılan zaten geçerli — dokunma
	}
	return mailVarsayilanYaz(false)
}

// MailVarsayilanPaneldenEsitle — varsayılanı panel host sertifikasıyla EŞİTLER.
// Panel sertifikası geçerli (CA imzalı, süresi dolmamış, anahtarıyla eşleşen)
// ise ve varsayılan ondan FARKLIYSA — süresi dolan eski kopya ya da oraya
// yazılmış bir kiracı sertifikası — üzerine yazar. Posta yığını kurulu
// değilse (dizin yok) ya da panel host LE'si yoksa hiçbir şey yapmaz.
//
// Yalnız yönetici/sistem akışlarından çağrılır: panel host SSL akışı
// (AdminOnly) ve MailVarsayilanNobetci.
func MailVarsayilanPaneldenEsitle() (bool, error) {
	mailVarsayilanMu.Lock()
	defer mailVarsayilanMu.Unlock()
	if fi, err := os.Stat(filepath.Dir(mailVarsayilanCrt)); err != nil || !fi.IsDir() {
		return false, nil // posta yığını kurulu değil — dizin YARATILMAZ
	}
	return mailVarsayilanYaz(true)
}

// mailVarsayilanYaz — panel host cert+key'i varsayılana yazar (kilit altında
// çağrılır). esitle=true: kaynak yoksa sessizce çıkar; varsayılan kaynakla
// bayt bayt aynıysa yazmaz/yenilemez.
func mailVarsayilanYaz(esitle bool) (bool, error) {
	cert, err := os.ReadFile(mailVarsayilanKaynakCrt)
	if err != nil {
		if esitle && errors.Is(err, fs.ErrNotExist) {
			return false, nil // panel host SSL'i yok — eşitlenecek kaynak yok
		}
		return false, err
	}
	key, err := os.ReadFile(mailVarsayilanKaynakKey)
	if err != nil {
		return false, err
	}
	// Kaynak da GEÇERLİ (CA imzalı + süresi dolmamış) olmalı: self-signed bir
	// kaynağı varsayılan yapmak Outlook'u yine reddettirir. Kurulum akışında
	// panel host LE henüz alınmamışsa bu dal self-signed'ı korur.
	if !certGecerliLE(cert) {
		return false, nil
	}
	// Sertifika ile anahtar EŞLEŞMELİ: acme yenilemesinin ortasında (cert
	// yazılmış, key henüz değil) ya da bozuk bir çift dovecot/postfix'i
	// düşürürdü — hiç yazmamak, yarım yazmaktan iyidir.
	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return false, fmt.Errorf("panel host sertifikası ile anahtarı eşleşmiyor: %w", err)
	}
	if esitle {
		eskiC, e1 := os.ReadFile(mailVarsayilanCrt)
		eskiK, e2 := os.ReadFile(mailVarsayilanKey)
		if e1 == nil && e2 == nil && bytes.Equal(eskiC, cert) && bytes.Equal(eskiK, key) {
			return false, nil // zaten eşit
		}
	}
	// Yedekle (ilk sefer), sonra değiştir.
	if _, e := os.Stat(mailVarsayilanCrt + ".selfsigned.yedek"); e != nil {
		if b, e2 := os.ReadFile(mailVarsayilanCrt); e2 == nil {
			_ = mailVarsayilanAtomikYaz(mailVarsayilanCrt+".selfsigned.yedek", b, 0o644)
		}
		if b, e2 := os.ReadFile(mailVarsayilanKey); e2 == nil {
			_ = mailVarsayilanAtomikYaz(mailVarsayilanKey+".selfsigned.yedek", b, 0o600)
		}
	}
	// Önce anahtar, sonra sertifika; her biri atomik (reload araya girse bile
	// yarım dosya görülmez).
	if err := mailVarsayilanAtomikYaz(mailVarsayilanKey, key, 0o600); err != nil {
		return false, err
	}
	if err := mailVarsayilanAtomikYaz(mailVarsayilanCrt, cert, 0o644); err != nil {
		return false, err
	}
	mailVarsayilanYenile()
	return true, nil
}

// mailVarsayilanAtomikYaz — aynı dizinde O_EXCL geçici dosya + rename.
// Hedefteki bir symlink İZLENMEZ, yerine konur; yarım yazılmış dosya asla
// hedef adda görünmez.
func mailVarsayilanAtomikYaz(yol string, b []byte, izin os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(yol), "."+filepath.Base(yol)+".gecici-*")
	if err != nil {
		return err
	}
	gecici := f.Name()
	hata := func(e error) error {
		_ = f.Close()
		_ = os.Remove(gecici)
		return e
	}
	if err := f.Chmod(izin); err != nil {
		return hata(err)
	}
	if _, err := f.Write(b); err != nil {
		return hata(err)
	}
	if err := f.Sync(); err != nil {
		return hata(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(gecici)
		return err
	}
	if err := os.Rename(gecici, yol); err != nil {
		_ = os.Remove(gecici)
		return err
	}
	return nil
}

// mailVarsayilanServisYenile — reload (restart değil — kuyruk/oturum
// kesintisiz). Eklenti servisleri yönetir ama varsayılan cert dosyaları
// core'un sorumluluğunda; reload'u da core tetikler.
func mailVarsayilanServisYenile() {
	_, _ = exec.Command("systemctl", "reload", "dovecot").CombinedOutput()
	if _, e := exec.Command("systemctl", "reload", "postfix").CombinedOutput(); e != nil {
		_, _ = exec.Command("systemctl", "restart", "postfix").CombinedOutput()
	}
}

// MailVarsayilanNobetci — varsayılanı panel host sertifikasıyla eşit tutar:
// açılışta bir kez, sonra periyodik. Panel sertifikası acme cron'uyla
// yenilendiğinde (reloadcmd yalnız nginx'i yeniler) posta kopyası da yenilenir;
// eski sürümün bir kiracı sertifikasını varsayılana yazdığı sunucularda panel
// LE'si varsa varsayılan geri alınır. ctx bitince döner.
func MailVarsayilanNobetci(ctx context.Context) {
	calis := func() {
		if degisti, err := MailVarsayilanPaneldenEsitle(); err != nil {
			log.Printf("posta-varsayilan: panel host sertifikasıyla eşitlenemedi: %v", err)
		} else if degisti {
			log.Printf("posta-varsayilan: varsayılan (SNI'sız) posta sertifikası panel host sertifikasıyla eşitlendi")
		}
	}
	calis()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			calis()
		}
	}
}

// mailVarsayilanSelfSigned — /etc/pki/mail/mail.crt kendinden imzalı mı
// (issuer == subject) ya da hiç yok mu? Yoksa "değiştirilmeli" sayılır.
func mailVarsayilanSelfSigned() bool {
	b, err := os.ReadFile(mailVarsayilanCrt)
	if err != nil {
		return true // dosya yok → geçerli LE ile doldurulmalı
	}
	blok, _ := pem.Decode(b)
	if blok == nil {
		return true
	}
	c, err := x509.ParseCertificate(blok.Bytes)
	if err != nil {
		return true
	}
	// Süresi geçmiş de "geçersiz" sayılır (yenilenmeli).
	if time.Now().After(c.NotAfter) {
		return true
	}
	return c.Issuer.CommonName == c.Subject.CommonName
}

// certGecerliLE — PEM cert CA-imzalı (issuer!=subject) VE süresi dolmamış mı?
func certGecerliLE(pemBytes []byte) bool {
	blok, _ := pem.Decode(pemBytes)
	if blok == nil {
		return false
	}
	c, err := x509.ParseCertificate(blok.Bytes)
	if err != nil {
		return false
	}
	if time.Now().After(c.NotAfter) {
		return false
	}
	return c.Issuer.CommonName != c.Subject.CommonName
}
