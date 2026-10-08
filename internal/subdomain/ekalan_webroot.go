package subdomain

// ekalan_webroot.go — ek (addon) alan adı BELGE KÖKÜ (docroot) ayarı. Ana alan adındaki
// domains.web_root ile aynı model ve AYNI kurallar: doğrulama provisioner.WebRootMutlakTaban
// (.. / mutlak yol / garip karakter / leaf + ara symlink / var olan dizin / taban içinde)
// tek kaynaktan; ek alan için taban <ev>/<sk>/domains/<alan>/public_html. Boş değer =
// taban (mevcut davranış). Vhost TEK üreticiden (ekVhostYenidenKur: nginx -t + geri alma).

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"girginospanel/internal/httpx"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"
)

// ekWebRootGuvenli — DB'deki mutlak web_root'u RENDER için güvenli kılar: boş/bozuk/taban
// dışı ise tabana düşer (kurcalanmış DB değeri keyfi yol sunamaz).
func ekWebRootGuvenli(sk, ad, stored string) string {
	return provisioner.GuvenliWebRootTaban(ekDocroot(sk, ad), stored)
}

// ekWebRootOku — ek alan satırındaki ham web_root ("" = taban / sütun yok).
func ekWebRootOku(ctx context.Context, db *sql.DB, ekID int64) string {
	var w string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM ek_alan_adlari WHERE id=?`, ekID).Scan(&w)
	return w
}

type ekWebRootIstek struct {
	AltDizin string `json:"alt_dizin"`
}

// GET /domains/{id}/ek-alan-adlari/{eid}/web-root → mevcut alt dizin + aday dizinler.
func (h *Handlers) EkWebRootGet(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	taban := ekDocroot(s.SK, s.Ad)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alan_adi":  s.Ad,
		"alt_dizin": provisioner.AltDizinCozTaban(taban, ekWebRootOku(r.Context(), h.DB, s.ID)),
		"adaylar":   provisioner.AdayKoklerTaban(taban),
	})
}

// PUT /domains/{id}/ek-alan-adlari/{eid}/web-root {"alt_dizin":"public"} → ek alanın
// public_html'i içindeki alt dizini belge kökü yapar. Boş = ek alanın kökü.
func (h *Handlers) EkWebRootPut(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	if s.Demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin belge kökü değiştirilemez")
		return
	}
	if !reEkSK.MatchString(s.SK) || !ekAdGuvenli(s.Ad) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz hesap")
		return
	}
	var req ekWebRootIstek
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	taban := ekDocroot(s.SK, s.Ad)
	// Ana alanla AYNI doğrulayıcı (traversal / cross-tenant / symlink kapalı, dizin var olmalı).
	abs, verr := provisioner.WebRootMutlakTaban(taban, req.AltDizin)
	if verr != nil {
		httpx.WriteError(w, http.StatusBadRequest, verr.Error())
		return
	}
	// Ek kapı: çözülmüş yol hesap dizininin içinde kalmalı (kök işlem yolu).
	if err := ekYolGuvenli(s.SK, abs); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dizin hesap dizini dışına çıkıyor")
		return
	}
	kaydet := abs
	if abs == taban {
		kaydet = "" // varsayılan: sütun boş kalır (mevcut davranışla birebir aynı vhost)
	}
	eski := ekWebRootOku(r.Context(), h.DB, s.ID)
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ek_alan_adlari SET web_root=? WHERE id=? AND domain_id=?`, kaydet, s.ID, s.DomainID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt güncellenemedi")
		return
	}
	if err := ekVhostYenidenKur(r.Context(), h.DB, s.ID); err != nil {
		// nginx -t / reload kapısı vhost'u zaten geri aldı; DB'yi de eski değere döndür.
		_, _ = h.DB.ExecContext(context.WithoutCancel(r.Context()),
			`UPDATE ek_alan_adlari SET web_root=? WHERE id=?`, eski, s.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "vhost render: "+nginxset.SunucuHatasi(r, "ek-alan.web-root", s.DomainID, err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"alt_dizin": provisioner.AltDizinCozTaban(taban, kaydet),
	})
}
