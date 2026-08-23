// Package toplu — domainlerde TOPLU islemler (DNS varsayilan reset, SSL kurulumu,
// sahiplik ve plan degisimi). Islemler ASENKRON calisir: istek aninda is kaydi
// acilir ve is_id doner; UI ilerlemeyi poll ederek process bar gosterir.
//
// 🔴 NEDEN ASENKRON: 20 domain icin Let's Encrypt kurulumu dakikalar surer. Senkron
// yapilirsa HTTP istegi zaman asimina ugrar, kullanici sayfayi kapatinca islem yarim
// kalir ve hangi domainin islendigi bilinmez. Is kaydi DB'de tutuldugu icin sayfa
// yenilense de (or. SSL degisiminde nginx reload) takip kaldigi yerden surer.
package toplu

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/dns"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
)

type Handlers struct{ DB *sql.DB }

// Ayni anda TEK toplu is: SSL/DNS islemleri nginx reload + acme cagirir; paralel
// calisirsa reload yarisir ve rate-limit'e girilir.
var (
	kilit    sync.Mutex
	aktifIs  int64
	iptalCtx = map[int64]context.CancelFunc{}
)

type baslatReq struct {
	Tip        string  `json:"tip"`
	IDs        []int64 `json:"ids"`
	ResellerID *int64  `json:"reseller_id"`
	CustomerID *int64  `json:"customer_id"`
	PlanID     *int64  `json:"plan_id"`
}

var gecerliTip = map[string]bool{"dns_reset": true, "ssl_kur": true, "sahip": true, "plan": true}

// Baslat: POST /domains/toplu/is
func (h *Handlers) Baslat(w http.ResponseWriter, r *http.Request) {
	var req baslatReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	if !gecerliTip[req.Tip] {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz işlem tipi")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "domain seçilmedi")
		return
	}
	if len(req.IDs) > 500 {
		httpx.WriteError(w, http.StatusBadRequest, "tek seferde en fazla 500 domain")
		return
	}
	// Sahiplik/plan hedefleri gecerli mi (yanlis id ile toplu bozma olmasin).
	if req.Tip == "sahip" {
		if req.ResellerID != nil && *req.ResellerID > 0 && !varMi(h.DB, "SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'", *req.ResellerID) {
			httpx.WriteError(w, http.StatusBadRequest, "bayi bulunamadı")
			return
		}
		if req.CustomerID != nil && *req.CustomerID > 0 && !varMi(h.DB, "SELECT COUNT(*) FROM customers WHERE id=?", *req.CustomerID) {
			httpx.WriteError(w, http.StatusBadRequest, "müşteri bulunamadı")
			return
		}
	}
	if req.Tip == "plan" {
		if req.PlanID == nil || *req.PlanID <= 0 || !varMi(h.DB, "SELECT COUNT(*) FROM service_plans WHERE id=?", *req.PlanID) {
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
	}

	kilit.Lock()
	if aktifIs != 0 {
		kilit.Unlock()
		httpx.WriteError(w, http.StatusConflict, "zaten çalışan bir toplu işlem var")
		return
	}
	aktifIs = -1 // rezerve
	kilit.Unlock()

	param, _ := json.Marshal(req)
	aktorUID, aktor := middleware.Aktor(r)
	res, err := h.DB.Exec(
		"INSERT INTO cp_toplu_isler (tip, durum, toplam, parametre, baslatan) VALUES (?, 'calisiyor', ?, ?, ?)",
		req.Tip, len(req.IDs), string(param), aktor)
	if err != nil {
		kilit.Lock()
		aktifIs = 0
		kilit.Unlock()
		httpx.WriteError(w, http.StatusInternalServerError, "iş kaydı açılamadı")
		return
	}
	isID, _ := res.LastInsertId()

	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Hour)
	kilit.Lock()
	aktifIs = isID
	iptalCtx[isID] = iptal
	kilit.Unlock()

	go h.calistir(ctx, isID, req, aktorUID, aktor)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"is_id": isID, "toplam": len(req.IDs)})
}

