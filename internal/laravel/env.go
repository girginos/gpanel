package laravel

import (
	"encoding/json"
	"net/http"

	"girginospanel/internal/httpx"
)

// GET /domains/{id}/laravel/env
func (h *Handlers) EnvOku(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	icerik, rerr := envOku(appDir)
	if rerr != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"var": false, "icerik": ""})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"var": true, "icerik": icerik})
}

// PUT /domains/{id}/laravel/env  {"icerik":"..."}
func (h *Handlers) EnvYaz(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde .env düzenlenemez")
		return
	}
	var req struct {
		Icerik string `json:"icerik"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := envYaz(sk, appDir, req.Icerik); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /domains/{id}/laravel/bakim  {"aktif":true}
func (h *Handlers) Bakim(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde bakım modu değiştirilemez")
		return
	}
	var req struct {
		Aktif bool `json:"aktif"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	komut := "up"
	if req.Aktif {
		komut = "down"
	}
	out, cok := TenantExec(r.Context(), sk, appDir, phpBin(phpSurum), "artisan", komut)
	if cok {
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET maintenance=? WHERE domain_id=?`,
			map[bool]int{true: 1, false: 0}[req.Aktif], id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": cok, "bakim": req.Aktif, "cikti": out})
}
