// Package uc — API anahtarı yönetim uçları (oluştur / listele / döndür / iptal /
// kalıcı sil / ben). middleware → apianahtar bağımlılığı tek yönlü kalsın diye ayrı alt paket.
//
// 🔴 Kapsam: bayi YALNIZ kendi anahtarlarını görür/iptal eder/siler (WHERE
// sahip_id=uid; istemci parametresi yok sayılır; yabancı kimlik 404 — varlık
// sızmaz). Kök kendi anahtarlarını yönetir, bayi anahtarlarını listeler, iptal eder
// ve siler ama ÜRETEMEZ ve DÖNDÜREMEZ (gizli, sahibinden başkasının elinde doğmasın).
//
// Uç anlamları: POST /{id}/iptal = yumuşak iptal (satır + iz kalır); DELETE /{id} =
// kalıcı silme (satır gider, denetim geçmişi kalır). Etkin anahtar DELETE ile
// ancak onay_ad = anahtarın adı verilirse silinir ("iptal et ve sil"); onaysız 409.
//
// 🔴 Tam anahtar YALNIZ oluştur/döndür yanıtında bir kez döner (no-store); hiçbir
// liste yanıtında ozet ya da anahtar alanı yoktur. Denetime yalnız önek yazılır.
package uc

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Handlers: yönetim uçları.
type Handlers struct {
	DB *sql.DB
}

// Sınırlar (S3/S6 önerilen varsayılanlar).
const (
	VarsayilanGun    = 365
	MaksGun          = 730
	VarsayilanHiz    = 120
	MinHiz           = 10
	MaksHiz          = 600
	MaksEtkinAnahtar = 25
	MaksGecisSaat    = 72
)

// Dikişler (testler).
var (
	yenidenDogrula = auth.YenidenDogrula
	simdiFn        = time.Now
	uretFn         = apianahtar.Uret
)

// Gorunum: listede dönen alanlar — ozet ve tam anahtar YOK.
type Gorunum struct {
	ID            int64    `json:"id"`
	Ad            string   `json:"ad"`
	Gosterim      string   `json:"gosterim"`
	Onek          string   `json:"onek"`
	Izinler       []string `json:"izinler"`
	IPler         []string `json:"izinli_ipler"`
	HizDk         int      `json:"hiz_dk"`
	SonKullanma   int64    `json:"son_kullanma_at"`
	Olusturma     int64    `json:"olusturma_at"`
	SonKullanim   int64    `json:"son_kullanim_at"`
	SonKullanimIP string   `json:"son_kullanim_ip"`
	IptalAt       int64    `json:"iptal_at"`
	Durum         string   `json:"durum"`
	IptalNeden    string   `json:"iptal_neden"`
	IptalYonetim  bool     `json:"iptal_yonetim"`
	SahipAskida   bool     `json:"sahip_askida"` // sahibi askıdaki bayi (kayıt değişmez)
	SahipID       int64    `json:"sahip_id"`
	SahipRol      string   `json:"sahip_rol"`
	SahipAd       string   `json:"sahip_ad,omitempty"`
}

func gorunum(k *apianahtar.Kayit, simdi int64, sahipAdGoster bool) Gorunum {
	g := Gorunum{
		ID: k.ID, Ad: k.Ad, Gosterim: apianahtar.Gosterim(k.Onek, k.Son4), Onek: k.Onek,
		Izinler: k.Izinler, IPler: k.IPler, HizDk: k.HizDk, SonKullanma: k.SonKullanma,
		Olusturma: k.Olusturma, SonKullanim: k.SonKullanim, SonKullanimIP: k.SonKullanimIP,
		IptalAt: k.IptalAt, Durum: k.GorunenDurum(simdi), IptalNeden: k.IptalNeden,
		SahipAskida:  k.SahipAskida(),
		IptalYonetim: k.IptalEdenID > 0 && k.IptalEdenID != k.SahipID,
		SahipID:      k.SahipID, SahipRol: k.SahipRol,
	}
	if g.Izinler == nil {
		g.Izinler = []string{}
	}
	if g.IPler == nil {
		g.IPler = []string{}
	}
	if sahipAdGoster {
		g.SahipAd = k.SahipAd
	}
	return g
}

func kimlik(r *http.Request) (*auth.Claims, bool) {
	c := middleware.ClaimsFrom(r)
	if c == nil || (c.Role != apianahtar.RolKok && c.Role != apianahtar.RolBayi) {
		return nil, false
	}
	if c.Role == apianahtar.RolBayi && (c.ResellerID <= 0 || c.ResellerID != c.UserID) {
		return nil, false
	}
	return c, true
}

