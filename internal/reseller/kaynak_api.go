package reseller

// Bayi kaynak havuzu API'si (CPU çekirdek / fiziksel RAM / disk havuzu / G-Ç ağırlığı).
//
//	yönetici : GET  /bayi-kaynak                       tüm bayilerin havuz özeti
//	           GET  /bayi-kaynak/{id}                  bir bayinin ayrıntılı durumu
//	           GET  /bayi-kaynak/{id}/kullanim?aralik= zaman serisi (24s | 7g | 30g)
//	           POST /bayi-kaynak/{id}/uygula           havuzu şimdi uzlaştır
//	           GET  /bayi-kaynak/disk-kotasi           XFS proje kotası durumu
//	           POST /bayi-kaynak/disk-kotasi/etkinlestir  rootflags'e pquota ekle (AÇIK onay)
//	bayi     : GET  /reseller/kaynak                   YALNIZ kendi havuzu
//	           GET  /reseller/kaynak/kullanim?aralik=  YALNIZ kendi serisi
//
// Bayi uçları kimliği istekten DEĞİL, oturumdan alır (middleware.ResellerIDFrom): başka bayinin
// kimliğini göndermenin bir yolu yoktur (IDOR yok). Yönetici bu uçlara girerse 400 alır.

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"girginospanel/internal/bayikaynak"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Sunucuya dokunan eylemler için dikiş noktaları: testler GERÇEK uzlaştırma / grubby çalıştırmaz.
var (
	kaynakUzlastirTam = func(ctx context.Context, db *sql.DB) bayikaynak.Rapor {
		return bayikaynak.UzlastirTam(ctx, db, false)
	}
	kaynakDiskEtkinlestir = bayikaynak.Etkinlestir
	// Aşım listesi sunucunun kota raporuna bağlıdır (xfs_quota); testler belirli bir listeyi sabitler.
	kaynakAsimdakiler = bayikaynak.AsimdakiBayiler
)

func kaynakAralik(r *http.Request) (string, bool) {
	a := r.URL.Query().Get("aralik")
	if a == "" {
		a = "24s"
	}
	return a, bayikaynak.AralikGecerli(a)
}

// bayiKimligi: URL'den bayi kimliği (yönetici uçları). Var olmayan/bayi olmayan kayıt → false.
func (h *Handlers) kaynakBayiKimligi(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz bayi kimliği")
		return 0, false
	}
	var n int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, id).Scan(&n)
	if n == 0 {
		httpx.WriteError(w, http.StatusNotFound, "bayi bulunamadı")
		return 0, false
	}
	return id, true
}

// GET /bayi-kaynak — tüm bayilerin havuz özeti (yönetici).
func (h *Handlers) KaynakListe(w http.ResponseWriter, r *http.Request) {
	liste, err := bayikaynak.TumOzetler(r.Context(), h.DB)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "bayi havuzları okunamadı")
		return
	}
	if liste == nil {
		liste = []bayikaynak.BayiOzet{}
	}
	httpx.WriteJSON(w, http.StatusOK, liste)
}

// GET /bayi-kaynak/{id} — bir bayinin ayrıntılı havuz durumu (yönetici).
func (h *Handlers) KaynakDetay(w http.ResponseWriter, r *http.Request) {
	id, ok := h.kaynakBayiKimligi(w, r)
	if !ok {
		return
	}
	d, err := bayikaynak.Durumu(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "havuz durumu okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

// GET /bayi-kaynak/{id}/kullanim?aralik= — zaman serisi (yönetici).
func (h *Handlers) KaynakKullanim(w http.ResponseWriter, r *http.Request) {
	id, ok := h.kaynakBayiKimligi(w, r)
	if !ok {
		return
	}
	h.kaynakSeriYaz(w, r, id)
}

func (h *Handlers) kaynakSeriYaz(w http.ResponseWriter, r *http.Request, bayiID int64) {
	aralik, gecerli := kaynakAralik(r)
	if !gecerli {
		httpx.WriteError(w, http.StatusBadRequest, "aralik 24s, 7g ya da 30g olmalı")
		return
	}
	seri, err := bayikaynak.Seri(r.Context(), h.DB, bayiID, aralik, bayikaynak.SimdiUTC())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kullanım serisi okunamadı")
		return
	}
	// Seri hiç satır yokken de boş dilim (nil değil) döner: sözleşme bayikaynak testlerinde kilitli.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"bayi_id": bayiID, "aralik": aralik,
		"adim_sn": int(bayikaynak.OrnekAraligi / time.Second), "noktalar": seri,
	})
}

