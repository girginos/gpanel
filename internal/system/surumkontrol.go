package system

// Sürüm kontrolü + güvenlik duyuru kanalı.
//
// NE YAPAR: Panel günde bir kez yayın manifestini (surum.json) okur; yeni sürüm
// veya kritik güvenlik duyurusu varsa arayüzde gösterir. Bugüne kadar müşterilere
// acil yama duyurusu yapacak HİÇBİR kanalımız yoktu — asıl kazanç bu.
//
// 🔒 GİZLİLİK — bilerek verilmiş kararlar:
//   - GÖNDERİLEN TEK ŞEY: anonim kurulum kimliği (?id=) + panel sürümü (?v=).
//     Kimlik SAF RASTGELEDİR; hostname/IP/MAC'ten türetilmez, dolayısıyla geri
//     çözülemez ve hangi müşteriye ait olduğu bilinemez.
//   - GÖNDERİLMEYENLER: domain adları, hostname, IP, e-posta, müşteri verisi,
//     veritabanı içeriği, lisans. Gövde ve özel başlık da yok.
//   - Sunucu tarafı erişim logu IP KAYDETMEZ (bkz. surum.girginos.io nginx
//     log_format surum_anonim) — sayım yalnız distinct kimlik üzerinden yapılır.
//   - PANEL_SURUM_KONTROL=0 → hiç istek atılmaz (goroutine hiç başlamaz).
//   - Bu davranış README'de açıkça belgelenmiştir.
//
// AĞ HATASI = SESSİZ. İnternet yoksa panel etkilenmez; durum "kontrol edilemedi"
// olarak kalır, hiçbir yerde hata patlatmaz.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

const (
	surumUCVarsayilan = "https://surum.girginos.io/surum.json"
	surumKimlikYol    = "/etc/girginospanel/kurulum-kimlik"
	surumOnbellekYol  = "/opt/girginospanel/surum-onbellek.json"
	surumPeriyot      = 24 * time.Hour
	surumGovdeSiniri  = 64 << 10 // 64KB — manifest küçüktür; fazlası kötüye kullanım
)

// SurumYayin — yayın manifestinin (surum.json) şeması.
type SurumYayin struct {
	SonSurum    string `json:"son_surum"`
	Duyuru      string `json:"duyuru"`
	Kritik      bool   `json:"kritik"`
	YayinTarihi string `json:"yayin_tarihi"`
}

type surumOnbellek struct {
	Yayin      SurumYayin `json:"yayin"`
	SonKontrol time.Time  `json:"son_kontrol"`
}

var (
	surumMu     sync.RWMutex
	surumYayin  SurumYayin
	surumSon    time.Time
	surumHata   string
	surumMevcut string
	surumAcik   bool
)

// surumKontrolAcikMi — PANEL_SURUM_KONTROL=0 ise tamamen kapalı.
func surumKontrolAcikMi() bool {
	v := strings.TrimSpace(os.Getenv("PANEL_SURUM_KONTROL"))
	return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "hayir")
}

func surumUC() string {
	if v := strings.TrimSpace(os.Getenv("PANEL_SURUM_UC")); v != "" {
		return v
	}
	return surumUCVarsayilan
}

// KurulumKimligi — kalıcı, anonim kurulum kimliği. Yoksa üretir.
// SAF RASTGELE: hostname/IP/MAC'ten TÜRETİLMEZ — türetilmiş kimlik geri
// çözülebilir, yani anonim değildir.
func KurulumKimligi() string {
	if b, err := os.ReadFile(surumKimlikYol); err == nil {
		if s := strings.TrimSpace(string(b)); len(s) >= 16 {
			return s
		}
	}
	ham := make([]byte, 16)
	if _, err := rand.Read(ham); err != nil {
		return "" // üretilemedi — sessiz geç, kritik değil
	}
	kimlik := hex.EncodeToString(ham)
	_ = os.MkdirAll(filepath.Dir(surumKimlikYol), 0o755)
	// 0600 + root: kimlik müşteri sitelerinden okunabilir olmamalı
	_ = os.WriteFile(surumKimlikYol, []byte(kimlik+"\n"), 0o600)
	return kimlik
}

// SurumBaslat — arka plan sürüm kontrolünü başlatır. Kapalıysa hiç çalışmaz.
func SurumBaslat(mevcutSurum string) {
	surumMu.Lock()
	surumMevcut = mevcutSurum
	surumAcik = surumKontrolAcikMi()
	surumMu.Unlock()

	// Kimliği kapalıyken de üret: ileride sayıma geçilirse kurulum kimliği hazır olur.
	_ = KurulumKimligi()

	if !surumKontrolAcikMi() {
		return
	}
	surumOnbellekYukle()

	go func() {
		// Açılışta rastgele 10-60sn gecikme: kurulumlar aynı anda vurup ucu
		// boğmasın (thundering herd). Statik CDN için bu serpiştirme yeterli;
		// dakikalarca beklemek panelin durumu geç göstermesine yol açardı.
		time.Sleep(surumRastgele(10*time.Second, 60*time.Second))
		for {
			surumGetir()
			// Periyoda ±2 saat serpiştirme — aynı sebep.
			time.Sleep(surumPeriyot + surumRastgele(-2*time.Hour, 2*time.Hour))
		}
	}()
}

