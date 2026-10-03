package domainip

// HTTP uçları — HEPSİ YALNIZ YÖNETİCİ (kullanıcı kararı 2026-10-03: alan adı IP
// değiştirme / IP seçimi bayide YOK; yalnız kök):
//   GET /domains/ip-secenekleri  (AdminOnly) — hedef IP'ler (sunucunun tüm global IPv4'leri)
//   PUT /domains/{id}/ip         (AdminOnly) — domain IP değiştir
//   PUT /admin/domains/{id}/ipv4 (AdminOnly) — eski yönetici ucu, aynı çekirdek
// Toplu "ip" işlemi POST /domains/toplu/is (AdminOnly, internal/toplu) üzerindedir.
// Kayıt TEK yerde (Rotalar): main.go ve testler aynı zinciri kullanır.

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

// Rotalar — /api/v1 kimlik doğrulamalı grubuna IP uçlarını AdminOnly ile kaydeder.
// Bayi (ve müşteri) 403 "sadece yöneticiler için" alır; handler'a hiç ulaşmaz.
func (h *Handler) Rotalar(r chi.Router) {
	r.With(middleware.AdminOnly).Put("/domains/{id}/ip", h.DomainIP)
	r.With(middleware.AdminOnly).Get("/domains/ip-secenekleri", h.IPSecenekleri)
}

type ipGovde struct {
	IPv4 string `json:"ipv4"`
}

// DomainIP — gövde {"ipv4":"…"}. Hata sözleşmesi: geçersiz/sunucuda olmayan IP 400;
// domain yok 404. Yetki rota katmanında (AdminOnly).
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

// IPSecenekleri — hedef IP açılır listesi: sunucunun tüm global IPv4'leri ve her
// birindeki domain sayısı.
func (h *Handler) IPSecenekleri(w http.ResponseWriter, r *http.Request) {
	liste, err := IPSecenekListesi(r.Context(), h.DB)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "IP listesi okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ipler": liste})
}
