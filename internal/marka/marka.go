// Package marka — panelin KENDİ görünen markası (whitelabel).
//
// MİMARİ KARARI: core burada hiçbir eklentiyi TANIMAZ. Yalnız sabit bir
// dosyayı okur; dosya yoksa varsayılan GirginOSPanel markasını döner. Dosyayı
// yazan taraf `whitelabel` eklentisidir, ama core onun kurulu olup olmadığını
// bilmez ve umursamaz — kanca jeneriktir.
//
// 🔴 NEDEN CORE'DA BİR PUBLIC UÇ VAR (eklenti proxy'si yerine):
// Marka bilgisini ilk gösteren yer GİRİŞ SAYFASIDIR ve o sayfa kimlik
// doğrulamadan ÖNCE render edilir. Eklenti proxy'si (/api/v1/eklenti/...)
// RequireAuth arkasındadır → giriş sayfası oradan okuyamaz. Ayrıca nginx
// yalnız /api/ yolunu panele proxyler; ayrı bir statik yol açmak vhost'a
// dokunmayı gerektirirdi. Bu yüzden okuma ucu core'da ve auth dışındadır.
//
// 🔴 BU UÇ KİMLİK DOĞRULAMASIZDIR → yalnız marka alanları döner. Sürüm,
// sunucu adı, kurulu eklenti listesi gibi parmak-izi bilgisi BURADAN SIZMAZ.
package marka

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Kok — marka artefaktlarının dizini. Eklenti buraya yazar, core buradan okur.
//
// 🔴 frontend-dist ALTINDA DEĞİL: panel güncellemesi frontend-dist'i yeniden
// serer; marka orada olsaydı her güncellemede sessizce SIFIRLANIRDI.
const Kok = "/opt/girginospanel/marka"

const (
	AyarDosya = "marka.json"
	LogoDosya = "logo.bin" // uzantı YOK: ad bizim, yüklenen dosya adı ASLA kullanılmaz
	TipDosya  = "logo.tip"

	// Giriş sayfası bandı — logodan AYRI bir görsel yuvası.
	BannerDosya = "banner.bin"
	BannerTip   = "banner.tip"

	// Favicon — tarayıcı sekmesi simgesi. Logodan AYRI yuva: panel logosu
	// genelde yatay ve geniştir; onu 16-32 piksele sıkıştırmak sekmede
	// tanınmaz bir leke bırakır. Marka sahibi ikisini ayrı seçebilmeli.
	FaviconDosya = "favicon.bin"
	FaviconTip   = "favicon.tip"

	// LogoMaks — 512 KB. Panel logosu için fazlasıyla yeterli; üst sınır
	// olmadan kimliği doğrulanmamış /marka/logo ucu disk-okuma amplifikatörü
	// olurdu.
	LogoMaks = 512 * 1024
)

// Marka — giriş sayfası ve panel kabuğunun okuduğu alanlar.
//
// Alanlar bilinçli olarak DAR: hepsi düz METİN ya da katı biçimli (renk/URL).
// Serbest HTML/CSS alanı YOKTUR — bir whitelabel formu, panelin kendi
// oturumunda çalışan XSS için en doğal taşıyıcıdır.
type Marka struct {
	PanelAdi  string `json:"panel_adi"`
	Baslik    string `json:"baslik"`  // <title>
	KisaAd    string `json:"kisa_ad"` // dar kenar çubuğu
	VurguRenk string `json:"vurgu_renk"`
	Logo      bool   `json:"logo"`       // /api/v1/marka/logo servis edilebilir mi
	LogoSurum int64  `json:"logo_surum"` // önbellek kırıcı (mtime)
	LogoTip   string `json:"-"`          // sunucu içi; istemciye gerekmez

	// Favicon — tarayıcı sekmesi simgesi; logodan AYRI yuva (bkz. FaviconDosya).
	Favicon      bool   `json:"favicon"`
	FaviconSurum int64  `json:"favicon_surum"`
	FaviconTip   string `json:"-"`

	// Footer — panelin HER sayfasında görünen alt bilgi. `giris_dipnot`tan
	// AYRIDIR: o yalnız giriş ekranına aittir. Boşsa alt bilgi hiç çizilmez —
	// boş bir şerit bırakmak, markasız panelde gereksiz gürültü olurdu.
	Footer string `json:"footer"`

	GirisBaslik string `json:"giris_baslik"`
	GirisAlt    string `json:"giris_alt"`
	GirisDipnot string `json:"giris_dipnot"`
	SurumGoster bool   `json:"surum_goster"`
	DestekURL   string `json:"destek_url"`

	// Giriş sayfası bandı: görsel ve/veya metin. İkisi de isteğe bağlı ve
	// birlikte kullanılabilir (görselin altında duyuru metni).
	// WebmailMarka — Roundcube webmail de markalansin mi.
	//
	// 🔴 VARSAYILAN KAPALI. Webmail, RPM ile gelen ayri bir uygulamadir ve
	// musteriye donuk bir yuzeydir; panel markasi degistigi anda onun da
	// gorunumunu degistirmek, istenmeyen bir yan etki olurdu. Acmak
	// bilincli bir karar olsun.
	WebmailMarka bool `json:"webmail_marka"`

	Banner      bool   `json:"banner"`
	BannerSurum int64  `json:"banner_surum"`
	BannerMetin string `json:"banner_metin"`
	BannerURL   string `json:"banner_url"`
}

