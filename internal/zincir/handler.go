package zincir

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

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
// reseller=kendi, müşteri=domaini. Döndürülen kosul HEM av_zincir HEM av_olay'a
// uygulanır (ikisinde de reseller_id + domain_id sütunu var).
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
		`SELECT z.id, z.domain_id, COALESCE(d.alan_adi,''), z.asamalar, z.guven, z.seviye,
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
		// 🔴 seviye DB'den okunur (tek gerçek kaynak) — guven>=80 ile YENİDEN
		// HESAPLANMAZ: ZincirPuanla kritik'i nedensel/3-aşama ile verir (güvenden
		// bağımsız); yeniden-hesap bildirim ile UI'yı ÇELİŞTİRİRDİ.
		if rows.Scan(&z.ID, &dom, &z.AlanAdi, &asamaStr, &z.Guven, &z.Seviye, &z.Tarih) != nil {
			continue
		}
		if dom.Valid {
			z.DomainID = &dom.Int64
		}
		z.Asamalar = strings.Split(asamaStr, ">")
		for _, a := range z.Asamalar {
			z.AsamaAd = append(z.AsamaAd, asamaAd[a])
		}
		if dom.Valid {
			z.Olaylar = zincirOlaylari(r, h.DB, dom.Int64, z.Tarih, kos, arg)
		}
		out = append(out, z)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"zincirler": out})
}

// zincirOlaylari — bir zincirin olay zaman-çizelgesi. 🔴 İZOLASYON: av_olay da
// çağıranın KAPSAMIYLA süzülür (kos/scopeArg) — zincirin snapshot domain_id'sine
// GÜVENİLMEZ (domain devri sonrası eski zincir yeni sahibin olaylarını sızdırırdı).
func zincirOlaylari(r *http.Request, db *sql.DB, domID int64, tarih, kos string, scopeArg []any) []OlayDTO {
	ctx, iptal := context.WithTimeout(r.Context(), 5*time.Second)
	defer iptal()
	args := append([]any{domID}, scopeArg...)
	args = append(args, tarih, tarih, pencereDk)
	// FAZ3b: reseller-seviye 'giris' (domain_id NULL) olaylarını da göster —
	// AMA kos yine uygulanır. müşteri kapsamı (domain_id=?) NULL≠domID olduğundan
	// giris'i otomatik dışlar (kiracının panel-login atağını müşteriye sızdırmaz);
	// admin/reseller kapsamı reseller_id üzerinden giris'i dahil eder.
	rows, err := db.QueryContext(ctx,
		`SELECT kaynak, asama, seviye, ozet, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		 FROM av_olay
		 WHERE (domain_id=? OR (domain_id IS NULL AND asama='giris')) AND `+kos+`
		   AND created_at <= ? AND created_at >= (? - INTERVAL ? MINUTE)
		 ORDER BY created_at LIMIT 50`, args...)
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
