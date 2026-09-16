//go:build windows

// plan_windows.go — HOSTING PLANLARI (paket): disk kotasi + IIS limitleri, siteye
// atanabilir preset. Linux paneldeki "paket" kavraminin Windows karsiligi.
//
// 🔴 IKI FARKLI MOTOR:
//   - IIS LIMITLERI (baglanti/bant genisligi/CPU/bellek): appcmd ile SITE ve
//     HAVUZ uzerine uygulanir; IIS her kurulumda vardir → HER ZAMAN calisir.
//   - DISK KOTASI: FSRM (Dosya Sunucusu Kaynak Yoneticisi) ile webroot'a sabit
//     kota uygulanir. FSRM standart bir sunucu OZELLIGIDIR ve cogu kurulumda
//     YOK. Kurulu degilse kotayi UYGULAMAYIZ ve durumu ACIKCA bildiririz —
//     "kota aktif" yanilsamasi vermeyiz (yanlis guvence, sahte doluluk).
//
// Depolama: C:\ProgramData\girginospanel\planlar.json (dizin ACL'i E duzeltmesiyle
// SYSTEM+Admins'e kilitli → dosya miras alir).
package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const planlarYolu = `C:\ProgramData\girginospanel\planlar.json`

// planAdiRe — plan adi: harf/rakam/bosluk/alt-cizgi/tire, 1..40. JSON atama
// anahtarina girer; dar tutariz (kontrol karakteri/kacis yok).
var planAdiRe = regexp.MustCompile(`^[\p{L}0-9 _-]{1,40}$`)

// Plan — tek hosting plani. Her limitte 0 = SINIRSIZ (limit uygulanmaz).
type Plan struct {
	Ad                  string `json:"ad"`
	DiskKotaMB          int64  `json:"diskKotaMB"`
	MaxBaglanti         int64  `json:"maxBaglanti"`
	MaxBantGenisligiKBs int64  `json:"maxBantGenisligiKBs"`
	CpuLimitYuzde       int    `json:"cpuLimitYuzde"`
	BellekMB            int64  `json:"bellekMB"`
}

// PlanDosya — planlar.json semasi.
type PlanDosya struct {
	Planlar  []Plan            `json:"planlar"`
	Atamalar map[string]string `json:"atamalar"` // site alanAdi -> plan Ad
}

// PlanDurumu — GET /api/yerel/planlar yaniti; kota motoru (FSRM) durumu dahil.
type PlanDurumu struct {
	Planlar    []Plan            `json:"planlar"`
	Atamalar   map[string]string `json:"atamalar"`
	KotaMotoru bool              `json:"kotaMotoru"` // FSRM kurulu mu
}

// AtaSonuc — PlanAta ciktisi; limit ve kota AYRI raporlanir (biri basarisiz
// olsa da diger uygulanmis olabilir).
type AtaSonuc struct {
	Site           string   `json:"site"`
	Plan           string   `json:"plan"`
	LimitUygulandi bool     `json:"limitUygulandi"`
	KotaUygulandi  bool     `json:"kotaUygulandi"`
	KotaNot        string   `json:"kotaNot"`
	Uyarilar       []string `json:"uyarilar"`
}

func planOku() (PlanDosya, error) {
	d := PlanDosya{Planlar: []Plan{}, Atamalar: map[string]string{}}
	b, err := os.ReadFile(planlarYolu)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return d, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("planlar.json bozuk: %w", err)
	}
	if d.Planlar == nil {
		d.Planlar = []Plan{}
	}
	if d.Atamalar == nil {
		d.Atamalar = map[string]string{}
	}
	return d, nil
}

