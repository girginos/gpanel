// Package performans: domain hızlandırıcı/performans özeti (salt-okunur).
// Mevcut php_settings + nginx_settings verilerini derler, öneriler üretir.
package performans

import (
	"database/sql"
	"net/http"
	"strconv"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

type Oge struct {
	Ad       string `json:"ad"`
	Aktif    bool   `json:"aktif"`
	Deger    string `json:"deger"`
	Ayar     string `json:"ayar"` // hangi ayar sayfası (slug)
	Aciklama string `json:"aciklama"`
}
type Oneri struct {
	Metin string `json:"metin"`
	Onem  string `json:"onem"` // "yuksek" | "orta" | "bilgi"
	Ayar  string `json:"ayar"`
}
type Ozet struct {
	AlanAdi  string  `json:"alan_adi"`
	PHPSurum string  `json:"php_surum"`
	Skor     int     `json:"skor"` // 0-100 kaba performans skoru
	Ogeler   []Oge   `json:"ogeler"`
	Oneriler []Oneri `json:"oneriler"`
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, phpSurum string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, php_surum FROM domains WHERE id=?`, id).Scan(&alanAdi, &phpSurum); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	o := Ozet{AlanAdi: alanAdi, PHPSurum: phpSurum}

	// php_settings (yoksa varsayılan)
	var opcache, fileUploads int
	var memLimit, pmStrateji string
	var pmMaxChildren, maxExec int
	opcache, memLimit, pmStrateji, pmMaxChildren, maxExec = 1, "256M", "ondemand", 8, 30
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT opcache_enable, memory_limit, pm_strategy, pm_max_children, max_execution_time
		   FROM php_settings WHERE domain_id=?`, id).
		Scan(&opcache, &memLimit, &pmStrateji, &pmMaxChildren, &maxExec)
	_ = fileUploads

	// nginx_settings (yoksa varsayılan)
	var fastcgi, browserCache, bcGun int
	browserCache, bcGun = 1, 30
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT fastcgi_cache, browser_cache, browser_cache_gun FROM nginx_settings WHERE domain_id=?`, id).
		Scan(&fastcgi, &browserCache, &bcGun)

	b := func(i int) bool { return i == 1 }
	o.Ogeler = []Oge{
		{Ad: "OPcache", Aktif: b(opcache), Deger: durumStr(b(opcache)), Ayar: "php", Aciklama: "PHP bytecode önbelleği — CPU'yu ciddi düşürür."},
		{Ad: "FastCGI Cache", Aktif: b(fastcgi), Deger: durumStr(b(fastcgi)), Ayar: "web-sunucu", Aciklama: "nginx dinamik PHP çıktısını önbelleğe alır (yüksek trafik)."},
		{Ad: "Tarayıcı Önbelleği", Aktif: b(browserCache), Deger: iff(b(browserCache), strconv.Itoa(bcGun)+" gün", "kapalı"), Ayar: "web-sunucu", Aciklama: "Statik dosyalar için uzun süreli önbellek başlıkları."},
		{Ad: "PHP-FPM Havuzu", Aktif: true, Deger: pmStrateji + " · " + strconv.Itoa(pmMaxChildren) + " worker", Ayar: "php", Aciklama: "İşçi süreç yönetimi stratejisi."},
		{Ad: "Bellek Limiti", Aktif: true, Deger: memLimit, Ayar: "php", Aciklama: "PHP memory_limit."},
	}

	// Skor + öneriler
	skor := 40
	if b(opcache) {
		skor += 30
	} else {
		o.Oneriler = append(o.Oneriler, Oneri{Metin: "OPcache kapalı — PHP performansı için açın.", Onem: "yuksek", Ayar: "php"})
	}
	if b(fastcgi) {
		skor += 20
	} else {
		o.Oneriler = append(o.Oneriler, Oneri{Metin: "FastCGI Cache kapalı — yoğun trafikte açmayı değerlendirin.", Onem: "orta", Ayar: "web-sunucu"})
	}
	if b(browserCache) {
		skor += 10
	} else {
		o.Oneriler = append(o.Oneriler, Oneri{Metin: "Tarayıcı önbelleği kapalı — statik varlıklar için açın.", Onem: "orta", Ayar: "web-sunucu"})
	}
	if phpSurum < "8.0" {
		o.Oneriler = append(o.Oneriler, Oneri{Metin: "Eski PHP sürümü (" + phpSurum + ") — 8.3+ önerilir (hız + güvenlik).", Onem: "yuksek", Ayar: "php"})
	}
	if len(o.Oneriler) == 0 {
		o.Oneriler = append(o.Oneriler, Oneri{Metin: "Performans ayarları iyi durumda 👍", Onem: "bilgi", Ayar: ""})
	}
	o.Skor = skor
	httpx.WriteJSON(w, http.StatusOK, o)
}

func durumStr(b bool) string {
	if b {
		return "Açık"
	}
	return "Kapalı"
}
func iff(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}
