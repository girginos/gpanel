// Plan atama + kaynak limit yeniden uygulama.
package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

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
	// Plan var mı doğrula (+ reseller ise plan sahipligi: kendi plani veya global)
	rid := middleware.ResellerIDFrom(r)
	if req.PlanID != nil {
		var planRid int64
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT reseller_id FROM service_plans WHERE id=?`, *req.PlanID).Scan(&planRid); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
		if rid > 0 && planRid != 0 && planRid != rid {
			httpx.WriteError(w, http.StatusForbidden, "bu plana erişiminiz yok")
			return
		}
		// Reseller: yeni planin disk kotasi bayi havuzunu asmasin (mevcut domainin
		// kotasi dusulerek hesaplanir → yukseltme/dusurme dogru degerlendirilir).
		if rid > 0 {
			if err := h.resellerPlanDegisimKota(r.Context(), rid, id, *req.PlanID); err != nil {
				httpx.WriteError(w, http.StatusForbidden, err.Error())
				return
			}
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
		// Plan degisti → WAF plan varsayilani da degismis olabilir; vhost'u WAF ile yeniden
		// render et (domain override yoksa yeni planin WAF varsayilanini devralir).
		if err := provisioner.WAFUygula(h.DB, did); err != nil {
			log.Printf("waf apply (plan degisimi) domain=%d: %v", did, err)
		}
	}(id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "plan_id": req.PlanID})
}

// resellerPlanDegisimKota: mevcut hosting'in planini degistirirken bayi disk havuzunu
// dogrular. Mevcut planin kotasi havuzdan DUSULUR (ayni hesap iki kez sayilmaz) →
// yukseltme reddedilebilir, dusurme her zaman gecer.
func (h *Handlers) resellerPlanDegisimKota(ctx context.Context, rid, domainID, yeniPlanID int64) error {
	var maxDiskMB int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_disk_mb FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxDiskMB); err != nil {
		return fmt.Errorf("bayi kaydı bulunamadı")
	}
	if maxDiskMB <= 0 {
		return nil // limitsiz
	}
	var yeniKota, eskiKota, toplam int64
	_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(disk_kota_mb,0) FROM service_plans WHERE id=?`, yeniPlanID).Scan(&yeniKota)
	_ = h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(p.disk_kota_mb,0) FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.id=?`,
		domainID).Scan(&eskiKota)
	_ = h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(COALESCE(p.disk_kota_mb,0)),0) FROM domains d
		   LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.reseller_id=?`, rid).Scan(&toplam)
	if toplam-eskiKota+yeniKota > maxDiskMB {
		return fmt.Errorf("disk kotanız yetersiz (kullanılan %d MB - mevcut %d MB + yeni %d MB > limit %d MB)",
			toplam, eskiKota, yeniKota, maxDiskMB)
	}
	return nil
}

// OzelPlan: POST /domains/{id}/ozel-plan — TEK TIKLA bu hosting'e ozel plan.
// Mevcut plani (yoksa varsayilani) KOPYALAR, "<alan_adi> — Özel" adiyla kaydeder
// ve hosting'e atar. Boylece tek hesabin limitleri digerlerini etkilemeden
// serbestce duzenlenebilir (Hosting Planlari sayfasindan).
func (h *Handlers) OzelPlan(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid := middleware.ResellerIDFrom(r)

	var alanAdi string
	var mevcutPlan sql.NullInt64
	var domRid int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, plan_id, reseller_id FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &mevcutPlan, &domRid); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	kaynak := mevcutPlan
	if !kaynak.Valid || kaynak.Int64 == 0 {
		var def int64
		if e := h.DB.QueryRowContext(r.Context(),
			`SELECT id FROM service_plans WHERE varsayilan=1 AND reseller_id IN (?,0) ORDER BY reseller_id DESC, id LIMIT 1`,
			domRid).Scan(&def); e == nil {
			kaynak = sql.NullInt64{Int64: def, Valid: true}
		}
	}
	if !kaynak.Valid || kaynak.Int64 == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kopyalanacak plan bulunamadı; önce bir hosting planı oluşturun")
		return
	}
	// Ozel plan sahibi: reseller ise kendisi, admin ise domainin bayisi (yoksa global)
	sahip := domRid
	if rid > 0 {
		sahip = rid
	}
	// 🔴 Idempotent: aktif plan ZATEN bu hostinge ozel bir plansa (ve baska hosting
	// kullanmiyorsa) yenisini URETME — kullanici butona ikinci kez bastiginda
	// "<alan> — Özel 1234" cogaltmasi olusuyordu. Mevcut ozel plani dondur.
	if kaynak.Valid && mevcutPlan.Valid && kaynak.Int64 == mevcutPlan.Int64 {
		var mevcutAd string
		var kullananAdet int
		_ = h.DB.QueryRowContext(r.Context(), `SELECT ad FROM service_plans WHERE id=?`, kaynak.Int64).Scan(&mevcutAd)
		_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM domains WHERE plan_id=?`, kaynak.Int64).Scan(&kullananAdet)
		if strings.HasPrefix(mevcutAd, alanAdi+" — Özel") && kullananAdet <= 1 {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"ok": true, "plan_id": kaynak.Int64, "ad": mevcutAd, "zaten_ozel": true,
			})
			return
		}
	}
	ad := alanAdi + " — Özel"
	// Ayni isim varsa sirala (idempotent olmayan cakisma engeli)
	var varMi int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM service_plans WHERE ad=?`, ad).Scan(&varMi)
	if varMi > 0 {
		ad = fmt.Sprintf("%s — Özel %d", alanAdi, time.Now().Unix()%10000)
	}
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO service_plans (ad, aciklama, disk_kota_mb, trafik_kota_mb, max_domain, max_db, max_email, max_ftp,
		  cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti, pm_max_children,
		  io_read_mbps, io_write_mbps, io_read_iops, io_write_iops,
		  db_max_queries_per_hour, db_max_updates_per_hour, db_max_query_seconds,
		  php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler,
		  waf_enabled, waf_mode, waf_paranoia, varsayilan, reseller_id)
		SELECT ?, CONCAT('Yalnız ', ?, ' için özel plan'), disk_kota_mb, trafik_kota_mb, max_domain, max_db, max_email, max_ftp,
		  cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti, pm_max_children,
		  io_read_mbps, io_write_mbps, io_read_iops, io_write_iops,
		  db_max_queries_per_hour, db_max_updates_per_hour, db_max_query_seconds,
		  php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler,
		  waf_enabled, waf_mode, waf_paranoia, 0, ?
		FROM service_plans WHERE id=?`, ad, alanAdi, sahip, kaynak.Int64)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "özel plan oluşturulamadı: "+err.Error())
		return
	}
	yeniID, _ := res.LastInsertId()
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE domains SET plan_id=? WHERE id=?`, yeniID, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "plan atanamadı: "+err.Error())
		return
	}
	// Kaynak limitlerini uygula (arka plan, kendi context'i — SetPlan ile ayni desen)
	go func(did int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := kaynaklimit.UygulaHepsi(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply (ozel-plan) domain=%d: %v", did, err)
		}
	}(id)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "plan_id": yeniID, "ad": ad})
}
