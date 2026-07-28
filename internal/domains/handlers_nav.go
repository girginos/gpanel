package domains

import (
	"net/http"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// NavSayac: sol menudeki sayilar (Plesk deseni — "Alan Adları  38").
// Rol-duyarli: bayi yalnizca KENDI kayitlarinin sayisini gorur.
// Katalog disi (domaine-ozel) planlar sayilmaz — menude de listelenmiyorlar.
func (h *Handlers) NavSayac(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r)
	out := map[string]int{}

	say := func(anahtar, sorgu string, args ...any) {
		var n int
		if err := h.DB.QueryRowContext(r.Context(), sorgu, args...).Scan(&n); err == nil && n > 0 {
			out[anahtar] = n // 0 ise ANAHTAR YOK → menude rozet cikmaz (gurultu olmasin)
		}
	}

	if rid > 0 {
		say("domainler", `SELECT COUNT(*) FROM domains WHERE reseller_id=?`, rid)
		say("hizmet_planlari", `SELECT COUNT(*) FROM service_plans WHERE domain_id IS NULL AND reseller_id=?`, rid)
		httpx.WriteJSON(w, http.StatusOK, out)
		return
	}

	say("domainler", `SELECT COUNT(*) FROM domains`)
	say("subdomainler", `SELECT COUNT(*) FROM subdomanlar`)
	say("bayiler", `SELECT COUNT(*) FROM users WHERE role='reseller'`)
	say("hizmet_planlari", `SELECT COUNT(*) FROM service_plans WHERE domain_id IS NULL`)
	say("bayi_planlari", `SELECT COUNT(*) FROM reseller_plans`)
	say("musteriler", `SELECT COUNT(*) FROM customers`)
	httpx.WriteJSON(w, http.StatusOK, out)
}
