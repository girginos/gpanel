package middleware

// destek.go — Telegram destek botunun (internal/destek/arac) SÜREÇ İÇİ kimliği.
//
// 🔴 Kimlik HTTP başlığından ya da jetondan DEĞİL, yalnız context işaretinden
// gelir: işaret anahtarı bu paketin dışa kapalı bir türüdür ve yalnız
// DestekBagla ile (aynı süreçteki Go kodu) konur. Dışarıdan gelen hiçbir HTTP
// isteği context değeri taşıyamaz → başlık taklidiyle destek kimliği KURULAMAZ.
//
// 🔴 VARSAYILAN REDDET: destek kimliği yalnız DestekRotalari'ndaki GET uçlarına
// geçer (apianahtar/izin.go deseni; RoutePattern tam eşleşme). Tablo dışı her uç
// — yazma uçları, kimlik uçları, yarın eklenecek uçlar — 403. Etkili yetki =
// rol ara yazılımı (AdminOnly / MusteriScope / …) VE bu tablo; tablo yalnız daraltır.
//
// Kurulan kimlik panelin olağan kimliğidir (rol ara yazılımları değişmeden çalışır):
//   - müşteri → auth.MusteriClaims{DomainID} (Adina DEĞİL: AktorUID boş — adına
//     oturumun askı/sahiplik istisnaları destek botuna geçmez)
//   - bayi    → auth.Claims{Role: reseller, ResellerID: kendi users.id}
//   - kök     → auth.Claims{Role: admin}
//
// Denetim aktörü (Aktor): "destek-bot (tg:<kısa>)".

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// Destek kimliği rolleri (protokol.Rol ile aynı dizgeler; middleware protokolü içe aktarmaz).
const (
	DestekMusteri = "musteri"
	DestekBayi    = "bayi"
	DestekKok     = "kok"
)

// DestekKimlik — destek aracının süreç içi isteğinin kimliği.
type DestekKimlik struct {
	Rol         string // DestekMusteri | DestekBayi | DestekKok
	KullaniciID int64  // bayi/kök: users.id (müşteride 0)
	Kullanici   string // bayi/kök: Claims.Username (kökte "root")
	DomainID    int64  // müşteri: isteğin ait olduğu hosting hesabı
	AlanAdi     string // müşteri: MusteriClaims.AlanAdi
	TgKisa      string // denetim aktörü kısa kimliği (^[A-Za-z0-9_-]{1,24}$)
}

type destekKey struct{}

// DestekBagla — ctx'e destek kimliği işaretini koyar. YALNIZ süreç içi çağrı
// (internal/destek/arac) kullanır.
func DestekBagla(ctx context.Context, k DestekKimlik) context.Context {
	kk := k
	return context.WithValue(ctx, destekKey{}, &kk)
}

func destekCtx(ctx context.Context) *DestekKimlik {
	k, _ := ctx.Value(destekKey{}).(*DestekKimlik)
	return k
}

// DestekFrom — istek destek botunun süreç içi isteğiyse kimliği, değilse nil.
func DestekFrom(r *http.Request) *DestekKimlik {
	if r == nil {
		return nil
	}
	return destekCtx(r.Context())
}

var tgKisaRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,24}$`)

// DestekAktorAdi — denetim aktörü: "destek-bot (tg:<kısa>)". Biçim dışı kısa
// kimlik "?" olur (aktör kolonuna serbest metin girmez).
func DestekAktorAdi(tgKisa string) string {
	if !tgKisaRe.MatchString(tgKisa) {
		tgKisa = "?"
	}
	return "destek-bot (tg:" + tgKisa + ")"
}

const dp = "/api/v1"

// DestekRotalari — destek kimliğine AÇIK uçların TEK kaynağı (desen → kullanan
// araç). Yalnız GET/okuma. Değişiklik testdata/destek_rotalari.golden'da diff
// olarak görünür (TestDestekRotalariAltin); her girdi rota envanterinde olmalı
// (cmd/server TestDestekRotalariEnvanterde).
var DestekRotalari = map[string]string{
	"GET " + dp + "/domains/{id}":                      "hesap_ozet",
	"GET " + dp + "/domains/{id}/kaynak":               "hesap_ozet, disk_kota",
	"GET " + dp + "/domains/{id}/ssl":                  "ssl_durum",
	"GET " + dp + "/domains/{id}/cron":                 "cron_liste",
	"GET " + dp + "/domains/{id}/php-settings":         "php_ayar_oku",
	"GET " + dp + "/domains/{id}/php/debug-log":        "log_oku(php_debug)",
	"GET " + dp + "/domains/{id}/logs/oku":             "log_oku(nginx_access, nginx_error)",
	"GET " + dp + "/domains/{id}/backups":              "yedek_liste",
	"GET " + dp + "/domains/{id}/backup-schedule":      "yedek_liste",
	"GET " + dp + "/domains/{id}/wordpress":            "wp_durum",
	"GET " + dp + "/domains/{id}/wordpress/durum":      "wp_durum",
	"GET " + dp + "/domains/{id}/wordpress/eklentiler": "wp_durum",
	"GET " + dp + "/domains/{id}/dns":                  "dns_kontrol",
	"GET " + dp + "/admin/system/loglar":               "sunucu_log_oku (AdminOnly)",
}

// DestekYolIzinli — somut istek yolunun DestekRotalari'ndaki bir kalıba TAM uyup
// uymadığı. Özel köprü (internal/destek/kopru) isteği süreç içi yönlendiriciye
// vermeden ÖNCE bunu çağırır: kimlik zaten RoutePattern ile ikinci kez denetlenir
// (destekIstek), bu ise köprünün kendi, bağımsız varsayılan-red kapısıdır.
//
// Kurallar: yalnız GET; yol mutlak ve "/api/v1/" altında; "{id}" bölümü 1-18 rakam;
// diğer bölümler birebir; boş bölüm, ".", "..", "?", "#", "%", kontrol karakteri YOK.
// Dönen desen DestekRotalari anahtarıdır ("GET /api/v1/...").
func DestekYolIzinli(metod, yol string) (string, bool) {
	if metod != http.MethodGet || len(yol) < len(dp)+2 || len(yol) > 512 || !strings.HasPrefix(yol, dp+"/") {
		return "", false
	}
	for i := 0; i < len(yol); i++ {
		c := yol[i]
		if c <= ' ' || c >= 0x7f || c == '?' || c == '#' || c == '%' || c == '\\' {
			return "", false
		}
	}
	parcalar := strings.Split(yol[1:], "/")
	for _, pc := range parcalar {
		if pc == "" || pc == "." || pc == ".." {
			return "", false
		}
	}
	for desen := range DestekRotalari {
		_, kalip, ok := strings.Cut(desen, " ")
		if !ok || !strings.HasPrefix(desen, http.MethodGet+" ") {
			continue
		}
		kp := strings.Split(kalip[1:], "/")
		if len(kp) != len(parcalar) {
			continue
		}
		uydu := true
		for i := range kp {
			if kp[i] == "{id}" {
				if !destekIDRe.MatchString(parcalar[i]) {
					uydu = false
					break
				}
			} else if kp[i] != parcalar[i] {
				uydu = false
				break
			}
		}
		if uydu {
			return desen, true
		}
	}
	return "", false
}

var destekIDRe = regexp.MustCompile(`^[0-9]{1,18}$`)

// destekHesapSorgu — bayi/kök hesabının (rol, durum) satırı. Dikiş (testler).
var destekHesapSorgu = func(ctx context.Context, uid int64) (rol, durum string, err error) {
	if scopeDB == nil {
		return "", "", errVTYok
	}
	err = scopeDB.QueryRowContext(ctx, `SELECT role, status FROM users WHERE id=?`, uid).Scan(&rol, &durum)
	return
}

// destekIstek — RequireAuth'un destek dalı. Kimliği kurar ya da reddeder.
func destekIstek(w http.ResponseWriter, r *http.Request, next http.Handler, dk *DestekKimlik) {
	desen := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		desen = r.Method + " " + rc.RoutePattern()
	}
	if r.Method != http.MethodGet || DestekRotalari[desen] == "" {
		httpx.WriteError(w, http.StatusForbidden, "bu uç destek botuna kapalı")
		return
	}
	ctx := r.Context()
	switch dk.Rol {
	case DestekMusteri:
		if dk.DomainID <= 0 {
			httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
			return
		}
		mc := &auth.MusteriClaims{DomainID: dk.DomainID, AlanAdi: dk.AlanAdi, Kullanici: DestekAktorAdi(dk.TgKisa), Tip: "musteri"}
		ctx = context.WithValue(ctx, musteriClaimsKey, mc)
	case DestekBayi, DestekKok:
		beklenen := "reseller"
		if dk.Rol == DestekKok {
			beklenen = "admin"
		}
		if dk.KullaniciID <= 0 {
			httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
			return
		}
		// 🔴 Hesap HER istekte DB'den doğrulanır (fail-closed): rol uyuşmalı ve
		// etkin olmalı. Askıya alınan bayinin bağlantısı iptal edilmeden önce de
		// destek kimliği hiçbir uçtan geçemez.
		rol, durum, err := destekHesapSorgu(ctx, dk.KullaniciID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rol != beklenen) {
			httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin")
			return
		}
		if durum != "active" {
			redYaz(w, http.StatusForbidden, httpx.HesapAskidaMesaj)
			return
		}
		c := &auth.Claims{UserID: dk.KullaniciID, Username: dk.Kullanici, Role: beklenen}
		if dk.Rol == DestekBayi {
			c.ResellerID = dk.KullaniciID // 🔴 bayi kimliği rid'siz OLUŞAMAZ
		} else if c.Username == "" {
			c.Username = "root"
		}
		ctx = context.WithValue(ctx, claimsKey, c)
	default:
		httpx.WriteError(w, http.StatusUnauthorized, "geçersiz oturum")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	next.ServeHTTP(w, r.WithContext(ctx))
}
