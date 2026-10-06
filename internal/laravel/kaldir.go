package laravel

// kaldir.go — Laravel uygulamasını PANELDEN kaldırma. Rota: DELETE /domains/{id}/laravel
// (YALNIZ API: arayüzde düğme yok; isteyen otomasyon/destek kullanır; MusteriScope ile
// kapsam-kapılı, her işlem domain'in KENDİ sistem kullanıcısı üzerinden).
//
// Dosyalara DOKUNMAZ (kod/veri kullanıcıya aittir). Panel kaydını ve bağlı otomasyonu (cron
// zamanlayıcı, kuyruk birimi) kaldırır ve belge kökünü geri alır: web_root tam olarak
// <app kökü>/public ise alan adı varsayılanına (public_html) döner — aksi halde alan adı
// olmayan /public dizinini sunmaya devam eder (404, "Primary script unknown"). web_root elle
// başka yere ayarlanmışsa DOKUNULMAZ. WordPress kurulum ön kontrolü de bayat kaydı
// temizlemek için KaydiKaldir'i çağırır (aynı kurallar, aynı kilit).
//
// SIRA (yarım kalma güvenliği): 1) kurulum/dağıtım sürüyorsa RET (ErrIslemSuruyor);
// 2) web_root geri al — render hatasında HİÇBİR şeye dokunulmaz (cron/kuyruk/kayıt yerinde);
// 3) cron + kuyruk kapat — hata olursa kayıt SİLİNMEZ (sahipsiz cron çalışmaya devam etmesin;
// kayıt kalınca işlem yeniden denenebilir); 4) ancak hepsi başarılıysa kayıt silinir.
// app_root güvenle çözülemezse (geçersiz/gizli/ev dışı) web_root'a dokunulmaz ve geri=false
// döner (geri alma "yapılamadı" = hata değil); cron/kuyruk kapatma ve kayıt silme yine 3-4
// kuralına tabidir.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"girginospanel/internal/belgekok"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// ErrIslemSuruyor: bu domain için Laravel kurulumu/dağıtımı sürüyor; kayıt ve belge kökü
// dokunulmaz (kurulumu ezmesin).
var ErrIslemSuruyor = errors.New("laravel kurulum/dağıtım işlemi sürüyor")

// kuyrukKapatFn: dikiş (testler sahteler; üretimde kuyrukKapat — TestKaynakKoruma_DikisDegerleri kilitler).
var kuyrukKapatFn = kuyrukKapat

// kuyrukKapat: kuyruk işleyici birimini durdurup siler (Queue(aktif=false) ile aynı adımlar).
// Birim dosyası varken durdurma ya da dosya silme başarısızsa HATA döner (kayıt silinmesin).
func kuyrukKapat(id int64) error {
	unit := queueUnitName(id) + ".service"
	_, statErr := os.Stat(queueUnitPath(id))
	var hata error
	if statErr == nil {
		if err := exec.Command("systemctl", "disable", "--now", unit).Run(); err != nil {
			hata = fmt.Errorf("kuyruk birimi durdurulamadı: %w", err)
		}
	}
	if err := os.Remove(queueUnitPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		hata = fmt.Errorf("kuyruk birimi silinemedi: %w", err)
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "reset-failed", unit).Run()
	return hata
}

// KaydiKaldir: domain'in Laravel panel kaydını kaldırır. geri=true → web_root varsayılana
// döndürüldü. Kayıt yoksa (false, nil). Kurulum/dağıtım sürüyorsa ErrIslemSuruyor.
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
	if k.SonDeployDurum == "kuruluyor" || k.SonDeployDurum == "calisiyor" {
		return false, ErrIslemSuruyor
	}
	// (2) belge kökü ÖNCE: render hatasında cron/kuyruk/kayıt dokunulmamış kalır.
	if appDir, aerr := guvenliAppDir(sk, k.AppRoot); aerr == nil {
		base := evKoku + "/" + sk + "/public_html"
		if geri, err = belgekok.GeriAl(ctx, db, id, sk, base, appDir); err != nil {
			return false, err
		}
	}
	// (3) otomasyonu kapat; hata → kayıt kalır.
	if err = cronSil(id); err != nil {
		return geri, fmt.Errorf("zamanlanmış görev kaldırılamadı: %w", err)
	}
	if k.QueueEnabled {
		if err = kuyrukKapatFn(id); err != nil {
			return geri, err
		}
	}
	// (4) her şey başarılı → kayıt silinir.
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
	if !h.getKayit(r.Context(), id).Mevcut {
		httpx.WriteError(w, http.StatusNotFound, "bu domain için Laravel kaydı yok")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	geri, err := KaydiKaldir(kctx, h.DB, id, sk)
	if errors.Is(err, ErrIslemSuruyor) {
		httpx.WriteError(w, http.StatusConflict, "bu domain için bir işlem (kurulum/dağıtım) sürüyor — bitmesini bekleyin")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Laravel kaydı kaldırılamadı (belge kökü geri alınamadı ya da zamanlanmış görev/kuyruk kapatılamadı); kayıt yerinde, tekrar deneyin")
		return
	}
	var alan string
	_ = h.DB.QueryRowContext(kctx, `SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alan)
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "laravel.kaldir", alan, "hesap="+sk, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "web_root_geri_alindi": geri})
}
