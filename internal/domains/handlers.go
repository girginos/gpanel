package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/dns"
	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/kilit"
	"girginospanel/internal/kota"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/redis"

	"github.com/go-chi/chi/v5"
)

type Domain struct {
	ID              int64  `json:"id"`
	AlanAdi         string `json:"alan_adi"`
	PHPSurum        string `json:"php_surum"`
	SSL             bool   `json:"ssl"`
	SSLBitis        string `json:"ssl_bitis,omitempty"`
	Durum           string `json:"durum"`
	SistemKullanici string `json:"sistem_kullanici"`
	BoyutKB         int64  `json:"boyut_kb"`
	TrafikKB        int64  `json:"trafik_kb"`
	Olusturulma     string `json:"olusturulma"`
	IPv4            string `json:"ipv4"`
	FTPHost         string `json:"ftp_host"`
	FTPUser         string `json:"ftp_user"`
	DBHost          string `json:"db_host"`
	DBUser          string `json:"db_user"`
	DBAdi           string `json:"db_adi"`
	WebRoot         string `json:"web_root"`
	IsDemo          bool   `json:"is_demo"`
	Notlar          string `json:"notlar,omitempty"`
	PlanID          *int64 `json:"plan_id,omitempty"`
	PlanAd          string `json:"plan_ad,omitempty"`
	SshErisim       bool   `json:"ssh_erisim"`
	Askida          bool   `json:"askida"`
	// Sahip: domaini kimin actigi. reseller_id=0 -> panel sahibi (admin), >0 -> bayi.
	SahipAd    string `json:"sahip_ad,omitempty"`
	SahipTur   string `json:"sahip_tur,omitempty"` // "admin" | "bayi"
	ResellerID int64  `json:"reseller_id"`
}

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

const selectAll = `SELECT d.id, d.alan_adi, d.sistem_kullanici, d.php_surum, d.ssl_aktif,
  COALESCE(DATE_FORMAT(d.ssl_bitis,'%Y-%m-%d'),''), d.durum, d.ipv4, d.ftp_host, d.ftp_user,
  d.db_host, d.db_user, d.db_adi, d.web_root, d.boyut_kb, d.trafik_kb, d.is_demo,
  COALESCE(d.notlar,''), DATE_FORMAT(d.olusturulma,'%Y-%m-%d'),
  d.plan_id, COALESCE(p.ad,''), d.ssh_erisim, COALESCE(d.askida,0),
  CASE WHEN CHAR_LENGTH(TRIM(COALESCE(u.full_name,'')))>=2 THEN TRIM(u.full_name) ELSE COALESCE(NULLIF(u.username,''),'') END, COALESCE(d.reseller_id,0)
  FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
  LEFT JOIN users u ON u.id = d.reseller_id`

// kokAdi: panel sahibinin gorunen adi (users'taki ilk admin). Onbellege alinir —
// domain listesinde satir basina sorgu atmanin anlami yok.
var (
	kokAdiOnce sync.Once
	kokAdiVal  string
	KokDB      *sql.DB // main() tarafindan set edilir
)

func kokAdi() string {
	kokAdiOnce.Do(func() {
		if KokDB == nil {
			return
		}
		_ = KokDB.QueryRow(
			`SELECT COALESCE(NULLIF(full_name,''), username, '') FROM users WHERE role='admin' ORDER BY id LIMIT 1`).
			Scan(&kokAdiVal)
	})
	return kokAdiVal
}

func scan(rs interface{ Scan(...any) error }) (Domain, error) {
	var d Domain
	var ssl, demo, sshE, askida int
	var planID sql.NullInt64
	err := rs.Scan(&d.ID, &d.AlanAdi, &d.SistemKullanici, &d.PHPSurum, &ssl,
		&d.SSLBitis, &d.Durum, &d.IPv4, &d.FTPHost, &d.FTPUser,
		&d.DBHost, &d.DBUser, &d.DBAdi, &d.WebRoot, &d.BoyutKB, &d.TrafikKB, &demo,
		&d.Notlar, &d.Olusturulma,
		&planID, &d.PlanAd, &sshE, &askida,
		&d.SahipAd, &d.ResellerID)
	// Kok (reseller_id=0): users tablosunda admin satiri OLMAYABILIR (kurulum
	// varyanti) — sahip adini sorguya baglamak yerine burada cozuyoruz.
	d.SahipTur = "admin"
	if d.ResellerID > 0 {
		d.SahipTur = "bayi"
		if d.SahipAd == "" {
			d.SahipAd = "#" + strconv.FormatInt(d.ResellerID, 10) // silinmis bayi
		}
	} else if d.SahipAd = kokAdi(); d.SahipAd == "" {
		d.SahipAd = "Panel Sahibi"
	}
	d.SSL = ssl == 1
	d.IsDemo = demo == 1
	d.SshErisim = sshE == 1
	d.Askida = askida == 1
	if planID.Valid {
		v := planID.Int64
		d.PlanID = &v
	}
	return d, err
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	// Reseller kapsami: yalniz kendi hosting hesaplari. Admin: hepsi.
	sorgu := selectAll + " ORDER BY d.id DESC"
	args := []any{}
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		sorgu = selectAll + " WHERE d.reseller_id=? ORDER BY d.id DESC"
		args = append(args, rid)
	}
	rows, err := h.DB.QueryContext(r.Context(), sorgu, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı hatası: "+err.Error())
		return
	}
	defer rows.Close()
	out := make([]Domain, 0)
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
			return
		}
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE d.id=?", id)
	d, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

