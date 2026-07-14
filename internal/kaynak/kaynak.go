// Package kaynak: per-domain kaynak kullanim + plan limit ozeti
package kaynak

import (
	"context"
	"database/sql"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Limit struct {
	Kullanim int64 `json:"kullanim"`
	Limit    int64 `json:"limit"` // 0 = sınırsız
}

type Ozet struct {
	AlanAdi  string `json:"alan_adi"`
	SK       string `json:"sk"`
	PlanAdi  string `json:"plan_adi"`
	PHPSurum string `json:"php_surum"`
	IPv4     string `json:"ipv4"`
	SSLAktif bool   `json:"ssl_aktif"`
	SSLBitis string `json:"ssl_bitis,omitempty"`

	// Limit'li metrikler (kullanim / limit)
	DiskMB     Limit `json:"disk_mb"`     // disk_kota_mb plan'dan
	TrafikMB   Limit `json:"trafik_mb"`   // trafik_kota_mb plan'dan
	DBSayisi   Limit `json:"db_sayisi"`   // max_db plan'dan
	FTPSayisi  Limit `json:"ftp_sayisi"`  // max_ftp plan'dan
	EpostaSayi Limit `json:"eposta_sayi"` // max_email plan'dan
	DomainSayi Limit `json:"domain_sayi"` // max_domain plan'dan (subdomain dahil)

	// Bonus sayaclar (limit yok)
	DNSKayit    int64 `json:"dns_kayit"`
	CronIs      int64 `json:"cron_is"`
	YedekSayisi int64 `json:"yedek_sayisi"`
	YedekMB     int64 `json:"yedek_mb"`
}

type Handlers struct {
	DB *sql.DB
}

// duMB: home dizinin disk kullanimi MB cinsinden
func duMB(home string) int64 {
	out, err := exec.Command("du", "-sm", home).CombinedOutput()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(parts[0], 10, 64)
	return n
}

// dbTotalMB: panel kullanicisinin db'lerinin toplam boyutu MB
func dbTotalMB(ctx context.Context, db *sql.DB, dbUsers []string) int64 {
	if len(dbUsers) == 0 {
		return 0
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(dbUsers)), ",")
	args := make([]any, len(dbUsers))
	for i, u := range dbUsers {
		args[i] = u
	}
	// information_schema'dan toplam (data+index)
	q := `SELECT COALESCE(SUM((data_length+index_length))/1024/1024, 0)
	      FROM information_schema.tables
	      WHERE table_schema IN (
	          SELECT db_name FROM panel.db_accounts WHERE db_user IN (` + placeholders + `)
	      )`
	var mb float64
	_ = db.QueryRowContext(ctx, q, args...).Scan(&mb)
	return int64(mb)
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ctx := r.Context()

	var o Ozet
	var planID sql.NullInt64
	var sslBitis sql.NullString
	var sslA int
	err := h.DB.QueryRowContext(ctx,
		`SELECT d.alan_adi, d.sistem_kullanici, d.php_surum, d.ipv4, d.ssl_aktif,
		        DATE_FORMAT(d.ssl_bitis,'%Y-%m-%d'), d.plan_id
		 FROM domains d WHERE d.id=?`, id).
		Scan(&o.AlanAdi, &o.SK, &o.PHPSurum, &o.IPv4, &sslA, &sslBitis, &planID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	o.SSLAktif = sslA == 1
	if sslBitis.Valid {
		o.SSLBitis = sslBitis.String
	}

	// Plan limitleri
	var pAdi sql.NullString
	var diskKota, trafikKota, maxDom, maxDB, maxEmail, maxFTP int64
	if planID.Valid {
		_ = h.DB.QueryRowContext(ctx,
			`SELECT ad, disk_kota_mb, trafik_kota_mb, max_domain, max_db, max_email, max_ftp
			 FROM service_plans WHERE id=?`, planID.Int64).
			Scan(&pAdi, &diskKota, &trafikKota, &maxDom, &maxDB, &maxEmail, &maxFTP)
	}
	if pAdi.Valid {
		o.PlanAdi = pAdi.String
	} else {
		o.PlanAdi = "Sınırsız (paket atanmadı)"
	}

	// Disk kullanım
	home := "/home/" + o.SK
	o.DiskMB.Kullanim = duMB(home)
	_, _ = h.DB.ExecContext(ctx, `UPDATE domains SET boyut_kb=? WHERE id=?`, o.DiskMB.Kullanim*1024, id)
	o.DiskMB.Limit = diskKota

	// Trafik: domains.trafik_kb (KB → MB)
	var trafikKB int64
	_ = h.DB.QueryRowContext(ctx, `SELECT trafik_kb FROM domains WHERE id=?`, id).Scan(&trafikKB)
	o.TrafikMB.Kullanim = trafikKB / 1024
	o.TrafikMB.Limit = trafikKota

	// DB sayısı + toplam boyut
	rows, err := h.DB.QueryContext(ctx, `SELECT db_user FROM db_accounts WHERE domain_id=?`, id)
	dbUsers := []string{}
	if err == nil {
		for rows.Next() {
			var u string
			if rows.Scan(&u) == nil {
				dbUsers = append(dbUsers, u)
			}
		}
		rows.Close()
	}
	o.DBSayisi.Kullanim = int64(len(dbUsers))
	o.DBSayisi.Limit = maxDB

	// FTP sayısı
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=?`, id).Scan(&o.FTPSayisi.Kullanim)
	o.FTPSayisi.Limit = maxFTP

	// E-posta (modül yok şu an — 0)
	o.EpostaSayi.Kullanim = 0
	o.EpostaSayi.Limit = maxEmail

	// Domain sayısı: bu zaten 1 — ama abonelik kapsamında subdomain'leri saymak gerek
	// (Plesk modeli: subscription primary + 0 subdomain) — şimdilik kendisi = 1
	o.DomainSayi.Kullanim = 1
	o.DomainSayi.Limit = maxDom

	// DNS kayıt
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_records WHERE domain_id=?`, id).Scan(&o.DNSKayit)

	// Cron iş sayısı (host'taki user crontab)
	if out, err := exec.Command("crontab", "-u", o.SK, "-l").CombinedOutput(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			s := strings.TrimSpace(ln)
			if s != "" && !strings.HasPrefix(s, "#") {
				o.CronIs++
			}
		}
	}

	// Yedek sayısı + toplam boyut
	_ = h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(boyut),0) FROM backups WHERE domain_id=?`, id).
		Scan(&o.YedekSayisi, &o.YedekMB)
	o.YedekMB = o.YedekMB / (1024 * 1024) // byte → MB

	// DB total MB (kullanim göstergesi için DiskMB.Kullanim'a EKLEME — ayrı tutalim)
	// (Şu an DiskMB sadece home; DB ayrı disk üzerinde olabilir, gerek yok)
	_ = dbTotalMB // future use

	httpx.WriteJSON(w, http.StatusOK, o)
}
