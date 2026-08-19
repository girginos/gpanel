package zincir

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

type Handlers struct{ DB *sql.DB }

type OlayDTO struct {
	Kaynak  string `json:"kaynak"`
	Asama   string `json:"asama"`
	AsamaAd string `json:"asama_ad"`
	Seviye  string `json:"seviye"`
	Ozet    string `json:"ozet"`
	Tarih   string `json:"tarih"`
}

type ZincirDTO struct {
	ID       int64     `json:"id"`
	DomainID *int64    `json:"domain_id"`
	AlanAdi  string    `json:"alan_adi"`
	Asamalar []string  `json:"asamalar"`
	AsamaAd  []string  `json:"asama_ad"`
	Guven    int       `json:"guven"`
	Seviye   string    `json:"seviye"`
	Tarih    string    `json:"tarih"`
	Olaylar  []OlayDTO `json:"olaylar"`
}

// kapsam — bildirim ile AYNI rol-kapsam (izolasyon): admin=panel-geneli,
// reseller=kendi, müşteri=domaini.
func kapsam(r *http.Request) (string, []any, bool) {
	if c := middleware.ClaimsFrom(r); c != nil {
		switch c.Role {
		case "admin":
			return "reseller_id IS NULL", nil, true
		case "reseller":
			return "reseller_id = ?", []any{c.ResellerID}, true
		}
	}
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		return "domain_id = ?", []any{mc.DomainID}, true
	}
	return "", nil, false
}

// Liste — GET /antivirus/zincirler — son saldırı zincirleri (rol-kapsamlı), her
// zincirin olay zaman-çizelgesiyle. Canlı saldırı ekranının veri kaynağı.
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	kos, arg, ok := kapsam(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT z.id, z.domain_id, COALESCE(d.alan_adi,''), z.asamalar, z.guven,
		        DATE_FORMAT(z.created_at,'%Y-%m-%d %H:%i:%s')
		 FROM av_zincir z LEFT JOIN domains d ON d.id=z.domain_id
		 WHERE z.`+kos+` ORDER BY z.created_at DESC LIMIT 50`, arg...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []ZincirDTO{}
	for rows.Next() {
		var z ZincirDTO
		var dom sql.NullInt64
		var asamaStr string
		if rows.Scan(&z.ID, &dom, &z.AlanAdi, &asamaStr, &z.Guven, &z.Tarih) != nil {
			continue
		}
		if dom.Valid {
			z.DomainID = &dom.Int64
		}
		z.Asamalar = strings.Split(asamaStr, ">")
		for _, a := range z.Asamalar {
			z.AsamaAd = append(z.AsamaAd, asamaAd[a])
		}
		z.Seviye = "uyari"
		if z.Guven >= 80 {
			z.Seviye = "kritik"
		}
		if dom.Valid {
			z.Olaylar = zincirOlaylari(r, h.DB, dom.Int64, z.Tarih)
		}
		out = append(out, z)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"zincirler": out})
}

// zincirOlaylari — bir zincirin domaini için olay zaman-çizelgesi (zincir
// zamanından geriye pencere kadar). N+1 ama LIMIT 50 zincir × ≤ birkaç olay.
func zincirOlaylari(r *http.Request, db *sql.DB, domID int64, tarih string) []OlayDTO {
	rows, err := db.QueryContext(r.Context(),
		`SELECT kaynak, asama, seviye, ozet, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		 FROM av_olay
		 WHERE domain_id=? AND created_at <= ? AND created_at >= (? - INTERVAL ? MINUTE)
		 ORDER BY created_at LIMIT 50`, domID, tarih, tarih, pencereDk)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []OlayDTO
	for rows.Next() {
		var o OlayDTO
		if rows.Scan(&o.Kaynak, &o.Asama, &o.Seviye, &o.Ozet, &o.Tarih) == nil {
			o.AsamaAd = asamaAd[o.Asama]
			out = append(out, o)
		}
	}
	return out
}

var _ = strconv.Itoa
