//go:build windows

// kurulum_windows.go — KURULUM KATALOGU: yerel panel sihirbazinin arka ucu.
//
// Katalog "bu sunucuya ne kurulabilir"i, is modeli "su an ne kuruluyor"u tutar.
// Kurulumlar ASENKRON kosar: HTTP istegi is kimligiyle hemen doner, ilerleme
// is gunlugunden yoklanir — SQL Express gibi paketler 15 dakika surebilir,
// hicbir HTTP istemcisi o kadar acik beklemez.
//
// 🔴 KADEME DURUSTLUGU: "guvenilir" gercekten calistigi gorulmus yol,
// "deneysel" henuz filoda sinanmamis yol demektir ve bu etiket KULLANICIYA
// AYNEN gosterilir; "karar-bekliyor" (mail) ise KURULAMAZ — cozum secilmeden
// kurulum dugmesi koymak kullaniciya yalan soylemek olurdu.
package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── katalog ─────────────────────────────────────────────────────────────────

const (
	kademeGuvenilir     = "guvenilir"
	kademeDeneysel      = "deneysel"
	kademeKararBekliyor = "karar-bekliyor"
)

// KatalogKalemi — kurulabilir tek yazilim; Yetenek alani kurulum sonrasi
// acilmasi beklenen kesif bitidir (kurulu-mu tespitinde de kullanilir).
type KatalogKalemi struct {
	Anahtar     string
	Ad          string
	Aciklama    string
	Kademe      string // guvenilir | deneysel | karar-bekliyor
	SureTahmini string
	Yetenek     Yetenek
}

// 🔴 SABIT SURUM URL'LERI: ssl/mysql/pgsql adresleri bilerek surume kilitli.
// "En son surum" baglantisi gunun birinde davranisi degismis bir paket
// indirir ve kurulum sessizce baskalasir; surum yukseltme bu katalogda
// BILINCLI bir degisiklikle yapilir. dotnet/mssql'de Microsoft'un kalici
// yonlendirme adresleri kullanilir (dogrudan CDN adresleri kalici degil).
const (
	indirilenDizin = `C:\ProgramData\girginospanel\indirilen`
	winAcmeDizin   = `C:\Program Files\GirginOSPanel\win-acme`

	dotnetHostingURL = "https://aka.ms/dotnet/8.0/dotnet-hosting-win.exe"
	winAcmeURL       = "https://github.com/win-acme/win-acme/releases/download/v2.2.9.1701/win-acme.v2.2.9.1701.x64.pluggable.zip"
	mssqlURL         = "https://go.microsoft.com/fwlink/?linkid=2216019"
	gosqlcmdURL      = "https://github.com/microsoft/go-sqlcmd/releases/download/v1.8.0/sqlcmd-windows-amd64.zip"
	mysqlURL         = "https://dev.mysql.com/get/Downloads/MySQLInstaller/mysql-installer-community-8.0.40.0.msi"
	pgsqlURL         = "https://get.enterprisedb.com/postgresql/postgresql-16.4-1-windows-x64.exe"

	// 🔴 KATALOG GENISLETME — SABIT SURUM URL'LERI: her adres surume kilitli;
	// "en son" baglantilari gunun birinde farkli paket indirir ve kurulum
	// sessizce baskalasir (mevcut ssl/mysql/pgsql ile ayni gerekce). redis
	// (Memurai) URL'i DENEYSEL: filoda sinanmadi, 404 gelirse gunluge acikca duser.
	nodeURL         = "https://nodejs.org/dist/v20.18.1/node-v20.18.1-x64.msi"
	gitURL          = "https://github.com/git-for-windows/git/releases/download/v2.47.1.windows.1/Git-2.47.1-64-bit.exe"
	rewriteURL      = "https://download.microsoft.com/download/1/2/8/128E2E22-C1B9-44A4-BE2A-5859ED1D4592/rewrite_amd64_en-US.msi"
	redisMemuraiURL = "https://download.memurai.com/Memurai-Developer-v4.1.6.msi"
	phpMyAdminURL   = "https://files.phpmyadmin.net/phpMyAdmin/5.2.1/phpMyAdmin-5.2.1-all-languages.zip"

	// phpMyAdmin acilma dizini (IIS bu ic klasore yonlendirilir; bkz. kurPhpMyAdmin).
	phpMyAdminDizin = `C:\inetpub\gpanel-tools\phpmyadmin`
)

var katalog = []KatalogKalemi{
	{Anahtar: "iis", Ad: "Web Sunucu (IIS)", Aciklama: "Internet Information Services web sunucu rolu, yonetim araclariyla.", Kademe: kademeGuvenilir, SureTahmini: "2-5 dk", Yetenek: YetSite},
	{Anahtar: "ftp", Ad: "IIS FTP Sunucusu", Aciklama: "IIS uzerinde FTP yayin servisi (ftpsvc).", Kademe: kademeGuvenilir, SureTahmini: "1-3 dk", Yetenek: YetFTP},
	{Anahtar: "dns", Ad: "DNS Server", Aciklama: "Windows DNS Server rolu, yonetim araclariyla.", Kademe: kademeGuvenilir, SureTahmini: "1-3 dk", Yetenek: YetDNS},
	{Anahtar: "dotnet", Ad: "ASP.NET Core Hosting Bundle", Aciklama: ".NET 8 Hosting Bundle; IIS'e ANCM V2 modulunu ekler.", Kademe: kademeGuvenilir, SureTahmini: "2-4 dk", Yetenek: YetDotNet},
	{Anahtar: "ssl", Ad: "SSL (win-acme)", Aciklama: "Let's Encrypt istemcisi win-acme v2.2.9.1701 (sabit surum).", Kademe: kademeGuvenilir, SureTahmini: "1-2 dk", Yetenek: YetSSL},
	{Anahtar: "mssql", Ad: "SQL Server 2022 Express", Aciklama: "Microsoft SQL Server 2022 Express; kurulum uzun surer.", Kademe: kademeDeneysel, SureTahmini: "5-15 dk", Yetenek: YetMSSQL},
	{Anahtar: "mysql", Ad: "MySQL Server", Aciklama: "MySQL Installer 8.0.40; ornek ayrica yapilandirilir.", Kademe: kademeDeneysel, SureTahmini: "3-8 dk", Yetenek: YetMySQL},
	{Anahtar: "pgsql", Ad: "PostgreSQL", Aciklama: "PostgreSQL 16.4, katilimsiz kurulum (port 5432).", Kademe: kademeDeneysel, SureTahmini: "3-8 dk", Yetenek: YetPgSQL},
	// 🔴 KATALOG GENISLETME: asagidaki kalemler bir Yetenek bitine BAGLI DEGIL
	// (platform.go'ya yeni bit eklenMEZ); kurulu-mu tespiti kalemKuruluMu icinde
	// ozel yapilir (PATH / dosya / servis / dizin), Yetenek alani bilerek bostur.
	{Anahtar: "node", Ad: "Node.js LTS", Aciklama: "Node.js 20.18.1 LTS calisma zamani (MSI, sessiz kurulum).", Kademe: kademeGuvenilir, SureTahmini: "1-3 dk"},
	{Anahtar: "git", Ad: "Git for Windows", Aciklama: "Git 2.47.1 surum kontrol istemcisi (sessiz kurulum).", Kademe: kademeGuvenilir, SureTahmini: "1-3 dk"},
	{Anahtar: "rewrite", Ad: "IIS URL Rewrite", Aciklama: "IIS URL Rewrite Module 2.1; temiz URL / yonlendirme kurallari icin.", Kademe: kademeGuvenilir, SureTahmini: "1-2 dk"},
	{Anahtar: "redis", Ad: "Redis (Memurai)", Aciklama: "Windows'ta resmi Redis yok; Memurai dogrudan indirme kayit istiyor (403). Cozum karari verilmedi.", Kademe: kademeKararBekliyor, SureTahmini: "-"},
	{Anahtar: "phpmyadmin", Ad: "phpMyAdmin", Aciklama: "DENEYSEL: phpMyAdmin 5.2.1 dosyalari; CALISMASI ICIN PHP + IIS gerekir.", Kademe: kademeDeneysel, SureTahmini: "1-2 dk"},
	{Anahtar: "mail", Ad: "Windows Mail", Aciklama: "Windows uzerinde posta sunucusu — cozum karari verilmedi, kurulamaz.", Kademe: kademeKararBekliyor, SureTahmini: "-", Yetenek: YetMail},
}

