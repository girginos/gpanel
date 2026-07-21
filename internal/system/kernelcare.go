package system

// KernelCare (TuxCare) entegrasyonu — REBOOTSUZ canlı çekirdek yaması.
// KernelCare, çalışan çekirdeğe güvenlik yamalarını bellekte uygular; böylece kernel
// CVE'leri sunucu yeniden başlatılmadan kapatılır (cPanel'in kullandığı yaklaşım).
//
// NOT: Yama beslemesi TuxCare'in tescilli ürünüdür; biz yalnız `kcarectl` ajanını
// entegre ederiz. Ajan + lisans anahtarı operatör tarafından kurulur/kaydedilir.
// kcarectl YOKKA bu katman tamamen sessizdir (Kurulu=false) — mevcut CVE akışı aynen çalışır.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

const (
	kcUnit    = "girginospanel-kernelcare-update"
	kcLogYol  = "/opt/girginospanel/logs/kernelcare-update.log"
	kcWrapper = "/opt/girginospanel/kernelcare-update.sh"
)

// KcDurum — KernelCare ajan durumu (CVE özetine gömülür).
type KcDurum struct {
	Kurulu        bool     `json:"kurulu"`         // kcarectl mevcut mu
	Aktif         bool     `json:"aktif"`          // yamalar çalışan çekirdeğe yüklü mü
	Kayitli       bool     `json:"kayitli"`        // lisans kayıtlı mı
	EfektifKernel string   `json:"efektif_kernel"` // kcarectl --uname (yamalı-eşdeğer sürüm)
	YamaliCve     []string `json:"yamali_cve"`     // patch-info'dan çıkarılan CVE'ler
	Calisiyor     bool     `json:"calisiyor"`      // --update arka planda sürüyor mu
}

// kcRun — kcarectl'i timeout ile çalıştırır; (çıktı, exit-code) döner.
func kcRun(d time.Duration, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, err := exec.CommandContext(ctx, "kcarectl", args...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	return string(out), -1
}

func kernelcareKurulu() bool {
	_, err := exec.LookPath("kcarectl")
	return err == nil
}

func kernelcareCalisiyor() bool {
	d := strings.TrimSpace(runOut("systemctl", "is-active", kcUnit))
	return d == "active" || d == "activating"
}

// kernelcareDurum — ajanı sorgular. kcarectl yoksa boş (Kurulu=false) döner.
func kernelcareDurum() KcDurum {
	kc := KcDurum{}
	if !kernelcareKurulu() {
		return kc
	}
	kc.Kurulu = true
	kc.Calisiyor = kernelcareCalisiyor()

	if o, c := kcRun(10*time.Second, "--uname"); c == 0 {
		kc.EfektifKernel = strings.TrimSpace(o)
	}

	// patch-info: yamalar yüklüyse (exit 0 + boş değil) aktif; CVE'leri çıkar.
	if pi, pc := kcRun(15*time.Second, "--patch-info"); pc == 0 && strings.TrimSpace(pi) != "" {
		kc.Aktif = true
		seen := map[string]bool{}
		for _, tok := range strings.FieldsFunc(pi, func(r rune) bool {
			return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == ';' || r == '(' || r == ')'
		}) {
			if strings.HasPrefix(tok, "CVE-") && !seen[tok] {
				seen[tok] = true
				kc.YamaliCve = append(kc.YamaliCve, tok)
			}
		}
	}

	// kayıt durumu: --info çıktısında "unregistered/not registered/no key" yoksa kayıtlı say.
	info, _ := kcRun(10*time.Second, "--info")
	low := strings.ToLower(info)
	kc.Kayitli = strings.TrimSpace(info) != "" &&
		!strings.Contains(low, "unregistered") &&
		!strings.Contains(low, "not registered") &&
		!strings.Contains(low, "no key") &&
		!strings.Contains(low, "no valid key")

	return kc
}

// KernelcareDurumHandler — GET /system/kernelcare : ajan durumu (poll için).
func KernelcareDurumHandler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, kernelcareDurum())
}

const kcWrapperIcerik = `#!/usr/bin/env bash
set -uo pipefail
echo "════════ KernelCare canlı çekirdek yaması — $(date "+%Y-%m-%d %H:%M:%S") ════════"
echo
if command -v kcarectl >/dev/null 2>&1; then
  kcarectl --update
else
  echo "  (kcarectl bulunamadı — KernelCare kurulu değil)"
fi
echo
echo "════════ ✓ Canlı yama tamamlandı ════════"
`

func kcWrapperYaz() error {
	tmp := kcWrapper + ".tmp"
	if err := os.WriteFile(tmp, []byte(kcWrapperIcerik), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, kcWrapper)
}

// KernelcareYamala — POST /system/kernelcare/yamala : `kcarectl --update`'i arka planda çalıştırır
// (systemd-run, sekme/panel kapansa da sürer).
func KernelcareYamala(w http.ResponseWriter, r *http.Request) {
	if !kernelcareKurulu() {
		httpx.WriteError(w, http.StatusBadRequest, "KernelCare kurulu değil")
		return
	}
	if kernelcareCalisiyor() {
		httpx.WriteError(w, http.StatusConflict, "canlı yama zaten çalışıyor")
		return
	}
	_ = os.MkdirAll("/opt/girginospanel/logs", 0o750)
	if err := kcWrapperYaz(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hazırlanamadı: "+err.Error())
		return
	}
	bas := fmt.Sprintf("=== KernelCare canlı yama başlatıldı: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	if err := os.WriteFile(kcLogYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}
	cmd := exec.Command("systemd-run",
		"--collect",
		"--unit", kcUnit,
		"--description", "GirginOSPanel KernelCare canlı çekirdek yaması",
		"-p", "StandardOutput=append:"+kcLogYol,
		"-p", "StandardError=append:"+kcLogYol,
		kcWrapper)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"baslatildi": true})
}
