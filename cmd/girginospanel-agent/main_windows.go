//go:build windows

// girginospanel-agent — Windows tarafinin AYRI, KENDINI KURAN binary'si.
//
// 🔴 NEDEN AYRI BINARY: kullanicinin sarti "Linux'ta bir sey bozarsak Windows
// bozulmasin". Panel binary'sine gomulseydi her Linux yayini Windows kodunu da
// tasir, ayni artefakti paylasirlardi. Ayri binary + ayri kanal (platform.Kanal)
// ile iki taraf ancak kendi yayiniyla degisir.
//
// 🔴 TLS: ajan kendi self-signed ECDSA P-256 sertifikasiyla HTTPS dinler
// (C:\ProgramData\girginospanel\ajan.crt + ajan.key, ilk calistirmada uretilir).
// Panel sertifikayi CA zinciriyle degil SHA256 parmak iziyle dogrular (TOFU
// igneleme); parmak izi `parmak-izi` komutuyla ve loglarla goruntulenebilir.
//
// 🔴 IKI KAPI, TEK SUREC (0.3.0): 8460 merkezi ajan API'si (gPanel X-Gosp-Jeton
// ile konusur) AYNEN KALIR; 8443 YEREL WEB PANELI (gomulu statik UI +
// /api/yerel/*, cerez oturumu) — panele kayitli olmayan, tek basina calisan
// sunucunun tarayici arayuzu; ayrinti yerelpanel_windows.go. Iki kapi AYNI
// TLS sertifikasini paylasir; servis durunca ikisi de kapanir.
//
// Komutlar:
//
//	girginospanel-agent kur           — kendini C:\Program Files\GirginOSPanel'e
//	                                    kopyalar, jeton + panel parolasi uretir,
//	                                    Windows SERVISI olarak kurar + baslatir
//	                                    + firewall kurallari (8460 ve 8443)
//	girginospanel-agent kaldir        — servisi + firewall kurallarini kaldirir
//	                                    (config/sertifika BILEREK kalir: yeniden
//	                                    kurulumda kimlik/igne korunur)
//	girginospanel-agent kendini-sina  — hostu kanitla: gercek IIS yoluyla sinama
//	                                    sitesi ac/dogrula/sil, muhur bas
//	girginospanel-agent panel-parola  — yerel panel 'admin' parolasini yeniler
//	                                    ve duz halini BIR KEZ ekrana basar
//	girginospanel-agent parmak-izi    — sertifika SHA256 parmak izini yazdir
//	girginospanel-agent               — on planda calistir (gelistirme)
//	girginospanel-agent hizmet        — SCM tarafindan cagrilir (elle kullanma)
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sys/windows/svc"

	"girginospanel/internal/platform"
)

// sertifikaDizini — sertifika/config/log evi. ProgramData: servis hangi hesapla
// kosarsa kossun sabit; kullanici profiline bagli degil.
const sertifikaDizini = `C:\ProgramData\girginospanel`

const (
	kurulumDizini   = `C:\Program Files\GirginOSPanel`
	exeAdi          = "girginospanel-agent.exe"
	servisAdi       = "GirginOSPanelAgent"
	fwKuralAdi      = "GirginOSPanel Agent 8460"
	fwPanelKuralAdi = "GirginOSPanel Yerel Panel 8443"
	varsayilanKapi  = "0.0.0.0:8460"
)

// ajanAyar — servis modunda ortam degiskeni tasinamayacagi icin jeton ve adres
// dosyada durur. Ortam degiskeni (gelistirme icin) dosyayi EZER.
type ajanAyar struct {
	Jeton string `json:"jeton"`
	Adres string `json:"adres"`
	// PanelParolaHash — yerel panelin (8443) 'admin' parolasinin bcrypt hash'i.
	// Duz parola HICBIR YERDE saklanmaz: kur/panel-parola uretir, BIR KEZ
	// ekrana basar. Ortam degiskeni karsiligi bilerek yok — hash dosyada durur,
	// dosya icacls ile SYSTEM+Administrators'a kilitlidir.
	PanelParolaHash string `json:"panel_parola_hash"`
}

func ayarYolu() string { return filepath.Join(sertifikaDizini, "ajan.json") }

