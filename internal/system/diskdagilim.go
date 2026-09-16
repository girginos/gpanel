package system

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/httpx"
)

// Disk dagilimi — panonun "Depolama Kullanimi" halkasi icin DIZIN BAZLI kirilim
// (host / sql / yedek / log).
//
// 🔴 NEDEN CACHE + PARALEL: `du -sb` buyuk dizinlerde saniyeler surer. Pano 5
// saniyede bir yenileniyor; her istekte du kosmak sunucuyu bosuna yorardi.
// Sonuc 5 dakika cache'lenir ve dort dizin PARALEL olculur (seri olsaydi ilk
// istek dort katina cikardi). Zaman asimina ugrayan dizin 0 doner, istek asilmaz.

type DagilimKalem struct {
	Anahtar string `json:"anahtar"`
	Ad      string `json:"ad"`
	Yol     string `json:"yol"`
	Byte    int64  `json:"byte"`
}

type DiskDagilimYanit struct {
	Kalemler       []DagilimKalem `json:"kalemler"`
	ToplamByte     int64          `json:"toplam_byte"`
	KullanilanByte int64          `json:"kullanilan_byte"`
	Olculdu        string         `json:"olculdu"`
}

var (
	ddMu    sync.Mutex
	ddCache *DiskDagilimYanit
	ddZaman time.Time
)

const ddTTL = 5 * time.Minute

// duByte — `du -sbx`: -s ozet, -b bayt, -x dosya sistemi sinirinda kal
// (baska bir mount'a tasip toplami sisirmesin).
func duByte(yol string) int64 {
	ctx, iptal := context.WithTimeout(context.Background(), 12*time.Second)
	defer iptal()
	out, err := exec.CommandContext(ctx, "du", "-sbx", yol).Output()
	if err != nil {
		return 0
	}
	alan := strings.Fields(string(out))
	if len(alan) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(alan[0], 10, 64)
	return n
}

// yedekKok — yedeklerin gercekte durdugu dizin kurulumdan kuruluma degisir.
func yedekKok() string {
	for _, c := range []string{
		"/var/backups/girginospanel",
		"/opt/girginospanel/yedekler",
		"/opt/girginospanel/backups",
		"/var/backups",
	} {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

// DiskDagilim — GET /system/disk-dagilim (AdminOnly)
func DiskDagilim(w http.ResponseWriter, r *http.Request) {
	ddMu.Lock()
	if ddCache != nil && time.Since(ddZaman) < ddTTL {
		y := *ddCache
		ddMu.Unlock()
		httpx.WriteJSON(w, http.StatusOK, y)
		return
	}
	ddMu.Unlock()

	tanim := []DagilimKalem{
		{Anahtar: "host", Ad: "Host dizini", Yol: "/home"},
		{Anahtar: "sql", Ad: "SQL dizini", Yol: "/var/lib/mysql"},
		{Anahtar: "yedek", Ad: "Yedek dizini", Yol: yedekKok()},
		{Anahtar: "log", Ad: "Log dizini", Yol: "/var/log"},
	}

	var wg sync.WaitGroup
	for i := range tanim {
		if tanim[i].Yol == "" {
			continue
		}
		if st, err := os.Stat(tanim[i].Yol); err != nil || !st.IsDir() {
			tanim[i].Yol = ""
			continue
		}
		wg.Add(1)
		go func(k *DagilimKalem) {
			defer wg.Done()
			k.Byte = duByte(k.Yol)
		}(&tanim[i])
	}
	wg.Wait()

	y := DiskDagilimYanit{Olculdu: time.Now().Format("2006-01-02 15:04:05")}
	for _, k := range tanim {
		if k.Yol != "" {
			y.Kalemler = append(y.Kalemler, k)
		}
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil {
		y.ToplamByte = int64(fs.Blocks) * int64(fs.Bsize)
		y.KullanilanByte = int64(fs.Blocks-fs.Bfree) * int64(fs.Bsize)
	}

	ddMu.Lock()
	ddCache = &y
	ddZaman = time.Now()
	ddMu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, y)
}
