package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/user"
	"strconv"
	"strings"

	"girginospanel/internal/dns"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kota"
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
}

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

const selectAll = `SELECT d.id, d.alan_adi, d.sistem_kullanici, d.php_surum, d.ssl_aktif,
  COALESCE(DATE_FORMAT(d.ssl_bitis,'%Y-%m-%d'),''), d.durum, d.ipv4, d.ftp_host, d.ftp_user,
  d.db_host, d.db_user, d.db_adi, d.web_root, d.boyut_kb, d.trafik_kb, d.is_demo,
  COALESCE(d.notlar,''), DATE_FORMAT(d.olusturulma,'%Y-%m-%d'),
  d.plan_id, COALESCE(p.ad,''), d.ssh_erisim, COALESCE(d.askida,0)
  FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id`

func scan(rs interface{ Scan(...any) error }) (Domain, error) {
	var d Domain
	var ssl, demo, sshE, askida int
	var planID sql.NullInt64
	err := rs.Scan(&d.ID, &d.AlanAdi, &d.SistemKullanici, &d.PHPSurum, &ssl,
		&d.SSLBitis, &d.Durum, &d.IPv4, &d.FTPHost, &d.FTPUser,
		&d.DBHost, &d.DBUser, &d.DBAdi, &d.WebRoot, &d.BoyutKB, &d.TrafikKB, &demo,
		&d.Notlar, &d.Olusturulma,
		&planID, &d.PlanAd, &sshE, &askida)
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
	rows, err := h.DB.QueryContext(r.Context(), selectAll+" ORDER BY d.id DESC")
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

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	req.AlanAdi = strings.ToLower(strings.TrimSpace(req.AlanAdi))
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
		   ftp_host, ftp_user, db_host, db_user, db_adi, web_root, is_demo)
		 VALUES(?,?,?,0,'aktif',?,?,?, 'localhost',?,?,?, 0)`,
		req.AlanAdi, pr.SistemKullanici, req.PHPSurum, h.IPv4,
		h.IPv4, pr.SistemKullanici, dbUser, dbName, pr.WebRoot)
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

	// 5) DNS şablonu otomatik tohumla + BIND zone yaz + reload
	if _, err := dns.SeedDefaults(r.Context(), h.DB, id, req.AlanAdi, h.IPv4); err != nil {
		log.Printf("DNS SeedDefaults %q hata: %v", req.AlanAdi, err)
	}
	if err := dns.WriteZone(r.Context(), h.DB, id); err != nil {
		log.Printf("DNS WriteZone %q hata: %v", req.AlanAdi, err)
	}

	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE d.id=?", id)
	d, _ := scan(row)

	resp := createResp{Domain: d}
	resp.OluşturulanParolalar.FTP = ftpPass
	resp.OluşturulanParolalar.DB = dbPass
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
		// nginx vhost + PHP pool + Linux user
		if err := provisioner.Deprovision(alanAdi, sk); err != nil {
			log.Printf("deprovision warn (%s): %v", alanAdi, err)
		}
		// Redis tenant cache: Valkey ACL user + WP drop-in + cp_domain_redis satırı.
		// cp_domain_redis'te CASCADE FK olmadığı için domain silinince satır orphan kalıyordu.
		redis.KapatDomain(h.DB, id, sk)
		// NOT: /var/backups/girginospanel/<sk>/ dizini KASITLI olarak korunur.
		// Müşteri domaini yanlışlıkla silmiş olabilir → yedekler kurtarma için saklanır.
		// (Manuel temizlik için backups.RemoveDomainBackups mevcut.)
	}

	// Orphan temizliği: bu tablolarda FK cascade yok (mevcut kurulumlar için),
	// domain silinince satırlar orphan kalmasın diye açıkça sil.
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM domain_trafik WHERE domain_id=?`, id)
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM domain_trafik_imlec WHERE domain_id=?`, id)
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM wp_bakim WHERE domain_id=?`, id)

	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM domains WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silme hatası: "+err.Error())
		return
	}

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
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type createDBReq struct {
	DBAdi       string `json:"db_adi"`
	DBKullanici string `json:"db_kullanici"`
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
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe veritabanı eklenemez")
		return
	}
	if err := kota.CheckDBEklenebilir(r.Context(), h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	// Guvenlik: musteri-verdigi ad SQL-guvenli VE domain kullanicisiyla namespaced olmali
	if req.DBAdi != "" && !hesaplar.MusteriDBKimlikGecerli(sk, req.DBAdi) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz veritabanı adı (yalnız harf/rakam/alt-çizgi; '"+sk+"_' önekiyle)")
		return
	}
	if req.DBKullanici != "" && !hesaplar.MusteriDBKimlikGecerli(sk, req.DBKullanici) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz veritabanı kullanıcısı (yalnız harf/rakam/alt-çizgi; '"+sk+"_' önekiyle)")
		return
	}
	if req.DBAdi == "" {
		req.DBAdi = sk + "_ek" + strconv.FormatInt(id, 10)
	}
	if req.DBKullanici == "" {
		req.DBKullanici = req.DBAdi
	}
	pass := hesaplar.RandomParola(24)
	if err := hesaplar.MySQLCreateDB(h.DB, id, req.DBAdi, req.DBKullanici, pass); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB oluşturma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "domain_id": id, "db_adi": req.DBAdi, "db_kullanici": req.DBKullanici, "db_parola": pass,
	})
}

func (h *Handlers) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var dbName, dbUser string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB'si silinemez")
		return
	}
	if err := hesaplar.MySQLDropDB(h.DB, dbName, dbUser); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB silme: "+err.Error())
		return
	}
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
		httpx.WriteError(w, http.StatusBadRequest, "geÃ§ersiz gÃ¶vde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "boÅŸ ids")
		return
	}
	if req.Durum != "aktif" && req.Durum != "pasif" {
		httpx.WriteError(w, http.StatusBadRequest, "geÃ§ersiz durum")
		return
	}
	placeholders := make([]string, len(req.IDs))
	args := []any{req.Durum}
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	sql := `UPDATE domains SET durum=? WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	res, err := h.DB.ExecContext(r.Context(), sql, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "gÃ¼ncelleme: "+err.Error())
		return
	}
	n, _ := res.RowsAffected()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "guncellenen": n})
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
