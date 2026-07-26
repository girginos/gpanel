package wordpress

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// wpKapsam: WordPress islemlerinin hedef kokunu + site host unu cozer.
// URL de {sid} (subdomain) varsa islemi o alt-alanin docroot una hapseder;
// yoksa domain in public_html ine. Guvenlik: root daima /home/<sk> altinda,
// tam_ad DB den (domain_id eslesmeli) gelir; tenant tampere edemez.
func (h *Handlers) wpKapsam(r *http.Request, sk string) (root, host string, altAlan bool) {
	sidStr := chi.URLParam(r, "sid")
	if sidStr != "" {
		sid, _ := strconv.ParseInt(sidStr, 10, 64)
		id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		var tamAd string
		if err := h.DB.QueryRow(`SELECT tam_ad FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&tamAd); err == nil && tamAd != "" {
			return "/home/" + sk + "/subdomains/" + tamAd, tamAd, true
		}
	}
	return "/home/" + sk + "/public_html", "", false
}
