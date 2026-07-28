package reseller

import (
	"net/http"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// Ozet: GET /reseller/ozet — bayinin kendi panosu (hosting adedi, disk/trafik havuzu,
// plan sayisi, askidaki hesaplar). Admin cagirirsa 400 (adminin kendi panosu ayri).
func (h *Handlers) Ozet(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r)
	if rid == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bu özet yalnız bayi hesapları içindir")
		return
	}
	var maxDomain int
	var maxDisk, maxTrafik int64
	var paketAd string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT u.max_domain, u.max_disk_mb, u.max_trafik_mb,
		        COALESCE((SELECT ad FROM reseller_plans rp WHERE rp.id=u.reseller_plan_id),'')
		   FROM users u WHERE u.id=?`, rid).
		Scan(&maxDomain, &maxDisk, &maxTrafik, &paketAd)

	var adet, askida int
	var diskKB, trafikKB int64
	var taahhutDisk, taahhutTrafik int64
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(boyut_kb),0), COALESCE(SUM(trafik_kb),0),
		        COALESCE(SUM(COALESCE(askida,0)),0)
		   FROM domains WHERE reseller_id=?`, rid).
		Scan(&adet, &diskKB, &trafikKB, &askida)
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(COALESCE(p.disk_kota_mb,0)),0), COALESCE(SUM(COALESCE(p.trafik_kota_mb,0)),0)
		   FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.reseller_id=?`, rid).
		Scan(&taahhutDisk, &taahhutTrafik)

	var planAdet, sablonSatir int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM service_plans WHERE reseller_id=?`, rid).Scan(&planAdet)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM dns_template WHERE reseller_id=?`, rid).Scan(&sablonSatir)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"paket_ad":           paketAd,
		"hosting_adet":       adet,
		"hosting_limit":      maxDomain,
		"askida_adet":        askida,
		"disk_kullanim_kb":   diskKB,
		"disk_taahhut_mb":    taahhutDisk,
		"disk_limit_mb":      maxDisk,
		"trafik_kullanim_kb": trafikKB,
		"trafik_taahhut_mb":  taahhutTrafik,
		"trafik_limit_mb":    maxTrafik,
		"plan_adet":          planAdet,
		"dns_sablon_ozel":    sablonSatir > 0,
	})
}
