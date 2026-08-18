// Package wpsaglama — WordPress çekirdek md5 sağlamalarını sağlar.
//
// 🔴 NEDEN AYRI PAKET: motorun (avmotor) en güçlü katmanı (WP bütünlük) bir
// SaglamaKaynagi'na bağlı ama motorun kendisi ağ/dosya bilmemeli. Bu paket o
// arayüzü hayata geçirir: sürüm → yol→md5 tablosu.
//
// 🔴 GITHUB BAĞIMLILIĞI DERSİ (reference_girginospanel_github_bagimliligi):
// dış servise DOĞRUDAN bağımlılık müşteri sunucularını kilitledi. Sıra:
//  1. DİSK ÖNBELLEĞİ  (/var/lib/girginospanel/wpsaglama/<sürüm>.json)
//  2. KENDİ AYNAMIZ   (surum.girginos.io/wpsaglama/<sürüm>.json)
//  3. WP.org API      (api.wordpress.org/core/checksums)
//
// Her katman bir sonrakine düşer; hiçbiri çalışmazsa motor bütünlüğü ATLAR
// (nil döner) — "ölçemedik" ile "temiz" ASLA karıştırılmaz.
package wpsaglama

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	onbellekDir = "/var/lib/girginospanel/wpsaglama"
	aynaTaban   = "https://surum.girginos.io/wpsaglama"
	wporgAPI    = "https://api.wordpress.org/core/checksums/1.0/"
)

// Kaynak — SaglamaKaynagi arayüzünü hayata geçirir (avmotor'un beklediği).
//
// 🔴 wpKok → sürüm eşlemesini önbellekler: aynı kurulumun her dosyası için
// version.php okumak ve ağ sorgulamak israf olurdu. Bellek-içi harita +
// disk önbelleği iki katman.
type Kaynak struct {
	mu     sync.RWMutex
	bellek map[string]map[string]string // sürüm → (yol→md5)
	http   *http.Client
	kapali bool // ağ tamamen kapalı mod (yalnız disk)
}

