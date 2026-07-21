package system

// CVE / güvenlik denetimi — AlmaLinux `dnf updateinfo` (ALSA→CVE eşlemesi) üzerinden
// sunucunun kendi OS'undaki bilinen açıkları özetler + tek-tık güvenlik güncellemesi.
//
// GÜVENLİK: komutlar SABİT (argv-only, kullanıcı girdisi yok). Tarama read-only.
// Güncelleme (dnf --security) uzun sürer + servis etkileyebilir → optimize/güncelleme
// gibi systemd-run ile PID 1 altında AYRI transient unit'te koşar (sekme/panel kapansa
// da ölmez, dnf-kilidi güvenli).

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

const (
	cveUnit    = "girginospanel-cve-update"
	cveLogYol  = "/opt/girginospanel/logs/cve-update.log"
	cveWrapper = "/opt/girginospanel/cve-update.sh"
)

type CveKayit struct {
	Id       string `json:"id"`
	Severity string `json:"severity"` // kritik | onemli | orta | dusuk
	Paket    string `json:"paket"`
}

type CveOzet struct {
	Kritik              int        `json:"kritik"`
	Onemli              int        `json:"onemli"`
	Orta                int        `json:"orta"`
	Dusuk               int        `json:"dusuk"`
	ToplamCve           int        `json:"toplam_cve"`
	ToplamDanisman      int        `json:"toplam_danisman"`
	SonTarama           string     `json:"son_tarama"`
	TopCve              []CveKayit `json:"top_cve"`
	GuncellemeCalisiyor bool       `json:"guncelleme_calisiyor"`
	RebootGerekli       bool       `json:"reboot_gerekli"`
	KernelCare          KcDurum    `json:"kernelcare"`
}

var (
	cveMu      sync.Mutex
	cveCache   *CveOzet
	cveCacheTs time.Time
)

const cveCacheTTL = 30 * time.Minute

// cveRun — dnf'i timeout ile çalıştırır (metadata çekimi asılı kalmasın).
func cveRun(d time.Duration, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "dnf", args...).Output()
	return string(out)
}

func cveGuncellemeCalisiyor() bool {
	d := strings.TrimSpace(runOut("systemctl", "is-active", cveUnit))
	return d == "active" || d == "activating"
}

// rebootGerekli — en son KURULU kernel çalışandan farklıysa true. Güvenlik yamalı
// yeni çekirdek kurulmuş ama henüz boot edilmemişse, kernel CVE'leri sistem yeniden
// başlatılana kadar "açık" görünür (dnf: "installed security update" ≠ "running version").
// `dnf update --security` "Nothing to do" dese bile bu durumda sayı düşmez → kullanıcıya açıkla.
func rebootGerekli() bool {
	running := strings.TrimSpace(runOut("uname", "-r"))
	if running == "" {
		return false
	}
	// rpm -q --last kernel: en son kurulan çekirdek ilk satırda.
	for _, ln := range strings.Split(runOut("rpm", "-q", "--last", "kernel"), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		newest := strings.TrimPrefix(f[0], "kernel-")
		return newest != "" && newest != running
	}
	return false
}

var cveAgirlik = map[string]int{"kritik": 4, "onemli": 3, "orta": 2, "dusuk": 1}

func cveEtiket(sev string) string {
	switch {
	case strings.Contains(sev, "critical"):
		return "kritik"
	case strings.Contains(sev, "important"):
		return "onemli"
	case strings.Contains(sev, "moderate"):
		return "orta"
	case strings.Contains(sev, "low"):
		return "dusuk"
	}
	return ""
}

// cveTara — dnf updateinfo çıktısını parse eder (read-only).
// Aynı CVE birden çok pakette görünebilir → BENZERSIZ CVE üzerinden sayarız
// (CVE başına en yüksek önem). "486 satır" değil, gerçek benzersiz zafiyet sayısı.
func cveTara() *CveOzet {
	o := &CveOzet{SonTarama: time.Now().Format("2006-01-02 15:04")}
	// CVE listesi satırı: "CVE-2025-68724  Important/Sec. kernel-...x86_64"
	sevOf := map[string]string{} // cveID -> en yüksek önem etiketi
	pkgOf := map[string]string{} // cveID -> o önemdeki örnek paket
	for _, ln := range strings.Split(cveRun(150*time.Second, "-q", "updateinfo", "list", "cves"), "\n") {
		f := strings.Fields(ln)
		if len(f) < 3 || !strings.HasPrefix(f[0], "CVE-") {
			continue
		}
		id, etiket, pkg := f[0], cveEtiket(strings.ToLower(f[1])), f[len(f)-1]
		if etiket == "" {
			continue
		}
		if cveAgirlik[etiket] > cveAgirlik[sevOf[id]] {
			sevOf[id] = etiket
			pkgOf[id] = pkg
		}
	}
	// Benzersiz CVE sayımları + öncelik sıralı liste için topla.
	var kritikIds, onemliIds []string
	for id, et := range sevOf {
		o.ToplamCve++
		switch et {
		case "kritik":
			o.Kritik++
			kritikIds = append(kritikIds, id)
		case "onemli":
			o.Onemli++
			onemliIds = append(onemliIds, id)
		case "orta":
			o.Orta++
		case "dusuk":
			o.Dusuk++
		}
	}
	// Top liste: önce kritik (ID sıralı), sonra önemli — en fazla 10 (deterministik).
	sort.Strings(kritikIds)
	sort.Strings(onemliIds)
	for _, id := range append(kritikIds, onemliIds...) {
		if len(o.TopCve) >= 10 {
			break
		}
		o.TopCve = append(o.TopCve, CveKayit{Id: id, Severity: sevOf[id], Paket: pkgOf[id]})
	}
	// Toplam danışman sayısı (özet): "    15 Security notice(s)"
	for _, ln := range strings.Split(cveRun(60*time.Second, "-q", "updateinfo", "--summary"), "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasSuffix(t, "Security notice(s)") &&
			!strings.Contains(t, "Critical") && !strings.Contains(t, "Important") &&
			!strings.Contains(t, "Moderate") && !strings.Contains(t, "Low") {
			var n int
			if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
				o.ToplamDanisman = n
			}
		}
	}
	return o
}