type createReq struct {
	AlanAdi    string `json:"alan_adi"`
	PHPSurum   string `json:"php_surum"`
	CustomerID *int64 `json:"customer_id,omitempty"`
	PlanID     *int64 `json:"plan_id,omitempty"`
}

type createResp struct {
	Domain
	OluşturulanParolalar struct {
		FTP string `json:"ftp"`
		DB  string `json:"db"`
	} `json:"olusturulan_parolalar"`
}

// resellerKotaKontrol: reseller'in toplam kotalarini (max_domain / max_disk_mb)
// yeni hosting olusturmadan ONCE dogrular. 0 = limitsiz.
// Disk: mevcut hosting'lerin PLAN disk kotalari toplami + yeni planin kotasi
// (taahhut-bazli; asiri-satisi onler).
// bayiKilitleri: kota kontrolu ile INSERT arasindaki TOCTOU yarisini kapatir.
// Iki paralel istek ayni bayide COUNT okuyup ikisi de gecerse limit asiliyordu
// (max_domain=1 iken 2 hosting acildi). Panel tek-node oldugu icin surec-ici
// kilit yeterli; kilit bayi BAZINDA, farkli bayiler birbirini beklemez.
// bayiKilidi: ortak kilit paketine devreder (aski zinciri/plan degisimi/silme
// ayni kilidi kullanir → islemler birbirini beklemek zorunda).
func bayiKilidi(rid int64) *sync.Mutex { return kilit.Bayi(rid) }

