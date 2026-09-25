package plans

// kapsam.go — bayi (reseller) plan KAPSAMI: kuralın TEK kaynağı.
//
// Plan modeli:
//
//	service_plans.reseller_id  planın SAHİBİ (0 = kök / panel sahibi)
//	service_plans.domain_id    NULL  → katalog planı ("Hizmet Planları"nda listelenir)
//	                           dolu  → yalnız o hostinge ÖZEL plan (OzelPlan "tek tıkla
//	                                   özel plan", sahiplik devri)
//	domains.plan_id            hostingin bağlı olduğu plan — limitler buradan CANLI okunur
//	                           (kaynaklimit, MySQL governor, WAF)
//	domains.reseller_id        hostingin sahibi bayi (0 = kök)
//	reseller_plans             bayi PAKETLERİ (ayrı tablo; burada yalnız posta tavanı okunur)
//
// Bayi (rid>0):
//
//	GÖRÜR    kendi katalog planları               (reseller_id=rid, domain_id NULL) → TAM
//	         kökün GLOBAL katalogu                (reseller_id=0,   domain_id NULL) → YALNIZ seçim özeti
//	         KENDİ hostinglerine özel planlar     (domain_id ∈ bayinin hostingleri) → sahibiyse TAM
//	GÖRMEZ   başka bayinin planları; kökün / başka bayinin hostingine özel planlar (GL-04)
//	DÜZENLER yalnız sahibi olduğu ve gördüğü planı; plan kapsamı dışındaki bir hostinge
//	/ SİLER  bağlıysa (devir ya da yönetici ataması artığı) DÜZENLEYEMEZ (GL-03)
//	BAĞLAR   gördüğü katalog planını kendi hostingine; özel planı yalnız ait olduğu hostinge
//	SAYAR    yalnız kendi hostinglerini (domain_sayisi, silme engeli mesajı — GL-05)
//
// Yönetici (rid=0) kısıtsızdır — davranışı değişmedi.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/httpx"
)

// bayiGorurKosul — service_plans üzerinde (takma adsız) bayi görünürlük koşulu.
// List/Get/Update/Delete/DomainlerAra/BaglanabilirMi HEPSİ bunu kullanır.
func bayiGorurKosul(rid int64) (string, []any) {
	return `((domain_id IS NULL AND reseller_id IN (0, ?)) OR domain_id IN (SELECT id FROM domains WHERE reseller_id = ?))`,
		[]any{rid, rid}
}

// PlanOzet — bayiye SAHİBİ OLMADIĞI (seçebildiği) plan için dönen görünüm: seçim
// için gereken ad + temel kotalar. İç ayrıntılar (açıklama, nginx ek direktifleri,
// WAF, IO/DB ayarları, process/FPM/bağlantı sınırları, varsayılan işareti) YOK (GL-06).
type PlanOzet struct {
	ID                int64  `json:"id"`
	Ad                string `json:"ad"`
	DiskKotaMB        int    `json:"disk_kota_mb"`
	TrafikKotaMB      int    `json:"trafik_kota_mb"`
	MaxDomain         int    `json:"max_domain"`
	MaxDB             int    `json:"max_db"`
	MaxEmail          int    `json:"max_email"`
	SaatlikMailLimiti int    `json:"saatlik_mail_limiti"`
	MailKutuKotaMB    int    `json:"mail_kutu_kota_mb"`
	MaxFTP            int    `json:"max_ftp"`
	CPUYuzde          int    `json:"cpu_yuzde"`
	RAMMB             int    `json:"ram_mb"`
	PHPSurum          string `json:"php_surum"`
	DomainID          *int64 `json:"domain_id,omitempty"`
	Global            bool   `json:"global"`
	Duzenlenebilir    bool   `json:"duzenlenebilir"` // her zaman false
	Ozet              bool   `json:"ozet"`           // her zaman true
}

