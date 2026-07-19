package system

// Sunucu optimizasyonu + sistem paket güncellemesi — panelden tetiklenen,
// UZUN SÜREN, servis-etkileyebilen bir bakım işi.
//
// GÜVENLİK: komut SABİT — hiçbir kullanıcı girdisi argümana geçmez. Panel root
// çalıştığı için ayrıcalık zaten var; iş, panelin systemd cgroup'unda DEĞİL,
// systemd-run ile PID 1 altında AYRI transient unit olarak koşar (panel restart
// olsa/güncellense bile iş ölmez). Çıktı systemd'nin StandardOutput=append: ile
// log dosyasına yazılır → shell string / kabuk yorumlaması YOKTUR (argv-only).
//
// AKIŞ (sabit wrapper script):
//   1) sistem paket güncellemesi: dnf -y update (yoksa yum -y update)
//   2) MariaDB/nginx/PHP performans ayarı: girginospanel-optimize

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

const (
	optimizeUnit    = "girginospanel-optimize-run"
	optimizeLogYol  = "/opt/girginospanel/logs/optimize.log"
	optimizeWrapper = "/opt/girginospanel/optimize-run.sh"
)

// optimizeWrapperIcerik — SABİT script. Kullanıcı girdisi İÇERMEZ; her başlatmada
// diske atomik yazılır (kaynak-doğruluğu Go tarafında tek yerde). dnf/yum -y update
// + girginospanel-optimize. Her adım kendi başına idempotent + güvenli.
const optimizeWrapperIcerik = `#!/usr/bin/env bash
set -uo pipefail
echo "════════ Sunucu Optimizasyonu — $(date "+%Y-%m-%d %H:%M:%S") ════════"
echo
echo "▶ 1/2 · Sistem paket güncellemesi (AlmaLinux)"
if command -v dnf >/dev/null 2>&1; then
  dnf -y update
elif command -v yum >/dev/null 2>&1; then
  yum -y update
else
  echo "  (dnf/yum bulunamadı — paket güncellemesi atlandı)"
fi
echo
echo "▶ 2/2 · MariaDB / nginx / PHP performans ayarı"
if command -v girginospanel-optimize >/dev/null 2>&1; then
  girginospanel-optimize
else
  echo "  (girginospanel-optimize bulunamadı — tuning atlandı)"
fi
echo
echo "════════ ✓ Optimizasyon tamamlandı ════════"
`

// optimizeCalisiyor — transient unit hâlâ çalışıyor mu.
func optimizeCalisiyor() (bool, string) {
	d := strings.TrimSpace(runOut("systemctl", "is-active", optimizeUnit))
	return d == "active" || d == "activating", d
}

// OptimizeDurum — GET /system/optimize.
func OptimizeDurum(w http.ResponseWriter, r *http.Request) {
	calisiyor, durum := optimizeCalisiyor()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"calisiyor": calisiyor,
		"durum":     durum,
	})
}

// optimizeWrapperYaz — sabit wrapper scriptini atomik yazar (0700, panel-özel).
func optimizeWrapperYaz() error {
	tmp := optimizeWrapper + ".tmp"
	if err := os.WriteFile(tmp, []byte(optimizeWrapperIcerik), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, optimizeWrapper) // atomik
}

// OptimizeBaslat — POST /system/optimize/baslat: optimizasyonu ayrı systemd
// unit'inde başlatır.
func OptimizeBaslat(w http.ResponseWriter, r *http.Request) {
	if c, _ := optimizeCalisiyor(); c {
		httpx.WriteError(w, http.StatusConflict, "optimizasyon zaten çalışıyor")
		return
	}
	// Panel güncellemesiyle çakışmasın (ikisi de paket/servis dokunur).
	if c, _ := guncelleCalisiyor(); c {
		httpx.WriteError(w, http.StatusConflict, "panel güncellemesi sürüyor — bitince tekrar deneyin")
		return
	}
	_ = os.MkdirAll("/opt/girginospanel/logs", 0o750)
	if err := optimizeWrapperYaz(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hazırlanamadı: "+err.Error())
		return
	}
	bas := fmt.Sprintf("=== Optimizasyon başlatıldı: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	if err := os.WriteFile(optimizeLogYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}
	// systemd-run: PID 1 altında transient unit; çıktı append: ile log dosyasına
	// (shell string YOK — tüm argümanlar sabit).
	cmd := exec.Command("systemd-run",
		"--collect",
		"--unit", optimizeUnit,
		"--description", "GirginOSPanel sunucu optimizasyonu",
		"-p", "StandardOutput=append:"+optimizeLogYol,
		"-p", "StandardError=append:"+optimizeLogYol,
		optimizeWrapper)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"baslatildi": true})
}

// OptimizeLog — GET /system/optimize/log: log kuyruğu + durum.
func OptimizeLog(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(optimizeLogYol)
	if err != nil {
		b = nil
	}
	s := string(b)
	if len(s) > 60000 {
		s = s[len(s)-60000:]
	}
	calisiyor, durum := optimizeCalisiyor()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"log":       s,
		"calisiyor": calisiyor,
		"durum":     durum,
	})
}
