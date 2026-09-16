package reseller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// Paket: adminin bayilere satacagi hazir limit paketi (Bronz/Gumus/Altin gibi).
type Paket struct {
	ID          int64  `json:"id"`
	Ad          string `json:"ad"`
	Aciklama    string `json:"aciklama"`
	MaxDomain   int    `json:"max_domain"`
	MaxDiskMB   int64  `json:"max_disk_mb"`
	MaxTrafikMB int64  `json:"max_trafik_mb"`
	// Posta TAVANLARI — domain başına üst sınır (havuz değil). 0 = sınırsız.
	MailMaxEmail     int   `json:"mail_max_email"`
	MailSaatlikLimit int   `json:"mail_saatlik_limit"`
	MailKutuKotaMB   int   `json:"mail_kutu_kota_mb"`
	FiyatKurus       int64 `json:"fiyat_kurus"`
	// Ilkeler (Plesk: "Fazla kullanim" + "Fazla satma"):
	AsimIlkesi   string `json:"asim_ilkesi"`   // yok | disk_trafik | tumu
	AsimBildirim bool   `json:"asim_bildirim"` // asimda yoneticiye e-posta
	FazlaSatis   bool   `json:"fazla_satis"`   // sahip oldugundan fazlasini satabilir mi
	Varsayilan   bool   `json:"varsayilan"`
	BayiSayisi   int    `json:"bayi_sayisi"`
	Olusturulma  string `json:"olusturulma"`
}

