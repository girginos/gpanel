// Package oturumbosta — session idle timeout. Kullanıcı N dakika boyunca
// istek atmazsa, sonraki istekte 401 döner ve oturum kapanır.
//
// 🔴 JWT TTL'den AYRI bir kavram. JWT TTL = mutlak yaşam süresi (12 saat).
// Idle timeout = son aktiviteden bu yana geçen süre (30 dakika). İkisi bir
// arada:
//   - JWT hâlâ geçerli + son aktivite < 30 dk → OK
//   - JWT hâlâ geçerli + son aktivite ≥ 30 dk → 401 (idle)
//   - JWT süresi dolmuş → zaten normal jwt kontrolünde 401
//
// Fail modeli: DB hatasında IdleMi FAIL-OPEN. Idle bir POLİÇE, güvenlik
// sınırı değil — mevcut auth katmanı (token_gecersiz_ts damgası) güvenlik
// sınırını zaten koruyor. DB blip'inde tüm kullanıcıları anında çıkışa itmek
// kötü UX ve fayda sağlamaz. Karşı görüş: fail-closed. Tercih: kullanılabilirlik.

package oturumbosta

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	ayarAnahtar = "session_idle_minutes"

	// Ayar cache TTL. Admin değişikliği en fazla bu kadar geç uygulanır;
	// AyarKaydet ayrıca cache'i zorla temizler.
	ayarCacheTTL = 60 * time.Second

	// Son aktivite güncelleme throttle'ı. Yoğun polling endpoint'inde her
	// isteği DB'ye yazmak wasteful. 30sn eşiği pratik: idle sınırı dakika
	// bazında ölçülüyor, saniye hassasiyeti önemsiz.
	aktiviteThrottle = 30 * time.Second

	// Idle üst sınırı — kullanıcı yanlışlıkla 999999 girdiğinde overflow /
	// tuhaf davranış olmasın. 24 saat mantıklı bir tavan.
	esikMaxDk = 60 * 24
)

var (
	ayarMu      sync.RWMutex
	ayarCache   int
	ayarZaman   time.Time
	ayarBiliyor bool
	sonAktMu    sync.Mutex
	sonAktKayit = map[int64]time.Time{}
)

