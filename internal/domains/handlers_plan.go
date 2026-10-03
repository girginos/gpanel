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
	"time"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/kilit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"
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
		var planDomain sql.NullInt64
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT domain_id FROM service_plans WHERE id=?`, *req.PlanID).Scan(&planDomain); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
		// Bayi kapsamı TEK kaynaktan (plans/kapsam.go; yönetici kısıtsız). Eskiden
		// `reseller_id==0` her planı geçiriyordu: kökün başka bir hostinge ÖZEL planı
		// (sınırsız kotalar, kökün nginx direktifleri) bayinin hostingine
		// bağlanabiliyordu (GL-04); özel plan yalnız ait olduğu hostinge bağlanır.
		if err := plans.BaglanabilirMi(r.Context(), h.DB, rid, *req.PlanID, id); err != nil {
			httpx.WriteError(w, http.StatusForbidden, err.Error())
			return
		}
		// Eski "<alan> — Özel" kopyası (dönüştürülmemiş) YALNIZ kendi hostingine
		// bağlanır — yönetici dahil herkes için (başka hostingten geri bağlama yok).
		// Bayi kapsam kapısından SONRA: görünmeyen plan için ayrı ileti sızmasın.
		if planDomain.Valid && planDomain.Int64 != id {
			httpx.WriteError(w, http.StatusForbidden, plans.ErrOzelPlanBaskaHosting.Error())
			return
		}
		// Reseller: yeni planin disk kotasi bayi havuzunu asmasin (mevcut domainin
		// kotasi dusulerek hesaplanir → yukseltme/dusurme dogru degerlendirilir).
		if rid > 0 {
			// Bayi: hostingin FİİLİ disk kullanımı yeni planın kotasını aşıyorsa
			// geçiş reddedilir (aksi hâlde hesap anında kota aşımına düşerdi).
			if err := h.planKullanimSigar(r.Context(), id, *req.PlanID); err != nil {
				httpx.WriteError(w, http.StatusConflict, err.Error())
				return
			}
			// Kilit: iki paralel yukseltme ayni eski toplami okuyup kotayi asamasin.
			k := kilit.Bayi(rid)
			k.Lock()
			defer k.Unlock()
			if err := h.resellerPlanDegisimKota(r.Context(), rid, id, *req.PlanID); err != nil {
				httpx.WriteError(w, http.StatusForbidden, err.Error())
				return
			}
		}
	}
	// Domain var mı
	var sk, alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi FROM domains WHERE id=?`, id).Scan(&sk, &alanAdi); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		} else {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	// Eski plani (degisimden ONCE) not al — domaine-ozel ise sonra temizlenecek.
	var eskiPlan sql.NullInt64
	_ = h.DB.QueryRowContext(r.Context(), `SELECT plan_id FROM domains WHERE id=?`, id).Scan(&eskiPlan)

	// Güncelle
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET plan_id=? WHERE id=?`, req.PlanID, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}
	// 🔴 Sahipsiz kalan domaine-ozel plani SIL: hosting baska bir plana gectiginde
	// eski ozel kopya kimseye ait degil — katalogu ve DB'yi sismesin.
	if eskiPlan.Valid && (req.PlanID == nil || *req.PlanID != eskiPlan.Int64) {
		OzelPlanTemizle(h.DB, eskiPlan.Int64, id)
	}
	// Hostinge özel limitler (hosting_limit_ust) plan değişiminde KORUNUR (kullanıcı
	// kararı 2026-09-29) — burada dokunulmaz; efektif değer yeni plan ⊕ üst yazımdır.
	// Kaynak limitlerini yeniden uygula — arkaplanda + kendi context'i
	// (r.Context() HTTP request bitince iptal olur, cgroup yazımı yarıda kalır)
	go planSonrasiUygula(h.DB, id, alanAdi)
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "hosting.plan", alanAdi,
		fmt.Sprintf("yeni plan=%d", planNoLog(req.PlanID)), id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "plan_id": req.PlanID})
}

// resellerPlanDegisimKota: mevcut hosting'in planini degistirirken bayi disk havuzunu
// dogrular. Mevcut planin kotasi havuzdan DUSULUR (ayni hesap iki kez sayilmaz) →
// yukseltme reddedilebilir, dusurme her zaman gecer.
func (h *Handlers) resellerPlanDegisimKota(ctx context.Context, rid, domainID, yeniPlan int64) error {
	var maxDiskMB, maxTrafikMB int64
	var fazlaSatis int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_disk_mb, max_trafik_mb, fazla_satis FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDiskMB, &maxTrafikMB, &fazlaSatis); err != nil {
		return fmt.Errorf("bayi kaydı bulunamadı")
	}
	if fazlaSatis == 1 {
		return nil // "fazla satisa izinli" bayide taahhut kapisi atlanir (create ile ayni)
	}
	if maxDiskMB < 0 {
		maxDiskMB = 0
	}
	if maxTrafikMB < 0 {
		maxTrafikMB = 0
	}
	// Yeni EFEKTİF kota (yeni plan ⊕ korunan üst yazım) ve mevcut efektif kota.
	yeniDisk, yeniTrafik, err := yeniPlanEfektifKota(ctx, h.DB, domainID, yeniPlan)
	if err != nil {
		return fmt.Errorf("kota hesaplanamadı")
	}
	var eskiDisk, eskiTrafik int64
	_ = h.DB.QueryRowContext(ctx,
		`SELECT `+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+`, `+efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0)+`
		   FROM domains d`+efektiflimit.Join+`WHERE d.id=?`, domainID).
		Scan(&eskiDisk, &eskiTrafik)
	toplamDisk, toplamTrafik, err := BayiTaahhut(ctx, h.DB, rid)
	if err != nil {
		return fmt.Errorf("kota hesaplanamadı")
	}

	// 🔴 Sinir konvansiyonu CREATE yoluyla AYNI: ">=" (sinira ulasmak doludur).
	// Eskiden burada ">" vardi ve tam-esitlik create'te RED, plan degisiminde
	// KABUL ediliyordu — ayni aritmetik iki farkli cevap veriyordu.
	if maxDiskMB > 0 && yeniDisk > 0 {
		if (toplamDisk-eskiDisk)+yeniDisk >= maxDiskMB {
			return fmt.Errorf("disk kotanız yetersiz (kullanılan %d MB - mevcut %d MB + yeni %d MB, limit %d MB)",
				toplamDisk, eskiDisk, yeniDisk, maxDiskMB)
		}
	}
	// 🔴 TRAFIK KAPISI EKLENDI: eskiden plan degisimi trafik havuzunu HIC
	// kontrol etmiyordu; bayi kucuk planla domain acip sonra yukselterek
	// trafik kotasini tamamen atlayabiliyordu (create yolu reddederken).
	if maxTrafikMB > 0 && yeniTrafik > 0 {
		if (toplamTrafik-eskiTrafik)+yeniTrafik >= maxTrafikMB {
			return fmt.Errorf("trafik kotanız yetersiz (taahhüt %d MB - mevcut %d MB + yeni %d MB, limit %d MB)",
				toplamTrafik, eskiTrafik, yeniTrafik, maxTrafikMB)
		}
	}
	return nil
}

// OzelPlan — ESKİ uç (POST /domains/{id}/ozel-plan): artık katalog planı
// kopyalanmaz; hostinge özel limitler hosting üzerinde (GET/PUT/DELETE
// /domains/{id}/limitler). 410: önbellekteki eski arayüz katalog planını "özel"
// sanıp düzenlemesin (o plan başka hostingleri de etkilerdi).
func (h *Handlers) OzelPlan(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, http.StatusGone, "özel limitler artık hosting üzerinde düzenleniyor — sayfayı yenileyin")
}

// OzelPlanTemizle: verilen plan YALNIZ bu domaine ait (dönüştürülmemiş, ESKİ) ozel
// plansa (domain_id=did) ve artik hicbir domain kullanmiyorsa siler. Katalog
// planlarina DOKUNMAZ. Yeni kopya üretilmediği için yalnız açılış dönüşümünün
// (plans.OzelPlanlariDonustur) tabanı belirleyemediği eski kopyalarda iş görür.
func OzelPlanTemizle(db *sql.DB, planID, did int64) {
	if planID <= 0 {
		return
	}
	var planDomain sql.NullInt64
	if err := db.QueryRow(`SELECT domain_id FROM service_plans WHERE id=?`, planID).Scan(&planDomain); err != nil {
		return
	}
	if !planDomain.Valid || planDomain.Int64 != did {
		return // katalog plani ya da baska domainin ozel plani → dokunma
	}
	var kullanan int
	_ = db.QueryRow(`SELECT COUNT(*) FROM domains WHERE plan_id=?`, planID).Scan(&kullanan)
	if kullanan > 0 {
		return
	}
	if _, err := db.Exec(`DELETE FROM service_plans WHERE id=? AND domain_id=?`, planID, did); err == nil {
		log.Printf("ozel plan temizlendi: plan=%d (domain=%d artik kullanmiyor)", planID, did)
	}
}

// planKullanimSigar: hostingin fiili disk kullanımı (domains.boyut_kb) yeni planın
// disk kotasına sığıyor mu? Kota 0 = sınırsız → her zaman sığar.
func (h *Handlers) planKullanimSigar(ctx context.Context, domainID, planID int64) error {
	var boyutKB int64
	// Yeni planın EFEKTİF kotası (korunan üst yazım dahil).
	kotaMB, _, _ := yeniPlanEfektifKota(ctx, h.DB, domainID, planID)
	if kotaMB <= 0 {
		return nil
	}
	if err := h.DB.QueryRowContext(ctx, `SELECT COALESCE(boyut_kb,0) FROM domains WHERE id=?`, domainID).Scan(&boyutKB); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // hosting yok: SetPlan aşağıda 404 "domain bulunamadı" döner
		}
		return fmt.Errorf("disk kullanımı okunamadı: %w", err)
	}
	if boyutKB > kotaMB*1024 {
		return fmt.Errorf("mevcut disk kullanımı (%d MB) yeni planın disk sınırını (%d MB) aşıyor", boyutKB/1024, kotaMB)
	}
	return nil
}

// yeniPlanEfektifKota — hosting planID'ye geçerse efektif disk/trafik (MB) ne olur:
// yeni plan ⊕ korunan üst yazım ⊕ domains.disk_kota_mb mirası (efektiflimit sırası).
func yeniPlanEfektifKota(ctx context.Context, db *sql.DB, domainID, planID int64) (disk, trafik int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(`+efektiflimit.IfadePlanla("disk_kota_mb", "np")+`,0), COALESCE(`+efektiflimit.IfadePlanla("trafik_kota_mb", "np")+`,0)
		   FROM domains d LEFT JOIN hosting_limit_ust u ON u.domain_id=d.id LEFT JOIN service_plans np ON np.id=?
		  WHERE d.id=?`, planID, domainID).Scan(&disk, &trafik)
	if errors.Is(err, sql.ErrNoRows) {
		// Hosting yok (SetPlan aşağıda 404 döner): yalnız planın değeri.
		err = db.QueryRowContext(ctx, `SELECT COALESCE(disk_kota_mb,0), COALESCE(trafik_kota_mb,0) FROM service_plans WHERE id=?`, planID).
			Scan(&disk, &trafik)
	}
	return disk, trafik, err
}