// KalemDurum — GET /api/yerel/katalog yanitindaki tek satir (etiketsiz, Go
// alan adlariyla — SiteSonuc/OlayKaydi ile ayni sozlesme bicimi). Yetenek
// biti ic detaydir, disari SIZDIRILMAZ; bu yuzden gomme degil duz kopya.
type KalemDurum struct {
	Anahtar     string
	Ad          string
	Aciklama    string
	Kademe      string
	SureTahmini string
	Kurulu      bool
	Kurulabilir bool
}

// KatalogDurumu — katalogun tamamini kurulu/kurulabilir bilgisiyle dondurur.
func KatalogDurumu() []KalemDurum {
	algilanan := algilananYetenekler() // tek Get-Service + 60 sn onbellek
	durumlar := make([]KalemDurum, 0, len(katalog))
	for _, k := range katalog {
		kurulu := kalemKuruluMu(k, algilanan)
		durumlar = append(durumlar, KalemDurum{
			Anahtar:     k.Anahtar,
			Ad:          k.Ad,
			Aciklama:    k.Aciklama,
			Kademe:      k.Kademe,
			SureTahmini: k.SureTahmini,
			Kurulu:      kurulu,
			Kurulabilir: !kurulu && k.Kademe != kademeKararBekliyor,
		})
	}
	return durumlar
}

// node/git BILINEN YOLLARI: kurulum sistem PATH'ini registry'de gunceller ama
// ZATEN CALISAN ajan surecinin PATH'i bayat kalir → exec.LookPath kurulumdan
// SONRA bile bulamaz → katalog "kurulu degil" gosterir → gereksiz yeniden-kurulum
// dongusu (denetim 01/4). Sabit kurulum yollarini os.Stat ile de kontrol ederek
// bunu onleriz (ssl/rewrite/redis zaten os.Stat/sc kullaniyor; sorun yalniz LookPath).
var (
	nodeBilinenYollar = []string{`C:\Program Files\nodejs\node.exe`, `C:\Program Files (x86)\nodejs\node.exe`}
	gitBilinenYollar  = []string{`C:\Program Files\Git\cmd\git.exe`, `C:\Program Files\Git\bin\git.exe`, `C:\Program Files (x86)\Git\cmd\git.exe`}
)

// komutVeyaDosya — komut PATH'te VEYA bilinen sabit yollardan birinde varsa true.
// Bayat surec PATH'i tuzagini (B-14) sabit-yol yedegiyle kapatir.
func komutVeyaDosya(komut string, yollar []string) bool {
	if _, err := exec.LookPath(komut); err == nil {
		return true
	}
	for _, y := range yollar {
		if _, err := os.Stat(y); err == nil {
			return true
		}
	}
	return false
}

// kalemKuruluMu — kurulu tespiti MEVCUT kesif desenini kullanir: servis
// tabanli kalemler algilananYetenekler'den okunur (windows.go'daki tek
// Get-Service cagrisi + onbellek; burada ikinci bir powershell firtinasi
// ACILMAZ), iis/ssl ise diskteki izinden anlasilir.
func kalemKuruluMu(k KatalogKalemi, algilanan Yetenek) bool {
	switch k.Anahtar {
	case "iis":
		// appcmd.exe yalniz IIS rolu kuruluysa var (Dogrula ile ayni olcut).
		// YetSite BILEREK kullanilmaz: o bit kendini-sina muhruyle acilir,
		// "IIS kurulu" ile "host sinanmis" ayni sey degildir.
		_, err := os.Stat(appcmdYolu())
		return err == nil
	case "ssl":
		_, err := os.Stat(filepath.Join(winAcmeDizin, "wacs.exe"))
		return err == nil
	case "mail":
		return false // cozum secilmedi; kurulu sayilacak bir sey yok
	case "node":
		return komutVeyaDosya("node", nodeBilinenYollar)
	case "git":
		return komutVeyaDosya("git", gitBilinenYollar)
	case "rewrite":
		_, err := os.Stat(rewriteDllYolu())
		return err == nil
	case "redis":
		return memuraiKuruluMu()
	case "phpmyadmin":
		_, err := os.Stat(phpMyAdminDizin)
		return err == nil
	case "mssql":
		// 🔴 KISMI KURULUM CIKMAZI (denetim 01/1): motor kurulu ama sqlcmd
		// yoksa vt yonetimi (liste/olustur/sil) sessizce patlar. Bu durumu
		// "kurulu" SAYMA — kalem KURULABILIR kalsin ki kurucu ikinci kez kosup
		// (motoru atlayarak) yalniz sqlcmd'yi onarabilsin. Iki kosul da sart:
		// motor (YetMSSQL) + sqlcmd (bilinen konum ya da PATH).
		if !algilanan.Var(YetMSSQL) {
			return false
		}
		if _, err := os.Stat(sqlcmdBilinenYol); err == nil {
			return true
		}
		_, err := exec.LookPath("sqlcmd")
		return err == nil
	default:
		return algilanan.Var(k.Yetenek)
	}
}

// ── is modeli (asenkron, tek ucus) ──────────────────────────────────────────

const (
	isKosuyor   = "kosuyor"
	isBasarili  = "basarili"
	isBasarisiz = "basarisiz"
	isKesildi   = "kesildi" // ajan restart'i kurulumu yarida kesti (disk marker)
	isKismi     = "kismi"   // adim gecti ama tam degil: ek yapilandirma gerekli (B-07)

	// logSatirTavani — is basina tutulan en cok gunluk satiri; asilirsa en
	// eskiler atilir (kurulum gunlugu canli izleme icindir, arsiv degil).
	logSatirTavani = 500

	// kurulumZamanAsimi — hem indirme hem kurucu komut icin ust sinir.
	// SQL Express 15 dk surebilir; 30 dk yavas hatta bile bol pay birakir,
	// takilan bir kurulumun sonsuza dek "kosuyor" gorunmesini onler.
	kurulumZamanAsimi = 30 * time.Minute
)

// ErrKurulumSuruyor — tek-ucus kurali reddetti (HTTP tarafi 409'a cevirir).
var ErrKurulumSuruyor = errors.New("baska bir kurulum suruyor")

// ErrKurulamaz — kalem bu haliyle kurulamaz: taninmiyor, zaten kurulu ya da
// karar-bekliyor (HTTP tarafi 422'ye cevirir).
var ErrKurulamaz = errors.New("kurulamaz")

// ErrKismiKurulum — kurucu adimlari HATASIZ kostu ama sonuc TAM DEGIL: ek
// yapilandirma gerekiyor (orn. mysql yalnizca Installer aracini kurdu, calisan
// ORNEK yok; phpmyadmin dosyalari acildi ama PHP gerekli). Is `basarili` DEGIL
// `kismi` isaretlenir — sahte "hazir" izlenimi vermemek icin (denetim 01/3,
// "basarisizlik guvence gibi gorunur" dersi). Kurucu bunu SARARAK doner.
var ErrKismiKurulum = errors.New("kurulum kismi: ek yapilandirma gerekli")

// ErrKurulumAsili — ajan onceki kurulum bitmeden yeniden basladi; diskte marker
// kaldi. Yeni kurulum, operator asiliyi temizleyene kadar REDDEDILIR (HTTP 409).
var ErrKurulumAsili = errors.New("onceki kurulum yarim kaldi (asili)")

// ── restart-guvenli tek-ucus: disk kilidi (denetim 01/2) ────────────────────
//
// 🔴 Bellek-ici aktifID ajan restart'inda sifirlanir; ama Windows'ta ebeveyn
// oldugunde cocuk yukleyici (msiexec/SQL setup) OLMEZ, OS'ta kosmaya devam eder.
// Ajan geri gelince tek-ucus BOS gorunur → operator tekrar dener → IKI kurulum
// ayni anda → CBS/MSI cakismasi. Cozum: kurulum baslarken diske marker yaz,
// bitince sil; acilista marker VARSA "asili" say ve yeni kurulumu reddet.
const kurulumMarkeriYolu = `C:\ProgramData\girginospanel\kurulum-aktif.json`

type kurulumMarker struct {
	ID        string    `json:"id"`
	Anahtar   string    `json:"anahtar"`
	Baslangic time.Time `json:"baslangic"`
}

