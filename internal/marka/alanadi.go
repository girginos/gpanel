package marka

// alanadi.go — bayinin özel panel alan adı (ör. panel.bayi.com), cp_marka_alanadi (göç 0123).
//
// Kullanıcı kararı (2026-09-29, 5): varsayılan KAPALI; kök bayi başına açar
// (cp_marka.alanadi_izni), bayi başına en çok 1 (UNIQUE reseller_id).
//
// Akış: ekle (dns_bekliyor + TXT jetonu) → dogrula (A kaydı bu sunucuya +
// TXT _gpanel-dogrulama.<ad> = jeton, KAMU çözücüsünden) → ssl_bekliyor →
// provisioner/panelmarka.go döngüsü (acme.sh HTTP-01 + SNI/include + nginx -t +
// TLS ölçümü) → aktif (ya da hata + gerekçe, geri çekilmeyle yeniden dener).
// Host çözümü yalnız 'aktif' satırları ve alanadi_izni=1 bayileri kullanır
// (cozum.go hostRID).
//
// 🔴 TXT ŞART: A kaydı yalnız "bu sunucuya işaret ediyor" der; aynı sunucudaki
// başka bir bayi DNS'i buraya bakan ama sahiplenilmemiş bir adı kapabilirdi.
// 🔴 Hata metinleri GENEL: başka bayinin adı/kimliği/ham DNS çıktısı dönmez.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"girginospanel/internal/dnsutil"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// AdAlaniKanca — hosting ad-alanı denetimi (domains.AdAlaniDenetle; main.go
// bağlar — import döngüsü olmasın). Dönen hata metni GENEL olmalı.
var AdAlaniKanca func(ctx context.Context, ad string, rid int64) error

// AlanDegistiKanca — özel adın sertifika/nginx uzlaştırmasını hemen tetikler
// (provisioner.PanelMarkaDurt; main bağlar). ad: yeniden doğrulanan ad ya da "".
var AlanDegistiKanca func(ad string)

func alanDegisti(ad string) {
	if AlanDegistiKanca != nil {
		AlanDegistiKanca(ad)
	}
}

// DogrulamaOnek — TXT kaydının adı: _gpanel-dogrulama.<alan>
const DogrulamaOnek = "_gpanel-dogrulama."

// txtCoz / aBuSunucuya — testler değiştirir.
var (
	txtCoz      = kamuTXT
	aBuSunucuya = func(ad string, ipler []string) bool { return dnsutil.BuSunucuya(ad, ipler...) }
)

// kamuTXT — TXT kaydını yerel named'den DEĞİL kamu çözücülerinden sorar
// (yerel BIND sunucunun kendi zone'larını yetkili yanıtlar; doğrulama
// sahibin GERÇEK DNS'ine bakmalı).
func kamuTXT(ctx context.Context, ad string) ([]string, error) {
	var sonHata error
	for _, ns := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
		ns := ns
		res := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, network, ns)
		}}
		c, iptal := context.WithTimeout(ctx, 6*time.Second)
		v, err := res.LookupTXT(c, ad)
		iptal()
		if err == nil {
			return v, nil
		}
		sonHata = err
	}
	return nil, sonHata
}

type alanKaydi struct {
	AlanAdi   string `json:"alan_adi"`
	Durum     string `json:"durum"`
	TXTAd     string `json:"txt_ad"`
	TXTDeger  string `json:"txt_deger"`
	SonHata   string `json:"son_hata"`
	Olusturma string `json:"olusturma"`
	SSLBitis  string `json:"ssl_bitis"`
}

func alanOku(ctx context.Context, db *sql.DB, rid int64) (*alanKaydi, error) {
	var k alanKaydi
	err := db.QueryRowContext(ctx, `SELECT alan_adi, durum, dogrulama, son_hata_genel, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i'),
		COALESCE(DATE_FORMAT(ssl_bitis,'%Y-%m-%d'),'')
		FROM cp_marka_alanadi WHERE reseller_id=?`, rid).Scan(&k.AlanAdi, &k.Durum, &k.TXTDeger, &k.SonHata, &k.Olusturma, &k.SSLBitis)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.TXTAd = DogrulamaOnek + k.AlanAdi
	return &k, nil
}

// alanBayi — yalnız bayi; izin şart. (rid, kayıt, ok)
func (h *Handlers) alanBayi(w http.ResponseWriter, r *http.Request) (BayiKayit, bool) {
	c := middleware.ClaimsFrom(r)
	if c == nil || c.Role != "reseller" || c.ResellerID <= 0 {
		yazHata(w, http.StatusForbidden, "Özel panel adresi bayi hesapları içindir; yönetici adresi Panel Hostname sayfasından ayarlanır.")
		return BayiKayit{}, false
	}
	k, ok := h.bayiYetkili(w, r, c.ResellerID)
	if !ok {
		return k, false
	}
	if !k.AlanadiIzni {
		yazHata(w, http.StatusForbidden, "Özel panel adresi bu hesap için açık değil. Hizmet sağlayıcınızla iletişime geçin.")
		return k, false
	}
	return k, true
}

// AlanGetir — GET /marka/alan-adi
func (h *Handlers) AlanGetir(w http.ResponseWriter, r *http.Request) {
	k, ok := h.alanBayi(w, r)
	if !ok {
		return
	}
	a, err := alanOku(r.Context(), h.DB, k.RID)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres okunamadı.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayit": a, "sunucu_ip": h.SunucuIPleri})
}