func (h *Handlers) resellerKotaKontrol(ctx context.Context, rid int64, planID *int64) error {
	var maxDomain int
	var maxDiskMB int64
	var durum string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_domain, max_disk_mb, status FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDomain, &maxDiskMB, &durum); err != nil {
		return fmt.Errorf("bayi kaydı bulunamadı")
	}
	// 🔴 Askiya alinmis bayi hosting ACAMAZ. Aksi halde aski islemiyle es zamanli
	// gelen istek, askidan MUAF (askida=0) canli bir site dogurup kaliyordu.
	if durum != "active" {
		return fmt.Errorf("bayi hesabı askıda — yeni hosting açılamaz")
	}
	// Negatif limit "sinirsiz" sayilmasin (eski kayitlar/elle mudahale).
	if maxDomain < 0 {
		maxDomain = 0
	}
	if maxDiskMB < 0 {
		maxDiskMB = 0
	}
	if maxDomain > 0 {
		var adet int
		_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE reseller_id=?`, rid).Scan(&adet)
		if adet >= maxDomain {
			return fmt.Errorf("hosting hesabı limitiniz dolu (%d/%d)", adet, maxDomain)
		}
	}
	// 🔴 Fazla satma ilkesi: bayi "fazla satisa izinli" ise TAAHHUT (plan kotalari
	// toplami) kontrolu ATLANIR — Plesk "oversell allowed" davranisi. Fiili kullanim
	// kapisi (asagida) her durumda calisir; boylece sunucu yine korunur.
	var fazlaSatis int
	_ = h.DB.QueryRowContext(ctx, `SELECT fazla_satis FROM users WHERE id=? AND role='reseller'`, rid).Scan(&fazlaSatis)

	// Trafik havuzu (taahhut): mevcut planlarin trafik kotalari + yeni plan
	if planID != nil && fazlaSatis == 0 {
		var maxTrafikMB int64
		_ = h.DB.QueryRowContext(ctx, `SELECT max_trafik_mb FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxTrafikMB)
		if maxTrafikMB > 0 {
			var yeniTr, mevcutTr int64
			_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(trafik_kota_mb,0) FROM service_plans WHERE id=?`, *planID).Scan(&yeniTr)
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(COALESCE(p.trafik_kota_mb,0)),0) FROM domains d
				   LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.reseller_id=?`, rid).Scan(&mevcutTr)
			if mevcutTr+yeniTr >= maxTrafikMB && yeniTr > 0 {
				return fmt.Errorf("trafik kotanız yetersiz (taahhüt %d MB + yeni %d MB > limit %d MB)", mevcutTr, yeniTr, maxTrafikMB)
			}
		}
	}
	// Gercek kullanim: bayinin hosting hesaplarinin FIILI disk toplami limiti astiysa
	// yeni hesap acilamaz (taahhut kontrolunden bagimsiz ikinci kapi).
	if maxDiskMB > 0 {
		var kullanimKB int64
		_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=?`, rid).Scan(&kullanimKB)
		if kullanimKB/1024 >= maxDiskMB { // sinira ULASMAK doludur (tum kapilarda ayni kural)
			return fmt.Errorf("disk kullanımınız limitte (%d MB / %d MB)", kullanimKB/1024, maxDiskMB)
		}
	}
	if maxDiskMB > 0 && planID != nil && fazlaSatis == 0 {
		var yeniKota int64
		_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(disk_kota_mb,0) FROM service_plans WHERE id=?`, *planID).Scan(&yeniKota)
		var mevcut int64
		_ = h.DB.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(COALESCE(p.disk_kota_mb,0)),0) FROM domains d
			   LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.reseller_id=?`, rid).Scan(&mevcut)
		if mevcut+yeniKota >= maxDiskMB && yeniKota > 0 {
			return fmt.Errorf("disk kotanız yetersiz (taahhüt %d MB + yeni %d MB > limit %d MB)", mevcut, yeniKota, maxDiskMB)
		}
	}
	return nil
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r) // >0 ise reseller olusturuyor
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	req.AlanAdi = strings.ToLower(strings.TrimSpace(req.AlanAdi))
	// Plan seçilmediyse varsayılan planı ata — kaynak limitleri HER domaine uygulanır
	// (plan-driven default). Varsayılan yoksa plansız devam eder (limit uygulanmaz).
	if req.PlanID == nil {
		var defID int64
		// Reseller ise ONCE kendi varsayilan plani, yoksa global (reseller_id=0).
		if e := h.DB.QueryRowContext(r.Context(),
			`SELECT id FROM service_plans WHERE varsayilan=1 AND reseller_id IN (?,0) ORDER BY reseller_id DESC, id LIMIT 1`, rid).Scan(&defID); e == nil && defID > 0 {
			req.PlanID = &defID
		}
	}
	// Reseller kapsam denetimi: kota + plan sahipligi + musteri atamasi yasak.
	if rid > 0 {
		req.CustomerID = nil // reseller musteri atayamaz (Faz 2 kapsami)
		if req.PlanID != nil {
			var planRid int64
			if e := h.DB.QueryRowContext(r.Context(), `SELECT reseller_id FROM service_plans WHERE id=?`, *req.PlanID).Scan(&planRid); e != nil {
				httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
				return
			}
			if planRid != 0 && planRid != rid {
				httpx.WriteError(w, http.StatusForbidden, "bu plana erişiminiz yok")
				return
			}
		}
		// 🔴 Kilit BURADA alinir ve istek bitene kadar tutulur: kota okumasi ile
		// domains INSERT'i arasinda baska bir istek araya giremesin (TOCTOU).
		kilit := bayiKilidi(rid)
		kilit.Lock()
		defer kilit.Unlock()
		if err := h.resellerKotaKontrol(r.Context(), rid, req.PlanID); err != nil {
			httpx.WriteError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	if req.PHPSurum == "" {
		req.PHPSurum = "8.3"
		// Plan seçildiyse PHP sürümünü plandan miras al
		if req.PlanID != nil {
			var pv string
			if e := h.DB.QueryRowContext(r.Context(), `SELECT php_surum FROM service_plans WHERE id=?`, *req.PlanID).Scan(&pv); e == nil && strings.TrimSpace(pv) != "" {
				req.PHPSurum = pv
			}
		}
	}
	if err := provisioner.ValidateDomain(req.AlanAdi); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var existing int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT id FROM domains WHERE alan_adi=?`, req.AlanAdi).Scan(&existing)
	if err == nil {
		httpx.WriteError(w, http.StatusConflict, "bu alan adı zaten kayıtlı")
		return
	}

	// 1) Linux user + nginx + PHP pool
	if err := kota.CheckDomainEklenebilir(r.Context(), h.DB, nil); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	pr, err := provisioner.Provision(req.AlanAdi, req.PHPSurum)
	if err != nil {
		log.Printf("provision %q başarısız: %v", req.AlanAdi, err)
		httpx.WriteError(w, http.StatusInternalServerError, "sağlama başarısız: "+err.Error())
		return
	}

	dbUser := pr.SistemKullanici + "_db"
	dbName := pr.SistemKullanici + "_main"

	// 2) domains satırı
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO domains(alan_adi, sistem_kullanici, php_surum, ssl_aktif, durum, ipv4,
		   ftp_host, ftp_user, db_host, db_user, db_adi, web_root, is_demo, reseller_id)
		 VALUES(?,?,?,0,'aktif',?,?,?, 'localhost',?,?,?, 0, ?)`,
		req.AlanAdi, pr.SistemKullanici, req.PHPSurum, h.IPv4,
		h.IPv4, pr.SistemKullanici, dbUser, dbName, pr.WebRoot, rid)
	if err != nil {
		_ = provisioner.Deprovision(req.AlanAdi, pr.SistemKullanici)
		httpx.WriteError(w, http.StatusInternalServerError, "DB kayıt başarısız: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()

	if req.CustomerID != nil || req.PlanID != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE domains SET customer_id=?, plan_id=? WHERE id=?`,
			req.CustomerID, req.PlanID, id)
	}
	// Plan seçildiyse nginx web-sunucusu varsayılanlarını domain'e tohumla + vhost yenile
	if req.PlanID != nil {
		h.applyPlanNginxDefaults(r.Context(), id, *req.PlanID, pr.SistemKullanici, req.PHPSurum)
	}
	// 🔴🔴 A: IZOLASYON HERKESE — plan SECILMESE DE calisir.
	//
	// ESKI HAL (kapatilan delik): bu blok yukaridaki `if req.PlanID != nil` govdesinin
	// ICINDEYDI. Plansiz olusturulan YENI bir domain UygulaHepsi'yi HIC cagirmiyordu →
	// per-tenant CageFS FPM'e HIC gecmiyor, paylasilan master'da (izolasyonsuz) kaliyordu.
	// Yani "izolasyon" sessizce bir plan ozelligi haline gelmisti.
	//
	// YENI HAL: kosulsuz cagrilir. UygulaHepsi plansiz tenant'ta yalnizca KAYNAK
	// LIMITLERINI atlar (slice yazmaz, governor uygulamaz); izolasyonu (EnableTenantFPM)
	// her durumda kurar. Boylece yeni domain unit'i dogrudan guncel sertlestirilmis
	// sablonla (renderTenantUnit + damga) olusur.
	//
	// Arka plan + kendi 5dk context'i ZORUNLU: r.Context() HTTP istegi bitince iptal olur,
	// cutover yarida kalirdi. SetPlan ile ayni desen.
	go func(did int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := kaynaklimit.UygulaHepsi(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply (create) domain=%d: %v", did, err)
		}
	}(id)

	// 3) FTP hesap (random parola)
	ftpPass := hesaplar.RandomParola(20)
	uidN, gidN := uidGidOf(pr.SistemKullanici)
	if err := hesaplar.FTPCreate(h.DB, id, pr.SistemKullanici, ftpPass, uidN, gidN); err != nil {
		log.Printf("FTP create %q hata: %v", pr.SistemKullanici, err)
	}

	// 4) Default MySQL veritabanı + kullanıcı
	dbPass := hesaplar.RandomParola(24)
	if err := hesaplar.MySQLCreateDB(h.DB, id, dbName, dbUser, dbPass); err != nil {
		log.Printf("MySQL create %q hata: %v", dbName, err)
	}

	// 4.5) HTTPS-ZORUNLU uzanti (.app/.dev/.page...) → sertifikayi HEMEN dene.
	// Tarayici bu TLD'lerde HSTS preload nedeniyle sertifika hatasini atlatmaya
	// IZIN VERMEZ; sertifika olmadan site hic acilmaz. DNS henuz yayilmadiysa
	// sessiz basarisiz olur (fail-safe self-signed 443'u ayakta tutar).
	// Sertifikasiz kalirsa site TAMAMEN erisilemez olacaksa (HTTPS-zorunlu uzanti
	// ya da CDN/proxy arkasi) sertifikayi kurulum aninda dene.
	if gerek, _ := provisioner.SertifikaSartMi(req.AlanAdi); gerek {
		go func(alan, sk, php, backend string, did int64) {
			crt, key, gercek := provisioner.OtoSSLDene(alan, sk, php, backend)
			if !gercek {
				return // DNS yayilmamis olabilir; panelden elle kurulabilir
			}
			// Gercek CA cert'i kuruldu → panel "SSL var" gostersin.
			_, _ = h.DB.Exec(`UPDATE domains SET ssl_aktif=1, ssl_kaynak='letsencrypt',
			  cert_path=?, key_path=?, ssl_bitis=DATE_ADD(NOW(), INTERVAL 90 DAY) WHERE id=?`,
				crt, key, did)
		}(req.AlanAdi, pr.SistemKullanici, req.PHPSurum, "php-fpm", id)
	}

	// 5) DNS şablonu otomatik tohumla + BIND zone yaz + reload
	if _, err := dns.SeedDefaults(r.Context(), h.DB, id, req.AlanAdi, h.IPv4); err != nil {
		log.Printf("DNS SeedDefaults %q hata: %v", req.AlanAdi, err)
	}
	if err := dns.WriteZone(r.Context(), h.DB, id); err != nil {
		log.Printf("DNS WriteZone %q hata: %v", req.AlanAdi, err)
	}

	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE d.id=?", id)
	d, _ := scan(row)

	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "hosting.olustur", req.AlanAdi, fmt.Sprintf("plan=%d", planNoLog(req.PlanID)), rid, true)
	resp := createResp{Domain: d}
	resp.OluşturulanParolalar.FTP = ftpPass
	resp.OluşturulanParolalar.DB = dbPass
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// 🔴 Silme ile aski zinciri ayni kilitte: zincir, Deprovision nginx conf'unu
	// sildikten SONRA RerenderVhost ile geri yaziyordu → DB'de olmayan domain
	// icin YETIM vhost kaliyordu (nginx silinmis alan adini dinlemeye devam).
	if kap := httpx.DomainKapsam(h.DB, id); true {
		k := kilit.Bayi(kap)
		k.Lock()
		defer k.Unlock()
	}
	var alanAdi, sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}

	if isDemo == 0 {
		// MariaDB'deki gerçek DB'leri kaldır (CASCADE FK sadece panel DB metadata'sını siler)
		_ = hesaplar.MySQLDropAllForDomain(h.DB, id)
		// nginx vhost + PHP pool + Linux user + per-tenant FPM servisi (Deprovision içinde)
		if err := provisioner.Deprovision(alanAdi, sk); err != nil {
			log.Printf("deprovision warn (%s): %v", alanAdi, err)
		}
		// Kaynak-limit slice'ını (girginos-<sk>.slice) kaldır (Deprovision FPM'i söktü).
		_ = kaynaklimit.SystemdSliceSil(sk)
		// Redis tenant cache: Valkey ACL user + WP drop-in + cp_domain_redis satırı.
		// cp_domain_redis'te CASCADE FK olmadığı için domain silinince satır orphan kalıyordu.
		redis.KapatDomain(h.DB, id, sk)
		// NOT: /var/backups/girginospanel/<sk>/ dizini KASITLI olarak korunur.
		// Müşteri domaini yanlışlıkla silmiş olabilir → yedekler kurtarma için saklanır.
		// (Manuel temizlik için backups.RemoveDomainBackups mevcut.)
	}

	// Orphan temizliği: bu tablolarda FK cascade yok (mevcut kurulumlar için),
	// domain silinince satırlar orphan kalmasın diye açıkça sil.
	// 🔴 Domaine-ÖZEL plan da gider: katalog planları (domain_id NULL) korunur.
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM service_plans WHERE domain_id=?`, id)
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM domain_trafik WHERE domain_id=?`, id)
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM domain_trafik_imlec WHERE domain_id=?`, id)
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM wp_bakim WHERE domain_id=?`, id)

	// Denetim kaydi SILMEDEN once alinir: kapsam (domains.reseller_id) satir
	// gittikten sonra cozulemez.
	kapsamSil := httpx.DomainKapsam(h.DB, id)
	uidSil, kulSil := middleware.Aktor(r)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM domains WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silme hatası: "+err.Error())
		return
	}
	httpx.Denetim(h.DB, r, uidSil, kulSil, "hosting.sil", alanAdi, "sistem_kullanici="+sk, kapsamSil, true)

	// BIND zone temizliği DELETE'ten SONRA: updateZoneIncludes zones.conf'u domains
	// tablosundan yeniden üretir; domain hâlâ tabloda olsaydı (eski sıra) son silinen
	// domainin zone include'u geri yazılırdı (dangling → named reload hatası).
	if isDemo == 0 {
		if err := dns.DeleteZone(r.Context(), h.DB, alanAdi); err != nil {
			log.Printf("DNS DeleteZone warn (%s): %v", alanAdi, err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"silinen": map[string]string{"alan_adi": alanAdi, "sistem_kullanici": sk},
	})
}

