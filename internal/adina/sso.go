package adina

// sso.go — WHMCS (ve benzeri otomasyon) için müşteri TEK TIK girişi:
//
//	POST /api/v1/domains/{id}/sso   (API anahtarı, izin grubu "sso", varsayılan kapalı)
//
// Yeni bir oturum mekanizması DEĞİL: aynı tek kullanımlık, 60 sn'lik kod deposu
// (kodSakla/kodTuket) ve aynı tüketici (POST /musteri/adina-giris, yalnız müşteri
// kapısı) kullanılır. Üretilen oturum bir "müşteri adına oturum"dur: müşteri
// parolası/kimlik işlemleri AdinaYasak ile 403; aktör = anahtarın sahibi (denetime
// "sahip (API anahtarı ad)" yazılır). Fark: hesap askıdaysa giriş YOKTUR (409) ve
// açık SSO oturumu da askıda düşer (auth.MusteriClaims.SSO).
//
// 🔴 Kod yanıt gövdesi (URL parçası) dışında hiçbir yere yazılmaz: ne log'a ne
// denetim ayrıntısına. Yanıt Cache-Control: no-store.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// ssoHizDk: anahtar başına dakikada en çok bu kadar SSO kodu (genel API hız
// sınırına EK; müşteri başına giriş sayısı küçüktür).
var ssoHizDk = 30

var (
	ssoMu     sync.Mutex
	ssoKovasi = map[int64][]time.Time{} // anahtar id → son 1 dk içindeki istekler
)

// ssoHizSinir: anahtar için bir istek hakkı harcar; sınır aşıldıysa beklenecek süre.
func ssoHizSinir(anahtarID int64) (bekle time.Duration, ok bool) {
	ssoMu.Lock()
	defer ssoMu.Unlock()
	now := simdiFn()
	esik := now.Add(-time.Minute)
	for id, ts := range ssoKovasi { // süresi dolan anahtar kovalarını temizle
		if len(ts) == 0 || ts[len(ts)-1].Before(esik) {
			delete(ssoKovasi, id)
		}
	}
	ts := ssoKovasi[anahtarID]
	i := 0
	for i < len(ts) && ts[i].Before(esik) {
		i++
	}
	ts = ts[i:]
	if len(ts) >= ssoHizDk {
		ssoKovasi[anahtarID] = ts
		bekle = ts[0].Add(time.Minute).Sub(now)
		if bekle < time.Second {
			bekle = time.Second
		}
		return bekle, false
	}
	ssoKovasi[anahtarID] = append(ts, now)
	return 0, true
}

// SSO: POST /domains/{id}/sso.
func (h *Handlers) SSO(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	anahtar := middleware.AnahtarFrom(r)
	c := middleware.ClaimsFrom(r)
	if anahtar == nil || c == nil || (c.Role != "admin" && c.Role != "reseller") {
		httpx.WriteError(w, http.StatusForbidden, "bu uç yalnızca API anahtarıyla kullanılabilir")
		return
	}
	if bekle, ok := ssoHizSinir(anahtar.ID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(bekle.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "müşteri girişi hız sınırı aşıldı, birazdan tekrar deneyin")
		return
	}
	// Gövde boş ya da {} (başka içerik anlamsız ama zararsız; yalnız boyut sınırlı).
	if r.Body != nil {
		var govde json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&govde); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
			return
		}
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid := int64(0)
	if c.Role == "reseller" {
		rid = c.ResellerID
	}
	// Kapsam: yok ya da bayi kapsamı dışı → AYNI 404 (varlık sızdırma yok).
	var alanAdi string
	var sahip int64
	var askida int
	err := sql.ErrNoRows
	if id > 0 {
		err = h.DB.QueryRowContext(r.Context(),
			`SELECT alan_adi, COALESCE(reseller_id,0), COALESCE(askida,0) FROM domains WHERE id = ?`, id).
			Scan(&alanAdi, &sahip, &askida)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && c.Role == "reseller" && (rid <= 0 || sahip != rid)) {
		httpx.WriteError(w, http.StatusNotFound, "alan adı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if askida == 1 {
		httpx.WriteError(w, http.StatusConflict, "hesap askıda — askıdaki hesaba müşteri girişi yapılamaz")
		return
	}
	// Adına-oturumun ortak kararı (fail-closed DB, kök askısı vb.).
	if kod, mesaj := middleware.AdinaDomainKarar(r.Context(), c.Role, rid, id); kod != 0 {
		if kod == http.StatusForbidden {
			kod, mesaj = http.StatusConflict, "hesap askıda — müşteri girişi yapılamaz"
		}
		httpx.WriteError(w, kod, mesaj)
		return
	}
	alanAdi, ftpID, kullanici, err := h.anaFTP(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusConflict, "bu hostingin panel kullanıcısı yok")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if h.SSOAdresi == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "müşteri paneli adresi çözülemedi")
		return
	}
	adres, ok := h.SSOAdresi(r)
	if !ok || adres == "" {
		httpx.WriteError(w, http.StatusServiceUnavailable, "müşteri paneli adresi çözülemedi")
		return
	}
	kod, err := kodUret()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	k := kodKaydi{
		domainID: id, alanAdi: alanAdi, ftpID: ftpID, kullanici: kullanici,
		aktorUID: c.UserID, aktorAd: c.Username, aktorRol: c.Role, aktorRID: rid,
		bitis: simdiFn().Add(KodOmru), sso: true, anahtarAd: anahtar.Ad,
	}
	if !kodSakla(kod, k) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "çok fazla bekleyen giriş isteği, birazdan tekrar deneyin")
		return
	}
	uid, kul := aktorAdi(r) // "sahip (API anahtarı ad)"
	httpx.DenetimDomain(h.DB, r, uid, kul, "musteri.sso_kod", alanAdi, "API anahtarıyla müşteri girişi", id, true)
	log.Printf("sso: domain=%d anahtar=%d için müşteri giriş kodu üretildi", id, anahtar.ID) // kod/URL YAZILMAZ
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"url":           strings.TrimRight(adres, "/") + "/adina#kod=" + url.QueryEscape(kod),
		"gecerlilik_sn": int(KodOmru / time.Second),
	})
}
