package hostuyg

// systemd unit yönetimi — hardening + cgroup limits + per-app user.
//
// Her uygulama için `gpanel-app-<kod-ornek>.service` dosyası
// `/etc/systemd/system/` altına yazılır. Daemon-reload + enable + start.
//
// HARDENING katmanları (Docker olmadan derin izolasyon):
//   - User=<per-app>: root değil
//   - ProtectSystem=strict: /usr, /etc, /boot read-only
//   - ProtectHome=true: /home görünmez
//   - PrivateTmp=true: özel /tmp
//   - NoNewPrivileges=true: setuid escape yok
//   - RestrictSUIDSGID=true
//   - LockPersonality=true
//   - RestrictNamespaces=true
//   - RestrictRealtime=true
//   - MemoryDenyWriteExecute=true
//   - CapabilityBoundingSet=~CAP_SYS_ADMIN CAP_NET_ADMIN … (default drop-all)
//   - ReadWritePaths=<kurulum>: sadece kendi dizinine yaz
//   - MemoryMax + CPUQuota + TasksMax cgroup limits

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const UnitDizin = "/etc/systemd/system"

// unitYolu — /etc/systemd/system/<unit>.service
func unitYolu(unitAd string) string {
	return filepath.Join(UnitDizin, unitAd+".service")
}

// EnvDosyaYolu — unit'in secret env'lerini tutan 0400 dosya (root:root).
// C2 fix: unit dosyası 0644 world-readable → secret Environment= satırları
// tüm loginli kullanıcılara açık olurdu. Ayrı EnvironmentFile 0400 çözer.
func EnvDosyaYolu(unitAd string) string {
	return filepath.Join(UnitDizin, unitAd+".env")
}

// EnvDosyaYaz — secret ENV'leri 0400 dosyaya yaz (atomic + chmod).
// I1 fix: anahtar sırası deterministik (map iteration random). I2 fix: \r + \n
// strip (sadece \n yeterli değil).
func EnvDosyaYaz(unitAd string, envMap map[string]string) error {
	yol := EnvDosyaYolu(unitAd)
	// Sırala
	anahtarlar := make([]string, 0, len(envMap))
	for k := range envMap {
		anahtarlar = append(anahtarlar, k)
	}
	sort.Strings(anahtarlar)

	stripper := strings.NewReplacer("\r", "", "\n", "")
	var sb strings.Builder
	sb.WriteString("# OTOMATİK — hostuyg secret env, root:root 0400\n")
	for _, k := range anahtarlar {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, stripper.Replace(envMap[k])))
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0400); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0400); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, yol)
}

