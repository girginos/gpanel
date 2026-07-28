// Package plans: hizmet paketi (service plan) CRUD + seed + kaynak limit alanları
package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type Plan struct {
	ID                 int64  `json:"id"`
	Ad                 string `json:"ad"`
	Aciklama           string `json:"aciklama"`
	DiskKotaMB         int    `json:"disk_kota_mb"` // 0 = sınırsız
	TrafikKotaMB       int    `json:"trafik_kota_mb"`
	MaxDomain          int    `json:"max_domain"`
	MaxDB              int    `json:"max_db"`
	MaxEmail           int    `json:"max_email"`
	MaxFTP             int    `json:"max_ftp"`
	CPUYuzde           int    `json:"cpu_yuzde"`   // 100 = 1 core
	RAMMB              int    `json:"ram_mb"`      // hard limit MB
	MaxProcess         int    `json:"max_process"` // TasksMax
	InodeKota          int    `json:"inode_kota"`
	IOAgirlik          int    `json:"io_agirlik"` // 1-1000 (systemd IOWeight — göreli öncelik)
	MySQLMaxBaglanti   int    `json:"mysql_max_baglanti"`
	PMMaxChildren      int    `json:"pm_max_children"`         // PHP-FPM pm.max_children; 0 = otomatik max(4, ram_mb/64)
	IOReadMBps         int    `json:"io_read_mbps"`            // mutlak disk okuma bant genişliği MB/s; 0 = sınırsız
	IOWriteMBps        int    `json:"io_write_mbps"`           // mutlak disk yazma bant genişliği MB/s; 0 = sınırsız
	IOReadIOPS         int    `json:"io_read_iops"`            // mutlak disk okuma IOPS; 0 = sınırsız
	IOWriteIOPS        int    `json:"io_write_iops"`           // mutlak disk yazma IOPS; 0 = sınırsız
	DBMaxQueriesPerHr  int    `json:"db_max_queries_per_hour"` // MySQL MAX_QUERIES_PER_HOUR; 0 = sınırsız
	DBMaxUpdatesPerHr  int    `json:"db_max_updates_per_hour"` // MySQL MAX_UPDATES_PER_HOUR; 0 = sınırsız
	DBMaxQuerySeconds  int    `json:"db_max_query_seconds"`    // yavaş-sorgu KILL eşiği (sn); 0 = öldürme yok
	PHPSurum           string `json:"php_surum"`
	FastCgiCache       bool   `json:"fastcgi_cache"`
	ClientMaxBodyMB    int    `json:"client_max_body_mb"`
	NginxEkDirektifler string `json:"nginx_ek_direktifler"`
	// WAF (ModSecurity + OWASP CRS) plan varsayilani — bu plandaki domainler devralir.
	WafEnabled  bool   `json:"waf_enabled"`  // plan varsayilani WAF acik mi
	WafMode     string `json:"waf_mode"`     // "on" (engelle) | "detect" (yalniz kaydet) | "off"
	WafParanoia int    `json:"waf_paranoia"` // CRS paranoia 1..4
	Varsayilan  bool   `json:"varsayilan"`
	Olusturulma string `json:"olusturulma"`
}

type Handlers struct {
	DB *sql.DB
}

