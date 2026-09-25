package kaynak

// canli.go — GET /domains/{id}/canli: kiracının systemd slice'ından canlı kaynak
// kullanımı (CPU %, bellek, disk G/Ç hızı, süreç sayısı). Yalnız cgroup dosyası
// okur (internal/cgroup, salt-okur); ucuzdur. Ön yüz sekme görünürken ~10 sn'de bir
// yoklar. Yetki: rota MusteriScope arkasında (müşteri kendi domaini, bayi kendi
// domainleri, yönetici hepsi).

import (
	"database/sql"
	"errors"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"girginospanel/internal/cgroup"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// Canli: /canli yanıtı. Destek=false → kiracı cgroup'u yok; diğer alanlar 0/null.
type Canli struct {
	Destek        bool     `json:"destek"`
	CPUYuzde      *float64 `json:"cpu_yuzde"`       // tek çekirdek = 100; ilk örnekte null
	CPULimitYuzde float64  `json:"cpu_limit_yuzde"` // cpu.max kota/periyot*100; sınırsız → 0
	BellekB       int64    `json:"bellek_b"`        // memory.current
	BellekLimitB  int64    `json:"bellek_limit_b"`  // memory.max; sınırsız → 0
	IOOkumaBps    *float64 `json:"io_okuma_bps"`    // io.stat delta; ilk örnekte null
	IOYazmaBps    *float64 `json:"io_yazma_bps"`
	Surec         int64    `json:"surec"` // pids.current
	Zaman         int64    `json:"zaman"` // unix saniye
}

// canliOrnek: kiracı slice'ı başına son sayaç örneği (CPU/G/Ç delta'sı için).
// 2 dk'dan eski örneğin ortalaması "canlı" değildir: yeni taban alınır, hız null.
var canliOrnek = &cgroup.Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}

func (h *Handlers) Canli(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	err := h.DB.QueryRowContext(r.Context(), `SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		log.Printf("kaynak.Canli: domain okunamadı (id=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, canliOku(canliOrnek, cgroup.KiraciDizin(sk), time.Now()))
}

// canliOku: dizin = kiracı slice cgroup dizini ("" → sk biçim dışı → destek yok).
func canliOku(o *cgroup.Ornekleyici, dizin string, simdi time.Time) Canli {
	c := Canli{Zaman: simdi.Unix()}
	if dizin == "" {
		return c
	}
	// Çekirdek iki dosya okunamıyorsa (slice yok, cgroup v1, denetleyici kapalı)
	// ölçüm yok. Kısmi değerler "0 kullanım" gibi görünürdü.
	bellek, bellekVar := cgroup.Deger(filepath.Join(dizin, "memory.current"))
	s := cgroup.Sayac{Zaman: simdi}
	s.CPUUsec, s.CPUVar = cgroup.CPUUsec(dizin)
	if !bellekVar || !s.CPUVar {
		return c
	}
	s.IOOkuma, s.IOYazma, s.IOVar = cgroup.IOToplam(dizin)

	c.Destek = true
	c.BellekB = bellek
	if v, ok := cgroup.Deger(filepath.Join(dizin, "memory.max")); ok && v > 0 {
		c.BellekLimitB = v
	}
	if kota, periyot, ok := cgroup.CPUMax(dizin); ok && kota > 0 {
		c.CPULimitYuzde = yuvarla(float64(kota)*100/float64(periyot), 2)
	}
	if v, ok := cgroup.Deger(filepath.Join(dizin, "pids.current")); ok && v > 0 {
		c.Surec = v
	}
	hiz := o.Hesapla(dizin, s)
	c.CPUYuzde = yuvarlaP(hiz.CPUYuzde, 2)
	c.IOOkumaBps = yuvarlaP(hiz.IOOkumaBps, 0)
	c.IOYazmaBps = yuvarlaP(hiz.IOYazmaBps, 0)
	return c
}

func yuvarla(v float64, basamak int) float64 {
	k := math.Pow(10, float64(basamak))
	return math.Round(v*k) / k
}

func yuvarlaP(v *float64, basamak int) *float64 {
	if v == nil {
		return nil
	}
	y := yuvarla(*v, basamak)
	return &y
}