// Ayar — session_idle_minutes değerini oku, cache'li. Kayıt yoksa 0 (=kapalı).
func Ayar(db *sql.DB) int {
	ayarMu.RLock()
	if ayarBiliyor && time.Since(ayarZaman) < ayarCacheTTL {
		v := ayarCache
		ayarMu.RUnlock()
		return v
	}
	ayarMu.RUnlock()

	var deger string
	err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar=?`, ayarAnahtar).Scan(&deger)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// DB hatası: cache'i güncelleme, en son bilinen değerle devam et.
		// İlk çağrıda cache yoksa 0 (kapalı) — fail-open.
		ayarMu.RLock()
		v := ayarCache
		ayarMu.RUnlock()
		return v
	}
	dk, _ := strconv.Atoi(deger)
	if dk < 0 {
		dk = 0
	}
	if dk > esikMaxDk {
		dk = esikMaxDk
	}
	ayarMu.Lock()
	ayarCache = dk
	ayarZaman = time.Now()
	ayarBiliyor = true
	ayarMu.Unlock()
	return dk
}

// AyarKaydet — yeni değeri yaz, cache'i temizle. Yalnız admin çağırır.
func AyarKaydet(db *sql.DB, dk int) error {
	if dk < 0 {
		dk = 0
	}
	if dk > esikMaxDk {
		return errors.New("üst sınır 1440 dakika (24 saat)")
	}
	_, err := db.Exec(
		`INSERT INTO cp_ayarlar (anahtar, deger) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE deger=VALUES(deger)`,
		ayarAnahtar, strconv.Itoa(dk),
	)
	if err != nil {
		return err
	}
	ayarMu.Lock()
	ayarCache = dk
	ayarZaman = time.Now()
	ayarBiliyor = true
	ayarMu.Unlock()
	return nil
}

// IdleMi — kullanıcının son_aktivite eşiği aştı mı? Middleware'den çağrılır.
// Fail-OPEN: DB hatasında false (idle değil) döner ve mevcut isteği geçirir.
// Ayar=0 ise kontrol tamamen atlanır (özellik kapalı).
// tokenIat: oturumu acan token'in uretim zamani (unix). TAZE bir token
// tanimi geregi TAZE aktivitedir; bu yuzden damga ile token'in daha YENI
// olani esas alinir.
//
// 🔴 Bu parametre olmadan su kilitlenme olusuyordu: IdleMi 401 verip
// donuyor, aktivite damgasini guncelleyen satir cagirann altinda kaldigi
// icin HIC calismiyor, ve Login de damgayi sifirlamiyordu. Damga bir kez
// bayatlayinca kullanici giris yapsa bile her istekte 401 alip login
// ekranina geri atiliyordu — kalici kilitlenme.
func IdleMi(db *sql.DB, uid int64, tokenIat int64) bool {
	esik := Ayar(db)
	if esik <= 0 || uid <= 0 {
		return false
	}
	var son int64
	err := db.QueryRow(`SELECT last_activity_ts FROM users WHERE id=?`, uid).Scan(&son)
	if err != nil {
		return false // fail-open (bkz. yorum)
	}
	// Token damgadan yeniyse onu kullan.
	if tokenIat > son {
		son = tokenIat
	}
	// İlk kez giren kullanıcı: last_activity=0. Bu, henüz aktivite kaydı yok
	// demek — YENI oturumu idle SAYMAYIZ. Bir sonraki istekte guncellenecek.
	if son == 0 {
		return false
	}
	fark := time.Now().Unix() - son
	return fark > int64(esik)*60
}

// SonAktiviteGuncelle — kullanıcının son_aktivite damgasını şimdi olarak
// güncelle. 30sn throttle: bir kullanıcı aynı süre içinde birçok istek atsa
// bile DB'ye tek yazma. Yoğun polling'de (nav-sayaclar vs.) yazma amplification
// önlenmiş olur.
//
// Async çağrılmalı — DB latency'si isteğin yanıt süresine binmesin.
func SonAktiviteGuncelle(db *sql.DB, uid int64) {
	if uid <= 0 {
		return
	}
	now := time.Now()
	sonAktMu.Lock()
	if son, ok := sonAktKayit[uid]; ok && now.Sub(son) < aktiviteThrottle {
		sonAktMu.Unlock()
		return
	}
	sonAktKayit[uid] = now
	sonAktMu.Unlock()

	_, _ = db.Exec(`UPDATE users SET last_activity_ts=? WHERE id=?`, now.Unix(), uid)
}

/* ---------------- HTTP handler'ları ---------------- */

type aktorFn func(r *http.Request) (uid int64, ok bool)

type Handler struct {
	DB    *sql.DB
	Aktor aktorFn
}

type getYanit struct {
	Dakika int `json:"dakika"`
}

// Get — herkes okuyabilir (kendi oturum penceresini bilsin).
func (h *Handler) Get(w http.ResponseWriter, _ *http.Request) {
	jsonYaz(w, http.StatusOK, getYanit{Dakika: Ayar(h.DB)})
}

type putGovde struct {
	Dakika int `json:"dakika"`
}

// Put — yalnız admin. Middleware.AdminOnly ile korunur.
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	var g putGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if err := AyarKaydet(h.DB, g.Dakika); err != nil {
		hataYaz(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonYaz(w, http.StatusOK, getYanit{Dakika: g.Dakika})
}

func jsonYaz(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func hataYaz(w http.ResponseWriter, kod int, mesaj string) {
	jsonYaz(w, kod, map[string]string{"error": mesaj})
}
