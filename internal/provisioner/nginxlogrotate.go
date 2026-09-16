package provisioner

// nginx log döndürme — kalıcı düzeltme + donmuş durumdan kurtarma.
//
// ─── ÖLÇÜLEN ARIZA ──────────────────────────────────────────────────────────
// Üretimde (.180) nginx işçileri, döndürülmüş log dosyalarına yazmaya devam
// ediyordu: 4 işçide toplam 332 log tanıtıcısının **220'si döndürülmüş
// dosyalarda**, 54 ayrı dosya, **436,6 MB**. Güncel `.log` dosyalarının
// 80/83'ü 0 bayttı. Tek bir dosya (islam-tr.org.access.log-20260827) 320 MB'a
// ulaşmıştı ve hâlâ büyüyordu. Bu dosyalar bir daha döndürülmedi,
// sıkıştırılmadı, `rotate 10` hiç uygulanmadı.
//
// ─── KÖK NEDEN (syscall seviyesinde ölçüldü) ────────────────────────────────
// `/var/log/nginx` dizini `0700 root:root`'tu. nginx işçileri `nginx`
// kullanıcısı olarak koşar ve döndürmeden sonra log dosyasını yeniden açması
// gereken taraf ONLARDIR. strace ile işçilere bağlanıldığında:
//     isci  : openat("/var/log/nginx/x.access.log", O_WRONLY|O_CREAT|O_APPEND) = -1 EACCES
//     master: openat("/var/log/nginx/x.access.log", O_WRONLY|O_CREAT|O_APPEND) = 6
// Master (root) açar, işçiler açamaz → işçiler devraldıkları ESKİ tanıtıcıya
// yazmaya devam eder. Gözlenen imza: yenide 1 tanıtıcı (master), eskide N.
// Doğrulama: 0700'de donma 4/4, 0710 root:nginx'te kurtulma 8/8.
//
// 🔴 İKİ İDDİA ÇÜRÜTÜLDÜ — bu yorumun önceki hâli ikisini de yanlış yazıyordu:
//
//  (a) "Kalıcı kilit" DEĞİL. 0700'de bile bir SIGHUP *reload* logging'i tam
//      kurtarır: root master dosyayı açar, forkladığı yeni işçiler geçerli
//      fd'yi miras alır — izin hiç devreye girmez. Yani arıza, bir sonraki
//      rotasyona kadar her reload'da kendiliğinden "düzelmiş" görünür ve
//      tekrar döner. Bu, teşhisi zorlaştıran asıl özellik.
//
//  (b) `notifempty` "USR1 bir daha hiç gönderilmez" DEĞİL. Üretim
//      logrotate.status'ü, donmadan BİR GÜN SONRA (28 Ağustos) 3 dosyanın
//      döndüğünü gösteriyor; `sharedscripts` nedeniyle postrotate ÇALIŞTI ve
//      USR1 yeniden gönderildi — yine işe yaramadı, çünkü kilidi tutan şey
//      izindi. notifempty bir AMPLİFİKATÖRDÜR: tüm dosyalar 0 bayta
//      donduktan sonra hiç döndürme kalmaz, `rotate N` saklama sınırı
//      by-pass edilir ve dosyalar sınırsız büyür. Yine de kaldırılmalı.
//
// ─── NEDEN KİMSE FARK ETMEDİ ────────────────────────────────────────────────
// Hazır ayardaki postrotate satırı şuydu:
//     /bin/kill -USR1 `cat /run/nginx.pid 2>/dev/null` 2>/dev/null || true
// Başarısız yeniden açma hiçbir yere hata bırakmıyor. Arıza, güven olarak
// render ediliyordu. [[feedback_failure_renders_as_reassurance]]
//
// ─── ModSecurity AYRI BİR SORUN ─────────────────────────────────────────────
// `modsec_audit.log` tanıtıcısını libmodsecurity yönetir; nginx'in USR1'i onu
// yeniden AÇMAZ. Ölçüldü: elle `nginx -s reopen` sonrası dosya üzerindeki 4
// tanıtıcı aynen kaldı. Bu dosya için tek doğru yöntem `copytruncate`
// (yeniden adlandırma yok — içerik kopyalanır, dosya sıfırlanır; tanıtıcı
// geçerli kalır). Bu yüzden ayrı bir bölüme alınır ve genel kalıptan çıkarılır.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const nginxLogrotateYolu = "/etc/logrotate.d/nginx"