func uidGidOf(u string) (int, int) {
	uu, err := user.Lookup(u)
	if err != nil {
		return 0, 0
	}
	uid, _ := strconv.Atoi(uu.Uid)
	gid, _ := strconv.Atoi(uu.Gid)
	return uid, gid
}

type setPHPReq struct {
	PHPSurum string `json:"php_surum"`
}

func (h *Handlers) SetPHP(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setPHPReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if req.PHPSurum == "" {
		httpx.WriteError(w, http.StatusBadRequest, "php_surum zorunlu")
		return
	}
	var alanAdi, sk, backend, certPath, keyPath, sslKaynak string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(web_backend,'php-fpm'), COALESCE(cert_path,''), COALESCE(key_path,''), COALESCE(ssl_kaynak,'') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo, &backend, &certPath, &keyPath, &sslKaynak)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin PHP sürümü değiştirilemez")
		return
	}
	socket, err := provisioner.SetPHPVersion(alanAdi, sk, req.PHPSurum, certPath, keyPath, sslKaynak, backend)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "değişim başarısız: "+err.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET php_surum=? WHERE id=?`, req.PHPSurum, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "php_surum": req.PHPSurum, "socket": socket,
	})
}

// Web backend seçici — "php-fpm" | "apache" | "static"
type setBackendReq struct {
	Backend string `json:"backend"`
}

var gecerliBackendler = map[string]bool{"php-fpm": true, "apache": true, "static": true}

func (h *Handlers) GetWebBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var backend string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).Scan(&backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"backend":   backend,
		"mevcutlar": []string{"php-fpm", "apache", "static"},
	})
}

func (h *Handlers) SetWebBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setBackendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if !gecerliBackendler[req.Backend] {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz backend (php-fpm|apache|static)")
		return
	}
	var alanAdi, sk, phpSurum string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin backend'i değiştirilemez")
		return
	}
	_ = alanAdi
	// 1) DB güncelle
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET web_backend=? WHERE id=?`, req.Backend, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	// 2) Vhost'u yeniden uygula (nginx + apache yöneticisi web_backend'i DB'den okur)
	socket, _ := provisioner.PHPSocketFor(sk, phpSurum)
	if err := provisioner.ApplyVhostForDomain(h.DB, id, socket, phpSurum); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost render: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "backend": req.Backend,
	})
}