// BayiTaahhut — bayinin tüm hostinglerinin EFEKTİF disk/trafik kotası toplamı (MB).
// Havuz kapıları (oluşturma, plan değişimi, özel limit) bunu kullanır: üst yazım
// sayılmazsa bayi hostinge özel limitle havuzu aşabilirdi.
func BayiTaahhut(ctx context.Context, db *sql.DB, rid int64) (disk, trafik int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+`),0),
		        COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0)+`),0)
		   FROM domains d`+efektiflimit.Join+`WHERE d.reseller_id=?`, rid).Scan(&disk, &trafik)
	return disk, trafik, err
}

// planSonrasiUygula — plan değişiminden sonra (arka planda, kendi context'i) limitleri,
// WAF'ı ve posta limitlerini yeniden uygular. Değişken: testler sistem yan etkisi
// (systemd/nginx) olmadan sahteyle değiştirir.
var planSonrasiUygula = func(db *sql.DB, did int64, ad string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := kaynaklimit.UygulaHepsi(ctx, db, did); err != nil {
		log.Printf("kaynaklimit apply domain=%d: %v", did, err)
	}
	// Plan degisti → WAF plan varsayilani da degismis olabilir; vhost'u WAF ile yeniden
	// render et (domain override yoksa yeni planin WAF varsayilanini devralir).
	if err := provisioner.WAFUygula(db, did); err != nil {
		log.Printf("waf apply (plan degisimi) domain=%d: %v", did, err)
	}
	// Plan posta limitlerini (max_email + saatlik) mail eklentisine senkronla (best-effort).
	mailKutuLimitSenkron(ctx, db, did, ad)
}
