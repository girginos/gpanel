package system

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/provisioner"
)

const PanelSurum = "GirginOSPanel 0.2.0"

type CPUUsage struct {
	Yuzde    float64 `json:"yuzde"`
	Cekirdek int     `json:"cekirdek"`
	Yuk1     float64 `json:"yuk_1dk"`
	Yuk5     float64 `json:"yuk_5dk"`
	Yuk15    float64 `json:"yuk_15dk"`
}

type MemUsage struct {
	ToplamKB     int64   `json:"toplam_kb"`
	KullanilanKB int64   `json:"kullanilan_kb"`
	BosKB        int64   `json:"bos_kb"`
	Yuzde        float64 `json:"yuzde"`
}

type SwapUsage struct {
	ToplamKB     int64   `json:"toplam_kb"`
	KullanilanKB int64   `json:"kullanilan_kb"`
	Yuzde        float64 `json:"yuzde"`
}

type DiskUsage struct {
	ToplamByte     uint64  `json:"toplam_byte"`
	KullanilanByte uint64  `json:"kullanilan_byte"`
	BosByte        uint64  `json:"bos_byte"`
	Yuzde          float64 `json:"yuzde"`
	Mount          string  `json:"mount"`
	FS             string  `json:"fs,omitempty"`
}

type AgUsage struct {
	Arayuz   string `json:"arayuz"`
	RxBytes  int64  `json:"rx_bytes_sn"`
	TxBytes  int64  `json:"tx_bytes_sn"`
	RxToplam int64  `json:"rx_toplam_byte"`
	TxToplam int64  `json:"tx_toplam_byte"`
}

type ServiceStat struct {
	Ad     string `json:"ad"`
	Etiket string `json:"etiket"`
	Aktif  bool   `json:"aktif"`
}

type SystemInfo struct {
	Hostname    string `json:"hostname"`
	IP          string `json:"ip"`
	OSAdi       string `json:"os_adi"`
	Kernel      string `json:"kernel"`
	Mimari      string `json:"mimari"`
	CPUModeli   string `json:"cpu_modeli"`
	CPUCekirdek int    `json:"cpu_cekirdek"`
	PanelSurum  string `json:"panel_surum"`
}

type Usage struct {
	Sistem    SystemInfo    `json:"sistem"`
	CPU       CPUUsage      `json:"cpu"`
	Bellek    MemUsage      `json:"bellek"`
	Swap      SwapUsage     `json:"swap"`
	Disk      DiskUsage     `json:"disk"`
	Diskler   []DiskUsage   `json:"diskler"`
	Ag        AgUsage       `json:"ag"`
	Servisler []ServiceStat `json:"servisler"`
	UptimeSn  int64         `json:"uptime_sn"`

	// KotaRebootGerekli: disk kotası enforcement AKTİF DEĞİL (fs noquota / uqnoenforce) →
	// tek seferlik reboot bekliyor. UI'da sarı uyarı banner'ı bunu okur.
	KotaRebootGerekli bool `json:"kota_reboot_gerekli"`

	// IzolasyonKaybi: 🔴 GUVENLIK — su anda CageFS (per-tenant FPM) izolasyonu OLMAYAN
	// tenant'lar. Dolu ise UI'da KIRMIZI banner cikar. Bos slice = her sey yolunda.
	IzolasyonKaybi []string `json:"izolasyon_kaybi"`
}

type cpuStat struct{ total, idle uint64 }

func readCPUStat() (cpuStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStat{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			var total, idle uint64
			for i, fld := range fields[1:] {
				n, _ := strconv.ParseUint(fld, 10, 64)
				total += n
				if i == 3 {
					idle = n
				}
			}
			return cpuStat{total: total, idle: idle}, nil
		}
	}
	return cpuStat{}, fmt.Errorf("cpu satiri bulunamadi")
}