// asiliKurulum — acilista diskte yarim kalmis marker bulunduysa doldurulur;
// bos degilse Baslat yeni kurulumu reddeder. KurulumAsiliTemizle temizler.
var asiliKurulum struct {
	sync.Mutex
	marker *kurulumMarker
}

func kurulumMarkeriYaz(id, anahtar string) {
	b, err := json.Marshal(kurulumMarker{ID: id, Anahtar: anahtar, Baslangic: time.Now()})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(kurulumMarkeriYolu), 0o755)
	_ = os.WriteFile(kurulumMarkeriYolu, b, 0o644)
}

func kurulumMarkeriSil() { _ = os.Remove(kurulumMarkeriYolu) }

// KurulumAsiliYukle — AJAN ACILISINDA cagrilir. Diskte kurulum markeri varsa
// onceki kurulum yarim kalmis demektir: ilgili is kaydini "kesildi" olarak
// yeniden olusturur (UI 0/1'de donmesin) ve asiliKurulum'u doldurur (Baslat
// reddedecek). Marker SILINMEZ — operator KurulumAsiliTemizle ile bilincli siler.
func KurulumAsiliYukle() {
	b, err := os.ReadFile(kurulumMarkeriYolu)
	if err != nil {
		return // marker yok — normal acilis
	}
	var m kurulumMarker
	if json.Unmarshal(b, &m) != nil || m.Anahtar == "" {
		m.Anahtar = "bilinmeyen" // bozuk marker: yine de asili say
	}
	asiliKurulum.Lock()
	asiliKurulum.marker = &m
	asiliKurulum.Unlock()
	if m.ID != "" {
		is := &KurulumIsi{ID: m.ID, Anahtar: m.Anahtar, Baslangic: m.Baslangic, Durum: isKesildi}
		is.LogSatirlari = []string{time.Now().Format("15:04:05") +
			" ajan yeniden basladi — bu kurulum YARIM KALDI (kesildi). Calisan bir yukleyici olmadigindan emin olun, sonra 'asili kilidi temizle'."}
		kurulumlar.Lock()
		kurulumlar.isler[m.ID] = is
		kurulumlar.Unlock()
	}
}

// KurulumAsiliVar — asili (yarim kalmis) bir kurulum var mi + anahtari.
func KurulumAsiliVar() (bool, string) {
	asiliKurulum.Lock()
	defer asiliKurulum.Unlock()
	if asiliKurulum.marker == nil {
		return false, ""
	}
	return true, asiliKurulum.marker.Anahtar
}

// KurulumAsiliTemizle — operator onayi: asili markeri siler, yeni kurulumlara
// izin verir. Operatorun calisan bir yukleyici olmadigindan emin olmasi beklenir.
func KurulumAsiliTemizle() {
	asiliKurulum.Lock()
	asiliKurulum.marker = nil
	asiliKurulum.Unlock()
	kurulumMarkeriSil()
}

// Ilerleme — yapisal (makine-okunur) ilerleme; metin loguna EK olarak canli
// ETA cubugu + realtime akis icindir. Iki asama vardir: "indiriliyor" (bayt
// bazli, Content-Length biliniyorsa yuzde+hiz+ETA belirlenir) ve "kuruluyor"
// (yukleyici kosuyor — SURESI GERCEKTEN kestirilemez, o yuzden Yuzde=-1 belirsiz
// isaretlenir; sahte bir ETA gostermek "basarisizlik guvence gibi gorunur"
// dersinin ihlali olurdu). Asama bos = henuz somut bir asamada degil.
type Ilerleme struct {
	Asama      string `json:"asama"`       // "indiriliyor" | "kuruluyor" | ""
	Etiket     string `json:"etiket"`      // insana gorunur ad (dosya adi vb.)
	ByteInen   int64  `json:"byte_inen"`   // inen bayt (surdurme dahil kumulatif)
	ByteToplam int64  `json:"byte_toplam"` // tam dosya boyutu; 0 = bilinmiyor
	Yuzde      int    `json:"yuzde"`       // 0..100; belirsiz = -1
	HizBps     int64  `json:"hiz_bps"`     // ortalama hiz (bayt/sn); 0 = bilinmiyor
	KalanSn    int    `json:"kalan_sn"`    // ETA saniye; -1 = bilinmiyor
}

// KurulumIsi — tek kurulum kosusunun canli kaydi.
type KurulumIsi struct {
	ID        string
	Anahtar   string
	Baslangic time.Time

	// mu asagidaki alanlari korur: kurucu goroutine yazar, HTTP tarafi okur.
	mu           sync.Mutex
	Durum        string   // kosuyor | basarili | basarisiz
	LogSatirlari []string // zaman damgali; logSatirTavani ustu en eskiden atilir
	Ilerleme     Ilerleme // canli yapisal ilerleme (ETA cubugu + SSE)
	// 🔴 SirCikti: tek-seferlik hassas cikti (or. uretilen superuser parolasi).
	// Gunluge (LogSatirlari) YAZILMAZ (CWE-532: scrollback'te kalir, her acilista
	// yeniden gorunurdu); panel bunu ayri "gizli cikti" alaninda BIR KEZ gosterir.
	SirCikti string
}

// ilerlemeYaz — is'in yapisal ilerlemesini kilitli gunceller. Kurucu goroutine
// (indirme/kurma) yazar; IsDurumu okuyup HTTP/SSE'ye tasir.
func (is *KurulumIsi) ilerlemeYaz(p Ilerleme) {
	is.mu.Lock()
	is.Ilerleme = p
	is.mu.Unlock()
}

// 🔴 TEK UCUS: ayni anda EN COK BIR kurulum kosar. Iki dism/MSI kurulumu
// cakisirsa Windows Installer/CBS kilitlenir ve sistem yarim kurulumla
// bozulabilir. Ikinci istek KUYRUGA ALINMAZ, acikca reddedilir: gizli bir
// sirada bekletmek yerine operatore "baska kurulum suruyor" denir, o da
// bitisi gorup kendisi yeniden baslatir.
var kurulumlar = struct {
	sync.Mutex
	isler   map[string]*KurulumIsi
	aktifID string // bos = su an kurulum yok
}{isler: map[string]*KurulumIsi{}}

