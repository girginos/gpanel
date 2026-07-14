// Package dns: per-domain DNS kayit yonetimi (sablon)
// Not: BIND/PowerDNS henuz kurulu degil. Kayitlar DB'de tutulur,
// kullanici kendi DNS saglayicisina kopyalayabilir; ileride zone file/PowerDNS API yazimi eklenebilir.
package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Kayit struct {
	ID        int64  `json:"id"`
	DomainID  int64  `json:"domain_id"`
	Ad        string `json:"ad"`
	Tip       string `json:"tip"`
	Deger     string `json:"deger"`
	TTL       int    `json:"ttl"`
	Oncelik   int    `json:"oncelik"`
	Aktif     bool   `json:"aktif"`
	Olusturma string `json:"olusturma"`
}

var GecerliTipler = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA", "PTR", "DS", "TLSA", "SSHFP", "NAPTR"}

type Handlers struct {
	DB *sql.DB
}

const selectAll = `SELECT id, domain_id, ad, tip, deger, ttl, oncelik, aktif,
  DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM dns_records`

func scan(rs interface{ Scan(...any) error }) (Kayit, error) {
	var k Kayit
	var ak int
	err := rs.Scan(&k.ID, &k.DomainID, &k.Ad, &k.Tip, &k.Deger, &k.TTL, &k.Oncelik, &ak, &k.Olusturma)
	k.Aktif = ak == 1
	return k, err
}

func (h *Handlers) lookup(r *http.Request) (string, bool, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, is_demo FROM domains WHERE id=?`, id).Scan(&alanAdi, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, os.ErrNotExist
	}
	return alanAdi, isDemo == 1, err
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := h.DB.QueryContext(r.Context(), selectAll+" WHERE domain_id=? ORDER BY tip, ad", id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := make([]Kayit, 0)
	for rows.Next() {
		k, err := scan(rows)
		if err == nil {
			out = append(out, k)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var k Kayit
	if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	k.Tip = strings.ToUpper(strings.TrimSpace(k.Tip))
	if !gecerliTip(k.Tip) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz DNS tipi")
		return
	}
	if k.Ad == "" {
		k.Ad = "@"
	}
	if k.TTL <= 0 {
		k.TTL = 3600
	}
	ak := 1
	if !k.Aktif && k.Aktif != true {
		// JSON'da aktif false ise 0 yaz, default true (yeni eklemede çoğunlukla aktif)
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
		 VALUES(?,?,?,?,?,?,?)`,
		id, k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik, ak)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nid, _ := res.LastInsertId()
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", nid)
	saved, _ := scan(row)
	_ = WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusCreated, saved)
}

func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid, _ := strconv.ParseInt(chi.URLParam(r, "rid"), 10, 64)
	_, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var k Kayit
	if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	k.Tip = strings.ToUpper(strings.TrimSpace(k.Tip))
	if !gecerliTip(k.Tip) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz DNS tipi")
		return
	}
	ak := 0
	if k.Aktif {
		ak = 1
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE dns_records SET ad=?, tip=?, deger=?, ttl=?, oncelik=?, aktif=?
		 WHERE id=? AND domain_id=?`,
		k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik, ak, rid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", rid)
	saved, _ := scan(row)
	_ = WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid, _ := strconv.ParseInt(chi.URLParam(r, "rid"), 10, 64)
	_, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM dns_records WHERE id=? AND domain_id=?`, rid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// TopluSil: birden fazla DNS kaydini tek istekte sil.
// POST /domains/{id}/dns/toplu-sil  {"ids":[1,2,3]}
func (h *Handlers) TopluSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kayıt seçilmedi")
		return
	}
	ph := make([]string, len(req.IDs))
	args := make([]any, 0, len(req.IDs)+1)
	for i, rid := range req.IDs {
		ph[i] = "?"
		args = append(args, rid)
	}
	args = append(args, id)
	res, err := h.DB.ExecContext(r.Context(),
		"DELETE FROM dns_records WHERE id IN ("+strings.Join(ph, ",")+") AND domain_id=?", args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	_ = WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": n})
}

// TopluDurum: secili kayitlari topluca aktif/pasif yap.
// POST /domains/{id}/dns/toplu-durum  {"ids":[1,2],"aktif":true}
func (h *Handlers) TopluDurum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var req struct {
		IDs   []int64 `json:"ids"`
		Aktif bool    `json:"aktif"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kayıt seçilmedi")
		return
	}
	ak := 0
	if req.Aktif {
		ak = 1
	}
	ph := make([]string, len(req.IDs))
	args := make([]any, 0, len(req.IDs)+2)
	args = append(args, ak)
	for i, rid := range req.IDs {
		ph[i] = "?"
		args = append(args, rid)
	}
	args = append(args, id)
	res, err := h.DB.ExecContext(r.Context(),
		"UPDATE dns_records SET aktif=? WHERE id IN ("+strings.Join(ph, ",")+") AND domain_id=?", args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	_ = WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "guncellenen": n})
}

// Sablonu uygula: 6 default kayit ekle (idempotent — varsa atla)
func (h *Handlers) ApplyTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe şablon uygulanamaz")
		return
	}
	var ipv4 string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT ipv4 FROM domains WHERE id=?`, id).Scan(&ipv4)
	n, err := SeedDefaults(r.Context(), h.DB, id, alanAdi, ipv4)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = WriteZone(r.Context(), h.DB, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eklenen": n})
}

// SeedDefaults: domain icin standart DNS sablonu (idempotent)
func SeedDefaults(ctx context.Context, db *sql.DB, domainID int64, alanAdi, ipv4 string) (int, error) {
	if ipv4 == "" {
		ipv4 = "127.0.0.1"
	}
	tmpl := []struct {
		Ad, Tip, Deger string
		TTL, Oncelik   int
	}{
		{"@", "A", ipv4, 3600, 0},
		{"www", "A", ipv4, 3600, 0},
		{"@", "MX", "mail." + alanAdi, 3600, 10},
		{"mail", "A", ipv4, 3600, 0},
		{"@", "TXT", "v=spf1 mx ip4:" + ipv4 + " ~all", 3600, 0},
		{"ns1", "A", ipv4, 3600, 0},
		{"ns2", "A", ipv4, 3600, 0},
		{"@", "NS", "ns1." + alanAdi, 86400, 0},
		{"@", "NS", "ns2." + alanAdi, 86400, 0},
	}
	added := 0
	for _, t := range tmpl {
		// Aynısı zaten varsa atla
		var n int
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
			domainID, t.Ad, t.Tip, t.Deger).Scan(&n)
		if n > 0 {
			continue
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?,?,?,?,?,?, 1)`,
			domainID, t.Ad, t.Tip, t.Deger, t.TTL, t.Oncelik)
		if err != nil {
			log.Printf("dns seed %s/%s: %v", t.Ad, t.Tip, err)
			continue
		}
		added++
	}
	return added, nil
}

func gecerliTip(t string) bool {
	for _, x := range GecerliTipler {
		if x == t {
			return true
		}
	}
	return false
}
