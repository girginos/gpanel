//go:build windows

// site_yonetim_windows.go — IIS SITE/UYGULAMA yonetimi (appcmd tabanli).
//
// windows.go site YASAM DONGUSUNU (ac/sil/sina) tutar; bu dosya var olan bir
// sitenin GUNLUK OPERASYONUNU tutar: detay okuma, uygulama havuzu islemleri,
// baglama ekleme/silme, tek tikla SSL (win-acme). Tumu appcmd.exe uzerinden.
//
// 🔴 NEDEN appcmd metnine guveniyoruz: appcmd'nin `list ...` ciktisi TEK
// SATIRLIK ve YERELLESMEZ bir bicimdedir (Get-Service/wevtutil'in aksine),
// bu yuzden regexle guvenle ayristirilir — kaliba uymayan satir sessizce atlanir.
// Mutasyon (set/start/stop) tarafinda ise ciktiyi ayristirmayiz; windows.go'daki
// `kos` yardimcisi hatayi (ve appcmd mesajini) oldugu gibi yukari tasir.
package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── disa acik goruntu tipleri (etiketsiz: SiteSonuc/OlayKaydi ile ayni
//    sozlesme bicimi — Go alan adlariyla JSON'a cikar) ─────────────────────────

// BaglamaGoruntu — tek bir IIS baglamasi. SSL ayri bir bayrak DEGIL, yalniz
// protokolden turetilir: protokol "https" ise SSL=true.
type BaglamaGoruntu struct {
	Protokol string
	Port     int
	Host     string
	SSL      bool
}

// HavuzGoruntu — sitenin kok uygulamasinin bagli oldugu uygulama havuzu.
type HavuzGoruntu struct {
	Ad          string
	Durum       string
	DotNetSurum string
}

// SiteDetayGoruntu — GET /api/yerel/site-detay yaniti.
type SiteDetayGoruntu struct {
	Ad          string
	Durum       string
	FizikselYol string
	Havuz       HavuzGoruntu
	Baglamalar  []BaglamaGoruntu
}

// ── appcmd cikti kaliplari (YERELLESMEZ tek satir bicimi) ────────────────────
var (
	// SITE "alan.com" (id:2,bindings:http/*:80:alan.com,state:Started)
	siteDetaySatirRe = regexp.MustCompile(`^SITE "(.+)" \(id:(\d+),bindings:(.*),state:(\w+)\)$`)
	// APPPOOL "gp_x" (MgdVersion:v4.0,MgdMode:Integrated,state:Started)
	havuzSatirRe = regexp.MustCompile(`^APPPOOL "(.+)" \(MgdVersion:(.*),MgdMode:(.*),state:(\w+)\)$`)
	// APP "alan.com/" (applicationPool:gp_x)
	appSatirRe = regexp.MustCompile(`^APP "(.+)" \(applicationPool:(.*)\)$`)
	// VDIR "alan.com/" (physicalPath:C:\inetpub\gpanel\gp_x\httpdocs)
	vdirSatirRe = regexp.MustCompile(`^VDIR "(.+)" \(physicalPath:(.*)\)$`)
)

// komutCikti — komutu kosar, stdout+stderr birlesik metnini dondurur.
// windows.go'daki `kos` ciktiyi ATAR (yalniz hata dondurur); appcmd `list`
// ciktisini AYRISTIRMAK icin metne ihtiyac var, o yuzden ayri yardimci.
func komutCikti(ad string, arg ...string) (string, error) {
	out, err := exec.Command(ad, arg...).CombinedOutput()
	return string(out), err
}

// SiteGoruntu — GET /api/yerel/siteler yanitindaki tek satir.
// 🔴 ETIKETLI (json:"ad"...) — istisna: webui bu ucu {ad,durum,baglama}
// (kucuk harf) ile okuyor; sozlesmeyi bozmamak icin etiketler KORUNUR.
type SiteGoruntu struct {
	Ad      string `json:"ad"`
	Durum   string `json:"durum"`
	Baglama string `json:"baglama"`
}

// siteSatirlariCoz — `appcmd list site` ciktisini ayristirir. SAF (exec yok):
// birim testi bunu dogrudan sinar. siteDetaySatirRe yeniden kullanilir
// (grup 1=ad, 2=id, 3=baglama, 4=durum); kaliba uymayan satir sessizce atlanir.
func siteSatirlariCoz(out string) []SiteGoruntu {
	siteler := make([]SiteGoruntu, 0, 8)
	for _, satir := range strings.Split(out, "\n") {
		m := siteDetaySatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r"))
		if m == nil {
			continue
		}
		siteler = append(siteler, SiteGoruntu{Ad: m[1], Durum: m[4], Baglama: m[3]})
	}
	return siteler
}

// SiteListe — IIS site listesini (ad/durum/baglama) dondurur. Ayristirma PLATFORM
// katindadir; yerel panel artik ham appcmd'ye uzanmaz ve appcmd yolunu
// KOPYALAMAZ (denetim 04/4: katman ihlali + kopya-yol drift'i giderildi).
func SiteListe() ([]SiteGoruntu, error) {
	out, err := komutCikti(appcmdYolu(), "list", "site")
	if err != nil {
		return nil, fmt.Errorf("site listesi alinamadi: %v — %s", err, strings.TrimSpace(out))
	}
	return siteSatirlariCoz(out), nil
}

// SiteDetay — tek bir sitenin durum + fiziksel yol + havuz + baglama detayini
// dort appcmd sorgusuyla toplar. Alan adi bicimi alanAdiRe ile dogrulanir;
// bicim hatasi ErrGecersizIstek sarar (cagiran 400'e cevirir), bulunamayan
// site islem hatasidir (cagiran 500'e cevirir).
func SiteDetay(alan string) (SiteDetayGoruntu, error) {
	alan = strings.ToLower(strings.TrimSpace(alan))
	if !alanAdiRe.MatchString(alan) {
		return SiteDetayGoruntu{}, fmt.Errorf("gecersiz alan adi: %q: %w", alan, ErrGecersizIstek)
	}
	appcmd := appcmdYolu()

	// 1) SITE: durum + ham baglama listesi (+ id, SSL tarafinda degil burada
	//    kullanilmasa da kaliptan gelir).
	var d SiteDetayGoruntu
	siteCikti, cerr := komutCikti(appcmd, "list", "site", "/name:"+alan)
	bulundu := false
	for _, satir := range strings.Split(siteCikti, "\n") {
		m := siteDetaySatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r"))
		if m == nil {
			continue
		}
		d.Ad = m[1]
		d.Durum = m[4]
		d.Baglamalar = baglamalariCoz(m[3])
		bulundu = true
		break
	}
	if !bulundu {
		if cerr != nil {
			return SiteDetayGoruntu{}, fmt.Errorf("site sorgulanamadi (%q): %v — %s", alan, cerr, strings.TrimSpace(siteCikti))
		}
		return SiteDetayGoruntu{}, fmt.Errorf("site bulunamadi: %q", alan)
	}

	// 2) APP: kok uygulamanin bagli oldugu havuz adi.
	if appCikti, _ := komutCikti(appcmd, "list", "app", "/site.name:"+alan); appCikti != "" {
		for _, satir := range strings.Split(appCikti, "\n") {
			if m := appSatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r")); m != nil {
				d.Havuz.Ad = m[2]
				break
			}
		}
	}

	// 3) VDIR: kok sanal dizinin fiziksel yolu.
	if vdirCikti, _ := komutCikti(appcmd, "list", "vdir", "/app.name:"+alan+"/"); vdirCikti != "" {
		for _, satir := range strings.Split(vdirCikti, "\n") {
			if m := vdirSatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r")); m != nil {
				d.FizikselYol = m[2]
				break
			}
		}
	}

	// 4) APPPOOL: havuz durumu + yonetilen .NET surumu (havuz adi cozulduyse).
	//    Havuz KONUMSAL kimlikle sorgulanir: `list site /name:` windows.go'da
	//    kanitli ama `list apppool` icin /name: vs /apppool.name: belirsiz;
	//    konumsal kimlik her appcmd nesne turunde calisir, riski keser.
	if d.Havuz.Ad != "" {
		if hCikti, _ := komutCikti(appcmd, "list", "apppool", d.Havuz.Ad); hCikti != "" {
			for _, satir := range strings.Split(hCikti, "\n") {
				if m := havuzSatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r")); m != nil {
					d.Havuz.DotNetSurum = m[2]
					d.Havuz.Durum = m[4]
					break
				}
			}
		}
	}
	return d, nil
}