func planYaz(d PlanDosya) error {
	if err := os.MkdirAll(filepath.Dir(planlarYolu), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	// Atomik: ayni dizinde gecici dosya + rename. Dizin ACL'i (E duzeltmesi)
	// miras alinir; planlar.json gizli degil ama yine de SYSTEM+Admins ile kilitli.
	gecici := planlarYolu + ".yeni"
	if err := os.WriteFile(gecici, b, 0o600); err != nil {
		return err
	}
	return os.Rename(gecici, planlarYolu)
}

func planBul(d PlanDosya, ad string) int {
	for i := range d.Planlar {
		if strings.EqualFold(d.Planlar[i].Ad, ad) {
			return i
		}
	}
	return -1
}

func planDogrula(p Plan) error {
	if !planAdiRe.MatchString(strings.TrimSpace(p.Ad)) {
		return fmt.Errorf("plan adi gecersiz (1-40 harf/rakam/bosluk/_/-): %w", ErrGecersizIstek)
	}
	if p.DiskKotaMB < 0 || p.MaxBaglanti < 0 || p.MaxBantGenisligiKBs < 0 || p.BellekMB < 0 {
		return fmt.Errorf("negatif limit olamaz: %w", ErrGecersizIstek)
	}
	if p.CpuLimitYuzde < 0 || p.CpuLimitYuzde > 100 {
		return fmt.Errorf("CPU limiti 0-100 arasi olmali: %w", ErrGecersizIstek)
	}
	return nil
}

// PlanDurumuGetir — planlar + atamalar + FSRM durumu.
func PlanDurumuGetir() (PlanDurumu, error) {
	d, err := planOku()
	if err != nil {
		return PlanDurumu{}, err
	}
	return PlanDurumu{Planlar: d.Planlar, Atamalar: d.Atamalar, KotaMotoru: fsrmVar()}, nil
}

// PlanKaydet — plan ekler veya (ayni ad, buyuk/kucuk duyarsiz) gunceller.
func PlanKaydet(p Plan) error {
	p.Ad = strings.TrimSpace(p.Ad)
	if err := planDogrula(p); err != nil {
		return err
	}
	d, err := planOku()
	if err != nil {
		return err
	}
	if i := planBul(d, p.Ad); i >= 0 {
		d.Planlar[i] = p
	} else {
		d.Planlar = append(d.Planlar, p)
	}
	return planYaz(d)
}

// PlanSil — plani siler; bir siteye ATANMISSA reddeder (once atama kaldirilmali).
func PlanSil(ad string) error {
	ad = strings.TrimSpace(ad)
	d, err := planOku()
	if err != nil {
		return err
	}
	for site, pl := range d.Atamalar {
		if strings.EqualFold(pl, ad) {
			return fmt.Errorf("plan %q siteye atali (%s); once atamayi kaldirin: %w", ad, site, ErrGecersizIstek)
		}
	}
	i := planBul(d, ad)
	if i < 0 {
		return fmt.Errorf("plan bulunamadi: %q: %w", ad, ErrGecersizIstek)
	}
	d.Planlar = append(d.Planlar[:i], d.Planlar[i+1:]...)
	return planYaz(d)
}

// PlanAta — plani siteye uygular: IIS limitleri (appcmd) + disk kotasi (FSRM).
// Limit uygulama HATA verirse doner; kota FSRM yoksa zarifce atlanir (KotaNot).
func PlanAta(site, planAd string) (AtaSonuc, error) {
	site = strings.TrimSpace(strings.ToLower(site))
	planAd = strings.TrimSpace(planAd)
	if !alanAdiRe.MatchString(site) {
		return AtaSonuc{}, fmt.Errorf("gecersiz alan adi: %q: %w", site, ErrGecersizIstek)
	}
	d, err := planOku()
	if err != nil {
		return AtaSonuc{}, err
	}
	i := planBul(d, planAd)
	if i < 0 {
		return AtaSonuc{}, fmt.Errorf("plan bulunamadi: %q: %w", planAd, ErrGecersizIstek)
	}
	p := d.Planlar[i]

	// Site detayi -> havuz adi + webroot (var olan, dogrulanmis okuma).
	det, err := SiteDetay(site)
	if err != nil {
		return AtaSonuc{}, err
	}

	sonuc := AtaSonuc{Site: site, Plan: p.Ad}

	// --- IIS SITE limitleri: baglanti + bant genisligi ---
	// 0 = sinirsiz -> IIS'in azami uint32 degeri (4294967295). maxBandwidth
	// bayt/sn cinsindendir; plan KB/sn tutar → *1024.
	baglanti := p.MaxBaglanti
	if baglanti <= 0 || baglanti > 4294967295 {
		baglanti = 4294967295 // 0=sinirsiz; ustu de IIS azamisi (uint32) ile kirpilir
	}
	var bantB int64 = 4294967295
	if p.MaxBantGenisligiKBs > 0 {
		bantB = p.MaxBantGenisligiKBs * 1024
		if bantB > 4294967295 {
			bantB = 4294967295 // appcmd hard-hata yerine azamiye kirp
		}
	}
	if err := kos(appcmdYolu(), "set", "site", "/site.name:"+site,
		fmt.Sprintf("/limits.maxConnections:%d", baglanti),
		fmt.Sprintf("/limits.maxBandwidth:%d", bantB)); err != nil {
		return sonuc, fmt.Errorf("IIS site limitleri uygulanamadi: %w", err)
	}

	// --- IIS HAVUZ limitleri: CPU throttle + ozel bellek geri donusumu ---
	if havuz := det.Havuz.Ad; havuz != "" {
		cpuLimit := p.CpuLimitYuzde * 1000 // appcmd birimi: %'in 1/1000'i (20% -> 20000)
		cpuAksiyon := "Throttle"
		if p.CpuLimitYuzde <= 0 {
			cpuLimit = 0
			cpuAksiyon = "NoAction"
		}
		if err := kos(appcmdYolu(), "set", "apppool", "/apppool.name:"+havuz,
			fmt.Sprintf("/cpu.limit:%d", cpuLimit), "/cpu.action:"+cpuAksiyon); err != nil {
			sonuc.Uyarilar = append(sonuc.Uyarilar, "havuz CPU limiti uygulanamadi: "+err.Error())
		}
		var bellekKB int64 // 0 = sinirsiz (periyodik ozel bellek geri donusumu kapali)
		if p.BellekMB > 0 {
			bellekKB = p.BellekMB * 1024
		}
		if err := kos(appcmdYolu(), "set", "apppool", "/apppool.name:"+havuz,
			fmt.Sprintf("/recycling.periodicRestart.privateMemory:%d", bellekKB)); err != nil {
			sonuc.Uyarilar = append(sonuc.Uyarilar, "havuz bellek limiti uygulanamadi: "+err.Error())
		}
	} else {
		sonuc.Uyarilar = append(sonuc.Uyarilar, "site havuzu bulunamadi; CPU/bellek limiti atlandi")
	}
	sonuc.LimitUygulandi = true

	// --- DISK KOTASI (FSRM) ---
	if p.DiskKotaMB > 0 {
		switch {
		case !fsrmVar():
			sonuc.KotaUygulandi = false
			sonuc.KotaNot = "FSRM (Dosya Sunucusu Kaynak Yoneticisi) kurulu degil — disk kotasi UYGULANMADI. 'Kota motorunu kur' ile etkinlestirin."
		case det.FizikselYol == "":
			sonuc.KotaUygulandi = false
			sonuc.KotaNot = "site fiziksel yolu bulunamadi; kota uygulanmadi"
		default:
			if err := fsrmKotaUygula(det.FizikselYol, p.DiskKotaMB); err != nil {
				sonuc.KotaUygulandi = false
				sonuc.KotaNot = "kota uygulanamadi: " + err.Error()
			} else {
				sonuc.KotaUygulandi = true
				sonuc.KotaNot = fmt.Sprintf("%d MB kota uygulandi (%s)", p.DiskKotaMB, det.FizikselYol)
			}
		}
	} else {
		sonuc.KotaUygulandi = true
		sonuc.KotaNot = "disk kotasi sinirsiz"
		if fsrmVar() && det.FizikselYol != "" {
			_ = fsrmKotaKaldir(det.FizikselYol) // plan sinirsiza dondu → varsa eski kotayi kaldir
		}
	}

	d.Atamalar[site] = p.Ad
	if err := planYaz(d); err != nil {
		return sonuc, fmt.Errorf("atama kaydedilemedi: %w", err)
	}
	return sonuc, nil
}

// PlanKaldir — sitenin plan atamasini kaldirir: IIS limitlerini sinirsiza
// dondurur, varsa kotayi kaldirir. Limit sifirlama best-effort'tur (site/havuz
// silinmis olabilir).
func PlanKaldir(site string) error {
	site = strings.TrimSpace(strings.ToLower(site))
	if !alanAdiRe.MatchString(site) {
		return fmt.Errorf("gecersiz alan adi: %q: %w", site, ErrGecersizIstek)
	}
	d, err := planOku()
	if err != nil {
		return err
	}
	if _, ok := d.Atamalar[site]; !ok {
		return fmt.Errorf("sitede plan atamasi yok: %q: %w", site, ErrGecersizIstek)
	}
	det, derr := SiteDetay(site)
	_ = kos(appcmdYolu(), "set", "site", "/site.name:"+site,
		"/limits.maxConnections:4294967295", "/limits.maxBandwidth:4294967295")
	if derr == nil && det.Havuz.Ad != "" {
		_ = kos(appcmdYolu(), "set", "apppool", "/apppool.name:"+det.Havuz.Ad,
			"/cpu.limit:0", "/cpu.action:NoAction", "/recycling.periodicRestart.privateMemory:0")
	}
	if derr == nil && det.FizikselYol != "" && fsrmVar() {
		_ = fsrmKotaKaldir(det.FizikselYol)
	}
	delete(d.Atamalar, site)
	return planYaz(d)
}

// PlanSiteTemizle — site SILINDIGINDE cagrilir: o siteye ait plan atamasini
// planlar.json'dan dusurur. Aksi halde atama OKSUZ kalir ve PlanSil o plani
// "siteye atali" (artik olmayan siteye) diye reddeder = plan silinemez kilit
// (denetim bulgusu). FSRM kotasi site webroot'una bagliydi; site IIS'ten
// kalktigi icin webroot guvenilir cozulemez, dizindeki kota kalabilir (zararsiz;
// dizin yeni bir siteye atanirsa PlanAta gunceller). En kritigi atamayi dusurmek.
// Best-effort: hata site silmeyi bozmamali (cagiran '_' ile yutar).
func PlanSiteTemizle(site string) error {
	site = strings.TrimSpace(strings.ToLower(site))
	d, err := planOku()
	if err != nil {
		return err
	}
	if _, ok := d.Atamalar[site]; !ok {
		return nil
	}
	delete(d.Atamalar, site)
	return planYaz(d)
}

// ── FSRM (disk kotasi) yardimcilari ──────────────────────────────────────────

// fsrmVar — FSRM PowerShell modulu (Get-FsrmQuota) yuklu mu.
func fsrmVar() bool {
	out, err := komutCikti("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"if (Get-Command Get-FsrmQuota -ErrorAction SilentlyContinue) { 'VAR' } else { 'YOK' }")
	if err != nil {
		return false
	}
	return strings.Contains(out, "VAR")
}

// psYolKacis — yol'u PowerShell TEK-tirnakli literaline gomer (” kacisiyla).
// $(...) / degisken enjeksiyonu literalde etkisizdir.
func psYolKacis(yol string) string {
	return "'" + strings.ReplaceAll(yol, "'", "''") + "'"
}

// fsrmKotaUygula — webroot'a sabit (hard) disk kotasi uygular; varsa gunceller.
func fsrmKotaUygula(yol string, mb int64) error {
	betik := fmt.Sprintf(
		"$p=%s; $s=%dMB; if (Get-FsrmQuota -Path $p -ErrorAction SilentlyContinue) "+
			"{ Set-FsrmQuota -Path $p -Size $s -ErrorAction Stop } "+
			"else { New-FsrmQuota -Path $p -Size $s -ErrorAction Stop }",
		psYolKacis(yol), mb)
	if out, err := komutCikti("powershell", "-NoProfile", "-NonInteractive", "-Command", betik); err != nil {
		return fmt.Errorf("%v — %s", err, strings.TrimSpace(out))
	}
	return nil
}

// fsrmKotaKaldir — webroot'taki kotayi (varsa) siler.
func fsrmKotaKaldir(yol string) error {
	p := psYolKacis(yol)
	betik := fmt.Sprintf("if (Get-FsrmQuota -Path %s -ErrorAction SilentlyContinue) "+
		"{ Remove-FsrmQuota -Path %s -Confirm:$false -ErrorAction Stop }", p, p)
	if out, err := komutCikti("powershell", "-NoProfile", "-NonInteractive", "-Command", betik); err != nil {
		return fmt.Errorf("%v — %s", err, strings.TrimSpace(out))
	}
	return nil
}

// KotaMotoruKur — FSRM ozelligini kurar (Install-WindowsFeature). UZUN surer;
// yerel panel sunucusunda WriteTimeout yok, senkron cagri guvenli. Yeniden
// baslatma gerekiyorsa true doner.
func KotaMotoruKur() (bool, error) {
	out, err := komutCikti("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$r = Install-WindowsFeature -Name FS-Resource-Manager -IncludeManagementTools; "+
			"if (-not $r.Success) { throw 'ozellik kurulamadi' }; "+
			"if ($r.RestartNeeded -eq 'Yes') { 'RESTART' } else { 'OK' }")
	if err != nil {
		return false, fmt.Errorf("%v — %s", err, strings.TrimSpace(out))
	}
	return strings.Contains(out, "RESTART"), nil
}