// ── Belge koku (web_root) — Laravel vb. icin dinamik public_html/<alt> ──
type setWebRootReq struct {
	AltDizin string `json:"alt_dizin"`
}

// GET /domains/{id}/web-root → mevcut alt dizin + aday dizinler (dinamik secim)
func (h *Handlers) GetWebRoot(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk, webRoot string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(web_root,'') FROM domains WHERE id=?`, id).Scan(&sk, &webRoot)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alt_dizin": provisioner.AltDizinCoz(sk, webRoot),
		"adaylar":   provisioner.AdayKokler(sk),
	})
}

// PUT /domains/{id}/web-root {"alt_dizin":"public"} → public_html/<alt>'i belge koku yap.
// Bos alt_dizin = public_html koku. Dizin VAR OLMALI + public_html icinde OLMALI.
func (h *Handlers) SetWebRoot(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setWebRootReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin belge kökü değiştirilemez")
		return
	}
	// doğrula + mutlak yol (traversal/cross-tenant/symlink kapalı, dizin var olmalı)
	abs, verr := provisioner.WebRootMutlak(sk, req.AltDizin)
	if verr != nil {
		httpx.WriteError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET web_root=? WHERE id=?`, abs, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	// vhost yeniden render (nginx -t + rollback İÇERİDE — bozuk config diske kalmaz)
	if err := provisioner.RerenderVhost(h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost render: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"alt_dizin": provisioner.AltDizinCoz(sk, abs),
	})
}

