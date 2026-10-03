package uygulama

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

type Handler struct {
	DB *sql.DB
	// DomainAl — HTTP context'inden aktif tenant domain'i çöz (mevcut middleware.Aktor deseni)
	DomainAl func(r *http.Request) (Domain, error)
	// MySQL fabrika fn'ler (opsiyonel — production'da hesaplar paketi enjekte)
	MySQLOlustur func(domainID int64, dbAd, dbUser, dbSifre string) error
	MySQLDrop    func(domainID int64, dbAd, dbUser string) error
}

func (h *Handler) Katalog(w http.ResponseWriter, _ *http.Request) {
	yaz(w, 200, map[string]any{"items": Katalog})
}

func (h *Handler) Liste(w http.ResponseWriter, r *http.Request) {
	d, err := h.DomainAl(r)
	if err != nil {
		yazHata(w, 403, err.Error())
		return
	}
	items, err := Liste(r.Context(), h.DB, d.ID)
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, map[string]any{"items": items, "domain": d.AlanAdi})
}

type kurGovde struct {
	Kod      string `json:"kod"`
	AltDizin string `json:"alt_dizin"`
	DBAdi    string `json:"db_adi,omitempty"`
	DBSifre  string `json:"db_sifre,omitempty"`
}

func (h *Handler) Kur(w http.ResponseWriter, r *http.Request) {
	d, err := h.DomainAl(r)
	if err != nil {
		yazHata(w, 403, err.Error())
		return
	}
	var g kurGovde
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		yazHata(w, 400, "geçersiz gövde")
		return
	}
	tarif := KatalogAra(g.Kod)
	if tarif == nil {
		yazHata(w, 404, "recipe bulunamadı: "+g.Kod)
		return
	}
	// İndirme + açma + DB çoğu zaman 30 sn'yi aşar: istemci koparsa r.Context() işi
	// iptal edip geri alıyordu (büyük uygulama hiç kurulamıyordu). Kopuk bağlam.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	kayit, err := Kur(kctx, h.DB, KurArgs{
		Domain: d, Tarif: tarif, AltDizin: g.AltDizin,
		DBAdi: g.DBAdi, DBSifre: g.DBSifre,
	}, h.MySQLOlustur, h.MySQLDrop)
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 201, kayit)
}

func (h *Handler) Sil(w http.ResponseWriter, r *http.Request) {
	d, err := h.DomainAl(r)
	if err != nil {
		yazHata(w, 403, err.Error())
		return
	}
	// URL: /domains/{id}/uygulamalar/{kayit_id}
	parcalar := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parcalar) < 1 {
		yazHata(w, 400, "kayit_id gerekli")
		return
	}
	kid, err := strconv.ParseInt(parcalar[len(parcalar)-1], 10, 64)
	if err != nil || kid <= 0 {
		yazHata(w, 400, "geçersiz kayit_id")
		return
	}
	// Dosya + DB silindikten sonra kayıt da silinmeli (kopuk bağlam; yetim kayıt kalmaz).
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if err := Sil(kctx, h.DB, kid, d, h.MySQLDrop); err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, map[string]any{"ok": true})
}

// kopukSure — istemciden kopuk kurulum/silme zincirinin üst süresi.
const kopukSure = 15 * time.Minute

func yaz(w http.ResponseWriter, k int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(k)
	_ = json.NewEncoder(w).Encode(v)
}
func yazHata(w http.ResponseWriter, k int, m string) {
	yaz(w, k, map[string]string{"error": m})
}

// Ensure errors used
var _ = errors.New
var _ = fmt.Sprintf