// baglamalariCoz — appcmd bindings alanini ("http/*:80:host,https/*:443:host")
// yapisal listeye cevirir. Her parca "protokol/ip:port:host" bicimindedir.
func baglamalariCoz(ham string) []BaglamaGoruntu {
	ham = strings.TrimSpace(ham)
	if ham == "" {
		return nil
	}
	var liste []BaglamaGoruntu
	for _, parca := range strings.Split(ham, ",") {
		parca = strings.TrimSpace(parca)
		if parca == "" {
			continue
		}
		egik := strings.IndexByte(parca, '/') // protokol / geri-kalan
		if egik < 0 {
			continue
		}
		b := BaglamaGoruntu{
			Protokol: parca[:egik],
			SSL:      strings.EqualFold(parca[:egik], "https"),
		}
		// geri = "ip:port:host" — bastan alan alan cozulur (host bos olabilir).
		alanlar := strings.SplitN(parca[egik+1:], ":", 3)
		if len(alanlar) == 3 {
			b.Port, _ = strconv.Atoi(alanlar[1])
			b.Host = alanlar[2]
		}
		liste = append(liste, b)
	}
	return liste
}

// ── uygulama havuzu islemleri ────────────────────────────────────────────────

// HavuzIslem — uygulama havuzunu baslatir/durdurur/geri donusturur.
// islem BEYAZ LISTEDEN olmak ZORUNDA: serbest metni appcmd fiiline cevirmek
// keyfi komut yuzeyi acardi; liste disi deger ErrGecersizIstek dondurur.
// havuzAdiRe — yonetilebilir havuz adi: yalniz panelin urettigi gp_ havuzlari
// (sistemKullanici bicimi: gp_ + <=8 govde + 8 hex). Sistem havuzlarina
// (DefaultAppPool vb.) dokunmayi VE enjeksiyon karakterlerini engeller —
// HavuzIslem/HavuzDotNet adi appcmd'ye gecirir (denetim 03/6: tutarli disiplin).
var havuzAdiRe = regexp.MustCompile(`^gp_[a-z0-9]{1,20}$`)

