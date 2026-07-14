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
	IOAgirlik          int    `json:"io_agirlik"` // 1-1000 (systemd IOWeight)
	MySQLMaxBaglanti   int    `json:"mysql_max_baglanti"`
	PHPSurum           string `json:"php_surum"`
	FastCgiCache       bool   `json:"fastcgi_cache"`
	ClientMaxBodyMB    int    `json:"client_max_body_mb"`
	NginxEkDirektifler string `json:"nginx_ek_direktifler"`
	Varsayilan         bool   `json:"varsayilan"`
	Olusturulma        string `json:"olusturulma"`
}

type Handlers struct {
	DB *sql.DB
}

const selectAll = `SELECT id, ad, aciklama, disk_kota_mb, trafik_kota_mb,
  max_domain, max_db, max_email, max_ftp,
  cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
  php_surum, fastcgi_cache, client_max_body_mb, COALESCE(nginx_ek_direktifler,''), varsayilan, DATE_FORMAT(created_at,'%Y-%m-%d') FROM service_plans`

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scan(rs interface{ Scan(...any) error }) (Plan, error) {
	var p Plan
	var vars, fc int
	err := rs.Scan(&p.ID, &p.Ad, &p.Aciklama, &p.DiskKotaMB, &p.TrafikKotaMB,
		&p.MaxDomain, &p.MaxDB, &p.MaxEmail, &p.MaxFTP,
		&p.CPUYuzde, &p.RAMMB, &p.MaxProcess, &p.InodeKota, &p.IOAgirlik, &p.MySQLMaxBaglanti,
		&p.PHPSurum, &fc, &p.ClientMaxBodyMB, &p.NginxEkDirektifler, &vars, &p.Olusturulma)
	p.Varsayilan = vars == 1
	p.FastCgiCache = fc == 1
	return p, err
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), selectAll+" ORDER BY varsayilan DESC, id ASC")
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
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", id)
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
	v := 0
	if p.Varsayilan {
		v = 1
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE service_plans SET varsayilan=0`)
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
		   max_domain, max_db, max_email, max_ftp,
		   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
		   php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler, varsayilan)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Ad, p.Aciklama, p.DiskKotaMB, p.TrafikKotaMB,
		p.MaxDomain, p.MaxDB, p.MaxEmail, p.MaxFTP,
		p.CPUYuzde, p.RAMMB, p.MaxProcess, p.InodeKota, p.IOAgirlik, p.MySQLMaxBaglanti,
		p.PHPSurum, b01(p.FastCgiCache), p.ClientMaxBodyMB, p.NginxEkDirektifler, v)
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
	v := 0
	if p.Varsayilan {
		v = 1
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE service_plans SET varsayilan=0 WHERE id<>?`, id)
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE service_plans SET ad=?, aciklama=?, disk_kota_mb=?, trafik_kota_mb=?,
		   max_domain=?, max_db=?, max_email=?, max_ftp=?,
		   cpu_yuzde=?, ram_mb=?, max_process=?, inode_kota=?, io_agirlik=?, mysql_max_baglanti=?,
		   php_surum=?, fastcgi_cache=?, client_max_body_mb=?, nginx_ek_direktifler=?, varsayilan=?
		 WHERE id=?`,
		p.Ad, p.Aciklama, p.DiskKotaMB, p.TrafikKotaMB,
		p.MaxDomain, p.MaxDB, p.MaxEmail, p.MaxFTP,
		p.CPUYuzde, p.RAMMB, p.MaxProcess, p.InodeKota, p.IOAgirlik, p.MySQLMaxBaglanti,
		p.PHPSurum, b01(p.FastCgiCache), p.ClientMaxBodyMB, p.NginxEkDirektifler, v, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", id)
	saved, _ := scan(row)
	httpx.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, alan_adi, sistem_kullanici, durum, DATE_FORMAT(olusturulma,'%Y-%m-%d')
		 FROM domains WHERE plan_id=? ORDER BY id`, id)
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
	rows := []struct {
		Ad, Aciklama                                 string
		Disk, Trafik, MaxDom, MaxDB, MaxMail, MaxFTP int
		CPU, RAM, Proc, Inode, IO, MyC               int
		Default                                      int
	}{
		{"Başlangıç", "Tek site, küçük proje", 1024, 5120, 1, 1, 5, 2,
			50, 256, 30, 25000, 100, 15, 1},
		{"Standart", "Birden çok proje + e-posta", 10240, 51200, 5, 10, 25, 10,
			100, 512, 60, 100000, 100, 30, 0},
		{"Profesyonel", "Yoğun trafik + büyük site", 51200, 204800, 25, 50, 100, 50,
			200, 2048, 150, 500000, 200, 100, 0},
	}
	for _, p := range rows {
		_, err := db.ExecContext(ctx,
			`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
			   max_domain, max_db, max_email, max_ftp,
			   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
			   varsayilan)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Ad, p.Aciklama, p.Disk, p.Trafik, p.MaxDom, p.MaxDB, p.MaxMail, p.MaxFTP,
			p.CPU, p.RAM, p.Proc, p.Inode, p.IO, p.MyC, p.Default)
		if err != nil {
			log.Printf("seed plan %s: %v", p.Ad, err)
		}
	}
	return nil
}
