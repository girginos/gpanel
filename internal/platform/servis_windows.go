//go:build windows

// servis_windows.go — SERVIS operasyonu + KAYNAK durumu.
//
// 🔴 ALLOWLIST DOKTRINI: panel operatoru web/db/dns/ftp yigininin DISINA
// cikamaz. Keyfi bir Windows servisini durdurmak/yeniden baslatmak sistemi
// bozabilir (orn. RpcSs, LSASS, Winlogon bagimliliklari). Bu yuzden hem
// listeleme hem islem YALNIZ sabit beyaz listedeki servislere dokunur; sinir
// niyete degil KODA gomulu.
package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// servisBeyazListe — panelin gorebilecegi/yonetebilecegi TEK servis kumesi:
//
//	W3SVC/WAS         — IIS cekirdegi (yayin + surec etkinlestirme)
//	MSSQLSERVER       — SQL Server varsayilan ornegi
//	SQLBrowser        — SQL Server Browser
//	ftpsvc            — IIS FTP
//	DNS               — DNS Server rolu
//	MySQL80 / MySQL   — MySQL servis adi surumden surume degisir, ikisi de aday
//	postgresql-x64-16 — PostgreSQL 16
//
// Kurulu olmayanlar listede gorunmez; liste disi bir ad islem gormez.
var servisBeyazListe = []string{
	"W3SVC", "WAS", "MSSQLSERVER", "SQLBrowser", "ftpsvc",
	"DNS", "MySQL80", "MySQL", "postgresql-x64-16",
}

// beyazListeKanon — verilen adin beyaz listedeki KANONIK karsiligini dondurur
// (buyuk/kucuk harf duyarsiz; Windows servis adlari zaten oyle), yoksa "".
// Islem komutlarina kullanicinin yankisi degil bu kanonik ad gecirilir.
func beyazListeKanon(ad string) string {
	for _, s := range servisBeyazListe {
		if strings.EqualFold(s, ad) {
			return s
		}
	}
	return ""
}

// ServisGoruntu — GET /api/yerel/servisler yanitindaki tek satir (etiketsiz,
// Go alan adlariyla).
type ServisGoruntu struct {
	Ad            string
	GoruntuAd     string
	Durum         string
	BaslangicTuru string
}

// ServisListe — beyaz listedeki servisleri TEK PowerShell cagrisiyla sorgular.
//
// 🔴 IKI TUZAK (windows.go'daki servisAdlari deseninden BIREBIR):
//  1. TEK OGE TUZAGI — ConvertTo-Json tek sonucta dizi DEGIL duz nesne yazar
//     ({...}), birden cokta dizi ([{...}]). Ilk bayta bakip ikisini de kabul ederiz.
//  2. `; exit 0` SIGORTASI — `Get-Service -Name` listesinde var OLMAYAN adlar
//     (kurulu olmayan servisler) SilentlyContinue ile bastirilsa da powershell
//     cikis kodunu 1 yapar; Output() bunu hata sayip gecerli JSON'u cope atardi.
//
// Durum/BaslangicTuru [string] ile ONCEDEN metne cevrilir: enum'lar PS 5.1'de
// JSON'a SAYI olarak sizardi (GorevleriOku'daki [string]$_.State ile ayni gerekce).
func ServisListe() ([]ServisGoruntu, error) {
	// -Name argumani beyaz listeden uretilir: tek kaynak, drift olmaz. Adlar
	// yalniz harf/rakam/tire icerir; tek tirnak icinde guvenle durur.
	tirnakli := make([]string, len(servisBeyazListe))
	for i, s := range servisBeyazListe {
		tirnakli[i] = "'" + s + "'"
	}
	komut := "Get-Service -Name " + strings.Join(tirnakli, ",") +
		" -ErrorAction SilentlyContinue | ForEach-Object { [PSCustomObject]@{ " +
		"Ad = $_.Name; GoruntuAd = $_.DisplayName; Durum = [string]$_.Status; " +
		"BaslangicTuru = [string]$_.StartType } } | ConvertTo-Json -Compress; exit 0"

	out, err := exec.Command("powershell", "-NoProfile", "-Command", komut).Output()
	if err != nil {
		return nil, fmt.Errorf("servis listesi alinamadi: %v", err)
	}

	ham := bytes.TrimSpace(out)
	kayitlar := []ServisGoruntu{}
	switch {
	case len(ham) == 0:
		// beyaz listeden hicbir servis kurulu degil — bos liste, hata degil
	case ham[0] == '{':
		var tek ServisGoruntu
		if e := json.Unmarshal(ham, &tek); e != nil {
			return nil, fmt.Errorf("servis JSON cozulemedi: %v", e)
		}
		kayitlar = append(kayitlar, tek)
	default:
		if e := json.Unmarshal(ham, &kayitlar); e != nil {
			return nil, fmt.Errorf("servis JSON cozulemedi: %v", e)
		}
	}
	return kayitlar, nil
}