func ayarYukle() (ajanAyar, error) {
	var a ajanAyar
	if b, err := os.ReadFile(ayarYolu()); err == nil {
		_ = json.Unmarshal(b, &a)
	}
	if v := os.Getenv("GOSP_AGENT_JETON"); v != "" {
		a.Jeton = v
	}
	if v := os.Getenv("GOSP_AGENT_ADRES"); v != "" {
		a.Adres = v
	}
	if a.Adres == "" {
		a.Adres = varsayilanKapi
	}
	if a.Jeton == "" {
		return a, fmt.Errorf("jeton yok: `girginospanel-agent kur` kosun ya da GOSP_AGENT_JETON verin")
	}
	return a, nil
}

func main() {
	komut := ""
	if len(os.Args) > 1 {
		komut = os.Args[1]
	}

	switch komut {
	case "kendini-sina":
		if err := platform.KendiniSina(); err != nil {
			fmt.Fprintln(os.Stderr, "kendini-sina BASARISIZ:", err)
			os.Exit(1)
		}
		fmt.Println("kendini-sina GECTI — YetSite bu hostta acildi (surum " + platform.Surum + ")")
		return
	case "parmak-izi":
		sert, err := sertifikaYukle()
		if err != nil {
			fmt.Fprintln(os.Stderr, "sertifika hazirlanamadi:", err)
			os.Exit(1)
		}
		oz := sha256.Sum256(sert.Certificate[0])
		fmt.Println(hex.EncodeToString(oz[:]))
		return
	case "kur":
		if err := kur(); err != nil {
			fmt.Fprintln(os.Stderr, "KURULUM BASARISIZ:", err)
			os.Exit(1)
		}
		return
	case "kaldir":
		if err := kaldir(); err != nil {
			fmt.Fprintln(os.Stderr, "KALDIRMA BASARISIZ:", err)
			os.Exit(1)
		}
		return
	case "panel-parola":
		if err := panelParolaSifirla(); err != nil {
			fmt.Fprintln(os.Stderr, "PANEL PAROLASI YENILENEMEDI:", err)
			os.Exit(1)
		}
		return
	case "surum":
		// Makine-okunur surum satiri: "<surum> <kanal>". guncelle bunu hem
		// yeni binary'nin CALISTIGININ kaniti (on-sinama) hem surum tespiti icin
		// kullanir; disaridan da betiklenebilir.
		fmt.Println(platform.Surum + " " + platform.Kanal)
		return
	case "guncelle":
		yeni := ""
		if len(os.Args) > 2 {
			yeni = os.Args[2]
		}
		if yeni == "" {
			fmt.Fprintln(os.Stderr, "kullanim: girginospanel-agent guncelle <yeni-exe-yolu>")
			os.Exit(1)
		}
		if err := guncelle(yeni); err != nil {
			fmt.Fprintln(os.Stderr, "GUNCELLEME:", err)
			os.Exit(1)
		}
		return
	case "kurulum-kilit-temizle":
		// Operator CLI'i: ajan restart'inda yarim kalan (asili) kurulum kilidini
		// temizler. Calisan bir yukleyici olmadigindan emin olduktan sonra kosun.
		platform.KurulumAsiliTemizle()
		fmt.Println("asili kurulum kilidi temizlendi (varsa) — yeni kurulumlar acildi.")
		return
	}

	// SCM altinda miyiz? (binPath "hizmet" der ama otomatik algi da var —
	// yanlis binPath yazilsa bile servis 30 sn'de SCM tarafindan oldurulmesin.)
	servisModu, _ := svc.IsWindowsService()
	if komut == "hizmet" || servisModu {
		servisKos()
		return
	}
	onPlandaKos()
}

// ── kurulum / kaldirma ──────────────────────────────────────────────────────

func yoneticiMi() bool { return exec.Command("net", "session").Run() == nil }

func kos(ad string, arg ...string) (string, error) {
	out, err := exec.Command(ad, arg...).CombinedOutput()
	return string(out), err
}

