package subdomain

// Alt alan adı erişim kısıtlama API'si.
//
// GET/PUT  /domains/{id}/subdomain/{sid}/erisim
// POST     /domains/{id}/subdomain/{sid}/erisim/kural
// DELETE   /domains/{id}/subdomain/{sid}/erisim/kural/{kid}
//
// Ana domain tarafıyla AYNI motoru kullanır (provisioner'daki doğrulama,
// normalizasyon, fail-closed varsayılan, bozuk-veri koruması). Ana domainde
// denetimlerin ortaya çıkardığı her ders burada BAŞTAN uygulanmıştır:
//
//   - kanonik CIDR (1.2.3.4/0 -> 0.0.0.0/0), yoksa panel "1 adres" derken
//     nginx tüm interneti açar
//   - render başarısızsa TELAFİ EDİCİ GERİ ALMA — aksi halde "başarısız"
//     denen değişiklik, ilgisiz bir tetikleyiciyle sonra sessizce uygulanır
//   - başarısızlıklar da denetim kaydına yazılır
//   - ham DB/nginx hatası istemciye sızmaz
//   - kural sayısı sınırı (her kural tam render + global nginx -t tetikler)

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type erisimKural struct {
	ID       int64  `json:"id"`
	Tip      string `json:"tip"`
	CIDR     string `json:"cidr"`
	Aciklama string `json:"aciklama"`
	Sira     int    `json:"sira"`
}

// altAlanCoz — {id} ve {sid}'yi çözer ve alt alanın GERÇEKTEN o domaine ait
// olduğunu doğrular.
//
// 🔴 `MusteriScope` yalnız {id}'yi (üst domain sahipliğini) kontrol eder.
// {sid} ayrıca doğrulanmazsa, kendi domainine erişebilen biri BAŞKA bir
// müşterinin alt alanının kısıtını değiştirebilirdi.
func (h *Handlers) altAlanCoz(w http.ResponseWriter, r *http.Request) (domainID, sid int64, sk, altAd, tamAd, php string, ok bool) {
	domainID, sk, _, php, _, pok := h.parent(r)
	if !pok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	sid, _ = strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var sPHP sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alt_ad, tam_ad, php_surum FROM subdomanlar WHERE id=? AND domain_id=?`,
		sid, domainID).Scan(&altAd, &tamAd, &sPHP)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "alt alan bulunamadı")
		return
	}
	if sPHP.Valid && strings.TrimSpace(sPHP.String) != "" {
		php = sPHP.String
	}
	ok = true
	return
}

// denetle — politika değişikliklerini audit_log'a yazar (başarı VE başarısızlık).
func (h *Handlers) erisimDenetle(r *http.Request, domainID int64, eylem, hedef, detay string, basarili bool) {
	var uid int64
	var kullanici string
	if c := middleware.ClaimsFrom(r); c != nil {
		uid, kullanici = c.UserID, c.Username
	} else if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		kullanici = "musteri:" + strconv.FormatInt(mc.DomainID, 10)
	}
	httpx.DenetimDomain(h.DB, r, uid, kullanici, eylem, hedef, detay, domainID, basarili)
}

func kisaHata(err error) string {
	m := err.Error()
	if i := strings.Index(m, "/etc/nginx"); i >= 0 {
		m = m[:i] + "(yapılandırma dosyası)"
	}
	if len(m) > 180 {
		m = m[:180] + "…"
	}
	return m
}

// GET /domains/{id}/subdomain/{sid}/erisim
func (h *Handlers) ErisimGoster(w http.ResponseWriter, r *http.Request) {
	_, sid, _, _, tamAd, _, ok := h.altAlanCoz(w, r)
	if !ok {
		return
	}
	aktif, varsayilan := 0, "izin"
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT aktif, varsayilan FROM subdomain_erisim_kisit WHERE subdomain_id=?`, sid).
		Scan(&aktif, &varsayilan)

	kurallar := []erisimKural{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, tip, cidr, aciklama, sira FROM subdomain_erisim_kurallari
		 WHERE subdomain_id=? ORDER BY sira ASC, id ASC`, sid)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k erisimKural
			if rows.Scan(&k.ID, &k.Tip, &k.CIDR, &k.Aciklama, &k.Sira) == nil {
				kurallar = append(kurallar, k)
			}
		}
	}
	var vekilSayisi int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM guvenilir_vekil`).Scan(&vekilSayisi)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"tam_ad":       tamAd,
		"aktif":        aktif == 1,
		"varsayilan":   varsayilan,
		"kurallar":     kurallar,
		"istemci_ip":   httpx.ClientIP(r),
		"vekil_sayisi": vekilSayisi,
	})
}

