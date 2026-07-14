package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

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
		if ClaimsFrom(r) == nil {
			httpx.WriteError(w, http.StatusForbidden, "sadece yöneticiler için")
			return
		}
		next.ServeHTTP(w, r)
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
			if ClaimsFrom(r) != nil {
				next.ServeHTTP(w, r) // admin
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
			next.ServeHTTP(w, r)
		})
	}
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