// Baslat — katalog kalemi icin kurulumu asenkron baslatir, is kimligi doner.
// Is kayitlari surec omru boyunca bellekte kalir; katalog kucuk ve kurulum
// insan hiziyla tetiklenir, sinirsiz buyume soz konusu degil.
func Baslat(anahtar string) (string, error) {
	// 🔴 ASILI KURULUM (disk kilidi, denetim 01/2): onceki kurulum ajan
	// restart'inda yarim kaldiysa yeni kurulumu REDDET — korlemesine ikinci
	// kurulum, OS'ta hala kosuyor olabilecek yukleyiciyle CBS/MSI cakismasi demek.
	if asili, anah := KurulumAsiliVar(); asili {
		return "", fmt.Errorf("onceki kurulum (%s) yarim kaldi; calisan bir yukleyici olmadigindan emin olup asili kilidi temizleyin: %w", anah, ErrKurulumAsili)
	}
	var kalem *KatalogKalemi
	for i := range katalog {
		if katalog[i].Anahtar == anahtar {
			kalem = &katalog[i]
			break
		}
	}
	if kalem == nil {
		return "", fmt.Errorf("katalogda boyle bir kalem yok: %q: %w", anahtar, ErrKurulamaz)
	}
	if kalem.Kademe == kademeKararBekliyor {
		return "", fmt.Errorf("%s icin cozum karari henuz verilmedi: %w", kalem.Ad, ErrKurulamaz)
	}
	if kalemKuruluMu(*kalem, algilananYetenekler()) {
		return "", fmt.Errorf("%s zaten kurulu: %w", kalem.Ad, ErrKurulamaz)
	}
	kurucu := kurucular[anahtar]
	if kurucu == nil {
		// katalogda olup kurucusu olmayan kalem programlama hatasidir; yine de
		// panik yerine acik hata: panel ayakta kalir.
		return "", fmt.Errorf("%s icin kurucu tanimli degil: %w", anahtar, ErrKurulamaz)
	}

	id, err := rasgeleHex(8)
	if err != nil {
		return "", fmt.Errorf("is kimligi uretilemedi: %w", err)
	}

	kurulumlar.Lock()
	if aktif := kurulumlar.aktifID; aktif != "" {
		kurulumlar.Unlock()
		return "", fmt.Errorf("su an %q isi kosuyor: %w", aktif, ErrKurulumSuruyor)
	}
	is := &KurulumIsi{ID: id, Anahtar: anahtar, Baslangic: time.Now(), Durum: isKosuyor}
	kurulumlar.isler[id] = is
	kurulumlar.aktifID = id
	kurulumlar.Unlock()
	// 🔴 DISK KILIDI: is basladi — diske marker yaz. Ajan bu kurulum bitmeden
	// restart olursa acilista (KurulumAsiliYukle) marker bulunur ve yeni kurulum
	// reddedilir. Marker asagidaki goroutine cleanup'inda (basari/hata/panik) silinir.
	kurulumMarkeriYaz(id, anahtar)

	go func() {
		var hata error
		// 🔴 IZOLASYON (stabilite rehberi #1 + denetim 01/2): kurucudaki bir
		// PANIK TUM ajani dusurmesin. platform.go izolasyonu ayri binary/kanal
		// vaat eder; ayni disiplin surec ICINDE de gecerli olmali — tek kalemin
		// kurulumu, calisan siteleri ve merkezi API'yi indirmemeli. Panik burada
		// yakalanir, basarisiz is'e cevrilir; asagidaki temizlik (aktifID sifirla)
		// HER hAlde kosar, yoksa tek-ucus kilidi sonsuza dek kilitli kalirdi.
		func() {
			defer func() {
				if r := recover(); r != nil {
					hata = fmt.Errorf("kurulum icinde panik (izole edildi): %v", r)
					logSatiri(is, "🔴 KURULUM PANIK — izole edildi, ajan ayakta: %v", r)
				}
			}()
			logSatiri(is, "kurulum basladi: %s [%s]", kalem.Ad, kalem.Kademe)
			hata = kurucu(is)
		}()
		switch {
		case hata == nil:
			logSatiri(is, "kurulum tamamlandi: %s", kalem.Ad)
		case errors.Is(hata, ErrKismiKurulum):
			logSatiri(is, "KURULUM KISMI: %v", hata)
		default:
			logSatiri(is, "KURULUM BASARISIZ: %v", hata)
		}

		// 🔴 ONBELLEK DUSURME: windows.go'daki 60 sn'lik kesif onbellegi
		// burada bosa dusurulur ki panelin yetenek cipleri kurulum biter
		// bitmez tazelensin. Basarisizlikta da dusurulur: yarim kurulum bile
		// servis kaydi birakmis olabilir, kesif gercegi soylesin.
		yetenekOnbellegiDusur()

		// Durum EN SON degisir: istemci bitti=true gordugu anda gunlugun son
		// satirlari coktan yerinde olsun.
		is.mu.Lock()
		switch {
		case hata == nil:
			is.Durum = isBasarili
		case errors.Is(hata, ErrKismiKurulum):
			is.Durum = isKismi
		default:
			is.Durum = isBasarisiz
		}
		is.mu.Unlock()

		kurulumlar.Lock()
		kurulumlar.aktifID = ""
		kurulumlar.Unlock()
		kurulumMarkeriSil() // is bitti (basari/hata/panik) — disk kilidi kalkar
	}()
	return id, nil
}

// IsGoruntu — IsDurumu'nun anlik kopyasi; cagiran elinde guvenle tutabilir.
type IsGoruntu struct {
	ID       string
	Anahtar  string
	Durum    string
	Bitti    bool
	Log      []string
	Ilerleme Ilerleme
	SirCikti string // tek-seferlik hassas cikti (parola vb.); gunlukte DEGIL
}

// IsDurumu — is kaydinin anlik goruntusu; ikinci deger "is bulundu mu"dur.
// Gunluk zaten logSatirTavani ile sinirli tutuldugundan tamami doner.
func IsDurumu(id string) (*IsGoruntu, bool) {
	kurulumlar.Lock()
	is := kurulumlar.isler[id]
	kurulumlar.Unlock()
	if is == nil {
		return nil, false
	}
	is.mu.Lock()
	defer is.mu.Unlock()
	return &IsGoruntu{
		ID:       is.ID,
		Anahtar:  is.Anahtar,
		Durum:    is.Durum,
		Bitti:    is.Durum != isKosuyor,
		Log:      append([]string(nil), is.LogSatirlari...), // kopya: yaris yok
		Ilerleme: is.Ilerleme,
		SirCikti: is.SirCikti,
	}, true
}

// yetenekOnbellegiDusur — windows.go'daki yetenekOnbellegi'ni bosa dusurur.
// AYNI paketteyiz: windows.go'ya DOKUNMADAN onbellek degiskenine dogrudan
// erisilir; zaman sifirlaninca algilananYetenekler bir sonraki cagrida
// yeniden kesif yapar (60 sn bayatlik beklenmez).
func yetenekOnbellegiDusur() {
	yetenekOnbellegi.Lock()
	yetenekOnbellegi.zaman = time.Time{}
	yetenekOnbellegi.Unlock()
}

// logSatiri — is gunlugune zaman damgali tek satir ekler; tavan asilirsa en
// eski satirlar atilir.
func logSatiri(is *KurulumIsi, bicim string, arg ...any) {
	satir := time.Now().Format("15:04:05") + " " + fmt.Sprintf(bicim, arg...)
	is.mu.Lock()
	defer is.mu.Unlock()
	is.LogSatirlari = append(is.LogSatirlari, satir)
	if len(is.LogSatirlari) > logSatirTavani {
		// yeni dilime kopyalanir ki atilan bas kismin bellegi geri verilsin
		is.LogSatirlari = append([]string(nil), is.LogSatirlari[len(is.LogSatirlari)-logSatirTavani:]...)
	}
}

func rasgeleHex(bayt int) (string, error) {
	b := make([]byte, bayt)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── komut kosucu (akisli gunluk) ────────────────────────────────────────────

// logYazici — exec ciktisini satir satir is gunlugune akitir. Stdout ve
// Stderr'e AYNI deger verilir; os/exec sozlesmesi geregi ayni Writer'a ayni
// anda tek goroutine yazar, kalan tamponu bu yuzden ek kilit istemez.
type logYazici struct {
	is    *KurulumIsi
	kalan []byte
}

func (y *logYazici) Write(p []byte) (int, error) {
	y.kalan = append(y.kalan, p...)
	for {
		i := bytes.IndexByte(y.kalan, '\n')
		if i < 0 {
			break
		}
		satir := strings.TrimSpace(strings.TrimRight(string(y.kalan[:i]), "\r"))
		y.kalan = y.kalan[i+1:]
		if satir != "" {
			logSatiri(y.is, "  %s", satir)
		}
	}
	return len(p), nil
}

// bosalt — komut bitince yarim kalan son satiri da gunluge basar.
func (y *logYazici) bosalt() {
	if satir := strings.TrimSpace(string(y.kalan)); satir != "" {
		logSatiri(y.is, "  %s", satir)
	}
	y.kalan = nil
}

// kurulumKomutu — komutu zaman asimli kosar, ciktisini satir satir gunluge
// akitir. CombinedOutput BILEREK kullanilmadi: 15 dakikalik SQL kurulumunda
// operator canli ilerleme gormeli, is bitince tek blok degil. Argumanlar
// yalniz sabit kurucu kodundan gelir; kabuk birlestirme yok.
func kurulumKomutu(is *KurulumIsi, ad string, arg ...string) error {
	// 🔴 DURUST BELIRSIZLIK: yukleyici kosarken sure GERCEKTEN kestirilemez
	// (msiexec/SQL setup ne kadar surecegini soylemez). Bu asamayi "kuruluyor"
	// + Yuzde=-1 isaretle ki UI belirsiz (sweep) cubuk gostersin, sahte bir
	// yuzde/ETA uydurmasin (denetim 01/3 "basarisizlik guvence gibi gorunur").
	is.ilerlemeYaz(Ilerleme{Asama: "kuruluyor", Etiket: filepath.Base(ad), Yuzde: -1, KalanSn: -1})
	logSatiri(is, "> %s %s", ad, strings.Join(arg, " "))
	ctx, iptal := context.WithTimeout(context.Background(), kurulumZamanAsimi)
	defer iptal()
	cmd := exec.CommandContext(ctx, ad, arg...)
	y := &logYazici{is: is}
	cmd.Stdout = y
	cmd.Stderr = y
	err := cmd.Run()
	y.bosalt()
	if err != nil {
		// Zaman asimi kontrolu BILEREK hata dalinin icinde: Run basariyla
		// dondukten sonra dolan sure basarili kurulumu "zaman asimi" yapmasin.
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s: %.0f dakikalik zaman asimi doldu", ad, kurulumZamanAsimi.Minutes())
		}
		// 3010 = ERROR_SUCCESS_REBOOT_REQUIRED: kurulum dunyasinda "kuruldu,
		// yeniden baslatma gerekli" demektir; basarisizlik SAYILMAZ (aksi
		// halde basarili SQL/MSI kurulumu yanlis alarm uretirdi).
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 3010 {
			logSatiri(is, "cikis kodu 3010: kurulum tamam, yeniden baslatma gerekli")
			return nil
		}
		return fmt.Errorf("%s: %w", ad, err)
	}
	return nil
}