// CveDurum — GET /system/cve : cache'li özet (yenile=1 ile zorla tara).
func CveDurum(w http.ResponseWriter, r *http.Request) {
	cveMu.Lock()
	defer cveMu.Unlock()
	guncelleniyor := cveGuncellemeCalisiyor()
	kc := kernelcareDurum()
	// KernelCare çalışan çekirdeği canlı yamaladıysa reboot GEREKMEZ (rebootsuz koruma).
	reboot := rebootGerekli() && !kc.Aktif
	zorla := r.URL.Query().Get("yenile") == "1"
	// Güvenlik güncellemesi sürerken dnf/rpm kilidi tutulur; yeniden `dnf updateinfo`
	// kilide takılır → mevcut cache'i tazelemeden döndür (kilit çekişmesi yok).
	if cveCache != nil && (guncelleniyor || (!zorla && time.Since(cveCacheTs) <= cveCacheTTL)) {
		ozet := *cveCache
		ozet.GuncellemeCalisiyor = guncelleniyor
		ozet.RebootGerekli = reboot
		ozet.KernelCare = kc
		httpx.WriteJSON(w, http.StatusOK, ozet)
		return
	}
	if guncelleniyor { // cache yok + güncelleme sürüyor: kilide girme, bayrağı döndür.
		httpx.WriteJSON(w, http.StatusOK, CveOzet{GuncellemeCalisiyor: true, RebootGerekli: reboot, KernelCare: kc})
		return
	}
	cveCache = cveTara()
	cveCacheTs = time.Now()
	ozet := *cveCache
	ozet.RebootGerekli = reboot
	ozet.KernelCare = kc
	httpx.WriteJSON(w, http.StatusOK, ozet)
}

const cveWrapperIcerik = `#!/usr/bin/env bash
set -uo pipefail
echo "════════ Güvenlik güncellemeleri — $(date "+%Y-%m-%d %H:%M:%S") ════════"
echo
if command -v dnf >/dev/null 2>&1; then
  dnf -y --refresh update --security
elif command -v yum >/dev/null 2>&1; then
  yum -y update --security
else
  echo "  (dnf/yum bulunamadı — güncelleme atlandı)"
fi
echo
echo "════════ ✓ Güvenlik güncellemeleri tamamlandı ════════"
`

func cveWrapperYaz() error {
	tmp := cveWrapper + ".tmp"
	if err := os.WriteFile(tmp, []byte(cveWrapperIcerik), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, cveWrapper)
}

// CveGuncelle — POST /system/cve/guncelle : güvenlik güncellemelerini arka planda kur.
func CveGuncelle(w http.ResponseWriter, r *http.Request) {
	if cveGuncellemeCalisiyor() {
		httpx.WriteError(w, http.StatusConflict, "güvenlik güncellemesi zaten çalışıyor")
		return
	}
	if c, _ := optimizeCalisiyor(); c {
		httpx.WriteError(w, http.StatusConflict, "optimizasyon sürüyor — bitince tekrar deneyin")
		return
	}
	if c, _ := guncelleCalisiyor(); c {
		httpx.WriteError(w, http.StatusConflict, "panel güncellemesi sürüyor — bitince tekrar deneyin")
		return
	}
	_ = os.MkdirAll("/opt/girginospanel/logs", 0o750)
	if err := cveWrapperYaz(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hazırlanamadı: "+err.Error())
		return
	}
	bas := fmt.Sprintf("=== Güvenlik güncellemesi başlatıldı: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	if err := os.WriteFile(cveLogYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}
	cmd := exec.Command("systemd-run",
		"--collect",
		"--unit", cveUnit,
		"--description", "GirginOSPanel güvenlik güncellemeleri",
		"-p", "StandardOutput=append:"+cveLogYol,
		"-p", "StandardError=append:"+cveLogYol,
		cveWrapper)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	// cache'i sıfırla — güncelleme sonrası tarama tazelensin.
	cveMu.Lock()
	cveCache = nil
	cveMu.Unlock()
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"baslatildi": true})
}

// CveLog — GET /system/cve/log : güncelleme log kuyruğu + durum.
func CveLog(w http.ResponseWriter, r *http.Request) {
	b, _ := os.ReadFile(cveLogYol)
	s := string(b)
	if len(s) > 60000 {
		s = s[len(s)-60000:]
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"log":       s,
		"calisiyor": cveGuncellemeCalisiyor(),
	})
}