func varMi(db *sql.DB, q string, arg any) bool {
	var n int
	_ = db.QueryRow(q, arg).Scan(&n)
	return n > 0
}

// calistir — is yurutucusu. Her domain icin tipe gore islemi yapar, ilerlemeyi
// DB'ye yazar (UI poll eder). Hata TEK domaini etkiler; is devam eder.
func (h *Handlers) calistir(ctx context.Context, isID int64, req baslatReq, aktorUID int64, aktorAd string) {
	defer func() {
		kilit.Lock()
		if aktifIs == isID {
			aktifIs = 0
		}
		delete(iptalCtx, isID)
		kilit.Unlock()
	}()

	basari, hata := 0, 0
	var detay []string
	for i, did := range req.IDs {
		if ctx.Err() != nil {
			h.DB.Exec("UPDATE cp_toplu_isler SET durum='iptal', bitis=NOW() WHERE id=?", isID)
			return
		}
		var alanAdi, sk, php, backend string
		if err := h.DB.QueryRowContext(ctx,
			"SELECT alan_adi, sistem_kullanici, COALESCE(php_surum,'8.3'), COALESCE(NULLIF(web_backend,''),'php-fpm') FROM domains WHERE id=?",
			did).Scan(&alanAdi, &sk, &php, &backend); err != nil {
			hata++
			detay = append(detay, fmt.Sprintf("#%d: domain bulunamadı", did))
			continue
		}
		h.DB.Exec("UPDATE cp_toplu_isler SET aktif_domain=? WHERE id=?", alanAdi, isID)

		var err error
		switch req.Tip {
		case "dns_reset":
			err = h.dnsReset(ctx, did, alanAdi)
		case "ssl_kur":
			err = h.sslKur(ctx, did, alanAdi, sk, php, backend)
		case "sahip":
			var ds *devirSonuc
			ds, err = h.devret(ctx, did, alanAdi, sk, req.ResellerID, req.CustomerID, aktorUID, aktorAd)
			if err == nil && ds != nil {
				// Yeni FTP/SSH parolası admin'e SADECE burada gösterilir —
				// rotasyon yapılıp parola söylenmezse hesap erişilemez olurdu.
				if ds.YeniFTPParola != "" {
					detay = append(detay, alanAdi+": yeni FTP/SSH parolası: "+ds.YeniFTPParola)
				}
				for _, u := range ds.Uyarilar {
					detay = append(detay, alanAdi+": "+u)
				}
				// Kritik: sahiplik değişti ama erişim TAM kesilemedi (ör. SSH
				// parolası eşitlenemedi). Sessizce "başarılı" saymak yanlış
				// güven üretir → bu domaini hata olarak işaretle.
				if ds.Kritik {
					err = fmt.Errorf("devir kısmi: erişim tam kesilemedi (detaylara bakın)")
				}
			}
		case "plan":
			err = h.planDegistir(ctx, did, *req.PlanID)
		}
		if err != nil {
			hata++
			detay = append(detay, alanAdi+": "+kisalt(err.Error(), 160))
		} else {
			basari++
		}
		h.DB.Exec("UPDATE cp_toplu_isler SET tamamlanan=?, basari=?, hata=? WHERE id=?", i+1, basari, hata, isID)
	}

	durum := "tamam"
	if hata > 0 && basari == 0 {
		durum = "hata"
	} else if hata > 0 {
		durum = "kismi"
	}
	dj, _ := json.Marshal(detay)
	h.DB.Exec("UPDATE cp_toplu_isler SET durum=?, aktif_domain='', detay=?, bitis=NOW() WHERE id=?", durum, string(dj), isID)
}

