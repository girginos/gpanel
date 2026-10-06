// Package adina — bayi/yöneticinin müşteri ADINA panele girmesi (cPanel tarzı tek
// tık giriş, "impersonation").
//
// AKIŞ (iki adım; token hiçbir zaman URL'de taşınmaz):
//  1. POST /domains/{id}/adina-oturum (bayi/yönetici kapısı, SahipYonetim):
//     sahiplik + kök askısı doğrulanır, 60 sn geçerli TEK KULLANIMLIK bir giriş
//     kodu ve müşteri kapısının adresi döner. Kod bellekte yalnız SHA-256 özetiyle
//     tutulur.
//  2. Arayüz müşteri kapısına `/adina#kod=…` ile gider (URL parçası sunucuya,
//     nginx günlüğüne ve Referer'a GİTMEZ) ve POST /musteri/adina-giris ile kodu
//     kısa ömürlü (30 dk, yenilenmez) bir müşteri-adına token'ına çevirir. Token
//     müşteri kapısına bağlıdır (kpi) ve gerçek aktörü taşır (auid/aad/arol/arid).
//
// Sonraki her istekte geçerlilik middleware.adinaOturumGecerli'dedir (aktör hesabı
// + domain sahipliği + kök askısı). Kimlik işlemleri middleware.AdinaYasak ile 403.
package adina

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// KodOmru: giriş kodunun geçerlilik süresi.
const KodOmru = 60 * time.Second

// kodSiniri: bellekte en çok bu kadar bekleyen kod (bellek tüketimi sınırı).
const kodSiniri = 10000

type Handlers struct {
	DB     *sql.DB
	Secret []byte
	// MusteriAdresi: isteğin host'una göre müşteri kapısının kök adresi
	// ("https://host:port"). "" = ayrı müşteri kapısı yok (kapı modu kapalı) →
	// arayüz aynı kökende kalır. main() kurar.
	MusteriAdresi func(r *http.Request) (adres string, gerekli bool)
	// SSOAdresi: API anahtarıyla müşteri girişi (sso.go) için müşteri paneli kök
	// adresi ("https://host:port"); çözülemezse ("", false). main() kurar.
	SSOAdresi func(r *http.Request) (adres string, ok bool)
}

type kodKaydi struct {
	domainID  int64
	alanAdi   string
	ftpID     int64
	kullanici string
	aktorUID  int64
	aktorAd   string
	aktorRol  string
	aktorRID  int64
	bitis     time.Time
	// sso: kod API anahtarıyla (POST /domains/{id}/sso) üretildi; anahtarAd yalnız
	// denetim ayrıntısı içindir (gizli değil).
	sso       bool
	anahtarAd string
}

var (
	kodMu    sync.Mutex
	kodlar   = map[string]kodKaydi{}
	simdiFn  = time.Now
	kapiCoz  = kapi.Istek
	kodUret  = rastgeleKod
	aktorAdi = middleware.Aktor
)

func rastgeleKod() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ozet(kod string) string {
	h := sha256.Sum256([]byte(kod))
	return hex.EncodeToString(h[:])
}

// kodSakla: süresi dolanları temizler, sınırı aşmıyorsa kaydı ekler.
func kodSakla(kod string, k kodKaydi) bool {
	kodMu.Lock()
	defer kodMu.Unlock()
	now := simdiFn()
	for o, v := range kodlar {
		if now.After(v.bitis) {
			delete(kodlar, o)
		}
	}
	if len(kodlar) >= kodSiniri {
		return false
	}
	kodlar[ozet(kod)] = k
	return true
}

// kodTuket: kodu TEK KULLANIMLIK alır (bulunsun bulunmasın siler).
func kodTuket(kod string) (kodKaydi, bool) {
	kodMu.Lock()
	defer kodMu.Unlock()
	o := ozet(kod)
	k, ok := kodlar[o]
	delete(kodlar, o)
	if !ok || simdiFn().After(k.bitis) {
		return kodKaydi{}, false
	}
	return k, true
}