const selectAll = `SELECT id, ad, aciklama, disk_kota_mb, trafik_kota_mb,
  max_domain, max_db, max_email, max_ftp,
  cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
  COALESCE(pm_max_children,0),
  COALESCE(io_read_mbps,0), COALESCE(io_write_mbps,0), COALESCE(io_read_iops,0), COALESCE(io_write_iops,0),
  COALESCE(db_max_queries_per_hour,0), COALESCE(db_max_updates_per_hour,0), COALESCE(db_max_query_seconds,0),
  php_surum, fastcgi_cache, client_max_body_mb, COALESCE(nginx_ek_direktifler,''),
  COALESCE(waf_enabled,0), COALESCE(waf_mode,'on'), COALESCE(waf_paranoia,1),
  varsayilan, DATE_FORMAT(created_at,'%Y-%m-%d') FROM service_plans`

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scan(rs interface{ Scan(...any) error }) (Plan, error) {
	var p Plan
	var vars, fc, wafEn int
	err := rs.Scan(&p.ID, &p.Ad, &p.Aciklama, &p.DiskKotaMB, &p.TrafikKotaMB,
		&p.MaxDomain, &p.MaxDB, &p.MaxEmail, &p.MaxFTP,
		&p.CPUYuzde, &p.RAMMB, &p.MaxProcess, &p.InodeKota, &p.IOAgirlik, &p.MySQLMaxBaglanti,
		&p.PMMaxChildren,
		&p.IOReadMBps, &p.IOWriteMBps, &p.IOReadIOPS, &p.IOWriteIOPS,
		&p.DBMaxQueriesPerHr, &p.DBMaxUpdatesPerHr, &p.DBMaxQuerySeconds,
		&p.PHPSurum, &fc, &p.ClientMaxBodyMB, &p.NginxEkDirektifler,
		&wafEn, &p.WafMode, &p.WafParanoia,
		&vars, &p.Olusturulma)
	p.Varsayilan = vars == 1
	p.FastCgiCache = fc == 1
	p.WafEnabled = wafEn == 1
	return p, err
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	// Reseller: kendi planlari + global (reseller_id=0). Admin: hepsi.
	// 🔴 Domaine-OZEL planlar (domain_id dolu) katalogda LISTELENMEZ — yoksa her
	// ozellestirilen hosting katalogu bir satir daha sisirirdi (100 domain = 100 plan).
	// `?domain=<id>` ile yalniz O domainin ozel plani listeye dahil edilir.
	ozelDomain := strings.TrimSpace(r.URL.Query().Get("domain"))
	kosul := " WHERE (domain_id IS NULL"
	args := []any{}
	if ozelDomain != "" {
		if did, e := strconv.ParseInt(ozelDomain, 10, 64); e == nil && did > 0 {
			kosul += " OR domain_id=?"
			args = append(args, did)
		}
	}
	kosul += ")"
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		kosul += " AND reseller_id IN (?,0)"
		args = append(args, rid)
	}
	sorgu := selectAll + kosul + " ORDER BY varsayilan DESC, id ASC"
	rows, err := h.DB.QueryContext(r.Context(), sorgu, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := make([]Plan, 0)
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	sorguGet := selectAll + " WHERE id=?"
	argsGet := []any{id}
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		sorguGet = selectAll + " WHERE id=? AND reseller_id IN (?,0)"
		argsGet = append(argsGet, rid)
	}
	row := h.DB.QueryRowContext(r.Context(), sorguGet, argsGet...)
	p, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "plan bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Domain kullanımı
	var dCount int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domains WHERE plan_id=?`, id).Scan(&dCount)
	resp := map[string]any{
		"plan":          p,
		"domain_sayisi": dCount,
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func varsayilanDoldur(p *Plan) {
	if p.CPUYuzde == 0 {
		p.CPUYuzde = 100
	}
	if p.RAMMB == 0 {
		p.RAMMB = 512
	}
	if p.MaxProcess == 0 {
		p.MaxProcess = 50
	}
	if p.InodeKota == 0 {
		p.InodeKota = 50000
	}
	if p.IOAgirlik == 0 {
		p.IOAgirlik = 100
	}
	if p.MySQLMaxBaglanti == 0 {
		p.MySQLMaxBaglanti = 25
	}
	if strings.TrimSpace(p.PHPSurum) == "" {
		p.PHPSurum = "8.3"
	}
	if p.ClientMaxBodyMB == 0 {
		p.ClientMaxBodyMB = 64
	}
	// WAF varsayilanlari
	switch strings.ToLower(strings.TrimSpace(p.WafMode)) {
	case "on", "detect", "off":
		p.WafMode = strings.ToLower(strings.TrimSpace(p.WafMode))
	default:
		p.WafMode = "on"
	}
	if p.WafParanoia < 1 || p.WafParanoia > 4 {
		p.WafParanoia = 1
	}
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	var p Plan
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	p.Ad = strings.TrimSpace(p.Ad)
	if p.Ad == "" {
		httpx.WriteError(w, http.StatusBadRequest, "plan adı zorunlu")
		return
	}
	varsayilanDoldur(&p)
	if err := provisioner.ValidateNginxDirectives(p.NginxEkDirektifler); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "nginx direktif doğrulaması başarısız:\n"+err.Error())
		return
	}
	ridC := middleware.ResellerIDFrom(r)
	// 🔴 Bayi plani SINIRSIZ olamaz: kota=0 "sinirsiz" demek ve taahhut hesabina
	// 0 olarak girdigi icin bayi, disk havuzu dolu olsa bile kota=0 plan acip
	// sinirsiz disk dagitabiliyordu. Kok icin 0 serbest (kendi sunucusu).
	if ridC > 0 && (p.DiskKotaMB <= 0 || p.TrafikKotaMB <= 0) {
		httpx.WriteError(w, http.StatusBadRequest,
			"bayi planlarında disk ve trafik kotası sıfır (sınırsız) olamaz")
		return
	}
	v := 0
	if p.Varsayilan {
		v = 1
		// KRITIK: varsayilan sifirlama KENDI kapsaminda (aksi halde bir bayi
		// digerlerinin/globalin varsayilan planini sifirlar).
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE service_plans SET varsayilan=0 WHERE reseller_id=?`, ridC)
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
		   max_domain, max_db, max_email, max_ftp,
		   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
		   pm_max_children,
		   io_read_mbps, io_write_mbps, io_read_iops, io_write_iops,
		   db_max_queries_per_hour, db_max_updates_per_hour, db_max_query_seconds,
		   php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler,
		   waf_enabled, waf_mode, waf_paranoia, varsayilan, reseller_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Ad, p.Aciklama, p.DiskKotaMB, p.TrafikKotaMB,
		p.MaxDomain, p.MaxDB, p.MaxEmail, p.MaxFTP,
		p.CPUYuzde, p.RAMMB, p.MaxProcess, p.InodeKota, p.IOAgirlik, p.MySQLMaxBaglanti,
		p.PMMaxChildren,
		p.IOReadMBps, p.IOWriteMBps, p.IOReadIOPS, p.IOWriteIOPS,
		p.DBMaxQueriesPerHr, p.DBMaxUpdatesPerHr, p.DBMaxQuerySeconds,
		p.PHPSurum, b01(p.FastCgiCache), p.ClientMaxBodyMB, p.NginxEkDirektifler,
		b01(p.WafEnabled), p.WafMode, p.WafParanoia, v, ridC)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", id)
	saved, _ := scan(row)
	httpx.WriteJSON(w, http.StatusCreated, saved)
}