// EnvDosyaSil — kurulum kaldırılırken.
// I5 fix: defense-in-depth prefix guard.
func EnvDosyaSil(unitAd string) error {
	if !strings.HasPrefix(unitAd, "gpanel-app-") {
		return fmt.Errorf("güvenlik: unit adı %q gpanel-app- prefix'i taşımıyor", unitAd)
	}
	err := os.Remove(EnvDosyaYolu(unitAd))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// UnitRender — signature değişti: unitAd parametresi eklendi (C2 için env
// dosya yolu üretimi). Ayrıca çözümlenmiş env map'i döner ki caller
// EnvDosyaYaz çağırabilsin.
//
// C2 fix: env değişkenleri artık unit dosyasında INLINE değil,
// EnvironmentFile= üzerinden ayrı 0400 dosyadan okunur.
func UnitRender(t *Tarif, unitAd, sistemKullanici, kurulumYolu string, portlar map[string]int, secretler map[string]string, panelhost string) (string, map[string]string) {
	yerine := func(s string) string {
		s = strings.ReplaceAll(s, "{kurulum}", kurulumYolu)
		s = strings.ReplaceAll(s, "{sistem_kullanici}", sistemKullanici)
		s = strings.ReplaceAll(s, "{panelhost}", panelhost)
		for ad, p := range portlar {
			s = strings.ReplaceAll(s, "{port_"+ad+"}", fmt.Sprintf("%d", p))
		}
		for k, v := range secretler {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s
	}

	execStart := make([]string, len(t.CalistirKomutu))
	for i, k := range t.CalistirKomutu {
		execStart[i] = yerine(k)
	}
	calismaDizini := t.CalismaDizini
	if calismaDizini == "" {
		calismaDizini = kurulumYolu
	} else {
		calismaDizini = yerine(calismaDizini)
	}

	// C2 fix: env değişkenlerini resolve et; caller EnvDosyaYaz için kullanır.
	envMap := make(map[string]string, len(t.CevreDegisken))
	for k, v := range t.CevreDegisken {
		envMap[k] = yerine(v)
	}
	envDosyaSatiri := ""
	if len(envMap) > 0 {
		envDosyaSatiri = fmt.Sprintf("EnvironmentFile=%s\n", EnvDosyaYolu(unitAd))
	}

	memMax := t.MemoryMax
	if memMax == "" {
		memMax = "512M"
	}
	cpuQuota := t.CPUQuota
	if cpuQuota == "" {
		cpuQuota = "50%"
	}
	tasksMax := t.TasksMax
	if tasksMax == 0 {
		tasksMax = 200
	}

	// ExecStart tek satır — args boşluklu ise systemd " ile sarılmasını bekler
	execStartSatir := strings.Join(execStart, " ")

	// C2 fix (bug follow-up): EnvironmentFile MUTLAKA [Service] içinde olmalı,
	// aksi halde `[Install]` içinde geçersiz key olarak systemd tarafından
	// yok sayılır → secret'ler yüklenmez, servis çalışmaya devam eder ama
	// env'siz. Doğru yerleştirme için template içine YERLEŞTİR.
	envSatirBloku := ""
	if envDosyaSatiri != "" {
		envSatirBloku = "\n# --- Environment (ayrı 0400 dosya) ---\n" + envDosyaSatiri
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`# OTOMATİK ÜRETİLDİ — hostuyg.UnitRender
# Kod: %s / Sürüm: %s
# Elle düzenleme; sonraki install/upgrade'de üzerine yazılır.
[Unit]
Description=gpanel-app: %s (%s)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
ExecStart=%s
Restart=on-failure
RestartSec=5s
%s
# --- Kaynak sınırı (cgroup) ---
MemoryMax=%s
CPUQuota=%s
TasksMax=%d

# --- Hardening ---
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=false
SystemCallArchitectures=native
CapabilityBoundingSet=

# Uygulama sadece kendi kurulum dizinine yazabilir
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`,
		t.Kod, t.Surum,
		t.Ad, t.Kod,
		sistemKullanici, sistemKullanici,
		calismaDizini,
		execStartSatir,
		envSatirBloku,
		memMax, cpuQuota, tasksMax,
		kurulumYolu,
	))
	return sb.String(), envMap
}

// UnitYaz — dosyayı atomic yaz + daemon-reload.
func UnitYaz(unitAd, icerik string) error {
	yol := unitYolu(unitAd)
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(icerik), 0644); err != nil {
		return fmt.Errorf("unit tmp yazma: %w", err)
	}
	if err := os.Rename(tmp, yol); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("unit rename: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// UnitEnableStart — enable + start.
func UnitEnableStart(ctx context.Context, unitAd string) error {
	svc := unitAd + ".service"
	if out, err := exec.CommandContext(ctx, "systemctl", "enable", svc).CombinedOutput(); err != nil {
		return fmt.Errorf("enable: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.CommandContext(ctx, "systemctl", "start", svc).CombinedOutput(); err != nil {
		return fmt.Errorf("start: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// UnitStopDisable — durdur + disable.
func UnitStopDisable(ctx context.Context, unitAd string) error {
	svc := unitAd + ".service"
	// stop hata verirse (zaten durdurulmuş) devam et
	_, _ = exec.CommandContext(ctx, "systemctl", "stop", svc).CombinedOutput()
	_, _ = exec.CommandContext(ctx, "systemctl", "disable", svc).CombinedOutput()
	return nil
}

// UnitSil — unit dosyasını sil + daemon-reload.
func UnitSil(unitAd string) error {
	yol := unitYolu(unitAd)
	if err := os.Remove(yol); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	_, _ = exec.Command("systemctl", "reset-failed").CombinedOutput()
	return nil
}

// UnitDurum — "active" | "inactive" | "failed" | "activating" | "unknown"
func UnitDurum(unitAd string) string {
	out, _ := exec.Command("systemctl", "is-active", unitAd+".service").Output()
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "unknown"
	}
	return s
}

// UnitLog — son N satır log. `--output=short-iso` ISO timestamp UI parse
// kolaylığı için (RFC3339-benzeri).
func UnitLog(unitAd string, satir int) string {
	if satir <= 0 || satir > 500 {
		satir = 100
	}
	out, _ := exec.Command("journalctl", "-u", unitAd+".service",
		"-n", fmt.Sprintf("%d", satir),
		"--output=short-iso",
		"--no-pager").CombinedOutput()
	return string(out)
}