// havuzAdiDogrula — havuz adini kirpar + desene gore dogrular; bos/gecersiz ise
// ErrGecersizIstek (cagiran 400'e cevirir).
func havuzAdiDogrula(havuz string) (string, error) {
	havuz = strings.TrimSpace(havuz)
	if !havuzAdiRe.MatchString(havuz) {
		return "", fmt.Errorf("gecersiz havuz adi %q (gp_ ile baslar, harf/rakam, 1-20): %w", havuz, ErrGecersizIstek)
	}
	return havuz, nil
}

func HavuzIslem(havuz, islem string) error {
	havuz, err := havuzAdiDogrula(havuz)
	if err != nil {
		return err
	}
	var fiil string
	switch islem {
	case "baslat":
		fiil = "start"
	case "durdur":
		fiil = "stop"
	case "geridonustur":
		fiil = "recycle"
	default:
		return fmt.Errorf("gecersiz islem %q (baslat|durdur|geridonustur): %w", islem, ErrGecersizIstek)
	}
	return kos(appcmdYolu(), fiil, "apppool", "/apppool.name:"+havuz)
}

// HavuzDotNet — havuzun yonetilen .NET surumunu ayarlar. YALNIZ uc deger
// gecerli: "v4.0", "v2.0", "" (bos = No Managed Code). Baska her deger reddedilir
// — appcmd'ye serbest surum dizgesi gecirmek yapilandirmayi sessizce bozardi.
func HavuzDotNet(havuz, surum string) error {
	havuz, err := havuzAdiDogrula(havuz)
	if err != nil {
		return err
	}
	switch surum {
	case "v4.0", "v2.0", "":
	default:
		return fmt.Errorf("gecersiz .NET surumu %q (v4.0|v2.0|bos): %w", surum, ErrGecersizIstek)
	}
	return kos(appcmdYolu(), "set", "apppool", havuz, "/managedRuntimeVersion:"+surum)
}

