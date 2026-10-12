// Package toplu — domainlerde TOPLU islemler (DNS varsayilan reset, SSL kurulumu,
// sahiplik, plan ve IP degisimi). Islemler ASENKRON calisir: istek aninda is kaydi
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
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/dns"
	"girginospanel/internal/domainip"
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
	IPv4       string  `json:"ipv4"` // tip "ip": hedef IP (sunucuda olmalı)
}

var gecerliTip = map[string]bool{"dns_reset": true, "ssl_kur": true, "sahip": true, "plan": true, "ip": true}

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
		if req.PlanID == nil || *req.PlanID <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
		if kod, mesaj := planHedeflereUygun(r.Context(), h.DB, *req.PlanID, req.IDs); kod != 0 {
			httpx.WriteError(w, kod, mesaj)
			return
		}
	}

	// IP: hedef sunucunun gerçekten sahip olduğu bir IPv4 olmalı (istemciye güvenme).
	// Çekirdek (domainip.DomainIPDegistir) her domainde yeniden doğrular.
	if req.Tip == "ip" {
		req.IPv4 = domainip.NormIP(req.IPv4)
		if req.IPv4 == "" {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz IPv4 adresi")
			return
		}
		if !domainip.SunucudaMi(req.IPv4) {
			httpx.WriteError(w, http.StatusBadRequest, "bu IP sunucuda tanımlı değil")
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

// planHedeflereUygun: toplu plan değişiminde plan seçilen hostinglerin HEPSİNE uygun mu?
//   - Yalnız katalog planı: eski hostinge özel kopya toplu olarak başka hostinglere
//     bağlanamaz (D1). Hostinge özel limitler (üst yazım) plan değişiminde korunur.
//   - Kökün planı (reseller_id=0) her hostinge atanabilir (bayiler kökün paylaşılan
//     planlarını da seçebilir).
//   - Bir BAYİNİN planı yalnız o bayinin hostinglerine atanabilir. Eskiden yalnız planın
//     varlığına bakılıyordu: kök, A bayisinin planını kendi ya da B bayisinin
//     hostinglerine bağlayabiliyordu (A planını düzenleyince başkasının hostingi değişirdi).
//
// (0,"") = uygun. Ölçüm hatası = red (fail-closed).
func planHedeflereUygun(ctx context.Context, db *sql.DB, planID int64, ids []int64) (int, string) {
	var planRID int64
	err := db.QueryRowContext(ctx,
		"SELECT reseller_id FROM service_plans WHERE id=? AND domain_id IS NULL", planID).Scan(&planRID)
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusBadRequest, "plan bulunamadı"
	}
	if err != nil {
		return http.StatusInternalServerError, "plan okunamadı"
	}
	if planRID == 0 || len(ids) == 0 {
		return 0, ""
	}
	yer := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, planRID)
	for _, id := range ids {
		args = append(args, id)
	}
	var uyumsuz int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM domains WHERE reseller_id<>? AND id IN ("+yer+")", args...).Scan(&uyumsuz); err != nil {
		return http.StatusInternalServerError, "hostingler denetlenemedi"
	}
	if uyumsuz > 0 {
		return http.StatusBadRequest, fmt.Sprintf("bu plan bir bayiye ait; yalnız o bayinin hostinglerine atanabilir (%d hosting uygun değil)", uyumsuz)
	}
	return 0, ""
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
			if _, e := h.DB.Exec("UPDATE cp_toplu_isler SET durum='iptal', bitis=NOW() WHERE id=?", isID); e != nil {
				log.Printf("toplu: iş iptal durumu yazılamadı (is=%d): %v", isID, e)
			}
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
		if _, e := h.DB.Exec("UPDATE cp_toplu_isler SET aktif_domain=? WHERE id=?", alanAdi, isID); e != nil {
			log.Printf("toplu: aktif_domain güncellenemedi (is=%d): %v", isID, e)
		}

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
		case "ip":
			var ds domainip.DegisimSonuc
			ds, err = domainip.DomainIPDegistir(ctx, h.DB, did, req.IPv4)
			if err == nil {
				if !ds.Degisti {
					detay = append(detay, alanAdi+": zaten "+req.IPv4)
				}
				for _, u := range ds.Uyarilar {
					detay = append(detay, alanAdi+": "+u)
				}
				if ds.Degisti {
					httpx.DenetimSistem(h.DB, aktorUID, aktorAd, "domain.ip_degistir", alanAdi,
						fmt.Sprintf("toplu #%d: %s → %s (A:%d SPF:%d)", isID, ds.EskiIP, ds.YeniIP, ds.DNSKayit, ds.SPFKayit),
						httpx.DomainKapsam(h.DB, did), true)
				}
			}
		}
		if err != nil {
			hata++
			detay = append(detay, alanAdi+": "+kisalt(err.Error(), 160))
		} else {
			basari++
		}
		if _, e := h.DB.Exec("UPDATE cp_toplu_isler SET tamamlanan=?, basari=?, hata=? WHERE id=?", i+1, basari, hata, isID); e != nil {
			log.Printf("toplu: ilerleme yazılamadı (is=%d): %v", isID, e)
		}
	}

	durum := "tamam"
	if hata > 0 && basari == 0 {
		durum = "hata"
	} else if hata > 0 {
		durum = "kismi"
	}
	dj, _ := json.Marshal(detay)
	if _, e := h.DB.Exec("UPDATE cp_toplu_isler SET durum=?, aktif_domain='', detay=?, bitis=NOW() WHERE id=?", durum, string(dj), isID); e != nil {
		log.Printf("toplu: iş sonucu yazılamadı (is=%d): %v", isID, e)
	}
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
