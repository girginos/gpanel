package portyonetim

// HTTP endpoint'leri:
//   GET  /admin/portlar               — durum + aktif iş (+ kapı özeti)
//   POST /admin/portlar/backend       — backend port değiştir (async)
//   POST /admin/portlar/dis           — dış port değiştir (async; kapı modunda reddedilir)
//   GET  /admin/portlar/is            — aktif iş durumu (polling)
//   GET  /admin/portlar/gecmis        — cp_port_gecmisi (son 20)
//   GET  /admin/portlar/yasakli       — yasaklı portlar listesi (UI için)
//   POST /admin/portlar/kapi/...      — rol kapıları (bkz. kapi.go)

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

type Handler struct {
	DB    *sql.DB
	Aktor func(r *http.Request) (int64, bool)
	// Denetle: güvenlik açısından önemli işlemleri audit_log'a yazar (main bağlar;
	// nil → yazılmaz).
	Denetle func(r *http.Request, eylem, hedef, detay string, basarili bool)
}

type degistirGovde struct {
	YeniPort int `json:"yeni_port"`
}

func (h *Handler) aktorUID(r *http.Request) *int64 {
	if h.Aktor == nil {
		return nil
	}
	if u, ok := h.Aktor(r); ok {
		return &u
	}
	return nil
}

func (h *Handler) denetle(r *http.Request, eylem, hedef, detay string, basarili bool) {
	if h.Denetle != nil {
		h.Denetle(r, eylem, hedef, detay, basarili)
	}
}

// degisiklikHataYaz: eşzamanlı iş çakışması 409, diğerleri 400.
func degisiklikHataYaz(w http.ResponseWriter, err error) {
	if errors.Is(err, errDegisiklikSuruyor) || errors.Is(err, errIPIsiSuruyor) || errors.Is(err, errKapiModu) {
		hataYaz(w, http.StatusConflict, err.Error())
		return
	}
	hataYaz(w, http.StatusBadRequest, err.Error())
}

func (h *Handler) Durum(w http.ResponseWriter, _ *http.Request) {
	jsonYaz(w, 200, DurumGetir())
}

func (h *Handler) IsDurum(w http.ResponseWriter, _ *http.Request) {
	jsonYaz(w, 200, map[string]any{"is": isSnapshot()})
}

func (h *Handler) YasakliListele(w http.ResponseWriter, _ *http.Request) {
	// Map'i liste'ye dönüştür
	out := make([]map[string]any, 0, len(YasakliPortlar))
	for p, aciklama := range YasakliPortlar {
		out = append(out, map[string]any{"port": p, "aciklama": aciklama})
	}
	jsonYaz(w, 200, map[string]any{
		"portlar":    out,
		"sistem_alt": 1023,
		"not":        "1-1023 sistem portları + listedeki portlar REDDEDİLİR",
	})
}

func (h *Handler) BackendDegistir(w http.ResponseWriter, r *http.Request) {
	var g degistirGovde
	if !govdeCoz(w, r, &g) {
		return
	}
	is, err := BackendPortDegistir(r.Context(), h.DB, g.YeniPort, h.aktorUID(r))
	if err != nil {
		degisiklikHataYaz(w, err)
		return
	}
	jsonYaz(w, 202, map[string]any{"ok": true, "is": is})
}

func (h *Handler) DisDegistir(w http.ResponseWriter, r *http.Request) {
	var g degistirGovde
	if !govdeCoz(w, r, &g) {
		return
	}
	is, err := DisPortDegistir(r.Context(), h.DB, g.YeniPort, h.aktorUID(r))
	if err != nil {
		degisiklikHataYaz(w, err)
		return
	}
	jsonYaz(w, 202, map[string]any{"ok": true, "is": is})
}

type gecmisSatir struct {
	ID        int64  `json:"id"`
	Tip       string `json:"tip"`
	EskiPort  int    `json:"eski_port"`
	YeniPort  int    `json:"yeni_port"`
	Basarili  bool   `json:"basarili"`
	Rollback  bool   `json:"rollback"`
	SonHata   string `json:"son_hata"`
	AktorUID  *int64 `json:"aktor_uid"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) Gecmis(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, tip, eski_port, yeni_port, basarili, rollback, son_hata, aktor_uid, created_at
		 FROM cp_port_gecmisi ORDER BY id DESC LIMIT 20`)
	if err != nil {
		hataYaz(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []gecmisSatir{}
	for rows.Next() {
		var s gecmisSatir
		if err := rows.Scan(&s.ID, &s.Tip, &s.EskiPort, &s.YeniPort,
			&s.Basarili, &s.Rollback, &s.SonHata, &s.AktorUID, &s.CreatedAt); err == nil {
			out = append(out, s)
		}
	}
	jsonYaz(w, 200, map[string]any{"items": out})
}

func jsonYaz(w http.ResponseWriter, k int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(k)
	_ = json.NewEncoder(w).Encode(v)
}
func hataYaz(w http.ResponseWriter, k int, m string) {
	jsonYaz(w, k, map[string]string{"error": m})
}
