// Package domainyasak — phishing korumalı yasaklı domain listesi.
//
// Amaç: bir tenant'ın "sahibinden.com" ya da "login.sahibinden.com" gibi bir
// hostname'i panele eklemesini engellemek. Kullanıcı DNS'in gerçek sahibi
// olmasa bile panelde bir vhost açıp trafiği yeniden yönlendirebilir; bu
// klasik bir phishing tekniğidir.
//
// Alt-domain kapsamı KAYIT BAŞINA seçilebilir. Varsayılan true — phishing
// tam olarak alt-domain'de gizler; "sahibinden.com" yasakken "x.sahibinden.com"
// serbest bırakmak deliği açık tutmaktır. False sadece istisnai durumlar için.
//
// 🔴 provisioner buraya doğrudan bağımlı DEĞİL — callback deseni ile bağlı.
// main.go açılışta `SetYasakliKontrolu(domainyasak.Yasakli)` çağırır.

package domainyasak

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 60 * time.Second

type Kayit struct {
	Domain          string `json:"domain"`
	Description     string `json:"description"`
	MatchSubdomains bool   `json:"match_subdomains"`
	CreatedAt       string `json:"created_at"`
}

type cacheKayit struct {
	matchSubdomains bool
}

var (
	mu         sync.RWMutex
	cache      map[string]cacheKayit
	cacheZaman time.Time
	db         *sql.DB
)

// Init — main.go'da bir kez çağrılır; DB handle'ını saklar ve ilk yüklemeyi
// yapar. DB hatası fail-OPEN: panel açılması yasaklı liste yüzünden bloklanmaz.
func Init(d *sql.DB) error {
	mu.Lock()
	db = d
	mu.Unlock()
	return yenile()
}

// Yasakli — hostname yasak mı? provisioner.ValidateDomain çağırır.
//
// Kural (sırayla):
//  1. Tam eşleşme: hostname listede varsa → yasak
//  2. Alt-domain: hostname'in üst domain'lerinden biri listede VE
//     match_subdomains=true ise → yasak
//
// Örnek liste = { "sahibinden.com" (match_subdomains=true) }:
//   sahibinden.com          → yasak (exact)
//   login.sahibinden.com    → yasak (subdomain match)
//   x.login.sahibinden.com  → yasak (nested subdomain match)
//   sahibindenmadam.com     → SERBEST (substring değil, sadece domain eşleşmesi)
func Yasakli(hostname string) (bool, string) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return false, ""
	}
	if !taze() {
		if err := yenile(); err != nil {
			// DB blip → fail-open. Yasaklı liste bir POLİÇE, güvenlik sınırı
			// değil. Domain açma bu yüzden durmasın; denetim log'una hata düşer.
			return false, ""
		}
	}
	mu.RLock()
	defer mu.RUnlock()

	// 1) Exact
	if _, ok := cache[hostname]; ok {
		return true, hostname
	}
	// 2) Subdomain — sağdan sola üst domain'leri kontrol et
	parcalar := strings.Split(hostname, ".")
	for i := 1; i < len(parcalar); i++ {
		ust := strings.Join(parcalar[i:], ".")
		if k, ok := cache[ust]; ok && k.matchSubdomains {
			return true, ust
		}
	}
	return false, ""
}

func taze() bool {
	mu.RLock()
	defer mu.RUnlock()
	return cache != nil && time.Since(cacheZaman) < cacheTTL
}

func yenile() error {
	mu.RLock()
	d := db
	mu.RUnlock()
	if d == nil {
		return errors.New("domainyasak: Init çağrılmamış")
	}
	rows, err := d.Query(`SELECT domain, match_subdomains FROM cp_banned_domains`)
	if err != nil {
		return err
	}
	defer rows.Close()
	yeni := make(map[string]cacheKayit, 128)
	for rows.Next() {
		var dom string
		var ms int
		if err := rows.Scan(&dom, &ms); err != nil {
			return err
		}
		yeni[strings.ToLower(strings.TrimSpace(dom))] = cacheKayit{matchSubdomains: ms != 0}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	mu.Lock()
	cache = yeni
	cacheZaman = time.Now()
	mu.Unlock()
	return nil
}

func gecersizKil() {
	mu.Lock()
	cacheZaman = time.Time{}
	mu.Unlock()
}

// Refresh — cache'i şimdi (senkron) DB'den yeniden yükle. Test ve elle
// admin CLI'ları için; HTTP CRUD gecersizKil kullanır (lazy).
func Refresh() error { return yenile() }

/* ---------------- HTTP handler'ları (AdminOnly rotalar) ---------------- */

func domainTemizle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "www.")
	// URL yapıştırılırsa yol/parametreyi at
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	// Baş/son nokta
	s = strings.Trim(s, ".")
	return s
}