// nginxLogrotateIcerik — panel-sahipli döndürme kuralı.
//
// Kalıp bilinçli olarak `*.log` DEĞİL: `modsec_audit.log` bu kalıba girmemeli
// (copytruncate ile ayrı yönetiliyor). Ölçüldü — /var/log/nginx altında
// `*.access.log` / `*.error.log` dışında yalnız access.log, error.log ve
// modsec_audit.log var.
const nginxLogrotateIcerik = `# GirginOSPanel tarafından yönetilir — elle düzenleme KORUNMAZ.
#
# Dağıtımın hazır ayarına göre üç fark var, üçü de ölçülmüş bir arızaya dayanıyor:
#
#  1) notifempty KALDIRILDI. İşçiler yeniden açmayı kaçırınca güncel .log 0
#     bayt kalır; notifempty boş dosyayı atladığı için o dosya bir daha HİÇ
#     döndürülmez — yani rotate N saklama sınırı by-pass edilir ve bayat
#     dosya sınırsız büyür (üretimde tek dosyada 320 MB ölçüldü). Kaldırınca
#     döndürme her gün çalışır: hem saklama sınırı işler hem de yeniden-açma
#     sinyali düzenli olarak tekrar gönderilir.
#     NOT: bu tek başına kök nedeni ÇÖZMEZ — kök neden log dizini iznidir
#     (bkz. nginxlog_heal.go). notifempty amplifikatördür.
#
#  2) postrotate HATAYI YUTMUYOR. Eski satır "2>/dev/null || true" ile
#     biterdi; başarısız yeniden açma hiçbir yere iz bırakmazdı.
#
#  3) modsec_audit.log AYRI BÖLÜMDE (copytruncate) — bkz. aşağıdaki gerekçe.

/var/log/nginx/*.access.log
/var/log/nginx/*.error.log
/var/log/nginx/access.log
/var/log/nginx/error.log
{
    create 0640 nginx root
    daily
    rotate 14
    missingok
    compress
    delaycompress
    sharedscripts
    postrotate
        # nginx çalışmıyorsa sessiz geç (gürültü yok); ÇALIŞIYORSA yeniden
        # açma başarısızlığı logrotate çıktısına düşsün — yutulmasın.
        if [ -s /run/nginx.pid ] && kill -0 "$(cat /run/nginx.pid)" 2>/dev/null; then
            /usr/sbin/nginx -s reopen
        fi
    endscript
}

# ModSecurity denetim günlüğü — copytruncate ZORUNLU.
#
# Bu dosyanın tanıtıcısını libmodsecurity tutar ve nginx'in USR1 sinyaline
# yanıt vermez. Ölçüldü: yeniden adlandırma sonrası "nginx -s reopen"
# çalıştırıldı, dosya üzerindeki 4 tanıtıcı AYNEN kaldı ve ModSecurity eski
# inode'a yazmaya devam etti. copytruncate dosyayı yeniden adlandırmaz;
# içeriği kopyalayıp dosyayı sıfırlar, böylece açık tanıtıcı geçerli kalır.
/var/log/nginx/modsec_audit.log {
    daily
    rotate 14
    missingok
    notifempty
    compress
    delaycompress
    copytruncate
}
`

// NginxLogrotateEnsure — döndürme kuralını panel sahipliğine alır (idempotent).
// İçerik zaten aynıysa dosyaya DOKUNMAZ.
func NginxLogrotateEnsure() {
	if b, err := os.ReadFile(nginxLogrotateYolu); err == nil && string(b) == nginxLogrotateIcerik {
		return
	}
	// Dağıtımın dosyasını ilk kez devralırken bir kopyasını sakla.
	if b, err := os.ReadFile(nginxLogrotateYolu); err == nil &&
		!strings.Contains(string(b), "GirginOSPanel tarafından yönetilir") {
		_ = os.WriteFile(nginxLogrotateYolu+".dagitim.yedek", b, 0o644)
	}
	if err := os.WriteFile(nginxLogrotateYolu, []byte(nginxLogrotateIcerik), 0o644); err != nil {
		log.Printf("🔴 nginx logrotate kurali yazilamadi: %v", err)
		return
	}
	// Yazdıktan sonra DOĞRULA: bozuk bir logrotate kuralı sessizce hiçbir şey
	// döndürmez — tam da düzeltmeye çalıştığımız arızanın aynısı.
	if out, err := exec.Command("logrotate", "-d", nginxLogrotateYolu).CombinedOutput(); err != nil {
		log.Printf("🔴 nginx logrotate kurali GECERSIZ, dagitim yedegine donuluyor: %s",
			strings.TrimSpace(string(out)))
		if b, e := os.ReadFile(nginxLogrotateYolu + ".dagitim.yedek"); e == nil {
			_ = os.WriteFile(nginxLogrotateYolu, b, 0o644)
		}
		return
	}
	log.Printf("nginx logrotate kurali panel sahipligine alindi (notifempty kaldirildi, modsec copytruncate)")
}

