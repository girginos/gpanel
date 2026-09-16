//go:build windows

package platform

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// windowsSaglayici — IIS + NTFS uzerinde site yasam dongusu.
//
// 🔴 YETENEK KAPISI: YetSite bayragi koddan ACIK gelmez. Ancak bu makinede
// `girginospanel-agent kendini-sina` KOSUP GECINCE acilir (asagidaki muhur
// dosyasi). Boylece "yetenek ancak dogrulaninca ilan edilir" kurali kagit
// uzerinde degil, MAKINE BASINA mekanik olarak uygulanir: sinanmamis hosta
// site istegi dusmez.
//
// SSLVer bilerek hala ErrDesteklenmiyor: win-acme entegrasyonu ayri adim.
type windowsSaglayici struct{}

var aktifSaglayici Saglayici = windowsSaglayici{}

// sinaMuhruYolu — kendini-sina'nin bastigi muhur. Icinde gecen surum tutulur;
// ajan guncellenince muhur ESKI surume ait kalir ve YetSite yine kapanir:
// her yeni ajan surumu kendini yeniden kanitlamak zorundadir.
const sinaMuhruYolu = `C:\ProgramData\girginospanel\kendini-sina.ok`

const kokDizin = `C:\inetpub\gpanel`

func (windowsSaglayici) Ad() string { return "windows" }

func (windowsSaglayici) Yetenekler() Yetenek {
	var y Yetenek
	// 🔴 Muhur mantigi AYNEN korunur: YetSite yalniz kendini-sina muhru BU
	// surumle eslesirse acilir — kesif genisledi diye site kapisi GEVSEMEZ.
	b, err := os.ReadFile(sinaMuhruYolu)
	if err == nil && strings.TrimSpace(string(b)) == Surum {
		y |= YetSite
	}
	// Olay gunlugu + zamanli gorevler OS-YERLESIKTIR (her Windows'ta var,
	// IIS'ten bagimsiz) — kosulsuz ilan edilir, muhur gerektirmez.
	y |= YetOlayGunlugu | YetZamanliGorev
	return y | algilananYetenekler()
}

// ── yetenek kesfi (servis + dizin tabanli) ──────────────────────────────────

// yetenekOnbellegi — algilananYetenekler sonucunun 60 saniyelik onbellegi.
//
// 🔴 NEDEN ONBELLEK: panel /saglik'i 5 saniyede bir yoklayabilir ve /saglik
// her seferinde Yetenekler()'i cagirir. Her cagrida yeni bir powershell.exe
// acmak surec firtinasi demektir (cagri basina ~1-2 sn + cocuk surec).
// 60 sn bayatlik zararsizdir: sunucuya servis kurmak zaten dakikalar
// suren bir istir, kesif bir sonraki pencerede kendiliginden yakalar.
var yetenekOnbellegi struct {
	sync.Mutex
	deger Yetenek
	zaman time.Time
}

const yetenekOnbellekOmru = 60 * time.Second

