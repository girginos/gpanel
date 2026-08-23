package panelhost

// Let's Encrypt SSL kurma — acme.sh webroot modu.
//
// Akış:
//  1. Ön koşul: hostname `_panel.conf` server_name'inde olmalı (aksi halde
//     kullanıcı önce Ayarla ile eklemeli). Aksi halde LE cert alsak bile
//     nginx bu hostname'i kabul etmez, ziyaretçi cert-mismatch alır.
//  2. :80 mini vhost'u üret (acme_vhost.go). `.well-known/acme-challenge`
//     yolunu webroot'a bağlar, kalan trafiği HTTPS'e 301.
//  3. `acme.sh --issue --webroot /var/www/gpanel-acme-challenge -d hostname
//     --server letsencrypt --config-home /opt/girginospanel/acme`
//  4. `acme.sh --install-cert -d hostname --cert-file ... --key-file ...
//     --reloadcmd "systemctl reload nginx"`
//  5. Sertifika bilgisi doğrulama — panel.crt'nin Subject.CN yeni hostname mi?
//
// 🔴 :80 vhost'u panel HAYATTAKİ diğer :80 servislerini ETKİLEMEZ. server_name
// hostname'e özgü; başka isim (veya IP) ile gelen istekler default_server'a
// düşer (mail.example.com vs.). Sadece "panel.musteri.com:80" isteği bu
// vhost'a gelir.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"
)

func SslKurAsync(hostname string) *Is {
	if !TipKilitle("sslkur") {
		return nil
	}
	// 🔴 LE rate-limit cooldown — son 3 fail'i takip et. 3 fail sonrası
	// 4. deneme reddedilir (LE bir haftalık ban vermeden koru).
	if !RateLimitIzinli(hostname) {
		return nil
	}
	is := IsBaslat("sslkur", hostname)
	go func() {
		defer TipSerbest("sslkur")
		sslKurKos(is, hostname)
	}()
	return is
}

