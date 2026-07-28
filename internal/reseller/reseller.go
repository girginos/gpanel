// Package reseller: admin tarafindan reseller (bayi) hesaplari yonetimi (Faz 1).
// reseller = users satiri role='reseller'; sahip oldugu kaynaklarin scope-anahtari kendi users.id.
package reseller

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kilit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

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
	// 🔴 Paket ZORUNLU: paketsiz bayide tum limitler 0 kalir ve "0 = sinirsiz"
	// semantigi yuzunden bayi SINIRSIZ hosting/disk/trafik satabiliyordu.
	if req.PaketID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bayi paketi seçilmelidir")
		return
	}
	// Paket limitlerini PAKETTEN kopyala (anlik-goruntu). Paket yoksa 400 —
	// yoksa sarkik reseller_plan_id ile limitsiz bayi olusuyordu.
	ilkeAsim, ilkeBildirim, ilkeFazla := h.paketIlkeleri(r, req.PaketID)
	md, dk, tr, ok := h.paketLimitleri(r, req.PaketID)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "bayi paketi bulunamadı")
		return
	}
	req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB = md, dk, tr
	if req.MaxDomain < 0 || req.MaxDiskMB < 0 || req.MaxTrafikMB < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "limitler negatif olamaz")
		return
	}
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, req.Kullanici).Scan(&n)
	if n > 0 {
		uid, kul := middleware.Aktor(r)
		httpx.Denetim(h.DB, r, uid, kul, "bayi.olustur", req.Kullanici, "kullanıcı adı zaten var", 0, false)
		httpx.WriteError(w, http.StatusConflict, "bu kullanıcı adı zaten var")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Parola), 12)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "parola işlenemedi")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO users(username, password_hash, role, full_name, status, max_domain, max_disk_mb, max_trafik_mb,
		   asim_ilkesi, asim_bildirim, fazla_satis, reseller_plan_id)
		 VALUES(?,?, 'reseller', ?, 'active', ?, ?, ?, ?, ?, ?, ?)`,
		req.Kullanici, string(hash), strings.TrimSpace(req.AdSoyad), req.MaxDomain, req.MaxDiskMB, req.MaxTrafikMB,
		ilkeAsim, ilkeBildirim, ilkeFazla, req.PaketID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oluşturulamadı: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "bayi.olustur", req.Kullanici,
		fmt.Sprintf("paket=%d domain=%d disk=%dMB", req.PaketID, req.MaxDomain, req.MaxDiskMB), id, true)
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
	// Denetim icin ONCEKI degerler (B10: "bayi.guncelle" detayi bostu; hangi
	// alanin ne yapildigi denetlenemiyordu — fatura konusu limitlerde sart).
	var oncePaket int64
	var onceDomain int
	var onceDisk, onceTrafik int64
	var onceDurum string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT reseller_plan_id, max_domain, max_disk_mb, max_trafik_mb, status FROM users WHERE id=?`, id).
		Scan(&oncePaket, &onceDomain, &onceDisk, &onceTrafik, &onceDurum)
	degisim := []string{}

	sets := []string{}
	args := []any{}
	if req.PaketID != nil {
		if *req.PaketID <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz paket")
			return
		}
		if _, _, _, ok := h.paketLimitleri(r, *req.PaketID); !ok {
			httpx.WriteError(w, http.StatusBadRequest, "bayi paketi bulunamadı")
			return
		}
		degisim = append(degisim, fmt.Sprintf("paket %v→%v", oncePaket, *req.PaketID))
		sets = append(sets, "reseller_plan_id=?")
		args = append(args, *req.PaketID)
		if md, dk, tr, ok := h.paketLimitleri(r, *req.PaketID); ok {
			ia, ib, ifz := h.paketIlkeleri(r, *req.PaketID)
			// Paket degisimi limitleri VE ilkeleri (fazla_satis dahil) sessizce
			// degistiriyordu; asiri-satis izni denetim izi birakmadan veriliyordu.
			degisim = append(degisim,
				fmt.Sprintf("paketle gelen: domain=%d disk=%dMB trafik=%dMB asim=%s fazla_satis=%d", md, dk, tr, ia, ifz))
			sets = append(sets, "asim_ilkesi=?", "asim_bildirim=?", "fazla_satis=?")
			args = append(args, ia, ib, ifz)
			sets = append(sets, "max_domain=?", "max_disk_mb=?", "max_trafik_mb=?")
			args = append(args, md, dk, tr)
		}
	}
	if req.AdSoyad != nil {
		ad := strings.TrimSpace(*req.AdSoyad)
		if len([]rune(ad)) > 120 {
			httpx.WriteError(w, http.StatusBadRequest, "ad soyad en fazla 120 karakter olabilir")
			return
		}
		sets = append(sets, "full_name=?")
		args = append(args, ad)
	}
	durumDegisti := ""
	if req.Durum != nil {
		if *req.Durum != "active" && *req.Durum != "suspended" {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz durum")
			return
		}
		degisim = append(degisim, fmt.Sprintf("durum %s→%s", onceDurum, *req.Durum))
		sets = append(sets, "status=?")
		args = append(args, *req.Durum)
		durumDegisti = *req.Durum
	}
	if req.MaxDomain != nil {
		// 🔴 Negatif limit "0 = sinirsiz" kapisindan geciyordu → bayi tum kotalari
		// atlatabiliyordu. Ust sinir da var: MySQL INT tasmasi 500 uretiyordu.
		if *req.MaxDomain < 0 || *req.MaxDomain > 100000 {
			httpx.WriteError(w, http.StatusBadRequest, "domain limiti 0-100000 aralığında olmalı")
			return
		}
		degisim = append(degisim, fmt.Sprintf("max_domain %v→%v", onceDomain, *req.MaxDomain))
		sets = append(sets, "max_domain=?")
		args = append(args, *req.MaxDomain)
	}
	if req.MaxDiskMB != nil {
		if *req.MaxDiskMB < 0 || *req.MaxDiskMB > 1<<40 {
			httpx.WriteError(w, http.StatusBadRequest, "disk limiti geçersiz")
			return
		}
		degisim = append(degisim, fmt.Sprintf("max_disk_mb %v→%v", onceDisk, *req.MaxDiskMB))
		sets = append(sets, "max_disk_mb=?")
		args = append(args, *req.MaxDiskMB)
	}
	if req.MaxTrafikMB != nil {
		if *req.MaxTrafikMB < 0 || *req.MaxTrafikMB > 1<<40 {
			httpx.WriteError(w, http.StatusBadRequest, "trafik limiti geçersiz")
			return
		}
		degisim = append(degisim, fmt.Sprintf("max_trafik_mb %v→%v", onceTrafik, *req.MaxTrafikMB))
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
	uid, kul := middleware.Aktor(r)
	var bayiAd string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id=?`, id).Scan(&bayiAd)

	// Parola degisti → acik oturumlar dussun.
	if req.Parola != nil && *req.Parola != "" {
		middleware.OturumlariDusur(h.DB, id)
		httpx.Denetim(h.DB, r, uid, kul, "bayi.parola", bayiAd, "", id, true)
	}

	etkilenen, basarisiz := 0, 0
	if durumDegisti != "" {
		askida := durumDegisti == "suspended"
		if askida {
			// Askiya alinan bayi ACIK oturumla is yapmaya devam edemez.
			middleware.OturumlariDusur(h.DB, id)
		}
		// 🔴 Kilit: askiya alma sirasinda UCUSTA olan "hosting olustur" istegi
		// bitene kadar bekle; aksi halde yeni hosting askidan MUAF canli doguyor
		// ve bayi geri acilinca zincir ona hic dokunmadigi icin kalicilasiyordu.
		k := kilit.Bayi(id)
		k.Lock()
		n, hatali, err := provisioner.BayiAskiUygula(h.DB, id, askida)
		k.Unlock()
		etkilenen, basarisiz = n, hatali
		if err != nil {
			log.Printf("bayi %d aski zinciri: %v", id, err)
		}
		if basarisiz > 0 {
			// DB ile nginx ayristi: sessizce "tamam" deme, operatore soyle.
			log.Printf("bayi %d aski zinciri: %d hosting UYGULANAMADI", id, basarisiz)
		}
		eylem := "bayi.askidan_al"
		if askida {
			eylem = "bayi.askiya_al"
		}
		httpx.Denetim(h.DB, r, uid, kul, eylem, bayiAd,
			fmt.Sprintf("etkilenen hosting=%d basarisiz=%d", etkilenen, basarisiz), id, basarisiz == 0)
	} else {
		httpx.Denetim(h.DB, r, uid, kul, "bayi.guncelle", bayiAd, strings.Join(degisim, ", "), id, true)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": basarisiz == 0, "etkilenen_hosting": etkilenen, "basarisiz_hosting": basarisiz,
	})
}

// DELETE /resellers/{id} — reseller sil (admin). Hosting hesabi varsa reddet.
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	// 🔴 ParseInt hatasi 0'a dusuyordu: /resellers/abc ya da /resellers/0 istegi
	// "DELETE FROM service_plans WHERE reseller_id=0" yoluna, yani KOKUN GLOBAL
	// PLANLARINA gidiyordu. Once id'yi dogrula, sonra satirin gercekten bir bayi
	// oldugunu kontrol et (yoksa 404) — "sahte basari" kaydi da boylece bitiyor.
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz bayi kimliği")
		return
	}
	var rol string
	if e := h.DB.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id=?`, id).Scan(&rol); e != nil || rol != "reseller" {
		httpx.WriteError(w, http.StatusNotFound, "bayi bulunamadı")
		return
	}
	var dom int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE reseller_id=?`, id).Scan(&dom)
	if dom > 0 {
		uid, kul := middleware.Aktor(r)
		httpx.Denetim(h.DB, r, uid, kul, "bayi.sil", strconv.FormatInt(id, 10),
			fmt.Sprintf("reddedildi: %d hosting hesabı var", dom), 0, false)
		httpx.WriteError(w, http.StatusConflict, "önce bu reseller'ın hosting hesaplarını taşıyın/silin")
		return
	}
	var bayiAd string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id=?`, id).Scan(&bayiAd)
	// Bayi + planlari TEK islemde gitsin: arada surec olurse bayi silinip planlari
	// kokun katalogunda oksuz kalmasin.
	tx, err2 := h.DB.BeginTx(r.Context(), nil)
	err = err2
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM users WHERE id=? AND role='reseller'`, id); err != nil {
		_ = tx.Rollback()
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM service_plans WHERE reseller_id=?
		   AND id NOT IN (SELECT COALESCE(plan_id,0) FROM domains WHERE plan_id IS NOT NULL)`, id); err != nil {
		_ = tx.Rollback()
		httpx.WriteError(w, http.StatusInternalServerError, "planlar silinemedi: "+err.Error())
		return
	}
	// Yetim kayitlar: bayi ID'si ileride yeniden kullanilirsa yeni bayi eskinin
	// DNS sablonunu devralir ve denetim kayitlarini gorurdu (kimlik sizintisi).
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM dns_template WHERE reseller_id=?`, id); err != nil {
		log.Printf("bayi %d dns_template temizligi: %v", id, err)
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE audit_log SET reseller_id=0 WHERE reseller_id=?`, id); err != nil {
		log.Printf("bayi %d denetim kapsami anonimlestirilemedi: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
		return
	}
	uid, kul := middleware.Aktor(r)
	// Kapsam KOK: bayi artik yok; ID ileride yeniden kullanilirsa yeni bayi
	// bu kaydi kendi gecmisi sanmasin (kok zaten tum kayitlari gorur).
	httpx.Denetim(h.DB, r, uid, kul, "bayi.sil", bayiAd, fmt.Sprintf("silinen bayi id=%d", id), 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// paketIlkeleri: paketin asim/fazla-satis ilkelerini doner (bayiye KOPYALANIR —
// paket sonradan degisse mevcut bayinin ilkesi kendiliginden degismez).
func (h *Handlers) paketIlkeleri(r *http.Request, paketID int64) (asim string, bildirim, fazlaSatis int) {
	asim, bildirim, fazlaSatis = "disk_trafik", 1, 0
	if paketID <= 0 {
		return
	}
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT asim_ilkesi, asim_bildirim, fazla_satis FROM reseller_plans WHERE id=?`, paketID).
		Scan(&asim, &bildirim, &fazlaSatis)
	return
}