// GET /reseller-plans — bayi paketleri (admin).
func (h *Handlers) PaketListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT p.id, p.ad, p.aciklama, p.max_domain, p.max_disk_mb, p.max_trafik_mb,
		        COALESCE(p.mail_max_email,0), COALESCE(p.mail_saatlik_limit,0), COALESCE(p.mail_kutu_kota_mb,0),
		        p.fiyat_kurus, p.asim_ilkesi, p.asim_bildirim, p.fazla_satis, p.varsayilan,
		        (SELECT COUNT(*) FROM users u WHERE u.reseller_plan_id=p.id AND u.role='reseller'),
		        COALESCE(DATE_FORMAT(p.created_at,'%Y-%m-%d'),'')
		   FROM reseller_plans p ORDER BY p.varsayilan DESC, p.max_domain, p.id`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi: "+err.Error())
		return
	}
	defer rows.Close()
	out := []Paket{}
	for rows.Next() {
		var x Paket
		var vars, bild, fs int
		if err := rows.Scan(&x.ID, &x.Ad, &x.Aciklama, &x.MaxDomain, &x.MaxDiskMB, &x.MaxTrafikMB,
			&x.MailMaxEmail, &x.MailSaatlikLimit, &x.MailKutuKotaMB,
			&x.FiyatKurus, &x.AsimIlkesi, &bild, &fs, &vars, &x.BayiSayisi, &x.Olusturulma); err == nil {
			x.Varsayilan = vars == 1
			x.AsimBildirim = bild == 1
			x.FazlaSatis = fs == 1
			out = append(out, x)
		}
	}
	_ = rows.Err()
	httpx.WriteJSON(w, http.StatusOK, out)
}

type paketReq struct {
	AsimIlkesi       string `json:"asim_ilkesi"`
	AsimBildirim     bool   `json:"asim_bildirim"`
	FazlaSatis       bool   `json:"fazla_satis"`
	Ad               string `json:"ad"`
	Aciklama         string `json:"aciklama"`
	MaxDomain        int    `json:"max_domain"`
	MaxDiskMB        int64  `json:"max_disk_mb"`
	MaxTrafikMB      int64  `json:"max_trafik_mb"`
	MailMaxEmail     int    `json:"mail_max_email"`
	MailSaatlikLimit int    `json:"mail_saatlik_limit"`
	MailKutuKotaMB   int    `json:"mail_kutu_kota_mb"`
	FiyatKurus       int64  `json:"fiyat_kurus"`
	Varsayilan       bool   `json:"varsayilan"`
}

func (q *paketReq) dogrula() string {
	q.Ad = strings.TrimSpace(q.Ad)
	if q.Ad == "" || len(q.Ad) > 100 {
		return "paket adı zorunlu (en çok 100 karakter)"
	}
	if q.MaxDomain < 0 || q.MaxDiskMB < 0 || q.MaxTrafikMB < 0 || q.FiyatKurus < 0 {
		return "limitler/fiyat negatif olamaz"
	}
	return ""
}

// enKucukTaahhut: HER bayinin kullanabilecegi (global katalog) en kucuk hosting
// planinin disk/trafik taahhudunu, plan adiyla birlikte dondurur.
//
// 🔴 KAPSAM: reseller_id=0 (admin katalogu — her bayiye acik) VE
// domain_id IS NULL (domaine-ozel planlar katalogda degil, 0046 gocu). Bayinin
// KENDI planlari (reseller_id>0) bilerek HARIC: onlar yalniz o bayiye aciktir,
// yeni acilan bir bayi onlari kullanamaz → kapi yanlis gecerdi.
//
// 0 doner = "olcut yok" (hic plan yok ya da tum planlar sinirsiz) → kapi uygulanmaz.
func (h *Handlers) enKucukTaahhut(r *http.Request) (diskAd string, diskMB int64, trafikAd string, trafikMB int64) {
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT ad, disk_kota_mb FROM service_plans
		  WHERE reseller_id=0 AND domain_id IS NULL AND disk_kota_mb>0
		  ORDER BY disk_kota_mb ASC, id ASC LIMIT 1`).Scan(&diskAd, &diskMB)
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT ad, trafik_kota_mb FROM service_plans
		  WHERE reseller_id=0 AND domain_id IS NULL AND trafik_kota_mb>0
		  ORDER BY trafik_kota_mb ASC, id ASC LIMIT 1`).Scan(&trafikAd, &trafikMB)
	return
}

// taahhutKapisi: paket limitleri en kucuk hosting planinin taahhudunu karsilamiyorsa
// NET Turkce mesaj doner (bos = gecti).
//
// 🔴 NEDEN: paket limiti tek bir hosting planinin taahhudunun altindaysa o bayi
// TEK hosting bile acamaz; panel bugun bunu SESSIZCE kaydediyor, hata ancak bayi
// hosting acmaya calisinca "trafik/disk kotanız yetersiz..." diye ortaya cikiyordu
// (bkz. internal/domains/handlers.go taahhut kapisi).
//
// 🔴 KARSILASTIRMA "<=": domains tarafindaki kapi `mevcut+yeni >= limit` ile
// reddediyor — yani SINIR DEGERINE ULASMAK da dolu sayiliyor. Paket limiti bu
// yuzden plan taahhudunden KESINLIKLE BUYUK olmali; esitlik de kullanilamaz paket.
//
// 🔴 fazla_satis (oversell) ACIK ise kapi UYGULANMAZ: o modda domains
// tarafindaki taahhut kapisi zaten atlaniyor (fazlaSatis==1 kontrolu).
//
// 0 = sinirsiz semantigi korunur: limiti 0 olan alan denetlenmez.
func (h *Handlers) taahhutKapisi(r *http.Request, req *paketReq) string {
	if req.FazlaSatis {
		return ""
	}
	diskAd, diskMB, trafikAd, trafikMB := h.enKucukTaahhut(r)
	if req.MaxDiskMB > 0 && diskMB > 0 && req.MaxDiskMB <= diskMB {
		return fmt.Sprintf("bu paketle tek hosting bile açılamaz: paketin disk limiti %d MB, "+
			"en küçük hosting planı %q ise %d MB taahhüt ediyor. Paket limiti bu taahhütten BÜYÜK olmalı "+
			"(sınır değerine ulaşmak dolu sayılır). Paket limitini artırın ya da “fazla satış”ı açın.",
			req.MaxDiskMB, diskAd, diskMB)
	}
	if req.MaxTrafikMB > 0 && trafikMB > 0 && req.MaxTrafikMB <= trafikMB {
		return fmt.Sprintf("bu paketle tek hosting bile açılamaz: paketin trafik limiti %d MB, "+
			"en küçük hosting planı %q ise %d MB taahhüt ediyor. Paket limiti bu taahhütten BÜYÜK olmalı "+
			"(sınır değerine ulaşmak dolu sayılır). Paket limitini artırın ya da “fazla satış”ı açın.",
			req.MaxTrafikMB, trafikAd, trafikMB)
	}
	return ""
}

// POST /reseller-plans — yeni bayi paketi (admin).
func (h *Handlers) PaketOlustur(w http.ResponseWriter, r *http.Request) {
	var req paketReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if msg := req.dogrula(); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.taahhutKapisi(r, &req); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	v := 0
	if req.Varsayilan {
		v = 1
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE reseller_plans SET varsayilan=0`); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "varsayılan sıfırlanamadı: "+err.Error())
			return
		}
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO reseller_plans(ad, aciklama, max_domain, max_disk_mb, max_trafik_mb,
		   mail_max_email, mail_saatlik_limit, mail_kutu_kota_mb, fiyat_kurus,
		   asim_ilkesi, asim_bildirim, fazla_satis, varsayilan)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.Ad, strings.TrimSpace(req.Aciklama), req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB,
		req.MailMaxEmail, req.MailSaatlikLimit, req.MailKutuKotaMB, req.FiyatKurus,
		asimNormalize(req.AsimIlkesi), b01(req.AsimBildirim), b01(req.FazlaSatis), v)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			httpx.WriteError(w, http.StatusConflict, "bu isimde paket zaten var")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "oluşturulamadı: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// PUT /reseller-plans/{id} — paket güncelle (admin). Mevcut bayilerin limitleri DEĞİŞMEZ
