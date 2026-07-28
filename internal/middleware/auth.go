package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// scopeDB: MusteriScope'un domain askıya-alma durumunu kontrol edebilmesi için DB
// handle'ı. main() içinde middleware.Init(db) ile set edilir. nil ise askı kontrolü
// atlanır (mc.DomainID eşleşmesi zaten şarttır; askı yalnızca EK bir kısıttır).
var scopeDB *sql.DB

// Init: middleware paketine DB handle'ı verir (müşteri-scope askı kontrolü için).
func Init(db *sql.DB) { scopeDB = db }

type ctxKey int

const (
	claimsKey        ctxKey = 1
	musteriClaimsKey ctxKey = 2
)

// RequireAuth: hem admin (auth.Claims) hem müşteri (auth.MusteriClaims) token'larını kabul eder.
// Müşteri ise context'e MusteriClaims, admin ise Claims yerleştirir.
func RequireAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimSpace(r.Header.Get("Authorization"))
			const p = "Bearer "
			if !strings.HasPrefix(raw, p) {
				httpx.WriteError(w, http.StatusUnauthorized, "yetkilendirme gerekli")
				return
			}
			tokenRaw := raw[len(p):]
			// CVE-2025-30204 savunma: dogrulama ONCESI asiri-uzun token reddedilir (pre-auth DoS yuzeyi kucultulur)
			if len(tokenRaw) > 8192 {
				httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
				return
			}

			// Önce admin claims dene
			if c, err := auth.Parse(secret, tokenRaw); err == nil {
				ctx := context.WithValue(r.Context(), claimsKey, c)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// Sonra müşteri claims dene
			if mc, err := auth.ParseMusteri(secret, tokenRaw); err == nil {
				ctx := context.WithValue(r.Context(), musteriClaimsKey, mc)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
		})
	}
}

