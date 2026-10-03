package middleware

// anahtar.go — API anahtarıyla kimlik doğrulama (D4). RequireAuth, Authorization
// şeması "gosp" ise ("gosp <kullanıcı adı>:<anahtar>") JWT yerine buraya dallanır.
// Anahtar YALNIZ sahibinin kullanıcı adıyla birlikte geçerlidir (sabit zamanlı
// karşılaştırma; eşleşmezse geçersiz anahtarla aynı 401). "Bearer gosp_…" reddedilir.
//
// 🔴 Üretilen kimlik JWT ile BİREBİR aynı auth.Claims'tir (uid, usr, rol, rid, kpi):
// AdminOnly / SahipYonetim / MusteriScope / DBSahipligi / denetim kapsamı hiçbir
// değişiklik olmadan çalışır. Bayi anahtarında ResellerID = sahip ZORUNLU (rid=0
// bayi tüm sunucuyu görürdü — eleştiri #3/K1).
//
// 🔴 Anahtar yolu oturum boşta sayacına DOKUNMAZ (IdleMi/SonAktiviteGuncelle
// çağrılmaz): otomasyonun düzenli yoklaması insanın tarayıcı oturumunu sonsuza dek
// açık tutmasın, anahtar da "boşta" diye düşmesin. token_gecersiz_ts (JWT oturum
// düşürme) anahtara UYGULANMAZ; anahtarın kendi iptal yaşam döngüsü vardır.

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"

	"github.com/go-chi/chi/v5"
)

const anahtarKey ctxKey = 3

// AnahtarBilgi: isteği doğrulayan API anahtarının (gizli OLMAYAN) özeti.
type AnahtarBilgi struct {
	ID          int64
	Ad          string
	Onek        string
	Son4        string
	Rol         string
	SahipID     int64
	Izinler     []string
	SonKullanma int64
}

// AnahtarFrom: istek API anahtarıyla doğrulandıysa anahtar bilgisi, değilse nil.
func AnahtarFrom(r *http.Request) *AnahtarBilgi {
	if r == nil {
		return nil
	}
	v, _ := r.Context().Value(anahtarKey).(*AnahtarBilgi)
	return v
}