// FTP parola değiştir
type setFTPPwReq struct {
	Parola string `json:"parola"`
}

func (h *Handlers) SetFTPPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setFTPPwReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if req.Parola == "" {
		req.Parola = hesaplar.RandomParola(20)
	}
	if !hesaplar.ParolaGecerli(req.Parola) {
		httpx.WriteError(w, http.StatusBadRequest, "parola geçersiz karakter (satır sonu) içeriyor")
		return
	}
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin FTP parolası değiştirilemez")
		return
	}
	if err := hesaplar.FTPUpdatePassword(h.DB, sk, req.Parola); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "FTP parola güncelleme: "+err.Error())
		return
	}
	// SSH açıksa sistem (SSH) parolasını da FTP ile senkronla
	var sshOn int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT ssh_erisim FROM domains WHERE id=?`, id).Scan(&sshOn)
	if sshOn == 1 {
		_ = hesaplar.SyncSSHPassword(h.DB, sk)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "username": sk, "parola": req.Parola,
	})
}

// Veritabanı listele (domain'e ait)
type DBAccount struct {
	ID          int64  `json:"id"`
	DomainID    int64  `json:"domain_id"`
	DBAdi       string `json:"db_adi"`
	DBKullanici string `json:"db_kullanici"`
	DBHost      string `json:"db_host"`
	DBParola    string `json:"db_parola"`
	Olusturulma string `json:"olusturulma"`
}

func (h *Handlers) ListDatabases(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, domain_id, db_name, db_user, db_host, db_pass_plain, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		 FROM db_accounts WHERE domain_id=? ORDER BY id`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB sorgu: "+err.Error())
		return
	}
	defer rows.Close()
	out := make([]DBAccount, 0)
	for rows.Next() {
		var d DBAccount
		if err := rows.Scan(&d.ID, &d.DomainID, &d.DBAdi, &d.DBKullanici, &d.DBHost, &d.DBParola, &d.Olusturulma); err != nil {
			continue
		}
		d.DBParola = gizli.CozBagli(d.DBParola, d.DBKullanici) // at-rest sifreli → sahibine ACIK gosterilir
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// createDBReq: "Yeni Veritabanı" istegi.
//
// Otomatik=true (veya hicbir alan verilmezse) → DB adi/kullanici/parola OTOMATIK uretilir
// (eski davranis, geriye uyumlu). Aksi halde musteri OZELLESTIRIR:
//   - DBSonek: DB adi soneki → panel `<sk>_` onekini ZORUNLU ekler (cakisma-guvenli).
//   - KullaniciTipi "yeni": KullaniciSonek gir (onek eklenir); "mevcut": MevcutKullanici sec.
//   - Parola: musteri girer (guclu olmali) VEYA bos → panel guclu rastgele uretir.
type createDBReq struct {
	Otomatik        bool   `json:"otomatik"`
	DBSonek         string `json:"db_sonek"`
	KullaniciTipi   string `json:"kullanici_tipi"` // "yeni" | "mevcut"
	KullaniciSonek  string `json:"kullanici_sonek"`
	MevcutKullanici string `json:"mevcut_kullanici"`
	Parola          string `json:"parola"`
}

func (h *Handlers) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req createDBReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domain sorgu: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe veritabanı eklenemez")
		return
	}
	if err := kota.CheckDBEklenebilir(r.Context(), h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// Geriye uyumlu: gövde boş / Otomatik=true → hepsini otomatik üret (eski davranış).
	otomatik := req.Otomatik ||
		(req.DBSonek == "" && req.KullaniciSonek == "" && req.MevcutKullanici == "" && req.Parola == "")

	var dbAdi, dbKullanici, parola string
	mevcutKullaniciModu := false

	if otomatik {
		dbAdi = sk + "_ek" + strconv.FormatInt(id, 10)
		dbKullanici = dbAdi
		parola = hesaplar.RandomParola(24)
	} else {
		// --- DB adı: müşteri SONEK verir, panel `<sk>_` önekini ZORUNLU ekler ---
		if req.DBSonek == "" {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı adı soneki gerekli")
			return
		}
		if !hesaplar.GecerliDBSonek(req.DBSonek) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz veritabanı soneki (yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter)")
			return
		}
		dbAdi = sk + "_" + req.DBSonek
		if !hesaplar.GecerliDBKimlik(dbAdi) {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
			return
		}

		// --- Kullanıcı: yeni (sonek) VEYA mevcut (bu domaine ait) ---
		switch req.KullaniciTipi {
		case "mevcut":
			if req.MevcutKullanici == "" || !hesaplar.GecerliDBKimlik(req.MevcutKullanici) {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz mevcut kullanıcı")
				return
			}
			// Sahiplik: seçilen kullanıcı GERÇEKTEN bu domaine ait olmalı (önek garantisi).
			var n int
			_ = h.DB.QueryRowContext(r.Context(),
				`SELECT COUNT(*) FROM db_accounts WHERE domain_id=? AND db_user=?`, id, req.MevcutKullanici).Scan(&n)
			if n == 0 {
				httpx.WriteError(w, http.StatusBadRequest, "seçilen kullanıcı bu domaine ait değil")
				return
			}
			dbKullanici = req.MevcutKullanici
			mevcutKullaniciModu = true
		default: // "yeni"
			if req.KullaniciSonek == "" {
				httpx.WriteError(w, http.StatusBadRequest, "kullanıcı adı soneki gerekli")
				return
			}
			if !hesaplar.GecerliDBSonek(req.KullaniciSonek) {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı soneki (yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter)")
				return
			}
			dbKullanici = sk + "_" + req.KullaniciSonek
			if !hesaplar.GecerliDBKimlik(dbKullanici) {
				httpx.WriteError(w, http.StatusBadRequest, "kullanıcı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
				return
			}
			// Yeni kullanıcı için parola: müşteri girer (güçlü) VEYA boş → panel üretir.
			if req.Parola == "" {
				parola = hesaplar.RandomParola(24)
			} else {
				if ok, neden := hesaplar.ParolaGucluMu(req.Parola); !ok {
					httpx.WriteError(w, http.StatusBadRequest, neden)
					return
				}
				parola = req.Parola
			}
		}
	}

	// İsim çakışması → net 409 (duplicate-key 500 yerine).
	var cakisma int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM db_accounts WHERE db_name=?`, dbAdi).Scan(&cakisma)
	if cakisma > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu isimde bir veritabanı zaten var: "+dbAdi)
		return
	}

	if mevcutKullaniciModu {
		if err := hesaplar.MySQLCreateDBForUser(h.DB, id, dbAdi, dbKullanici); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "DB oluşturma: "+err.Error())
			return
		}
		// Mevcut kullanıcının parolasını yanıtta göster (müşteri zaten sahibi).
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT db_pass_plain FROM db_accounts WHERE db_user=? LIMIT 1`, dbKullanici).Scan(&parola)
		parola = gizli.CozBagli(parola, dbKullanici) // yanitta ACIK deger doner (musteri baglanacak)
	} else {
		if err := hesaplar.MySQLCreateDB(h.DB, id, dbAdi, dbKullanici, parola); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "DB oluşturma: "+err.Error())
			return
		}
	}

	// Governor/limit: yeni DB-kullanıcısına plan limitlerini uygula (arka planda, best-effort).
	go func(did int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := kaynaklimit.UygulaHepsi(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply (db-create) domain=%d: %v", did, err)
		}
	}(id)

	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.olustur", dbAdi, "kullanici="+dbKullanici, id, true)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "domain_id": id, "db_adi": dbAdi, "db_kullanici": dbKullanici, "db_parola": parola,
	})
}

