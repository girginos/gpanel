// Package platform — gPanel'in isletim sistemi SOZLESMESI.
//
// Bu dosya OS-NOTR olmak ZORUNDA: hicbir exec.Command, syscall veya /proc
// okumasi icermez. Hem Linux panelinde hem Windows ajaninda derlenir.
// Platforma ozgu her sey `*_linux.go` / `*_windows.go` dosyalarinda durur ve
// derleme etiketiyle ayrilir.
//
// 🔴 IZOLASYON GARANTISI UC KATMANDA:
//  1. DERLEME  — build tag: Linux koduna eklenen bir syscall Windows
//     artefaktina GIRMEZ, cunku o dosya Windows icin hic derlenmez.
//  2. ARTEFAKT — Windows tarafi AYRI binary (cmd/girginospanel-agent).
//     Panel binary'sinin icine girmez; panel bozulsa da ajan calismaya devam eder.
//  3. YAYIN    — ayri surum + ayri kanal (bkz. surum_*.go). Linux yayinlamak
//     Windows surumunu OYNATMAZ.
package platform

import "errors"

// ErrDesteklenmiyor — bu platform o yetenegi saglamiyor.
// 🔴 Cagiran taraf bunu HATA degil, DURUM olarak ele almali: ozelligi gizle,
// akisi kes degil. Windows'ta mail yok diye site acilisi patlamamali.
var ErrDesteklenmiyor = errors.New("platform bu yetenegi desteklemiyor")

// ErrGecersizIstek — cagiranin verdigi parametre hatali; HTTP tarafi bunu 400'e
// cevirir. OS-NOTR (bu dosyada): dogrulama/kacis mantigi ve testleri de OS-notr
// dosyalara tasindigi icin (B-13) sentinel burada, ortak sozlesmede durur.
var ErrGecersizIstek = errors.New("gecersiz istek")

// Yetenek — platformun neyi yapabildigi. Panel VARSAYMAK yerine SORAR.
// Yeni bir platform eklendiginde eksik yetenekler kendiliginden kapali gelir.
//
// 🔴 SIRA SOZLESMEDIR: bit degerleri DB'de ve panelde SAYI olarak kayitli.
// Mevcut satirlarin SIRASI ASLA degistirilmez; yeni yetenek yalniz zincirin
// SONUNA eklenir — araya giren tek satir tum kayitli degerleri kaydirir.
type Yetenek uint32

const (
	YetSite           Yetenek = 1 << iota // temel site yasam dongusu
	YetSSL                                // sertifika verme/yenileme
	YetIzolasyon                          // kiraci izolasyonu (systemd slice / app pool)
	YetKota                               // disk kotasi
	YetMail                               // posta sunucusu
	YetPHPSurumSecimi                     // site basina PHP surumu
	YetZorunluErisim                      // zorunlu erisim denetimi (SELinux vb.)
	YetOlayGunlugu                        // isletim sistemi olay gunlugu (Event Log)
	YetZamanliGorev                       // zamanlanmis gorevler (Task Scheduler)
	YetMSSQL                              // Microsoft SQL Server servisi
	YetFTP                                // FTP sunucusu (IIS ftpsvc)
	YetDNS                                // DNS sunucu rolu
	YetDotNet                             // ASP.NET Core Hosting Bundle kurulu
	YetMySQL                              // MySQL/MariaDB servisi
	YetPgSQL                              // PostgreSQL servisi
)

func (y Yetenek) Var(b Yetenek) bool { return y&b != 0 }

// SiteIstek — bir sitenin acilmasi icin gereken en az bilgi.
type SiteIstek struct {
	AlanAdi  string
	PHPSurum string // platform YetPHPSurumSecimi desteklemiyorsa yok sayilir
}

// SiteKimlik — var olan bir siteye isaret eder.
type SiteKimlik struct {
	AlanAdi         string
	SistemKullanici string
}

// SiteSonuc — platformdan bagimsiz site ciktisi.
// Alan adlari Linux `provisioner.Result` ile bilerek ayni tutuldu ki
// Linux adaptoru birebir esleme yapsin, cevirim hatasi olmasin.
type SiteSonuc struct {
	SistemKullanici string
	WebRoot         string
	FTPHost         string
	PHPSurum        string
	PHPSocket       string // Windows'ta bos: IIS uygulama havuzu kullanir
}

// Sertifika — verilen sertifikanin disk yollari.
type Sertifika struct {
	SertifikaYolu string
	AnahtarYolu   string
}

// SSLIstek — sertifika verme istegi.
// PHPSurum + Backend alanlari UYDURMA degil: Linux'taki gercek cagri
// `EnableLetsEncrypt(alanAdi, sk, phpSurum, backend)` imzasindan geliyor
// (vhost yeniden yazimi icin gerekiyorlar). Windows bu iki alani yok sayar.
type SSLIstek struct {
	SiteKimlik
	PHPSurum string
	Backend  string
}

// Saglayici — platformun uygulamasi gereken sozlesme.
//
// 🔴 KUCUK TUTULDU. Bugun 9.620 satirlik provisioner'in TAMAMINI soyutlamak
// Linux davranisini degistirme riski demekti. Bu yuzden sozlesme yalnizca
// yasam dongusunun cekirdegini kapsar; gerisi platforma ozgu kalir ve
// zamanla, her biri ayri ayri dogrulanarak buraya tasinir.
type Saglayici interface {
	Ad() string
	Yetenekler() Yetenek

	// Dogrula — ortam bu saglayici icin hazir mi (gerekli servisler, yollar).
	// Panel acilista cagirip eksikleri RAPORLAR, sessizce devam ETMEZ.
	Dogrula() error

	SiteOlustur(SiteIstek) (SiteSonuc, error)
	SiteSil(SiteKimlik) error
	SSLVer(SSLIstek) (Sertifika, error)
}

// Aktif — derlenmis platformun saglayicisi. Her OS dosyasi kendi
// `aktifSaglayici` degerini verir; boylece secim CALISMA ZAMANINDA degil
// DERLEME ZAMANINDA yapilir ve yanlis platform kodu binary'ye hic girmez.
func Aktif() Saglayici { return aktifSaglayici }