// ── indirme + arsiv yardimcilari ────────────────────────────────────────────

// ilerlemeOkuyucu — okunan baytlari sayar. IKI islevi var: (1) her 10 MB'da bir
// gunluge metin satiri duser (arsiv/geriye-uyum), (2) ~250 ms'de bir is'in
// YAPISAL ilerlemesini gunceller (canli ETA cubugu + SSE). Hiz, indirme
// baslangicindan bu yana ORTALAMA olarak hesaplanir: anlik hizdan daha az
// zipzip yapar, ETA daha kararli olur.
type ilerlemeOkuyucu struct {
	r           io.Reader
	is          *KurulumIsi
	etiket      string
	toplam      int64     // simdiye dek okunan (surdurme ofseti dahil kumulatif)
	boyut       int64     // tam dosya boyutu; 0 = bilinmiyor (belirsiz cubuk)
	sonRapor    int64     // 10 MB'lik metin log satiri icin son esik
	baslama     time.Time // bu denemenin baslama ani (hiz icin)
	baslamaBayt int64     // baslamadaki bayt (surdurme ofseti) — hiz payi
	sonGuncel   time.Time // yapisal guncelleme kismasi (~250 ms)
}

func (o *ilerlemeOkuyucu) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	o.toplam += int64(n)
	if o.toplam-o.sonRapor >= 10<<20 {
		o.sonRapor = o.toplam
		logSatiri(o.is, "  ... %d MB indi", o.toplam>>20)
	}
	simdi := time.Now()
	if err != nil || simdi.Sub(o.sonGuncel) >= 250*time.Millisecond {
		o.sonGuncel = simdi
		o.yapisalYaz(simdi)
	}
	return n, err
}

// yapisalYaz — anlik bayt/hiz/ETA'yi is'in Ilerleme alanina yazar.
func (o *ilerlemeOkuyucu) yapisalYaz(simdi time.Time) {
	p := Ilerleme{Asama: "indiriliyor", Etiket: o.etiket, ByteInen: o.toplam, ByteToplam: o.boyut, Yuzde: -1, KalanSn: -1}
	gecen := simdi.Sub(o.baslama).Seconds()
	if gecen > 0 {
		hiz := float64(o.toplam-o.baslamaBayt) / gecen // ortalama bayt/sn
		p.HizBps = int64(hiz)
		if o.boyut > 0 {
			p.Yuzde = int(float64(o.toplam) / float64(o.boyut) * 100)
			if p.Yuzde > 100 {
				p.Yuzde = 100
			}
			if hiz > 0 {
				p.KalanSn = int(float64(o.boyut-o.toplam) / hiz)
			}
		}
	}
	o.is.ilerlemeYaz(p)
}

// ── indirme: retry + backoff + resume + checksum (B-06, B-12) ────────────────

const (
	indirmeDenemeUst  = 4               // en cok deneme sayisi
	indirmeBekleTaban = 2 * time.Second // ussel backoff tabani: 2, 4, 8 sn
)

// errIndirmeKalici — retry EDILMEYECEK indirme hatasi (404/403/410: dosya yok,
// yeniden denemek fayda etmez). Diger hatalar (baglanti, 5xx, yarim govde,
// checksum) GECICI sayilir ve backoff'la yeniden denenir.
var errIndirmeKalici = errors.New("indirme kalici hata")

// indirmeSha — SABIT SURUM URL'leri icin beklenen SHA-256 (B-12 tedarik-zinciri).
// 🔴 Yalniz SURUME KILITLI, DEGISMEZ adresler pinlenir; aka.ms/fwlink gibi
// yonlendiren adresler (mssql SSEI, dotnet) arkadaki dosyayi guncelleyebildigi
// icin PINLENMEZ (yanlis-pozitif olurdu), onlarda dogrulama atlanir ve gunluge
// ACIKCA yazilir (sessiz gecis YOK). Haritada olmayan URL = "checksum yok".
var indirmeSha = map[string]string{
	// 181'de indirilip hesaplandi (2026-09-04); yalniz surume kilitli dogrudan adresler.
	gosqlcmdURL:   "fcfc2960426637e049d961722ad5eed6f4a824c9724163ef7f681fc568420b41",
	winAcmeURL:    "a2c874e9893a1d91e0329887f72c067dcc49800a49963fa7d61c5a4e09058f0c",
	rewriteURL:    "37342ff2f585f263f34f48e9de59eb1051d61015a8e967dbde4075716230a32a",
	mysqlURL:      "f7ac30efc8b04a8348756ce3b69718da9619a5c5b061354fafd3849fd339c90d",
	phpMyAdminURL: "31c95fe5c00e0f899b5d31ac6fff506cf8061f2f746e9d7084c395f47451946e",
	nodeURL:       "658930e6136d01bf244146a6436d8ea146cd50557c1b2a2617a60bcd9dce0da1",
	gitURL:        "25527923debc06515b3016f2d6bca0820656e8281a23be2f43bfb658bd5dda70",
	pgsqlURL:      "f4bf0ac4b33471f18aad7d1d9cc52613003f3a3a612aae167366bf7f7840b2bc",
	// PINLENMEDI (bilincli): mssqlURL/dotnetHostingURL yonlendirme (fwlink/aka.ms
	// arkadaki dosyayi guncelleyebilir), redis 403. Bunlarda dogrulama atlanir + loglanir.
}

// indir — url'deki dosyayi GUVENLI indirir: gecici `.indiriliyor` dosyasina
// akitir, ussel-backoff'la yeniden dener, kesilmis indirmeyi HTTP Range ile
// SURDURUR, akista SHA-256 hesaplar ve (indirmeSha'da varsa) dogrular; ANCAK
// basari + dogrulama sonrasi hedefe ATOMIK tasir. aka.ms/fwlink yonlendirmelerini
// http.Client izler. Her deneme icin zaman asimi 30 dk.
func indir(is *KurulumIsi, url, hedef string) error {
	beklenen := indirmeSha[url]
	gecici := hedef + ".indiriliyor"
	etiket := filepath.Base(hedef) // canli ETA cubugunda gosterilen ad
	logSatiri(is, "indiriliyor: %s", url)
	var sonHata error
	for deneme := 1; deneme <= indirmeDenemeUst; deneme++ {
		if deneme > 1 {
			bekle := indirmeBekleTaban * time.Duration(int64(1)<<(deneme-2)) // 2,4,8
			logSatiri(is, "  indirme yeniden denenecek (%d/%d, %v sonra): %v", deneme, indirmeDenemeUst, bekle, sonHata)
			time.Sleep(bekle)
		}
		sonHata = indirDene(is, url, gecici, beklenen, etiket)
		if sonHata == nil {
			if err := os.Rename(gecici, hedef); err != nil {
				_ = os.Remove(gecici)
				return fmt.Errorf("indirilen dosya tasinamadi: %w", err)
			}
			logSatiri(is, "indirildi: %s", filepath.Base(hedef))
			return nil
		}
		if errors.Is(sonHata, errIndirmeKalici) {
			_ = os.Remove(gecici)
			return sonHata // 404/403/410: yeniden deneme fayda etmez
		}
	}
	_ = os.Remove(gecici) // son deneme de basarisiz: kalintiyi birakma
	return fmt.Errorf("indirme %d denemede basarisiz: %w", indirmeDenemeUst, sonHata)
}

