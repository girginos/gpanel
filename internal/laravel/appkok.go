package laravel

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/httpx"
)

// laravelKokAdaylari: public_html + 1 seviye alt dizinlerde 'artisan' içeren (yani
// Laravel projesi kökü olan) yolları döndürür (home'a göre relative).
func laravelKokAdaylari(sk string) []string {
	base := "/home/" + sk + "/public_html"
	out := []string{}
	if _, err := os.Stat(filepath.Join(base, "artisan")); err == nil {
		out = append(out, "public_html")
	}
	if ents, err := os.ReadDir(base); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(base, e.Name(), "artisan")); err == nil {
				out = append(out, "public_html/"+e.Name())
			}
		}
	}
	return out
}

// GET /domains/{id}/laravel/app-adaylar — mevcut app_root + algılanan Laravel kökleri
func (h *Handlers) AppAdaylar(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	k := h.getKayit(r.Context(), id)
	mevcut := k.AppRoot
	if mevcut == "" {
		mevcut = "public_html"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"mevcut":  mevcut,
		"adaylar": laravelKokAdaylari(sk),
	})
}

// PUT /domains/{id}/laravel/app-root {"app_root":"public_html/uygulama"}
// Toolkit'in yöneteceği Laravel proje kökünü değiştirir (alt klasör / mevcut kurulumu
// sahiplenme). public_html içine confine (guvenliAppDir). Yeni kökte public varsa
// belge kökü otomatik <app_root>/public'e ayarlanır.
func (h *Handlers) SetAppRoot(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde değiştirilemez")
		return
	}
	var req struct {
		AppRoot string `json:"app_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := guvenliAppDir(sk, req.AppRoot) // '..'/symlink/public_html-dışı reddi
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	appRoot := strings.Trim(strings.TrimSpace(req.AppRoot), "/")
	if appRoot == "" {
		appRoot = "public_html"
	}
	if _, err := os.Stat(appDir); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dizin bulunamadı: "+appRoot)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE cp_laravel_apps SET app_root=? WHERE domain_id=?`, appRoot, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi: "+err.Error())
		return
	}
	artisan, _ := laravelKurulu(appDir)
	// yeni kökte public varsa belge kökünü de taşı
	if artisan {
		if _, e := os.Stat(filepath.Join(appDir, "public")); e == nil {
			_ = h.setDocroot(r.Context(), id, sk, altDizinPublic(appRoot))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "app_root": appRoot, "kurulu": artisan,
	})
}