// YalnizOturum: yalnız etkileşimli (JWT) oturuma açık uçlar — API anahtarıyla 403.
// Varsayılan-reddet izin tablosuna EK, derinlemesine savunma (anahtar yönetimi:
// bir anahtar yeni anahtar üretemez/döndüremez/iptal edemez).
func YalnizOturum(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AnahtarFrom(r) != nil {
			httpx.WriteError(w, http.StatusForbidden, "bu işlem API anahtarıyla yapılamaz")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AnahtarAktorAdi: audit_log.actor_username (VARCHAR(64)) için
// "<sahip> (API anahtarı <ad>)". Ad gerekirse kısaltılır; sahip ve ek korunur.
func AnahtarAktorAdi(sahip, ad string) string {
	on := []rune(sahip + " (API anahtarı ")
	if len(on) > 48 {
		on = append([]rune(string([]rune(sahip)[:32])), []rune(" (API anahtarı ")...)
	}
	kalan := 64 - len(on) - 1
	a := []rune(ad)
	if len(a) > kalan {
		a = a[:kalan]
	}
	return string(on) + string(a) + ")"
}

// ── Dikişler (testler) ─────────────────────────────────────────────────────
var (
	anahtarGetir = func(ctx context.Context, onek string) (*apianahtar.Kayit, error) {
		if scopeDB == nil {
			return nil, errVTYok
		}
		return apianahtar.OnekleGetir(ctx, scopeDB, onek)
	}
	anahtarIstemciIP   = httpx.ClientIPKesin
	anahtarSimdi       = time.Now
	anahtarSonKullanim = func(id int64, ip string) { apianahtar.SonKullanimYaz(scopeDB, id, ip) }
	anahtarRedDenetim  = func(r *http.Request, k *apianahtar.Kayit, neden string) {
		kapsam := int64(0)
		if k.SahipRol == apianahtar.RolBayi {
			kapsam = k.SahipID
		}
		httpx.Denetim(scopeDB, r, k.SahipID, AnahtarAktorAdi(anahtarSahipAdi(k), k.Ad),
			"api_anahtar.red", apianahtar.Gosterim(k.Onek, k.Son4), neden, kapsam, false)
	}
)

type vtYokHata struct{}

func (vtYokHata) Error() string { return "veritabanı bağlı değil" }

var errVTYok error = vtYokHata{}

// sahteOzet: önek bulunamadığında da özet hesaplanır (zamanlama eşitliği).
var sahteOzet = apianahtar.Ozet("gosp_zamanlama_esitligi_icin_sahte_deger")

// sahteKullanici: önek bulunamadığında da kullanıcı adı karşılaştırılır.
const sahteKullanici = "\x00zamanlama-esitligi"

// anahtarKullaniciUyar: verilen kullanıcı adı anahtarın SAHİBİNİNKİ mi? Bayi:
// users.username. Kök: "root" (JWT usr) ya da users.username. İki karşılaştırma da
// HER ZAMAN yapılır (kısa devre yok).
func anahtarKullaniciUyar(k *apianahtar.Kayit, verilen string) bool {
	beklenen, kok := sahteKullanici, false
	if k != nil {
		beklenen, kok = k.SahipAd, k.SahipRol == apianahtar.RolKok
	}
	ad := apianahtar.KullaniciEsit(beklenen, verilen)
	kokAd := apianahtar.KullaniciEsit(apianahtar.KokKullaniciAdi, verilen)
	return ad || (kok && kokAd)
}

// anahtarEskiBicim: "Bearer gosp_…" — kullanıcı adısız eski biçim. DB'ye gidilmez;
// hatalı deneme sayılır (IP kilidi), yanıt doğru biçimi söyler.
func anahtarEskiBicim(w http.ResponseWriter, r *http.Request) {
	ip := anahtarIstemciIP(r)
	if ip == "" {
		ip = "kanitsiz"
	}
	simdi := anahtarSimdi()
	if kalan := anahtarHata.kilitli(ip, simdi); kalan > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(kalan.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "çok fazla hatalı API anahtarı denemesi, daha sonra tekrar deneyin")
		return
	}
	anahtarHata.say(ip, simdi)
	httpx.WriteError(w, http.StatusUnauthorized, apianahtar.EskiBicimMesaji)
}

func anahtarSahipAdi(k *apianahtar.Kayit) string {
	if k.SahipRol == apianahtar.RolKok {
		return "root" // JWT'deki usr ile aynı (users.username "admin" olabilir)
	}
	return k.SahipAd
}

// anahtarIstek: API anahtarını doğrular; geçerse kimliği bağlayıp next'i çağırır,
// geçmezse yanıtı yazar.
//
// kimlikDeger: şemasız başlık değeri "<kullanıcı adı>:<anahtar>".
func anahtarIstek(w http.ResponseWriter, r *http.Request, next http.Handler, kimlikDeger, istekKapi string) {
	ip := anahtarIstemciIP(r)
	kilitAnahtari := ip
	if kilitAnahtari == "" {
		kilitAnahtari = "kanitsiz"
	}
	simdi := anahtarSimdi()
	if kalan := anahtarHata.kilitli(kilitAnahtari, simdi); kalan > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(kalan.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "çok fazla hatalı API anahtarı denemesi, daha sonra tekrar deneyin")
		return
	}
	gecersiz := func() {
		anahtarHata.say(kilitAnahtari, simdi)
		httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
	}
	kullanici, tokenRaw, ok := apianahtar.KimlikAyristir(kimlikDeger)
	if !ok {
		gecersiz()
		return
	}
	onek, ok := apianahtar.Ayristir(tokenRaw)
	if !ok {
		gecersiz()
		return
	}
	k, err := anahtarGetir(r.Context(), onek)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "API anahtarı şu anda doğrulanamıyor, birazdan tekrar deneyin")
		return
	}
	beklenen := sahteOzet
	if k != nil {
		beklenen = k.Ozet
	}
	// Özet ve kullanıcı adı İKİSİ DE her durumda hesaplanır; sonuç birleşik.
	ozetTutar := apianahtar.OzetEsit(beklenen, apianahtar.Ozet(tokenRaw))
	kullaniciTutar := anahtarKullaniciUyar(k, kullanici)
	if !ozetTutar || k == nil {
		gecersiz()
		return
	}
	if !kullaniciTutar {
		// Geçerli anahtar + yanlış kullanıcı adı: istemciye geçersiz anahtarla AYNI
		// yanıt (hangisinin yanlış olduğu sızmaz); denetime seyrek ret kaydı.
		if anahtarRedSeyrek(k.ID, simdi) {
			anahtarRedDenetim(r, k, "kullanıcı adı uyuşmuyor")
		}
		gecersiz()
		return
	}
	red := func(kod int, mesaj, neden string) {
		if anahtarRedSeyrek(k.ID, simdi) {
			anahtarRedDenetim(r, k, neden)
		}
		redYaz(w, kod, mesaj)
	}
	if !k.Gecerli(simdi.Unix()) {
		anahtarHata.say(kilitAnahtari, simdi)
		red(http.StatusUnauthorized, "API anahtarı iptal edilmiş ya da süresi dolmuş", "iptal/süre")
		return
	}
	// Rol tutarlılığı: satırdaki sahip_rol, users.role ile eşleşmeli; kök = uid 1.
	switch {
	case k.SahipRol == apianahtar.RolKok && k.SahipUyeRol == "admin" && k.SahipID == 1:
	case k.SahipRol == apianahtar.RolBayi && k.SahipUyeRol == "reseller" && k.SahipID > 0:
	default:
		red(http.StatusUnauthorized, "geçersiz oturum", "rol tutarsız")
		return
	}
	// Bayi askısı: anahtar İPTAL EDİLMEZ, askıda kalır (kullanıcı kararı 2026-10-03).
	// Sahibin durumu her istekte users.status'tan okunur (OnekleGetir JOIN; önbellek
	// yok) → askıdan alınınca anahtar kendiliğinden yeniden çalışır. "active" dışı her
	// durum reddedilir (fail-closed); kod hesap_askida DEĞİL bayi_askida — ön yüzün
	// oturum askı ekranıyla karışmasın, istemci geçici olduğunu bilsin.
	if k.SahipRol == apianahtar.RolBayi && k.SahipDurum != "active" {
		if anahtarRedSeyrek(k.ID, simdi) {
			anahtarRedDenetim(r, k, "bayi askıda")
		}
		httpx.WriteErrorKod(w, http.StatusForbidden, apianahtar.BayiAskidaMesaj, apianahtar.KodBayiAskida)
		return
	}
	// Kapı: kök anahtarı yalnız yönetici kapısında, bayi anahtarı yalnız bayi
	// kapısında (zorlama kapalı/eski kapı: her ikisi). Hangi kapının geçerli olduğu
	// sızmasın diye genel 401.
	if !kapi.AnahtarUyumlu(istekKapi, k.SahipRol) {
		red(http.StatusUnauthorized, "geçersiz oturum", "kapı uyumsuz")
		return
	}
	// Tarayıcı kökeni: anahtar tarayıcı JS'ine gömülmesin (gürültülü bozulma).
	if r.Header.Get("Origin") != "" {
		red(http.StatusForbidden, "API anahtarı tarayıcıdan kullanılamaz", "origin")
		return
	}
	// IP izin listesi — kökte zorunlu (boş liste = bozuk satır → reddet).
	if k.SahipRol == apianahtar.RolKok && len(k.IPler) == 0 {
		red(http.StatusForbidden, "bu IP'den erişim izni yok", "kök IP listesi boş")
		return
	}
	if !apianahtar.IPIzinli(k.IPler, ip) {
		red(http.StatusForbidden, "bu IP'den erişim izni yok", "ip "+ip)
		return
	}
	// İzin: varsayılan reddet (rota tablosu, tam desen).
	desen := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		desen = r.Method + " " + rc.RoutePattern()
	}
	switch apianahtar.Karar(desen, k.SahipRol, k.Izinler) {
	case apianahtar.KararIzinli:
	case apianahtar.KararIzinYetmez:
		red(http.StatusForbidden, "anahtarın bu işlem için izni yok", "izin "+desen)
		return
	default:
		red(http.StatusForbidden, "bu uç API anahtarıyla kullanılamaz", "uç kapalı "+desen)
		return
	}
	// Anahtar başına hız sınırı.
	limit, kalan, bekle := anahtarKova.al(k.ID, k.HizDk, simdi)
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(kalan))
	if bekle > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(bekle.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "API anahtarı hız sınırı aşıldı")
		return
	}
	c := &auth.Claims{
		UserID:   k.SahipID,
		Username: anahtarSahipAdi(k),
		Role:     k.SahipRol,
		Kapi:     istekKapi,
	}
	if k.SahipRol == apianahtar.RolBayi {
		c.ResellerID = k.SahipID
	}
	// 🔴 Değişmez: bayi kimliği rid'siz OLUŞAMAZ (kapsam ResellerID>0'a bağlı).
	if c.Role == "reseller" && c.ResellerID <= 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
		return
	}
	if anahtarSonKullanimSeyrek(k.ID, simdi) {
		go anahtarSonKullanim(k.ID, ip)
	}
	bilgi := &AnahtarBilgi{ID: k.ID, Ad: k.Ad, Onek: k.Onek, Son4: k.Son4, Rol: k.SahipRol,
		SahipID: k.SahipID, Izinler: k.Izinler, SonKullanma: k.SonKullanma}
	ctx := context.WithValue(r.Context(), claimsKey, c)
	ctx = context.WithValue(ctx, anahtarKey, bilgi)
	w.Header().Set("Cache-Control", "no-store")
	next.ServeHTTP(w, r.WithContext(ctx))
}