func kapsamOf(c *auth.Claims) int64 {
	if c.Role == apianahtar.RolBayi {
		return c.UserID
	}
	return 0
}

// Liste: GET /api-anahtarlari[?sahip=bayiler]
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	sahip := c.UserID
	bayiler := false
	// Bayide sahip parametresi YOK SAYILIR (kapsam her zaman kendisi).
	if c.Role == apianahtar.RolKok && r.URL.Query().Get("sahip") == "bayiler" {
		sahip, bayiler = apianahtar.TumBayiler, true
	}
	ks, err := apianahtar.Listele(r.Context(), h.DB, sahip)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarları okunamadı")
		return
	}
	simdi := simdiFn().Unix()
	out := make([]Gorunum, 0, len(ks))
	for _, k := range ks {
		out = append(out, gorunum(k, simdi, bayiler))
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"anahtarlar": out})
}

// Katalog: GET /api-anahtarlari/izin-gruplari — rolüne göre süzülmüş izin grupları
// ve sınırlar (arayüzün tek kaynağı).
func (h *Handlers) Katalog(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	var gruplar []apianahtar.Grup
	for _, g := range apianahtar.Gruplar {
		for _, rol := range g.Roller {
			if rol == c.Role {
				gruplar = append(gruplar, g)
				break
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"gruplar":        gruplar,
		"ip_zorunlu":     c.Role == apianahtar.RolKok,
		"varsayilan_gun": VarsayilanGun, "maks_gun": MaksGun,
		"varsayilan_hiz": VarsayilanHiz, "min_hiz": MinHiz, "maks_hiz": MaksHiz,
		"maks_ip": apianahtar.MaksIP,
		// Authorization: gosp <kullanici_adi>:<anahtar> — anahtar yalnız bu adla geçer.
		// Kökte JWT usr ("root"); bayide users.username (JWT usr ile aynı).
		"kullanici_adi": c.Username,
		"kimlik_sema":   apianahtar.Sema,
	})
}

type olusturIstek struct {
	Ad      string   `json:"ad"`
	Izinler []string `json:"izinler"`
	IPler   []string `json:"izinli_ipler"`
	Gun     int      `json:"son_kullanma_gun"`
	HizDk   int      `json:"hiz_dk"`
	Parola  string   `json:"parola"`
	Kod     string   `json:"kod"`
}

// adDogrula: 1-64 karakter, kontrol karakteri yok.
func adDogrula(ad string) (string, bool) {
	ad = strings.TrimSpace(ad)
	n := len([]rune(ad))
	if n == 0 || n > 64 {
		return "", false
	}
	for _, r := range ad {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return ad, true
}

// Olustur: POST /api-anahtarlari
func (h *Handlers) Olustur(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var req olusturIstek
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	ad, ok := adDogrula(req.Ad)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "anahtar adı 1-64 karakter olmalı")
		return
	}
	izinler, err := apianahtar.IzinleriKanonik(req.Izinler, c.Role)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "izin listesi geçersiz (en az bir izin seçin; rolünüzün seçemeyeceği izin olamaz)")
		return
	}
	ipler, err := apianahtar.IPListesiKanonik(req.IPler, c.Role == apianahtar.RolKok)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, ipHataMetni(err))
		return
	}
	gun := req.Gun
	if gun == 0 {
		gun = VarsayilanGun
	}
	if gun < 1 || gun > MaksGun {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("geçerlilik süresi 1-%d gün olmalı", MaksGun))
		return
	}
	hiz := req.HizDk
	if hiz == 0 {
		hiz = VarsayilanHiz
	}
	if hiz < MinHiz || hiz > MaksHiz {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("hız sınırı %d-%d istek/dk olmalı", MinHiz, MaksHiz))
		return
	}
	if !h.yenidenDogrula(w, r, c, req.Parola, req.Kod) {
		return
	}
	simdi := simdiFn()
	if n, err := h.etkinSayi(r, c.UserID, simdi.Unix()); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarları okunamadı")
		return
	} else if n >= MaksEtkinAnahtar {
		httpx.WriteError(w, http.StatusConflict, fmt.Sprintf("en çok %d etkin API anahtarı olabilir; önce kullanılmayanları iptal edin", MaksEtkinAnahtar))
		return
	}
	k := &apianahtar.Kayit{
		SahipID: c.UserID, SahipRol: c.Role, Ad: ad, Izinler: izinler, IPler: ipler,
		HizDk: hiz, SonKullanma: simdi.Add(time.Duration(gun) * 24 * time.Hour).Unix(),
	}
	tam, err := h.ekle(r, k)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı oluşturulamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.olustur", apianahtar.Gosterim(k.Onek, k.Son4),
		fmt.Sprintf("ad=%s izinler=%s ip=%d gün=%d", ad, strings.Join(izinler, ","), len(ipler), gun), kapsamOf(c), true)
	k.Olusturma = simdi.Unix()
	h.tekKezYanit(w, k, tam, simdi.Unix())
}