// indirDene — TEK indirme denemesi. gecici dosyada kismi veri varsa Range ile
// SURDURUR (206); sunucu Range'i yoksayarsa (200) bastan indirir. Akista SHA-256
// hesaplar; beklenen BOS ise dogrulama atlanir (gunluge yazilir), doluysa
// uyusmazlik gecici dosyayi siler ve GECICI hata doner (disaridaki dongu bastan
// dener). Gecici→hedef atomik tasima cagirandadir.
func indirDene(is *KurulumIsi, url, gecici, beklenen, etiket string) error {
	if err := os.MkdirAll(filepath.Dir(gecici), 0o755); err != nil {
		return fmt.Errorf("indirme dizini acilamadi: %w", err)
	}
	// 🔴 Checksum YOKSA (aka.ms/fwlink yonlendirmeleri) kismi dosyayi SURDURME:
	// yeniden calismada hedef baytlari degismisse "bozuk dikis" olusur, dogrulanamaz
	// ve SYSTEM olarak calisir. Bastan indir.
	if beklenen == "" {
		_ = os.Remove(gecici)
	}
	var baslangic int64
	if fi, err := os.Stat(gecici); err == nil {
		baslangic = fi.Size()
	}
	ctx, iptal := context.WithTimeout(context.Background(), kurulumZamanAsimi)
	defer iptal()
	istek, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("%w: istek kurulamadi: %v", errIndirmeKalici, err)
	}
	if baslangic > 0 {
		istek.Header.Set("Range", fmt.Sprintf("bytes=%d-", baslangic))
	}
	yanit, err := http.DefaultClient.Do(istek)
	if err != nil {
		return fmt.Errorf("baglanti: %w", err) // gecici — retry
	}
	defer yanit.Body.Close()

	surdur := false
	switch yanit.StatusCode {
	case http.StatusOK:
		surdur = false // sunucu Range yoksaydi ya da ilk indirme
	case http.StatusPartialContent:
		surdur = true // 206 — surdurme kabul edildi
	case http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(gecici) // kismi bozuk/fazla; bastan
		return fmt.Errorf("range reddedildi (416), bastan denenecek")
	case http.StatusNotFound, http.StatusForbidden, http.StatusGone:
		return fmt.Errorf("%w: HTTP %d (%s)", errIndirmeKalici, yanit.StatusCode, url)
	default:
		return fmt.Errorf("HTTP %d (%s)", yanit.StatusCode, url) // 5xx vb — retry
	}

	h := sha256.New()
	bayrak := os.O_CREATE | os.O_WRONLY
	if surdur {
		// SHA butun dosya uzerinden dogru olsun diye mevcut kismi baytlari ONCE hashle.
		if r, e := os.Open(gecici); e == nil {
			_, _ = io.Copy(h, r)
			r.Close()
		}
		bayrak |= os.O_APPEND
	} else {
		bayrak |= os.O_TRUNC
		baslangic = 0
	}
	f, err := os.OpenFile(gecici, bayrak, 0o644)
	if err != nil {
		return fmt.Errorf("gecici dosya acilamadi: %w", err)
	}
	// Tam boyut: 206'da ContentLength KALAN bayttir (baslangic + kalan = tam);
	// 200'de tam boyuttur. Bilinmiyorsa (-1) 0 birak → UI belirsiz cubuk gosterir.
	boyut := int64(0)
	if yanit.ContentLength > 0 {
		if surdur {
			boyut = baslangic + yanit.ContentLength
		} else {
			boyut = yanit.ContentLength
		}
	}
	simdi := time.Now()
	rdr := &ilerlemeOkuyucu{
		r: yanit.Body, is: is, etiket: etiket,
		toplam: baslangic, boyut: boyut, sonRapor: baslangic,
		baslama: simdi, baslamaBayt: baslangic, sonGuncel: simdi,
	}
	n, err := io.Copy(io.MultiWriter(f, h), rdr)
	kapatma := f.Close()
	if err != nil {
		return fmt.Errorf("indirme yarim kaldi (%d bayt): %w", baslangic+n, err) // gecici — Range ile surer
	}
	if kapatma != nil {
		return fmt.Errorf("indirilen dosya kapatilamadi: %w", kapatma)
	}
	if beklenen != "" {
		bulunan := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(bulunan, beklenen) {
			_ = os.Remove(gecici) // bozuk indirme: sil, bastan dene
			return fmt.Errorf("checksum tutmadi (beklenen %s, bulunan %s)", beklenen, bulunan)
		}
		logSatiri(is, "  checksum dogrulandi (sha256 %s…, %.1f MB)", bulunan[:12], float64(baslangic+n)/(1<<20))
	} else {
		logSatiri(is, "  not: bu indirme icin checksum tanimli DEGIL (dogrulama atlandi) — %.1f MB", float64(baslangic+n)/(1<<20))
	}
	return nil
}

// zipAc — arsivi hedef dizine acar.
//
// 🔴 ZIPSLIP: "..\..\x.exe" gibi bir girdi hedef dizinin DISINA yazardi —
// arsiv sonucta internetten iniyor, surum sabit olsa da icerige koru korune
// guvenilmez. Her girdinin cozulmus yolu hedefin altinda kalmak ZORUNDA;
// disari cikan TEK girdi bile tum arsivi reddettirir (kismi acma yok).
func zipAc(is *KurulumIsi, zipYolu, hedefDizin string) error {
	r, err := zip.OpenReader(zipYolu)
	if err != nil {
		return fmt.Errorf("zip acilamadi: %w", err)
	}
	defer r.Close()
	if err := os.MkdirAll(hedefDizin, 0o755); err != nil {
		return fmt.Errorf("hedef dizin acilamadi: %w", err)
	}
	kok := filepath.Clean(hedefDizin) + string(os.PathSeparator)
	for _, g := range r.File {
		hedef := filepath.Join(hedefDizin, g.Name) // Join iceride Clean uygular
		if !strings.HasPrefix(hedef, kok) {
			return fmt.Errorf("zip girdisi hedef disina cikiyor, arsiv reddedildi: %q", g.Name)
		}
		if g.FileInfo().IsDir() {
			if err := os.MkdirAll(hedef, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(hedef), 0o755); err != nil {
			return err
		}
		if err := zipGirdiYaz(g, hedef); err != nil {
			return fmt.Errorf("%s cikarilamadi: %w", g.Name, err)
		}
	}
	logSatiri(is, "arsiv acildi: %d girdi -> %s", len(r.File), hedefDizin)
	return nil
}

func zipGirdiYaz(g *zip.File, hedef string) error {
	src, err := g.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(hedef)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// ── kurucular ───────────────────────────────────────────────────────────────

// kurucular — anahtar -> kurucu. "mail" BILEREK yok: karar-bekliyor.
var kurucular = map[string]func(*KurulumIsi) error{
	"iis":        kurIIS,
	"ftp":        kurFTP,
	"dns":        kurDNS,
	"dotnet":     kurDotNet,
	"ssl":        kurSSL,
	"mssql":      kurMSSQL,
	"mysql":      kurMySQL,
	"pgsql":      kurPgSQL,
	"node":       kurNode,
	"git":        kurGit,
	"rewrite":    kurRewrite,
	"redis":      kurRedis,
	"phpmyadmin": kurPhpMyAdmin,
}

// rolKur — Install-WindowsFeature kosar (yalniz Server SKU'da vardir; istemci
// Windows'ta hata gunluge duser, ajan zaten sunucu isletim sistemi hedefler).
// Success/RestartNeeded gunluge Format-List ile satir satir yazilir ve
// Success=False, cikis kodu 0 olsa bile BASARISIZLIK sayilir: cmdlet kismi
// hatada 0 donebilir, sonuc nesnesine bakmak sart. ozellikler parametresi
// YALNIZ asagidaki sabit cagrilardan gelir; kabuk birlestirme riski yok.
func rolKur(is *KurulumIsi, ozellikler string) error {
	// 🔴 ProgressPreference: ServerManager cmdlet'leri ilerleme cubugu cizmeye
	// calisir; konsolsuz baglamlarda (servis, SSH pty) bu "Access is denied
	// reading the console output buffer" ile CMDLET'I DUSURUR — gercek VM'de
	// SSH uzerinden birebir yakalandi (2026-09-03). Sessize almak sart.
	komut := "$ProgressPreference='SilentlyContinue'; $s = Install-WindowsFeature -Name " + ozellikler +
		"; $s | Format-List Success,RestartNeeded,ExitCode" +
		"; if (-not $s.Success) { exit 1 }"
	return kurulumKomutu(is, "powershell", "-NoProfile", "-Command", komut)
}

func kurIIS(is *KurulumIsi) error { return rolKur(is, "Web-Server -IncludeManagementTools") }
func kurFTP(is *KurulumIsi) error { return rolKur(is, "Web-Ftp-Server,Web-Mgmt-Console") }
func kurDNS(is *KurulumIsi) error { return rolKur(is, "DNS -IncludeManagementTools") }

// kurDotNet — .NET 8 Hosting Bundle: indir, sessiz kur, iisreset. Bundle
// /norestart ile IIS'i kendisi durdurmaz; ANCM modulunun yuklenmesi icin
// sondaki iisreset SART (yoksa modul ilk elle restart'a kadar gorunmez).
func kurDotNet(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "dotnet-hosting-win.exe")
	if err := indir(is, dotnetHostingURL, hedef); err != nil {
		return err
	}
	if err := kurulumKomutu(is, hedef, "/install", "/quiet", "/norestart"); err != nil {
		return err
	}
	return kurulumKomutu(is, "iisreset")
}

// kurSSL — win-acme'yi sabit surumden indirir ve acar; kurulum degil kopyalama
// oldugu icin en hizli kalemdir. Sonda wacs.exe'nin gercekten yerinde oldugu
// DOGRULANIR: zip yapisi bir gun degisirse "basarili ama kurulu gorunmuyor"
// tutarsizligi yerine acik hata verilsin.
func kurSSL(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "win-acme.zip")
	if err := indir(is, winAcmeURL, hedef); err != nil {
		return err
	}
	if err := zipAc(is, hedef, winAcmeDizin); err != nil {
		return err
	}
	wacs := filepath.Join(winAcmeDizin, "wacs.exe")
	if _, err := os.Stat(wacs); err != nil {
		return fmt.Errorf("arsiv acildi ama wacs.exe yerinde yok — zip yapisi beklenenden farkli: %w", err)
	}
	logSatiri(is, "win-acme hazir: %s", wacs)
	return nil
}