func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ridU := middleware.ResellerIDFrom(r)
	if ridU > 0 && !h.planSahibi(r, id, ridU) {
		httpx.WriteError(w, http.StatusForbidden, "bu planı düzenleyemezsiniz")
		return
	}
	var p Plan
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	p.Ad = strings.TrimSpace(p.Ad)
	if p.Ad == "" {
		httpx.WriteError(w, http.StatusBadRequest, "plan adı zorunlu")
		return
	}
	varsayilanDoldur(&p)
	// 🔴 Bayi plani SINIRSIZ olamaz: kota=0 "sinirsiz" demek ve taahhut
	// hesabina 0 olarak girdigi icin bayi, disk havuzu dolu olsa bile
	// kota=0 plan acip sinirsiz disk dagitabiliyordu. Kok icin 0 serbest
	// (kendi sunucusu), bayi icin pozitif deger zorunlu.
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		if p.DiskKotaMB <= 0 || p.TrafikKotaMB <= 0 {
			httpx.WriteError(w, http.StatusBadRequest,
				"bayi planlarında disk ve trafik kotası sıfır (sınırsız) olamaz")
			return
		}
	}
	if err := provisioner.ValidateNginxDirectives(p.NginxEkDirektifler); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "nginx direktif doğrulaması başarısız:\n"+err.Error())
		return
	}
	v := 0
	if p.Varsayilan {
		v = 1
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE service_plans SET varsayilan=0 WHERE id<>? AND reseller_id=?`, id, ridU)
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE service_plans SET ad=?, aciklama=?, disk_kota_mb=?, trafik_kota_mb=?,
		   max_domain=?, max_db=?, max_email=?, max_ftp=?,
		   cpu_yuzde=?, ram_mb=?, max_process=?, inode_kota=?, io_agirlik=?, mysql_max_baglanti=?,
		   pm_max_children=?,
		   io_read_mbps=?, io_write_mbps=?, io_read_iops=?, io_write_iops=?,
		   db_max_queries_per_hour=?, db_max_updates_per_hour=?, db_max_query_seconds=?,
		   php_surum=?, fastcgi_cache=?, client_max_body_mb=?, nginx_ek_direktifler=?,
		   waf_enabled=?, waf_mode=?, waf_paranoia=?, varsayilan=?
		 WHERE id=?`,
		p.Ad, p.Aciklama, p.DiskKotaMB, p.TrafikKotaMB,
		p.MaxDomain, p.MaxDB, p.MaxEmail, p.MaxFTP,
		p.CPUYuzde, p.RAMMB, p.MaxProcess, p.InodeKota, p.IOAgirlik, p.MySQLMaxBaglanti,
		p.PMMaxChildren,
		p.IOReadMBps, p.IOWriteMBps, p.IOReadIOPS, p.IOWriteIOPS,
		p.DBMaxQueriesPerHr, p.DBMaxUpdatesPerHr, p.DBMaxQuerySeconds,
		p.PHPSurum, b01(p.FastCgiCache), p.ClientMaxBodyMB, p.NginxEkDirektifler,
		b01(p.WafEnabled), p.WafMode, p.WafParanoia, v, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Plan WAF varsayilani degismis olabilir → bu plandaki (domain override'i olmayan)
	// domainlerin vhost'unu arka planda yeniden render et ki WAF direktifi guncellensin.
	// (nginx -t gate + rollback her render'da korur; hata log'lanir, panel bloklanmaz.)
	go h.wafPlanReapply(id)
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", id)
	saved, _ := scan(row)
	// Kapsam: planin SAHIBI (kok global bir plani degistirdiginde de o plani
	// kullanan bayiler degisikligi kendi kaydinda gorebilmeli degil — plan kokun,
	// kayit kokun; ama bayi plani degisirse kayit bayinin kapsamina yazilir).
	var planSahip int64
	_ = h.DB.QueryRowContext(r.Context(), `SELECT reseller_id FROM service_plans WHERE id=?`, id).Scan(&planSahip)
	uidU, kulU := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uidU, kulU, "plan.guncelle", p.Ad, "", planSahip, true)
	httpx.WriteJSON(w, http.StatusOK, saved)
}

