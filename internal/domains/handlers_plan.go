// Plan atama + kaynak limit yeniden uygulama.
package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"

	"github.com/go-chi/chi/v5"
)

// PUT /domains/{id}/plan  body: {"plan_id": 3}  (null → planı kaldır)
type setPlanReq struct {
	PlanID *int64 `json:"plan_id"`
}

func (h *Handlers) SetPlan(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setPlanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// Plan var mı doğrula
	if req.PlanID != nil {
		var n int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM service_plans WHERE id=?`, *req.PlanID).Scan(&n); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
	}
	// Domain var mı
	var sk string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		} else {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	// Güncelle
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET plan_id=? WHERE id=?`, req.PlanID, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}
	// Kaynak limitlerini yeniden uygula — arkaplanda + kendi context'i
	// (r.Context() HTTP request bitince iptal olur, cgroup yazımı yarıda kalır)
	go func(did int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := kaynaklimit.UygulaHepsi(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply domain=%d: %v", did, err)
		}
	}(id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "plan_id": req.PlanID})
}