// Varsayilan — whitelabel kurulu değilken panelin kendi kimliği.
func Varsayilan() Marka {
	return Marka{
		PanelAdi: "GirginOSPanel",
		Baslik:   "GirginOSPanel",
		KisaAd:   "gPanel",
		// 🔴 Varsayılan MAVİ (#2563eb). Eskiden #ea580c idi ve frontend bu
		// değeri görüp "bu API varsayılanıdır" varsayarak maviye eşliyordu —
		// o eşleme yönetici turuncuyu BİLEREK seçtiğinde de devreye girip
		// seçimi eziyordu. Eşleme kaldırıldı; varsayılanı burada söylüyoruz.
		// styles.css :root --brand-600 ve frontend VARSAYILAN ile aynı olmalı.
		VurguRenk:   "#2563eb",
		GirisBaslik: "Hoş geldiniz",
		GirisAlt:    "",
		GirisDipnot: "",
		SurumGoster: true,
	}
}

// LogoTipGecerli — servis edilebilir logo MIME'ları.
//
// 🔴 image/svg+xml BİLEREK YOK. SVG bir BELGEDİR: <script> taşıyabilir ve
// /api/v1/marka/logo adresine doğrudan gidildiğinde tarayıcı onu sayfa olarak
// render eder → panelin KENDİ kaynağında (origin) betik çalışır → oturum
// çalınabilir. <img> içinde zararsız olması yeterli değil; uç doğrudan
// gezilebilir. Bu yüzden SVG hem yüklemede hem serviste reddedilir.
func LogoTipGecerli(t string) bool {
	switch t {
	case "image/png", "image/jpeg", "image/webp", "image/x-icon", "image/gif":
		return true
	}
	return false
}

// hexRGB — #rrggbb → [r,g,b]. Gecersizse nil.
func hexRGB(h string) []int {
	if !RenkGecerli(h) {
		return nil
	}
	out := make([]int, 3)
	for i := 0; i < 3; i++ {
		var v int
		for j := 0; j < 2; j++ {
			c := h[1+i*2+j]
			var d int
			switch {
			case c >= '0' && c <= '9':
				d = int(c - '0')
			case c >= 'a' && c <= 'f':
				d = int(c-'a') + 10
			default:
				d = int(c-'A') + 10
			}
			v = v*16 + d
		}
		out[i] = v
	}
	return out
}

// ── okuma + önbellek ────────────────────────────────────────────────────────

type onbellek struct {
	mu    sync.RWMutex
	m     Marka
	mtime int64
	boyut int64
	dolu  bool
}

var ob onbellek

// Oku — marka.json'u okur. Dosya yoksa/bozuksa VARSAYILANA düşer.
//
// 🔴 FAIL-OPEN BİLİNÇLİ: bu bir yetki kapısı değil, bir görünüm ayarıdır.
// Bozuk bir marka dosyası yüzünden GİRİŞ SAYFASININ AÇILMAMASI, korunan
// hiçbir şeyi korumaz — yalnız paneli erişilemez kılardı.
func Oku() Marka {
	m, _ := OkuKesin()
	return m
}