func jetonUret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func kur() error {
	if !yoneticiMi() {
		return fmt.Errorf("yonetici (elevated) PowerShell/CMD gerekli — 'Run as administrator'")
	}
	fmt.Println("GirginOSPanel Windows Ajani — kurulum (" + platform.Surum + " / " + platform.Kanal + ")")

	// 1) dizinler + kendini kopyala
	if err := os.MkdirAll(kurulumDizini, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(sertifikaDizini, 0o755); err != nil {
		return err
	}
	kendi, err := os.Executable()
	if err != nil {
		return err
	}
	hedef := filepath.Join(kurulumDizini, exeAdi)
	if !ayniDosyaMi(kendi, hedef) {
		if err := kopyala(kendi, hedef); err != nil {
			return fmt.Errorf("exe kopyalanamadi: %w", err)
		}
	}

	// 2) jeton: config'te varsa KORU (yeniden kurulum paneli kirmasin), yoksa uret
	var ayar ajanAyar
	if b, e := os.ReadFile(ayarYolu()); e == nil {
		_ = json.Unmarshal(b, &ayar)
	}
	yeniJeton := false
	if ayar.Jeton == "" {
		if ayar.Jeton, err = jetonUret(); err != nil {
			return err
		}
		yeniJeton = true
	}
	if ayar.Adres == "" {
		ayar.Adres = varsayilanKapi
	}
	// 2b) yerel panel parolasi: hash yoksa uret. Duz hali YALNIZ ekranda BIR KEZ
	// gorunur, diske sadece bcrypt hash'i yazilir. Hash varsa KORU — yeniden
	// kurulum operatorun bildigi parolayi sessizce degistirmesin (jetonla ayni
	// disiplin); bilincli sifirlama icin `panel-parola` komutu var.
	panelParola := ""
	if ayar.PanelParolaHash == "" {
		if panelParola, err = panelParolaUret(); err != nil {
			return err
		}
		h, err := bcrypt.GenerateFromPassword([]byte(panelParola), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("panel parolasi hashlenemedi: %w", err)
		}
		ayar.PanelParolaHash = string(h)
	}
	b, _ := json.MarshalIndent(ayar, "", "  ")
	if err := os.WriteFile(ayarYolu(), b, 0o600); err != nil {
		return fmt.Errorf("ayar yazilamadi: %w", err)
	}
	// 🔴 ProgramData varsayilaninda Users OKUYABILIR — jeton dosyasini yalniz
	// SYSTEM + Administrators gorebilir hale getir. (SID kullaniliyor: S-1-5-18
	// SYSTEM, S-1-5-32-544 Administrators — yerellestirilmis grup adlari
	// Turkce/Ingilizce kurulumda degisir, SID degismez.)
	if out, e := kos("icacls", ayarYolu(), "/inheritance:r",
		"/grant", "*S-1-5-18:F", "/grant", "*S-1-5-32-544:F"); e != nil {
		return fmt.Errorf("jeton dosyasi kilitlenemedi: %v — %s", e, out)
	}
	// 🔴 TUM DIZINI kilitle: ajan.key (TLS OZEL ANAHTARI)/ajan.crt/log/indirilenler
	// C:\ProgramData\girginospanel altinda. Windows'ta os.WriteFile 0o600 ACL
	// DEGISTIRMEZ (yalniz read-only biti) → dosyalar ProgramData varsayilaninda
	// Users'a OKUNUR. (OI)(CI) ile sonradan yazilan cert/key de miras alir.
	if out, e := kos("icacls", sertifikaDizini, "/inheritance:r",
		"/grant", "*S-1-5-18:(OI)(CI)F", "/grant", "*S-1-5-32-544:(OI)(CI)F"); e != nil {
		return fmt.Errorf("veri dizini kilitlenemedi: %v — %s", e, out)
	}

	// 3) firewall: panelin ozel agdan 8460'a ulasabilmesi icin gelen kural
	_, _ = kos("netsh", "advfirewall", "firewall", "delete", "rule", "name="+fwKuralAdi)
	if out, e := kos("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+fwKuralAdi, "dir=in", "action=allow", "protocol=TCP", "localport=8460"); e != nil {
		return fmt.Errorf("firewall kurali: %v — %s", e, out)
	}
	// 3b) yerel panel (8443) icin AYRI kural: operator panel kapisini merkezi
	// API kapisindan bagimsiz acip kapatabilsin (ayni delete+add deseni).
	_, _ = kos("netsh", "advfirewall", "firewall", "delete", "rule", "name="+fwPanelKuralAdi)
	if out, e := kos("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+fwPanelKuralAdi, "dir=in", "action=allow", "protocol=TCP", "localport=8443"); e != nil {
		return fmt.Errorf("panel firewall kurali: %v — %s", e, out)
	}

	// 4) servis: varsa durdur+sil, temiz kur. Cokme durumunda 5 sn'de yeniden
	// baslar (failure actions) — kamu sahasinda kimse elle baslatamaz.
	_, _ = kos("sc", "stop", servisAdi)
	_, _ = kos("sc", "delete", servisAdi)
	time.Sleep(1 * time.Second)
	if out, e := kos("sc", "create", servisAdi,
		"binPath=", "\""+hedef+"\" hizmet", "start=", "auto",
		"DisplayName=", "GirginOSPanel Agent"); e != nil {
		return fmt.Errorf("servis olusturulamadi: %v — %s", e, out)
	}
	_, _ = kos("sc", "description", servisAdi, "GirginOSPanel Windows ajani ("+platform.Kanal+")")
	_, _ = kos("sc", "failure", servisAdi, "reset=", "86400", "actions=", "restart/5000/restart/5000/restart/5000")
	if out, e := kos("sc", "start", servisAdi); e != nil {
		return fmt.Errorf("servis baslatilamadi: %v — %s", e, out)
	}

	// 5) sertifika parmak izi (yoksa simdi uretilir — servisle ayni dosyalar)
	sert, err := sertifikaYukle()
	if err != nil {
		return fmt.Errorf("sertifika: %w", err)
	}
	oz := sha256.Sum256(sert.Certificate[0])

	fmt.Println()
	fmt.Println("KURULUM TAMAM ✓")
	fmt.Println("  Servis          :", servisAdi, "(otomatik, cokmede 5 sn'de yeniden baslar)")
	fmt.Println("  Dinleme         :", ayar.Adres, "(TLS)")
	if yeniJeton {
		fmt.Println("  JETON (YENI)    :", ayar.Jeton)
		fmt.Println("                    Bu jetonu panelde 'Windows Sunucular > Ajan Ekle' alanina girin.")
	} else {
		fmt.Println("  JETON           : mevcut jeton KORUNDU (" + ayarYolu() + ")")
	}
	fmt.Println("  Parmak izi      :", hex.EncodeToString(oz[:]))
	fmt.Println("  Log             :", filepath.Join(sertifikaDizini, "ajan.log"))
	// Yerel panel giris bilgisi: duz parola yalniz YENI uretildiyse basilir
	// (bir daha gosterilmez); korunduysa sifirlama yolu hatirlatilir.
	if panelParola != "" {
		fmt.Println("  PANEL GIRISI    : https://" + panelAdresi() + ":8443  kullanici: admin  parola: " + panelParola)
	} else {
		fmt.Println("  Panel parolasi  : KORUNDU (sifirlamak icin: panel-parola)")
	}
	fmt.Println()
	fmt.Println("SONRAKI ADIMLAR:")
	fmt.Println("  1) IIS kuruluysa hostu kanitla:  \"" + hedef + "\" kendini-sina")
	fmt.Println("  2) Panelde ekle: Sunucu Yonetimi > Windows Sunucular > Ajan Ekle")
	fmt.Println("     (adres: bu makinenin OZEL ag IP'si + :8460)")
	return nil
}

func kaldir() error {
	if !yoneticiMi() {
		return fmt.Errorf("yonetici (elevated) gerekli")
	}
	_, _ = kos("sc", "stop", servisAdi)
	if out, e := kos("sc", "delete", servisAdi); e != nil {
		return fmt.Errorf("servis silinemedi: %v — %s", e, out)
	}
	_, _ = kos("netsh", "advfirewall", "firewall", "delete", "rule", "name="+fwKuralAdi)
	_, _ = kos("netsh", "advfirewall", "firewall", "delete", "rule", "name="+fwPanelKuralAdi)
	fmt.Println("Servis ve firewall kurallari (8460, 8443) kaldirildi.")
	fmt.Println("Config + sertifika BILEREK birakildi (" + sertifikaDizini + "):")
	fmt.Println("yeniden kurulumda ayni jeton ve ayni parmak izi korunur, paneldeki kayit kirilmaz.")
	return nil
}

// panelParolaUret — 12 bayt rastgelelik, hex (24 karakter): elle yazilabilir
// ama kaba kuvvetle taranamaz.
func panelParolaUret() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// panelParolaSifirla — `panel-parola` komutu: yeni parola uretir, bcrypt
// hash'ini ayara yazar, duz halini BIR KEZ ekrana basar.
//
// 🔴 Ayar dosyasi DOGRUDAN okunur (ayarYukle DEGIL): ayarYukle jeton yoksa
// hata verir ve ortam degiskeni ezmelerini uygular — ezilmis degerlerin
// diske geri yazilmasi config'i bozardi (kur ile ayni disiplin).
// Calisan servise restart GEREKMEZ: giris ucu hash'i her denemede dosyadan
// taze okur (bkz. yerelpanel_windows.go girisUcu).
func panelParolaSifirla() error {
	if !yoneticiMi() {
		return fmt.Errorf("yonetici (elevated) gerekli")
	}
	b, err := os.ReadFile(ayarYolu())
	if err != nil {
		return fmt.Errorf("ayar okunamadi (%s) — once `girginospanel-agent kur` kosun: %w", ayarYolu(), err)
	}
	var ayar ajanAyar
	if err := json.Unmarshal(b, &ayar); err != nil {
		return fmt.Errorf("ayar cozulemedi: %w", err)
	}
	parola, err := panelParolaUret()
	if err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(parola), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("parola hashlenemedi: %w", err)
	}
	ayar.PanelParolaHash = string(h)
	y, _ := json.MarshalIndent(ayar, "", "  ")
	if err := os.WriteFile(ayarYolu(), y, 0o600); err != nil {
		return fmt.Errorf("ayar yazilamadi: %w", err)
	}
	fmt.Println("Yerel panel parolasi YENILENDI:")
	fmt.Println("  kullanici : admin")
	fmt.Println("  parola    : " + parola)
	fmt.Println("Yeni parola HEMEN gecerli (servis restart gerekmez); acik oturumlar")
	fmt.Println("dusmez, en gec 3 saatlik omurleri dolunca kapanir.")
	return nil
}

