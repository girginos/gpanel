// Package bildirim — panel/müşteri bildirim akışı (cp_bildirim).
//
// 🔴 NEDEN GENEL: antivirüs bulgusu müşteriye/operatöre AKMALI — yoksa tarama
// sessizce loglanır, kimse görmez ("başarısızlık güven olarak render"). Ama
// bu paket yalnız AV'ye özel değil; kategori alanıyla her modül kullanabilir
// (SSL bitişi, yedek hatası, kota aşımı...).
package bildirim

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
)

type Handlers struct{ DB *sql.DB }

type Bildirim struct {
	ID       int64  `json:"id"`
	Seviye   string `json:"seviye"`
	Kategori string `json:"kategori"`
	Baslik   string `json:"baslik"`
	Mesaj    string `json:"mesaj"`
	DomainID *int64 `json:"domain_id"`
	RefTur   string `json:"ref_tur"`
	RefID    int64  `json:"ref_id"`
	Okundu   bool   `json:"okundu"`
	Tarih    string `json:"tarih"`
}

// Liste — GET /api/v1/bildirimler?sadece_okunmamis=1&kategori=antivirus
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	q := `SELECT id, seviye, kategori, baslik, mesaj, domain_id, ref_tur, ref_id, okundu,
	             DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
	        FROM cp_bildirim`
	var kos []string
	var arg []any
	if r.URL.Query().Get("sadece_okunmamis") == "1" {
		kos = append(kos, "okundu=0")
	}
	if k := r.URL.Query().Get("kategori"); k != "" {
		kos = append(kos, "kategori=?")
		arg = append(arg, k)
	}
	if len(kos) > 0 {
		q += " WHERE " + joinAnd(kos)
	}
	q += " ORDER BY created_at DESC LIMIT 200"

	rows, err := h.DB.QueryContext(r.Context(), q, arg...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []Bildirim{}
	for rows.Next() {
		var b Bildirim
		var dom sql.NullInt64
		if rows.Scan(&b.ID, &b.Seviye, &b.Kategori, &b.Baslik, &b.Mesaj, &dom,
			&b.RefTur, &b.RefID, &b.Okundu, &b.Tarih) != nil {
			continue
		}
		if dom.Valid {
			b.DomainID = &dom.Int64
		}
		out = append(out, b)
	}
	// Okunmamış sayısı — panel badge için.
	var okunmamis int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM cp_bildirim WHERE okundu=0`).Scan(&okunmamis)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"bildirimler": out, "okunmamis": okunmamis})
}

// Okundu — POST /api/v1/bildirimler/{id}/okundu  (id=0 → hepsi)
func (h *Handlers) Okundu(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var err error
	if id == 0 {
		_, err = h.DB.Exec(`UPDATE cp_bildirim SET okundu=1 WHERE okundu=0`)
	} else {
		_, err = h.DB.Exec(`UPDATE cp_bildirim SET okundu=1 WHERE id=?`, id)
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Yaz — modül-içi kullanım (AV, SSL, ...): bildirim oluştur.
func Yaz(db *sql.DB, seviye, kategori, baslik, mesaj string, domainID int64, refTur string, refID int64) {
	if db == nil {
		return
	}
	var dom any
	if domainID > 0 {
		dom = domainID
	}
	_, _ = db.Exec(
		`INSERT INTO cp_bildirim (seviye, kategori, baslik, mesaj, domain_id, ref_tur, ref_id)
		 VALUES (?,?,?,?,?,?,?)`, seviye, kategori, baslik, mesaj, dom, refTur, refID)
}

func joinAnd(l []string) string {
	out := ""
	for i, s := range l {
		if i > 0 {
			out += " AND "
		}
		out += s
	}
	return out
}

var _ = json.Marshal // (ileride)
