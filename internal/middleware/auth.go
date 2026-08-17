package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/oturumbosta"

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
				// 🔴 Imza gecerli olmasi YETMEZ: askiya alinan / silinen / parolasi
				// degisen hesabin ACIK oturumu da dusmeli. Aksi halde askiya alinan
				// bayi, token omru (8 saat) boyunca hosting acmaya devam ediyordu.
				if kod, mesaj := oturumGecerli(r.Context(), c); kod != 0 {
					httpx.WriteError(w, kod, mesaj)
					return
				}
				// 🔴 Oturum boşta süresi (kayıt-tabanlı). Ayar=0 iken kontrol
				// atlanır (özellik kapalı). Fail-OPEN: DB blip'inde IdleMi
				// false döner, oturum kesintiye uğramaz.
				iat := int64(0)
				if c.IssuedAt != nil {
					iat = c.IssuedAt.Unix()
				}
				if oturumbosta.IdleMi(scopeDB, c.UserID, iat) {
					httpx.WriteError(w, http.StatusUnauthorized, "oturum boşta süresi doldu, tekrar giriş yapın")
					return
				}
				ctx := context.WithValue(r.Context(), claimsKey, c)
				// Async — DB latency'si yanıt süresine binmesin. Throttle
				// oturumbosta içinde (30sn).
				go oturumbosta.SonAktiviteGuncelle(scopeDB, c.UserID)
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

// oturumGecerli: token imzasi disinda HESABIN hala gecerli olup olmadigini
// dogrular. (0,"") = gecerli; aksi halde HTTP kodu + mesaj.
//   - bayi : users satiri var olmali VE status='active' olmali
//   - herkes: token, hesabin token_gecersiz_ts damgasindan SONRA uretilmis olmali
//
// scopeDB nil ise (test/erken acilis) kontrol atlanir — fail-open yalniz DB
// baglanmadan once mumkun, o asamada zaten istek servis edilmiyor.
// oturumGecerli: gövdesi oturum_cache.go'ya taşındı (retry + kısa-ömürlü durum
// önbelleği + fail-CLOSED). Eski davranış geçici DB hatasında isteği sessizce
// GEÇİRİYORDU (fail-open) → kesinti penceresinde askıya alınmış/geçersiz oturum
// kabul edilebiliyordu.
func oturumGecerli(ctx context.Context, c *auth.Claims) (int, string) {
	return oturumGecerliImpl(ctx, c)
}

// OturumlariDusur: hesabin tum ACIK oturumlarini gecersiz kilar (askiya alma,
// parola degisimi, silme). Bundan once uretilmis JWT'ler reddedilir.
func OturumlariDusur(db *sql.DB, userID int64) {
	if db == nil || userID <= 0 {
		return
	}
	// +1 saniye: JWT 'iat' saniye cozunurlugunde. Damga tam SU AN olsaydi, ayni
	// saniye icinde uretilmis bir token (iat == damga) karsilastirmayi geciyordu —
	// E2E'de parola degisiminden hemen once alinan token boyle hayatta kaldi.
	_, _ = db.Exec(`UPDATE users SET token_gecersiz_ts=UNIX_TIMESTAMP()+1 WHERE id=?`, userID)
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
			// 🔴 Askı kontrolü artık oturumGecerli ile AYNI bounded fail-CLOSED
			// desenini kullanır (retry + kısa-ömürlü önbellek). Eski hali geçici
			// DB hatasında fail-OPEN idi → askıdaki müşteri kesinti penceresinde
			// kendi domainine erişebiliyordu.
			if kod, mesaj := domainAskidaKod(r.Context(), mc.DomainID); kod != 0 {
				httpx.WriteError(w, kod, mesaj)
				return
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

// DBSahipligi: {dbid} gibi TUREV kaynak parametrelerinde sahipligi ROTA
// seviyesinde zorlar. Bu uclarda URL'de {id} domain param'i olmadigi icin
// MusteriScope uygulanamiyordu ve koruma tek bir handler-ici if satirina
// baginliydi; ileride o satiri unutan bir handler sessizce cross-tenant IDOR
// acardi. Handler-ici kontrol KALIYOR (iki katman).
//
// Yonetimsel uclar icin: musteri token'i GECMEZ (kendi DB'sini silemez).
func DBSahipligi(param string) func(http.Handler) http.Handler {
	return dbSahipligi(param, false)
}

// DBSahipligiMusteri: okuma/oturum uclari (phpMyAdmin SSO) — musteri de kendi
// domaininin DB'si icin gecebilir.
func DBSahipligiMusteri(param string) func(http.Handler) http.Handler {
	return dbSahipligi(param, true)
}

func dbSahipligi(param string, musteriGecer bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scopeDB == nil {
				next.ServeHTTP(w, r) // acilis penceresi; handler-ici kontrol yine calisir
				return
			}
			dbID, _ := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
			var domainID int64
			if err := scopeDB.QueryRowContext(r.Context(),
				`SELECT domain_id FROM db_accounts WHERE id=?`, dbID).Scan(&domainID); err != nil {
				// Varlik sizdirma: yok da olabilir, baskasinin da olabilir → 404.
				httpx.WriteError(w, http.StatusNotFound, "veritabanı bulunamadı")
				return
			}
			izin := YonetimSahibi(r, domainID)
			if !izin && musteriGecer {
				izin = DomainSahibiMi(r, domainID)
			}
			if !izin {
				httpx.WriteError(w, http.StatusNotFound, "veritabanı bulunamadı")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// YonetimSahibi: YONETIMSEL bir islemi (DB silme, parola sifirlama gibi) bu
// domain uzerinde yapabilir mi? Admin evet; bayi YALNIZ kendi hosting hesabinda;
// MUSTERI ASLA (kendi DB'sini silemez — bu bayinin/yoneticinin isi).
func YonetimSahibi(r *http.Request, domainID int64) bool {
	c := ClaimsFrom(r)
	if c == nil {
		return false
	}
	if c.Role == "admin" {
		return true
	}
	if c.Role == "reseller" {
		return resellerDomainSahibi(r.Context(), domainID, c.ResellerID)
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