// ── baglama (binding) ekleme/silme ───────────────────────────────────────────

// baglamaBileseniDogrula — protokol/port/host uclusunu dogrular ve normalize
// eder. Ekle ve sil AYNI dogrulamayi paylasir (tek kaynak).
func baglamaBileseniDogrula(protokol, port, host string) (BaglamaGoruntu, error) {
	var b BaglamaGoruntu
	switch protokol {
	case "http", "https":
		b.Protokol = protokol
		b.SSL = protokol == "https"
	default:
		return b, fmt.Errorf("gecersiz protokol %q (http|https): %w", protokol, ErrGecersizIstek)
	}
	p, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || p < 1 || p > 65535 {
		return b, fmt.Errorf("gecersiz port %q (1-65535): %w", port, ErrGecersizIstek)
	}
	b.Port = p
	host = strings.ToLower(strings.TrimSpace(host))
	if host != "" && !alanAdiRe.MatchString(host) {
		return b, fmt.Errorf("gecersiz host %q: %w", host, ErrGecersizIstek)
	}
	b.Host = host
	return b, nil
}

// BaglamaEkle — siteye yeni bir baglama ekler (appcmd /+bindings.[...]).
func BaglamaEkle(site, protokol, port, host string) error {
	site = strings.ToLower(strings.TrimSpace(site))
	if !alanAdiRe.MatchString(site) {
		return fmt.Errorf("gecersiz site adi: %q: %w", site, ErrGecersizIstek)
	}
	b, err := baglamaBileseniDogrula(protokol, port, host)
	if err != nil {
		return err
	}
	return kos(appcmdYolu(), "set", "site", "/site.name:"+site,
		fmt.Sprintf("/+bindings.[protocol='%s',bindingInformation='*:%d:%s']", b.Protokol, b.Port, b.Host))
}

// BaglamaSil — siteden bir baglamayi kaldirir (appcmd /-bindings.[...]).
func BaglamaSil(site, protokol, port, host string) error {
	site = strings.ToLower(strings.TrimSpace(site))
	if !alanAdiRe.MatchString(site) {
		return fmt.Errorf("gecersiz site adi: %q: %w", site, ErrGecersizIstek)
	}
	b, err := baglamaBileseniDogrula(protokol, port, host)
	if err != nil {
		return err
	}
	return kos(appcmdYolu(), "set", "site", "/site.name:"+site,
		fmt.Sprintf("/-bindings.[protocol='%s',bindingInformation='*:%d:%s']", b.Protokol, b.Port, b.Host))
}

// ── SSL (win-acme / Let's Encrypt) ───────────────────────────────────────────