// kurMSSQL — 🔴 DENEYSEL ve bunu SAKLAMAYIZ: filoda henuz sinanmadi. SSEI
// onyukleyicisi kurulum medyasini kendisi indirip sessiz kurar; surec uzun
// surer ve dakikalarca cikti gelmeyebilir. Hata olursa gunluk yeter; otomatik
// telafi denemesi bilerek yok (yarim SQL kurulumunu korlemesine kurcalamak
// duzeltmez, bozar).
// kurMSSQL — 🔴 IKI ASAMALI. SSEI'nin `/ACTION=INSTALL /QUIET` modu asil SQL
// medyasini kendi indirmeye calisirken "Downloading install package... Failure"
// ile 1 saniyede DUSUYOR (gercek VM'de kanitlandi, 2026-09-03). Ama ayni
// bootstrapper'in `/ACTION=Download` modu 748 MB SQLEXPR medyasini SORUNSUZ
// indiriyor. Bu yuzden: (1) SSEI ile medyayi indir, (2) cikan SQLEXPR
// self-extracting installer'ini dogrudan setup parametreleriyle kur.
func kurMSSQL(is *KurulumIsi) error {
	// 🔴 IDEMPOTENT / ONARIM (denetim 01/1): motor zaten kuruluysa (yeniden
	// calistirma ya da onceki kosuda sqlcmd'nin yarim kalmasi) 750 MB medya
	// indirme + engine kurulumunu ATLA, dogrudan sqlcmd onarimina gec. Aksi
	// halde SQLEXPR /ACTION=Install "instance zaten var" ile patlar ve kismi
	// kurulum onarilamaz kalirdi.
	if algilananYetenekler().Var(YetMSSQL) {
		logSatiri(is, "SQL Server motoru zaten kurulu — engine kurulumu atlaniyor, yalniz sqlcmd denetlenip onarilacak")
		return mssqlSqlcmdOnar(is)
	}
	logSatiri(is, "deneysel: kurulum uzun surer (medya ~750 MB), cikti dakikalarca susabilir")
	ssei := filepath.Join(indirilenDizin, "SQL2022-SSEI-Expr.exe")
	if err := indir(is, mssqlURL, ssei); err != nil {
		return err
	}
	// 1. asama: medyayi indir. SSEI /ACTION=Download tek bir SQLEXPR_x64_ENU.exe
	// (self-extracting kurulum) uretir.
	medyaDizin := filepath.Join(indirilenDizin, "sqlmedya")
	logSatiri(is, "1/2 — SQL medyasi indiriliyor (SSEI /ACTION=Download, ~750 MB)")
	if err := kurulumKomutu(is, ssei, "/ACTION=Download", "/MEDIAPATH="+medyaDizin,
		"/MEDIATYPE=Core", "/QUIET", "/HIDEPROGRESSBAR"); err != nil {
		return fmt.Errorf("SQL medyasi indirilemedi: %w", err)
	}
	medya := filepath.Join(medyaDizin, "SQLEXPR_x64_ENU.exe")
	if _, err := os.Stat(medya); err != nil {
		return fmt.Errorf("indirilen medya bulunamadi (%s): %w", medya, err)
	}
	// 2. asama: sessiz kurulum. SQLEXPR self-extracting; setup parametreleri
	// dogrudan gecer. SYSADMIN = yerel Administrators (SID degil grup adi gecmez,
	// setup BUILTIN\Administrators kabul eder; TR/EN Windows'ta BUILTIN sabit).
	logSatiri(is, "2/2 — SQL Server kuruluyor (SQLEXPRESS ornegi)")
	// SQLBROWSERSVCSTARTUPTYPE=Automatic: named instance'a (SQLEXPRESS) TCP ile
	// baglanmak SQL Browser gerektirir; olmadan go-sqlcmd instance'i bulamaz.
	if err := kurulumKomutu(is, medya, "/Q", "/ACTION=Install", "/FEATURES=SQLENGINE",
		"/INSTANCENAME=SQLEXPRESS", "/SQLSYSADMINACCOUNTS=BUILTIN\\Administrators",
		"/TCPENABLED=1", "/SQLBROWSERSVCSTARTUPTYPE=Automatic",
		"/IACCEPTSQLSERVERLICENSETERMS"); err != nil {
		return err
	}
	// SQL Browser'i baslat (Automatic ama ilk kurulumda durabilir).
	_, _ = exec.Command("sc", "start", "SQLBrowser").CombinedOutput()

	// sqlcmd AYRI adim + AYRI/yeniden-denenebilir: motor kuruldu, simdi araci getir.
	return mssqlSqlcmdOnar(is)
}

// mssqlSqlcmdOnar — go-sqlcmd'yi sabit konuma indirir (yoksa). AYRI/idempotent
// adim: SQL Server Engine sqlcmd ILE GELMEZ ama vt yonetimi (liste/olustur/sil)
// buna dayanir. Bu adim kurMSSQL'in hem taze hem onarim yolundan cagrilir; motor
// kurulu olup sqlcmd yarim kaldiginda kurMSSQL yeniden kosunca YALNIZ burasi
// isler (750 MB medya tekrar inmez). sqlcmd zaten yerindeyse dokunmadan doner.
func mssqlSqlcmdOnar(is *KurulumIsi) error {
	if _, err := os.Stat(sqlcmdBilinenYol); err == nil {
		logSatiri(is, "sqlcmd zaten yerinde: "+sqlcmdBilinenYol)
		return nil
	}
	logSatiri(is, "sqlcmd araci indiriliyor (go-sqlcmd, ODBC'siz)")
	sqlcmdZip := filepath.Join(indirilenDizin, "go-sqlcmd.zip")
	if err := indir(is, gosqlcmdURL, sqlcmdZip); err != nil {
		return fmt.Errorf("sqlcmd indirilemedi: %w", err)
	}
	if err := zipAc(is, sqlcmdZip, filepath.Dir(sqlcmdBilinenYol)); err != nil {
		return fmt.Errorf("sqlcmd acilamadi: %w", err)
	}
	if _, err := os.Stat(sqlcmdBilinenYol); err != nil {
		return fmt.Errorf("sqlcmd cikarildi ama bulunamadi (%s): %w", sqlcmdBilinenYol, err)
	}
	logSatiri(is, "sqlcmd hazir: "+sqlcmdBilinenYol)
	return nil
}

