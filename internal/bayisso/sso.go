// Package bayisso — otomasyonun (WHMCS bayi ürünü) BAYİYİ bayi paneline tek tıkla
// girdirmesi (cPanel/WHM "create_user_session" karşılığı).
//
// AKIŞ (müşteri SSO'su ile aynı iki adım; bkz. internal/adina/sso.go):
//  1. POST /api/v1/resellers/{id}/sso — YALNIZ kök API anahtarı; rota "sso:yaz" ister,
//     işleyici ayrıca "bayi:yaz" ister (yalnız müşteri girişi için verilmiş "sso" izni
//     bayiye girdirmez). 60 sn geçerli, TEK KULLANIMLIK kod; yanıtta bayi kapısının
//     `/sso#kod=…` adresi. Kod URL PARÇASINDA taşınır: sunucuya, nginx günlüğüne ve
//     Referer'a gitmez.
//  2. Arayüz bayi kapısında POST /api/v1/auth/sso-giris ile kodu olağan bir bayi
//     oturumuna çevirir (ömür, boşta düşme ve oturum düşürme damgası parolalı girişle
//     aynı). Kod üretimiyle değişim arasında askıya alınan/silinen bayi giriş alamaz.
//
// Fark (anahtar kimlik basamaz ilkesi, apianahtar.KimlikUclari): token "sso"
// damgalıdır; API anahtarı yönetimi ve 2FA değişikliği bu oturumda 403
// (middleware.YalnizOturum) — kök anahtarını ele geçiren biri bayiye kalıcı bir anahtar
// ya da kendi 2FA'sını bırakamasın. Parola değişikliği mevcut parolayı istediğinden
// açıktır (değiştiren parolayı bildiğini kanıtlamıştır; yeni token damgasızdır).
//
// 🔴 Kod yanıt gövdesi dışında hiçbir yere yazılmaz: ne log'a ne denetim ayrıntısına.
// Yanıtlar Cache-Control: no-store.
package bayisso

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// KodOmru: giriş kodunun geçerlilik süresi.
const KodOmru = 60 * time.Second

// kodSiniri: bellekte en çok bu kadar bekleyen kod (bellek tüketimi sınırı).
const kodSiniri = 1000

// ssoHizDk: anahtar başına dakikada en çok bu kadar bayi giriş kodu (genel API hız
// sınırına EK).
var ssoHizDk = 30

// gecersizMesaj: kod yok/bitti/tüketildi/bayi silindi — hepsi AYNI yanıt.
const gecersizMesaj = "giriş bağlantısı geçersiz ya da süresi doldu"

type Handlers struct {
	DB          *sql.DB
	Secret      []byte
	LifetimeSec int
	// BayiAdresi: bayi paneli kök adresi ("https://host:port"); çözülemezse ("", false).
	// main() kurar.
	BayiAdresi func(r *http.Request) (adres string, ok bool)
}

type kodKaydi struct {
	bayiID    int64
	anahtarID int64
	anahtarAd string // yalnız denetim ayrıntısı (gizli değil)
	bitis     time.Time
}

var (
	kodMu   sync.Mutex
	kodlar  = map[string]kodKaydi{}
	simdiFn = time.Now
	kapiCoz = kapi.Istek
	kodUret = rastgeleKod

	hizMu     sync.Mutex
	hizKovasi = map[int64][]time.Time{} // anahtar id → son 1 dk içindeki istekler
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

// kodSakla: süresi dolanları temizler, sınırı aşmıyorsa kaydı ekler (yalnız özet tutulur).
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

// hizSinir: anahtar için bir istek hakkı harcar; sınır aşıldıysa beklenecek süre.
func hizSinir(anahtarID int64) (bekle time.Duration, ok bool) {
	hizMu.Lock()
	defer hizMu.Unlock()
	now := simdiFn()
	esik := now.Add(-time.Minute)
	for id, ts := range hizKovasi {
		if len(ts) == 0 || ts[len(ts)-1].Before(esik) {
			delete(hizKovasi, id)
		}
	}
	ts := hizKovasi[anahtarID]
	i := 0
	for i < len(ts) && ts[i].Before(esik) {
		i++
	}
	ts = ts[i:]
	if len(ts) >= ssoHizDk {
		hizKovasi[anahtarID] = ts
		bekle = ts[0].Add(time.Minute).Sub(now)
		if bekle < time.Second {
			bekle = time.Second
		}
		return bekle, false
	}
	hizKovasi[anahtarID] = append(ts, now)
	return 0, true
}

// bayiYazIzni: anahtarda "bayi:yaz" var mı.
func bayiYazIzni(izinler []string) bool {
	for _, iz := range izinler {
		if iz == "bayi:"+apianahtar.Yaz {
			return true
		}
	}
	return false
}

// Uret: POST /resellers/{id}/sso — kök API anahtarıyla bayi paneline giriş kodu.
func (h *Handlers) Uret(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	anahtar := middleware.AnahtarFrom(r)
	c := middleware.ClaimsFrom(r)
	if anahtar == nil || c == nil {
		httpx.WriteError(w, http.StatusForbidden, "bu uç yalnızca API anahtarıyla kullanılabilir")
		return
	}
	if c.Role != "admin" {
		httpx.WriteError(w, http.StatusForbidden, "bayi paneline giriş yalnız kök kullanıcının API anahtarıyla yapılabilir")
		return
	}
	if !bayiYazIzni(anahtar.Izinler) {
		httpx.WriteError(w, http.StatusForbidden, "bayi paneline giriş için anahtarda \"bayi\" (yazma) izni de gerekir")
		return
	}
	if bekle, ok := hizSinir(anahtar.ID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(bekle.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "bayi girişi hız sınırı aşıldı, birazdan tekrar deneyin")
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
	var kul, durum string
	err := sql.ErrNoRows
	if id > 0 {
		err = h.DB.QueryRowContext(r.Context(),
			`SELECT username, status FROM users WHERE id = ? AND role = 'reseller'`, id).Scan(&kul, &durum)
	}
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "bayi bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if durum != "active" {
		httpx.WriteError(w, http.StatusConflict, "bayi hesabı askıda — askıdaki bayiye giriş yapılamaz")
		return
	}
	adres, ok := "", false
	if h.BayiAdresi != nil {
		adres, ok = h.BayiAdresi(r)
	}
	if !ok || adres == "" {
		httpx.WriteError(w, http.StatusServiceUnavailable, "bayi paneli adresi çözülemedi")
		return
	}
	kod, err := kodUret()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	if !kodSakla(kod, kodKaydi{bayiID: id, anahtarID: anahtar.ID, anahtarAd: anahtar.Ad, bitis: simdiFn().Add(KodOmru)}) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "çok fazla bekleyen giriş isteği, birazdan tekrar deneyin")
		return
	}
	uid, akt := middleware.Aktor(r) // "root (API anahtarı ad)"
	// Kapsam = bayi: bayi kendi denetim kaydında kökün kendisi adına giriş kodu aldığını görür.
	httpx.Denetim(h.DB, r, uid, akt, "bayi.sso_kod", kul, "API anahtarıyla bayi paneli girişi", id, true)
	log.Printf("bayisso: bayi=%d anahtar=%d için giriş kodu üretildi", id, anahtar.ID) // kod/URL YAZILMAZ
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"url":           strings.TrimRight(adres, "/") + "/sso#kod=" + url.QueryEscape(kod),
		"gecerlilik_sn": int(KodOmru / time.Second),
	})
}