// RequireRole: sadece admin rol kontrolü
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := ClaimsFrom(r)
			if c == nil || !allowed[c.Role] {
				httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminOnly: yalnız admin token'ı geçer (müşteri olduğunda 403)
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := ClaimsFrom(r)
		if c == nil || c.Role != "admin" {
			httpx.WriteError(w, http.StatusForbidden, "sadece yöneticiler için")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminVeyaReseller: admin VEYA reseller token'i gecer (musteri token'i gecmez).
// Handler icinde RolFrom/ResellerIDFrom ile kapsam (scope) uygulanmalidir.
func AdminVeyaReseller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := ClaimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "reseller") {
			httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SahipYonetim: YONETIMSEL islemler (askiya alma, plan degistirme, silme gibi).
// Admin her zaman; reseller YALNIZ kendi hosting hesabinda; MUSTERI ASLA
// (musteri kendi hesabini askiya alamaz / planini degistiremez).
func SahipYonetim(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := ClaimsFrom(r)
		if c == nil {
			httpx.WriteError(w, http.StatusForbidden, "bu işlem için yetkiniz yok")
			return
		}
		if c.Role == "admin" {
			next.ServeHTTP(w, r)
			return
		}
		if c.Role == "reseller" {
			id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
			if !resellerDomainSahibi(r.Context(), id, c.ResellerID) {
				httpx.WriteError(w, http.StatusForbidden, "bu hosting hesabına erişiminiz yok")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		httpx.WriteError(w, http.StatusForbidden, "bu işlem için yetkiniz yok")
	})
}

// MusteriScope: müşteri token'ı varsa URL'deki {id} domain ID müşterinin domain'iyle eşleşmeli.
// Admin ise serbest. Param adı varsayılan "id" — farklı param için MusteriScopeParam.
func MusteriScope(next http.Handler) http.Handler {
	return MusteriScopeParam("id")(next)
}

func MusteriScopeParam(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c := ClaimsFrom(r); c != nil {
				if c.Role == "admin" {
					next.ServeHTTP(w, r) // admin: full
					return
				}
				if c.Role == "reseller" {
					urlID, _ := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
					if !resellerDomainSahibi(r.Context(), urlID, c.ResellerID) {
						httpx.WriteError(w, http.StatusForbidden, "bu domaine erisim yok")
						return
					}
					next.ServeHTTP(w, r)
					return
				}
				httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
				return
			}
			mc := MusteriClaimsFrom(r)
			if mc == nil {
				httpx.WriteError(w, http.StatusUnauthorized, "yetkilendirme gerekli")
				return
			}
			urlID, _ := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
			if urlID != mc.DomainID {
				httpx.WriteError(w, http.StatusForbidden, "bu domain'e erişim yok")
				return
			}
			// Askıya-alma zorlaması: askıdaki domain için müşteri token'ı (önceden
			// verilmiş/hâlâ geçerli olsa bile) TÜM işlemlerde 403 alır. Admin bu
			// bloktan önce (ClaimsFrom != nil) zaten geçmiştir; yönetici askıyı kaldırabilir.
			if scopeDB != nil {
				var askida int
				if err := scopeDB.QueryRowContext(r.Context(),
					`SELECT COALESCE(askida,0) FROM domains WHERE id=?`, mc.DomainID).Scan(&askida); err == nil && askida == 1 {
					httpx.WriteError(w, http.StatusForbidden, "hesap askıya alınmış")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DomainSahibiMi: verilen domain ID cagirana ait mi? Merkezi sahiplik denetimi.
//   - Admin token   => her zaman true (tum domainlere erisir).
//   - Musteri token => yalniz kendi DomainID'siyle eslesiyorsa true.
//   - Kimlik yoksa   => false.
//
// MusteriScope middleware'inin handler-ici eslenigi: URL'de {id} domain param'i
// bulunmayan (or. {dbId} gibi turev kaynak) uclarda, kaynagin domain_id'si DB'den
// cozuldukten sonra bu fonksiyonla sahiplik dogrulanir.
func DomainSahibiMi(r *http.Request, domainID int64) bool {
	if c := ClaimsFrom(r); c != nil {
		if c.Role == "admin" {
			return true // admin: tum domainlere erisir
		}
		if c.Role == "reseller" {
			return resellerDomainSahibi(r.Context(), domainID, c.ResellerID)
		}
		return false
	}
	if mc := MusteriClaimsFrom(r); mc != nil {
		return mc.DomainID == domainID
	}
	return false
}

// resellerDomainSahibi: domain bu reseller'a mi ait (domains.reseller_id == resellerID).
func resellerDomainSahibi(ctx context.Context, domainID, resellerID int64) bool {
	if scopeDB == nil || resellerID == 0 {
		return false
	}
	var rid int64
	if err := scopeDB.QueryRowContext(ctx, `SELECT reseller_id FROM domains WHERE id=?`, domainID).Scan(&rid); err != nil {
		return false
	}
	return rid == resellerID
}

// Aktor: denetim kaydi icin cagiran kimligi (uid, kullanici-adi). Musteri token'inda
// uid 0, kullanici "musteri:<domain_id>" doner.
func Aktor(r *http.Request) (int64, string) {
	if c := ClaimsFrom(r); c != nil {
		return c.UserID, c.Username
	}
	if mc := MusteriClaimsFrom(r); mc != nil {
		return 0, "musteri:" + strconv.FormatInt(mc.DomainID, 10)
	}
	return 0, "anonim"
}

// RolFrom: admin/reseller token rol (yoksa "").
func RolFrom(r *http.Request) string {
	if c := ClaimsFrom(r); c != nil {
		return c.Role
	}
	return ""
}

// ResellerIDFrom: reseller token ise sahiplik anahtari, degilse 0.
func ResellerIDFrom(r *http.Request) int64 {
	if c := ClaimsFrom(r); c != nil {
		return c.ResellerID
	}
	return 0
}

func ClaimsFrom(r *http.Request) *auth.Claims {
	v := r.Context().Value(claimsKey)
	if v == nil {
		return nil
	}
	c, _ := v.(*auth.Claims)
	return c
}

func MusteriClaimsFrom(r *http.Request) *auth.MusteriClaims {
	v := r.Context().Value(musteriClaimsKey)
	if v == nil {
		return nil
	}
	c, _ := v.(*auth.MusteriClaims)
	return c
}