// wafPlanReapply: bu plana bagli tum domainlerin WAF ayarini (plan varsayilanini devralanlar
// dahil) yeniden uygular. Arka plan goroutine — kendi baglaminda calisir.
func (h *Handlers) wafPlanReapply(planID int64) {
	rows, err := h.DB.Query(`SELECT id FROM domains WHERE plan_id=?`, planID)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var did int64
		if rows.Scan(&did) == nil {
			ids = append(ids, did)
		}
	}
	rows.Close()
	for _, did := range ids {
		if err := provisioner.WAFUygula(h.DB, did); err != nil {
			log.Printf("waf plan reapply domain=%d: %v", did, err)
		}
	}
}

// planSahibi: plan bu bayiye mi ait (global planlar bayi tarafindan DEGISTIRILEMEZ).
func (h *Handlers) planSahibi(r *http.Request, planID, rid int64) bool {
	var owner int64
	if err := h.DB.QueryRowContext(r.Context(), `SELECT reseller_id FROM service_plans WHERE id=?`, planID).Scan(&owner); err != nil {
		return false
	}
	return owner == rid
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if rid := middleware.ResellerIDFrom(r); rid > 0 && !h.planSahibi(r, id, rid) {
		httpx.WriteError(w, http.StatusForbidden, "bu planı silemezsiniz")
		return
	}
	var n int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domains WHERE plan_id=?`, id).Scan(&n); err == nil && n > 0 {
		httpx.WriteError(w, http.StatusConflict,
			"bu plan "+strconv.Itoa(n)+" abonelikte kullanıldığı için silinemez")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM service_plans WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /plans/{id}/domains — bu plana bağlı domain listesi
func (h *Handlers) DomainlerAra(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// Bayi: yalniz KENDI planini sorgulayabilir ve yalniz KENDI domainlerini gorur
	// (global bir plani baska kiracilarin da kullanmasi onu ilgilendirmez).
	sorgu := `SELECT id, alan_adi, sistem_kullanici, durum, DATE_FORMAT(olusturulma,'%Y-%m-%d')
		 FROM domains WHERE plan_id=? ORDER BY id`
	argl := []any{id}
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		var sahip int64
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT reseller_id FROM service_plans WHERE id=?`, id).Scan(&sahip); err != nil || (sahip != rid && sahip != 0) {
			httpx.WriteError(w, http.StatusForbidden, "bu plana erişiminiz yok")
			return
		}
		sorgu = `SELECT id, alan_adi, sistem_kullanici, durum, DATE_FORMAT(olusturulma,'%Y-%m-%d')
		 FROM domains WHERE plan_id=? AND reseller_id=? ORDER BY id`
		argl = append(argl, rid)
	}
	rows, err := h.DB.QueryContext(r.Context(), sorgu, argl...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type dom struct {
		ID          int64  `json:"id"`
		AlanAdi     string `json:"alan_adi"`
		SK          string `json:"sistem_kullanici"`
		Durum       string `json:"durum"`
		Olusturulma string `json:"olusturulma"`
	}
	out := make([]dom, 0)
	for rows.Next() {
		var d dom
		if err := rows.Scan(&d.ID, &d.AlanAdi, &d.SK, &d.Durum, &d.Olusturulma); err == nil {
			out = append(out, d)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// SeedIfEmpty: 3 demo plan (Başlangıç, Standart, Profesyonel) — yeni kaynak limitleriyle
func SeedIfEmpty(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_plans`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("seed: 3 demo paket ekleniyor")
	for _, p := range seedPlanlari() {
		_, err := db.ExecContext(ctx,
			`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
			   max_domain, max_db, max_email, max_ftp,
			   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
			   pm_max_children, varsayilan)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Ad, p.Aciklama, p.Disk, p.Trafik, p.MaxDom, p.MaxDB, p.MaxMail, p.MaxFTP,
			p.CPU, p.RAM, p.Proc, p.Inode, p.IO, p.MyC, p.PMMax, p.Default)
		if err != nil {
			log.Printf("seed plan %s: %v", p.Ad, err)
		}
	}
	return nil
}