// (anlık-görüntü modeli); istenirse "bayiye uygula" ile tek tek güncellenir.
func (h *Handlers) PaketGuncelle(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req paketReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if msg := req.dogrula(); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.taahhutKapisi(r, &req); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	v := 0
	if req.Varsayilan {
		v = 1
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE reseller_plans SET varsayilan=0 WHERE id<>?`, id); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "varsayılan sıfırlanamadı: "+err.Error())
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE reseller_plans SET ad=?, aciklama=?, max_domain=?, max_disk_mb=?, max_trafik_mb=?,
		   mail_max_email=?, mail_saatlik_limit=?, mail_kutu_kota_mb=?, fiyat_kurus=?, asim_ilkesi=?, asim_bildirim=?, fazla_satis=?, varsayilan=? WHERE id=?`,
		req.Ad, strings.TrimSpace(req.Aciklama), req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB,
		req.MailMaxEmail, req.MailSaatlikLimit, req.MailKutuKotaMB,
		req.FiyatKurus, asimNormalize(req.AsimIlkesi), b01(req.AsimBildirim), b01(req.FazlaSatis),
		v, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "güncellenemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /reseller-plans/{id} — paket sil (admin). Bagli bayi varsa reddet.
func (h *Handlers) PaketSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var n int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE reseller_plan_id=? AND role='reseller'`, id).Scan(&n)
	if n > 0 {
		httpx.WriteError(w, http.StatusConflict,
			"bu pakete bağlı "+strconv.Itoa(n)+" bayi var; önce onları başka pakete taşıyın")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM reseller_plans WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// paketLimitleri: paket id -> limitler (bayi olustururken/guncellenirken kopyalanir).
func (h *Handlers) paketLimitleri(r *http.Request, paketID int64) (maxDomain int, diskMB, trafikMB int64, ok bool) {
	if paketID <= 0 {
		return 0, 0, 0, false
	}
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT max_domain, max_disk_mb, max_trafik_mb FROM reseller_plans WHERE id=?`, paketID).
		Scan(&maxDomain, &diskMB, &trafikMB); err != nil {
		return 0, 0, 0, false
	}
	return maxDomain, diskMB, trafikMB, true
}

// asimNormalize: bilinmeyen/boş değerde güvenli varsayilan ('disk_trafik' —
// Plesk'in de onerdigi orta yol: disk+trafik esnek, diger kaynaklar sert).
func asimNormalize(v string) string {
	switch strings.TrimSpace(v) {
	case "yok", "tumu", "disk_trafik":
		return strings.TrimSpace(v)
	}
	return "disk_trafik"
}

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PaketDetay: GET /reseller-plans/{id} — ozel duzenleme sayfasi icin tek paket.
func (h *Handlers) PaketDetay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var x Paket
	var vars, bild, fs int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT p.id, p.ad, p.aciklama, p.max_domain, p.max_disk_mb, p.max_trafik_mb,
		        COALESCE(p.mail_max_email,0), COALESCE(p.mail_saatlik_limit,0), COALESCE(p.mail_kutu_kota_mb,0),
		        p.fiyat_kurus, p.asim_ilkesi, p.asim_bildirim, p.fazla_satis, p.varsayilan,
		        (SELECT COUNT(*) FROM users u WHERE u.reseller_plan_id=p.id AND u.role='reseller'),
		        COALESCE(DATE_FORMAT(p.created_at,'%Y-%m-%d'),'')
		   FROM reseller_plans p WHERE p.id=?`, id).
		// 🔴 mail_* alanları EKSİKTİ → düzenleme sayfası bunları 0 gösterip
		// kaydedince plandaki mail tavanlarını SIFIRLIYORDU (veri kaybı).
		Scan(&x.ID, &x.Ad, &x.Aciklama, &x.MaxDomain, &x.MaxDiskMB, &x.MaxTrafikMB,
			&x.MailMaxEmail, &x.MailSaatlikLimit, &x.MailKutuKotaMB,
			&x.FiyatKurus, &x.AsimIlkesi, &bild, &fs, &vars, &x.BayiSayisi, &x.Olusturulma)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "paket bulunamadı")
		return
	}
	x.Varsayilan, x.AsimBildirim, x.FazlaSatis = vars == 1, bild == 1, fs == 1
	httpx.WriteJSON(w, http.StatusOK, x)
}
