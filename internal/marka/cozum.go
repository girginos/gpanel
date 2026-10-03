package marka

// cozum.go — hangi istek hangi markayı görür.
//
//	Giriş öncesi (GET /marka, kimliksiz):
//	  1) doğrulanmış Host → cp_marka_alanadi (YALNIZ durum='aktif') → bayi
//	  2) ?m=<anahtar> (tahmin edilemez; bayinin paylaştığı müşteri bağlantısı)
//	  3) kök
//	  Bilinmeyen/pasif anahtar ve bilinmeyen Host → kök yanıtıyla BAYT BAYT AYNI
//	  (anahtarın varlığı/yokluğu sızmaz; sayısal id/bayi adı hiç kullanılmaz).
//	Giriş sonrası (GET /marka/oturum): YALNIZ jeton belirler —
//	  admin → kök; bayi → kendi markası; müşteri (ve müşteri ADINA oturum) →
//	  domains.reseller_id → o bayinin markası.
//
// 🔴 Host yalnız GÖRÜNÜMÜ seçer, hiçbir zaman yetki vermez.

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// Handlers — marka uçları.
type Handlers struct {
	DB *sql.DB
	// SunucuIPleri — özel alan adı doğrulamasında A kaydının işaret etmesi
	// gereken adresler (boşsa dnsutil.BuSunucuya yerel arayüzlere bakar).
	SunucuIPleri []string
}

// ── host → rid (yalnız aktif özel alan adları) ─────────────────────────────

var hostOb struct {
	sync.Mutex
	m  map[string]int64
	ts time.Time
}

func hostTemizle() {
	hostOb.Lock()
	hostOb.m, hostOb.ts = nil, time.Time{}
	hostOb.Unlock()
}

// HostNormalize — "Panel.Bayi.com:2083" → "panel.bayi.com". Geçersizse "".
func HostNormalize(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(h, ".")
	if !HostnameGecerli(h) {
		return ""
	}
	return h
}

// HostnameGecerli — RFC 1123 ad (en az bir nokta, küçük harf, IP değil).
func HostnameGecerli(s string) bool {
	if s == "" || len(s) > 253 || !strings.Contains(s, ".") || net.ParseIP(s) != nil {
		return false
	}
	for _, etiket := range strings.Split(s, ".") {
		if len(etiket) == 0 || len(etiket) > 63 || etiket[0] == '-' || etiket[len(etiket)-1] == '-' {
			return false
		}
		for i := 0; i < len(etiket); i++ {
			c := etiket[i]
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return false
			}
		}
	}
	// Son etiket tamamen sayısal olamaz (IP benzeri ad).
	son := s[strings.LastIndexByte(s, '.')+1:]
	return strings.Trim(son, "0123456789") != ""
}

// hostRID — aktif özel alan adının bayisi.
func hostRID(ctx context.Context, db *sql.DB, host string) (int64, bool) {
	host = HostNormalize(host)
	if host == "" || db == nil {
		return 0, false
	}
	hostOb.Lock()
	defer hostOb.Unlock()
	if hostOb.m == nil || time.Since(hostOb.ts) > onbellekOmur {
		// Yalnız AKTİF ve kökün izni AÇIK olan adlar (izin kapatılınca ad
		// ≤60 sn içinde marka seçmez olur; satır silinmez).
		rows, err := db.QueryContext(ctx, `SELECT a.alan_adi, a.reseller_id FROM cp_marka_alanadi a
			JOIN cp_marka m ON m.reseller_id=a.reseller_id
			WHERE a.durum='aktif' AND m.alanadi_izni=1`)
		if err == nil {
			yeni := map[string]int64{}
			for rows.Next() {
				var a string
				var rid int64
				if rows.Scan(&a, &rid) == nil {
					yeni[strings.ToLower(a)] = rid
				}
			}
			rows.Close()
			hostOb.m, hostOb.ts = yeni, time.Now()
		} else if hostOb.m == nil {
			// Soğuk açılış + DB yok: Host çözümü köke düşer (eski davranış).
			return 0, false
		}
	}
	rid, ok := hostOb.m[host]
	return rid, ok
}