// ilkIPv4 — arayuzlerdeki ilk global unicast IPv4 (ozel ag adresleri dahil;
// yonetim duzlemi zaten ozel agda). Loopback/link-local elenir.
func ilkIPv4() string {
	adresler, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range adresler {
		agAdr, ok := a.(*net.IPNet)
		if !ok || !agAdr.IP.IsGlobalUnicast() {
			continue
		}
		if v4 := agAdr.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

// panelAdresi — kur ciktisindaki panel adresi: ilk IPv4; bulunamazsa
// operatorun kendisinin dolduracagi yer tutucu.
func panelAdresi() string {
	if ip := ilkIPv4(); ip != "" {
		return ip
	}
	return "SUNUCU-IP"
}

func ayniDosyaMi(a, b string) bool {
	ai, e1 := os.Stat(a)
	bi, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(ai, bi)
}

func kopyala(kaynak, hedef string) error {
	in, err := os.Open(kaynak)
	if err != nil {
		return err
	}
	defer in.Close()
	// Calisan servisin exe'si kilitli olabilir: once durdur, sonra yaz.
	_, _ = kos("sc", "stop", servisAdi)
	time.Sleep(1 * time.Second)
	out, err := os.Create(hedef)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close() // 🔴 flush/close hatasi = kopya BASARISIZ (truncation gizleme)
}

// ── servis / on plan calisma ────────────────────────────────────────────────

// gospServis — SCM el sikismasi. Bu OLMADAN calisan konsol uygulamasini SCM
// 30 saniyede "yanit vermiyor" diye oldurur; sorun tam olarak bu tiple cozulur.
// srvler[0] merkezi ajan API'si (8460), srvler[1] yerel panel (8443):
// Stop/Shutdown IKISINI de kapatir.
type gospServis struct{ srvler [2]*http.Server }

func (g *gospServis) Execute(args []string, istek <-chan svc.ChangeRequest, durum chan<- svc.Status) (bool, uint32) {
	durum <- svc.Status{State: svc.StartPending}
	hata := make(chan error, 1)
	go func() { hata <- sunucuKos(&g.srvler) }()
	durum <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case e := <-hata:
			log.Printf("sunucu kapandi: %v", e)
			durum <- svc.Status{State: svc.StopPending}
			return false, 1
		case c := <-istek:
			switch c.Cmd {
			case svc.Interrogate:
				durum <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				durum <- svc.Status{State: svc.StopPending}
				for _, s := range g.srvler {
					if s != nil {
						_ = s.Close()
					}
				}
				return false, 0
			}
		}
	}
}