// kurMySQL — DENEYSEL: msi yalniz MySQL Installer araci kurar, calisan bir
// MySQL ORNEGI kurmaz; ornek yapilandirmasi sonraki fazin isi. Bu gercek
// kullanicidan saklanmaz, gunluge acikca yazilir.
func kurMySQL(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "mysql-installer-community-8.0.40.0.msi")
	if err := indir(is, mysqlURL, hedef); err != nil {
		return err
	}
	if err := kurulumKomutu(is, "msiexec", "/i", hedef, "/qn"); err != nil {
		return err
	}
	logSatiri(is, "not: bu paket yalniz MySQL Installer'i kurar; calisan MySQL ORNEGI ayrica yapilandirilmali — sonraki faz")
	// 🔴 KISMI (B-07): adim hatasiz ama calisan ornek YOK. Isi `basarili` degil
	// `kismi` isaretle — operator "MySQL hazir" sanmasin.
	return fmt.Errorf("MySQL Installer araci kuruldu, calisan MySQL ornegi yok: %w", ErrKismiKurulum)
}

// kurPgSQL — DENEYSEL: EDB kurucusuyla katilimsiz kurulum, port 5432.
// pgOptionDosyasi — pg superuser parolasini BitRock --optionfile bicimiyle
// gecici bir dosyaya (0600) yazar; yolu dondurur. Cagiran kullandiktan HEMEN
// sonra siler. Parolayi komut satirindan uzak tutar (denetim 01/7).
func pgOptionDosyasi(dizin, parola string) (string, error) {
	suf, err := rasgeleHex(4)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dizin, 0o755); err != nil {
		return "", err
	}
	yol := filepath.Join(dizin, "pg-opt-"+suf+".ini")
	if err := os.WriteFile(yol, []byte("superpassword="+parola+"\n"), 0o600); err != nil {
		return "", err
	}
	return yol, nil
}

func kurPgSQL(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "postgresql-16.4-1-windows-x64.exe")
	if err := indir(is, pgsqlURL, hedef); err != nil {
		return err
	}
	parola, err := rasgeleHex(16) // 32 hex karakter (>=128 bit; B-11: eski 64-bit zayifti)
	if err != nil {
		return fmt.Errorf("postgres parolasi uretilemedi: %w", err)
	}
	// 🔴 PAROLA KOMUT SATIRINDA DEGIL (denetim 01/7): `--superpassword <deger>`
	// Windows'ta kurulum boyunca baska yerel surecce okunabilir
	// (Get-CimInstance Win32_Process -> CommandLine). Cozum: BitRock --optionfile
	// (0600, kullan+SIL) — parola yalniz kisa omurlu dosyada, cmdline'da GORUNMEZ.
	optYol, err := pgOptionDosyasi(indirilenDizin, parola)
	if err != nil {
		return fmt.Errorf("pg option dosyasi hazirlanamadi: %w", err)
	}
	defer os.Remove(optYol) // parola dosyasi kullanildiktan HEMEN sonra silinir
	// 🔴 CWE-532: parola GUNLUGE (LogSatirlari) YAZILMAZ — gunluk scrollback'te
	// kalir ve is bellekte oldukca her acilista yeniden gorunurdu. Bunun yerine
	// tek-seferlik SirCikti alanina konur; panel ayri "gizli cikti" alaninda BIR
	// KEZ gosterir. (Katilimsiz kurulumda operatore ulastirmanin tek yolu; kanal
	// oturumlu + TLS panel.)
	is.mu.Lock()
	is.SirCikti = parola
	is.mu.Unlock()
	logSatiri(is, "postgres superuser parolasi uretildi — panelde 'gizli cikti' alanindan BIR KEZ alip kaydedin")
	return kurulumKomutu(is, hedef, "--mode", "unattended", "--serverport", "5432", "--optionfile", optYol)
}

// ── katalog genisletme: kurulu-mu yardimcilari ───────────────────────────────

// rewriteDllYolu — URL Rewrite modulunun IIS'e kurdugu DLL yolu; varligi
// modulun kurulu oldugunun guvenilir isaretidir (appcmdYolu ile ayni WINDIR deseni).
func rewriteDllYolu() string {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	return filepath.Join(win, "System32", "inetsrv", "rewrite.dll")
}

// memuraiKuruluMu — redis (Memurai) kurulu mu: once ucuz kontroller (PATH,
// bilinen exe), en son servis sorgusu. Katalog insan hiziyla cekildigi icin
// buradaki tek sc cagrisi surec firtinasi yaratmaz (algilananYetenekler'in
// aksine ayri onbellek gerekmez).
func memuraiKuruluMu() bool {
	if _, err := exec.LookPath("memurai"); err == nil {
		return true
	}
	if _, err := os.Stat(`C:\Program Files\Memurai\memurai.exe`); err == nil {
		return true
	}
	// sc query 0 donerse servis kayitli (kurulu degilse 1060 doner).
	return exec.Command("sc", "query", "Memurai").Run() == nil
}

// ── katalog genisletme: kurucular ────────────────────────────────────────────

// kurNode — Node.js LTS MSI'i sessiz kurar.
func kurNode(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "node-v20.18.1-x64.msi")
	if err := indir(is, nodeURL, hedef); err != nil {
		return err
	}
	return kurulumKomutu(is, "msiexec", "/i", hedef, "/qn", "/norestart")
}

// kurGit — Git for Windows kurucusunu katilimsiz kurar (Inno Setup bayraklari).
func kurGit(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "Git-2.47.1-64-bit.exe")
	if err := indir(is, gitURL, hedef); err != nil {
		return err
	}
	return kurulumKomutu(is, hedef, "/VERYSILENT", "/NORESTART")
}

// kurRewrite — IIS URL Rewrite Module MSI'i sessiz kurar.
func kurRewrite(is *KurulumIsi) error {
	hedef := filepath.Join(indirilenDizin, "rewrite_amd64_en-US.msi")
	if err := indir(is, rewriteURL, hedef); err != nil {
		return err
	}
	return kurulumKomutu(is, "msiexec", "/i", hedef, "/qn", "/norestart")
}

// kurRedis — 🔴 DENEYSEL ve SAKLANMAZ: Windows'ta RESMI Redis yoktur; Memurai
// (Redis-uyumlu) MSI'i denenir. Surum URL'i SABIT ama filoda sinanmadi — 404
// gelirse indir() acik hata verir, deneysel kademe bunu KABUL eder.
func kurRedis(is *KurulumIsi) error {
	logSatiri(is, "deneysel: Windows'ta resmi Redis yok; Memurai (Redis-uyumlu) denenecek")
	hedef := filepath.Join(indirilenDizin, "Memurai-Developer.msi")
	if err := indir(is, redisMemuraiURL, hedef); err != nil {
		return err
	}
	return kurulumKomutu(is, "msiexec", "/i", hedef, "/qn", "/norestart")
}

// kurPhpMyAdmin — 🔴 DENEYSEL: phpMyAdmin PHP + IIS ISTER; bu kurucu YALNIZ
// dosyalari indirip acar, PHP KURMAZ — calismasi PHP'ye baglidir (kalem
// aciklamasinda belirtildi). Zip kendi ust klasoruyle acilir; IIS o ic klasore
// yonlendirilmeli.
func kurPhpMyAdmin(is *KurulumIsi) error {
	logSatiri(is, "deneysel: phpMyAdmin dosyalari aciliyor; calismasi icin PHP kurulu olmali")
	hedef := filepath.Join(indirilenDizin, "phpMyAdmin-5.2.1-all-languages.zip")
	if err := indir(is, phpMyAdminURL, hedef); err != nil {
		return err
	}
	if err := zipAc(is, hedef, phpMyAdminDizin); err != nil {
		return err
	}
	// 🔴 KISMI (B-07): dosyalar acildi ama PHP olmadan CALISMAZ. Isi `basarili`
	// degil `kismi` isaretle — operator "phpMyAdmin hazir" sanmasin.
	return fmt.Errorf("phpMyAdmin dosyalari acildi, calismasi icin PHP+IIS yapilandirmasi gerekli: %w", ErrKismiKurulum)
}