// gorunum — planın çağırana dönecek hâli. Yönetici ve planın sahibi bayi TAM
// planı (düzenlenebilir) alır; bayi sahibi olmadığı planın yalnız özetini alır.
func (p Plan) gorunum(rid int64) any {
	p.Global = p.resellerID == 0 && p.DomainID == nil
	if rid <= 0 || p.resellerID == rid {
		p.Duzenlenebilir = true
		return p
	}
	return PlanOzet{
		ID: p.ID, Ad: p.Ad,
		DiskKotaMB: p.DiskKotaMB, TrafikKotaMB: p.TrafikKotaMB,
		MaxDomain: p.MaxDomain, MaxDB: p.MaxDB, MaxEmail: p.MaxEmail,
		SaatlikMailLimiti: p.SaatlikMailLimiti, MailKutuKotaMB: p.MailKutuKotaMB, MaxFTP: p.MaxFTP,
		CPUYuzde: p.CPUYuzde, RAMMB: p.RAMMB, PHPSurum: p.PHPSurum,
		DomainID: p.DomainID, Global: p.Global, Ozet: true,
	}
}

// bayiPlan — tek bir planın bayiye göre durumu. Görünmeyen plan ile hiç olmayan
// plan AYIRT EDİLMEZ (kimlik taraması varlık sızdırmasın).
type bayiPlan struct {
	gorur    bool
	sahip    bool
	domainID sql.NullInt64
}

func bayiPlanOku(ctx context.Context, db *sql.DB, rid, planID int64) (bayiPlan, error) {
	kosul, args := bayiGorurKosul(rid)
	var sahip int64
	var dom sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT reseller_id, domain_id FROM service_plans WHERE id=? AND `+kosul,
		append([]any{planID}, args...)...).Scan(&sahip, &dom)
	if errors.Is(err, sql.ErrNoRows) {
		return bayiPlan{}, nil
	}
	if err != nil {
		return bayiPlan{}, err
	}
	return bayiPlan{gorur: true, sahip: sahip == rid, domainID: dom}, nil
}

// bayiYazmaKapisi — bayi bu planı DEĞİŞTİREBİLİR mi (Update)? (0,"") = evet.
// Sahip olmak yetmez: plan bayinin kapsamı dışındaki bir hostinge bağlıysa
// (devirden önce kalan ya da yöneticinin atadığı), plan düzenlemesi o yabancı
// hostingin CPU/RAM/process/yavaş-sorgu limitlerini değiştirir (GL-03).
func (h *Handlers) bayiYazmaKapisi(ctx context.Context, rid, planID int64, redMesaji string) (int, string) {
	bp, err := bayiPlanOku(ctx, h.DB, rid, planID)
	if err != nil {
		log.Printf("plans: bayi kapsam okunamadı (plan=%d bayi=%d): %v", planID, rid, err)
		return http.StatusInternalServerError, "plan okunamadı"
	}
	if !bp.gorur || !bp.sahip {
		return http.StatusForbidden, redMesaji
	}
	var yabanci int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM domains WHERE plan_id=? AND reseller_id<>?`, planID, rid).Scan(&yabanci); err != nil {
		log.Printf("plans: yabancı kullanım okunamadı (plan=%d): %v", planID, err)
		return http.StatusInternalServerError, "plan okunamadı"
	}
	if yabanci > 0 {
		return http.StatusConflict, "bu plan kapsamınız dışındaki bir hostinge bağlı olduğu için düzenlenemez — yöneticinize başvurun"
	}
	return 0, ""
}

// ErrPlanKapsamDisi / ErrOzelPlanBaskaHosting — BaglanabilirMi ret nedenleri
// (mesajları doğrudan kullanıcıya gösterilir; yabancı veri içermez).
var (
	ErrPlanKapsamDisi       = errors.New("bu plana erişiminiz yok")
	ErrOzelPlanBaskaHosting = errors.New("bu özel plan yalnız ait olduğu hostinge atanabilir")
)

