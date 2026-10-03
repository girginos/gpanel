package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// SOA: per-domain Start of Authority ayarları (düzenlenebilir).
type SOA struct {
	PrimaryNS  string `json:"primary_ns"` // ör. ns1.alan.com
	Hostmaster string `json:"hostmaster"` // e-posta ör. admin@alan.com
	Refresh    int    `json:"refresh"`
	Retry      int    `json:"retry"`
	Expire     int    `json:"expire"`
	Minimum    int    `json:"minimum"`
	TTL        int    `json:"ttl"`
}

func defaultSOA(alanAdi string) SOA {
	return SOA{
		PrimaryNS:  "ns1." + alanAdi,
		Hostmaster: "admin@" + alanAdi,
		Refresh:    3600,
		Retry:      900,
		Expire:     1209600,
		Minimum:    3600,
		TTL:        3600,
	}
}

// LoadSOA: DB'den oku; satır yoksa domain adına göre default üret.
func LoadSOA(ctx context.Context, db *sql.DB, domainID int64, alanAdi string) SOA {
	s := defaultSOA(alanAdi)
	var ns, hm string
	var ref, ret, exp, mini, ttl int
	if err := db.QueryRowContext(ctx,
		`SELECT primary_ns, hostmaster, refresh, retry, expire, minimum, ttl FROM dns_soa WHERE domain_id=?`,
		domainID).Scan(&ns, &hm, &ref, &ret, &exp, &mini, &ttl); err != nil {
		return s
	}
	if ns != "" {
		s.PrimaryNS = ns
	}
	if hm != "" {
		s.Hostmaster = hm
	}
	if ref > 0 {
		s.Refresh = ref
	}
	if ret > 0 {
		s.Retry = ret
	}
	if exp > 0 {
		s.Expire = exp
	}
	if mini > 0 {
		s.Minimum = mini
	}
	if ttl > 0 {
		s.TTL = ttl
	}
	return s
}

// GetSOA: GET /domains/{id}/dns/soa
func (h *Handlers) GetSOA(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, LoadSOA(r.Context(), h.DB, id, alanAdi))
}

// PutSOA: PUT /domains/{id}/dns/soa — SOA'yı güncelle + zone'u yeniden yaz.
func (h *Handlers) PutSOA(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT alan_adi, is_demo FROM domains WHERE id=?`, id).Scan(&alanAdi, &isDemo); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin SOA'sı değiştirilemez")
		return
	}
	var s SOA
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	d := defaultSOA(alanAdi)
	if s.PrimaryNS = strings.TrimSpace(s.PrimaryNS); s.PrimaryNS == "" {
		s.PrimaryNS = d.PrimaryNS
	}
	if s.Hostmaster = strings.TrimSpace(s.Hostmaster); s.Hostmaster == "" {
		s.Hostmaster = d.Hostmaster
	}
	if s.Refresh <= 0 {
		s.Refresh = d.Refresh
	}
	if s.Retry <= 0 {
		s.Retry = d.Retry
	}
	if s.Expire <= 0 {
		s.Expire = d.Expire
	}
	if s.Minimum <= 0 {
		s.Minimum = d.Minimum
	}
	if s.TTL <= 0 {
		s.TTL = d.TTL
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO dns_soa(domain_id, primary_ns, hostmaster, refresh, retry, expire, minimum, ttl)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE primary_ns=VALUES(primary_ns), hostmaster=VALUES(hostmaster),
		   refresh=VALUES(refresh), retry=VALUES(retry), expire=VALUES(expire),
		   minimum=VALUES(minimum), ttl=VALUES(ttl)`,
		id, s.PrimaryNS, s.Hostmaster, s.Refresh, s.Retry, s.Expire, s.Minimum, s.TTL); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	if zerr := WriteZone(zctx, h.DB, id); zerr != nil {
		log.Printf("dns WriteZone(soa) domain=%d: %v", id, zerr)
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}