// dnsReset — domainin TUM DNS kayitlarini silip sablondan yeniden uretir.
func (h *Handlers) dnsReset(ctx context.Context, did int64, alanAdi string) error {
	var ipv4 string
	_ = h.DB.QueryRowContext(ctx, "SELECT COALESCE(ipv4,'') FROM domains WHERE id=?", did).Scan(&ipv4)
	if _, err := h.DB.ExecContext(ctx, "DELETE FROM dns_records WHERE domain_id=?", did); err != nil {
		return fmt.Errorf("mevcut kayıtlar silinemedi: %w", err)
	}
	n, err := dns.SeedDefaults(ctx, h.DB, did, alanAdi, ipv4)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("şablondan kayıt üretilmedi")
	}
	return nil
}

// sslKur — Let's Encrypt. Zaten gecerli cert varsa acme.sh yeniden almaz (idempotent).
func (h *Handlers) sslKur(ctx context.Context, did int64, alanAdi, sk, php, backend string) error {
	crt, key, err := provisioner.EnableLetsEncrypt(alanAdi, sk, php, backend)
	if err != nil {
		return err
	}
	_, err = h.DB.ExecContext(ctx,
		"UPDATE domains SET ssl_aktif=1, ssl_kaynak='letsencrypt', cert_path=?, key_path=? WHERE id=?",
		crt, key, did)
	return err
}

// sahipDegistir KALDIRILDI → devir.go:devret(). Sahiplik devri artık yalnız
// iki kolonun UPDATE'i değil; kimlik rotasyonu + capability iptali + denetim
// içeren tek bir yaşam-döngüsü işlemidir.

// planDegistir — plan_id'yi yazar ve YENI plan limitlerini UYGULAR (disk/inode/FPM).
func (h *Handlers) planDegistir(ctx context.Context, did, planID int64) error {
	if _, err := h.DB.ExecContext(ctx, "UPDATE domains SET plan_id=? WHERE id=?", planID, did); err != nil {
		return err
	}
	limCtx, iptal := context.WithTimeout(ctx, 60*time.Second)
	defer iptal()
	return kaynaklimit.UygulaHepsi(limCtx, h.DB, did)
}

// Durum: GET /domains/toplu/is?id=  — UI 1-2 sn'de bir poll eder.
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "id gerekli")
		return
	}
	var tip, durum, aktif, detay, baslatan string
	var toplam, tamamlanan, basari, hata int
	var bitis sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT tip, durum, toplam, tamamlanan, basari, hata, aktif_domain, COALESCE(detay,''), baslatan, DATE_FORMAT(bitis,'%Y-%m-%d %H:%i:%s') FROM cp_toplu_isler WHERE id=?",
		id).Scan(&tip, &durum, &toplam, &tamamlanan, &basari, &hata, &aktif, &detay, &baslatan, &bitis)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı")
		return
	}
	var detaylar []string
	if detay != "" {
		_ = json.Unmarshal([]byte(detay), &detaylar)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "tip": tip, "durum": durum, "toplam": toplam, "tamamlanan": tamamlanan,
		"basari": basari, "hata": hata, "aktif_domain": aktif, "detaylar": detaylar,
		"baslatan": baslatan, "bitis": bitis.String,
	})
}

// Iptal: POST /domains/toplu/is/iptal?id=
func (h *Handlers) Iptal(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	kilit.Lock()
	iptal, ok := iptalCtx[id]
	kilit.Unlock()
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "çalışan iş yok")
		return
	}
	iptal()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Aktif: GET /domains/toplu/aktif — sayfa yenilenince devam eden isi bul (SSL
// degisiminde nginx reload olunca sayfa yeniden yuklenebiliyor).
func (h *Handlers) Aktif(w http.ResponseWriter, r *http.Request) {
	var id int64
	_ = h.DB.QueryRowContext(r.Context(),
		"SELECT id FROM cp_toplu_isler WHERE durum='calisiyor' ORDER BY id DESC LIMIT 1").Scan(&id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"is_id": id})
}

func kisalt(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > n {
		return s[:n]
	}
	return s
}