func ReadCPU() (CPUUsage, error) {
	s1, err := readCPUStat()
	if err != nil {
		return CPUUsage{}, err
	}
	time.Sleep(150 * time.Millisecond)
	s2, err := readCPUStat()
	if err != nil {
		return CPUUsage{}, err
	}
	totalDelta := float64(s2.total - s1.total)
	idleDelta := float64(s2.idle - s1.idle)
	pct := 0.0
	if totalDelta > 0 {
		pct = 100.0 * (totalDelta - idleDelta) / totalDelta
	}
	var load1, load5, load15 float64
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 3 {
			load1, _ = strconv.ParseFloat(f[0], 64)
			load5, _ = strconv.ParseFloat(f[1], 64)
			load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	return CPUUsage{
		Yuzde:    round2(pct),
		Cekirdek: runtime.NumCPU(),
		Yuk1:     load1, Yuk5: load5, Yuk15: load15,
	}, nil
}

func ReadMem() (MemUsage, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemUsage{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var total, free, buffers, cached, available int64
	for s.Scan() {
		line := s.Text()
		var v int64
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			fmt.Sscanf(line, "MemTotal: %d kB", &v)
			total = v
		case strings.HasPrefix(line, "MemFree:"):
			fmt.Sscanf(line, "MemFree: %d kB", &v)
			free = v
		case strings.HasPrefix(line, "Buffers:"):
			fmt.Sscanf(line, "Buffers: %d kB", &v)
			buffers = v
		case strings.HasPrefix(line, "Cached:"):
			fmt.Sscanf(line, "Cached: %d kB", &v)
			cached = v
		case strings.HasPrefix(line, "MemAvailable:"):
			fmt.Sscanf(line, "MemAvailable: %d kB", &v)
			available = v
		}
	}
	used := total - available
	if used <= 0 {
		used = total - free - buffers - cached
	}
	pct := 0.0
	if total > 0 {
		pct = 100.0 * float64(used) / float64(total)
	}
	return MemUsage{
		ToplamKB:     total,
		KullanilanKB: used,
		BosKB:        total - used,
		Yuzde:        round2(pct),
	}, nil
}

func ReadSwap() SwapUsage {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return SwapUsage{}
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var total, free int64
	for s.Scan() {
		line := s.Text()
		var v int64
		switch {
		case strings.HasPrefix(line, "SwapTotal:"):
			fmt.Sscanf(line, "SwapTotal: %d kB", &v)
			total = v
		case strings.HasPrefix(line, "SwapFree:"):
			fmt.Sscanf(line, "SwapFree: %d kB", &v)
			free = v
		}
	}
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = 100.0 * float64(used) / float64(total)
	}
	return SwapUsage{ToplamKB: total, KullanilanKB: used, Yuzde: round2(pct)}
}

func ReadDisk(mount string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mount, &st); err != nil {
		return DiskUsage{}, err
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = 100.0 * float64(used) / float64(total)
	}
	return DiskUsage{
		ToplamByte:     total,
		KullanilanByte: used,
		BosByte:        free,
		Yuzde:          round2(pct),
		Mount:          mount,
	}, nil
}