func surumRastgele(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	if err != nil {
		return min
	}
	return min + time.Duration(n.Int64())
}

// surumIstekURL — uca anonim kurulum kimliği + sürümü ekler.
// Uç zaten sorgu dizesi içeriyorsa (operatör kendi ucunu verdiyse) korunur.
func surumIstekURL() string {
	ham := surumUC()
	u, err := url.Parse(ham)
	if err != nil {
		return ham // ayrıştırılamadı — olduğu gibi dene
	}
	q := u.Query()
	if k := KurulumKimligi(); k != "" {
		q.Set("id", k)
	}
	q.Set("v", surumMevcutOku())
	u.RawQuery = q.Encode()
	return u.String()
}

// surumGetir — manifesti çeker (anonim kimlik + sürüm ile).
func surumGetir() {
	cli := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, surumIstekURL(), nil)
	if err != nil {
		surumHataYaz("istek kurulamadı")
		return
	}
	// Sürüm bilgisi yalnız User-Agent'ta; sunucu tarafı yayın dağılımını
	// görmek isterse buradan görür, ayrı bir tanımlayıcı taşımaz.
	req.Header.Set("User-Agent", "GirginOSPanel/"+surumMevcutOku())

	resp, err := cli.Do(req)
	if err != nil {
		surumHataYaz("ağa ulaşılamadı")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		surumHataYaz("uç HTTP " + resp.Status)
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, surumGovdeSiniri))
	if err != nil {
		surumHataYaz("yanıt okunamadı")
		return
	}
	var y SurumYayin
	if err := json.Unmarshal(b, &y); err != nil {
		surumHataYaz("manifest çözümlenemedi")
		return
	}
	if strings.TrimSpace(y.SonSurum) == "" {
		surumHataYaz("manifestte sürüm yok")
		return
	}

	surumMu.Lock()
	surumYayin = y
	surumSon = time.Now()
	surumHata = ""
	surumMu.Unlock()
	surumOnbellekYaz()
}

func surumMevcutOku() string {
	surumMu.RLock()
	defer surumMu.RUnlock()
	return surumMevcut
}

func surumHataYaz(m string) {
	surumMu.Lock()
	surumHata = m
	surumMu.Unlock()
}

func surumOnbellekYaz() {
	surumMu.RLock()
	o := surumOnbellek{Yayin: surumYayin, SonKontrol: surumSon}
	surumMu.RUnlock()
	if b, err := json.Marshal(o); err == nil {
		_ = os.MkdirAll(filepath.Dir(surumOnbellekYol), 0o755)
		_ = os.WriteFile(surumOnbellekYol, b, 0o644)
	}
}

// surumOnbellekYukle — restart sonrası son bilinen durumu geri yükler ki
// panel açılır açılmaz "bilinmiyor" göstermesin.
func surumOnbellekYukle() {
	b, err := os.ReadFile(surumOnbellekYol)
	if err != nil {
		return
	}
	var o surumOnbellek
	if json.Unmarshal(b, &o) != nil {
		return
	}
	surumMu.Lock()
	surumYayin = o.Yayin
	surumSon = o.SonKontrol
	surumMu.Unlock()
}

// SurumKontrolYenile — operatör "şimdi kontrol et" derse. Kapalıysa istek atmaz.
func SurumKontrolYenile(w http.ResponseWriter, r *http.Request) {
	if !surumKontrolAcikMi() {
		httpx.WriteError(w, http.StatusConflict, "sürüm kontrolü kapalı (PANEL_SURUM_KONTROL=0)")
		return
	}
	surumGetir()
	SurumKontrolDurum(w, r)
}

// SurumKontrolDurum — arayüz için mevcut durum.
func SurumKontrolDurum(w http.ResponseWriter, r *http.Request) {
	surumMu.RLock()
	mevcut, y, son, hata, acik := surumMevcut, surumYayin, surumSon, surumHata, surumAcik
	surumMu.RUnlock()

	// Kasıtlı olarak SADECE eşitlik kıyası: sürüm etiketleri "0.3.0-f2" gibi
	// serbest biçimli; semver sıralaması yanlış pozitif üretir. Farklıysa
	// "yeni sürüm var" deriz — dev makinede ileri sürüm çalıştıran operatör
	// için yanıltıcı olabilir, bilinçli kabul.
	guncellemeVar := acik && y.SonSurum != "" && y.SonSurum != mevcut

	cevap := map[string]any{
		"acik":           acik,
		"mevcut":         mevcut,
		"son":            y.SonSurum,
		"guncelleme_var": guncellemeVar,
		"duyuru":         y.Duyuru,
		"kritik":         y.Kritik && guncellemeVar,
		"yayin_tarihi":   y.YayinTarihi,
		"hata":           hata,
	}
	if !son.IsZero() {
		cevap["son_kontrol"] = son.UTC().Format(time.RFC3339)
	}
	httpx.WriteJSON(w, http.StatusOK, cevap)
}
