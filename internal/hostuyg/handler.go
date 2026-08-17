package hostuyg

// AdminOnly HTTP endpoint'leri:
//   GET  /admin/hostuyg/katalog                 — kurulabilir tarifler
//   GET  /admin/hostuyg                          — kurulu uygulamalar
//   GET  /admin/hostuyg/{id}                     — tek uygulama detay
//   POST /admin/hostuyg                          — kur (async iş başlat)
//   POST /admin/hostuyg/{id}/baslat              — start
//   POST /admin/hostuyg/{id}/durdur              — stop
//   DELETE /admin/hostuyg/{id}                   — kaldır (async iş)
//   GET  /admin/hostuyg/{id}/log?satir=100       — journalctl
//   GET  /admin/hostuyg/is/{is_id}               — async iş durum polling
//
// KURULUM iş boru hattı henüz TAM implement edilmedi (Faz 3.1). Şu an sadece
// skeleton: DB kayıt + user oluştur + unit yaz. İndir + config render + start
// + healthcheck + rollback SONRAKI turnuvada.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	DB    *sql.DB
	Aktor func(r *http.Request) (int64, bool)
}

/* ---------------- Katalog + liste ---------------- */

func (h *Handler) Katalog(w http.ResponseWriter, _ *http.Request) {
	yaz(w, 200, map[string]any{"items": KatalogListe()})
}

func (h *Handler) Liste(w http.ResponseWriter, r *http.Request) {
	items, err := UygulamaListe(r.Context(), h.DB)
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, map[string]any{"items": items})
}

func (h *Handler) Detay(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	k.UnitDurumu = UnitDurum(k.SystemdUnit)
	yaz(w, 200, k)
}

/* ---------------- Kur ---------------- */

type kurGovde struct {
	Kod     string `json:"kod"`
	OrnekAd string `json:"ornek_ad"` // opsiyonel — aynı katalog kodunu birden fazla kez kurmak için
}

// Kur — async iş başlat. Şu an SKELETON: tarif doğrula + DB kayıt + user +
// unit dosyası. Gerçek indirme + start SONRAKI faz.
func (h *Handler) Kur(w http.ResponseWriter, r *http.Request) {
	var g kurGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		yazHata(w, 400, "geçersiz gövde: "+err.Error())
		return
	}
	tarif := KatalogAra(g.Kod)
	if tarif == nil {
		yazHata(w, 404, "recipe bulunamadı: "+g.Kod)
		return
	}
	if err := tarif.Dogrula(); err != nil {
		yazHata(w, 500, "recipe geçersiz: "+err.Error())
		return
	}
	if g.OrnekAd == "" {
		g.OrnekAd = tarif.Kod
	}

	uid := aktorUID(h.Aktor, r)
	is, err := KurAsync(h.DB, tarif, g.OrnekAd, uid)
	if err != nil {
		yazHata(w, 409, err.Error()) // aynı ornek_ad çakışması veya validate fail
		return
	}
	yaz(w, 202, map[string]any{"ok": true, "is_id": is.ID})
}

/* ---------------- Baslat / Durdur / Sil ---------------- */

func (h *Handler) Baslat(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "baslat")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 15*time.Second)
	defer iptal()
	if err := UnitEnableStart(ctx, k.SystemdUnit); err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	_ = UygulamaDurumGuncelle(r.Context(), h.DB, id, "aktif", "")
	yaz(w, 200, map[string]any{"ok": true, "durum": UnitDurum(k.SystemdUnit)})
}

// Restart — POST /admin/hostuyg/{id}/restart — stop+start atomic-ish.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "restart")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 30*time.Second)
	defer iptal()
	// systemctl restart tek komut, race yok
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", k.SystemdUnit+".service").CombinedOutput(); err != nil {
		_ = UygulamaDurumGuncelle(r.Context(), h.DB, id, "hata", "restart fail: "+strings.TrimSpace(string(out)))
		yazHata(w, 500, "restart: "+strings.TrimSpace(string(out))+" ("+err.Error()+")")
		return
	}
	// MV#11 fix: post-check — systemctl restart async, unit failed olabilir
	time.Sleep(2 * time.Second)
	durum := UnitDurum(k.SystemdUnit)
	if durum == "failed" || durum == "inactive" {
		_ = UygulamaDurumGuncelle(r.Context(), h.DB, id, "hata",
			"restart sonrası unit "+durum+" — journalctl -u "+k.SystemdUnit+" incele")
		yazHata(w, 500, "restart sonrası unit "+durum)
		return
	}
	_ = UygulamaDurumGuncelle(r.Context(), h.DB, id, "aktif", "")
	yaz(w, 200, map[string]any{"ok": true, "durum": durum})
}

