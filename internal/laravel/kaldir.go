package laravel

// kaldir.go — Laravel uygulamasını PANELDEN kaldırma (DELETE /domains/{id}/laravel).
//
// Dosyalara DOKUNMAZ (kod/veri kullanıcıya aittir). Yalnız panel kaydını ve bağlı
// otomasyonu (cron zamanlayıcı, kuyruk birimi) kaldırır ve belge kökünü geri alır:
// web_root tam olarak <app kökü>/public ise alan adı varsayılanına (public_html) döner —
// aksi halde alan adı olmayan /public dizinini sunmaya devam eder (404, "Primary script
// unknown"). web_root elle başka yere ayarlanmışsa DOKUNULMAZ (bilinçli ayar).
// WordPress kurulum ön kontrolü de bayat kaydı temizlemek için KaydiKaldir'i çağırır.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/exec"

	"girginospanel/internal/belgekok"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// kuyrukKapat: kuyruk işleyici birimini durdurup siler (Queue(aktif=false) ile aynı adımlar).
func kuyrukKapat(id int64) {
	unit := queueUnitName(id) + ".service"
	_ = exec.Command("systemctl", "disable", "--now", unit).Run()
	_ = os.Remove(queueUnitPath(id))
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "reset-failed", unit).Run()
}

// KaydiKaldir: domain'in Laravel panel kaydını kaldırır (cron + kuyruk + web_root geri alma
// + cp_laravel_apps satırı). geri=true → web_root varsayılana döndürüldü. Kayıt yoksa
// (false, nil). web_root geri alınamazsa (vhost render hatası) satır SİLİNMEZ: yeniden denenebilir.
func KaydiKaldir(ctx context.Context, db *sql.DB, id int64, sk string) (geri bool, err error) {
	if !gecerliSK(sk) {
		return false, errors.New("geçersiz kullanıcı")
	}
	defer kilitle(id)()
	h := &Handlers{DB: db}
	k := h.getKayit(ctx, id)
	if !k.Mevcut {
		return false, nil
	}
	_ = cronSil(id)
	if k.QueueEnabled {
		kuyrukKapat(id)
	}
	// app_root güvenle çözülemezse (geçersiz/gizli/ev dışı) web_root'a dokunulmaz.
	if appDir, aerr := guvenliAppDir(sk, k.AppRoot); aerr == nil {
		base := evKoku + "/" + sk + "/public_html"
		if geri, err = belgekok.GeriAl(ctx, db, id, sk, base, appDir); err != nil {
			return false, err
		}
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM cp_laravel_apps WHERE domain_id=?`, id); err != nil {
		return geri, err
	}
	return geri, nil
}

// DELETE /domains/{id}/laravel
func (h *Handlers) Kaldir(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde Laravel kaydı kaldırılamaz")
		return
	}
	k := h.getKayit(r.Context(), id)
	if !k.Mevcut {
		httpx.WriteError(w, http.StatusNotFound, "bu domain için Laravel kaydı yok")
		return
	}
	if k.SonDeployDurum == "kuruluyor" || k.SonDeployDurum == "calisiyor" {
		httpx.WriteError(w, http.StatusConflict, "bu domain için bir işlem (kurulum/dağıtım) sürüyor — bitmesini bekleyin")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	geri, err := KaydiKaldir(kctx, h.DB, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Laravel kaydı kaldırılamadı (belge kökü geri alınamadı); tekrar deneyin")
		return
	}
	var alan string
	_ = h.DB.QueryRowContext(kctx, `SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alan)
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "laravel.kaldir", alan, "hesap="+sk, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "web_root_geri_alindi": geri})
}