func servisKos() {
	// SCM altinda stdout kaybolur — log dosyaya akar; E2E ayiklamada tek iz bu.
	_ = os.MkdirAll(sertifikaDizini, 0o755)
	if f, err := os.OpenFile(filepath.Join(sertifikaDizini, "ajan.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
	if err := svc.Run(servisAdi, &gospServis{}); err != nil {
		log.Fatalf("servis calistirilamadi: %v", err)
	}
}

func onPlandaKos() {
	var srvler [2]*http.Server
	if err := sunucuKos(&srvler); err != nil {
		log.Fatal(err)
	}
}

// sunucuKos — iki HTTPS sunucusunu kurar ve dinler. Hem servis hem on plan
// modu AYNI yolu kosar; iki modda farkli davranis = ayiklanamayan hata demektir.
//
// srvOut[0] merkezi ajan API'si (8460, X-Gosp-Jeton), srvOut[1] yerel panel
// (8443, cerez oturumu — bkz. yerelpanel_windows.go). Panel AYRI goroutine'de
// kosar: 8443 dolu olsa bile merkezi API ayakta kalir. Merkezi sunucu kapaninca
// fonksiyon doner; servis tarafi (Execute) iki sunucuyu da Close eder.
func sunucuKos(srvOut *[2]*http.Server) error {
	ayar, err := ayarYukle()
	if err != nil {
		return err
	}
	jeton := ayar.Jeton

	p := platform.Aktif()
	if err := p.Dogrula(); err != nil {
		// Ortam bozuksa yine ayaga kalkariz ama /saglik bunu ACIKCA soyler;
		// panel "erisilemez" ile "hazir degil"i ayirt edebilmeli.
		log.Printf("UYARI — ortam dogrulamasi: %v", err)
	}
	// 🔴 DISK KILIDI (B-05): onceki kurulum ajan restart'inda yarim kaldiysa
	// diskteki marker "asili" olarak yuklenir; yeni kurulumlar operator temizleyene
	// kadar reddedilir (korlemesine ikinci kurulum = CBS/MSI cakismasi).
	platform.KurulumAsiliYukle()
	if asili, anah := platform.KurulumAsiliVar(); asili {
		log.Printf("🔴 UYARI — onceki kurulum (%s) YARIM KALDI (asili); yeni kurulumlar temizlenene kadar reddedilecek", anah)
	}

	yetkili := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			jt := r.Header.Get("X-Gosp-Jeton")
			// 🔴 sabit-zamanli karsilastirma (timing side-channel; bcrypt yolu ile tutarli)
			if len(jt) != len(jeton) || subtle.ConstantTimeCompare([]byte(jt), []byte(jeton)) != 1 {
				http.Error(w, "yetkisiz", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	yazJSON := func(w http.ResponseWriter, kod int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(kod)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/saglik", yetkili(func(w http.ResponseWriter, r *http.Request) {
		ortam := ""
		if err := p.Dogrula(); err != nil {
			ortam = err.Error()
		}
		yazJSON(w, http.StatusOK, map[string]any{
			"platform":    p.Ad(),
			"surum":       platform.Surum,
			"kanal":       platform.Kanal,
			"yetenekler":  uint32(p.Yetenekler()),
			"ortam_hata":  ortam,
			"sinanmis_mi": p.Yetenekler().Var(platform.YetSite),
		})
	}))

	// POST /site  {"alan_adi": "..."}         -> site ac
	// DELETE /site {"alan_adi": "...", "sistem_kullanici": "..."} -> site sil
	mux.HandleFunc("/site", yetkili(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var ist struct {
				AlanAdi  string `json:"alan_adi"`
				PHPSurum string `json:"php_surum"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
				zarfYaz(w, r, http.StatusBadRequest, KodGovde, "govde okunamadi", false)
				return
			}
			son, err := p.SiteOlustur(platform.SiteIstek{AlanAdi: ist.AlanAdi, PHPSurum: ist.PHPSurum})
			if err != nil {
				zarfYaz(w, r, http.StatusUnprocessableEntity, KodDesteklenmiyor, err.Error(), false)
				return
			}
			log.Printf("site acildi: %s (%s)", ist.AlanAdi, son.SistemKullanici)
			yazJSON(w, http.StatusOK, son)
		case http.MethodDelete:
			var ist struct {
				AlanAdi         string `json:"alan_adi"`
				SistemKullanici string `json:"sistem_kullanici"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
				zarfYaz(w, r, http.StatusBadRequest, KodGovde, "govde okunamadi", false)
				return
			}
			if err := p.SiteSil(platform.SiteKimlik{AlanAdi: ist.AlanAdi, SistemKullanici: ist.SistemKullanici}); err != nil {
				zarfYaz(w, r, http.StatusUnprocessableEntity, KodDesteklenmiyor, err.Error(), false)
				return
			}
			log.Printf("site silindi: %s", ist.AlanAdi)
			yazJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			zarfYaz(w, r, http.StatusMethodNotAllowed, KodYontem, "yontem desteklenmiyor", false)
		}
	}))

	// GET /olaylar?gunluk=System&adet=50  -> {"olaylar":[...]}  (salt okunur)
	// gunluk zorunlu (System|Application|Security), adet istege bagli (vars. 50).
	// Beyaz liste + 1..200 kirpma platform.OlaylariOku'da; burada CIFT dogrulama
	// yok, yalniz sorgu dizgisi tasinir. ErrGecersizIstek -> 400, gerisi 500.
	mux.HandleFunc("/olaylar", yetkili(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			zarfYaz(w, r, http.StatusMethodNotAllowed, KodYontem, "yontem desteklenmiyor", false)
			return
		}
		adet := 50
		if v := r.URL.Query().Get("adet"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				zarfYaz(w, r, http.StatusBadRequest, KodGecersizIstek, "adet sayi olmali", false)
				return
			}
			adet = n
		}
		olaylar, err := platform.OlaylariOku(r.URL.Query().Get("gunluk"), adet)
		if err != nil {
			kod := http.StatusInternalServerError
			zkod := KodIcHata
			if errors.Is(err, platform.ErrGecersizIstek) {
				kod = http.StatusBadRequest
				zkod = KodGecersizIstek
			}
			zarfYaz(w, r, kod, zkod, err.Error(), kod >= 500)
			return
		}
		yazJSON(w, http.StatusOK, map[string]any{"olaylar": olaylar})
	}))

	// GET /gorevler?tumu=0|1  -> {"gorevler":[...]}  (salt okunur)
	// tumu=1: \Microsoft\ altindaki OS ic gorevleri de dahil; varsayilan 0.
	mux.HandleFunc("/gorevler", yetkili(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			zarfYaz(w, r, http.StatusMethodNotAllowed, KodYontem, "yontem desteklenmiyor", false)
			return
		}
		gorevler, err := platform.GorevleriOku(r.URL.Query().Get("tumu") == "1")
		if err != nil {
			zarfYaz(w, r, http.StatusInternalServerError, KodIcHata, err.Error(), true)
			return
		}
		yazJSON(w, http.StatusOK, map[string]any{"gorevler": gorevler})
	}))

	sert, err := sertifikaYukle()
	if err != nil {
		return fmt.Errorf("TLS sertifikasi hazirlanamadi: %w", err)
	}
	// Yaprak sertifikanin SHA256'si = panelin igneledigi deger.
	parmakIzi := sha256.Sum256(sert.Certificate[0])

	srv := &http.Server{
		Addr:    ayar.Adres,
		Handler: mux,
		TLSConfig: &tls.Config{
			// 🔴 TLS 1.2 taban — eski protokol pazarligina kapali.
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{sert},
		},
	}
	// Yerel panel (8443) — ayni surec, AYNI sertifika; bkz. yerelpanel_windows.go.
	oturumlar.yukle()              // restart'ta oturumlar DUSMESIN (kalici depo)
	go oturumlar.periyodikKaydet() // sliding tazelemelerini periyodik diske yaz
	panelSrv := yerelPanelSunucu(ayar, sert)
	if srvOut != nil {
		srvOut[0] = srv
		srvOut[1] = panelSrv
	}
	log.Printf("girginospanel-agent %s (%s) TLS dinliyor: %s", platform.Surum, platform.Kanal, ayar.Adres)
	log.Printf("sertifika SHA256 parmak izi: %s", hex.EncodeToString(parmakIzi[:]))
	log.Printf("yerel panel: https://%s", panelSrv.Addr)
	// 🔴 Yerel panel IKINCI goroutine'de: 8443 dolu ya da panel hatali olsa
	// bile merkezi ajan API'si (8460) calismaya devam eder — panel katmani
	// cekirdegi asla dusurmez.
	go func() {
		if err := panelSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("UYARI — yerel panel sunucusu kapandi: %v", err)
		}
	}()
	// Sertifika TLSConfig.Certificates'ten gelir; dosya parametreleri o yuzden bos.
	return srv.ListenAndServeTLS("", "")
}