func ReadDiskler() []DiskUsage {
	skipFS := map[string]bool{
		"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true,
		"devpts": true, "cgroup": true, "cgroup2": true, "pstore": true,
		"bpf": true, "tracefs": true, "debugfs": true, "configfs": true,
		"securityfs": true, "fusectl": true, "mqueue": true, "hugetlbfs": true,
		"binfmt_misc": true, "autofs": true, "rpc_pipefs": true, "nfsd": true,
		"selinuxfs": true, "fuse.gvfsd-fuse": true, "overlay": true, "squashfs": true,
		"ramfs": true,
	}
	skipPrefix := []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/var/lib/containers", "/snap", "/home/jails"}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	seen := map[string]bool{}
	seenDev := map[string]bool{} // mükerrer /dev aygıtı (bind mount) eleme
	out := []DiskUsage{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) < 3 {
			continue
		}
		dev := parts[0]
		mount := parts[1]
		fs := parts[2]
		if skipFS[fs] {
			continue
		}
		skip := false
		for _, p := range skipPrefix {
			if strings.HasPrefix(mount, p+"/") || mount == p {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true
		// bind-mount / mükerrer aygıt eleme: jail home bind'leri kök ile aynı /dev'i paylaşır,
		// aynı boyutu tekrar gösterir. Gerçek blok aygıtı bir kez listele.
		if strings.HasPrefix(dev, "/dev/") {
			if seenDev[dev] {
				continue
			}
			seenDev[dev] = true
		}
		d, err := ReadDisk(mount)
		if err != nil {
			continue
		}
		d.FS = fs
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}

var (
	agMu     sync.Mutex
	agOnceki = map[string]agSnap{}
)

type agSnap struct {
	rx, tx int64
	t      time.Time
}

func ReadAg() AgUsage {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return AgUsage{}
	}
	defer f.Close()
	type rec struct {
		name   string
		rx, tx int64
	}
	var stats []rec
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") ||
			strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "tap") ||
			strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "wg") ||
			strings.HasPrefix(name, "virbr") || strings.HasPrefix(name, "vnet") {
			continue
		}
		fld := strings.Fields(line[i+1:])
		if len(fld) < 9 {
			continue
		}
		rx, _ := strconv.ParseInt(fld[0], 10, 64)
		tx, _ := strconv.ParseInt(fld[8], 10, 64)
		stats = append(stats, rec{name: name, rx: rx, tx: tx})
	}
	if len(stats) == 0 {
		return AgUsage{}
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].rx > stats[j].rx })
	primary := stats[0]
	now := time.Now()
	agMu.Lock()
	defer agMu.Unlock()
	prev, vardi := agOnceki[primary.name]
	agOnceki[primary.name] = agSnap{rx: primary.rx, tx: primary.tx, t: now}
	var rxRate, txRate int64
	if vardi {
		dt := now.Sub(prev.t).Seconds()
		if dt > 0 {
			rxRate = int64(float64(primary.rx-prev.rx) / dt)
			txRate = int64(float64(primary.tx-prev.tx) / dt)
			if rxRate < 0 {
				rxRate = 0
			}
			if txRate < 0 {
				txRate = 0
			}
		}
	}
	return AgUsage{
		Arayuz: primary.name, RxBytes: rxRate, TxBytes: txRate,
		RxToplam: primary.rx, TxToplam: primary.tx,
	}
}

func ReadUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(data))
	if len(f) > 0 {
		sec, _ := strconv.ParseFloat(f[0], 64)
		return int64(sec)
	}
	return 0
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}

func birinciIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		n := iface.Name
		if strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "br-") ||
			strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "tap") ||
			strings.HasPrefix(n, "tun") || strings.HasPrefix(n, "wg") ||
			strings.HasPrefix(n, "virbr") || strings.HasPrefix(n, "vnet") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				ip := ipnet.IP.To4()
				if ip == nil {
					continue
				}
				if ip.IsPrivate() || ip.IsLoopback() {
					continue
				}
				return ip.String()
			}
		}
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ip := ipnet.IP.To4(); ip != nil && !ip.IsLoopback() {
					return ip.String()
				}
			}
		}
	}
	return ""
}

var (
	infoCache   *SystemInfo
	infoCacheMu sync.Mutex
)

func ReadInfo() SystemInfo {
	infoCacheMu.Lock()
	defer infoCacheMu.Unlock()
	if infoCache != nil {
		c := *infoCache
		c.IP = birinciIP()
		return c
	}
	info := SystemInfo{PanelSurum: PanelSurum, CPUCekirdek: runtime.NumCPU(), Mimari: runtime.GOARCH}
	info.Hostname, _ = os.Hostname()
	info.IP = birinciIP()
	var u syscall.Utsname
	if err := syscall.Uname(&u); err == nil {
		info.Kernel = utsToString(u.Release[:])
	}
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				v := strings.TrimPrefix(line, "PRETTY_NAME=")
				v = strings.Trim(v, "\"'")
				info.OSAdi = v
				break
			}
		}
	}
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := s.Text()
			if strings.HasPrefix(line, "model name") {
				if i := strings.Index(line, ":"); i >= 0 {
					info.CPUModeli = strings.TrimSpace(line[i+1:])
					break
				}
			}
		}
		f.Close()
	}
	cached := info
	infoCache = &cached
	return info
}