// OkuKesin — Oku'nun hata döndüren biçimi.
//
// 🔴 NEDEN AYRI BİR BİÇİM GEREKİYOR: Oku() bozuk/okunamayan dosyada
// Varsayilan()'a düşer ve bu bir GÖRÜNÜM ucu için doğrudur. Ama
// Varsayilan().WebmailMarka == false'tur ve webmail döngüsü bu değeri
// "kullanıcı markayı kapattı" sayıp üretilmiş temayı SİLİYORDU. Yani geçici
// bir okuma hatası, YIKICI bir eyleme dönüşüyordu (ölçüldü: bozuk JSON →
// 30 sn içinde skins/gosp ve gosp-marka.inc.php yok oldu).
//
// Kural: fail-open YALNIZ görüntüleme içindir. Yıkıcı bir kol, ancak
// BAŞARIYLA OKUNMUŞ bir "kapalı" değeriyle tetiklenebilir.
//
// Dosyanın hiç olmaması hata DEĞİLDİR (henüz hiç marka kaydedilmemiş);
// okunamama/çözümlenememe hatadır.
func OkuKesin() (Marka, error) {
	yol := filepath.Join(Kok, AyarDosya)
	fi, err := os.Stat(yol)
	if err != nil {
		if os.IsNotExist(err) {
			return Varsayilan(), nil
		}
		return Varsayilan(), err
	}
	mt, bo := fi.ModTime().UnixNano(), fi.Size()

	ob.mu.RLock()
	if ob.dolu && ob.mtime == mt && ob.boyut == bo {
		m := ob.m
		ob.mu.RUnlock()
		return m, nil
	}
	ob.mu.RUnlock()

	ham, err := os.ReadFile(yol)
	if err != nil {
		return Varsayilan(), err
	}
	m := Varsayilan()
	if err := json.Unmarshal(ham, &m); err != nil {
		return Varsayilan(), fmt.Errorf("marka.json çözümlenemedi: %w", err)
	}
	m = Normalize(m)

	ob.mu.Lock()
	ob.m, ob.mtime, ob.boyut, ob.dolu = m, mt, bo, true
	ob.mu.Unlock()
	return m, nil
}

// Normalize — okunan değerleri güvenli sınırlara çeker.
//
// 🔴 DOĞRULAMA HEM YAZARKEN HEM OKURKEN YAPILIR. Yalnız yazarken doğrulamak,
// dosyaya kabuktan elle dokunan (ya da eski sürümün yazdığı) bir değerin
// doğrulanmadan panele basılması demekti.
func Normalize(m Marka) Marka {
	v := Varsayilan()
	m.PanelAdi = metinKirp(m.PanelAdi, 40)
	m.Baslik = metinKirp(m.Baslik, 60)
	m.KisaAd = metinKirp(m.KisaAd, 12)
	m.GirisBaslik = metinKirp(m.GirisBaslik, 60)
	m.GirisAlt = metinKirp(m.GirisAlt, 120)
	m.GirisDipnot = metinKirp(m.GirisDipnot, 120)
	m.BannerMetin = metinKirp(m.BannerMetin, 160)
	// 🔴 Footer da OKURKEN kırpılır. Eksikti: yalnız eklenti YAZARKEN kırpıyordu
	// (api.go: kirp(m.Footer, 200)); elle düzenlenmiş bir marka.json'daki
	// `"footer": "   "` buradan geçip DashboardLayout'ta truthy olur ve içi boş,
	// üstünde border-t bulunan bir şerit çizerdi — struct yorumunun "Boşsa alt
	// bilgi hiç çizilmez" vaadinin tersi. Ayrıca metinKirp atlandığı için 200
	// rune sınırı ve görünmez/BIDI karakter süzgeci bu tek alanda devre dışıydı.
	// Sınır 200: eklentideki kırpma ile AYNI olmalı, yoksa katman anlamını yitirir.
	m.Footer = metinKirp(m.Footer, 200)
	if m.PanelAdi == "" {
		m.PanelAdi = v.PanelAdi
	}
	if m.Baslik == "" {
		m.Baslik = m.PanelAdi
	}
	if m.KisaAd == "" {
		m.KisaAd = v.KisaAd
	}
	if m.GirisBaslik == "" {
		m.GirisBaslik = v.GirisBaslik
	}
	if !RenkGecerli(m.VurguRenk) {
		m.VurguRenk = v.VurguRenk
	}
	// Küçük harfe indir: `#EA580C` ile `#ea580c` aynı rengi gösterir ama
	// farklı imza üretip webmail temasını boşuna yeniden yazdırırdı.
	m.VurguRenk = strings.ToLower(m.VurguRenk)
	if !URLGecerli(m.DestekURL) {
		m.DestekURL = ""
	}
	if !URLGecerli(m.BannerURL) {
		m.BannerURL = ""
	}
	return m
}

