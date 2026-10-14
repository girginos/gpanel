// Package dns: per-domain DNS kayit yonetimi (sablon)
// Not: BIND/PowerDNS henuz kurulu degil. Kayitlar DB'de tutulur,
// kullanici kendi DNS saglayicisina kopyalayabilir; ileride zone file/PowerDNS API yazimi eklenebilir.
package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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

// KayitAlanlariGecerli: zone satir-enjeksiyonu (newline) + $ yonerge enjeksiyonu engelle.
// Disa acik: site tasima (internal/tasima) kaynaktan okudugu kayitlari DB'ye
// yazmadan once ayni kuraldan gecirir.
func KayitAlanlariGecerli(ad, deger string) error {
	if strings.ContainsAny(ad, " \t\r\n") {
		return errors.New("kayıt adı boşluk veya satır sonu içeremez")
	}
	if strings.ContainsAny(deger, "\r\n") {
		return errors.New("kayıt değeri satır sonu içeremez")
	}
	// NUL ve diğer kontrol karakterleri zone ayrıştırıcısını şaşırtır. Sekme serbest.
	for _, c := range ad + deger {
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return errors.New("kayıt kontrol karakteri içeremez")
		}
	}
	if strings.HasPrefix(strings.TrimSpace(ad), "$") || strings.HasPrefix(strings.TrimSpace(deger), "$") {
		return errors.New("kayıt '$' ile başlayamaz")
	}
	return nil
}

// SOAAlanlariGecerli: SOA primary_ns ve hostmaster zone şablonuna AYNEN yazılır
// ({{soaHost}} / {{soaMail}}). Satır sonu, '$' yönergesi ($INCLUDE/$GENERATE),
// ';' yorum, '(' ')' ve '"' içeren değer zone'a satır/yönerge enjekte eder
// (örn. hostmaster "a@b\n$INCLUDE /etc/x" → named-checkzone dosyayı okur ve hata
// çıktısı müşteriye döner). Yalnız ad/e-posta karakterlerine izin verilir.
func SOAAlanlariGecerli(primaryNS, hostmaster string) error {
	if strings.Count(hostmaster, "@") > 1 {
		return errors.New("hostmaster tek '@' içeren bir e-posta adresi olmalı")
	}
	for _, f := range []struct{ ad, v string }{{"birincil NS", primaryNS}, {"hostmaster", hostmaster}} {
		if len(f.v) > 255 {
			return errors.New(f.ad + " çok uzun")
		}
		for _, c := range f.v {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
				c == '.' || c == '-' || c == '_' || (c == '@' && f.ad == "hostmaster") || c == '+'
			if !ok {
				return errors.New(f.ad + " geçersiz karakter içeriyor")
			}
		}
	}
	return nil
}

