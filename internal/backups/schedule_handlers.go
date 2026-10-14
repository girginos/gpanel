package backups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// ScheduleYanit: GET backup-schedule. Plan alanları + görünümün ihtiyaç duyduğu
// manuel yedek durumu. Duzenlenebilir YALNIZ gerçek yöneticide true (UI plan
// seçicisini yalnız o zaman çizer; sunucu kapısı yine SetSchedule'dadır).
type ScheduleYanit struct {
	Schedule
	Duzenlenebilir bool   `json:"duzenlenebilir"`
	SonSistemYedek string `json:"son_sistem_yedek,omitempty"` // "YYYY-MM-DD HH:MM" (yerel)
	Manuel         struct {
		BeklemeSn   int64 `json:"bekleme_sn"`   // yeni manuel yedek için kalan (0 = serbest)
		BeklemeSaat int   `json:"bekleme_saat"` // ManuelBeklemeSuresi (saat)
		Eszamanli   int   `json:"eszamanli"`    // ManuelEszamanliSinir
		Calisan     int   `json:"calisan"`      // sunucu genelinde şu an çalışan manuel yedek
		Sira        int   `json:"sira"`         // bu hesap sıradaysa sıra no
	} `json:"manuel"`
}

// GET /domains/{id}/backup-schedule
func (h *Handlers) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id, _, _, _, err := h.lookupDomain(r)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var y ScheduleYanit
	var last sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(backup_freq,'none'), COALESCE(backup_hour,3), COALESCE(backup_retention,7),
		        DATE_FORMAT(last_backup_at,'%Y-%m-%dT%H:%i:%sZ')
		 FROM domains WHERE id=?`, id).
		Scan(&y.Freq, &y.Hour, &y.Retention, &last); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if last.Valid {
		y.LastBackupAt = last.String
	}
	rol := rolCoz(r)
	y.Duzenlenebilir = planDuzenleyebilir(rol)
	var son sql.NullString
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT DATE_FORMAT(MAX(created_at),'%Y-%m-%d %H:%i') FROM backups
		 WHERE domain_id=? AND tip<>'kurtarma' AND (tip IN ('oto','planli') OR job_id IS NOT NULL)`, id).Scan(&son)
	y.SonSistemYedek = son.String
	if !beklemedenMuaf(rol) {
		y.Manuel.BeklemeSn = int64(manuelBeklemeKalan(r.Context(), h.DB, id) / time.Second)
	}
	y.Manuel.BeklemeSaat = int(ManuelBeklemeSuresi / time.Hour)
	y.Manuel.Eszamanli = ManuelEszamanliSinir
	y.Manuel.Calisan = manuelKuyrugu.Calisan()
	y.Manuel.Sira = manuelKuyrugu.Sira(id)
	httpx.WriteJSON(w, http.StatusOK, y)
}

// PUT /domains/{id}/backup-schedule — YALNIZ gerçek yönetici.
//
// 🔴 Kullanıcı kararı (2026-10-03): müşteri yedeği yalnız manuel. Müşteri, bayi
// ve müşteri-ADINA oturum (aktör yönetici olsa bile) plan yazamaz: çok inode'lu
// hesapta günlük/haftalık tam arşiv disk IO'yu sömürür. Kapı lookupDomain'den
// ÖNCE: yetkisiz çağıran domainin varlığını da öğrenmez.
func (h *Handlers) SetSchedule(w http.ResponseWriter, r *http.Request) {
	if !planDuzenleyebilir(rolCoz(r)) {
		httpx.WriteError(w, http.StatusForbidden,
			"Otomatik yedek planı yalnız sunucu yöneticisi tarafından yönetilir. Hesabınız sistem tarafından yedeklenir; ek yedek için 'Şimdi Yedekle'yi kullanın.")
		return
	}
	id, alanAdi, _, demo, err := h.lookupDomain(r)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin yedek planı değiştirilemez")
		return
	}
	var s Schedule
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if !gecerliFreq(s.Freq) {
		httpx.WriteError(w, http.StatusBadRequest, "freq: none|daily|weekly")
		return
	}
	if s.Hour < 0 || s.Hour > 23 {
		httpx.WriteError(w, http.StatusBadRequest, "hour: 0-23")
		return
	}
	if s.Retention < 1 {
		s.Retention = 1
	}
	if s.Retention > 90 {
		s.Retention = 90
	}
	q := `UPDATE domains SET backup_freq=?, backup_hour=?, backup_retention=?, backup_plan_kaynak='kok' WHERE id=?`
	if !planKaynakKolonuVar(r.Context(), h.DB) {
		// 0114 koşmamış: plan yine yazılır (kaynak işaretsiz kalır, semaDogrula uyarır).
		q = `UPDATE domains SET backup_freq=?, backup_hour=?, backup_retention=? WHERE id=?`
	}
	uid, kul := middleware.Aktor(r)
	detay := fmt.Sprintf("siklik=%s saat=%d saklama=%d", s.Freq, s.Hour, s.Retention)
	if _, err := h.DB.ExecContext(r.Context(), q, s.Freq, s.Hour, s.Retention, id); err != nil {
		httpx.DenetimDomain(h.DB, r, uid, kul, "yedek.zamanlama", alanAdi, detay, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	httpx.DenetimDomain(h.DB, r, uid, kul, "yedek.zamanlama", alanAdi, detay, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": s})
}

// POST /admin/backups/tick — scheduler'ı manuel tetikle (admin only).
// Şu saat slot'unda due olan tüm domainler yedeklenir + retention uygulanır.
func (h *Handlers) TickNow(w http.ResponseWriter, r *http.Request) {
	go TickOnce(h.DB)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "tick": "started"})
}

// VerifyNow: POST /admin/backups/verify — at-rest butunluk taramasini SIMDI calistirir
// (operator on-demand + otomatik 04:00 taramasiyla ayni is). Her domainin en yeni yedegini
// re-hash edip saklanan sha256 ile karsilastirir; bozulani dogrulama=bozuk + KRITIK bildirim.
func (h *Handlers) VerifyNow(w http.ResponseWriter, r *http.Request) {
	kontrol, bozuk := verifyYedekBozulma(h.DB)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "kontrol": kontrol, "bozuk": bozuk})
}