// AlanEkle — POST /marka/alan-adi {"alan_adi": "..."}
func (h *Handlers) AlanEkle(w http.ResponseWriter, r *http.Request) {
	k, ok := h.alanBayi(w, r)
	if !ok {
		return
	}
	var g struct {
		AlanAdi string `json:"alan_adi"`
	}
	if err := govdeCoz(r, &g); err != nil {
		yazHata(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	ad := HostNormalize(g.AlanAdi)
	if ad == "" || strings.Count(ad, ".") < 1 {
		yazHata(w, http.StatusBadRequest, "Geçerli bir alan adı girin (ör. panel.ornek.com).")
		return
	}
	if mevcut, err := alanOku(r.Context(), h.DB, k.RID); err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres okunamadı.")
		return
	} else if mevcut != nil {
		yazHata(w, http.StatusConflict, "Zaten bir özel adresiniz var; yenisini eklemek için önce onu kaldırın.")
		return
	}
	if AdAlaniKanca != nil {
		if err := AdAlaniKanca(r.Context(), ad, k.RID); err != nil {
			yazHata(w, http.StatusConflict, "Bu adres kullanılamıyor: "+err.Error())
			return
		}
	}
	jb := make([]byte, 16)
	if _, err := rand.Read(jb); err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres eklenemedi.")
		return
	}
	jeton := hex.EncodeToString(jb)
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum)
		VALUES (?, ?, ?, 'dns_bekliyor')`, ad, k.RID, jeton); err != nil {
		// PK (başka bayi aynı adı almış) ya da UNIQUE(reseller_id) çakışması:
		// kimin olduğu SÖYLENMEZ.
		h.denetim(r, "marka.alanadi.ekle", ad, "", k.RID, false)
		yazHata(w, http.StatusConflict, "Bu adres kullanılamıyor.")
		return
	}
	h.denetim(r, "marka.alanadi.ekle", ad, "", k.RID, true)
	a, _ := alanOku(r.Context(), h.DB, k.RID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayit": a, "sunucu_ip": h.SunucuIPleri})
}

// AlanDogrula — POST /marka/alan-adi/dogrula
func (h *Handlers) AlanDogrula(w http.ResponseWriter, r *http.Request) {
	k, ok := h.alanBayi(w, r)
	if !ok {
		return
	}
	a, err := alanOku(r.Context(), h.DB, k.RID)
	if err != nil || a == nil {
		yazHata(w, http.StatusNotFound, "Özel adres bulunamadı.")
		return
	}
	hata := ""
	if !aBuSunucuya(a.AlanAdi, h.SunucuIPleri) {
		hata = "A kaydı bu sunucunun IP adresine işaret etmiyor (DNS yayılması birkaç saat sürebilir)."
	} else if v, err := txtCoz(r.Context(), DogrulamaOnek+a.AlanAdi); err != nil || !txtIcerir(v, a.TXTDeger) {
		hata = "Doğrulama TXT kaydı bulunamadı ya da değeri eşleşmiyor."
	}
	durum := "ssl_bekliyor"
	if hata != "" {
		durum = "dns_bekliyor"
	}
	if a.Durum == "aktif" && hata == "" {
		durum = "aktif"
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE cp_marka_alanadi SET durum=?, son_hata_genel=? WHERE reseller_id=? AND alan_adi=?`,
		durum, hata, k.RID, a.AlanAdi); err != nil {
		log.Printf("marka: alan adı durumu yazılamadı (rid=%d): %v", k.RID, err)
	}
	hostTemizle()
	if durum == "ssl_bekliyor" {
		alanDegisti(a.AlanAdi)
	}
	h.denetim(r, "marka.alanadi.dogrula", a.AlanAdi, durum, k.RID, hata == "")
	a2, _ := alanOku(r.Context(), h.DB, k.RID)
	kod := http.StatusOK
	if hata != "" {
		kod = http.StatusUnprocessableEntity
	}
	httpx.WriteJSON(w, kod, map[string]any{"kayit": a2, "sunucu_ip": h.SunucuIPleri, "hata": hata})
}

func txtIcerir(v []string, beklenen string) bool {
	for _, s := range v {
		if strings.TrimSpace(s) == beklenen {
			return true
		}
	}
	return false
}

// AlanSil — DELETE /marka/alan-adi (yalnız kendi kaydı). İzin kapalı olsa da
// bayi kendi adresini KALDIRABİLİR (ekleyemez).
func (h *Handlers) AlanSil(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r)
	if c == nil || c.Role != "reseller" || c.ResellerID <= 0 {
		yazHata(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	h.alanSilRID(w, r, c.ResellerID, c.ResellerID)
}

// BayiAlanSil — DELETE /marka/bayiler/{id}/alan-adi (AdminOnly).
func (h *Handlers) BayiAlanSil(w http.ResponseWriter, r *http.Request) {
	k, ok := h.hedefBayi(w, r)
	if !ok {
		return
	}
	h.alanSilRID(w, r, k.RID, 0)
}

func (h *Handlers) alanSilRID(w http.ResponseWriter, r *http.Request, rid, kapsam int64) {
	a, err := alanOku(r.Context(), h.DB, rid)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres okunamadı.")
		return
	}
	if a == nil {
		yazHata(w, http.StatusNotFound, "Özel adres bulunamadı.")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM cp_marka_alanadi WHERE reseller_id=?`, rid); err != nil {
		h.denetim(r, "marka.alanadi.sil", a.AlanAdi, "", kapsam, false)
		yazHata(w, http.StatusInternalServerError, "Özel adres kaldırılamadı.")
		return
	}
	hostTemizle()
	alanDegisti("") // nginx + sertifika + acme yenileme kaydı temizliği
	h.denetim(r, "marka.alanadi.sil", a.AlanAdi, "", kapsam, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