// ServisIslem — beyaz listedeki bir servisi baslatir/durdurur/yeniden baslatir.
//
// 🔴 ALLOWLIST KAPISI: ad beyaz listede DEGILSE hicbir sey yapilmaz (keyfi
// servis durdurmak = sistem bozmak). islem de beyaz listeden olmali. Komutlara
// kullanicinin yankisi degil KANONIK ad gecirilir.
func ServisIslem(ad, islem string) error {
	kanon := beyazListeKanon(strings.TrimSpace(ad))
	if kanon == "" {
		return fmt.Errorf("servis beyaz listede degil: %q: %w", ad, ErrGecersizIstek)
	}
	var hedef string
	var komutHata error
	switch islem {
	case "baslat":
		komutHata = kos("sc", "start", kanon)
		hedef = "Running"
	case "durdur":
		komutHata = kos("sc", "stop", kanon)
		hedef = "Stopped"
	case "yeniden":
		// sc.exe'de tek komutluk "restart" yok; Restart-Service -Force bagimli
		// servisleri de birlikte cevirir. kanon sabit kumeden geldiginden (tirnak
		// icermez) dizge birlestirme burada guvenli.
		// 🔴 -ErrorAction Stop + try/catch: Restart-Service bir CMDLET'tir ve
		// $LASTEXITCODE'u SET ETMEZ; "geri gelmedi" hatasi da varsayilan olarak
		// TERMINATING DEGILDIR. Eski `exit $LASTEXITCODE` her zaman 0 donerdi
		// (sessiz basari). try/catch ile gercek hatada exit 1 zorlanir.
		komutHata = kos("powershell", "-NoProfile", "-Command",
			"$ProgressPreference='SilentlyContinue'; try { Restart-Service -Force -Name '"+kanon+"' -ErrorAction Stop } catch { Write-Error $_; exit 1 }")
		hedef = "Running"
	default:
		return fmt.Errorf("gecersiz islem %q (baslat|durdur|yeniden): %w", islem, ErrGecersizIstek)
	}
	// 🔴 SESSIZ BASARI YOK (Linux "basarisizlik guvence gibi gorunur" dersi):
	// `sc start/stop` ASENKRONDUR — cikis 0 "istek alindi" demek, "servis hedefe
	// ULASTI" demek DEGIL. Bu yuzden komut ciktisina degil GERCEK servis durumuna
	// bakariz. Hedefe ulasilirsa komut sikayet etmis olsa bile (idempotent: zaten
	// calisiyor / zaten durmus) BASARI; ulasilmazsa DURUST hata: ulasilan durum +
	// varsa komut hatasi birlikte bildirilir.
	ulasilan, err := servisDurumBekle(kanon, hedef, 30*time.Second)
	if err != nil {
		return err // servis durumu hic okunamadi (nadir) — olcemedik, durust soyle
	}
	if !strings.EqualFold(ulasilan, hedef) {
		if komutHata != nil {
			return fmt.Errorf("servis %s: %q istegi sonrasi durum %q (hedef %q) — %v", kanon, islem, ulasilan, hedef, komutHata)
		}
		return fmt.Errorf("servis %s: %q istegi kabul edildi ama durum %q oldu (hedef %q)", kanon, islem, ulasilan, hedef)
	}
	return nil
}

// servisDurumBekle — servisin Status'unu hedefe ulasana kadar yoklar; ulasilan
// (son gozlenen) durumu dondurur. Get-Service HIC calisamazsa hata doner
// (durumu olcemedik). Basarida hemen doner (genelde 1-3 sn); ulasilmazsa timeout
// sonunda son gozlenen durumu (hata degil) dondurur, karar cagirana birakilir.
func servisDurumBekle(kanon, hedef string, sure time.Duration) (string, error) {
	son := ""
	bitis := time.Now().Add(sure)
	for {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			"$ProgressPreference='SilentlyContinue'; (Get-Service -Name '"+kanon+"' -ErrorAction SilentlyContinue).Status; exit 0").Output()
		if err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				son = s
				if strings.EqualFold(son, hedef) {
					return son, nil
				}
			}
		}
		if time.Now().After(bitis) {
			if son == "" {
				return "", fmt.Errorf("servis %s durumu okunamadi (servis kayitli mi?)", kanon)
			}
			return son, nil
		}
		time.Sleep(750 * time.Millisecond)
	}
}