// metinKirp — kontrol karakterlerini atar, rune sınırına kırpar.
//
// 🔴 Kontrol karakterleri (\n, \r,  , NUL) atılır: bunlar JSON'da
// taşınıp panelde satır kırma/başlık enjeksiyonu gibi sürprizlere yol açar.
func metinKirp(s string, maks int) string {
	r := make([]rune, 0, len(s))
	for _, c := range s {
		if gorunmezMi(c) {
			continue
		}
		r = append(r, c)
		if len(r) >= maks {
			break
		}
	}
	// 🔴 strings.TrimSpace: eklentideki kırpma ile AYNI olmalı. Önceki sürüm
	// yalnız ' ' ve '\t' atıyordu; NBSP (U+00A0) gibi boşluklar geçiyordu ve
	// core'un "elle düzenlenmiş dosyaya karşı" savunması eklentiden ZAYIF
	// kalıyordu — iki katmanın farklı davranması, katmanın anlamını yitirir.
	return strings.TrimSpace(string(r))
}

// gorunmezMi — atılacak karakterler. Eklentideki eşiyle AYNI kümedir.
//
// 🔴 BIDI ve sıfır-genişlik karakterleri GÖRÜNMEDEN metnin okunma yönünü
// değiştirir; whitelabel bir panelde marka adını son kullanıcı görür ve
// "Nova<RLO>…" gibi bir ad başka bir markayı taklit edebilir.
func gorunmezMi(c rune) bool {
	switch {
	case c < 0x20, c == 0x7f:
		return true
	case c == 0x2028 || c == 0x2029:
		return true
	case c >= 0x200b && c <= 0x200f:
		return true
	case c >= 0x202a && c <= 0x202e:
		return true
	case c >= 0x2066 && c <= 0x2069:
		return true
	case c == 0xfeff:
		return true
	}
	return false
}