// SOASayilariGecerli — refresh/retry/expire/minimum/ttl üst sınırı (RFC 2181, INT sütunu):
// fazlası DB'de "out of range" ile 500 ve ham MySQL iletisi verirdi.
func SOASayilariGecerli(s SOA) error {
	for _, f := range []struct {
		ad string
		v  int
	}{{"refresh", s.Refresh}, {"retry", s.Retry}, {"expire", s.Expire}, {"minimum", s.Minimum}, {"ttl", s.TTL}} {
		if f.v < 0 || f.v > enBuyukTTL {
			return fmt.Errorf("SOA %s 0 ile %d arasında olmalı", f.ad, enBuyukTTL)
		}
	}
	return nil
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var k Kayit
	// aktif alani govdede YOKSA varsayilan AKTIF (DB default=1 + frontend uyumu).
	// Kayit.Aktif bool oldugundan "gonderilmedi" ile "false" ayirt edilemiyor; ayrica
	// *bool ile yakaliyoruz. Eskiden alan yokken Go sifiri (false) kaydi sessizce pasif
	// yapip WriteZone WHERE aktif=1 ile zone disinda birakiyordu (E2E bulgusu).
	ham, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := json.Unmarshal(ham, &k); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	var girdiAktif struct {
		Aktif *bool `json:"aktif"`
	}
	_ = json.Unmarshal(ham, &girdiAktif)
	k.Tip = strings.ToUpper(strings.TrimSpace(k.Tip))
	if !gecerliTip(k.Tip) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz DNS tipi")
		return
	}
	if k.Ad == "" {
		k.Ad = "@"
	}
	if k.TTL == 0 { // gönderilmedi → varsayılan; negatif KayitGecerli'de 400
		k.TTL = 3600
	}
	ak := 1
	if girdiAktif.Aktif != nil && !*girdiAktif.Aktif {
		ak = 0
	}
	k.Deger = strings.TrimSpace(k.Deger)
	k.Oncelik = oncelikNormalize(k.Tip, k.Oncelik)
	if err := kayitZoneGecerli(k, alanAdi); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	var saved Kayit
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(zctx,
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?,?,?,?,?,?,?)`,
			id, k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik, ak)
		if err != nil {
			return err
		}
		nid, _ := res.LastInsertId()
		saved, err = scan(tx.QueryRowContext(zctx, selectAll+" WHERE id=?", nid))
		return err
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.kayit.ekle", alanAdi, "zone yazılamadı, geri alındı: "+k.Ad+" "+k.Tip, false)
		}
		return
	}
	h.denetle(r, id, "dns.kayit.ekle", alanAdi, kayitOzeti(saved), true)
	httpx.WriteJSON(w, http.StatusCreated, saved)
}

// zoneGeriAlindi — zone yazımı başarısız olan uçların hata iletisi başı.
const zoneGeriAlindi = "zone güncellenemedi, değişiklik geri alındı: "

// guncelleIstek — PUT /domains/{id}/dns/{rid} gövdesi: KISMİ güncelleme. Gövdede
// OLMAYAN (ya da null) alan mevcut değerini korur.
type guncelleIstek struct {
	Ad      *string `json:"ad"`
	Tip     *string `json:"tip"`
	Deger   *string `json:"deger"`
	TTL     *int    `json:"ttl"`
	Oncelik *int    `json:"oncelik"`
	Aktif   *bool   `json:"aktif"`
}

func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid, _ := strconv.ParseInt(chi.URLParam(r, "rid"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	var g guncelleIstek
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if g.Tip != nil {
		// Geçersiz tip, tx içinde KayitGecerli'den (aynı ileti, 400) geçerken reddedilir.
		t := strings.ToUpper(strings.TrimSpace(*g.Tip))
		g.Tip = &t
	}
	if g.TTL != nil && (*g.TTL < 0 || *g.TTL > enBuyukTTL) {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("TTL 0 ile %d arasında olmalı", enBuyukTTL))
		return
	}
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	var eski, saved Kayit
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		// Sahiplik: kayıt BU domainin olmalı. Yabancı/olmayan rid → 404 (eskiden 200 + boş nesne).
		cur, err := scan(tx.QueryRowContext(zctx, selectAll+" WHERE id=? AND domain_id=? FOR UPDATE", rid, id))
		if errors.Is(err, sql.ErrNoRows) {
			return &islemHatasi{Kod: http.StatusNotFound, Ileti: "DNS kaydı bulunamadı"}
		}
		if err != nil {
			return err
		}
		eski = cur
		k := cur
		if g.Ad != nil {
			k.Ad = *g.Ad
			if k.Ad == "" {
				k.Ad = "@" // Create ile aynı varsayılan
			}
		}
		if g.Tip != nil {
			k.Tip = *g.Tip
		}
		if g.Deger != nil {
			k.Deger = strings.TrimSpace(*g.Deger)
		}
		if g.TTL != nil {
			k.TTL = *g.TTL
			if k.TTL == 0 {
				k.TTL = 3600 // Create ile aynı varsayılan
			}
		}
		if g.Oncelik != nil {
			k.Oncelik = *g.Oncelik
		}
		if g.Aktif != nil {
			k.Aktif = *g.Aktif
		}
		k.Oncelik = oncelikNormalize(k.Tip, k.Oncelik)
		// İçerik GERÇEKTEN değiştiyse ya da kayıt (yeniden) zone'a giriyorsa doğrula.
		// Arayüz formu tüm alanları gönderir: aynı değer içerik değişimi sayılmaz, böylece
		// doğrulayıcının BIND'den katı kaldığı eski bir kaydın TTL'si formdan değişebilir.
		// Eskiden kalmış bozuk bir kaydı yalnız pasife çekmek / silmek serbest kalır.
		icerikDegisti := k.Ad != cur.Ad || !strings.EqualFold(k.Tip, cur.Tip) ||
			k.Deger != cur.Deger || k.Oncelik != cur.Oncelik
		if icerikDegisti || (k.Aktif && !cur.Aktif) {
			if err := kayitZoneGecerli(k, alanAdi); err != nil {
				return &islemHatasi{Kod: http.StatusBadRequest, Ileti: err.Error()}
			}
		}
		ak := 0
		if k.Aktif {
			ak = 1
		}
		if _, err := tx.ExecContext(zctx,
			`UPDATE dns_records SET ad=?, tip=?, deger=?, ttl=?, oncelik=?, aktif=?
			 WHERE id=? AND domain_id=?`,
			k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik, ak, rid, id); err != nil {
			return err
		}
		saved, err = scan(tx.QueryRowContext(zctx, selectAll+" WHERE id=? AND domain_id=?", rid, id))
		return err
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.kayit.guncelle", alanAdi, "zone yazılamadı, geri alındı: #"+strconv.FormatInt(rid, 10), false)
		}
		return
	}
	h.denetle(r, id, "dns.kayit.guncelle", alanAdi, "önce: "+kayitOzeti(eski)+" → sonra: "+kayitOzeti(saved), true)
	httpx.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid, _ := strconv.ParseInt(chi.URLParam(r, "rid"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}
	// Silme de zone YENIDEN URETMELI. Create/Update/TopluSil/TopluDurum/ApplyTemplate
	// WriteZone cagiriyor; tek-kayit Delete atlanmisti -> silinen kayit baska bir
	// mutasyon zone uretene dek DNS uzerinde YAYINDA kaliyordu.
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	var silinen Kayit
	var n int64
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		silinen, _ = scan(tx.QueryRowContext(zctx, selectAll+" WHERE id=? AND domain_id=?", rid, id))
		res, err := tx.ExecContext(zctx, `DELETE FROM dns_records WHERE id=? AND domain_id=?`, rid, id)
		if err != nil {
			return err
		}
		if n, _ = res.RowsAffected(); n == 0 {
			// Update ile aynı: olmayan ya da başka domainin kaydı 404 (eskiden 200 ok:true).
			return &islemHatasi{Kod: http.StatusNotFound, Ileti: "DNS kaydı bulunamadı"}
		}
		return nil
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.kayit.sil", alanAdi, "zone yazılamadı, geri alındı: #"+strconv.FormatInt(rid, 10), false)
		}
		return
	}
	if n > 0 {
		h.denetle(r, id, "dns.kayit.sil", alanAdi, kayitOzeti(silinen), true)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// idListesi — "?,?,?" yer tutucuları + bağlanacak değerler (IN listesi için).
func idListesi(ids []int64) (string, []any) {
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, rid := range ids {
		ph[i] = "?"
		args[i] = rid
	}
	return strings.Join(ph, ","), args
}

// TopluSil: birden fazla DNS kaydini tek istekte sil.
// POST /domains/{id}/dns/toplu-sil  {"ids":[1,2,3]}
func (h *Handlers) TopluSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kayıt seçilmedi")
		return
	}
	ph, idArgs := idListesi(req.IDs)
	args := append(idArgs, id)
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	var n int64
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(zctx,
			"DELETE FROM dns_records WHERE id IN ("+ph+") AND domain_id=?", args...) //nolint:gosec // G202: ph = "?,?,..." (yalnız placeholder); req.IDs []int64 ve domain id, args ile ? bağlanır.
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.toplu.sil", alanAdi, "zone yazılamadı, geri alındı: "+strconv.Itoa(len(req.IDs))+" kayıt", false)
		}
		return
	}
	h.denetle(r, id, "dns.toplu.sil", alanAdi, fmt.Sprintf("%d kayıt silindi (istenen %d)", n, len(req.IDs)), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": n})
}

// TopluDurum: secili kayitlari topluca aktif/pasif yap.
// POST /domains/{id}/dns/toplu-durum  {"ids":[1,2],"aktif":true}
// Etkinleştirme: kayıt zone'a gireceği için KayitGecerli'den geçer (bozuk kayıt
// etkinleştirilemez); pasife çekme doğrulamaz (bozuk kaydı kapatmak düzeltme yoludur).
func (h *Handlers) TopluDurum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
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
	ph, idArgs := idListesi(req.IDs)
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	var n int64
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		if req.Aktif {
			rows, err := tx.QueryContext(zctx,
				selectAll+" WHERE id IN ("+ph+") AND domain_id=? ORDER BY id", append(append([]any{}, idArgs...), id)...) //nolint:gosec // G202: ph yalnız placeholder; değerler args ile bağlanır.
			if err != nil {
				return err
			}
			var kotu []string
			for rows.Next() {
				if k, e := scan(rows); e == nil {
					if verr := kayitZoneGecerli(k, alanAdi); verr != nil {
						kotu = append(kotu, fmt.Sprintf("#%d %s %s: %s", k.ID, k.Ad, k.Tip, verr))
					}
				}
			}
			rerr := rows.Err()
			_ = rows.Close()
			if rerr != nil {
				return rerr
			}
			if len(kotu) > 0 {
				return &islemHatasi{Kod: http.StatusBadRequest,
					Ileti: "geçersiz kayıt etkinleştirilemez — önce düzeltin: " + strings.Join(kotu, "; ")}
			}
		}
		res, err := tx.ExecContext(zctx,
			"UPDATE dns_records SET aktif=? WHERE id IN ("+ph+") AND domain_id=?", append(append([]any{ak}, idArgs...), id)...) //nolint:gosec // G202: ph yalnız placeholder; aktif, req.IDs []int64 ve domain id, args ile ? bağlanır.
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.toplu.durum", alanAdi, "zone yazılamadı, geri alındı: "+strconv.Itoa(len(req.IDs))+" kayıt", false)
		}
		return
	}
	durum := "pasif"
	if req.Aktif {
		durum = "aktif"
	}
	h.denetle(r, id, "dns.toplu.durum", alanAdi, fmt.Sprintf("%d kayıt %s yapıldı (istenen %d)", n, durum, len(req.IDs)), true)
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
	// Tohum + zone istemciden KOPUK (yarım tohum/zone kalmaz).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	// DKIM anahtarı (dosya + ayrı bağlantı) işlemden ÖNCE hazırlanır: kalıcı, yeniden
	// kullanılabilir bir yan etkidir ve işlem içinde kilit beklemesine yol açmasın.
	plan, err := seedPlanla(zctx, h.DB, id, alanAdi, ipv4)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var n int
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		var e error
		n, e = plan.uygula(zctx, tx, id, alanAdi)
		return e
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, zoneGeriAlindi, http.StatusUnprocessableEntity) {
			h.denetle(r, id, "dns.sablon.uygula", alanAdi, "zone yazılamadı, geri alındı", false)
		}
		return
	}
	detay := fmt.Sprintf("%d kayıt eklendi", n)
	if len(plan.atlanan) > 0 {
		detay += fmt.Sprintf(", %d geçersiz şablon satırı atlandı", len(plan.atlanan))
	}
	h.denetle(r, id, "dns.sablon.uygula", alanAdi, detay, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eklenen": n, "atlanan_gecersiz": plan.atlanan})
}

// seedSatir — şablondan üretilmiş, doğrulanmış tek kayıt.
type seedSatir struct {
	ad, tip, deger string
	ttl, oncelik   int
}

// seedPlani — SeedDefaults'un iki aşaması: planla (şablon + DKIM + doğrulama; ayrı
// bağlantıda) ve uygula (idempotent INSERT'ler; işlem içinde çalışabilir).
type seedPlani struct {
	satirlar []seedSatir
	meta     TemplateMeta
	atlanan  []string // geçersiz şablon satırları (açıklamalı)
}

// seedPlanla: domain icin MERKEZI DNS sablonundan kayit satirlarini uretir.
// Sablon Ayarlar'da duzenlenebilir; DMARC/SPF/DKIM dahil placeholder'lar cozulur.
// DKIM icin domain'e ozel anahtar cifti uretilir (varsa yeniden kullanilir).
func seedPlanla(ctx context.Context, db *sql.DB, domainID int64, alanAdi, ipv4 string) (*seedPlani, error) {
	if ipv4 == "" {
		ipv4 = "127.0.0.1"
	}
	// Domainin bayisinin (reseller) sablonu varsa ONU kullan; yoksa global.
	var rid int64
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).Scan(&rid)
	rows, err := LoadTemplateFor(ctx, db, rid)
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

	p := &seedPlani{meta: meta}
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
		oncelik := oncelikNormalize(tip, t.Oncelik)
		// Eski/elle yazılmış şablon satırı zone'a enjekte olamaz / zone'u bozamaz:
		// elle kayıtla AYNI doğrulayıcı (PutTemplate de enjeksiyonu doğrular).
		if verr := kayitZoneGecerli(Kayit{Ad: ad, Tip: tip, Deger: deger, TTL: t.TTL, Oncelik: oncelik}, alanAdi); verr != nil {
			log.Printf("dns seed: geçersiz şablon satırı atlandı (domain=%d tip=%s): %v", domainID, tip, verr)
			p.atlanan = append(p.atlanan, fmt.Sprintf("%s %s: %v", ad, tip, verr))
			continue
		}
		p.satirlar = append(p.satirlar, seedSatir{ad: ad, tip: tip, deger: deger, ttl: t.TTL, oncelik: oncelik})
	}
	return p, nil
}

// uygula: planlanan satirlari ekler (idempotent: ayni ad+tip+deger varsa atlanir) ve
// per-domain SOA'yi tohumlar. Eklenen kayit sayisini doner.
func (p *seedPlani) uygula(ctx context.Context, q sorgucu, domainID int64, alanAdi string) (int, error) {
	added := 0
	for _, s := range p.satirlar {
		// Ayni ad+tip+deger varsa atla (idempotent)
		var n int
		_ = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
			domainID, s.ad, s.tip, s.deger).Scan(&n)
		if n > 0 {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?,?,?,?,?,?, 1)`,
			domainID, s.ad, s.tip, s.deger, s.ttl, s.oncelik); err != nil {
			log.Printf("dns seed %s/%s: %v", s.ad, s.tip, err)
			continue
		}
		added++
	}

	// Per-domain SOA yoksa merkezi sablon SOA parametrelerinden tohumla
	seedSOAFromMeta(ctx, q, domainID, alanAdi, p.meta)
	return added, nil
}