// POST /bayi-kaynak/{id}/uygula — havuzu şimdi uzlaştır (yönetici). CPU/RAM dilimi ve üyelik
// eşzamanlı, disk projesi arka planda (büyük ev ağaçları uzun sürebilir).
func (h *Handlers) KaynakUygula(w http.ResponseWriter, r *http.Request) {
	id, ok := h.kaynakBayiKimligi(w, r)
	if !ok {
		return
	}
	rapor := kaynakUzlastirTam(r.Context(), h.DB)
	bayiHavuzUygula(h.DB)
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "bayi.kaynak_uygula", strconv.FormatInt(id, 10), "", id, len(rapor.Hatalar) == 0)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": len(rapor.Hatalar) == 0, "rapor": rapor})
}

// GET /bayi-kaynak/disk-kotasi — XFS proje kotası durumu + (kuru çalıştırma) önerilen komut (yönetici).
func (h *Handlers) DiskKotasiDurum(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.diskKotasiOnizleme(r))
}

type diskKotasiYanit struct {
	Durum    bayikaynak.DiskDurum        `json:"durum"`
	Onizleme bayikaynak.EtkinlestirSonuc `json:"onizleme"`
	Hata     string                      `json:"hata,omitempty"`
}

func (h *Handlers) diskKotasiOnizleme(r *http.Request) diskKotasiYanit {
	y := diskKotasiYanit{Durum: bayikaynak.DiskDurumu(r.Context())}
	s, err := kaynakDiskEtkinlestir(r.Context(), false)
	if err != nil {
		y.Hata = err.Error()
	}
	s.AsimdakiBayiler, _ = kaynakAsimdakiler(r.Context(), h.DB)
	y.Onizleme = s
	return y
}

type diskEtkinlestirReq struct {
	Onayla    bool `json:"onayla"`
	AsimKabul bool `json:"asim_kabul"`
}

// POST /bayi-kaynak/disk-kotasi/etkinlestir — rootflags'e pquota ekler (yönetici, AÇIK onay).
// Panel bunu kendiliğinden ASLA yapmaz. Etki bir sonraki yeniden başlatmada başlar. Kullanımı
// havuzunu zaten aşan bayiler varsa, sınır açılınca yazamayacakları için ayrıca "asim_kabul" ister.
func (h *Handlers) DiskKotasiEtkinlestir(w http.ResponseWriter, r *http.Request) {
	var req diskEtkinlestirReq
	if err := govdeCoz(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, govdeHata(err))
		return
	}
	if !req.Onayla {
		httpx.WriteError(w, http.StatusBadRequest, `onay gerekli: {"onayla":true} gönderin (önizleme için GET /bayi-kaynak/disk-kotasi)`)
		return
	}
	asim, _ := kaynakAsimdakiler(r.Context(), h.DB)
	if len(asim) > 0 && !req.AsimKabul {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"hata":             "kullanımı disk havuzunu zaten aşan bayiler var; sınır açılınca bu bayilerin hostingleri yazamaz hale gelir. Bilerek devam etmek için asim_kabul:true gönderin.",
			"asimdaki_bayiler": asim,
		})
		return
	}
	uid, kul := middleware.Aktor(r)
	s, err := kaynakDiskEtkinlestir(r.Context(), true)
	s.AsimdakiBayiler = asim
	if err != nil {
		httpx.Denetim(h.DB, r, uid, kul, "bayi.disk_kotasi_etkinlestir", "", err.Error(), 0, false)
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"hata": err.Error(), "sonuc": s})
		return
	}
	httpx.Denetim(h.DB, r, uid, kul, "bayi.disk_kotasi_etkinlestir", "", s.Komut, 0, true)
	httpx.WriteJSON(w, http.StatusOK, s)
}

// bayiKendi: bayi uçları için oturumdaki bayi kimliği; yönetici/bilinmeyen → 400.
func bayiKendi(w http.ResponseWriter, r *http.Request) (int64, bool) {
	rid := middleware.ResellerIDFrom(r)
	if rid <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bu bilgi yalnız bayi hesapları içindir")
		return 0, false
	}
	return rid, true
}

// GET /reseller/kaynak — bayinin KENDİ havuzu.
func (h *Handlers) KaynakBen(w http.ResponseWriter, r *http.Request) {
	rid, ok := bayiKendi(w, r)
	if !ok {
		return
	}
	d, err := bayikaynak.Durumu(r.Context(), h.DB, rid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "havuz durumu okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d.BayiGorunumu())
}

// GET /reseller/kaynak/kullanim?aralik= — bayinin KENDİ zaman serisi.
func (h *Handlers) KaynakBenKullanim(w http.ResponseWriter, r *http.Request) {
	rid, ok := bayiKendi(w, r)
	if !ok {
		return
	}
	h.kaynakSeriYaz(w, r, rid)
}
