// Package reseller: admin tarafindan reseller (bayi) hesaplari yonetimi (Faz 1).
// reseller = users satiri role='reseller'; sahip oldugu kaynaklarin scope-anahtari kendi users.id.
package reseller

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type Handlers struct {
	DB *sql.DB
}

var reKullanici = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type Reseller struct {
	ID           int64  `json:"id"`
	Kullanici    string `json:"kullanici"`
	AdSoyad      string `json:"ad_soyad"`
	Durum        string `json:"durum"`
	MaxDomain    int    `json:"max_domain"`
	MaxDiskMB    int64  `json:"max_disk_mb"`
	MaxTrafikMB  int64  `json:"max_trafik_mb"`
	DomainSayisi int    `json:"domain_sayisi"`
	DiskKullanKB int64  `json:"disk_kullanim_kb"`
	PaketID      int64  `json:"paket_id"`
	PaketAd      string `json:"paket_ad"`
	SonGiris     string `json:"son_giris"`
	Olusturulma  string `json:"olusturulma"`
}

// GET /resellers — tum reseller'lar + limit + kullanim (admin).
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT u.id, u.username, COALESCE(u.full_name,''), u.status,
		        u.max_domain, u.max_disk_mb, u.max_trafik_mb,
		        (SELECT COUNT(*) FROM domains d WHERE d.reseller_id=u.id),
		        COALESCE((SELECT SUM(d.boyut_kb) FROM domains d WHERE d.reseller_id=u.id),0),
		        u.reseller_plan_id, COALESCE((SELECT ad FROM reseller_plans rp WHERE rp.id=u.reseller_plan_id),''),
		        COALESCE(DATE_FORMAT(u.last_login_at,'%Y-%m-%d %H:%i'),''),
		        COALESCE(DATE_FORMAT(u.created_at,'%Y-%m-%d'),'')
		 FROM users u WHERE u.role='reseller' ORDER BY u.username`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi: "+err.Error())
		return
	}
	defer rows.Close()
	out := []Reseller{}
	for rows.Next() {
		var x Reseller
		if err := rows.Scan(&x.ID, &x.Kullanici, &x.AdSoyad, &x.Durum, &x.MaxDomain, &x.MaxDiskMB,
			&x.MaxTrafikMB, &x.DomainSayisi, &x.DiskKullanKB, &x.PaketID, &x.PaketAd, &x.SonGiris, &x.Olusturulma); err == nil {
			out = append(out, x)
		}
	}
	_ = rows.Err()
	httpx.WriteJSON(w, http.StatusOK, out)
}

type olusturReq struct {
	PaketID     int64  `json:"paket_id"`
	Kullanici   string `json:"kullanici"`
	Parola      string `json:"parola"`
	AdSoyad     string `json:"ad_soyad"`
	MaxDomain   int    `json:"max_domain"`
	MaxDiskMB   int64  `json:"max_disk_mb"`
	MaxTrafikMB int64  `json:"max_trafik_mb"`
}

// POST /resellers — yeni reseller (admin).
func (h *Handlers) Olustur(w http.ResponseWriter, r *http.Request) {
	var req olusturReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Kullanici = strings.ToLower(strings.TrimSpace(req.Kullanici))
	if !reKullanici.MatchString(req.Kullanici) || req.Kullanici == "root" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı adı (3-32, küçük harf/rakam/._-)")
		return
	}
	if len(req.Parola) < 8 || len(req.Parola) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "parola 8-128 karakter olmalı")
		return
	}
	// Paket secildiyse limitleri PAKETTEN kopyala (anlik-goruntu).
	if md, dk, tr, ok := h.paketLimitleri(r, req.PaketID); ok {
		req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB = md, dk, tr
	}
	if req.MaxDomain < 0 || req.MaxDiskMB < 0 || req.MaxTrafikMB < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "limitler negatif olamaz")
		return
	}
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, req.Kullanici).Scan(&n)
	if n > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu kullanıcı adı zaten var")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Parola), 12)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola işlenemedi")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO users(username, password_hash, role, full_name, status, max_domain, max_disk_mb, max_trafik_mb, reseller_plan_id)
		 VALUES(?,?, 'reseller', ?, 'active', ?, ?, ?, ?)`,
		req.Kullanici, string(hash), strings.TrimSpace(req.AdSoyad), req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB, req.PaketID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oluşturulamadı: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "kullanici": req.Kullanici})
}

type guncelleReq struct {
	PaketID     *int64  `json:"paket_id"`
	AdSoyad     *string `json:"ad_soyad"`
	Durum       *string `json:"durum"`
	Parola      *string `json:"parola"`
	MaxDomain   *int    `json:"max_domain"`
	MaxDiskMB   *int64  `json:"max_disk_mb"`
	MaxTrafikMB *int64  `json:"max_trafik_mb"`
}

// PUT /resellers/{id} — limit/durum/parola güncelle (admin).
func (h *Handlers) Guncelle(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var mevcut int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, id).Scan(&mevcut)
	if mevcut == 0 {
		httpx.WriteError(w, http.StatusNotFound, "reseller bulunamadı")
		return
	}
	var req guncelleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	sets := []string{}
	args := []any{}
	if req.PaketID != nil {
		sets = append(sets, "reseller_plan_id=?")
		args = append(args, *req.PaketID)
		if md, dk, tr, ok := h.paketLimitleri(r, *req.PaketID); ok {
			sets = append(sets, "max_domain=?", "max_disk_mb=?", "max_trafik_mb=?")
			args = append(args, md, dk, tr)
		}
	}
	if req.AdSoyad != nil {
		sets = append(sets, "full_name=?")
		args = append(args, strings.TrimSpace(*req.AdSoyad))
	}
	if req.Durum != nil {
		if *req.Durum != "active" && *req.Durum != "suspended" {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz durum")
			return
		}
		sets = append(sets, "status=?")
		args = append(args, *req.Durum)
	}
	if req.MaxDomain != nil {
		sets = append(sets, "max_domain=?")
		args = append(args, *req.MaxDomain)
	}
	if req.MaxDiskMB != nil {
		sets = append(sets, "max_disk_mb=?")
		args = append(args, *req.MaxDiskMB)
	}
	if req.MaxTrafikMB != nil {
		sets = append(sets, "max_trafik_mb=?")
		args = append(args, *req.MaxTrafikMB)
	}
	if req.Parola != nil && *req.Parola != "" {
		if len(*req.Parola) < 8 || len(*req.Parola) > 128 {
			httpx.WriteError(w, http.StatusBadRequest, "parola 8-128 karakter olmalı")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Parola), 12)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "parola işlenemedi")
			return
		}
		sets = append(sets, "password_hash=?")
		args = append(args, string(hash))
	}
	if len(sets) == 0 {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	args = append(args, id)
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id=? AND role='reseller'`, args...); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "güncellenemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /resellers/{id} — reseller sil (admin). Hosting hesabi varsa reddet.
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var dom int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE reseller_id=?`, id).Scan(&dom)
	if dom > 0 {
		httpx.WriteError(w, http.StatusConflict, "önce bu reseller'ın hosting hesaplarını taşıyın/silin")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM users WHERE id=? AND role='reseller'`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