// SeedDefaults: domain icin MERKEZI DNS sablonundan kayit uretir (idempotent).
// Sablon Ayarlar'da duzenlenebilir; DMARC/SPF/DKIM dahil placeholder'lar cozulur.
// DKIM icin domain'e ozel anahtar cifti uretilir (varsa yeniden kullanilir).
// Gecersiz sablon satiri (KayitGecerli) atlanir; ApplyTemplate uyarisi atlananlari bildirir.
func SeedDefaults(ctx context.Context, db *sql.DB, domainID int64, alanAdi, ipv4 string) (int, error) {
	p, err := seedPlanla(ctx, db, domainID, alanAdi, ipv4)
	if err != nil {
		return 0, err
	}
	return p.uygula(ctx, db, domainID, alanAdi)
}

// seedSOAFromMeta: domain'in dns_soa satiri yoksa merkezi sablon SOA degerleriyle olustur.
func seedSOAFromMeta(ctx context.Context, db sorgucu, domainID int64, alanAdi string, meta TemplateMeta) {
	var mevcut int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_soa WHERE domain_id=?`, domainID).Scan(&mevcut)
	if mevcut > 0 {
		return
	}
	d := defaultSOA(alanAdi)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO dns_soa(domain_id, primary_ns, hostmaster, refresh, retry, expire, minimum, ttl)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE domain_id=domain_id`,
		domainID, d.PrimaryNS, d.Hostmaster, meta.SOARefresh, meta.SOARetry, meta.SOAExpire, meta.SOAMinimum, meta.SOATTL); err != nil {
		log.Printf("dns.seedSOAFromMeta: SOA seed yazılamadı (domain=%d): %v — zone SOA'sız kalabilir", domainID, err)
	}
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
