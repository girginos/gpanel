// Package waf: per-domain WAF (ModSecurity + OWASP CRS) ayar API'si.
//
// GET/PUT /domains/{id}/waf — domain WAF modunu (devral/kapali/engelle/denetle) + paranoya
// seviyesini okur/yazar. Yazinca provisioner.WAFUygula ile per-domain modsec conf tazelenir
// + vhost yeniden render edilir (nginx -t gate + rollback). Modul yuklu degilse ayar yine de
// kaydedilir (persist) fakat render WAF'i graceful atlar — cevapta modul_yuklu bilgisi doner.
package waf

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// wafUygula — vhost yeniden render (sunucu geneli `nginx -t` + reload). Testler
// sahteyle değiştirir: hata yolu ana makinenin nginx'ine dokunmadan sınanır.
var wafUygula = provisioner.WAFUygula

type Handlers struct {
	DB *sql.DB
}

// Ayar: domain seviyesi WAF override'i (UI ile birebir).
//
//	mod: "devral" (plandan miras) | "kapali" | "engelle" (On) | "denetle" (DetectionOnly)
//	paranoya: 0 = devral, 1..4 = override
type Ayar struct {
	Mod      string `json:"mod"`
	Paranoya int    `json:"paranoya"`
}

type modBilgi struct {
	Aktif    bool   `json:"aktif"`
	Mod      string `json:"mod"`
	Paranoya int    `json:"paranoya"`
	Ad       string `json:"ad,omitempty"`
}

type efektifBilgi struct {
	Aktif    bool   `json:"aktif"`
	Engine   string `json:"engine"`
	Paranoya int    `json:"paranoya"`
}

// GET /domains/{id}/waf
func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, sk string
	var dEn, dPL sql.NullInt64
	var dMode sql.NullString
	var pEn sql.NullInt64
	var pMode sql.NullString
	var pPL sql.NullInt64
	var pAd sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.alan_adi, d.sistem_kullanici, d.waf_enabled, d.waf_mode, d.waf_paranoia,
		        p.waf_enabled, p.waf_mode, p.waf_paranoia, p.ad
		 FROM domains d LEFT JOIN service_plans p ON p.id = d.plan_id
		 WHERE d.id = ?`, id).
		Scan(&alanAdi, &sk, &dEn, &dMode, &dPL, &pEn, &pMode, &pPL, &pAd)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Domain override → Ayar
	ay := Ayar{Mod: "devral", Paranoya: 0}
	if dEn.Valid {
		if int(dEn.Int64) != 1 {
			ay.Mod = "kapali"
		} else if dMode.Valid && strings.ToLower(strings.TrimSpace(dMode.String)) == "detect" {
			ay.Mod = "denetle"
		} else {
			ay.Mod = "engelle"
		}
	}
	if dPL.Valid && dPL.Int64 > 0 {
		ay.Paranoya = int(dPL.Int64)
	}

	// Plan varsayilani (bilgi amacli)
	plan := modBilgi{Aktif: false, Mod: "kapali", Paranoya: 1}
	if pAd.Valid {
		plan.Ad = pAd.String
	}
	if pPL.Valid && pPL.Int64 > 0 {
		plan.Paranoya = int(pPL.Int64)
	}
	if pEn.Valid && pEn.Int64 == 1 {
		plan.Aktif = true
		m := "engelle"
		if pMode.Valid && strings.ToLower(strings.TrimSpace(pMode.String)) == "detect" {
			m = "denetle"
		}
		plan.Mod = m
	}

	// Efektif (provisioner ile ayni cozumleyici → drift yok)
	efAktif, efEngine, efPL := provisioner.WAFEfektif(h.DB, sk)
	ef := efektifBilgi{Aktif: efAktif, Engine: efEngine, Paranoya: efPL}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alan_adi":    alanAdi,
		"ayar":        ay,
		"plan":        plan,
		"efektif":     ef,
		"modul_yuklu": provisioner.WAFModulYuklu(),
	})
}

// PUT /domains/{id}/waf   body: {"ayar": {"mod":"engelle","paranoya":1}}
func (h *Handlers) Kaydet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		Ayar Ayar `json:"ayar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}

	var sk string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		} else {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// mod → (waf_enabled, waf_mode) ; nil = NULL = plandan devral
	var enVal, modeVal interface{}
	switch strings.ToLower(strings.TrimSpace(req.Ayar.Mod)) {
	case "devral", "":
		enVal, modeVal = nil, nil
	case "kapali":
		enVal, modeVal = 0, "off"
	case "engelle":
		enVal, modeVal = 1, "on"
	case "denetle":
		enVal, modeVal = 1, "detect"
	default:
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz mod (devral|kapali|engelle|denetle)")
		return
	}
	var plVal interface{}
	if req.Ayar.Paranoya >= 1 && req.Ayar.Paranoya <= 4 {
		plVal = req.Ayar.Paranoya
	} else {
		plVal = nil // devral
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET waf_enabled=?, waf_mode=?, waf_paranoia=? WHERE id=?`,
		enVal, modeVal, plVal, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}

	// Vhost'u yeniden render et — nginx -t gate + rollback koruyor. Modul yoksa graceful atlanir.
	// 🔴 Ham hata `nginx -t` çıktısını (komşu kiracıların vhost/sertifika
	// yolları) taşır → yönetici olmayana genel metin, ayrıntı günlüğe (F1).
	if err := wafUygula(h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"WAF ayarı kaydedildi ancak vhost render başarısız (nginx değişmedi): "+nginxset.SunucuHatasi(r, "waf", id, err))
		return
	}

	efAktif, efEngine, efPL := provisioner.WAFEfektif(h.DB, sk)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"efektif":     efektifBilgi{Aktif: efAktif, Engine: efEngine, Paranoya: efPL},
		"modul_yuklu": provisioner.WAFModulYuklu(),
	})
}
