package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// DNSSECDurum: DNSSEC bayrağı + (aktifse) registrar'a girilecek DS ve imzalama durumu.
type DNSSECDurum struct {
	Aktif  bool     `json:"aktif"`
	Imzali bool     `json:"imzali"` // zone DNSKEY yayınlıyor mu (imzalama tamam)
	DS     []string `json:"ds"`     // registrar'a girilecek DS kayıtları (CDS'ten türetilir)
	Durum  string   `json:"durum"`  // `rndc dnssec -status` özeti
}

// GetDNSSEC: GET /domains/{id}/dns/dnssec — bayrak + (aktifse) DS/durum.
func (h *Handlers) GetDNSSEC(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, _, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	var v int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT dnssec_aktif FROM domains WHERE id=?`, id).Scan(&v)
	out := DNSSECDurum{Aktif: v == 1}
	if out.Aktif {
		out.DS = dsForZone(r.Context(), alanAdi)
		out.Imzali = len(digShort(r.Context(), alanAdi, "DNSKEY")) > 0
		out.Durum = rndcDNSSECStatus(r.Context(), alanAdi)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// PostDNSSEC: POST /domains/{id}/dns/dnssec {"aktif":bool} — bayrağı değiştir + zone'u
// yeniden yaz (include DNSSEC policy'yi ekler/çıkarır, named reload/re-sign eder).
func (h *Handlers) PostDNSSEC(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNSSEC'i değiştirilemez")
		return
	}
	var req struct {
		Aktif bool `json:"aktif"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	ak := 0
	if req.Aktif {
		ak = 1
	}
	// DB değişti: zone istemci kopsa da yeniden yazılmalı (kopuk bağlam).
	zctx, ziptal := httpx.KopukBaglam(r, 2*time.Minute)
	defer ziptal()
	// Bayrak + zone TEK işlem: zone yazılamazsa bayrak da eski değerine döner
	// (DNSSEC'in yarım açık/kapalı kalması DS uyuşmazlığıyla SERVFAIL demektir).
	err = h.zoneliIslem(zctx, id, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(zctx, `UPDATE domains SET dnssec_aktif=? WHERE id=?`, ak, id)
		return e
	})
	if err != nil {
		if h.hataYanitla(w, r, id, err, "zone güncellenemedi: ", http.StatusInternalServerError) {
			log.Printf("dns WriteZone(dnssec) domain=%d: %v", id, err)
			h.denetle(r, id, "dns.dnssec.degistir", alanAdi, fmt.Sprintf("aktif=%t — zone yazılamadı, geri alındı", req.Aktif), false)
		}
		return
	}
	h.denetle(r, id, "dns.dnssec.degistir", alanAdi, fmt.Sprintf("aktif=%t", req.Aktif), true)
	out := DNSSECDurum{Aktif: req.Aktif}
	if req.Aktif {
		// İmzalama birkaç saniye sürebilir; DS/DNSKEY hemen görünmeyebilir → frontend "durum"
		// ile poll eder. Yine de anlık bir okuma dönelim.
		out.DS = dsForZone(zctx, alanAdi)
		out.Imzali = len(digShort(zctx, alanAdi, "DNSKEY")) > 0
		out.Durum = rndcDNSSECStatus(zctx, alanAdi)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// dsForZone: registrar'a girilecek DS kayıtlarını döner ("keytag alg digest-tip digest").
// Önce CDS'i dener (BIND'in yayınladığı "güvenli" sinyal). Yeni imzalamada CDS henüz
// yayınlanmamış olabilir (DS state=hidden, DNSKEY propagasyonu bekleniyor) → o durumda
// DNSKEY'den DS türetir ki kullanıcı registrar'a HEMEN girebilsin (aksi halde saatlerce
// boş DS görürdü). İki kaynak da aynı rdata formatına normalize edilir.
func dsForZone(ctx context.Context, zone string) []string {
	if cds := digShort(ctx, zone, "CDS"); len(cds) > 0 {
		return cds
	}
	return dsFromDNSKEY(ctx, zone)
}

// dsFromDNSKEY: canlı DNSKEY'lerden `dnssec-dsfromkey` ile DS türetir (SHA-256).
func dsFromDNSKEY(ctx context.Context, zone string) []string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dk, err := exec.CommandContext(cctx, "dig", "+noall", "+answer", "@127.0.0.1", zone, "DNSKEY").Output()
	if err != nil || len(strings.TrimSpace(string(dk))) == 0 {
		return nil
	}
	f, err := os.CreateTemp("", "gosp-dnskey-*.txt")
	if err != nil {
		return nil
	}
	defer os.Remove(f.Name())
	if _, werr := f.Write(dk); werr != nil {
		_ = f.Close()
		return nil
	}
	_ = f.Close()
	out, err := exec.CommandContext(cctx, "dnssec-dsfromkey", "-f", f.Name(), zone).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// "<name>. IN DS 23936 13 2 <digest>" → "23936 13 2 <digest>" (CDS +short ile aynı format)
		if i := strings.Index(l, " DS "); i >= 0 {
			l = strings.TrimSpace(l[i+4:])
		}
		lines = append(lines, l)
	}
	return lines
}

// digShort: `dig +short @127.0.0.1 <zone> <tip>` çıktısını satır listesine çevirir.
// Zone adı DB'den gelir (domain oluşturmada doğrulanır) ve exec ARGÜMANI olarak geçer
// (shell yok) → komut enjeksiyonu mümkün değil. Bağlam 5 sn ile sınırlıdır (hang önlenir).
func digShort(ctx context.Context, zone, tip string) []string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "dig", "+short", "@127.0.0.1", zone, tip).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// rndcDNSSECStatus: `rndc dnssec -status <zone>` çıktısı (imzalama/rollover durumu).
func rndcDNSSECStatus(ctx context.Context, zone string) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "rndc", "dnssec", "-status", zone).CombinedOutput()
	return strings.TrimSpace(string(out))
}
