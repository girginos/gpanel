package laravel

import (
	"encoding/json"
	"errors"
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
	icerik, rerr := envOku(sk, appDir)
	if errors.Is(rerr, errEnvYok) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"var": false, "icerik": ""})
		return
	}
	if rerr != nil {
		// Dosya var olabilir ama güvenle okunamadı (ör. uygulama dizini ev içi symlink
		// üzerinden): boş içerik DÖNME — düzenleyici onu kaydedip gerçek .env'i ezerdi.
		httpx.WriteError(w, http.StatusConflict, rerr.Error())
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
	// Mevcut .env güvenle okunamıyorsa yazma (düzenleyicide boş görünmüş olabilir).
	if err := envYazilabilir(sk, appDir); err != nil {
		httpx.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err := envYazici(sk, appDir, req.Icerik); err != nil {
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