// ── Seyreltme: son kullanım yazımı (60 sn) ve ret denetimi (5 dk) anahtar başına ──
type seyrek struct {
	mu  sync.Mutex
	son map[int64]time.Time
	ara time.Duration
}

func (s *seyrek) izin(id int64, simdi time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.son[id]; ok && simdi.Sub(t) < s.ara {
		return false
	}
	if len(s.son) > 50000 {
		s.son = map[int64]time.Time{}
	}
	s.son[id] = simdi
	return true
}

var (
	sonKullanimSeyrek = &seyrek{son: map[int64]time.Time{}, ara: 60 * time.Second}
	redSeyrek         = &seyrek{son: map[int64]time.Time{}, ara: 5 * time.Minute}
)

func anahtarSonKullanimSeyrek(id int64, t time.Time) bool { return sonKullanimSeyrek.izin(id, t) }
func anahtarRedSeyrek(id int64, t time.Time) bool         { return redSeyrek.izin(id, t) }

// ── IP başına hatalı anahtar sınırlayıcısı: 10 dk'da 20 hata → 30 dk kilit ──
type hataSinir struct {
	mu      sync.Mutex
	kayit   map[string]*hataKayit
	esik    int
	pencere time.Duration
	kilit   time.Duration
}

type hataKayit struct {
	sayi     int
	ilk      time.Time
	kilitSon time.Time
}