// algilananYetenekler — kurulu servislerden ve diskteki izlerden yetenek
// cikarir; sonuc yetenekOnbellegi'nde tutulur (gerekce yukarida).
func algilananYetenekler() Yetenek {
	yetenekOnbellegi.Lock()
	defer yetenekOnbellegi.Unlock()
	if !yetenekOnbellegi.zaman.IsZero() && time.Since(yetenekOnbellegi.zaman) < yetenekOnbellekOmru {
		return yetenekOnbellegi.deger
	}

	var y Yetenek
	// Tum aday servisler TEK PowerShell cagrisiyla sorgulanir. 🔴 ConvertTo-Json
	// sart: Get-Service'in tablo ciktisi YERELLESIR (Turkce kurulumda basliklar
	// ve durum metni degisir), JSON alan adlari degismez. Argumanlar exec.Command'e
	// AYRI AYRI verilir; kabuk birlestirme yok.
	// 🔴 `; exit 0` SART: listede var olmayan KESIN adlar (MSSQLSERVER vb.)
	// SilentlyContinue ile bastirilsa da powershell -Command cikis kodunu 1
	// yapar; Output() bunu hata sayip GECERLI JSON'u cope atiyordu. Gercek
	// VM'de SYSTEM gorev sondasiyla yakalandi (T2=1, cikti dogruydu) —
	// FTP kurulu oldugu halde yetenek biti yanmiyordu (2026-09-03).
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-Service -Name 'MSSQLSERVER','MSSQL$*','ftpsvc','DNS','MySQL*','postgresql*' -ErrorAction SilentlyContinue | Select-Object -Property Name | ConvertTo-Json -Compress; exit 0`,
	).Output()
	if err == nil {
		for _, ad := range servisAdlari(out) {
			k := strings.ToLower(ad)
			switch {
			case strings.HasPrefix(k, "mssql"): // MSSQLSERVER + MSSQL$ORNEK
				y |= YetMSSQL
			case k == "ftpsvc":
				y |= YetFTP
			case k == "dns":
				y |= YetDNS
			case strings.HasPrefix(k, "mysql"):
				y |= YetMySQL
			case strings.HasPrefix(k, "postgresql"):
				y |= YetPgSQL
			}
		}
	}
	// ASP.NET Core Hosting Bundle IIS modulunu bu sabit yola kurar; dizinin
	// varligi bundle'in guvenilir isaretidir (kayit defteri taramaya gerek yok).
	if fi, err := os.Stat(`C:\Program Files\IIS\Asp.Net Core Module\V2`); err == nil && fi.IsDir() {
		y |= YetDotNet
	}

	// 🔴 Hata durumunda da onbellege yazilir: PowerShell bozuksa 5 sn'de bir
	// yeniden denemek ayni surec firtinasini geri getirirdi.
	yetenekOnbellegi.deger = y
	yetenekOnbellegi.zaman = time.Now()
	return y
}

// servisAdlari — ConvertTo-Json ciktisindan Name alanlarini toplar.
// 🔴 TEK OGE TUZAGI: ConvertTo-Json tek sonucta dizi DEGIL nesne dondurur
// ({"Name":"DNS"}), birden cokta dizi. Ilk bayta bakip ikisini de kabul ederiz.
func servisAdlari(out []byte) []string {
	ham := bytes.TrimSpace(out)
	if len(ham) == 0 {
		return nil // hicbir aday servis kurulu degil
	}
	type kayit struct{ Name string }
	if ham[0] == '{' {
		var tek kayit
		if json.Unmarshal(ham, &tek) == nil && tek.Name != "" {
			return []string{tek.Name}
		}
		return nil
	}
	var liste []kayit
	if json.Unmarshal(ham, &liste) != nil {
		return nil
	}
	adlar := make([]string, 0, len(liste))
	for _, k := range liste {
		adlar = append(adlar, k.Name)
	}
	return adlar
}

// Dogrula — ortam bu saglayici icin hazir mi. Sessiz devam YOK: eksik ne
// varsa tek tek soylenir ki operator IIS kurmadan ajani ayakta sanmasin.
func (windowsSaglayici) Dogrula() error {
	var eksik []string
	if _, err := os.Stat(appcmdYolu()); err != nil {
		eksik = append(eksik, "IIS bulunamadi ("+appcmdYolu()+") — 'Web Server (IIS)' rolu kurulu olmali")
	}
	if _, err := exec.LookPath("icacls"); err != nil {
		eksik = append(eksik, "icacls PATH'te yok")
	}
	// Klasik yonetici testi: `net session` yalniz yukseltilmis oturumda basarilir.
	if err := exec.Command("net", "session").Run(); err != nil {
		eksik = append(eksik, "ajan yonetici (elevated) calismiyor — servis olarak LocalSystem ile kurun")
	}
	if len(eksik) > 0 {
		return fmt.Errorf("ortam hazir degil: %s", strings.Join(eksik, "; "))
	}
	return nil
}

func appcmdYolu() string {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	return filepath.Join(win, "System32", "inetsrv", "appcmd.exe")
}

var alanAdiRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// sistemKullanici — alan adindan Windows yerel kullanicisi turetir.
// 🔴 SAM hesap adi EN COK 20 karakter; bu Linux'taki gibi uzun olamaz.
// Bicim: "gp_" (3) + govde (<=8) + FNV-1a'nin TAM 32 biti (8 hex) = <=19 <= 20.
//
// 🔴 CAKISMA: govde kirpildigi icin ayni onEki paylasan alanlar ayirt edici
// olarak yalniz hash'e dayanir. Eski surum 16 bit (65 536 kova) kullaniyordu →
// ayni onEkte ~256 alanda %50 cakisma, saldirganca saniyeler icinde zorlanabilir
// idi (denetim 03/1). Tam 32 bite cikarildi (~4.29 milyar kova). Cakisma tek
// basina yeterli savunma DEGIL: siteOlusturCekirdek ayrica sahip-isareti ile
// dizin devralmayi ACIK reddeder (bkz. .gpanel-sahip) — hash cakissa bile baska
// kiracinin webRoot'u sessizce servis edilmez.
func sistemKullanici(alanAdi string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(alanAdi))
	govde := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, strings.ToLower(alanAdi))
	if len(govde) > 8 {
		govde = govde[:8]
	}
	return fmt.Sprintf("gp_%s%08x", govde, h.Sum32())
}

// sahipIsaretYolu — kiraci dizininin hangi alana ait oldugunu tutan isaret.
// httpdocs'un DISINDA (kiraci kokunde) durur → web'den servis edilmez.
func sahipIsaretYolu(sk string) string {
	return filepath.Join(kokDizin, sk, ".gpanel-sahip")
}

func rasgeleParola() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Windows parola politikasi karmasiklik ister; buyuk/kucuk/rakam/simge var.
	return "Gp!" + hex.EncodeToString(b) + "Z", nil
}

func kos(ad string, arg ...string) error {
	out, err := exec.Command(ad, arg...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v — %s", ad, strings.Join(arg, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (w windowsSaglayici) SiteOlustur(ist SiteIstek) (SiteSonuc, error) {
	if !w.Yetenekler().Var(YetSite) {
		return SiteSonuc{}, fmt.Errorf("SiteOlustur: bu host sinanmadi (once `girginospanel-agent kendini-sina` kosun): %w", ErrDesteklenmiyor)
	}
	return siteOlusturCekirdek(ist)
}

// siteOlusturCekirdek — yetenek kapisindan BAGIMSIZ cekirdek; kendini-sina da
// AYNI yolu kosar. Sinama ozel bir kisayolu degil gercek uretim yolunu
// dogrulamali, yoksa muhur hicbir sey kanitlamaz.
func siteOlusturCekirdek(ist SiteIstek) (SiteSonuc, error) {
	alan := strings.ToLower(strings.TrimSpace(ist.AlanAdi))
	if !alanAdiRe.MatchString(alan) {
		return SiteSonuc{}, fmt.Errorf("gecersiz alan adi: %q", ist.AlanAdi)
	}
	sk := sistemKullanici(alan)
	webRoot := filepath.Join(kokDizin, sk, "httpdocs")
	sahipDosya := sahipIsaretYolu(sk)
	appcmd := appcmdYolu()

	// 🔴 CAPRAZ-KIRACI SIZINTI KAPISI (denetim 03/1): SiteSil webRoot'u BILEREK
	// korur (musteri verisi). Silinmis alan A'nin dizini kalir; hash cakismasi
	// nedeniyle farkli alan B ayni sk'ye duserse, dizini SESSIZCE devralip A'nin
	// dosyalarini B'ye servis etmek bir veri sizintisidir. Sahip isareti BASKA
	// alani gosteriyorsa ACIK reddet — devralma yok.
	if b, err := os.ReadFile(sahipDosya); err == nil {
		if mevcut := strings.TrimSpace(string(b)); mevcut != "" && mevcut != alan {
			return SiteSonuc{}, fmt.Errorf("kullanici adi cakismasi: %s dizini zaten %q alanina ait (sk=%s) — bu alan farkli bir kullaniciya esleyemedi, lutfen bildirin", filepath.Dir(webRoot), mevcut, sk)
		}
	}

	// 🔴 GERI ALMA: adimlardan biri patlarsa oncekiler temizlenir. Yarim site
	// birakmak (kullanici var, site yok) sonraki denemeyi de kilitler.
	var geriAl []func()
	temizle := func() {
		for i := len(geriAl) - 1; i >= 0; i-- {
			geriAl[i]()
		}
	}

	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		return SiteSonuc{}, fmt.Errorf("webroot acilamadi: %w", err)
	}
	// Sahip isaretini yaz: bu dizinin sahibi bu alandir. Cakisan bir yeniden
	// kullanim girisimi yukaridaki kapiyla reddedilecek. (Ayni alanin yeniden
	// olusturmasi isareti eslesir, sorunsuz devam eder — idempotent.)
	if err := os.WriteFile(sahipDosya, []byte(alan+"\n"), 0o644); err != nil {
		return SiteSonuc{}, fmt.Errorf("sahip isareti yazilamadi: %w", err)
	}

	parola, err := rasgeleParola()
	if err != nil {
		return SiteSonuc{}, err
	}
	if err := kos("net", "user", sk, parola, "/add", "/expires:never", "/y"); err != nil {
		return SiteSonuc{}, fmt.Errorf("yerel kullanici: %w", err)
	}
	geriAl = append(geriAl, func() { _ = kos("net", "user", sk, "/delete") })

	// 🔴 SIRA ONEMLI: once HAVUZ, sonra ACL. `IIS AppPool\<ad>` sanal hesabi
	// havuz OLUSTURULMADAN var olmaz; ACL once denenirse icacls 1332 doner
	// ("No mapping between account names and security IDs") — bu hatayi ilk
	// gercek Windows Server sinamasi yakaladi (2026-09-03).
	if err := kos(appcmd, "add", "apppool", "/name:"+sk); err != nil {
		temizle()
		return SiteSonuc{}, fmt.Errorf("uygulama havuzu: %w", err)
	}
	geriAl = append(geriAl, func() { _ = kos(appcmd, "delete", "apppool", "/apppool.name:"+sk) })

	// NTFS: site kullanicisina Degistir (FTP/dagitim icin), uygulama havuzu
	// kimligine OkuCalistir.
	if err := kos("icacls", webRoot, "/inheritance:e",
		"/grant", sk+":(OI)(CI)M",
		"/grant", `IIS AppPool\`+sk+":(OI)(CI)RX"); err != nil {
		temizle()
		return SiteSonuc{}, fmt.Errorf("NTFS ACL: %w", err)
	}

	if err := kos(appcmd, "add", "site", "/name:"+alan,
		"/bindings:http/*:80:"+alan, "/physicalPath:"+webRoot); err != nil {
		temizle()
		return SiteSonuc{}, fmt.Errorf("IIS site: %w", err)
	}
	geriAl = append(geriAl, func() { _ = kos(appcmd, "delete", "site", "/site.name:"+alan) })

	if err := kos(appcmd, "set", "app", alan+"/", "/applicationPool:"+sk); err != nil {
		temizle()
		return SiteSonuc{}, fmt.Errorf("havuz baglama: %w", err)
	}

	return SiteSonuc{
		SistemKullanici: sk,
		WebRoot:         webRoot,
		FTPHost:         alan,
		// Windows'ta PHP surum secimi henuz yok (YetPHPSurumSecimi kapali);
		// istegi YANKILAMAYIZ — alan bos kalir, gercegi soyler.
		PHPSurum:  "",
		PHPSocket: "",
	}, nil
}

// SiteSil — IIS nesneleri + kullanici silinir; WEBROOT BILEREK KALIR.
// Musteri dosyasini otomatik silmek geri donusu olmayan tek adimdir; dosya
// temizligi operatorun acik karariyla yapilir (Linux'taki yedek-koruma
// disipliniyle ayni gerekce).
func (w windowsSaglayici) SiteSil(k SiteKimlik) error {
	if !w.Yetenekler().Var(YetSite) {
		return fmt.Errorf("SiteSil: bu host sinanmadi: %w", ErrDesteklenmiyor)
	}
	return siteSilCekirdek(k)
}

func siteSilCekirdek(k SiteKimlik) error {
	alan := strings.ToLower(strings.TrimSpace(k.AlanAdi))
	if !alanAdiRe.MatchString(alan) {
		return fmt.Errorf("gecersiz alan adi: %q", k.AlanAdi)
	}
	appcmd := appcmdYolu()
	// 🔴 sk COZUMU (denetim 03/5 — algoritma driftini onle): once cagirandan;
	// yoksa IIS'e bagli GERCEK havuz adindan; o da yoksa son care yeniden hesap.
	// sistemKullanici surumler arasi degisirse yeniden hesap yanlis sk uretip
	// gercek kullaniciyi orphan birakabilirdi; IIS'ten okumak bunu onler.
	sk := k.SistemKullanici
	if sk == "" {
		sk = havuzAdiIISten(alan)
	}
	if sk == "" {
		sk = sistemKullanici(alan)
	}
	var hatalar []string
	if err := kos(appcmd, "delete", "site", "/site.name:"+alan); err != nil {
		hatalar = append(hatalar, err.Error())
	}
	// 🔴 ACL SOKUMU (denetim 03/4): webRoot BILEREK kalir (musteri verisi) ama
	// silinen kullanicinin/pool'un ACE'leri korunan dizinde YETIM SID olarak
	// birikmesin ve cakisan bir yeniden kullanimda eski haklar TASINMASIN.
	// SIRA onemli: site SILINDIKTEN sonra (canli 403 penceresi yok) ama pool+user
	// SILINMEDEN once (adlar hala cozulur) sok. best-effort.
	webRoot := filepath.Join(kokDizin, sk, "httpdocs")
	if _, err := os.Stat(webRoot); err == nil {
		_ = kos("icacls", webRoot, "/remove:g", sk, "/remove:g", `IIS AppPool\`+sk, "/T", "/C", "/Q")
	}
	if err := kos(appcmd, "delete", "apppool", "/apppool.name:"+sk); err != nil {
		hatalar = append(hatalar, err.Error())
	}
	if err := kos("net", "user", sk, "/delete"); err != nil {
		hatalar = append(hatalar, err.Error())
	}
	if len(hatalar) > 0 {
		return fmt.Errorf("kismi silme: %s", strings.Join(hatalar, " | "))
	}
	return nil
}

// havuzAdiIISten — alana bagli IIS uygulama havuzunun adini (= sistem kullanicisi
// sk) appcmd'den okur. Silme boylece sistemKullanici algoritmasi degisse bile
// GERCEK kullaniciyi bulur. Bulunamazsa "" (cagiran son care yeniden hesaba duser).
func havuzAdiIISten(alan string) string {
	out, err := exec.Command(appcmdYolu(), "list", "app", alan+"/", "/text:applicationPool").Output()
	if err != nil {
		return ""
	}
	sk := strings.TrimSpace(string(out))
	// Beklenen bicim gp_...; beklenmeyen cikti (bos/coklu satir) reddedilir.
	if strings.HasPrefix(sk, "gp_") && !strings.ContainsAny(sk, " \r\n") {
		return sk
	}
	return ""
}

func (windowsSaglayici) SSLVer(ist SSLIstek) (Sertifika, error) {
	return Sertifika{}, fmt.Errorf("SSLVer(%s): win-acme entegrasyonu sonraki adim: %w", ist.AlanAdi, ErrDesteklenmiyor)
}

// KendiniSina — GERCEK uretim yolunu kosarak hostu kanitlar: sinama sitesi
// acar, appcmd listesinde gorur, siler; gecerse muhru basar. `.invalid` TLD
// bilerek secildi (RFC 2606): sinama alani gercek DNS'e asla cozulmez.
func KendiniSina() error {
	var w windowsSaglayici
	if err := w.Dogrula(); err != nil {
		return err
	}
	suf := make([]byte, 4)
	if _, err := rand.Read(suf); err != nil {
		return err
	}
	alan := "gosp-sina-" + hex.EncodeToString(suf) + ".invalid"
	son, err := siteOlusturCekirdek(SiteIstek{AlanAdi: alan})
	if err != nil {
		return fmt.Errorf("sinama sitesi acilamadi: %w", err)
	}
	out, err := exec.Command(appcmdYolu(), "list", "site", "/name:"+alan).CombinedOutput()
	if err != nil || !strings.Contains(string(out), alan) {
		_ = siteSilCekirdek(SiteKimlik{AlanAdi: alan, SistemKullanici: son.SistemKullanici})
		return fmt.Errorf("site acildi ama appcmd listesinde YOK — IIS durumu tutarsiz: %s", strings.TrimSpace(string(out)))
	}
	if err := siteSilCekirdek(SiteKimlik{AlanAdi: alan, SistemKullanici: son.SistemKullanici}); err != nil {
		return fmt.Errorf("sinama sitesi silinemedi: %w", err)
	}
	_ = os.RemoveAll(filepath.Join(kokDizin, son.SistemKullanici))
	if err := os.MkdirAll(filepath.Dir(sinaMuhruYolu), 0o755); err != nil {
		return err
	}
	return os.WriteFile(sinaMuhruYolu, []byte(Surum+"\n"), 0o644)
}

// ── olay gunlugu (YetOlayGunlugu) ───────────────────────────────────────────
// (ErrGecersizIstek OS-notr platform.go'ya tasindi — B-13.)

// OlayKaydi — /olaylar ucunun tek satiri. JSON'a Go alan adlariyla cikar
// (SiteSonuc ile ayni sozlesme bicimi: etiketsiz).
type OlayKaydi struct {
	Zaman  string // "2006-01-02 15:04:05" yerel saat
	Gunluk string
	Kaynak string // Provider adi
	OlayID int
	Seviye string // "hata" | "uyari" | "bilgi"
	Mesaj  string // en cok 2000 karakter
}

// wevtOlay — wevtutil RenderedXml ciktisindaki tek <Event> dugumu.
// Seviye System/Level'in SAYISAL degerinden turetilir; RenderingInfo/Level
// bilerek KULLANILMAZ, cunku o metin yerellesir ("Error" / "Hata").
type wevtOlay struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     int `xml:"EventID"`
		Level       int `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		Channel string `xml:"Channel"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Deger string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
	RenderingInfo struct {
		Message string `xml:"Message"`
	} `xml:"RenderingInfo"`
}

// OlaylariOku — verilen gunlugun son kayitlarini dondurur (en yeni once).
//
// gunluk yalniz beyaz listedendir: deger dogrudan wevtutil'e arguman gider;
// serbest metin hem arguman enjeksiyonu hem rastgele gunluk okuma kapisi acardi.
func OlaylariOku(gunluk string, adet int) ([]OlayKaydi, error) {
	switch gunluk {
	case "System", "Application", "Security":
	default:
		return nil, fmt.Errorf("gunluk %q taninmiyor (System | Application | Security): %w", gunluk, ErrGecersizIstek)
	}
	if adet < 1 {
		adet = 1
	}
	if adet > 200 {
		adet = 200
	}
	// /rd:true en yeni kayittan geriye okur; /f:RenderedXml mesaji da
	// (RenderingInfo) cozulmus halde getirir.
	out, err := exec.Command("wevtutil", "qe", gunluk,
		"/c:"+strconv.Itoa(adet), "/rd:true", "/f:RenderedXml").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("wevtutil %s: %v — %s", gunluk, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("wevtutil %s: %v", gunluk, err)
	}

	// 🔴 XML TUZAGI: wevtutil TEK KOK ELEMAN VERMEZ — cikti art arda dizilmis
	// <Event>...</Event> dugumleridir. xml.Unmarshal tek belge bekler ve ikinci
	// Event'te "unexpected token" ile PATLAR; bu yuzden xml.Decoder ile token
	// akisi gezilir, her <Event> AYRI decode edilir. ToValidUTF8: yerel kod
	// sayfasindan sizan bozuk baytlar Decoder'i durdurmasin diye isaretlenir.
	olaylar := make([]OlayKaydi, 0, adet)
	d := xml.NewDecoder(strings.NewReader(strings.ToValidUTF8(string(out), "�")))
	for {
		tk, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("olay XML akisi ayristirilamadi: %v", err)
		}
		se, ok := tk.(xml.StartElement)
		if !ok || se.Name.Local != "Event" {
			continue
		}
		var ev wevtOlay
		if err := d.DecodeElement(&ev, &se); err != nil {
			return nil, fmt.Errorf("olay kaydi cozulemedi: %v", err)
		}
		olaylar = append(olaylar, olayKaydinaCevir(gunluk, ev))
	}
	return olaylar, nil
}

// olayKaydinaCevir — ham XML dugumunu panelin kaydina indirger.
func olayKaydinaCevir(gunluk string, ev wevtOlay) OlayKaydi {
	// Windows Level: 1 Critical, 2 Error, 3 Warning, 4 Information, 5 Verbose,
	// 0 LogAlways (Security denetim kayitlari da 0 gelir). Panel uc seviye bilir.
	seviye := "bilgi"
	switch ev.System.Level {
	case 1, 2:
		seviye = "hata"
	case 3:
		seviye = "uyari"
	}

	// SystemTime UTC ISO-8601 gelir ("2026-09-03T10:15:30.1234567Z");
	// cozulemezse ham deger kalir — kaydi atmak bilgiyi atmak olurdu.
	zaman := ev.System.TimeCreated.SystemTime
	if t, err := time.Parse(time.RFC3339Nano, zaman); err == nil {
		zaman = t.Local().Format("2006-01-02 15:04:05")
	}

	mesaj := strings.TrimSpace(ev.RenderingInfo.Message)
	if mesaj == "" {
		// Mesaj DLL'i eksik/cozulememis — EventData parcalarindan derlenir ki
		// satir bos kalmasin.
		var parcalar []string
		for _, d := range ev.EventData.Data {
			if v := strings.TrimSpace(d.Deger); v != "" {
				parcalar = append(parcalar, v)
			}
		}
		mesaj = strings.Join(parcalar, " | ")
	}
	// 2000 KARAKTER siniri rune uzerinden: bayt kirpmak cok baytli karakteri
	// ortadan boler, JSON'a bozuk dizgi sizardi.
	if r := []rune(mesaj); len(r) > 2000 {
		mesaj = string(r[:2000])
	}

	gunlukAdi := ev.System.Channel
	if gunlukAdi == "" {
		gunlukAdi = gunluk
	}
	return OlayKaydi{
		Zaman:  zaman,
		Gunluk: gunlukAdi,
		Kaynak: ev.System.Provider.Name,
		OlayID: ev.System.EventID,
		Seviye: seviye,
		Mesaj:  mesaj,
	}
}

// ── zamanli gorevler (YetZamanliGorev) ──────────────────────────────────────

// GorevKaydi — /gorevler ucunun tek satiri (etiketsiz, Go alan adlariyla).
type GorevKaydi struct {
	Ad       string // "\Klasor\GorevAdi" (TaskPath + TaskName)
	Durum    string // Ready | Running | Disabled ...
	SonKosum string // "yyyy-MM-dd HH:mm:ss"; hic kosmadiysa ""
	Sonraki  string // sonraki planli kosum; yoksa ""
	SonSonuc uint32 // 0 = basari, digerleri Windows hata kodu
}

// gorevUstSiniri — tek yanitta panele tasinacak en cok kayit. Sunucularda
// binlerce gorev olabilir; liste ekrani icin 300 fazlasiyla yeter.
const gorevUstSiniri = 300

// GorevleriOku — Task Scheduler kayitlarini dondurur. tumu=false iken
// \Microsoft\ altindaki yuzlerce isletim sistemi ic gorevi ELENIR (gurultu);
// panelde musteriyi ilgilendiren gorevler kalir.
func GorevleriOku(tumu bool) ([]GorevKaydi, error) {
	// PSCustomObject alan adlari BILEREK Go alan adlariyla birebir secildi:
	// ara esleme katmani gerekmez. Tarih bicimi PowerShell tarafinda sabitlenir
	// ki yerel ayar (tr-TR "03.09.2026") JSON'a sizmasin. Komut icinde yalniz
	// TEK TIRNAK kullanilir ve exec.Command'e AYRI arguman gider — kabuk
	// birlestirme yok.
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-ScheduledTask | ForEach-Object { $i = $_ | Get-ScheduledTaskInfo; [PSCustomObject]@{ Ad = ($_.TaskPath + $_.TaskName); Durum = [string]$_.State; SonKosum = if ($i.LastRunTime) { $i.LastRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }; Sonraki = if ($i.NextRunTime) { $i.NextRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }; SonSonuc = $i.LastTaskResult } } | ConvertTo-Json -Compress; exit 0`,
	).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gorev listesi alinamadi: %v — %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gorev listesi alinamadi: %v", err)
	}

	// 🔴 JSON TUZAGI: ConvertTo-Json TEK ogede dizi degil DUZ NESNE yazar
	// ({...}, [{...}] degil). Ilk bayta bakip iki bicimi de kabul ederiz.
	// Gorev adindaki bozuk baytlar sorun degil: json.Unmarshal gecersiz UTF-8'i
	// hata yapmaz, U+FFFD ile degistirir.
	ham := bytes.TrimSpace(out)
	var kayitlar []GorevKaydi
	switch {
	case len(ham) == 0:
		// hic gorev yok — bos liste, hata degil
	case ham[0] == '{':
		var tek GorevKaydi
		if err := json.Unmarshal(ham, &tek); err != nil {
			return nil, fmt.Errorf("gorev JSON cozulemedi: %v", err)
		}
		kayitlar = []GorevKaydi{tek}
	default:
		if err := json.Unmarshal(ham, &kayitlar); err != nil {
			return nil, fmt.Errorf("gorev JSON cozulemedi: %v", err)
		}
	}

	gorevler := make([]GorevKaydi, 0, len(kayitlar))
	for _, k := range kayitlar {
		if !tumu && strings.HasPrefix(k.Ad, `\Microsoft\`) {
			continue
		}
		gorevler = append(gorevler, k)
		if len(gorevler) >= gorevUstSiniri {
			break
		}
	}
	return gorevler, nil
}