// seedTier: standart demo paketlerin sabit tanımı (SeedIfEmpty + SeedSync ortak kaynağı).
type seedTier struct {
	Ad, Aciklama                                 string
	Disk, Trafik, MaxDom, MaxDB, MaxMail, MaxFTP int
	CPU, RAM, Proc, Inode, IO, MyC, PMMax        int
	Default                                      int
}

func seedPlanlari() []seedTier {
	return []seedTier{
		{"Başlangıç", "Tek site, küçük proje", 1024, 5120, 1, 1, 5, 2,
			50, 256, 30, 25000, 100, 15, 4, 1},
		{"Standart", "Birden çok proje + e-posta", 10240, 51200, 5, 10, 25, 10,
			100, 512, 60, 100000, 100, 30, 8, 0},
		{"Profesyonel", "Yoğun trafik + büyük site", 51200, 204800, 25, 50, 100, 50,
			200, 2048, 150, 500000, 200, 100, 32, 0},
	}
}

// SeedSync: idempotent tohum senkronu — MEVCUT planlara DOKUNMADAN eksik standart
// tier'ları ekler (INSERT ... WHERE NOT EXISTS ad). SeedIfEmpty yalnız tablo boşken
// çalışır; SeedSync ise 177 gibi zaten dolu kurulumlarda yeni tier'ları güvenle ekler.
// Operatör tarafından düzenlenmiş planların değerleri KORUNUR.
func SeedSync(ctx context.Context, db *sql.DB) error {
	for _, p := range seedPlanlari() {
		// Varsayılan bayrağını burada set ETME (mevcut varsayılanı ezmemek için);
		// yalnız plan hiç yoksa, adıyla ekle.
		_, err := db.ExecContext(ctx,
			`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
			   max_domain, max_db, max_email, max_ftp,
			   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
			   pm_max_children, varsayilan)
			 SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0
			 FROM DUAL
			 WHERE NOT EXISTS (SELECT 1 FROM service_plans WHERE ad=?)`,
			p.Ad, p.Aciklama, p.Disk, p.Trafik, p.MaxDom, p.MaxDB, p.MaxMail, p.MaxFTP,
			p.CPU, p.RAM, p.Proc, p.Inode, p.IO, p.MyC, p.PMMax, p.Ad)
		if err != nil {
			log.Printf("SeedSync plan %s: %v", p.Ad, err)
		}
	}
	return nil
}
