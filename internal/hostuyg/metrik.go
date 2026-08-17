package hostuyg

// Fleet monitoring — kurulu app'lerin canlı kaynak metrikleri.
//
// systemd cgroup v2 üzerinden:
//   /sys/fs/cgroup/system.slice/<unit>.service/memory.current  → RAM byte
//   /sys/fs/cgroup/system.slice/<unit>.service/memory.peak      → peak RAM
//   /sys/fs/cgroup/system.slice/<unit>.service/cpu.stat         → CPU usec
//   /sys/fs/cgroup/system.slice/<unit>.service/pids.current     → task
//   /sys/fs/cgroup/system.slice/<unit>.service/pids.max         → limit
//
// Ayrıca:
//   systemctl show <unit> -p ActiveState,SubState,ActiveEnterTimestamp,NRestarts
//   du -sb <kurulum_yolu>  → disk kullanımı (yavaş, ayrı cache düşünülebilir)

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const CGroupKok = "/sys/fs/cgroup/system.slice"

type Metrik struct {
	UygulamaID     int64     `json:"uygulama_id"`
	UnitAd         string    `json:"unit_ad"`
	CGroupYol      string    `json:"cgroup_yol,omitempty"`
	BellekByte     int64     `json:"bellek_byte"`
	BellekPeakByte int64     `json:"bellek_peak_byte"`
	CPUToplamUsec  int64     `json:"cpu_toplam_usec"` // cumulative usage
	CPUYuzde       float64   `json:"cpu_yuzde"`       // son iki snapshot delta (varsa)
	TaskSayisi     int       `json:"task_sayisi"`
	TaskMax        int       `json:"task_max"`
	DiskByte       int64     `json:"disk_byte"`
	AktifDurum     string    `json:"aktif_durum"`      // active/inactive/failed/activating/deactivating
	AltDurum       string    `json:"alt_durum"`        // running/dead/failed/…
	Uptime         string    `json:"uptime,omitempty"` // "2h 15m"
	RestartSayi    int       `json:"restart_sayi"`
	Zaman          time.Time `json:"zaman"`
}

// CPU delta cache — anlık % ölçümü için 2 örnek arası fark.
type cpuSample struct {
	toplamUsec int64
	zaman      time.Time
}

var (
	cpuSamplesMu sync.Mutex
	cpuSamples   = map[string]cpuSample{}
)

// Disk kullanımı cache — `du -sb` pahalı (500ms+ Grafana gibi 500MB app'ler
// için). 5dk TTL — disk boyutu yavaş değişir, canlı doğruluk gerekmez.
type diskCache struct {
	byte  int64
	zaman time.Time
}

var (
	diskCacheMu sync.Mutex
	diskCacheM  = map[string]diskCache{}
)

const diskCacheTTL = 5 * time.Minute

// MetrikCacheSil — Kaldır sonrası cleanup (CPU sample + disk cache).
// Handler kaldirYurut sonrası çağırır; MetrikTumu de eksik unit'leri temizler.
func MetrikCacheSil(unitAd, kurulumYolu string) {
	cpuSamplesMu.Lock()
	delete(cpuSamples, unitAd)
	cpuSamplesMu.Unlock()
	if kurulumYolu != "" {
		diskCacheMu.Lock()
		delete(diskCacheM, kurulumYolu)
		diskCacheMu.Unlock()
	}
}

