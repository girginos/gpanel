package domains

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	destekiptal "girginospanel/internal/destek/iptal"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/hesapaski"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"

	"github.com/go-chi/chi/v5"
)

// AskiyaAl: POST /domains/{id}/askiya-al — hosting HESABINI askıya al.
// Askı hesap düzeyindedir: ana alan adı, TÜM alt alan adları, PHP havuzları,
// cron, FTP/panel/SSH girişleri ve veritabanı kullanıcıları birlikte kapanır
// (bkz. hesapaski). Askı durumu DB'de kalıcıdır; her yeniden render'da uygulanır.
func (h *Handlers) AskiyaAl(w http.ResponseWriter, r *http.Request) {
	h.askiToggle(w, r, true)
}

// AskidanAl: POST /domains/{id}/askidan-al — askıyı kaldır, hesabı askı öncesi
// hâline getir.
func (h *Handlers) AskidanAl(w http.ResponseWriter, r *http.Request) {
	h.askiToggle(w, r, false)
}

// askiAdimlari — hesap askısının adımları (dikiş). Üretimde hesapaski'nin
// adımları; ana vhost adımı paketin vhostYenidenCiz dikişinden geçer. Testler
// sahteyle değiştirir: ana makinenin nginx/php-fpm/MariaDB'sine dokunulmaz.
var askiAdimlari = func() hesapaski.Adimlar {
	a := hesapaski.Varsayilan()
	a.AnaVhost = vhostYenidenCiz
	return a
}

// AskiUygula: tek bir hosting hesabına askıyı uygular/kaldırır — TEK yol
// (hesapaski.Uygula). Tekil uç ve TOPLU işlem aynı makineyi kullanır; bayi
// zinciri de (hesapaski.BayiUygula) aynı Uygula'dan geçer. Uygula istek
// bağlamından bağımsız koşar: istemci kopsa da işlem yarıda kalmaz.
func (h *Handlers) AskiUygula(ctx context.Context, id int64, askida bool, kaynak string) (string, error) {
	alan, err := askiAdimlari().Uygula(ctx, h.DB, id, askida, kaynak)
	if askida && err == nil {
		// Telegram destek: askıdaki hostingin müşteri bot bağlantıları iptal
		// (kaçan yollar ajanın kimlik izi denetiminde yakalanır).
		destekiptal.Domain(ctx, h.DB, id, protokol.IptalAlanSilme, 0, "sistem")
	}
	return alan, err
}

// elleAskiKaynagi — elle askının kaynağı isteği yapana göre: kök (yönetici)
// ya da bayi. Bayi kökün askısını kaldıramaz (hesapaski.ErrKokAskisi); yönetici
// olmayan her rol bayi sayılır (SahipYonetim / AdminVeyaReseller başka rolü
// zaten geçirmez).
func elleAskiKaynagi(r *http.Request) string {
	if middleware.RolFrom(r) == "admin" {
		return hesapaski.KaynakKok
	}
	return hesapaski.KaynakBayiElle
}

func (h *Handlers) askiToggle(w http.ResponseWriter, r *http.Request, askida bool) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// Elle alınan askı — bayi zinciri bunu ezmez; bayi durum kuralları (bayi
	// askıdayken tek tek askıya alma/açma yok) ve kök askısı kuralı Uygula içinde
	// (hesap kilidi altında okunan kaynağa göre).
	alanAdi, err := h.AskiUygula(r.Context(), id, askida, elleAskiKaynagi(r))
	uid, kul := middleware.Aktor(r)
	eylem := "hosting.askidan_al"
	if askida {
		eylem = "hosting.askiya_al"
	}
	var vh *hesapaski.VhostHatasi
	var kh *hesapaski.KismiHata
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	case errors.Is(err, hesapaski.ErrDemo), errors.Is(err, hesapaski.ErrKokAskisi):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	case errors.Is(err, hesapaski.ErrBayiAskida), errors.Is(err, hesapaski.ErrBayiAskidaAcilamaz):
		httpx.WriteError(w, http.StatusConflict, err.Error())
		return
	case errors.As(err, &vh):
		// DB ile nginx ayrışmasın diye durum geri alındı — hiçbir şey değişmedi.
		httpx.WriteError(w, http.StatusInternalServerError, "vhost render: "+nginxset.SunucuHatasi(r, "domains.aski", id, vh.Err))
		return
	case errors.As(err, &kh):
		// Bir kısmı uygulandı: güvenli tarafta (hesap askıda) kalındı. Ham adım
		// hataları (nginx -t, systemctl, mysql çıktısı) yalnız yöneticiye.
		durum := "işlem kısmen uygulandı, hesap askıda bırakıldı: "
		if !kh.Askida {
			durum = "işlem kısmen uygulandı: "
		}
		httpx.DenetimDomain(h.DB, r, uid, kul, eylem, alanAdi, "kısmen uygulandı", id, false)
		httpx.WriteError(w, http.StatusInternalServerError,
			durum+nginxset.SunucuHatasi(r, "domains.aski", id, errors.Join(kh.Hatalar...)))
		return
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "askı uygulanamadı: "+nginxset.SunucuHatasi(r, "domains.aski", id, err))
		return
	}

	httpx.DenetimDomain(h.DB, r, uid, kul, eylem, alanAdi, "", id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "alan_adi": alanAdi, "askida": askida,
	})
}
