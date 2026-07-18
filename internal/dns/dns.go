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

// gecerliKayitAlanlari: zone satir-enjeksiyonu (newline) + $ yonerge enjeksiyonu engelle
func gecerliKayitAlanlari(ad, deger string) error {
	if strings.ContainsAny(ad, " \t\r\n") {
		return errors.New("kayıt adı boşluk veya satır sonu içeremez")
	}
	if strings.ContainsAny(deger, "\r\n") {
		return errors.New("kayıt değeri satır sonu içeremez")
	}
	if strings.HasPrefix(strings.TrimSpace(ad), "$") || strings.HasPrefix(strings.TrimSpace(deger), "$") {
		return errors.New("kayıt '$' ile başlayamaz")
	}
	return nil
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
	k.Oncelik = oncelikNormalize(k.Tip, k.Oncelik)
	if err := gecerliKayitAlanlari(k.Ad, k.Deger); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
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
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		log.Printf("dns WriteZone domain=%d: %v", id, zerr)
	}
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
	k.Oncelik = oncelikNormalize(k.Tip, k.Oncelik)
	if err := gecerliKayitAlanlari(k.Ad, k.Deger); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE dns_records SET ad=?, tip=?, deger=?, ttl=?, oncelik=?, aktif=?
		 WHERE id=? AND domain_id=?`,
		k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik, ak, rid, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Sahiplik: UPDATE domain_id ile scope'lu; read-back'i de domain_id ile sinirla
	// ki baska domain'in kaydi (0-satir guncellemede) yanitla sizmasin.
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=? AND domain_id=?", rid, id)
	saved, _ := scan(row)
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		log.Printf("dns WriteZone domain=%d: %v", id, zerr)
	}
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
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		log.Printf("dns WriteZone domain=%d: %v", id, zerr)
	}
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
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		log.Printf("dns WriteZone domain=%d: %v", id, zerr)
	}
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
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		log.Printf("dns WriteZone domain=%d: %v", id, zerr)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eklenen": n})
}

// SeedDefaults: domain icin MERKEZI DNS sablonundan kayit uretir (idempotent).
// Sablon Ayarlar'da duzenlenebilir; DMARC/SPF/DKIM dahil placeholder'lar cozulur.
// DKIM icin domain'e ozel anahtar cifti uretilir (varsa yeniden kullanilir).
func SeedDefaults(ctx context.Context, db *sql.DB, domainID int64, alanAdi, ipv4 string) (int, error) {
	if ipv4 == "" {
		ipv4 = "127.0.0.1"
	}
	rows, err := LoadTemplate(ctx, db)
	if err != nil || len(rows) == 0 {
		rows = builtinDefaults() // tablo bos/erisilemezse gomulu varsayilan
	}
	meta := LoadTemplateMeta(ctx, db)
	selector := meta.DKIMSelector

	// DKIM gerekiyorsa anahtari hazirla (bir kez)
	dkimTxt := ""
	if meta.DKIMAktif {
		for _, t := range rows {
			if t.Aktif && strings.Contains(t.Deger, "{DKIM}") {
				dkimTxt, _ = EnsureDKIM(ctx, db, domainID, alanAdi, selector)
				break
			}
		}
	}

	added := 0
	for _, t := range rows {
		if !t.Aktif {
			continue
		}
		// DKIM satiri ama anahtar uretilemedi/kapali → atla
		if strings.Contains(t.Deger, "{DKIM}") && (!meta.DKIMAktif || dkimTxt == "") {
			continue
		}
		ad := subst(t.Ad, alanAdi, ipv4, selector, dkimTxt)
		deger := subst(t.Deger, alanAdi, ipv4, selector, dkimTxt)
		tip := strings.ToUpper(strings.TrimSpace(t.Tip))
		// Ayni ad+tip+deger varsa atla (idempotent)
		var n int
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
			domainID, ad, tip, deger).Scan(&n)
		if n > 0 {
			continue
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?,?,?,?,?,?, 1)`,
			domainID, ad, tip, deger, t.TTL, oncelikNormalize(tip, t.Oncelik)); err != nil {
			log.Printf("dns seed %s/%s: %v", ad, tip, err)
			continue
		}
		added++
	}

	// Per-domain SOA yoksa merkezi sablon SOA parametrelerinden tohumla
	seedSOAFromMeta(ctx, db, domainID, alanAdi, meta)
	return added, nil
}

// seedSOAFromMeta: domain'in dns_soa satiri yoksa merkezi sablon SOA degerleriyle olustur.
func seedSOAFromMeta(ctx context.Context, db *sql.DB, domainID int64, alanAdi string, meta TemplateMeta) {
	var mevcut int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_soa WHERE domain_id=?`, domainID).Scan(&mevcut)
	if mevcut > 0 {
		return
	}
	d := defaultSOA(alanAdi)
	_, _ = db.ExecContext(ctx,
		`INSERT INTO dns_soa(domain_id, primary_ns, hostmaster, refresh, retry, expire, minimum, ttl)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE domain_id=domain_id`,
		domainID, d.PrimaryNS, d.Hostmaster, meta.SOARefresh, meta.SOARetry, meta.SOAExpire, meta.SOAMinimum, meta.SOATTL)
}

func gecerliTip(t string) bool {
	for _, x := range GecerliTipler {
		if x == t {
			return true
		}
	}
	return false
}

// oncelikNormalize: öncelik yalnızca MX ve SRV kayıtlarında anlamlıdır.
// Diğer tiplerde 0'a çekilir; aksi halde zone dosyasında "A 10 1.2.3.4" gibi
// GEÇERSİZ bir satır oluşur ve named-checkzone tüm zone'u reddeder.
func oncelikNormalize(tip string, oncelik int) int {
	if tip == "MX" || tip == "SRV" {
		return oncelik
	}
	return 0
}
