package portyonetim

// HTTP endpoint'leri:
//   GET  /admin/portlar               — durum + aktif iş
//   POST /admin/portlar/backend       — backend port değiştir (async)
//   POST /admin/portlar/dis           — dış port değiştir (async)
//   GET  /admin/portlar/is            — aktif iş durumu (polling)
//   GET  /admin/portlar/gecmis        — cp_port_gecmisi (son 20)
//   GET  /admin/portlar/yasakli       — yasaklı portlar listesi (UI için)

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type Handler struct {
	DB    *sql.DB
	Aktor func(r *http.Request) (int64, bool)
}

type degistirGovde struct {
	YeniPort int `json:"yeni_port"`
}

func (h *Handler) Durum(w http.ResponseWriter, _ *http.Request) {
	jsonYaz(w, 200, DurumGetir())
}

func (h *Handler) IsDurum(w http.ResponseWriter, _ *http.Request) {
	jsonYaz(w, 200, map[string]any{"is": isSnapshot()})
}

func (h *Handler) YasakliListele(w http.ResponseWriter, _ *http.Request) {
	// Map'i sıralı liste'ye dönüştür
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
	is, err := BackendPortDegistir(r.Context(), h.DB, g.YeniPort, uid)
	if err != nil {
		// Kilit durumu → 409
		if err.Error() == "başka bir port değişikliği devam ediyor" {
			hataYaz(w, 409, err.Error())
			return
		}
		hataYaz(w, 400, err.Error())
		return
	}
	jsonYaz(w, 202, map[string]any{"ok": true, "is": is})
}

func (h *Handler) DisDegistir(w http.ResponseWriter, r *http.Request) {
	var g degistirGovde
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
	is, err := DisPortDegistir(r.Context(), h.DB, g.YeniPort, uid)
	if err != nil {
		if err.Error() == "başka bir port değişikliği devam ediyor" {
			hataYaz(w, 409, err.Error())
			return
		}
		hataYaz(w, 400, err.Error())
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