// ── sertifika ───────────────────────────────────────────────────────────────

// sertifikaYukle — ajan.crt/ajan.key ciftini yukler; yoksa ILK calistirmada uretir.
//
// 🔴 CIFT VARSA ASLA YENIDEN URETME: panel bu sertifikanin SHA256 parmak izini
// TOFU ile DB'ye igneledi. Yeni sertifika = yeni parmak izi = paneldeki igne
// KIRILIR, ajan "sertifika degisti" diye reddedilir. Yenileme ancak bilincli
// yapilir: operator iki dosyayi da siler, ajani baslatir, panele YENIDEN kaydeder.
func sertifikaYukle() (tls.Certificate, error) {
	crtYol := filepath.Join(sertifikaDizini, "ajan.crt")
	keyYol := filepath.Join(sertifikaDizini, "ajan.key")

	_, crtErr := os.Stat(crtYol)
	_, keyErr := os.Stat(keyYol)
	if crtErr == nil && keyErr == nil {
		return tls.LoadX509KeyPair(crtYol, keyYol)
	}
	if (crtErr != nil && !os.IsNotExist(crtErr)) || (keyErr != nil && !os.IsNotExist(keyErr)) {
		return tls.Certificate{}, fmt.Errorf("sertifika dosyalari okunamadi: crt=%v key=%v", crtErr, keyErr)
	}
	// Ilk calistirma — ya da yarim kalmis cift; yarim cift zaten kullanilamaz,
	// bastan uretilir.
	if err := sertifikaUret(crtYol, keyYol); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(crtYol, keyYol)
}