func sslKurKos(is *Is, hostname string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panelhost.sslKurKos PANIC: %v\n%s", r, debug.Stack())
			is.Bitir(fmt.Sprintf("panik: %v", r))
		}
	}()

	if !AcmeVarMi() {
		is.AdimEkle("acme.sh yüklü değil (/root/.acme.sh/acme.sh)", false)
		is.Bitir("acme.sh bulunamadı")
		return
	}
	is.AdimEkle("acme.sh yüklü", true)

	// 1) hostname server_name içinde mi?
	d := DurumOku()
	varMi := false
	for _, ad := range d.Izinliler {
		if strings.EqualFold(ad, hostname) {
			varMi = true
			break
		}
	}
	if !varMi {
		is.AdimEkle("hostname panel vhost'unda tanımlı değil — önce Hostname Ayarla", false)
		is.Bitir("önce hostname ayarlanmalı")
		return
	}
	is.AdimEkle("hostname panel vhost'unda tanımlı", true)

	// 2) DNS kontrol — LE cert almaya gitmeden önce
	cozulen, eslesme := DNSCoz(hostname, d.SunucuIP4, d.SunucuIP6)
	if !eslesme {
		is.AdimEkle(fmt.Sprintf("DNS uyuşmuyor — çözülen: %v, sunucu: %v", cozulen, d.SunucuIP4), false)
		is.Bitir("DNS bu sunucuya yönlendirilmemiş")
		return
	}
	is.AdimEkle(fmt.Sprintf("DNS eşleşiyor: %v", cozulen), true)

	// 3) Webroot dizini hazırla
	if err := os.MkdirAll(WebrootDir, 0755); err != nil {
		is.AdimEkle("webroot oluşturulamadı: "+err.Error(), false)
		is.Bitir("webroot hata")
		return
	}
	is.AdimEkle("webroot dizini: "+WebrootDir, true)

	// 4) :80 acme vhost'u üret (idempotent, hostname değişikliğinde yenile)
	if err := AcmeVhostYaz(hostname); err != nil {
		is.AdimEkle("acme vhost yazılamadı: "+err.Error(), false)
		is.Bitir("acme vhost hata")
		return
	}
	is.AdimEkle("acme :80 vhost yazıldı: "+AcmeVhostPath, true)

	// nginx -t + reload
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		is.AdimEkle("nginx -t başarısız: "+strings.TrimSpace(string(out)), false)
		_ = AcmeVhostSil() // rollback
		is.Bitir("nginx yapılandırma hatası, geri alındı")
		return
	}
	is.AdimEkle("nginx -t geçti", true)
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		is.AdimEkle("nginx reload: "+strings.TrimSpace(string(out)), false)
		// 🔴 CRITICAL fix: reload fail'de acme vhost SIL — dosya diskte kalırsa
		// başka bir servisin sonraki reload denemesi de bu bozuk state ile fail
		// eder. -t bir sonraki değişikliğin yükünü ALMASIN.
		_ = AcmeVhostSil()
		is.Bitir("nginx reload başarısız, acme vhost geri alındı")
		return
	}
	is.AdimEkle("nginx reload OK", true)

	// 5) acme.sh --issue
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Minute)
	defer iptal()

	_ = os.MkdirAll(AcmeConfigHome, 0700)

	issueCmd := exec.CommandContext(ctx, AcmeBin,
		"--issue",
		"-d", hostname,
		"--webroot", WebrootDir,
		"--server", "letsencrypt",
		"--config-home", AcmeConfigHome,
	)
	is.AdimEkle("acme.sh --issue çağrılıyor…", true)
	out, err := issueCmd.CombinedOutput()
	// Çıktının son 20 satırını iş kaydına al (tam çıktı yüzlerce satır)
	for _, ln := range sonSatirlar(string(out), 20) {
		if ln == "" {
			continue
		}
		is.AdimEkle(ln, err == nil)
	}
	// 🔴 acme.sh "Domains not changed / Skipping" (exit 2) = SERTIFIKA ZATEN GECERLI.
	// Bu bir HATA DEGILDIR; eskiden hata sayilip install-cert ATLANIYORDU → panel
	// yeni cert'i hic kurmuyor, is "hata" gorunuyor ama sebebi "her sey zaten yolunda".
	// Bu durumda akisa DEVAM et: mevcut cert'i panel'e kur.
	zatenGecerli := false
	if err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "domains not changed") || strings.Contains(low, "skipping") ||
			strings.Contains(low, "next renewal time is") || strings.Contains(low, "cert success") {
			zatenGecerli = true
			err = nil
		}
	}
	if err != nil {
		RateLimitFail(hostname)
		is.Bitir("acme.sh --issue başarısız: " + err.Error())
		return
	}
	if zatenGecerli {
		is.AdimEkle("sertifika zaten geçerli (yeniden alınmadı) — panele kuruluyor", true)
	} else {
		is.AdimEkle("cert alındı", true)
	}

	// 5b) 🔴 CRITICAL fix: install-cert öncesi mevcut cert/key'i YEDEKLE — reload
	// sonrasında panel HTTPS'te doğrulanamıyor veya cert bozuksa geri koy.
	certYedek := CertPath + ".yedek-oncesi-le"
	keyYedek := KeyPath + ".yedek-oncesi-le"
	if raw, e := os.ReadFile(CertPath); e == nil {
		_ = os.WriteFile(certYedek, raw, 0644)
	}
	if raw, e := os.ReadFile(KeyPath); e == nil {
		_ = os.WriteFile(keyYedek, raw, 0600)
	}
	is.AdimEkle("mevcut cert/key yedeklendi (.yedek-oncesi-le)", true)

	// 6) install-cert — panel.crt/key'i değiştir + reload
	// 🔴 fix: --cert-file kaldırıldı; --fullchain-file zaten aynı yolu yazıyordu,
	// ikisi de aynı yolu alınca sadece son yazan geçerli. Tek dosya = tek yazma.
	installCmd := exec.CommandContext(ctx, AcmeBin,
		"--install-cert",
		"-d", hostname,
		"--key-file", KeyPath,
		"--fullchain-file", CertPath,
		"--reloadcmd", "systemctl reload nginx",
		"--config-home", AcmeConfigHome,
	)
	out2, err := installCmd.CombinedOutput()
	for _, ln := range sonSatirlar(string(out2), 10) {
		if ln == "" {
			continue
		}
		is.AdimEkle(ln, err == nil)
	}
	if err != nil {
		// install fail — cert/key büyük ihtimalle değişmedi, ama emin olmak için
		// yedekten geri yükle (dosya boyutu farklıysa).
		_ = certGeriYukle(certYedek, CertPath, keyYedek, KeyPath)
		is.AdimEkle("install-cert fail → cert/key yedekten geri alındı", true)
		is.Bitir("install-cert başarısız: " + err.Error())
		return
	}

	// 6b) 🔴 CRITICAL: yeni cert gerçekten geçerli mi? Bozuksa reload olsa da
	// yeni bağlantılar TLS handshake fail alır → panele giremezsin.
	if raw, e := os.ReadFile(CertPath); e != nil {
		_ = certGeriYukle(certYedek, CertPath, keyYedek, KeyPath)
		_, _ = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
		is.Bitir("yeni cert okunamıyor, yedekten geri alındı: " + e.Error())
		return
	} else if blok, _ := pem.Decode(raw); blok == nil {
		_ = certGeriYukle(certYedek, CertPath, keyYedek, KeyPath)
		_, _ = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
		is.Bitir("yeni cert PEM parse edilemedi, yedekten geri alındı")
		return
	}
	is.AdimEkle("yeni cert PEM olarak geçerli", true)

	// 6c) 🔴 CRITICAL fix: cron kaydı — panel-özel config-home standart cron'a
	// kayıtlı değil. `acme.sh --cron` default `/root/.acme.sh`'a bakar, panel
	// domain'ini GÖRMEZ → 90 gün sonra cert expire → panel cert-mismatch.
	// `--install-cronjob` ile cron kaydını KENDİ config-home'umuza bağla.
	cronCmd := exec.CommandContext(ctx, AcmeBin, "--install-cronjob", "--config-home", AcmeConfigHome)
	if out, e := cronCmd.CombinedOutput(); e != nil {
		// Cron zaten kurulu olabilir — hata fatal değil, sadece uyar
		is.AdimEkle("cron kayıt uyarısı (yenileme yine de default cron'dan gelmeyebilir): "+
			strings.TrimSpace(string(out)), false)
	} else {
		is.AdimEkle("acme.sh cron kaydı yenilendi (90 gün öncesi auto-renew)", true)
	}

	// 7) Doğrulama: panel.crt gerçekten yeni hostname'e mi ait?
	if raw, e := os.ReadFile(CertPath); e == nil {
		if blok, _ := pem.Decode(raw); blok != nil {
			if crt, e2 := x509.ParseCertificate(blok.Bytes); e2 == nil {
				dns := crt.DNSNames
				bulundu := false
				for _, n := range dns {
					if strings.EqualFold(n, hostname) {
						bulundu = true
						break
					}
				}
				if !bulundu {
					is.AdimEkle(fmt.Sprintf("kurulan cert'in SAN'ı hostname'i içermiyor: %v", dns), false)
					is.Bitir("cert doğrulama başarısız")
					return
				}
				is.AdimEkle("kurulan cert SAN'ında hostname var: "+strings.Join(dns, ", "), true)
				is.AdimEkle("cert bitiş: "+crt.NotAfter.UTC().Format(time.RFC3339), true)
			}
		}
	}
	// Başarı — rate-limit sayacını sıfırla
	RateLimitBasari(hostname)
	is.Bitir("")
}

// certGeriYukle — cert/key yedeğini üzerine yaz. Best-effort.
func certGeriYukle(certYedek, cert, keyYedek, key string) error {
	if raw, e := os.ReadFile(certYedek); e == nil {
		_ = os.WriteFile(cert, raw, 0644)
	}
	if raw, e := os.ReadFile(keyYedek); e == nil {
		_ = os.WriteFile(key, raw, 0600)
	}
	return nil
}

func sonSatirlar(s string, n int) []string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(all) <= n {
		return all
	}
	return all[len(all)-n:]
}
