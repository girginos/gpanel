// Package bayikaynak: bayi (reseller) başına VPS benzeri kaynak havuzu.
//
//	CPU çekirdek   → systemd dilimi CPUQuota   (havuz diliminde, bayidilim paketine bkz.)
//	Fiziksel RAM   → systemd dilimi MemoryMax  (+MemoryHigh %90, MemorySwapMax=0)
//	Disk           → XFS PROJE kotası, sert sınır = havuz (bayinin hosting ev dizinleri bir projede)
//	G/Ç ağırlığı   → IOWeight
//
// Değer 0 = SINIRSIZ: değer verilene kadar mevcut bayiler ETKİLENMEZ. Aşırı satış serbesttir
// (bayinin hostinglerine dağıttığı plan kotalarının toplamı havuzu aşabilir); GERÇEK kullanım
// havuzu aşamaz — havuz tavanı hosting dilimlerini SARAR, hosting limitleri içeride aynen çalışır.
//
// Sağlamlık ilkeleri: (1) her şey veritabanından türetilir, açılışta ve her olayda idempotent
// uzlaştırılır; (2) hata bir bayiyi etkiler, paneli düşürmez; (3) çalışan siteler ZORLA yeniden
// başlatılmaz — dilim taşıması birim yeniden başlayınca (ya da sunucu yeniden başlayınca) etkin
// olur, panel "bekleyen" durumunu raporlar; (4) birim/proje adları yalnız doğrulanmış sayısal
// kimlik ve sk allowlist'inden üretilir (enjeksiyon yok).
package bayikaynak

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"girginospanel/internal/bayidilim"
)

// Sınırlar (API doğrulaması + dilim üretimi).
const (
	EnAzCPUCekirdek  = 0.1
	EnCokCPUCekirdek = 1024.0
	EnAzRAMMB        = 64
	EnCokRAMMB       = int64(1) << 26 // 64 TiB
	EnCokIOAgirlik   = 10000
	EnCokDiskMB      = int64(1) << 40 // reseller.Guncelle ile aynı üst sınır
)

// Havuz: bir bayinin kaynak havuzu. 0 = sınırsız.
type Havuz struct {
	BayiID      int64   `json:"bayi_id"`
	CPUCekirdek float64 `json:"cpu_cekirdek"`
	RAMMB       int64   `json:"ram_mb"`
	DiskMB      int64   `json:"disk_mb"`
	IOAgirlik   int     `json:"io_agirlik"`
}

// DilimAktif: CPU/RAM/G-Ç havuz dilimi kurulmalı mı? (en az biri verilmiş)
func (h Havuz) DilimAktif() bool {
	return h.CPUCekirdek > 0 || h.RAMMB > 0 || h.IOAgirlik > 0
}

// DiskAktif: disk havuzu (XFS proje kotası) uygulanmalı mı?
func (h Havuz) DiskAktif() bool { return h.DiskMB > 0 }

// CPUKota: systemd CPUQuota yüzdesi (1 çekirdek = 100). 0 = sınırsız.
func (h Havuz) CPUKota() int {
	if h.CPUCekirdek <= 0 {
		return 0
	}
	y := int(math.Round(h.CPUCekirdek * 100))
	if y < 1 {
		y = 1
	}
	return y
}

// ErrDeger: doğrulama hatası (API 400'e çevirir).
var ErrDeger = errors.New("geçersiz kaynak değeri")

// DegerDogrula: API girdisi sınırlarını denetler. 0 her alanda "sınırsız" ve geçerlidir.
func DegerDogrula(cpu float64, ramMB, diskMB int64, io int) error {
	if math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0 {
		return fmt.Errorf("%w: CPU çekirdek sayısı geçersiz", ErrDeger)
	}
	if cpu != 0 && (cpu < EnAzCPUCekirdek || cpu > EnCokCPUCekirdek) {
		return fmt.Errorf("%w: CPU çekirdek 0 (sınırsız) ya da %.1f-%.0f arasında olmalı", ErrDeger, EnAzCPUCekirdek, EnCokCPUCekirdek)
	}
	if ramMB < 0 || (ramMB != 0 && (ramMB < EnAzRAMMB || ramMB > EnCokRAMMB)) {
		return fmt.Errorf("%w: RAM 0 (sınırsız) ya da en az %d MB olmalı", ErrDeger, EnAzRAMMB)
	}
	if diskMB < 0 || diskMB > EnCokDiskMB {
		return fmt.Errorf("%w: disk havuzu geçersiz", ErrDeger)
	}
	if io < 0 || io > EnCokIOAgirlik {
		return fmt.Errorf("%w: G/Ç ağırlığı 0 (varsayılan) ya da 1-%d olmalı", ErrDeger, EnCokIOAgirlik)
	}
	return nil
}

// Uye: bir bayinin hosting üyesi.
type Uye struct {
	DomainID int64  `json:"domain_id"`
	BayiID   int64  `json:"bayi_id"`
	SK       string `json:"sistem_kullanici"`
	Alan     string `json:"alan_adi,omitempty"`
}

// havuzSorgu: kaynak_* kolonları göç 0219 ile gelir. Kolon yoksa (göç uygulanmamış) sorgu
// hata verir ve uzlaştırma HİÇBİR ŞEY değiştirmez (güvenli taraf).
const havuzSorgu = `SELECT id, COALESCE(kaynak_cpu_cekirdek,0), COALESCE(kaynak_ram_mb,0),
       COALESCE(max_disk_mb,0), COALESCE(kaynak_io_agirlik,0)
  FROM users WHERE role='reseller'`