// HostKiraciRID — istek doğrulanmış (aktif) bir bayi alan adına mı geldi?
// Katı giriş kuralı (auth/musteri giriş uçları) bunu kullanır: yalnız
// REDDETMEK için; izin vermede Host hiç kullanılmaz.
func (h *Handlers) HostKiraciRID(r *http.Request) (int64, bool) {
	return hostRID(r.Context(), h.DB, r.Host)
}

// ── kimliksiz uçlar ─────────────────────────────────────────────────────────

// genelCoz — giriş öncesi marka (Host > ?m > kök).
func (h *Handlers) genelCoz(r *http.Request) (Marka, string) {
	ctx := r.Context()
	if rid, ok := hostRID(ctx, h.DB, r.Host); ok {
		if k, err := bayiOku(ctx, h.DB, rid); err == nil && k.Gecerli() {
			return k.Ayar, k.Anahtar
		}
		return Oku(), KokAnahtar
	}
	if m := r.URL.Query().Get("m"); m != "" {
		if rid, ok := anahtarRID(ctx, h.DB, m); ok {
			if k, err := bayiOku(ctx, h.DB, rid); err == nil && k.Gecerli() {
				return k.Ayar, k.Anahtar
			}
		}
	}
	return Oku(), KokAnahtar
}

// Genel — GET /api/v1/marka (kimliksiz).
func (h *Handlers) Genel(w http.ResponseWriter, r *http.Request) {
	m, anahtar := h.genelCoz(r)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "Host")
	genelYaz(w, m, anahtar)
}

// Oturum — GET /api/v1/marka/oturum (RequireAuth; 3 rol).
func (h *Handlers) Oturum(w http.ResponseWriter, r *http.Request) {
	m, anahtar := h.oturumCoz(r)
	w.Header().Set("Cache-Control", "private, no-store")
	genelYaz(w, m, anahtar)
}

// oturumCoz — YALNIZ jetondan. İstekteki hiçbir parametre sahibi seçmez.
func (h *Handlers) oturumCoz(r *http.Request) (Marka, string) {
	ctx := r.Context()
	if c := middleware.ClaimsFrom(r); c != nil {
		if c.Role == "reseller" && c.ResellerID > 0 {
			return BayiEtkin(ctx, h.DB, c.ResellerID)
		}
		return Oku(), KokAnahtar
	}
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		return DomainIcin(ctx, h.DB, mc.DomainID)
	}
	return Oku(), KokAnahtar
}

// Gorsel — GET /api/v1/marka/gorsel/{anahtar}/{yuva} (kimliksiz).
// anahtar: "kok" ya da ^[a-z2-7]{20}$; yuva: logo|banner|favicon. Başka her şey
// 404 (yol kaçışı: anahtar regex'i "/" ve "." içermez). Pasif bayi → 404.
func (h *Handlers) Gorsel(w http.ResponseWriter, r *http.Request) {
	y, ok := YuvaBul(chi.URLParam(r, "yuva"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	anahtar := chi.URLParam(r, "anahtar")
	if anahtar == KokAnahtar {
		m := gorselleriOlc(Oku(), Kok)
		gorselServisEt(w, r, yuvaVar(m, y.Ad), Kok, y.Veri, y.TipAd)
		return
	}
	rid, ok := anahtarRID(r.Context(), h.DB, anahtar)
	if !ok {
		http.NotFound(w, r)
		return
	}
	k, err := bayiOku(r.Context(), h.DB, rid)
	if err != nil || !k.Gecerli() || k.Anahtar != anahtar {
		http.NotFound(w, r)
		return
	}
	gorselServisEt(w, r, yuvaVar(k.Ayar, y.Ad), BayiDizin(anahtar), y.Veri, y.TipAd)
}

func yuvaVar(m Marka, ad string) bool {
	switch ad {
	case "logo":
		return m.Logo
	case "banner":
		return m.Banner
	case "favicon":
		return m.Favicon
	}
	return false
}

// yazHata — genel hata yanıtı (ham ayrıntı içermez).
func yazHata(w http.ResponseWriter, kod int, mesaj string) { httpx.WriteError(w, kod, mesaj) }
