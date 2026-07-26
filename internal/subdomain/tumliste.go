package subdomain

import (
	"net/http"

	"girginospanel/internal/httpx"
)

// SubGenel: ana Domainler listesinde gösterilmek üzere zenginleştirilmiş subdomain kaydı.
type SubGenel struct {
	ID          int64  `json:"id"`
	AltAd       string `json:"alt_ad"`
	TamAd       string `json:"tam_ad"`
	ParentID    int64  `json:"parent_id"`
	ParentAd    string `json:"parent_ad"`
	SistemK     string `json:"sistem_kullanici"`
	PHPSurum    string `json:"php_surum"`
	DocRoot     string `json:"docroot"`
	OlusturmaAt string `json:"olusturulma"`
}

// TumListe: GET /subdomains — tüm subdomain'leri parent domain bilgisiyle listeler (AdminOnly).
// Ana Domainler ekranı bunu çekip subdomain'leri parent'ının altında gösterir.
func (h *Handlers) TumListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT s.id, s.alt_ad, s.tam_ad, s.domain_id, d.alan_adi, d.sistem_kullanici,
		        s.php_surum, DATE_FORMAT(s.created_at,'%Y-%m-%d')
		   FROM subdomanlar s JOIN domains d ON d.id = s.domain_id
		  ORDER BY d.alan_adi, s.alt_ad`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	out := []SubGenel{}
	for rows.Next() {
		var s SubGenel
		if err := rows.Scan(&s.ID, &s.AltAd, &s.TamAd, &s.ParentID, &s.ParentAd,
			&s.SistemK, &s.PHPSurum, &s.OlusturmaAt); err == nil {
			s.DocRoot = docrootOf(s.SistemK, s.TamAd)
			out = append(out, s)
		}
	}
	_ = rows.Err()
	httpx.WriteJSON(w, http.StatusOK, out)
}