// ── kaynak durumu (CPU / RAM / disk) ─────────────────────────────────────────

// RamGoruntu — bellek kullanimi: yuzde + toplam/kullanilan (MB).
type RamGoruntu struct {
	Yuzde float64 `json:"yuzde"`
	TopMB float64 `json:"top_mb"`
	KulMB float64 `json:"kul_mb"`
}

// DiskGoruntu — tek bir yerel sabit disk (DriveType=3) icin doluluk.
type DiskGoruntu struct {
	Surucu string // "C:"
	Yuzde  float64
	TopGB  float64
	BosGB  float64
}

// KaynakGoruntu — GET /api/yerel/kaynak yaniti. Alan adlari uc sozlesmesini
// birebir yansitir (cpu_yuzde/ram/disk; disk ogeleri PascalCase).
type KaynakGoruntu struct {
	CPUYuzde float64       `json:"cpu_yuzde"`
	RAM      RamGoruntu    `json:"ram"`
	Diskler  []DiskGoruntu `json:"disk"`
}

// kaynakBetigi — CPU/RAM/disk'i TEK PowerShell cagrisiyla toplar ve ConvertTo-Json
// ile dondurur.
//
// 🔴 ProgressPreference SUSTURULUR (windows.go/kurulum_windows.go'da belgeli
// tuzak): CIM cmdlet'leri konsolsuz baglamda (servis) ilerleme cubugu cizmeye
// calisip "Access is denied reading the console output buffer" ile DUSEBILIR.
// disk @(...) ile ZORLA dizi yapilir (tek disk'te de dizi cikar); ust nesne
// zaten tekildir. -Depth 5: ic nesneler (ram/disk) varsayilan 2'de kirpilirdi.
const kaynakBetigi = `$ProgressPreference='SilentlyContinue'; ` +
	`$cpu=(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average; ` +
	`$os=Get-CimInstance Win32_OperatingSystem; ` +
	`$topKB=[double]$os.TotalVisibleMemorySize; $bosKB=[double]$os.FreePhysicalMemory; $kulKB=$topKB-$bosKB; ` +
	`$disk=Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object { ` +
	`$t=[double]$_.Size; $f=[double]$_.FreeSpace; [PSCustomObject]@{ ` +
	`Surucu=$_.DeviceID; Yuzde= if ($t -gt 0) { [math]::Round((($t-$f)/$t)*100,1) } else { 0 }; ` +
	`TopGB=[math]::Round($t/1GB,1); BosGB=[math]::Round($f/1GB,1) } }; ` +
	`[PSCustomObject]@{ ` +
	`cpu_yuzde= if ($cpu -ne $null) { [double]$cpu } else { 0 }; ` +
	`ram=[PSCustomObject]@{ ` +
	`yuzde= if ($topKB -gt 0) { [math]::Round(($kulKB/$topKB)*100,1) } else { 0 }; ` +
	`top_mb=[math]::Round($topKB/1024,1); kul_mb=[math]::Round($kulKB/1024,1) }; ` +
	`disk=@($disk) } | ConvertTo-Json -Compress -Depth 5`

// KaynakDurum — anlik CPU/RAM/disk ozetini dondurur.
func KaynakDurum() (KaynakGoruntu, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", kaynakBetigi).Output()
	if err != nil {
		return KaynakGoruntu{}, fmt.Errorf("kaynak durumu alinamadi: %v", err)
	}
	ham := bytes.TrimSpace(out)
	if len(ham) == 0 {
		return KaynakGoruntu{}, fmt.Errorf("kaynak durumu bos dondu")
	}
	var k KaynakGoruntu
	if e := json.Unmarshal(ham, &k); e != nil {
		return KaynakGoruntu{}, fmt.Errorf("kaynak JSON cozulemedi: %v", e)
	}
	if k.Diskler == nil {
		k.Diskler = []DiskGoruntu{}
	}
	return k, nil
}
