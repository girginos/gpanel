package system

// Panel içi güncelleme — CLI'ya bağımlı olmadan panelden güncelleme.
//
// NEDEN: girginospanel-update scriptini dağıtan tek mekanizma yine kendisiydi;
// script eklenmeden önce kurulum yapan müşteriler kısır döngüye giriyordu
// ("command not found" → güncelleyemiyor → scripti alamıyor). Bu uç nokta
// scripti gerekirse repo'dan indirip (bootstrap) çalıştırır.
//
// 🔴 KRİTİK: Güncelleme panelin KENDİ binary'sini değiştirip servisi restart eder.
// Süreç panelin systemd cgroup'unda çalışsaydı, restart sırasında SIGKILL yerdi
// (KillMode=control-group varsayılanı tüm cgroup'u öldürür) → güncelleme yarıda
// kalır, panel bozulur. Bu yüzden `systemd-run` ile PID 1 altında AYRI transient
// unit olarak başlatılır; panel restart olurken süreç yaşamaya devam eder.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

const (
	guncelleScript = "/usr/local/bin/girginospanel-update"
	guncelleRawURL = "https://raw.githubusercontent.com/girginos/gpanel/main/assets/ops/girginospanel-update"
	guncelleLogYol = "/opt/girginospanel/logs/guncelleme.log"
	guncelleUnit   = "girginospanel-guncelleme"
)

// guncelleCalisiyor — transient unit hâlâ çalışıyor mu.
func guncelleCalisiyor() (bool, string) {
	d := strings.TrimSpace(runOut("systemctl", "is-active", guncelleUnit))
	return d == "active" || d == "activating", d
}

// GuncellemeDurum — güncelleme aracı mevcut mu, çalışıyor mu.
func GuncellemeDurum(w http.ResponseWriter, r *http.Request) {
	_, serr := os.Stat(guncelleScript)
	calisiyor, durum := guncelleCalisiyor()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"arac_var":  serr == nil,
		"calisiyor": calisiyor,
		"durum":     durum,
	})
}

// guncelleAracIndir — eksik update scriptini repo'dan indirir (eski kurulum kurtarma).
func guncelleAracIndir() error {
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Get(guncelleRawURL)
	if err != nil {
		return fmt.Errorf("indirilemedi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indirme HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("okunamadı: %w", err)
	}
	// gelen şey gerçekten script mi (HTML hata sayfası vb. değil)
	if !strings.HasPrefix(string(b), "#!") {
		return fmt.Errorf("beklenmeyen içerik — script değil")
	}
	tmp := guncelleScript + ".tmp"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, guncelleScript) // atomik
}

// GuncellemeBaslat — aracı (gerekirse indirip) ayrı systemd unit'inde başlatır.
func GuncellemeBaslat(w http.ResponseWriter, r *http.Request) {
	if calisiyor, _ := guncelleCalisiyor(); calisiyor {
		httpx.WriteError(w, http.StatusConflict, "güncelleme zaten çalışıyor")
		return
	}
	aracIndirildi := false
	if _, err := os.Stat(guncelleScript); err != nil {
		if err := guncelleAracIndir(); err != nil {
			httpx.WriteError(w, http.StatusBadGateway, "güncelleme aracı alınamadı: "+err.Error())
			return
		}
		aracIndirildi = true
	}

	_ = os.MkdirAll("/opt/girginospanel/logs", 0o750)
	bas := fmt.Sprintf("=== Güncelleme başlatıldı: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	if aracIndirildi {
		bas += "(güncelleme aracı eksikti — repo'dan indirildi)\n"
	}
	if err := os.WriteFile(guncelleLogYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}

	// systemd-run: PID 1 altında ayrı transient unit → panel restart'ında ÖLMEZ.
	cmd := exec.Command("systemd-run",
		"--collect", // bitince unit'i temizle (failed olsa da)
		"--unit", guncelleUnit,
		"--description", "GirginOSPanel güncelleme",
		"/bin/bash", "-lc", fmt.Sprintf("%s >>%s 2>&1", guncelleScript, guncelleLogYol))
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"baslatildi":     true,
		"arac_indirildi": aracIndirildi,
	})
}

// GuncellemeLog — log kuyruğu + durum. Panel restart olsa da log dosyası diskte kalır.
func GuncellemeLog(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(guncelleLogYol)
	if err != nil {
		b = nil
	}
	s := string(b)
	if len(s) > 60000 { // son 60KB yeter
		s = s[len(s)-60000:]
	}
	calisiyor, durum := guncelleCalisiyor()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"log":       s,
		"calisiyor": calisiyor,
		"durum":     durum,
	})
}