func Yeni() *Kaynak {
	return &Kaynak{
		bellek: map[string]map[string]string{},
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

// YalnizDisk — ağ erişimini kapatır (izole/kısıtlı sunucular için).
func (k *Kaynak) YalnizDisk() *Kaynak { k.kapali = true; return k }

var surumRe = regexp.MustCompile(`\$wp_version\s*=\s*'([0-9][0-9a-zA-Z.\-]*)'`)

// 🔴 WordPress dil paketleri version.php'ye `$wp_local_package = 'tr_TR';`
// ekler — bu MESRU bir degisikliktir. WP.org'un en_US saglamasi bu satiri
// icermez, dolayisiyla Ingilizce OLMAYAN her kurulumda version.php YANLIS
// POZITIF verir (canli tr_TR sitede olculdu). Cozum: locale ile cekmek —
// WP.org `?locale=tr_TR` ile version.php'nin dogru (yerel) md5'ini verir.
var localeRe = regexp.MustCompile(`\$wp_local_package\s*=\s*'([a-zA-Z_]+)'`)

// Saglamalar — avmotor.SaglamaKaynagi. wpKok'un sürümünü bulur, o sürümün
// yol→md5 tablosunu döner. Bulunamazsa (nil, false) — ASLA boş harita.
func (k *Kaynak) Saglamalar(wpKok string) (map[string]string, bool) {
	surum := k.surumOku(wpKok)
	if surum == "" {
		return nil, false // sürüm okunamadı → ölçemeyiz
	}
	locale := k.localeOku(wpKok) // boş olabilir → en_US
	anahtar := surum + "|" + locale

	k.mu.RLock()
	if t, ok := k.bellek[anahtar]; ok {
		k.mu.RUnlock()
		if t == nil {
			return nil, false // daha önce denendi, bulunamadı (negatif önbellek)
		}
		return t, true
	}
	k.mu.RUnlock()

	t, kesinYok := k.surumYukle(surum, locale)

	k.mu.Lock()
	// 🔴 C.4: NEGATIF ONBELLEK YALNIZ KESIN YOKLUK icin. Gecici hata
	// (WP.org timeout, transport, non-200) ASLA onbellege girmemeli: --izle
	// uzun omurlu tek Kaynak kullanir; baslangictaki tek bir ag hickirigi
	// yuzunden nil onbellege girerse o kurulumun cekirdegi ajan yeniden
	// baslayana kadar HIC denetlenmez. Sadece 'WP.org 200 + checksums:false'.
	if t != nil {
		k.bellek[anahtar] = t
	} else if kesinYok {
		k.bellek[anahtar] = nil // negatif onbellek: bu surum gercekten yok
	}
	k.mu.Unlock()

	if t == nil {
		return nil, false
	}
	return t, true
}

// surumOku — wpKok/wp-includes/version.php'den $wp_version.
func (k *Kaynak) surumOku(wpKok string) string {
	b, err := os.ReadFile(filepath.Join(wpKok, "wp-includes", "version.php"))
	if err != nil {
		return ""
	}
	m := surumRe.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// localeOku — version.php'deki $wp_local_package (yoksa boş = en_US).
func (k *Kaynak) localeOku(wpKok string) string {
	b, err := os.ReadFile(filepath.Join(wpKok, "wp-includes", "version.php"))
	if err != nil {
		return ""
	}
	m := localeRe.FindSubmatch(b)
	if m == nil {
		return ""
	}
	l := string(m[1])
	// locale de dosya adi/URL'ye girer — dogrula.
	if !gecerliSurum(strings.ReplaceAll(l, "_", "-")) {
		return ""
	}
	return l
}

// surumYukle — 3 katman: disk → ayna → WP.org (locale duyarli).
// Doner: (tablo, kesinYok). kesinYok=true YALNIZCA WP.org 'surum yok' derse
// (200 + checksums:false); gecici hatada kesinYok=false -> negatif onbellege girmez.
func (k *Kaynak) surumYukle(surum, locale string) (map[string]string, bool) {
	if !gecerliSurum(surum) {
		return nil, false
	}
	// Onbellek/URL anahtari: surum + locale (locale bossa yalniz surum = en_US).
	etiket := surum
	if locale != "" && locale != "en_US" {
		etiket = surum + "-" + locale
	}

	// 1) Disk onbellegi
	if t := diskOku(etiket); t != nil {
		return t, false
	}
	if k.kapali {
		return nil, false // yalniz-disk: ag denenmez (gecici say)
	}

	// 2) Kendi aynamiz
	if t := k.jsonCek(aynaTaban + "/" + etiket + ".json"); t != nil {
		diskYaz(etiket, t)
		return t, false
	}

	// 3) WP.org API (locale duyarli). kesin: 200+checksums:false.
	if t, kesin := k.wporgCekK(surum, locale); t != nil {
		diskYaz(etiket, t)
		return t, false
	} else if kesin && (locale == "" || locale == "en_US") {
		return nil, true
	}
	// locale saglamasi gelmezse en_US'e DUS (A.2: dogru anahtara yaz).
	if locale != "" && locale != "en_US" {
		if t, kesin := k.wporgCekK(surum, ""); t != nil {
			diskYaz(etiket, t)
			return t, false
		} else if kesin {
			return nil, true
		}
	}
	return nil, false // gecici hata -> onbellege girmesin
}

// jsonCek — {yol:md5} biçiminde düz bir JSON çeker (aynamızın biçimi).
func (k *Kaynak) jsonCek(url string) map[string]string {
	resp, err := k.http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var t map[string]string
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&t) != nil {
		return nil
	}
	if len(t) == 0 {
		return nil
	}
	return t
}

// wporgCekK — (tablo, kesinYok). kesinYok=true YALNIZCA HTTP 200 + gecerli
// JSON ama checksums bos/false ise (surum gercekten yok). Ag/timeout/non-200
// -> (nil, false) = gecici, onbellege girmez.
func (k *Kaynak) wporgCekK(surum, locale string) (map[string]string, bool) {
	if locale == "" {
		locale = "en_US"
	}
	resp, err := k.http.Get(wporgAPI + "?version=" + surum + "&locale=" + locale)
	if err != nil {
		return nil, false // gecici (transport)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false // gecici (5xx/429)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, false
	}
	var sarma struct {
		Checksums map[string]string `json:"checksums"`
	}
	if json.Unmarshal(body, &sarma) != nil {
		return nil, false // bozuk yanit -> gecici say
	}
	if len(sarma.Checksums) == 0 {
		return nil, true // 200 + gecerli JSON ama checksums yok = KESIN yok
	}
	return sarma.Checksums, false
}

func gecerliSurum(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' ||
			c >= 'A' && c <= 'Z' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func diskYolu(surum string) string { return filepath.Join(onbellekDir, surum+".json") }

func diskOku(surum string) map[string]string {
	b, err := os.ReadFile(diskYolu(surum))
	if err != nil {
		return nil
	}
	var t map[string]string
	if json.Unmarshal(b, &t) != nil || len(t) == 0 {
		return nil
	}
	return t
}

func diskYaz(surum string, t map[string]string) {
	if err := os.MkdirAll(onbellekDir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(t)
	if err != nil {
		return
	}
	// 🔴 Atomik yaz: yarım yazılmış önbellek dosyası bir sonraki okumada
	// parse hatası verir ve her seferinde ağ sorgular. temp + rename.
	tmp := diskYolu(surum) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, diskYolu(surum))
	}
}

// wporgCek2 — test icin: verilen tam URL'den WP.org bicimini ceker.
// (wporgCek sabit API URL kullanir; test sahte sunucuya isaret etmeli.)
func (k *Kaynak) wporgCek2(url string) map[string]string {
	resp, err := k.http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var sarma struct {
		Checksums map[string]string `json:"checksums"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&sarma) != nil {
		return nil
	}
	return sarma.Checksums
}

// normalizeYol — WP.org bazı sürümlerde yolları farklı ayraçla verebilir;
// motorun beklediği ileri-eğik-çizgiye çevir. (Şu an WP.org zaten '/' veriyor,
// ama savunma amaçlı.)
func normalizeYol(y string) string { return strings.ReplaceAll(y, "\\", "/") }
