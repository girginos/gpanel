package provisioner

import (
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// HealNginxLogPerms — nginx erisim/hata loglarini KIRACIYA KAPATIR.
//
// 🔴 Kok sorun: nginx `/var/log/nginx/<domain>.access.log|.error.log` dosyalarini
// 0644 (dunya-okunur) + dizini 0711 (other=x) olarak birakiyor. Shell erisimi olan
// bir kiraci, dosya adi domain'den deterministik oldugu icin KOMSUSUNUN HTTP
// loglarini `cat /var/log/nginx/<komsu>.access.log` ile okuyabiliyordu — ziyaretci
// IP'leri, query-string'deki session/token/API-anahtari sizabilir.
//
// Cozum: dizinden other=x kaldir → kiraci ISME GORE bile dosyaya erisemez,
// cunku dizin gecisi engellenir + dosyalari 0640'a indir (derinlemesine savunma).
//
// 🔴 DUZELTME (olculdu): dizin 0700 root:root YAPILAMAZ. Onceki surum boyleydi
// ve gerekce "panel ve logrotate root oldugu icin etkilenmez" diyordu. Bu
// gerekcenin deligi su: etkilenen logrotate DEGIL, **nginx iscileriydi**.
// Isciler `nginx` kullanicisi olarak kosar ve dondurmeden sonra kendi log
// dosyalarini YENIDEN ACMASI GEREKEN taraf onlardir. 0700'de dizin gecisi
// engellendigi icin acamiyor, devraldiklari ESKI tanitiiciya yazmaya devam
// ediyorlardi. Sonuc: guncel .log 0 bayt kalir, dondurulmus dosya buyumeye
// devam eder, `notifempty` bos dosyayi atlayinca dondurme BIR DAHA calismaz
// → kalici kilitlenme. Uretimde (.180) 265 bayat tanitici / 408 MB olculdu.
//
// Dogru izin: **0710 root:nginx**. Grup `x` biti iscilere yalniz GECIS verir
// (listeleme yok, okuma yok); dosyalar 0640 nginx:root oldugu icin isci kendi
// dosyasini acabilir. Kiraci grupta olmadigi icin ne gecebilir ne listeleyebilir
// — izolasyon niyeti AYNEN korunur. Olculdu:
//
//	0700 → isci yazamaz, reopen sonrasi 3 bayat fd, guncel .log 0 bayt
//	0710 → isci yazar,   reopen sonrasi 0 bayat fd, guncel .log buyuyor
//
// Kiraci her iki durumda da komsu logunu OKUYAMAZ (negatif kontrol yapildi).
//
// 🔴 Izin duzeltmesi TEK BASINA mevcut donmayi cozmez: zaten acilmis bayat
// tanitiicilar yerinde kalir. Izin duzeltildikten SONRA bir yeniden-acma
// gerekir -- bu yuzden main.go'da HealNginxLogPerms'in HEMEN ARDINDAN
// HealNginxLogReopen cagrilir. Sira ters olsaydi reopen yine EACCES alirdi.
// Acilista + idempotent; nginx/logrotate/paket guncellemesi izni sifirlarsa bir
// sonraki panel yeniden baslatmada tekrar uygulanir.
func HealNginxLogPerms() {
	const dir = "/var/log/nginx"
	fi, err := os.Stat(dir)
	if err != nil {
		return
	}
	// Dizin: root + nginx grubu gezebilsin (0710 root:nginx).
	//
	// nginx grubu yoksa 0700'e dusulur: izolasyon korunur ama log yeniden-acma
	// KIRIK kalir. Bu durum sessiz gecilmez — acikca uyarilir, cunku sonucu
	// sinsi bir arizadir (loglar dondurulmus dosyaya akmaya devam eder).
	hedefMod := os.FileMode(0o710)
	grp, gerr := user.LookupGroup("nginx")
	if gerr != nil {
		hedefMod = 0o700
		log.Printf("🔴 'nginx' grubu bulunamadi (%v) — log dizini 0700 birakiliyor. "+
			"nginx iscileri dondurmeden sonra loglari YENIDEN ACAMAZ; loglar dondurulmus "+
			"dosyalara akmaya devam eder.", gerr)
	}
	if fi.Mode().Perm() != hedefMod {
		if e := os.Chmod(dir, hedefMod); e != nil {
			log.Printf("nginx log dizini izni sertlestirilemedi: %v", e)
		} else {
			log.Printf("nginx log dizini %#o yapildi (kiraci cross-tenant log okumasi kapali)", hedefMod)
		}
	}
	if gerr == nil {
		gid, cerr := strconv.Atoi(grp.Gid)
		if cerr == nil {
			// Sahiplik root:nginx olmali; grup yanlissa `x` biti ise yaramaz.
			// Lchown: dizin bir symlink'e cevrilmisse hedefi degil kendisini hedefle.
			if e := os.Lchown(dir, 0, gid); e != nil {
				log.Printf("nginx log dizini grubu ayarlanamadi: %v", e)
			}
		}
	}
	// Dosyalar: dunya-okunur bitini kaldir (0640). Derinlemesine savunma.
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ad := e.Name()
		if !strings.Contains(ad, ".log") {
			continue
		}
		info, err := e.Info() // ReadDir -> Lstat: symlink'in KENDISI
		if err != nil {
			continue
		}
		// 🔴 SYMLINK TAKIP ETME. os.Chmod symlink'i izler; bir symlink'in kendi
		// modu daima 0777 gorunur, yani "other biti var" testini her zaman
		// gecer ve chmod HEDEFE uygulanirdi. Bugun ulasilamaz (dizin 0710,
		// grup yazamaz) ama root'un yazabildigi herhangi bir yol buraya
		// /etc/shadow'a giden bir symlink birakirsa panel onu 30 dakikada bir
		// 0640 yapardi. [[feedback_root_fs_symlink_safe]]
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Mode().Perm()&0o007 == 0 {
			continue // zaten other-erisimi yok
		}
		_ = os.Chmod(filepath.Join(dir, ad), 0o640)
	}
}
