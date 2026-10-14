package provisioner

// Apache backend vhost'larında erişim günlüğünü kapatma — KURULU vhost'ların açılış süpürmesi.
//
// Şablon (apacheVhostTmpl) artık `CustomLog /dev/null combined` yazar (gerekçe: apacheErisimLoguHedefi).
// Eski şablonla yazılmış /etc/httpd/conf.d/dom_c_*.conf dosyalarındaki
// `CustomLog /var/log/httpd/<alan>.access.log combined` satırı burada /dev/null'a çevrilir; vhost'lar
// yalnız bir sonraki yeniden render'da (SSL yenileme, PHP sürümü vb.) kendiliğinden düzelirdi ve o zamana
// kadar günlük (180: haftada ~1 GB) yazılmaya devam ederdi.
//
// Güvenlik: yalnız panelin başlığını taşıyan dosyalar, yalnız tam bu satır. Yazım karşılaştırmalı +
// atomik; render kilidi altında (vhost yazan her yol aynı kilidi alır). Sonra `httpd -t`; başarısızsa
// değişiklikler GERİ ALINIR. Apache çalışıyorsa graceful reload (çalışmayan Apache BAŞLATILMAZ).
// Eski günlük dosyaları silinmez (logrotate yaşlandırır).

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Panelin Apache vhost başlığındaki ayırt edici ifade (apacheVhostTmpl ilk satırı).
const apacheVhostBaslikIzi = "GirginOSPanel Apache backend"

// reApacheEskiErisimLogu — eski şablonun yazdığı satır (girinti korunur). Başka biçim/yol/format eşleşmez.
var reApacheEskiErisimLogu = regexp.MustCompile(`(?m)^([ \t]*)CustomLog[ \t]+/var/log/httpd/[A-Za-z0-9._-]+\.access\.log[ \t]+combined[ \t]*$`)

// Dış etkili adımlar; testler geçici dizine / sahte komuta çevirir, üretimde SABİT.
var (
	apacheVhostDizini = "/etc/httpd/conf.d"
	apacheKomut       = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }
)

const apacheVhostBoyutSiniri = 1 << 20

// apacheErisimLoguYama — eski erişim günlüğü satırını yeni hedefe çevirir. Panelin başlığı yoksa ya da
// satır yoksa gövde aynen döner (false). İdempotent.
func apacheErisimLoguYama(govde string) (string, bool) {
	if !strings.Contains(govde, apacheVhostBaslikIzi) {
		return govde, false
	}
	yeni := reApacheEskiErisimLogu.ReplaceAllString(govde, "${1}CustomLog "+apacheErisimLoguHedefi+" combined")
	return yeni, yeni != govde
}

// apacheLogDosya: bir yamanın kaydı (geri alma için).
type apacheLogDosya struct {
	yol        string
	once, yeni []byte
}

// apacheLogOzet: bir süpürmenin sonucu (günlük ve testler için).
type apacheLogOzet struct {
	Yamalanan  int  // erişim günlüğü satırı /dev/null'a çevrilen vhost
	Hata       int  // okunamadı / yazılamadı
	GeriAlindi bool // httpd -t başarısız → yamaların hepsi geri alındı
	Yenilendi  bool // Apache graceful reload edildi
}

// apacheErisimLoguSupur — dom_c_*.conf vhost'larını yamalar, doğrular, gerekirse yeniler. Idempotent.
func apacheErisimLoguSupur() apacheLogOzet {
	var oz apacheLogOzet
	yollar, _ := filepath.Glob(filepath.Join(apacheVhostDizini, "dom_c_*.conf"))
	if len(yollar) == 0 {
		return oz
	}
	sort.Strings(yollar)

	// Vhost yazan her yol (WriteVhost → writeApacheVhost) renderKilidi altındadır: yarım okunmuş/yazılmış
	// dosya görmemek ve yeni render'ı ezmemek için aynı kilit.
	renderKilidi.Lock()
	defer renderKilidi.Unlock()

	var degisen []apacheLogDosya
	for _, yol := range yollar {
		if st, err := os.Lstat(yol); err != nil || !st.Mode().IsRegular() {
			continue // symlink / özel dosya: dokunulmaz
		}
		b, err := sinirliDosyaOku(yol, apacheVhostBoyutSiniri)
		if err != nil {
			oz.Hata++
			log.Printf("apache erişim günlüğü: %s okunamadı: %v", yol, err)
			continue
		}
		yeni, tamam := apacheErisimLoguYama(string(b))
		if !tamam {
			continue
		}
		yazildi, err := casYaz(yol, b, []byte(yeni))
		if err != nil {
			oz.Hata++
			log.Printf("apache erişim günlüğü: %s yazılamadı: %v", yol, err)
			continue
		}
		if yazildi {
			degisen = append(degisen, apacheLogDosya{yol: yol, once: b, yeni: []byte(yeni)})
		}
	}
	oz.Yamalanan = len(degisen)
	if oz.Yamalanan == 0 {
		return oz
	}

	if out, err := apacheKomut("httpd", "-t"); err != nil {
		for _, d := range degisen {
			// CAS: dosya hâlâ bizim yazdığımızsa geri al; bu arada başkası yazdıysa onunkine dokunma.
			_, _ = casYaz(d.yol, d.yeni, d.once)
		}
		oz.Yamalanan, oz.GeriAlindi = 0, true
		log.Printf("apache erişim günlüğü: httpd -t BAŞARISIZ, %d vhost yaması GERİ ALINDI (günlük hâlâ yazılıyor): %s",
			len(degisen), strings.TrimSpace(string(out)))
		return oz
	}
	if _, err := apacheKomut("systemctl", "is-active", "--quiet", "httpd"); err != nil {
		return oz // çalışmayan Apache başlatılmaz; sonraki başlatma dosyaları zaten yeni haliyle okur
	}
	if out, err := apacheKomut("systemctl", "reload", "httpd"); err != nil {
		log.Printf("apache erişim günlüğü: httpd reload başarısız (yama dosyada, ilk reload'da devreye girer): %s: %v",
			strings.TrimSpace(string(out)), err)
		return oz
	}
	oz.Yenilendi = true
	return oz
}

// ApacheErisimLoguKapatOnStartup: kurulu Apache vhost'larında erişim günlüğünü kapatır. Arka planda koşar
// (provisioner.Init'i BLOKLAMAZ: render kilidi + httpd -t + reload saniyeler sürebilir) ve çökmeye karşı
// korumalıdır. Idempotent: her açılışta güvenle tekrarlanır.
func ApacheErisimLoguKapatOnStartup() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("apache erişim günlüğü süpürmesi çöktü: %v", r)
			}
		}()
		oz := apacheErisimLoguSupur()
		if oz.Yamalanan+oz.Hata > 0 || oz.GeriAlindi {
			log.Printf("apache erişim günlüğü: %d vhost'ta günlük kapatıldı (/dev/null), %d hata, geri alınan: %v, reload: %v",
				oz.Yamalanan, oz.Hata, oz.GeriAlindi, oz.Yenilendi)
		}
	}()
}