func utsToString(b []int8) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}

var servisListesi = []struct{ ad, etiket string }{
	{"girginospanel", "Panel"},
	{"nginx", "Nginx"},
	{"mariadb", "MariaDB"},
	{"pure-ftpd-mysql", "FTP (Pure-FTPd)"},
	{"pure-ftpd", "FTP"},
	{"named", "DNS (BIND)"},
	{"php74-php-fpm", "PHP 7.4-FPM"},
	{"php82-php-fpm", "PHP 8.2-FPM"},
	{"php83-php-fpm", "PHP 8.3-FPM"},
	{"php84-php-fpm", "PHP 8.4-FPM"},
	{"crond", "Cron"},
	{"sshd", "SSH"},
	{"firewalld", "Firewalld"},
}

func ReadServisler() []ServiceStat {
	out := make([]ServiceStat, 0, len(servisListesi))
	type res struct {
		i     int
		aktif bool
	}
	ch := make(chan res, len(servisListesi))
	for i, s := range servisListesi {
		go func(i int, ad string) {
			cmd := exec.Command("systemctl", "is-active", ad)
			b, _ := cmd.Output()
			ch <- res{i: i, aktif: strings.TrimSpace(string(b)) == "active"}
		}(i, s.ad)
	}
	mat := make(map[int]bool)
	for i := 0; i < len(servisListesi); i++ {
		r := <-ch
		mat[r.i] = r.aktif
	}
	for i, s := range servisListesi {
		aktif := mat[i]
		// Kurulu DEGILSE (unit-file yok) servis absent'tir; "kapali" gosterip operatoru YANILTMA.
		// Yalniz kurulu-ama-duruk (unit var, inactive) gercek sorun olarak gorunur.
		if !aktif {
			cmd := exec.Command("systemctl", "list-unit-files", s.ad+".service", "--no-legend")
			b, _ := cmd.Output()
			if len(strings.TrimSpace(string(b))) == 0 {
				continue
			}
		}
		out = append(out, ServiceStat{Ad: s.ad, Etiket: s.etiket, Aktif: aktif})
	}
	return out
}

func Handler(w http.ResponseWriter, r *http.Request) {
	cpu, _ := ReadCPU()
	mem, _ := ReadMem()
	disk, _ := ReadDisk("/")

	var diskler []DiskUsage
	var ag AgUsage
	var servisler []ServiceStat
	var swap SwapUsage
	var info SystemInfo
	var kotaReboot bool
	var izolasyonKaybi []string

	var wg sync.WaitGroup
	wg.Add(7)
	go func() { defer wg.Done(); diskler = ReadDiskler() }()
	go func() { defer wg.Done(); ag = ReadAg() }()
	go func() { defer wg.Done(); servisler = ReadServisler() }()
	go func() { defer wg.Done(); swap = ReadSwap() }()
	go func() { defer wg.Done(); info = ReadInfo() }()
	go func() { defer wg.Done(); kotaReboot = kaynaklimit.KotaRebootGerekli() }()
	go func() { defer wg.Done(); izolasyonKaybi = provisioner.IzolasyonKaybiListesi() }()
	wg.Wait()

	httpx.WriteJSON(w, http.StatusOK, Usage{
		Sistem: info, CPU: cpu, Bellek: mem, Swap: swap,
		Disk: disk, Diskler: diskler, Ag: ag,
		Servisler: servisler, UptimeSn: ReadUptime(),
		KotaRebootGerekli: kotaReboot,
		IzolasyonKaybi:    izolasyonKaybi,
	})
}