// donmusTanitici — nginx süreçlerinin döndürülmüş log dosyalarında tuttuğu
// bayat tanıtıcıları döndürür: dosya adı → tanıtıcı sayısı.
//
// 🔴 Bu, ÜRETİMİ ölçen kontroldür. "Yapılandırma doğru" demek yetmez; arıza
// zaten yapılandırmanın doğru göründüğü hâlde oluşmuştu. Ölçüt, nginx'in
// GERÇEKTEN hangi inode'a yazdığıdır. [[feedback_guard_must_measure_production]]
func donmusTanitici() (map[string]int, error) {
	sonuc := map[string]int{}
	pids, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	for _, p := range pids {
		if !p.IsDir() {
			continue
		}
		if _, e := strconv.Atoi(p.Name()); e != nil {
			continue
		}
		// 🔴 `comm` SAHTE OLABILIR. Denetimde gosterildi: kiraci kodu
		// (Uygulama Calistirici, gercek surec olarak kosuyor) `process.title`
		// ile comm'unu "nginx" yapip, kendi mount ad-alaninda sahte bir
		// /var/log/nginx agaci kurarak bu taramayi zehirleyebiliyor. Sonuc:
		// silinemeyen sahte kirmizi alarm — ve kalici kirmizi bir nobetci,
		// GERCEK bir donmayi maskeler.
		//
		// Iki kapi: (1) gercek ikili yolu, (2) BIZIM mount ad-alanimiz.
		exe, e := os.Readlink(filepath.Join("/proc", p.Name(), "exe"))
		if e != nil || (!strings.HasSuffix(exe, "/nginx") && !strings.HasSuffix(exe, "/nginx (deleted)")) {
			continue
		}
		if !ayniMountNS(p.Name()) {
			continue
		}
		fdDir := filepath.Join("/proc", p.Name(), "fd")
		fds, e := os.ReadDir(fdDir)
		if e != nil {
			continue // süreç kayboldu ya da izin yok — atla
		}
		for _, fd := range fds {
			hedef, e := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if e != nil || !strings.HasPrefix(hedef, "/var/log/nginx/") {
				continue
			}
			// Döndürülmüş dosya deseni: ".log-20260827" ya da silinmiş inode.
			ad := filepath.Base(hedef)
			i := strings.Index(ad, ".log-")
			if i < 0 && !strings.HasSuffix(hedef, " (deleted)") {
				continue
			}
			sonuc[strings.TrimSuffix(hedef, " (deleted)")]++
		}
	}
	return sonuc, nil
}

// ayniMountNS — surec BIZIM mount ad-alanimizda mi?
//
// Farkli bir ad-alanindaki surecin gordugu "/var/log/nginx/..." yolu, bizim
// dosyalarimiz DEGILDIR; adi ayni olan baska bir agactir. Ad-alani farkliysa
// o surec olcume dahil edilmez.
func ayniMountNS(pid string) bool {
	bizim, e1 := os.Readlink("/proc/self/ns/mnt")
	onun, e2 := os.Readlink(filepath.Join("/proc", pid, "ns", "mnt"))
	if e1 != nil || e2 != nil {
		return false // okuyamiyorsak dahil ETME (sahte alarm uretmektense atla)
	}
	return bizim == onun
}

// HealNginxLogReopen — donmuş yeniden-açma durumunu ölçer, düzeltmeyi dener,
// SONRA TEKRAR ÖLÇER. Düzelmediyse sessizce geçmez; hangi dosyaların kaldığını
// açıkça bildirir.
func HealNginxLogReopen() {
	once, err := donmusTanitici()
	if err != nil {
		log.Printf("nginx log donma kontrolu yapilamadi: %v", err)
		return
	}
	if len(once) == 0 {
		return // sağlıklı
	}

	toplam := 0
	for _, n := range once {
		toplam += n
	}
	log.Printf("🔴 nginx %d bayat log tanitiicisi tutuyor (%d dosya) — dondurulmus dosyalara yaziliyor, yeniden acma deneniyor",
		toplam, len(once))

	if out, e := exec.Command("nginx", "-s", "reopen").CombinedOutput(); e != nil {
		log.Printf("🔴 nginx -s reopen BASARISIZ: %v — %s", e, strings.TrimSpace(string(out)))
	}

	sonra, err := donmusTanitici()
	if err != nil {
		return
	}
	if len(sonra) == 0 {
		log.Printf("✓ nginx log yeniden acma duzeltildi (%d dosya kurtarildi)", len(once))
		return
	}

	// 🔴 Negatif kontrol: yeniden açma BAZI dosyaları düzeltmez (ModSecurity
	// denetim günlüğü gibi). Bunu "düzeldi" diye raporlamak, arızayı güven
	// olarak render etmek olurdu. Kalanları isimleriyle bildir.
	kalan := make([]string, 0, len(sonra))
	for f := range sonra {
		kalan = append(kalan, filepath.Base(f))
	}
	sort.Strings(kalan)
	log.Printf("🔴 nginx -s reopen sonrasi HALA %d dosya donmus: %s — bu dosyalar yeniden-acma sinyaline yanit vermiyor (copytruncate gerekir)",
		len(kalan), strings.Join(kalan, ", "))
}