// MetrikTopla — tek app için snapshot metrik.
func MetrikTopla(ctx context.Context, k *UygulamaKayit) Metrik {
	m := Metrik{
		UygulamaID: k.ID,
		UnitAd:     k.SystemdUnit,
		Zaman:      time.Now(),
	}
	cgYol := filepath.Join(CGroupKok, k.SystemdUnit+".service")
	m.CGroupYol = cgYol

	m.BellekByte = intOku(filepath.Join(cgYol, "memory.current"))
	m.BellekPeakByte = intOku(filepath.Join(cgYol, "memory.peak"))
	m.TaskSayisi = int(intOku(filepath.Join(cgYol, "pids.current")))
	if v := stringOku(filepath.Join(cgYol, "pids.max")); v != "" {
		if v == "max" {
			m.TaskMax = -1 // frontend "∞" render
		} else {
			m.TaskMax, _ = strconv.Atoi(v)
		}
	}
	m.CPUToplamUsec = cpuStatOku(filepath.Join(cgYol, "cpu.stat"))

	// CPU% — son örnekle delta (mutex ile paralel-safe)
	// dt < 500ms ise gürültülü — sayacı güncelleme, önceki değeri koru.
	cpuSamplesMu.Lock()
	if prev, ok := cpuSamples[k.SystemdUnit]; ok && m.CPUToplamUsec > 0 {
		dt := m.Zaman.Sub(prev.zaman).Microseconds()
		if dt >= 500_000 { // 500ms minimum sample aralığı
			delta := m.CPUToplamUsec - prev.toplamUsec
			if delta >= 0 {
				m.CPUYuzde = float64(delta) * 100.0 / float64(dt)
			}
			cpuSamples[k.SystemdUnit] = cpuSample{
				toplamUsec: m.CPUToplamUsec,
				zaman:      m.Zaman,
			}
		}
		// dt < 500ms → sample güncellenmez, CPUYuzde=0 (bilinmiyor)
	} else {
		// İlk baseline
		cpuSamples[k.SystemdUnit] = cpuSample{
			toplamUsec: m.CPUToplamUsec,
			zaman:      m.Zaman,
		}
	}
	cpuSamplesMu.Unlock()

	// systemctl show — durum + uptime + restart
	sysCtx, iptal := context.WithTimeout(ctx, 3*time.Second)
	defer iptal()
	if out, err := exec.CommandContext(sysCtx, "systemctl", "show", k.SystemdUnit+".service",
		"-p", "ActiveState,SubState,ActiveEnterTimestamp,NRestarts").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			key, v, ok := strings.Cut(line, "=") // outer `k *UygulamaKayit` shadow'lanmasın
			if !ok {
				continue
			}
			switch key {
			case "ActiveState":
				m.AktifDurum = v
			case "SubState":
				m.AltDurum = v
			case "ActiveEnterTimestamp":
				// systemd `n/a` unit hiç start etmemişse; boş veya "0" da olası
				if v != "" && v != "0" && v != "n/a" {
					// systemd format "Sat 2026-08-15 12:34:56 UTC"
					if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", v); err == nil && !t.IsZero() {
						m.Uptime = kisaSure(time.Since(t))
					}
				}
			case "NRestarts":
				m.RestartSayi, _ = strconv.Atoi(v)
			}
		}
	}

	// Disk (5dk cache — pahalı)
	m.DiskByte = diskByteOku(ctx, k.KurulumYolu)
	return m
}

// diskByteOku — cache önce; miss/expire ise `du -sb`.
func diskByteOku(ctx context.Context, yol string) int64 {
	if yol == "" {
		return 0
	}
	diskCacheMu.Lock()
	if c, ok := diskCacheM[yol]; ok && time.Since(c.zaman) < diskCacheTTL {
		diskCacheMu.Unlock()
		return c.byte
	}
	diskCacheMu.Unlock()

	diskCtx, iptal := context.WithTimeout(ctx, 3*time.Second)
	defer iptal()
	// -sbx: mount cross ETME (CageFS/bindmount'lara dalmasın).
	out, err := exec.CommandContext(diskCtx, "du", "-sbx", yol).Output()
	if err != nil {
		return 0
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return 0
	}
	b, _ := strconv.ParseInt(f[0], 10, 64)
	diskCacheMu.Lock()
	diskCacheM[yol] = diskCache{byte: b, zaman: time.Now()}
	diskCacheMu.Unlock()
	return b
}

// MetrikTumu — tüm kurulu app'ler için paralel snapshot.
// DB hatası → log + boş slice (caller nil ile karışmasın).
func MetrikTumu(ctx context.Context, db *sql.DB) []Metrik {
	kayitlar, err := UygulamaListe(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hostuyg.MetrikTumu: DB fetch: %v\n", err)
		return []Metrik{}
	}
	// CPU cache temizle: DB'de olmayan (kaldırılmış) unit'lerin sample'ı kalmasın
	dbUnit := map[string]bool{}
	for _, k := range kayitlar {
		dbUnit[k.SystemdUnit] = true
	}
	cpuSamplesMu.Lock()
	for unit := range cpuSamples {
		if !dbUnit[unit] {
			delete(cpuSamples, unit)
		}
	}
	cpuSamplesMu.Unlock()
	out := make([]Metrik, len(kayitlar))
	// Sıralı: du komutu 2s timeout, N app için sequential
	// (paralel du IO yükü). Kabul edilebilir.
	for i, k := range kayitlar {
		out[i] = MetrikTopla(ctx, &k)
	}
	return out
}

/* ---- yardımcılar ---- */

func intOku(yol string) int64 {
	b, err := os.ReadFile(yol)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return -1
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func stringOku(yol string) string {
	b, err := os.ReadFile(yol)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// cpu.stat format:
//
//	usage_usec 12345
//	user_usec 6789
//	system_usec 4567
func cpuStatOku(yol string) int64 {
	b, err := os.ReadFile(yol)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "usage_usec ") {
			v, _ := strconv.ParseInt(strings.TrimPrefix(line, "usage_usec "), 10, 64)
			return v
		}
	}
	return 0
}

func kisaSure(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	gun := int(d.Hours()) / 24
	return fmt.Sprintf("%dg", gun)
}