// BaglanabilirMi — bayi (rid>0) planID'yi domainID hostingine BAĞLAYABİLİR mi?
// domainID=0: henüz oluşturulmamış hosting (POST /domains) — özel plan bağlanamaz.
// Yönetici (rid<=0) → nil (kısıtsız). Hostingin bayiye ait olduğunu ÇAĞIRAN
// doğrular (SahipYonetim / Create'te sahip=rid). DB hatasında KAPALI (ret).
func BaglanabilirMi(ctx context.Context, db *sql.DB, rid, planID, domainID int64) error {
	if rid <= 0 {
		return nil
	}
	bp, err := bayiPlanOku(ctx, db, rid, planID)
	if err != nil {
		log.Printf("plans: bağlama kapsamı okunamadı (plan=%d bayi=%d): %v", planID, rid, err)
		return ErrPlanKapsamDisi
	}
	if !bp.gorur {
		return ErrPlanKapsamDisi
	}
	if bp.domainID.Valid && bp.domainID.Int64 != domainID {
		return ErrOzelPlanBaskaHosting
	}
	return nil
}

// bayiHata — bayiye ham SQL hatası (tablo/sütun/anahtar adları, başka kiracının
// plan adı içeren "Duplicate entry") DÖNMEZ. Yönetici yanıtı değişmedi.
func bayiHata(w http.ResponseWriter, rid int64, err error) {
	if rid <= 0 {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tekrarAnahtar(err) {
		// service_plans.ad sunucu genelinde UNIQUE — adın kimde olduğu söylenmez.
		httpx.WriteError(w, http.StatusConflict, "bu plan adı kullanılamıyor — farklı bir ad seçin")
		return
	}
	log.Printf("plans: bayi %d isteği DB hatası: %v", rid, err)
	httpx.WriteError(w, http.StatusInternalServerError, "işlem tamamlanamadı")
}

func tekrarAnahtar(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// ── Sahiplik devri ──────────────────────────────────────────────────────────

// DevirPlanTasi — sahiplik devrinde (toplu/devir.go devirTx, AYNI işlem içinde)
// hostingin planını YENİ SAHİBİN kapsamına alır (GL-03). Eskiden devir yalnız
// domains.reseller_id'yi değiştiriyordu; hosting eski bayinin planında kalıyor,
// eski bayi planını düzenleyerek yeni sahibin sitesinin CPU/RAM/process/yavaş-
// sorgu KILL eşiğini değiştirebiliyordu (governor bu değeri canlı okur).
//
// Hostingin efektif limitleri DEĞİŞMEZ; eski sahibin planı ve diğer hostingleri
// ETKİLENMEZ (veri/hizmet kaybı yok):
//
//	plan yok / kökün global katalog planı / zaten yeni sahibin → dokunulmaz
//	yalnız BU hostinge özel ve başka hosting kullanmıyor        → sahipliği taşınır
//	diğer her durum (eski sahibin katalog planı, paylaşılan
//	özel plan, başka hostingin özel planı, kökün özel planı)    → birebir kopyası bu hostinge
//	                                                               ÖZEL olarak yeni sahip adına
//	                                                               açılır, hosting ona bağlanır
//
// Kopyanın adı "<alan> — Özel" (OzelPlan ile aynı kalıp): eski bayinin plan adı ve
// açıklaması yeni sahibe geçmez. Dönen metin yöneticinin devir özetine yazılır
// ("" = değişiklik yok). Hata → devir DÜŞER (erişim kesilemeyen devir yapılmaz).
func DevirPlanTasi(ctx context.Context, tx *sql.Tx, did, yeniSahip int64) (string, error) {
	var planID sql.NullInt64
	var alanAdi string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id, alan_adi FROM domains WHERE id=?`, did).Scan(&planID, &alanAdi); err != nil {
		return "", fmt.Errorf("hosting okunamadı: %w", err)
	}
	if !planID.Valid || planID.Int64 <= 0 {
		return "", nil
	}
	var sahip int64
	var dom sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT reseller_id, domain_id FROM service_plans WHERE id=? FOR UPDATE`, planID.Int64).
		Scan(&sahip, &dom)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // sarkık plan_id: devredilecek plan yok
	}
	if err != nil {
		return "", fmt.Errorf("plan okunamadı: %w", err)
	}
	if sahip == yeniSahip || (sahip == 0 && !dom.Valid) {
		return "", nil
	}
	if dom.Valid && dom.Int64 == did {
		var baska int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE plan_id=? AND id<>?`, planID.Int64, did).Scan(&baska); err != nil {
			return "", fmt.Errorf("plan kullanımı okunamadı: %w", err)
		}
		if baska == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE service_plans SET reseller_id=?, varsayilan=0 WHERE id=?`, yeniSahip, planID.Int64); err != nil {
				return "", fmt.Errorf("özel plan sahipliği taşınamadı: %w", err)
			}
			return fmt.Sprintf("plan: hostinge özel plan #%d yeni sahibe taşındı (limitler aynı)", planID.Int64), nil
		}
	}
	yeniID, ad, err := ozelKopya(ctx, tx, planID.Int64, did, alanAdi, yeniSahip)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET plan_id=? WHERE id=?`, yeniID, did); err != nil {
		return "", fmt.Errorf("hosting kopya plana bağlanamadı: %w", err)
	}
	return fmt.Sprintf("plan: #%d eski sahipte kaldı; hosting birebir kopyası olan özel plana (#%d %q) geçti (limitler aynı)",
		planID.Int64, yeniID, ad), nil
}

var sutunAdi = regexp.MustCompile(`^[a-z0-9_]+$`)

// ozelKopya — kaynak planın TÜM sütunlarını (kimlik/ad/sahiplik hariç) kopyalar.
// Sütun listesi şemadan okunur: ileride eklenecek limit sütunu da kopyaya girer
// (sabit liste "birebir kopya"yı sessizce bozardı — OzelPlan posta limitlerini
// böyle kaybediyor).
func ozelKopya(ctx context.Context, tx *sql.Tx, kaynakID, did int64, alanAdi string, yeniSahip int64) (int64, string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'service_plans' ORDER BY ORDINAL_POSITION`)
	if err != nil {
		return 0, "", fmt.Errorf("plan şeması okunamadı: %w", err)
	}
	haric := map[string]bool{"id": true, "ad": true, "aciklama": true, "varsayilan": true,
		"reseller_id": true, "domain_id": true, "created_at": true, "updated_at": true}
	var sut []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return 0, "", fmt.Errorf("plan şeması okunamadı: %w", err)
		}
		if haric[c] {
			continue
		}
		if !sutunAdi.MatchString(c) {
			rows.Close()
			return 0, "", fmt.Errorf("beklenmeyen plan sütunu %q", c)
		}
		sut = append(sut, "`"+c+"`")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, "", fmt.Errorf("plan şeması okunamadı: %w", err)
	}
	if len(sut) == 0 {
		return 0, "", fmt.Errorf("plan şeması boş")
	}
	liste := strings.Join(sut, ", ")
	aciklama := kisaltRune("Yalnız "+alanAdi+" için özel plan", 255)
	for n := 1; n <= 50; n++ {
		ad := ozelPlanAdi(alanAdi, n)
		res, err := tx.ExecContext(ctx,
			"INSERT INTO service_plans (ad, aciklama, varsayilan, reseller_id, domain_id, "+liste+") "+
				"SELECT ?, ?, 0, ?, ?, "+liste+" FROM service_plans WHERE id=?",
			ad, aciklama, yeniSahip, did, kaynakID)
		if tekrarAnahtar(err) {
			continue // ad dolu (UNIQUE) — ifade geri alındı, işlem sürüyor; sıradaki ad
		}
		if err != nil {
			return 0, "", fmt.Errorf("plan kopyalanamadı: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil || id <= 0 {
			return 0, "", fmt.Errorf("kopya plan kimliği alınamadı: %v", err)
		}
		return id, ad, nil
	}
	return 0, "", fmt.Errorf("kopya plan için boş ad bulunamadı")
}

const ozelEk = " — Özel"

// ozelPlanAdi — "<alan> — Özel" / "<alan> — Özel N"; service_plans.ad VARCHAR(64).
func ozelPlanAdi(alan string, n int) string {
	ek := ozelEk
	if n > 1 {
		ek += " " + strconv.Itoa(n)
	}
	return kisaltRune(alan, 64-utf8.RuneCountInString(ek)) + ek
}

func kisaltRune(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