func (h *Handler) Durdur(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "durdur")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 15*time.Second)
	defer iptal()
	_ = UnitStopDisable(ctx, k.SystemdUnit)
	_ = UygulamaDurumGuncelle(r.Context(), h.DB, id, "durduruldu", "")
	yaz(w, 200, map[string]any{"ok": true})
}

func (h *Handler) Kaldir(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	uid := aktorUID(h.Aktor, r)
	is := KaldirAsync(h.DB, k, uid)
	yaz(w, 202, map[string]any{"ok": true, "is_id": is.ID})
}

/* ---------------- Log + iş durum ---------------- */

func (h *Handler) Log(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "log")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	satir := 100
	if v := r.URL.Query().Get("satir"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			satir = n
		}
	}
	yaz(w, 200, map[string]any{"log": UnitLog(k.SystemdUnit, satir)})
}

func (h *Handler) IsDurum(w http.ResponseWriter, r *http.Request) {
	isID := sonParca(r.URL.Path)
	is := IsGetir(isID)
	if is == nil {
		yazHata(w, 404, "iş bulunamadı")
		return
	}
	yaz(w, 200, is)
}

// Metrik — GET /admin/hostuyg/{id}/metrik — tek app snapshot metrik.
func (h *Handler) Metrik(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "metrik")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	yaz(w, 200, MetrikTopla(r.Context(), k))
}

// MetrikTumu — GET /admin/hostuyg/tumu/metrik — tüm app'ler için snapshot.
func (h *Handler) MetriklerTumu(w http.ResponseWriter, r *http.Request) {
	yaz(w, 200, map[string]any{"items": MetrikTumu(r.Context(), h.DB)})
}

/* ---------------- Yedekleme (Faz 3.5) ---------------- */

type yedekGovde struct {
	Aciklama string `json:"aciklama"`
}

func (h *Handler) YedekAl(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "yedek")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	var g yedekGovde
	_ = json.NewDecoder(r.Body).Decode(&g)
	uid := aktorUID(h.Aktor, r)
	// Async — büyük app'ler için HTTP timeout riski yok
	is := YedekAlAsync(h.DB, k, g.Aciklama, uid)
	yaz(w, 202, map[string]any{"ok": true, "is_id": is.ID})
}

func (h *Handler) YedekListe(w http.ResponseWriter, r *http.Request) {
	// URL: /admin/hostuyg/{id}/yedekler
	yol := strings.TrimSuffix(r.URL.Path, "/yedekler")
	id, err := idAyikla(yol, "")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	items, err := YedekListe(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, map[string]any{"items": items})
}

type restoreGovde struct {
	YedekID int64 `json:"yedek_id"`
}

func (h *Handler) YedekGeriYukle(w http.ResponseWriter, r *http.Request) {
	yol := strings.TrimSuffix(r.URL.Path, "/yedek/geriyukle")
	id, err := idAyikla(yol, "")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	var g restoreGovde
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		yazHata(w, 400, "geçersiz gövde")
		return
	}
	// Async
	is := YedekGeriYukleAsync(h.DB, k, g.YedekID)
	yaz(w, 202, map[string]any{"ok": true, "is_id": is.ID})
}

func (h *Handler) YedekSil(w http.ResponseWriter, r *http.Request) {
	// URL: /admin/hostuyg/yedek/{yedek_id}
	yid, err := strconv.ParseInt(sonParca(r.URL.Path), 10, 64)
	if err != nil || yid <= 0 {
		yazHata(w, 400, "geçersiz yedek id")
		return
	}
	if err := YedekSil(r.Context(), h.DB, yid); err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, map[string]any{"ok": true})
}

/* ---------------- Helpers ---------------- */

func idAyikla(yol, sonEk string) (int64, error) {
	p := yol
	if sonEk != "" {
		p = strings.TrimSuffix(p, "/"+sonEk)
	}
	last := sonParca(p)
	id, err := strconv.ParseInt(last, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("geçersiz id")
	}
	return id, nil
}

func sonParca(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func aktorUID(fn func(*http.Request) (int64, bool), r *http.Request) *int64 {
	if fn == nil {
		return nil
	}
	if u, ok := fn(r); ok {
		return &u
	}
	return nil
}

func yaz(w http.ResponseWriter, k int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(k)
	_ = json.NewEncoder(w).Encode(v)
}
func yazHata(w http.ResponseWriter, k int, m string) {
	yaz(w, k, map[string]string{"error": m})
}
