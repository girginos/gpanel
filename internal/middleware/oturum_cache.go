package middleware

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"girginospanel/internal/auth"
)

// ── Oturum durum önbelleği (fail-CLOSED + kullanılabilirlik önbelleği) ────────
//
// 🔴 GÜVENLİK: eski davranış, geçici bir DB hatasında oturum kontrolünü SESSİZCE
// GEÇİYORDU (fail-open). Sonuç: MariaDB kısa bir süre cevap vermediğinde ASKIYA
// ALINMIŞ bir bayi ya da parola değişikliğiyle GEÇERSİZ KILINMIŞ bir oturum
// kabul edilirdi — kimlik-doğrulamalı her istek bu koddan geçtiği için bu, bir
// kesinti penceresinde yetkilendirme kontrolünün tümden kalkması demekti.
//
// Yeni davranış = FAIL-CLOSED + kısa-ömürlü kullanılabilirlik önbelleği:
//   1. Geçici hatada birkaç kez KISA retry (blip'lerin çoğu ms içinde geçer).
//   2. Hâlâ hata varsa: bu kullanıcı için SON BAŞARILI kontrol taze mi (< TTL)?
//      Evet → o değeri (durum + token_gecersiz_ts) yeniden uygula. Böylece
//      askıya alınmış/geçersiz kılınmış bir hesap ÖNBELLEKTE de reddedilir;
//      yakın zamanda görülmüş sağlam bir hesap ise kesinti boyunca çalışır
//      (kullanılabilirlik korunur).
//   3. Önbellekte taze kayıt yoksa → fail-CLOSED (503). "Doğrulayamıyorum" ile
//      "geçerli" ARTIK aynı şey değil.
//
// Önbellek yalnızca DB'nin DAHA ÖNCE döndürdüğü gerçeği saklar; bir yetkiyi
// kendi başına ÜRETMEZ. Askıya alma/oturum düşürme DB'ye yazıldıktan sonra ilk
// başarılı okumada önbelleğe de yansır ve sonraki kesintide de geçerli kalır.

type oturumDurum struct {
	durum      string
	gecersizTS int64
	zaman      time.Time
}

var (
	oturumCache   = map[int64]oturumDurum{}
	oturumCacheMu sync.RWMutex
)

const (
	oturumCacheTTL   = 30 * time.Second // kesintide kabul edilecek en eski kayıt
	oturumRetryAdet  = 2                // geçici hatada ek deneme sayısı
	oturumRetryBekle = 40 * time.Millisecond
)

func oturumCacheYaz(uid int64, durum string, gecersizTS int64, now time.Time) {
	oturumCacheMu.Lock()
	oturumCache[uid] = oturumDurum{durum: durum, gecersizTS: gecersizTS, zaman: now}
	oturumCacheMu.Unlock()
}

func oturumCacheOku(uid int64, now time.Time) (oturumDurum, bool) {
	oturumCacheMu.RLock()
	v, ok := oturumCache[uid]
	oturumCacheMu.RUnlock()
	if !ok || now.Sub(v.zaman) > oturumCacheTTL {
		return oturumDurum{}, false
	}
	return v, true
}

// nowFn: testlerde zaman sabitlemek için (üretimde time.Now).
var nowFn = time.Now

// oturumKarar: (durum, gecersizTS) → HTTP kodu + mesaj. DB-okuması ve önbellek
// dalları AYNI kararı kullanır ki davranış tutarlı olsun.
func oturumKarar(c *auth.Claims, durum string, gecersizTS int64) (int, string) {
	if c.Role == "reseller" && durum != "active" {
		return http.StatusForbidden, "hesabınız askıya alınmış"
	}
	if gecersizTS > 0 && c.IssuedAt != nil && c.IssuedAt.Unix() < gecersizTS {
		return http.StatusUnauthorized, "oturum sonlandırıldı, tekrar giriş yapın"
	}
	return 0, ""
}

// oturumSorguFn: DB-okuma adımı. Üretimde gerçek sorgu; testte stub edilir
// (böylece harici bir sqlmock bağımlılığına gerek kalmaz).
var oturumSorguFn = oturumDBSorgu

// oturumDBSorgu: users satırını okur; retry'ler dahil. (bulundu, durum, ts, err).
func oturumDBSorgu(ctx context.Context, uid int64) (bool, string, int64, error) {
	const sorgu = `SELECT status, COALESCE(token_gecersiz_ts,0) FROM users WHERE id=?`
	var son error
	for deneme := 0; deneme <= oturumRetryAdet; deneme++ {
		if deneme > 0 {
			select {
			case <-ctx.Done():
				return false, "", 0, ctx.Err()
			case <-time.After(oturumRetryBekle):
			}
		}
		var durum string
		var ts int64
		err := scopeDB.QueryRowContext(ctx, sorgu, uid).Scan(&durum, &ts)
		if err == nil {
			return true, durum, ts, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", 0, nil // "satır yok" geçici DEĞİL — retry etme
		}
		son = err
	}
	return false, "", 0, son
}

// oturumGecerliImpl: oturumGecerli'nin gerçek gövdesi (retry + önbellek + fail-closed).
func oturumGecerliImpl(ctx context.Context, c *auth.Claims) (int, string) {
	if scopeDB == nil || c == nil {
		return 0, ""
	}
	if c.Role != "reseller" && c.Role != "admin" {
		return 0, ""
	}
	now := nowFn()
	bulundu, durum, gecersizTS, err := oturumSorguFn(ctx, c.UserID)
	if err == nil {
		if !bulundu {
			// users satırı yok. Reseller için hesap gerçekten yok = reddet;
			// admin/kök için satır şart değil (kurulum varyantları).
			if c.Role == "reseller" {
				return http.StatusForbidden, "hesap bulunamadı"
			}
			return 0, ""
		}
		oturumCacheYaz(c.UserID, durum, gecersizTS, now)
		return oturumKarar(c, durum, gecersizTS)
	}

	// Geçici DB hatası (retry'ler de tükendi). SINIRLI fail-open:
	if v, ok := oturumCacheOku(c.UserID, now); ok {
		// Son bilinen durumu uygula — askı/oturum-düşürme burada da geçerli.
		return oturumKarar(c, v.durum, v.gecersizTS)
	}
	// Taze önbellek yok → FAIL-CLOSED. Sessizce geçirmek, kesinti penceresinde
	// yetkilendirmeyi tümden kapatırdı.
	log.Printf("oturum kontrolu (uid=%d) DB hatasi + onbellek yok: %v — istek REDDEDILDI (fail-closed)", c.UserID, err)
	return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin"
}
