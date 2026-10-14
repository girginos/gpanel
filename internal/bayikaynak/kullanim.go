package bayikaynak

// kullanim.go — havuz kullanım verisi: 5 dk çözünürlüklü zaman serisi (30 gün saklama),
// toplayıcı döngü ve okuma. Bayi grafiği (arayüz sonra) buradan beslenir.
//
// Kaynak: havuz diliminin cgroup dosyaları (cpu.stat, memory.current, memory.events, io.stat);
// disk için proje sayacı → kullanıcı kotası → veritabanı sırasıyla (DiskKullanimlari).
// Havuz dilimi yoksa ya da bazı üyeler henüz havuza taşınmadıysa o üyelerin ESKİ dilimleri
// toplanır, ölçüm eksik kalmaz (Kaynak = "hosting_toplami"). Havuzu olmayan bayide (hiç
// limit verilmemiş) örnek ALINMAZ: tablo bayi sayısıyla değil havuz sayısıyla büyür.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/bayidilim"
	"girginospanel/internal/cgroup"
)

// Örnekleme/saklama sabitleri.
const (
	OrnekAraligi = 5 * time.Minute
	Saklama      = 30 * 24 * time.Hour
)

// Nokta: bir zaman dilimi için bayi kullanımı. nil = ölçülemedi (ilk örnek, sayaç sıfırlandı).
type Nokta struct {
	T             string `json:"t"` // UTC, RFC3339
	CPUMilli      *int   `json:"cpu_milli"`
	CPUSinirMilli int    `json:"cpu_sinir_milli"`
	KisitlamaMS   *int   `json:"kisitlama_ms"`
	RAMMB         *int   `json:"ram_mb"`
	RAMSinirMB    int64  `json:"ram_sinir_mb"`
	OOM           *int   `json:"oom_olay"`
	DiskMB        *int64 `json:"disk_mb"`
	DiskSinirMB   int64  `json:"disk_sinir_mb"`
	IOOkuKBs      *int   `json:"io_oku_kbs"`
	IOYazKBs      *int   `json:"io_yaz_kbs"`
	Kaynak        string `json:"kaynak"`
}

// ham: bir bayinin birikimli cgroup sayaçları.
type ham struct {
	zaman         time.Time
	cpuUsec       int64
	kisitUsec     int64
	oom           int64
	ioR, ioW      int64
	ramBayt       int64
	cpuVar, ioVar bool
}

func (a *ham) topla(b ham) {
	a.cpuUsec += b.cpuUsec
	a.kisitUsec += b.kisitUsec
	a.oom += b.oom
	a.ioR += b.ioR
	a.ioW += b.ioW
	a.ramBayt += b.ramBayt
	a.cpuVar = a.cpuVar || b.cpuVar
	a.ioVar = a.ioVar || b.ioVar
}

func intNil(v int) *int { return &v }