// Baslat: POST /domains/{id}/adina-oturum — bayi/yönetici, kendi kapsamındaki
// domain için tek kullanımlık giriş kodu alır. Rota SahipYonetim ile korunur
// (müşteri token'ı ve başka bayinin domaini 403); kök askısı kuralı burada.
func (h *Handlers) Baslat(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r)
	if c == nil || (c.Role != "admin" && c.Role != "reseller") {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz domain")
		return
	}
	rid := int64(0)
	if c.Role == "reseller" {
		rid = c.ResellerID
	}
	// İkinci katman: rota SahipYonetim'e ek olarak aynı karar burada da (kök askısı dahil).
	if kod, mesaj := middleware.AdinaDomainKarar(r.Context(), c.Role, rid, id); kod != 0 {
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
	adres, gerekli := "", false
	if h.MusteriAdresi != nil {
		adres, gerekli = h.MusteriAdresi(r)
	}
	if gerekli && adres == "" {
		// Kapı modu açık ama müşteri kapısı çözülemedi: aynı kökende değişim
		// müşteri girişine kapalı olduğundan (404) hiç kod üretilmez.
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
		bitis: simdiFn().Add(KodOmru),
	}
	if !kodSakla(kod, k) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "çok fazla bekleyen giriş isteği, birazdan tekrar deneyin")
		return
	}
	uid, kul := aktorAdi(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "musteri.adina_kod", alanAdi, "müşteri paneline giriş kodu üretildi", id, true)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kod":        kod,
		"adres":      adres,
		"alan_adi":   alanAdi,
		"gecerlilik": int(KodOmru / time.Second),
	})
}

// anaFTP: hostingin ana FTP hesabı (adına-oturumun müşteri kimliği).
func (h *Handlers) anaFTP(r *http.Request, id int64) (alanAdi string, ftpID int64, kullanici string, err error) {
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT d.alan_adi, fa.id, fa.username FROM domains d
		   JOIN ftp_accounts fa ON fa.domain_id = d.id
		  WHERE d.id = ? AND fa.tur = 'ana'
		  ORDER BY (fa.username = d.ftp_user) DESC, fa.id
		  LIMIT 1`, id).Scan(&alanAdi, &ftpID, &kullanici)
	return
}

type girisReq struct {
	Kod string `json:"kod"`
}

// Giris: POST /musteri/adina-giris — kimliksiz; giriş kodunu müşteri-adına token'ına
// çevirir. Yalnız müşteri kapısında (ve geçişteki eski kapıda) vardır.
func (h *Handlers) Giris(w http.ResponseWriter, r *http.Request) {
	istekKapi := kapiCoz(r)
	if !kapi.GirisIzinli(istekKapi, kapi.Musteri) {
		http.NotFound(w, r)
		return
	}
	var req girisReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Kod = strings.TrimSpace(req.Kod)
	if len(req.Kod) < 32 || len(req.Kod) > 128 {
		httpx.WriteError(w, http.StatusUnauthorized, "giriş bağlantısı geçersiz ya da süresi doldu")
		return
	}
	k, ok := kodTuket(req.Kod)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "giriş bağlantısı geçersiz ya da süresi doldu")
		return
	}
	mc := auth.MusteriClaims{
		FTPHesapID: k.ftpID,
		DomainID:   k.domainID,
		Kullanici:  k.kullanici,
		AlanAdi:    k.alanAdi,
		Kapi:       istekKapi,
		AktorUID:   k.aktorUID,
		AktorAd:    k.aktorAd,
		AktorRol:   k.aktorRol,
		AktorRID:   k.aktorRID,
		SSO:        k.sso,
	}
	tok, exp, err := auth.GenerateMusteri(h.Secret, mc, auth.AdinaOmurSn)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	// Kod üretimiyle değişim arasında (≤60 sn) aktör askıya alınmış, domain
	// devredilmiş ya da kök askıya almış olabilir: token, her istekte koşan AYNI
	// kontrolden geçmeden verilmez.
	parsed, err := auth.ParseMusteri(h.Secret, tok)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if kod, mesaj := middleware.AdinaOturumGecerli(r.Context(), parsed); kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	kul := middleware.AdinaAktorAdi(k.aktorAd, k.domainID)
	detay := "müşteri adına oturum açıldı"
	if k.sso {
		detay += " (API anahtarı: " + k.anahtarAd + ")"
	}
	httpx.DenetimDomain(h.DB, r, k.aktorUID, kul, "musteri.adina_oturum", k.alanAdi, detay, k.domainID, true)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":     tok,
		"bitis":     exp,
		"domain_id": k.domainID,
		"alan_adi":  k.alanAdi,
		"kullanici": k.kullanici,
		"sso":       k.sso,
		"adina": map[string]any{
			"aktor": k.aktorAd,
			"rol":   k.aktorRol,
		},
	})
}

// Cikis: POST /musteri/adina-cikis — adına-oturumun kapanışını denetim kaydına
// yazar (token kısa ömürlüdür; arayüz kendi kopyasını siler). Olağan müşteri
// oturumunda işlem yapmaz.
func (h *Handlers) Cikis(w http.ResponseWriter, r *http.Request) {
	mc := middleware.MusteriClaimsFrom(r)
	if mc.Adina() {
		uid, kul := aktorAdi(r)
		httpx.DenetimDomain(h.DB, r, uid, kul, "musteri.adina_cikis", mc.AlanAdi, "müşteri adına oturum kapatıldı", mc.DomainID, true)
	} else if mc == nil {
		log.Printf("adina: cikis müşteri olmayan oturumdan çağrıldı")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