// PUT /domains/{id}/subdomain/{sid}/erisim
func (h *Handlers) ErisimKaydet(w http.ResponseWriter, r *http.Request) {
	domainID, sid, sk, altAd, tamAd, php, ok := h.altAlanCoz(w, r)
	if !ok {
		return
	}
	var req struct {
		Aktif      bool   `json:"aktif"`
		Varsayilan string `json:"varsayilan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	vs := strings.ToLower(strings.TrimSpace(req.Varsayilan))
	if vs != "izin" && vs != "red" {
		httpx.WriteError(w, http.StatusBadRequest, "varsayilan 'izin' ya da 'red' olmalı")
		return
	}

	eskiAktif, eskiVarsayilan := 0, "izin"
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT aktif, varsayilan FROM subdomain_erisim_kisit WHERE subdomain_id=?`, sid).
		Scan(&eskiAktif, &eskiVarsayilan)

	aktifVal := 0
	if req.Aktif {
		aktifVal = 1
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO subdomain_erisim_kisit (subdomain_id, aktif, varsayilan) VALUES (?,?,?)
		 ON DUPLICATE KEY UPDATE aktif=VALUES(aktif), varsayilan=VALUES(varsayilan)`,
		sid, aktifVal, vs); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}

	if err := h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php); err != nil {
		// Telafi: önceki durumu geri yükle ve eski hâli yeniden uygula.
		if _, e := h.DB.ExecContext(r.Context(),
			`INSERT INTO subdomain_erisim_kisit (subdomain_id, aktif, varsayilan) VALUES (?,?,?)
			 ON DUPLICATE KEY UPDATE aktif=VALUES(aktif), varsayilan=VALUES(varsayilan)`,
			sid, eskiAktif, eskiVarsayilan); e == nil {
			_ = h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php)
		}
		h.erisimDenetle(r, domainID, "altalan.erisim.ayar", tamAd,
			"render basarisiz, GERI ALINDI: "+err.Error(), false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"ayar uygulanamadı, önceki durum geri yüklendi: "+kisaHata(err))
		return
	}
	h.erisimDenetle(r, domainID, "altalan.erisim.ayar", tamAd,
		"aktif="+strconv.FormatBool(req.Aktif)+" varsayilan="+vs, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "aktif": req.Aktif, "varsayilan": vs})
}

// POST /domains/{id}/subdomain/{sid}/erisim/kural
func (h *Handlers) ErisimKuralEkle(w http.ResponseWriter, r *http.Request) {
	domainID, sid, sk, altAd, tamAd, php, ok := h.altAlanCoz(w, r)
	if !ok {
		return
	}
	var req struct {
		Tip      string `json:"tip"`
		CIDR     string `json:"cidr"`
		Aciklama string `json:"aciklama"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tip := strings.ToLower(strings.TrimSpace(req.Tip))
	if tip != "izin" && tip != "red" {
		httpx.WriteError(w, http.StatusBadRequest, "tip 'izin' ya da 'red' olmalı")
		return
	}
	cidr := provisioner.ErisimKuralNormalize(req.CIDR)
	if !provisioner.ErisimKuralGecerli(cidr) {
		httpx.WriteError(w, http.StatusBadRequest,
			"geçersiz IP/CIDR (örnek: 203.0.113.45 ya da 198.51.100.0/24)")
		return
	}
	aciklama := strings.TrimSpace(req.Aciklama)
	if len(aciklama) > 190 {
		aciklama = aciklama[:190]
	}
	var mevcut int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM subdomain_erisim_kurallari WHERE subdomain_id=?`, sid).Scan(&mevcut)
	if mevcut >= 200 {
		httpx.WriteError(w, http.StatusBadRequest, "kural sınırına ulaşıldı (en fazla 200)")
		return
	}

	var sira int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(sira),0)+10 FROM subdomain_erisim_kurallari WHERE subdomain_id=?`, sid).Scan(&sira)
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO subdomain_erisim_kurallari (subdomain_id, tip, cidr, aciklama, sira) VALUES (?,?,?,?,?)`,
		sid, tip, cidr, aciklama, sira)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			// Benzersizlik anahtari (domain/alt alan + cidr) `tip`'i kapsamiyor;
			// kullanici bir kurali izin<->red cevirmek isteyince buraya dusuyor
			// ve neden oldugunu anlamiyordu. Mesaj eylemi soylesin.
			httpx.WriteError(w, http.StatusConflict,
				"bu IP/CIDR zaten listede — izin/red yönünü değiştirmek için önce mevcut kuralı silin")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	kid, _ := res.LastInsertId()

	if err := h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php); err != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`DELETE FROM subdomain_erisim_kurallari WHERE id=? AND subdomain_id=?`, kid, sid)
		_ = h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php)
		h.erisimDenetle(r, domainID, "altalan.erisim.kural.ekle", tamAd,
			"render basarisiz, GERI ALINDI: "+tip+" "+cidr, false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"kural uygulanamadı, eklenmedi: "+kisaHata(err))
		return
	}
	h.erisimDenetle(r, domainID, "altalan.erisim.kural.ekle", tamAd, tip+" "+cidr, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": kid})
}

// DELETE /domains/{id}/subdomain/{sid}/erisim/kural/{kid}
func (h *Handlers) ErisimKuralSil(w http.ResponseWriter, r *http.Request) {
	domainID, sid, sk, altAd, tamAd, php, ok := h.altAlanCoz(w, r)
	if !ok {
		return
	}
	kid, _ := strconv.ParseInt(chi.URLParam(r, "kid"), 10, 64)

	var yTip, yCidr, yAciklama string
	var ySira int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT tip, cidr, aciklama, sira FROM subdomain_erisim_kurallari WHERE id=? AND subdomain_id=?`,
		kid, sid).Scan(&yTip, &yCidr, &yAciklama, &ySira)

	// subdomain_id koşulu ZORUNLU: yalnız kural id'siyle silmek, başka bir alt
	// alanın kuralını silmeye izin verirdi.
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM subdomain_erisim_kurallari WHERE id=? AND subdomain_id=?`, kid, sid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.WriteError(w, http.StatusNotFound, "kural bulunamadı")
		return
	}
	if err := h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php); err != nil {
		if yCidr != "" {
			_, _ = h.DB.ExecContext(r.Context(),
				`INSERT INTO subdomain_erisim_kurallari (id, subdomain_id, tip, cidr, aciklama, sira)
				 VALUES (?,?,?,?,?,?)`, kid, sid, yTip, yCidr, yAciklama, ySira)
			_ = h.rebuildVhost(r.Context(), sid, sk, altAd, tamAd, php)
		}
		h.erisimDenetle(r, domainID, "altalan.erisim.kural.sil", tamAd,
			"render basarisiz, GERI ALINDI: kural_id="+strconv.FormatInt(kid, 10), false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"kural silinemedi, geri yüklendi: "+kisaHata(err))
		return
	}
	h.erisimDenetle(r, domainID, "altalan.erisim.kural.sil", tamAd,
		"kural_id="+strconv.FormatInt(kid, 10), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