const (
	// wacsYolu — SSL katalog kalemi win-acme'yi bu sabit yola acar (bkz.
	// kurulum_windows.go winAcmeDizin). Yoksa hic denemeyiz.
	wacsYolu = `C:\Program Files\GirginOSPanel\win-acme\wacs.exe`

	// leYoneticiEposta — ACME kaydi icin SABIT yonetici adresi. Bilerek
	// "localhost" DEGIL: Let's Encrypt gecersiz/localhost adresi reddeder.
	leYoneticiEposta = "yonetici@girginos.io"

	// leZamanAsimi — wacs bir ACME dogrulamasi + IIS kurulumu yapar; 180 sn
	// gercek bir alanda fazlasiyla yeter, takilirsa sonsuza dek beklemez.
	leZamanAsimi = 180 * time.Second

	// leDurustlukNotu — cagirana AYNEN eklenir. 🔴 DURUSTLUK: LE gercek DNS +
	// disaridan 80/443 erisimi ister; .invalid/ozel ag test hostunda bu komut
	// BASARISIZ olur ve bu BEKLENEN durumdur — mesaj bunu acikca soyler.
	leDurustlukNotu = "not: sertifika ancak gercek alan adi + disaridan 80/443 erisimiyle alinir; " +
		".invalid veya ozel ag test hostunda bu komut BASARISIZ olur (beklenen)."
)

// SiteSSLLetsEncrypt — site icin win-acme'yi IIS moduyla kosar ve ciktisini
// (basari da hata da) AYNEN + durustluk notuyla dondurur.
//
// 🔴 SOZLESME: on kosul hatalari (gecersiz ad, wacs kurulu degil, site
// bulunamadi) HATA dondurur (cagiran 422/400'e cevirir). wacs GERCEKTEN
// kosulduysa — cikis kodu ne olursa olsun — nil hata + ciktiyi dondururuz:
// test hostunda basarisizlik beklendiginden onu sert hataya cevirmeyiz, cikti
// zaten gercegi anlatir. Boylece uc "200 {mesaj}" ile durust sonucu gosterir.
func SiteSSLLetsEncrypt(site string) (string, error) {
	site = strings.ToLower(strings.TrimSpace(site))
	if !alanAdiRe.MatchString(site) {
		return "", fmt.Errorf("gecersiz site adi: %q: %w", site, ErrGecersizIstek)
	}
	if _, err := os.Stat(wacsYolu); err != nil {
		return "", fmt.Errorf("win-acme bulunamadi (%s) — once katalogdan SSL/win-acme kurun", wacsYolu)
	}
	// wacs --siteid SAYISAL id ister; site adindan cozulur.
	id, err := siteIdCoz(site)
	if err != nil {
		return "", err
	}

	ctx, iptal := context.WithTimeout(context.Background(), leZamanAsimi)
	defer iptal()
	out, cerr := exec.CommandContext(ctx, wacsYolu,
		"--target", "iis",
		"--siteid", id,
		"--installation", "iis",
		"--accepttos",
		"--emailaddress", leYoneticiEposta,
	).CombinedOutput()

	metin := strings.TrimSpace(string(out))
	// 🔴 200-içi-başarısızlık DÜZELTİLDİ: wacs zaman aşımı VEYA hata ile çıkarsa
	// bu bir BAŞARISIZLIKTIR — err DÖNER (çağıran 422 gösterir), 200+gizli-hata
	// DEĞİL. Dürüst wacs çıktısı hata mesajında KORUNUR. Yalnız wacs GERÇEKTEN
	// başarılıysa (çıkış 0, zaman aşımı yok) 200 + dürüstlük notu döner.
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("SSL zaman aşımı (180 sn doldu, wacs durduruldu):\n%s", metin)
	}
	if cerr != nil {
		return "", fmt.Errorf("SSL sertifikası alınamadı (wacs hata: %s):\n%s", cerr.Error(), metin)
	}
	if metin != "" {
		metin += "\n\n"
	}
	return metin + leDurustlukNotu, nil
}

// siteIdCoz — site adindan IIS sayisal site id'sini cozer (wacs --siteid icin).
func siteIdCoz(site string) (string, error) {
	cikti, _ := komutCikti(appcmdYolu(), "list", "site", "/name:"+site)
	for _, satir := range strings.Split(cikti, "\n") {
		if m := siteDetaySatirRe.FindStringSubmatch(strings.TrimRight(satir, "\r")); m != nil {
			return m[2], nil
		}
	}
	return "", fmt.Errorf("site bulunamadi: %q — %s", site, strings.TrimSpace(cikti))
}
