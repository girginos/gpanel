package ipyonetim

// AdminOnly HTTP endpoint'leri:
//   GET  /admin/ipler                    — sunucudaki tüm IP'ler + panel kayıtları
//   POST /admin/ipler                    — yeni IP ekle
//   DELETE /admin/ipler/{ip}             — panel-etiketli IP kaldır
//   (PUT /admin/domains/{id}/ipv4 → internal/domainip)

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	DB    *sql.DB
	Aktor func(r *http.Request) (int64, bool)
}

type listeYanit struct {
	SunucuIPler      []SunucuIP `json:"sunucu_ipler"` // Sunucudaki TÜM IP'ler (kullanıcı+panel)
	DBKayitlar       []dbKayit  `json:"db_kayitlar"`  // Panel'in DB'de tuttuğu kayıtlar
	VarsayilanArayuz string     `json:"varsayilan_arayuz"`
}

type dbKayit struct {
	ID        int64  `json:"id"`
	IP        string `json:"ip"`
	Iface     string `json:"iface"`
	CIDR      int    `json:"cidr"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) Liste(w http.ResponseWriter, _ *http.Request) {
	y := listeYanit{
		SunucuIPler:      SunucuIPler(),
		VarsayilanArayuz: varsayilanArayuz(),
	}
	rows, err := h.DB.Query(`SELECT id, ip, iface, cidr, note, created_at FROM cp_server_ips ORDER BY created_at`)
	if err == nil {
		defer rows.Close()
		y.DBKayitlar = []dbKayit{}
		for rows.Next() {
			var k dbKayit
			if err := rows.Scan(&k.ID, &k.IP, &k.Iface, &k.CIDR, &k.Note, &k.CreatedAt); err == nil {
				y.DBKayitlar = append(y.DBKayitlar, k)
			}
		}
	} else {
		y.DBKayitlar = []dbKayit{}
	}
	jsonYaz(w, 200, y)
}

type ekleGovde struct {
	IP    string `json:"ip"`
	Iface string `json:"iface"`
	CIDR  int    `json:"cidr"`
	Note  string `json:"note"`
}

func (h *Handler) Ekle(w http.ResponseWriter, r *http.Request) {
	var g ekleGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, 400, "geçersiz gövde: "+err.Error())
		return
	}
	var uid *int64
	if h.Aktor != nil {
		if u, ok := h.Aktor(r); ok {
			uid = &u
		}
	}
	ctx, iptal := context.WithTimeout(r.Context(), 20*time.Second)
	defer iptal()
	if err := Ekle(ctx, h.DB, g.IP, g.Iface, g.CIDR, g.Note, uid); err != nil {
		hataYaz(w, 400, err.Error())
		return
	}
	jsonYaz(w, 201, map[string]any{"ok": true, "ip": g.IP})
}

func (h *Handler) Sil(w http.ResponseWriter, r *http.Request) {
	// URL: /admin/ipler/{ip}
	ip := pathSonParca(r.URL.Path)
	if ip == "" {
		hataYaz(w, 400, "IP gerekli")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 15*time.Second)
	defer iptal()
	if err := Sil(ctx, h.DB, ip); err != nil {
		hataYaz(w, 400, err.Error())
		return
	}
	jsonYaz(w, 200, map[string]any{"ok": true})
}

/* -------- Domain IP değiştirme --------
 * PUT /admin/domains/{id}/ipv4 artık internal/domainip (DNS A/SPF + zone +
 * ftp_host/db_host; sunucuda olmayan IP 400). Eski DB-only gövde kaldırıldı.
 */

func pathSonParca(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func jsonYaz(w http.ResponseWriter, k int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(k)
	_ = json.NewEncoder(w).Encode(v)
}
func hataYaz(w http.ResponseWriter, k int, m string) {
	jsonYaz(w, k, map[string]string{"error": m})
}

// _ importlar tut
var _ = net.ParseIP