// RenkGecerli — yalnız #rrggbb. Serbest CSS rengi KABUL EDİLMEZ: değer bir
// CSS değişkenine yazıldığı için `red;background:url(...)` gibi bir girdi
// stil enjeksiyonu olurdu.
func RenkGecerli(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// URLGecerli — yalnız http/https. 🔴 `javascript:` ve `data:` şemaları
// reddedilir: destek bağlantısı panelde tıklanabilir bir <a href> olur.
func URLGecerli(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 200 {
		return false
	}
	// Şema karşılaştırması VAKA-DUYARSIZ: `HTTP://ornek.test` geçerlidir.
	alt := strings.ToLower(s)
	if !strings.HasPrefix(alt, "http://") && !strings.HasPrefix(alt, "https://") {
		return false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e {
			return false // boşluk/kontrol/unicode → kabul etme
		}
	}
	return true
}

// ── HTTP uçları (AUTH YOK) ──────────────────────────────────────────────────

// genelGorunum — AUTH DIŞI uçtan dönen alanlar.
//
// 🔴 `webmail_marka` BİLEREK DIŞARIDA. Arayüzün ona ihtiyacı yok ama
// kimliği doğrulanmamış bir istemciye "bu sunucuda Roundcube kurulu ve
// markalanmış" bilgisini veriyordu — paket dokümanının "parmak izi bilgisi
// buradan sızmaz" iddiasıyla çelişiyordu.
type genelGorunum struct {
	PanelAdi     string `json:"panel_adi"`
	Baslik       string `json:"baslik"`
	KisaAd       string `json:"kisa_ad"`
	VurguRenk    string `json:"vurgu_renk"`
	Logo         bool   `json:"logo"`
	LogoSurum    int64  `json:"logo_surum"`
	Favicon      bool   `json:"favicon"`
	FaviconSurum int64  `json:"favicon_surum"`
	Footer       string `json:"footer"`
	GirisBaslik  string `json:"giris_baslik"`
	GirisAlt     string `json:"giris_alt"`
	GirisDipnot  string `json:"giris_dipnot"`
	SurumGoster  bool   `json:"surum_goster"`
	DestekURL    string `json:"destek_url"`
	Banner       bool   `json:"banner"`
	BannerSurum  int64  `json:"banner_surum"`
	BannerMetin  string `json:"banner_metin"`
	BannerURL    string `json:"banner_url"`
}

// Handler — GET /api/v1/marka
func Handler(w http.ResponseWriter, _ *http.Request) {
	m := Oku()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(genelGorunum{
		PanelAdi: m.PanelAdi, Baslik: m.Baslik, KisaAd: m.KisaAd,
		VurguRenk: m.VurguRenk, Logo: m.Logo, LogoSurum: m.LogoSurum,
		Favicon: m.Favicon, FaviconSurum: m.FaviconSurum, Footer: m.Footer,
		GirisBaslik: m.GirisBaslik, GirisAlt: m.GirisAlt, GirisDipnot: m.GirisDipnot,
		SurumGoster: m.SurumGoster, DestekURL: m.DestekURL,
		Banner: m.Banner, BannerSurum: m.BannerSurum,
		BannerMetin: m.BannerMetin, BannerURL: m.BannerURL,
	})
}

// LogoHandler — GET /api/v1/marka/logo
func LogoHandler(w http.ResponseWriter, r *http.Request) {
	m := Oku()
	gorselServisEt(w, r, m.Logo, LogoDosya, TipDosya)
}

// FaviconHandler — GET /api/v1/marka/favicon (tarayıcı sekmesi simgesi)
//
// Logo ile AYNI servis yolunu kullanır: tek bir güvenlik kontrolü kümesi
// (boyut, MIME beyaz listesi, önbellek başlıkları) üç görsel için de geçerli.
func FaviconHandler(w http.ResponseWriter, r *http.Request) {
	m := Oku()
	gorselServisEt(w, r, m.Favicon, FaviconDosya, FaviconTip)
}

// BannerHandler — GET /api/v1/marka/banner (giriş sayfası bandı)
func BannerHandler(w http.ResponseWriter, r *http.Request) {
	m := Oku()
	gorselServisEt(w, r, m.Banner, BannerDosya, BannerTip)
}

// gorselServisEt — logo ve bant için ORTAK servis yolu.
//
// İki ayrı kopya yazmak, birinde düzeltilen bir güvenlik kontrolünün
// diğerinde eksik kalmasına açık kapı bırakırdı.
func gorselServisEt(w http.ResponseWriter, r *http.Request, varMi bool, veriAd, tipAd string) {
	yol := filepath.Join(Kok, veriAd)
	fi, err := os.Stat(yol)
	if !varMi || err != nil {
		http.NotFound(w, r)
		return
	}
	tip := tipOku(tipAd)
	// 🔴 SERVİSTE DE DOĞRULA: dosyaya elle dokunulup MIME'ı svg yapılmış
	// olabilir. Yazma tarafındaki kontrole güvenip burada atlamak, tek bir
	// dosya düzenlemesini panelde XSS'e çevirirdi.
	if !LogoTipGecerli(tip) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", tip)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Uç doğrudan gezilebilir; belge olarak açılırsa hiçbir şey yükleyemesin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", `"`+strconv.FormatInt(fi.ModTime().UnixNano(), 36)+`"`)
	// 🔴 Dosya BELLEĞE okunur: http.ServeContent'e açık bir *os.File vermek,
	// onu kimin kapatacağı sorusunu doğurur. Boyut zaten LogoMaks ile sınırlı;
	// bir zamanlayıcıyla "sonra kapat" demek fd sızıntısıdır.
	if fi.Size() > LogoMaks {
		http.NotFound(w, r)
		return
	}
	ham, err := os.ReadFile(yol)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Stat ile okuma arasında dosya büyümüş olabilir (TOCTOU); sınırı
	// OKUNAN boyut üzerinde de uygula.
	if int64(len(ham)) > LogoMaks {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "gorsel", fi.ModTime(), bytes.NewReader(ham))
}

// DizinHazirla — marka dizinini var eder.
//
// 🔴 BU İŞİ CORE YAPAR, EKLENTİ DEĞİL. Eklenti unit'i ProtectSystem=strict +
// ReadWritePaths ile koşuyor; dizin yoksa systemd namespace'i KURAMAZ ve
// servis, ikili hiç çalışmadan "Failed at step NAMESPACE" ile ölür
// (ExecStartPre de aynı namespace'e tabi olduğu için orada da çözülemez).
// Core kısıtsız koştuğu için dizini o oluşturur; eklenti sertleştirmesini
// gevşetmeye gerek kalmaz.
func DizinHazirla() error {
	return os.MkdirAll(Kok, 0o755)
}

func tipOku(ad string) string {
	ham, err := os.ReadFile(filepath.Join(Kok, ad))
	if err != nil {
		return ""
	}
	return metinKirp(string(ham), 40)
}
