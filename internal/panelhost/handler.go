package panelhost

// AdminOnly rotalar — panel hostname/SSL yönetimi.
//
// GET  /admin/panel-host          — durum
// POST /admin/panel-host/dns      — {hostname} dry-run DNS kontrol
// POST /admin/panel-host/apply    — {hostname} async hostname ayarla, {is_id}
// POST /admin/panel-host/ssl      — {hostname} async LE cert kur, {is_id}
// GET  /admin/panel-host/is?id=X  — iş durumu (poll)

import (
	"strconv"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Handler struct{}

func (h *Handler) Durum(w http.ResponseWriter, _ *http.Request) {
	d := DurumOku()
	jsonYaz(w, 200, map[string]any{
		"durum":       d,
		"betik_var":   BetikVarMi(),
		"acme_var":    AcmeVarMi(),
	})
}

type hostnameGovde struct {
	Hostname string `json:"hostname"`
}

func normHostname(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.Trim(s, "/.")
	return s
}

func gecerliHostname(s string) bool {
	if s == "" || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, etiket := range strings.Split(s, ".") {
		if len(etiket) == 0 || len(etiket) > 63 {
			return false
		}
		if etiket[0] == '-' || etiket[len(etiket)-1] == '-' {
			return false
		}
		for i := 0; i < len(etiket); i++ {
			c := etiket[i]
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func gecerliGovdeCoz(w http.ResponseWriter, r *http.Request) (string, bool) {
	var g hostnameGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, 400, "geçersiz gövde: "+err.Error())
		return "", false
	}
	h := normHostname(g.Hostname)
	if !gecerliHostname(h) {
		hataYaz(w, 400, "geçersiz hostname (örn. panel.musteri.com)")
		return "", false
	}
	return h, true
}

func (h *Handler) DNS(w http.ResponseWriter, r *http.Request) {
	hostname, ok := gecerliGovdeCoz(w, r)
	if !ok {
		return
	}
	d := DurumOku()
	cozulen, eslesme := DNSCoz(hostname, d.SunucuIP4, d.SunucuIP6)
	jsonYaz(w, 200, map[string]any{
		"hostname":   hostname,
		"cozulen":    cozulen,
		"eslesme":    eslesme,
		"sunucu_ip4": d.SunucuIP4,
	})
}

func (h *Handler) Ayarla(w http.ResponseWriter, r *http.Request) {
	hostname, ok := gecerliGovdeCoz(w, r)
	if !ok {
		return
	}
	is := AyarlaAsync(hostname)
	if is == nil {
		hataYaz(w, 409, "başka bir hostname değişimi çalışıyor")
		return
	}
	jsonYaz(w, 202, map[string]any{"is_id": is.ID})
}

func (h *Handler) SslKur(w http.ResponseWriter, r *http.Request) {
	hostname, ok := gecerliGovdeCoz(w, r)
	if !ok {
		return
	}
	// Rate limit önce — kullanıcı LE ban yemesin.
	if !RateLimitIzinli(hostname) {
		_, bekleyen := RateLimitBilgi(hostname)
		hataYaz(w, 429, "peş peşe fail sınırı. "+bekleyen.Round(time.Minute).String()+" sonra tekrar deneyin (LE haftalık ban riski)")
		return
	}
	is := SslKurAsync(hostname)
	if is == nil {
		// SslKurAsync ya kilit ya rate limit — hangisi olduğunu ayır
		if !RateLimitIzinli(hostname) {
			_, bekleyen := RateLimitBilgi(hostname)
			hataYaz(w, 429, "rate limit. "+bekleyen.Round(time.Minute).String()+" sonra")
		} else {
			hataYaz(w, 409, "başka bir SSL kurma çalışıyor")
		}
		return
	}
	jsonYaz(w, 202, map[string]any{"is_id": is.ID})
}

func (h *Handler) IsDurum(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		hataYaz(w, 400, "id gerekli")
		return
	}
	is := IsGetir(id)
	if is == nil {
		hataYaz(w, 404, "iş bulunamadı (bellek-içi; panel restart oldu olabilir)")
		return
	}
	jsonYaz(w, 200, is)
}

func jsonYaz(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func hataYaz(w http.ResponseWriter, kod int, mesaj string) {
	jsonYaz(w, kod, map[string]string{"error": mesaj})
}

// Gecmis — GET /admin/panel-host/gecmis?limit=20
// panel Ayarla/SslKur iş geçmişi (audit).
func (h *Handler) Gecmis(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := IsGecmis(limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