var anahtarHata = &hataSinir{kayit: map[string]*hataKayit{}, esik: 20, pencere: 10 * time.Minute, kilit: 30 * time.Minute}

func (h *hataSinir) kilitli(anahtar string, simdi time.Time) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if k, ok := h.kayit[anahtar]; ok && simdi.Before(k.kilitSon) {
		return k.kilitSon.Sub(simdi)
	}
	return 0
}

func (h *hataSinir) say(anahtar string, simdi time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.kayit) > 100000 {
		for a, k := range h.kayit {
			if simdi.Sub(k.ilk) > h.pencere && simdi.After(k.kilitSon) {
				delete(h.kayit, a)
			}
		}
	}
	k, ok := h.kayit[anahtar]
	if !ok || simdi.Sub(k.ilk) > h.pencere {
		k = &hataKayit{ilk: simdi}
		h.kayit[anahtar] = k
	}
	k.sayi++
	if k.sayi >= h.esik {
		k.kilitSon = simdi.Add(h.kilit)
		k.sayi = 0
		k.ilk = simdi
	}
}

// ── Anahtar başına jeton kovası: kapasite = hiz_dk, dakikada hiz_dk dolum ──
type kovaSinir struct {
	mu   sync.Mutex
	kova map[int64]*kova
}

type kova struct {
	jeton float64
	son   time.Time
}

var anahtarKova = &kovaSinir{kova: map[int64]*kova{}}

// al: bir jeton harcar. (limit, kalan, bekle>0 ise reddedildi).
func (s *kovaSinir) al(id int64, dk int, simdi time.Time) (int, int, time.Duration) {
	if dk <= 0 {
		dk = 120
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.kova[id]
	if !ok {
		k = &kova{jeton: float64(dk), son: simdi}
		s.kova[id] = k
	}
	hiz := float64(dk) / 60.0 // saniyede
	k.jeton += simdi.Sub(k.son).Seconds() * hiz
	if k.jeton > float64(dk) {
		k.jeton = float64(dk)
	}
	k.son = simdi
	if k.jeton < 1 {
		bekle := time.Duration((1 - k.jeton) / hiz * float64(time.Second))
		return dk, 0, bekle
	}
	k.jeton--
	return dk, int(k.jeton), 0
}