func (h *Handlers) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var dbName, dbUser string
	var isDemo int
	var domainID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo, d.id
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo, &domainID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	// Rota seviyesinde {id} domain param'i yok → sahiplik BURADA dogrulanir.
	if !middleware.YonetimSahibi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB'si silinemez")
		return
	}
	// Kullanıcı başka DB'lerde de kullanılıyorsa (mevcut-kullanıcı modu) sadece DB'yi
	// düşür — kullanıcıyı koru (aksi halde paylaşan diğer DB'lerin erişimi kırılır).
	var paylasim int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM db_accounts WHERE db_user=? AND db_name<>?`, dbUser, dbName).Scan(&paylasim)
	if paylasim > 0 {
		if err := hesaplar.MySQLDropDBKeepUser(h.DB, dbName); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "DB silme: "+err.Error())
			return
		}
	} else if err := hesaplar.MySQLDropDB(h.DB, dbName, dbUser); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB silme: "+err.Error())
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.sil", dbName, "kullanici="+dbUser, domainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": dbName})
}

// TopluSahip: birden Ã§ok domain'in customer_id'sini gÃ¼ncelle
type topluSahipReq struct {
	IDs        []int64 `json:"ids"`
	CustomerID *int64  `json:"customer_id"`
}

func (h *Handlers) TopluSahip(w http.ResponseWriter, r *http.Request) {
	var req topluSahipReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geÃ§ersiz gÃ¶vde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "boÅŸ ids")
		return
	}
	// customer_id NULL veya pozitif olabilir
	if req.CustomerID != nil && *req.CustomerID > 0 {
		var exists int
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM customers WHERE id=?`, *req.CustomerID).Scan(&exists)
		if exists == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "mÃ¼ÅŸteri bulunamadÄ±")
			return
		}
	}
	// IN clause icin placeholder
	placeholders := make([]string, len(req.IDs))
	args := []any{}
	if req.CustomerID != nil && *req.CustomerID > 0 {
		args = append(args, *req.CustomerID)
	} else {
		args = append(args, nil)
	}
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	sql := `UPDATE domains SET customer_id=? WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	res, err := h.DB.ExecContext(r.Context(), sql, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "gÃ¼ncelleme: "+err.Error())
		return
	}
	n, _ := res.RowsAffected()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "guncellenen": n})
}

// TopluDurum: aktif/pasif toggle
type topluDurumReq struct {
	IDs   []int64 `json:"ids"`
	Durum string  `json:"durum"` // "aktif" | "pasif"
}

func (h *Handlers) TopluDurum(w http.ResponseWriter, r *http.Request) {
	var req topluDurumReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "boş ids")
		return
	}
	if req.Durum != "aktif" && req.Durum != "pasif" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz durum")
		return
	}
	// 🔴 "Pasif" ARTIK GERCEKTEN KAPATIR: eskiden yalnizca durum kolonu degisiyor,
	// site yayinda kaliyordu (kullanici "pasif yaptim ama bir sey olmadi" diyordu).
	// Artik tekil askiya-alma ile AYNI makineden gecer.
	askida := req.Durum == "pasif"
	rid := middleware.ResellerIDFrom(r)
	uid, kul := middleware.Aktor(r)

	basarili, atlanan := 0, 0
	for _, id := range req.IDs {
		// Kapsam: bayi yalniz KENDI hosting hesabina dokunabilir.
		if rid > 0 {
			var sahip int64
			if err := h.DB.QueryRowContext(r.Context(),
				`SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, id).Scan(&sahip); err != nil || sahip != rid {
				atlanan++
				continue
			}
		}
		alanAdi, err := h.AskiUygula(r.Context(), id, askida, "manuel")
		if err != nil {
			log.Printf("toplu durum: domain %d: %v", id, err)
			atlanan++
			continue
		}
		eylem := "hosting.askidan_al"
		if askida {
			eylem = "hosting.askiya_al"
		}
		httpx.DenetimDomain(h.DB, r, uid, kul, eylem, alanAdi, "toplu işlem", id, true)
		basarili++
	}
	httpx.Denetim(h.DB, r, uid, kul, "hosting.toplu_durum", req.Durum,
		fmt.Sprintf("istenen=%d uygulanan=%d atlanan=%d", len(req.IDs), basarili, atlanan), rid, atlanan == 0)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": atlanan == 0, "guncellenen": basarili, "atlanan": atlanan,
	})
}