// LogBakimBaslat — log izinlerini ve yeniden-açma sağlığını periyodik ölçer.
//
// 🔴 NEDEN PERİYODİK: iki ayrı pencere var, ikisi de açılışta yapılan tek
// seferlik kontrolün göremediği yerde:
//
//  1. nginx master YENİ bir log dosyasını kendisi açtığında dosya 0644
//     doğar (nginx'te dosya modu yapılandırılamaz). Yeni bir domain
//     eklendiğinde, panel yeniden başlayana kadar o log dünya-okunur kalır.
//     Dizin 0710 olduğu için kiracı DİZİNE giremez — ama savunma tek
//     katmana inmiş olur; ikinci katmanı da ayakta tutuyoruz.
//
//  2. Döndürme her gün olur, panel yeniden başlatması olmaz. Yeniden açma
//     bir gün sessizce başarısız olursa, bir sonraki panel restart'ına kadar
//     kimse görmez — arızayı ilk kez ortaya çıkaran boşluk tam olarak buydu.
//
// Ölçüm ucuz: bir ReadDir + değişen dosyalarda chmod. Aksiyon yalnız gerçek
// bir sapma varsa alınır.
func LogBakimBaslat(aralik time.Duration) {
	if aralik <= 0 {
		aralik = 30 * time.Minute
	}
	go func() {
		// 🔴 Arka plan isinde panic, TUM paneli dusurur.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔴 log bakim dongusu panikledi, durduruldu: %v", r)
			}
		}()
		t := time.NewTicker(aralik)
		defer t.Stop()
		// 🔴 GURULTU BASTIRMA. `modsec_audit.log-*` gibi bazi dosyalar
		// yeniden-acma sinyaline HIC yanit vermez (libmodsecurity kendi
		// tanitiicisini tutar). Onceki surum her 30 dakikada bir ayni iki
		// kirmizi satiri yaziyor ve TUM nginx iscilerine gercek bir
		// `nginx -s reopen` gonderiyordu — duzeltilemeyen bir kosul, sinirsiz
		// bir onarim dongusunu suruyordu.
		//
		// Daha kotusu: kalici kirmizi bir nobetci, GERCEK yeni bir donmayi
		// maskeler. Bu projenin daha once yandigi sinif
		// ([[reference_girginosvm_disk_alert_spam]],
		//  [[feedback_frequency_threshold_hides_faults]]).
		//
		// Artik yalniz KUME DEGISTIGINDE aksiyon alinir ve loglanir.
		var sonKume string
		for range t.C {
			HealNginxLogPerms()
			n, dosyalar := NginxLogDonmaDurumu()
			kume := strings.Join(dosyalar, ",")
			if n == 0 {
				if sonKume != "" {
					log.Printf("✓ nginx bayat log tanitiicisi kalmadi (onceki: %s)", sonKume)
					sonKume = ""
				}
				continue
			}
			if kume == sonKume {
				continue // ayni kosul, zaten bildirildi — sessiz kal
			}
			log.Printf("🔴 periyodik kontrol: nginx %d bayat log tanitiicisi tutuyor (%s) — yeniden acma deneniyor",
				n, strings.Join(dosyalar, ", "))
			HealNginxLogReopen()
			// Kurtarilamayanlari kaydet ki bir daha bagirmayalim.
			_, kalan := NginxLogDonmaDurumu()
			sonKume = strings.Join(kalan, ",")
		}
	}()
}

// NginxLogDonmaDurumu — doctor/sağlık ucu için okunabilir özet.
func NginxLogDonmaDurumu() (int, []string) {
	m, err := donmusTanitici()
	if err != nil || len(m) == 0 {
		return 0, nil
	}
	dosyalar := make([]string, 0, len(m))
	toplam := 0
	for f, n := range m {
		dosyalar = append(dosyalar, fmt.Sprintf("%s(%d)", filepath.Base(f), n))
		toplam += n
	}
	sort.Strings(dosyalar)
	return toplam, dosyalar
}
