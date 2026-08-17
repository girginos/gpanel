package optimize

// Sistem ölçüm: RAM, CPU, load, disk, swap, kernel + servis durumları.
//
// Tüm ölçüm root okumaya dayanır (procfs + basit komut). Panel root çalıştığı
// için sorun yok. Her ölçüm ucuz — GET /optimize/analiz her tıklamada yenile.

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Sistem struct {
	RAMToplamMB  int     `json:"ram_toplam_mb"`
	RAMKullanMB  int     `json:"ram_kullan_mb"`
	RAMBofBuffMB int     `json:"ram_buf_cache_mb"`
	SwapToplamMB int     `json:"swap_toplam_mb"`
	SwapKullanMB int     `json:"swap_kullan_mb"`
	CPUCekirdek  int     `json:"cpu_cekirdek"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	DiskToplamGB float64 `json:"disk_toplam_gb"`
	DiskKullanGB float64 `json:"disk_kullan_gb"`
	DiskYuzde    int     `json:"disk_yuzde"`
	UptimeSaniye int64   `json:"uptime_saniye"`
	Kernel       string  `json:"kernel"`
	Dagitim      string  `json:"dagitim"`
	Profil       string  `json:"profil"` // "kucuk" | "orta" | "buyuk" | "cok_buyuk"
	SwapKullanim int     `json:"swap_kullanim_yuzde"`
}

func SistemOku() *Sistem {
	s := &Sistem{CPUCekirdek: runtime.NumCPU()}

	// /proc/meminfo
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		vals := map[string]int{}
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				vals[strings.TrimSuffix(f[0], ":")] = n / 1024 // kB → MB
			}
		}
		s.RAMToplamMB = vals["MemTotal"]
		s.RAMBofBuffMB = vals["Buffers"] + vals["Cached"]
		s.RAMKullanMB = s.RAMToplamMB - vals["MemAvailable"]
		s.SwapToplamMB = vals["SwapTotal"]
		s.SwapKullanMB = vals["SwapTotal"] - vals["SwapFree"]
	}
	if s.SwapToplamMB > 0 {
		s.SwapKullanim = (s.SwapKullanMB * 100) / s.SwapToplamMB
	}

	// /proc/loadavg
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(strings.TrimSpace(string(b)))
		if len(f) >= 3 {
			s.Load1, _ = strconv.ParseFloat(f[0], 64)
			s.Load5, _ = strconv.ParseFloat(f[1], 64)
			s.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}

	// /proc/uptime
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if v, err := strconv.ParseFloat(strings.Fields(string(b))[0], 64); err == nil {
			s.UptimeSaniye = int64(v)
		}
	}

	// Disk /
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		toplam := float64(st.Blocks*uint64(st.Bsize)) / (1 << 30)
		bosB := float64(st.Bavail*uint64(st.Bsize)) / (1 << 30)
		s.DiskToplamGB = toplam
		s.DiskKullanGB = toplam - bosB
		if toplam > 0 {
			s.DiskYuzde = int((s.DiskKullanGB / toplam) * 100)
		}
	}

	// Kernel
	var uts syscall.Utsname
	if syscall.Uname(&uts) == nil {
		s.Kernel = utsInt8ToStr(uts.Release[:])
	}
	// Dağıtım (best-effort)
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "PRETTY_NAME=") {
				s.Dagitim = strings.Trim(strings.TrimPrefix(ln, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	// Profil (RAM bazlı — MariaDB script'iyle aynı eşikler)
	switch {
	case s.RAMToplamMB < 2048:
		s.Profil = "kucuk"
	case s.RAMToplamMB < 4096:
		s.Profil = "orta"
	case s.RAMToplamMB < 8192:
		s.Profil = "buyuk"
	default:
		s.Profil = "cok_buyuk"
	}
	return s
}

func utsInt8ToStr(b []int8) string {
	buf := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}

// ServisAktif — systemd is-active kontrolü.
func ServisAktif(unit string) bool {
	out, _ := exec.Command("systemctl", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// ServisDurum — durum + uptime + memory.
type ServisDurum struct {
	Aktif     bool   `json:"aktif"`
	Durum     string `json:"durum"`
	MemoryMB  int    `json:"memory_mb"`
	StartTime string `json:"start_time"`
	Restarts  int    `json:"restarts"`
}

func ServisDurumOku(unit string) *ServisDurum {
	d := &ServisDurum{}
	if out, err := exec.Command("systemctl", "show", unit,
		"-p", "ActiveState", "-p", "MemoryCurrent",
		"-p", "ExecMainStartTimestamp", "-p", "NRestarts").Output(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(ln, "=")
			if !ok {
				continue
			}
			switch k {
			case "ActiveState":
				d.Durum = v
				d.Aktif = v == "active"
			case "MemoryCurrent":
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
					d.MemoryMB = int(n / (1024 * 1024))
				}
			case "ExecMainStartTimestamp":
				d.StartTime = v
			case "NRestarts":
				d.Restarts, _ = strconv.Atoi(v)
			}
		}
	}
	return d
}