// domainGecerliMi — RFC 1035: her etiket 1-63 karakter, harf/rakam/tire,
// tire başta/sonda olmaz. Toplam ≤253.
func domainGecerliMi(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	if !strings.Contains(d, ".") {
		return false // "sahibinden" değil, "sahibinden.com" bekleniyor
	}
	for _, etiket := range strings.Split(d, ".") {
		if len(etiket) == 0 || len(etiket) > 63 {
			return false
		}
		if etiket[0] == '-' || etiket[len(etiket)-1] == '-' {
			return false
		}
		for i := 0; i < len(etiket); i++ {
			c := etiket[i]
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

type aktorFn func(r *http.Request) (uid int64, ok bool)

type Handler struct {
	DB    *sql.DB
	Aktor aktorFn
}

func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.DB.Query(`SELECT domain, description, match_subdomains, created_at FROM cp_banned_domains ORDER BY domain ASC`)
	if err != nil {
		hataYaz(w, http.StatusInternalServerError, "liste okunamadı: "+err.Error())
		return
	}
	defer rows.Close()
	out := []Kayit{}
	for rows.Next() {
		var k Kayit
		var ms int
		if err := rows.Scan(&k.Domain, &k.Description, &ms, &k.CreatedAt); err != nil {
			hataYaz(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
			return
		}
		k.MatchSubdomains = ms != 0
		out = append(out, k)
	}
	jsonYaz(w, http.StatusOK, out)
}

type ekleGovde struct {
	Domain          string `json:"domain"`
	Description     string `json:"description"`
	MatchSubdomains *bool  `json:"match_subdomains"` // pointer: gönderilmezse default true
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var g ekleGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	dom := domainTemizle(g.Domain)
	if !domainGecerliMi(dom) {
		hataYaz(w, http.StatusBadRequest, "geçersiz domain biçimi (örnek: sahibinden.com)")
		return
	}
	if len(g.Description) > 255 {
		g.Description = g.Description[:255]
	}
	ms := 1
	if g.MatchSubdomains != nil && !*g.MatchSubdomains {
		ms = 0
	}
	var uid *int64
	if h.Aktor != nil {
		if u, ok := h.Aktor(r); ok {
			uid = &u
		}
	}
	_, err := h.DB.Exec(
		`INSERT INTO cp_banned_domains (domain, description, match_subdomains, created_by) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE description=VALUES(description), match_subdomains=VALUES(match_subdomains)`,
		dom, g.Description, ms, uid,
	)
	if err != nil {
		hataYaz(w, http.StatusInternalServerError, "yazma hatası: "+err.Error())
		return
	}
	gecersizKil()
	jsonYaz(w, http.StatusCreated, map[string]any{"ok": true, "domain": dom})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	dom := domainTemizle(pathSonParca(r.URL.Path))
	if dom == "" {
		hataYaz(w, http.StatusBadRequest, "domain gerekli")
		return
	}
	res, err := h.DB.Exec(`DELETE FROM cp_banned_domains WHERE domain=?`, dom)
	if err != nil {
		hataYaz(w, http.StatusInternalServerError, "silme hatası: "+err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		hataYaz(w, http.StatusNotFound, "domain listede yok")
		return
	}
	gecersizKil()
	jsonYaz(w, http.StatusOK, map[string]any{"ok": true})
}

func pathSonParca(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func jsonYaz(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func hataYaz(w http.ResponseWriter, kod int, mesaj string) {
	jsonYaz(w, kod, map[string]string{"error": mesaj})
}