// HavuzlariOku: bayiID>0 ise yalnız o bayi, değilse tümü.
func HavuzlariOku(ctx context.Context, db *sql.DB, bayiID int64) (map[int64]Havuz, error) {
	q, args := havuzSorgu, []any{}
	if bayiID > 0 {
		q += ` AND id=?`
		args = append(args, bayiID)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("bayi havuzları okunamadı: %w", err)
	}
	defer rows.Close()
	out := map[int64]Havuz{}
	for rows.Next() {
		var h Havuz
		if err := rows.Scan(&h.BayiID, &h.CPUCekirdek, &h.RAMMB, &h.DiskMB, &h.IOAgirlik); err != nil {
			return nil, err
		}
		if !bayidilim.GecerliBayiID(h.BayiID) {
			continue
		}
		// Bozuk/negatif değer sınırsız sayılır (asla sıfırdan küçük tavan üretme).
		if h.CPUCekirdek < 0 || math.IsNaN(h.CPUCekirdek) {
			h.CPUCekirdek = 0
		}
		if h.RAMMB < 0 {
			h.RAMMB = 0
		}
		if h.DiskMB < 0 {
			h.DiskMB = 0
		}
		if h.IOAgirlik < 0 || h.IOAgirlik > EnCokIOAgirlik {
			h.IOAgirlik = 0
		}
		out[h.BayiID] = h
	}
	return out, rows.Err()
}

// UyeleriOku: bayiID>0 ise yalnız o bayinin, domainID>0 ise yalnız o hostingin üyeleri.
// Yalnız geçerli sk'ler ("c_..."); admin/kök hostingleri (reseller_id 0) dönmez.
func UyeleriOku(ctx context.Context, db *sql.DB, bayiID, domainID int64) ([]Uye, error) {
	q := `SELECT id, reseller_id, sistem_kullanici, COALESCE(alan_adi,'') FROM domains
	       WHERE reseller_id>0 AND sistem_kullanici LIKE 'c\_%'`
	args := []any{}
	if bayiID > 0 {
		q += ` AND reseller_id=?`
		args = append(args, bayiID)
	}
	if domainID > 0 {
		q += ` AND id=?`
		args = append(args, domainID)
	}
	q += ` ORDER BY id`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("bayi hostingleri okunamadı: %w", err)
	}
	defer rows.Close()
	var out []Uye
	for rows.Next() {
		var u Uye
		if err := rows.Scan(&u.DomainID, &u.BayiID, &u.SK, &u.Alan); err != nil {
			return nil, err
		}
		if !bayidilim.GecerliSK(u.SK) || !bayidilim.GecerliBayiID(u.BayiID) {
			continue
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ── Birim üretimi (saf işlevler; dosya içeriği testlerde doğrudan sınanır) ─────────────

// DilimImza: panelin yazdığı havuz dilimlerinin ilk satırı (yabancı dosyaya dokunmamak için).
const DilimImza = "# GirginOSPanel bayi kaynak havuzu — bayi "

// DilimIcerik: havuz dilimi birim dosyası. Alan 0 → ilgili direktif YAZILMAZ (sınırsız).
// Muhasebe (Accounting) her zaman açık: kullanım grafiği için cpu.stat/memory.current gerekir.
func DilimIcerik(h Havuz) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%d\n", DilimImza, h.BayiID)
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=GirginOS panel bayi kaynak havuzu %d\n", h.BayiID)
	b.WriteString("Before=slices.target\n\n[Slice]\n")
	b.WriteString("CPUAccounting=yes\nMemoryAccounting=yes\nTasksAccounting=yes\nIOAccounting=yes\n")
	if k := h.CPUKota(); k > 0 {
		fmt.Fprintf(&b, "CPUQuota=%d%%\n", k)
	}
	if h.RAMMB > 0 {
		fmt.Fprintf(&b, "MemoryMax=%dM\nMemoryHigh=%dM\n", h.RAMMB, memHigh(h.RAMMB))
		// "Fiziksel RAM": havuz swap'a taşarak tavanı aşamasın (VPS gibi).
		b.WriteString("MemorySwapMax=0\n")
	}
	if h.IOAgirlik > 0 {
		fmt.Fprintf(&b, "IOWeight=%d\n", h.IOAgirlik)
	}
	return b.String()
}

// memHigh: MemoryHigh = %90 (yumuşak kısma); en az 1 MB.
func memHigh(ramMB int64) int64 {
	v := ramMB * 90 / 100
	if v < 1 {
		v = 1
	}
	return v
}

// CanliOzellikArgs: etkin (aktif) dilime `systemctl set-property --runtime` ile uygulanacak
// özellikler. Alan 0 → "sıfırla" (boş atama) — >0→0 geçişi canlı kaldırılsın.
func CanliOzellikArgs(h Havuz) []string {
	a := []string{}
	if k := h.CPUKota(); k > 0 {
		a = append(a, fmt.Sprintf("CPUQuota=%d%%", k))
	} else {
		a = append(a, "CPUQuota=")
	}
	if h.RAMMB > 0 {
		a = append(a, fmt.Sprintf("MemoryMax=%dM", h.RAMMB), fmt.Sprintf("MemoryHigh=%dM", memHigh(h.RAMMB)), "MemorySwapMax=0")
	} else {
		a = append(a, "MemoryMax=infinity", "MemoryHigh=infinity", "MemorySwapMax=infinity")
	}
	if h.IOAgirlik > 0 {
		a = append(a, fmt.Sprintf("IOWeight=%d", h.IOAgirlik))
	} else {
		a = append(a, "IOWeight=")
	}
	return a
}