// sertifikaUret — self-signed ECDSA P-256 sertifika + anahtar, 10 yil gecerli.
//
// SAN listesi bilerek BOS: panel dogrulamayi ad/IP uzerinden degil parmak izi
// uzerinden yapar; ajanin adresi degisse bile igne gecerli kalir.
func sertifikaUret(crtYol, keyYol string) error {
	if err := os.MkdirAll(filepath.Dir(crtYol), 0o755); err != nil {
		return err
	}
	anahtar, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("anahtar uretilemedi: %w", err)
	}
	seriNo, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("seri no uretilemedi: %w", err)
	}
	simdi := time.Now()
	sablon := x509.Certificate{
		SerialNumber: seriNo,
		Subject:      pkix.Name{CommonName: "girginospanel-agent"},
		NotBefore:    simdi.Add(-time.Hour),
		NotAfter:     simdi.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &sablon, &sablon, &anahtar.PublicKey, anahtar)
	if err != nil {
		return fmt.Errorf("sertifika uretilemedi: %w", err)
	}
	keyDer, err := x509.MarshalECPrivateKey(anahtar)
	if err != nil {
		return fmt.Errorf("anahtar kodlanamadi: %w", err)
	}
	if err := os.WriteFile(crtYol, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	// Anahtar 0600 — yalniz sahibi okur.
	return os.WriteFile(keyYol, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600)
}