// ekle: üretir + saklar; önek çakışmasında (UNIQUE) birkaç kez yeniden dener.
func (h *Handlers) ekle(r *http.Request, k *apianahtar.Kayit) (string, error) {
	var son error
	for deneme := 0; deneme < 3; deneme++ {
		u, err := uretFn()
		if err != nil {
			return "", err
		}
		k.Onek, k.Ozet, k.Son4 = u.Onek, u.Ozet, u.Son4
		id, err := apianahtar.Ekle(r.Context(), h.DB, k)
		if err == nil {
			k.ID = id
			return u.Tam, nil
		}
		son = err
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			break
		}
	}
	return "", son
}

func (h *Handlers) tekKezYanit(w http.ResponseWriter, k *apianahtar.Kayit, tam string, simdi int64) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"anahtar_kaydi": gorunum(k, simdi, false),
		"anahtar":       tam, // YALNIZ BU YANITTA, bir kez
	})
}

func (h *Handlers) etkinSayi(r *http.Request, sahip, simdi int64) (int, error) {
	var n int
	err := h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM api_anahtarlari WHERE sahip_id=?
		AND (iptal_at IS NULL OR iptal_at > FROM_UNIXTIME(?))
		AND (son_kullanma_at IS NULL OR son_kullanma_at > FROM_UNIXTIME(?))`, sahip, simdi, simdi).Scan(&n)
	return n, err
}

func ipHataMetni(err error) string {
	switch {
	case errors.Is(err, apianahtar.ErrIPBos):
		return "kök API anahtarında en az bir izinli IP ya da CIDR zorunludur"
	case errors.Is(err, apianahtar.ErrIPCokGenis):
		return "IP aralığı çok geniş (0.0.0.0/0 kabul edilmez; kökte IPv4 en geniş /8, IPv6 en geniş /16)"
	case errors.Is(err, apianahtar.ErrIPCok):
		return fmt.Sprintf("en çok %d IP/CIDR girilebilir", apianahtar.MaksIP)
	}
	return "geçersiz IP ya da CIDR"
}

// ── Yeniden doğrulama + hata sınırlayıcısı (kullanıcı başına 15 dk'da 5 → 30 dk) ──

type ydKayit struct {
	sayi     int
	ilk      time.Time
	kilitSon time.Time
}

var (
	ydMu     sync.Mutex
	ydKayitl = map[int64]*ydKayit{}
)

func ydKilitli(uid int64, simdi time.Time) time.Duration {
	ydMu.Lock()
	defer ydMu.Unlock()
	if k, ok := ydKayitl[uid]; ok && simdi.Before(k.kilitSon) {
		return k.kilitSon.Sub(simdi)
	}
	return 0
}

func ydHata(uid int64, simdi time.Time) {
	ydMu.Lock()
	defer ydMu.Unlock()
	k, ok := ydKayitl[uid]
	if !ok || simdi.Sub(k.ilk) > 15*time.Minute {
		k = &ydKayit{ilk: simdi}
		ydKayitl[uid] = k
	}
	k.sayi++
	if k.sayi >= 5 {
		k.kilitSon = simdi.Add(30 * time.Minute)
		k.sayi, k.ilk = 0, simdi
	}
}

// yenidenDogrula: parola (+2FA). 🔴 Hatalı parola 403 döner, 401 DEĞİL — ön yüz her
// 401'de oturumu kapatır (K4).
func (h *Handlers) yenidenDogrula(w http.ResponseWriter, r *http.Request, c *auth.Claims, parola, kod string) bool {
	simdi := simdiFn()
	if kalan := ydKilitli(c.UserID, simdi); kalan > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(kalan.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "çok fazla hatalı deneme, daha sonra tekrar deneyin")
		return false
	}
	uid, kul := middleware.Aktor(r)
	switch yenidenDogrula(h.DB, c.UserID, c.Role, parola, kod) {
	case auth.YDTamam:
		return true
	case auth.YDKodGerekli:
		httpx.WriteErrorKod(w, http.StatusForbidden, "iki aşamalı doğrulama kodu gerekli", "iki_fa_gerekli")
	case auth.YDKodHatali:
		ydHata(c.UserID, simdi)
		httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.yeniden_dogrulama", "2fa", "hatalı kod", kapsamOf(c), false)
		httpx.WriteErrorKod(w, http.StatusForbidden, "2FA kodu hatalı veya tekrar kullanıldı", "iki_fa_hatali")
	case auth.YDHata:
		httpx.WriteError(w, http.StatusServiceUnavailable, "kimlik doğrulanamadı, birazdan tekrar deneyin")
	default:
		ydHata(c.UserID, simdi)
		httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.yeniden_dogrulama", "parola", "hatalı parola", kapsamOf(c), false)
		httpx.WriteErrorKod(w, http.StatusForbidden, "parola hatalı", "parola_hatali")
	}
	return false
}

type yenileIstek struct {
	Parola    string `json:"parola"`
	Kod       string `json:"kod"`
	GecisSaat int    `json:"gecis_saat"`
}

func idParam(r *http.Request) int64 {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// Yenile: POST /api-anahtarlari/{id}/yenile — aynı izin/IP/hız ile YENİ gizli;
// eski anahtar gecis_saat (0-72) sonra iptal. Yalnız SAHİBİ döndürebilir.
func (h *Handlers) Yenile(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id := idParam(r)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req yenileIstek
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if req.GecisSaat < 0 || req.GecisSaat > MaksGecisSaat {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("geçiş süresi 0-%d saat olmalı", MaksGecisSaat))
		return
	}
	eski, err := apianahtar.IDGetir(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı okunamadı")
		return
	}
	// 🔴 Sahiplik: yabancı (kök dahil — bayinin anahtarını döndüremez) → 404.
	if eski == nil || id == 0 || eski.SahipID != c.UserID || eski.SahipRol != c.Role {
		httpx.WriteError(w, http.StatusNotFound, "API anahtarı bulunamadı")
		return
	}
	simdi := simdiFn()
	if eski.Durum(simdi.Unix()) != apianahtar.DurumEtkin {
		httpx.WriteError(w, http.StatusConflict, "yalnız etkin bir anahtar döndürülebilir")
		return
	}
	// Kayıttaki izin/IP bugünkü kurallara yeniden uydurulur (kural sıkılaştıysa
	// eski geniş değer taşınmasın).
	izinler, err := apianahtar.IzinleriKanonik(eski.Izinler, c.Role)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "anahtarın izinleri artık geçerli değil; yeni anahtar oluşturun")
		return
	}
	ipler, err := apianahtar.IPListesiKanonik(eski.IPler, c.Role == apianahtar.RolKok)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "anahtarın IP listesi artık geçerli değil; yeni anahtar oluşturun")
		return
	}
	if !h.yenidenDogrula(w, r, c, req.Parola, req.Kod) {
		return
	}
	sure := eski.SonKullanma - eski.Olusturma
	if sure <= 0 || sure > int64(MaksGun)*86400 {
		sure = int64(VarsayilanGun) * 86400
	}
	yeni := &apianahtar.Kayit{
		SahipID: c.UserID, SahipRol: c.Role, Ad: eski.Ad, Izinler: izinler, IPler: ipler,
		HizDk: eski.HizDk, SonKullanma: simdi.Unix() + sure,
	}
	tam, err := h.ekle(r, yeni)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı oluşturulamadı")
		return
	}
	iptalZamani := simdi.Unix() + int64(req.GecisSaat)*3600
	if _, err := apianahtar.Iptal(r.Context(), h.DB, eski.ID, c.UserID, c.UserID, "yenilendi", iptalZamani); err != nil {
		// Yeni anahtar oluştu ama eski iptal edilemedi: yeniyi geri al (iki geçerli
		// anahtar kalmasın), hata dön.
		_, _ = apianahtar.Iptal(r.Context(), h.DB, yeni.ID, c.UserID, c.UserID, "yenileme_hatasi", simdi.Unix())
		httpx.WriteError(w, http.StatusInternalServerError, "eski anahtar iptal edilemedi; işlem geri alındı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.yenile", apianahtar.Gosterim(yeni.Onek, yeni.Son4),
		fmt.Sprintf("eski=%s geçiş=%d saat", apianahtar.Gosterim(eski.Onek, eski.Son4), req.GecisSaat), kapsamOf(c), true)
	yeni.Olusturma = simdi.Unix()
	h.tekKezYanit(w, yeni, tam, simdi.Unix())
}

// Iptal: POST /api-anahtarlari/{id}/iptal — yumuşak iptal (satır ve iz kalır).
// Bayi yalnız kendisininkini (yabancı → 404); kök kendisininkini ve bayi anahtarlarını.
func (h *Handlers) Iptal(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id := idParam(r)
	k, err := apianahtar.IDGetir(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı okunamadı")
		return
	}
	sahipKosul := int64(0)
	if c.Role == apianahtar.RolBayi {
		sahipKosul = c.UserID
	}
	if k == nil || id == 0 || (sahipKosul > 0 && k.SahipID != sahipKosul) {
		httpx.WriteError(w, http.StatusNotFound, "API anahtarı bulunamadı")
		return
	}
	simdi := simdiFn().Unix()
	if k.Durum(simdi) == apianahtar.DurumIptal {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "zaten": true})
		return
	}
	neden := "sahip"
	if k.SahipID != c.UserID {
		neden = "yonetim"
	}
	degisti, err := apianahtar.Iptal(r.Context(), h.DB, id, sahipKosul, c.UserID, neden, simdi)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı iptal edilemedi")
		return
	}
	if !degisti {
		httpx.WriteError(w, http.StatusNotFound, "API anahtarı bulunamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.iptal", apianahtar.Gosterim(k.Onek, k.Son4),
		fmt.Sprintf("ad=%s neden=%s", k.Ad, neden), kapsamOf(c), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type silIstek struct {
	OnayAd string `json:"onay_ad"`
}

// Sil: DELETE /api-anahtarlari/{id} — anahtar satırını KALICI siler; denetim
// geçmişi (audit_log) silinmez. İptal edilmiş / süresi dolmuş anahtar doğrudan
// silinir. Etkin (geçiş süresindeki ve sahibi askıdaki dahil) anahtar yalnız gövdede
// onay_ad = anahtarın adı ile silinir: tek adımda "iptal et ve sil" (satır gider →
// sonraki istek 401). Onaysız 409 anahtar_etkin. Bayi yalnız kendisininkini
// (yabancı → 404); kök kendisininkini ve bayi anahtarlarını.
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	c, ok := kimlik(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id := idParam(r)
	var req silIstek
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
			return
		}
	}
	k, err := apianahtar.IDGetir(r.Context(), h.DB, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı okunamadı")
		return
	}
	sahipKosul := int64(0)
	if c.Role == apianahtar.RolBayi {
		sahipKosul = c.UserID
	}
	if k == nil || id == 0 || (sahipKosul > 0 && k.SahipID != sahipKosul) {
		httpx.WriteError(w, http.StatusNotFound, "API anahtarı bulunamadı")
		return
	}
	simdi := simdiFn().Unix()
	etkin := k.Gecerli(simdi)
	if etkin {
		onay := strings.TrimSpace(req.OnayAd)
		if onay == "" {
			httpx.WriteErrorKod(w, http.StatusConflict, "anahtar hâlâ etkin; önce iptal edin ya da adını yazarak \"iptal et ve sil\" onayı verin", "anahtar_etkin")
			return
		}
		if onay != k.Ad {
			httpx.WriteErrorKod(w, http.StatusBadRequest, "onay için yazılan ad anahtarın adıyla eşleşmiyor", "onay_ad_uyusmuyor")
			return
		}
	}
	durum := k.GorunenDurum(simdi)
	silindi, err := apianahtar.Sil(r.Context(), h.DB, id, sahipKosul, !etkin, simdi)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "API anahtarı silinemedi")
		return
	}
	if !silindi {
		httpx.WriteError(w, http.StatusNotFound, "API anahtarı bulunamadı")
		return
	}
	detay := fmt.Sprintf("ad=%s durum=%s", k.Ad, durum)
	if etkin {
		detay += " (etkinken iptal edilip silindi)"
	}
	if k.SahipID != c.UserID {
		detay += fmt.Sprintf(" sahip=%s", k.SahipAd)
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "api_anahtar.sil", apianahtar.Gosterim(k.Onek, k.Son4), detay, kapsamOf(c), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Ben: GET /api-anahtari/ben — YALNIZ anahtarla; JWT ile 404. Otomasyonun
// "bağlantıyı test et" adımı.
func (h *Handlers) Ben(w http.ResponseWriter, r *http.Request) {
	k := middleware.AnahtarFrom(r)
	if k == nil {
		http.NotFound(w, r)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"anahtar_id": k.ID, "ad": k.Ad, "gosterim": apianahtar.Gosterim(k.Onek, k.Son4),
		"rol": k.Rol, "izinler": k.Izinler, "son_kullanma_at": k.SonKullanma,
	})
}