// Giris: POST /auth/sso-giris — kimliksiz; kodu bayi oturumuna çevirir. Yalnız bayi
// kapısında (ve geçişteki eski kapıda) vardır; başka kapıda var-olmayan rotayla aynı 404.
func (h *Handlers) Giris(w http.ResponseWriter, r *http.Request) {
	istekKapi := kapiCoz(r)
	if !kapi.GirisIzinli(istekKapi, kapi.Bayi) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		Kod string `json:"kod"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Kod = strings.TrimSpace(req.Kod)
	if len(req.Kod) < 32 || len(req.Kod) > 128 {
		httpx.WriteError(w, http.StatusUnauthorized, gecersizMesaj)
		return
	}
	k, ok := kodTuket(req.Kod)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, gecersizMesaj)
		return
	}
	// Kod üretimiyle değişim arasında (≤60 sn) bayi silinmiş ya da askıya alınmış olabilir:
	// durum yeniden okunur (adı da: değiştiyse token güncel adı taşır).
	var kul, adSoyad, durum string
	var damga int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT username, COALESCE(full_name,''), status, COALESCE(token_gecersiz_ts,0)
		   FROM users WHERE id = ? AND role = 'reseller'`, k.bayiID).Scan(&kul, &adSoyad, &durum, &damga)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusUnauthorized, gecersizMesaj)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	detay := "bayi paneline tek tık giriş (API anahtarı: " + k.anahtarAd + ")"
	// Katı giriş (D2 whitelabel): doğrulanmış BAŞKA bir bayinin özel panel adresinde giriş
	// yok (parolalı girişle aynı kural, bkz. auth.resellerLogin).
	if auth.HostKiraciKanca != nil {
		if hrid, ok := auth.HostKiraciKanca(r); ok && hrid != k.bayiID {
			httpx.Denetim(h.DB, r, k.bayiID, kul, "auth.sso.marka_host", kul, detay, k.bayiID, false)
			httpx.WriteError(w, http.StatusUnauthorized, gecersizMesaj)
			return
		}
	}
	if durum != "active" {
		httpx.Denetim(h.DB, r, k.bayiID, kul, "auth.sso", kul, detay, k.bayiID, false)
		httpx.WriteHesapAskida(w)
		return
	}
	tok, err := auth.IssueResellerSSOAt(h.Secret, h.LifetimeSec, k.bayiID, kul, k.bayiID, damga, istekKapi)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token üretilemedi")
		return
	}
	// last_activity_ts DE sıfırlanır: bayat damgayla ilk istek "oturum boşta" 401'i alırdı
	// (bkz. auth.resellerLogin).
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE users SET last_login_at=NOW(), last_login_ip=?, last_activity_ts=UNIX_TIMESTAMP() WHERE id=?`,
		httpx.ClientIP(r), k.bayiID)
	httpx.Denetim(h.DB, r, k.bayiID, kul, "auth.sso", kul, detay, k.bayiID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token": tok,
		"bitis": time.Now().Add(time.Duration(h.LifetimeSec) * time.Second).Unix(),
		"kullanici": map[string]any{
			"id": k.bayiID, "adi": kul, "rol": "reseller", "ad_soyad": adSoyad,
		},
		"sso": true,
	})
}