// applyPlanNginxDefaults, yeni domain bir plana bağlandığında planın nginx
// varsayılanlarını (FastCGI cache + client_max_body + ek direktifler) domain'in
// nginx_settings satırına yazar ve vhost'u bu ayarlarla yeniden render eder.
// Best-effort: hata olursa domain yine de oluşturulmuş kalır (yalnızca loglanır).
func (h *Handlers) applyPlanNginxDefaults(ctx context.Context, domainID, planID int64, sk, php string) {
	var fc, cmb int
	var ekPlan string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT fastcgi_cache, client_max_body_mb, COALESCE(nginx_ek_direktifler,'')
		   FROM service_plans WHERE id=?`, planID).Scan(&fc, &cmb, &ekPlan); err != nil {
		log.Printf("plan nginx defaults oku (plan=%d): %v", planID, err)
		return
	}
	ek := ""
	if cmb > 0 {
		ek = "client_max_body_size " + strconv.Itoa(cmb) + "m;\n"
	}
	if strings.TrimSpace(ekPlan) != "" {
		ek += ekPlan
	}
	if _, err := h.DB.ExecContext(ctx,
		`INSERT INTO nginx_settings(domain_id, fastcgi_cache, ek_direktifler)
		 VALUES(?,?,?)
		 ON DUPLICATE KEY UPDATE fastcgi_cache=VALUES(fastcgi_cache), ek_direktifler=VALUES(ek_direktifler)`,
		domainID, fc, ek); err != nil {
		log.Printf("nginx_settings tohumla (domain=%d): %v", domainID, err)
		return
	}
	socket, err := provisioner.PHPSocketFor(sk, php)
	if err != nil {
		log.Printf("php socket (domain=%d): %v", domainID, err)
		return
	}
	if err := provisioner.ApplyVhostForDomain(h.DB, domainID, socket, php); err != nil {
		log.Printf("plan vhost yeniden render (domain=%d): %v", domainID, err)
	}
}

// planNoLog: denetim kaydinda plan kimligini yazdirmak icin — nil ise 0.
func planNoLog(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
