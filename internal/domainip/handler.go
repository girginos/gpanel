package domainip

// HTTP uçları:
//   GET /domains/ip-secenekleri  (AdminVeyaReseller) — rolün seçebileceği hedef IP'ler
//   PUT /domains/{id}/ip         (SahipYonetim)      — domain IP değiştir
//   PUT /admin/domains/{id}/ipv4 (AdminOnly)         — eski yönetici ucu, aynı çekirdek

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
)

type Handler struct{ DB *sql.DB }

type ipGovde struct {
	IPv4 string `json:"ipv4"`
}

// DomainIP — gövde {"ipv4":"…"}. Hata sözleşmesi: geçersiz/sunucuda olmayan IP 400;
// sunucuda var ama bayiye açık değil 403; başka bayinin domaini SahipYonetim'de 403.
func (h *Handler) DomainIP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz domain id")
		return
	}
	var g ipGovde
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	yeni := NormIP(g.IPv4)
	if yeni == "" {
		httpx.WriteError(w, http.StatusBadRequest, ErrGecersizIP.Error())
		return
	}
	if !SunucudaMi(yeni) {
		httpx.WriteError(w, http.StatusBadRequest, ErrSunucudaYok.Error())
		return
	}
	if middleware.RolFrom(r) != "admin" {
		// Bayi: sunucudaki bir IP olsa bile yalnız kendisine açık olanlar.
		izinli, err := IzinliIPler(r.Context(), h.DB, false, middleware.ResellerIDFrom(r))
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "IP listesi okunamadı")
			return
		}
		ok := false
		for _, s := range izinli {
			if s.IP == yeni {
				ok = true
				break
			}
		}
		if !ok {
			httpx.WriteError(w, http.StatusForbidden, "bu IP hesabınızın kullanımına açık değil")
			return
		}
	}
	ctx, iptal := context.WithTimeout(r.Context(), 60*time.Second)
	defer iptal()
	sonuc, err := DomainIPDegistir(ctx, h.DB, id, yeni)
	uid, aktor := middleware.Aktor(r)
	detay := fmt.Sprintf("%s → %s (A:%d SPF:%d)", sonuc.EskiIP, yeni, sonuc.DNSKayit, sonuc.SPFKayit)
	switch {
	case errors.Is(err, ErrDomainYok):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, ErrGecersizIP), errors.Is(err, ErrSunucudaYok):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.DenetimDomain(h.DB, r, uid, aktor, "domain.ip_degistir", sonuc.AlanAdi, detay+": "+err.Error(), id, false)
		httpx.WriteError(w, http.StatusInternalServerError, nginxset.SunucuHatasi(r, "domain.ip", id, err))
		return
	}
	if sonuc.Degisti {
		httpx.DenetimDomain(h.DB, r, uid, aktor, "domain.ip_degistir", sonuc.AlanAdi, detay, id, true)
	}
	httpx.WriteJSON(w, http.StatusOK, sonuc)
}

// IPSecenekleri — hedef IP açılır listesi. Yönetici tüm sunucu IP'lerini, bayi
// yalnız kendisine açık olanları görür; kullanım sayısı rolün kapsamıdır.
func (h *Handler) IPSecenekleri(w http.ResponseWriter, r *http.Request) {
	admin := middleware.RolFrom(r) == "admin"
	liste, err := IzinliIPler(r.Context(), h.DB, admin, middleware.ResellerIDFrom(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "IP listesi okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ipler": liste})
}