func sayac(yol, anahtar string) (int64, bool) {
	b, err := os.ReadFile(yol)
	if err != nil {
		return 0, false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(ln, anahtar+" "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// dilimOku: bir cgroup dizininden sayaçlar; bellek okunamazsa ok=false (dizin canlı değil).
func dilimOku(dizin string) (ham, bool) {
	var h ham
	ram, ok := cgroup.Deger(filepath.Join(dizin, "memory.current"))
	if !ok {
		return h, false
	}
	h.ramBayt = ram
	if v, ok := cgroup.CPUUsec(dizin); ok {
		h.cpuUsec, h.cpuVar = v, true
	}
	h.kisitUsec, _ = sayac(filepath.Join(dizin, "cpu.stat"), "throttled_usec")
	h.oom, _ = sayac(filepath.Join(dizin, "memory.events"), "oom_kill")
	if r, w, ok := cgroup.IOToplam(dizin); ok {
		h.ioR, h.ioW, h.ioVar = r, w, true
	}
	return h, true
}

// bayiOrnekle: havuz dilimi + (henüz havuza girmemiş) üyelerin eski dilimleri.
func bayiOrnekle(id int64, uyeler []Uye) (ham, string, bool) {
	var toplam ham
	kaynak := ""
	havuzda := false
	if g := bayidilim.BayiCgroupGoreli(id); g != "" {
		if h, ok := dilimOku(filepath.Join(cgroup.Kok, g)); ok {
			toplam.topla(h)
			kaynak, havuzda = "havuz", true
		}
	}
	eklenen := 0
	for _, u := range uyeler {
		if havuzda {
			if st, err := os.Stat(filepath.Join(cgroup.Kok, "girginos.slice", bayidilim.BayiDilim(id), bayidilim.UyeDilim(id, u.SK))); err == nil && st.IsDir() {
				continue // zaten havuz diliminin içinde
			}
		}
		if h, ok := dilimOku(filepath.Join(cgroup.Kok, "girginos.slice", bayidilim.EskiDilim(u.SK))); ok {
			toplam.topla(h)
			eklenen++
		}
	}
	if kaynak == "" {
		if eklenen == 0 {
			return toplam, "", false
		}
		kaynak = "hosting_toplami"
	}
	return toplam, kaynak, true
}

// hizlar: iki birikimli örnekten aralık değerleri (sayaç geri gittiyse nil).
func hizlar(a, b ham) (cpuMilli, kisitMS *int, oom *int, ioOkuKBs, ioYazKBs *int) {
	dt := b.zaman.Sub(a.zaman)
	if dt <= 0 {
		return
	}
	if a.cpuVar && b.cpuVar && b.cpuUsec >= a.cpuUsec {
		v := int(float64(b.cpuUsec-a.cpuUsec) * 1000 / float64(dt.Microseconds()))
		cpuMilli = &v
		if b.kisitUsec >= a.kisitUsec {
			k := int((b.kisitUsec - a.kisitUsec) / 1000)
			kisitMS = &k
		}
	}
	if b.oom >= a.oom {
		o := int(b.oom - a.oom)
		oom = &o
	}
	if a.ioVar && b.ioVar && b.ioR >= a.ioR && b.ioW >= a.ioW {
		r := int(float64(b.ioR-a.ioR) / 1024 / dt.Seconds())
		w := int(float64(b.ioW-a.ioW) / 1024 / dt.Seconds())
		ioOkuKBs, ioYazKBs = &r, &w
	}
	return
}

// Toplayici: periyodik örnekleyici (bellek içi önceki sayaçlar).
type Toplayici struct {
	DB     *sql.DB
	onceki map[int64]ham
	budama time.Time
}

// Tek: bir örnekleme turu. Hata bir bayiyi atlatır, tur sürer.
func (t *Toplayici) Tek(ctx context.Context, simdi time.Time) error {
	if t.onceki == nil {
		t.onceki = map[int64]ham{}
	}
	havuzlar, err := HavuzlariOku(ctx, t.DB, 0)
	if err != nil {
		return err
	}
	uyeler, err := UyeleriOku(ctx, t.DB, 0, 0)
	if err != nil {
		return err
	}
	grup := map[int64][]Uye{}
	for _, u := range uyeler {
		grup[u.BayiID] = append(grup[u.BayiID], u)
	}
	var bayiler []int64
	for id, h := range havuzlar {
		if h.DilimAktif() || h.DiskAktif() {
			bayiler = append(bayiler, id)
		}
	}
	sort.Slice(bayiler, func(i, j int) bool { return bayiler[i] < bayiler[j] })
	disk := DiskKullanimlari(ctx, t.DB, bayiler)
	ts := simdi.UTC().Truncate(OrnekAraligi)
	for _, id := range bayiler {
		h := havuzlar[id]
		n := Nokta{T: ts.Format(time.RFC3339), CPUSinirMilli: h.CPUKota() * 10, RAMSinirMB: h.RAMMB, DiskSinirMB: h.DiskMB, Kaynak: "yok"}
		cur, kaynak, ok := bayiOrnekle(id, grup[id])
		cur.zaman = simdi
		if ok {
			n.Kaynak = kaynak
			mb := int(cur.ramBayt / (1 << 20))
			n.RAMMB = &mb
			if prev, var_ := t.onceki[id]; var_ {
				n.CPUMilli, n.KisitlamaMS, n.OOM, n.IOOkuKBs, n.IOYazKBs = hizlar(prev, cur)
			}
			t.onceki[id] = cur
		} else {
			delete(t.onceki, id)
		}
		if d, var_ := disk[id]; var_ {
			mb := d.KullanilanKB / 1024
			n.DiskMB = &mb
		}
		// Ölçülecek hiçbir şey yoksa satır yazılmaz: cgroup verisi yok ve disk havuzu da tanımlı
		// değilse (yalnız CPU/RAM havuzu, henüz çalışan hosting yok) tablo boşuna şişmez.
		if n.Kaynak == "yok" && (n.DiskMB == nil || !h.DiskAktif()) {
			continue
		}
		if err := noktaYaz(ctx, t.DB, id, ts, n); err != nil {
			log.Printf("bayikaynak: bayi %d kullanım örneği yazılamadı: %v", id, err)
		}
	}
	for id := range t.onceki { // havuzu kalkan bayinin bellek durumunu bırak
		if _, var_ := havuzlar[id]; !var_ {
			delete(t.onceki, id)
		}
	}
	if simdi.Sub(t.budama) > 6*time.Hour {
		t.budama = simdi
		if n, err := Buda(ctx, t.DB, simdi); err != nil {
			log.Printf("bayikaynak: kullanım budama: %v", err)
		} else if n > 0 {
			log.Printf("bayikaynak: %d eski kullanım satırı silindi", n)
		}
	}
	return nil
}

func noktaYaz(ctx context.Context, db *sql.DB, bayi int64, ts time.Time, n Nokta) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO bayi_kaynak_kullanim
		  (bayi_id, ts, cpu_milli, cpu_sinir_milli, kisitlama_ms, ram_mb, ram_sinir_mb, oom_olay,
		   disk_mb, disk_sinir_mb, io_oku_kbs, io_yaz_kbs, kaynak)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE cpu_milli=VALUES(cpu_milli), cpu_sinir_milli=VALUES(cpu_sinir_milli),
		   kisitlama_ms=VALUES(kisitlama_ms), ram_mb=VALUES(ram_mb), ram_sinir_mb=VALUES(ram_sinir_mb),
		   oom_olay=VALUES(oom_olay), disk_mb=VALUES(disk_mb), disk_sinir_mb=VALUES(disk_sinir_mb),
		   io_oku_kbs=VALUES(io_oku_kbs), io_yaz_kbs=VALUES(io_yaz_kbs), kaynak=VALUES(kaynak)`,
		bayi, ts.Format("2006-01-02 15:04:05"), n.CPUMilli, n.CPUSinirMilli, n.KisitlamaMS, n.RAMMB, n.RAMSinirMB, n.OOM,
		n.DiskMB, n.DiskSinirMB, n.IOOkuKBs, n.IOYazKBs, n.Kaynak)
	return err
}

// Buda: Saklama'dan eski satırları (UTC) siler; kilit süresi kısa kalsın diye parça parça.
func Buda(ctx context.Context, db *sql.DB, simdi time.Time) (int64, error) {
	esik := simdi.UTC().Add(-Saklama).Format("2006-01-02 15:04:05")
	var top int64
	for i := 0; i < 200; i++ {
		res, err := db.ExecContext(ctx, `DELETE FROM bayi_kaynak_kullanim WHERE ts < ? LIMIT 5000`, esik)
		if err != nil {
			return top, err
		}
		n, _ := res.RowsAffected()
		top += n
		if n < 5000 {
			break
		}
	}
	return top, nil
}

// Aralıklar: GET ...?aralik= değerleri → (süre, kova).
var aralikTablosu = map[string][2]time.Duration{
	"24s": {24 * time.Hour, OrnekAraligi},
	"7g":  {7 * 24 * time.Hour, 30 * time.Minute},
	"30g": {30 * 24 * time.Hour, 2 * time.Hour},
}

// AralikGecerli: bilinen aralık adı mı?
func AralikGecerli(a string) bool { _, ok := aralikTablosu[a]; return ok }

// Seri: bayinin kullanım serisi (eskiden yeniye). Uzun aralıklar kovalara indirgenir:
// CPU/G-Ç ortalama, RAM/disk en yüksek, kısma/OOM toplam. Hata → boş değil hata döner.
func Seri(ctx context.Context, db *sql.DB, bayiID int64, aralik string, simdi time.Time) ([]Nokta, error) {
	ar, ok := aralikTablosu[aralik]
	if !ok {
		return nil, fmt.Errorf("geçersiz aralık %q", aralik)
	}
	alt := simdi.UTC().Add(-ar[0]).Format("2006-01-02 15:04:05")
	rows, err := db.QueryContext(ctx,
		`SELECT DATE_FORMAT(ts,'%Y-%m-%d %H:%i:%s'), cpu_milli, cpu_sinir_milli, kisitlama_ms, ram_mb, ram_sinir_mb,
		        oom_olay, disk_mb, disk_sinir_mb, io_oku_kbs, io_yaz_kbs, kaynak
		   FROM bayi_kaynak_kullanim WHERE bayi_id=? AND ts >= ? ORDER BY ts`, bayiID, alt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type kova struct {
		n              Nokta
		cpuT, ioR, ioW int
		cpuN, ioN      int
		kisit, oom     int
		kisitN, oomN   int
		ramMax         int
		ramN           int
		diskMax        int64
		diskN          int
	}
	kovalar := map[int64]*kova{}
	var anahtarlar []int64
	for rows.Next() {
		var tsS string
		var n Nokta
		var cpu, kis, ram, oom, io1, io2 sql.NullInt64
		var disk sql.NullInt64
		if err := rows.Scan(&tsS, &cpu, &n.CPUSinirMilli, &kis, &ram, &n.RAMSinirMB, &oom, &disk, &n.DiskSinirMB, &io1, &io2, &n.Kaynak); err != nil {
			return nil, err
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", tsS, time.UTC)
		if err != nil {
			continue
		}
		bk := t.Unix() / int64(ar[1].Seconds()) * int64(ar[1].Seconds())
		k := kovalar[bk]
		if k == nil {
			k = &kova{n: Nokta{T: time.Unix(bk, 0).UTC().Format(time.RFC3339), CPUSinirMilli: n.CPUSinirMilli, RAMSinirMB: n.RAMSinirMB, DiskSinirMB: n.DiskSinirMB, Kaynak: n.Kaynak}}
			kovalar[bk] = k
			anahtarlar = append(anahtarlar, bk)
		}
		k.n.CPUSinirMilli, k.n.RAMSinirMB, k.n.DiskSinirMB, k.n.Kaynak = n.CPUSinirMilli, n.RAMSinirMB, n.DiskSinirMB, n.Kaynak
		if cpu.Valid {
			k.cpuT += int(cpu.Int64)
			k.cpuN++
		}
		if kis.Valid {
			k.kisit += int(kis.Int64)
			k.kisitN++
		}
		if oom.Valid {
			k.oom += int(oom.Int64)
			k.oomN++
		}
		if ram.Valid {
			k.ramN++
			if int(ram.Int64) > k.ramMax {
				k.ramMax = int(ram.Int64)
			}
		}
		if disk.Valid {
			k.diskN++
			if disk.Int64 > k.diskMax {
				k.diskMax = disk.Int64
			}
		}
		if io1.Valid && io2.Valid {
			k.ioR += int(io1.Int64)
			k.ioW += int(io2.Int64)
			k.ioN++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(anahtarlar, func(i, j int) bool { return anahtarlar[i] < anahtarlar[j] })
	out := make([]Nokta, 0, len(anahtarlar))
	for _, a := range anahtarlar {
		k := kovalar[a]
		n := k.n
		if k.cpuN > 0 {
			n.CPUMilli = intNil(k.cpuT / k.cpuN)
		}
		if k.kisitN > 0 {
			n.KisitlamaMS = intNil(k.kisit)
		}
		if k.oomN > 0 {
			n.OOM = intNil(k.oom)
		}
		if k.ramN > 0 {
			n.RAMMB = intNil(k.ramMax)
		}
		if k.diskN > 0 {
			d := k.diskMax
			n.DiskMB = &d
		}
		if k.ioN > 0 {
			n.IOOkuKBs, n.IOYazKBs = intNil(k.ioR/k.ioN), intNil(k.ioW/k.ioN)
		}
		out = append(out, n)
	}
	return out, nil
}
