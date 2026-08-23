package middleware

// musteri_oturum.go — MÜŞTERİ token'ının sunucu-taraflı geçerlilik kontrolü.
//
// 🔴 GÜVENLİK BOŞLUĞU (kapatıldı): müşteri token'ı yalnızca İMZA doğrulanarak
// kabul ediliyordu. Yani token üretildikten sonra:
//   - FTP hesabı askıya alınsa,
//   - FTP parolası değiştirilse,
//   - domainin SAHİBİ değiştirilse (devir),
// elindeki token 24 saat boyunca çalışmaya devam ediyordu. Devir, erişimi
// kesmeyen bir etiket değişimiydi.
//
// Artık admin/bayi tarafındaki oturum_cache.go ile AYNI sözleşme uygulanır:
// her istekte ftp_accounts satırı okunur (retry + 30 sn kullanılabilirlik
// önbelleği + fail-CLOSED). "Doğrulayamıyorum" ≠ "geçerli".

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

type musteriDurum struct {
	durum      string
	gecersizTS int64
	zaman      time.Time
}

var (
	musteriCache   = map[int64]musteriDurum{}
	musteriCacheMu sync.RWMutex
)

func musteriCacheYaz(fhid int64, durum string, gecersizTS int64, now time.Time) {
	musteriCacheMu.Lock()
	musteriCache[fhid] = musteriDurum{durum: durum, gecersizTS: gecersizTS, zaman: now}
	musteriCacheMu.Unlock()
}

func musteriCacheOku(fhid int64, now time.Time) (musteriDurum, bool) {
	musteriCacheMu.RLock()
	v, ok := musteriCache[fhid]
	musteriCacheMu.RUnlock()
	if !ok || now.Sub(v.zaman) > oturumCacheTTL {
		return musteriDurum{}, false
	}
	return v, true
}

// musteriKarar: (durum, gecersizTS) → HTTP kodu + mesaj. DB ve önbellek dalları
// aynı kararı kullanır.
func musteriKarar(mc *auth.MusteriClaims, durum string, gecersizTS int64) (int, string) {
	if durum != "active" {
		return http.StatusForbidden, "hesabınız askıya alınmış"
	}
	if gecersizTS > 0 && mc.IssuedAt != nil && mc.IssuedAt.Unix() < gecersizTS {
		return http.StatusUnauthorized, "oturum sonlandırıldı, tekrar giriş yapın"
	}
	return 0, ""
}

// musteriSorguFn: testte stub edilebilsin diye değişken.
var musteriSorguFn = musteriDBSorgu

func musteriDBSorgu(ctx context.Context, fhid int64) (bool, string, int64, error) {
	const sorgu = `SELECT status, COALESCE(token_gecersiz_ts,0) FROM ftp_accounts WHERE id=?`
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
		err := scopeDB.QueryRowContext(ctx, sorgu, fhid).Scan(&durum, &ts)
		if err == nil {
			return true, durum, ts, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", 0, nil // hesap silinmiş — retry anlamsız
		}
		son = err
	}
	return false, "", 0, son
}

// musteriOturumGecerli: (0,"") = geçerli; aksi halde HTTP kodu + mesaj.
func musteriOturumGecerli(ctx context.Context, mc *auth.MusteriClaims) (int, string) {
	if scopeDB == nil || mc == nil {
		return 0, ""
	}
	if mc.FTPHesapID <= 0 {
		// Eski (kimliksiz) token — hesap satırına bağlanamıyor, iptal edilemez.
		// Yeni girişler her zaman fhid taşır; bu token'ı kabul etmek iptal
		// mekanizmasını tümden delerdi.
		return http.StatusUnauthorized, "oturum yenilenmeli, tekrar giriş yapın"
	}
	now := nowFn()
	bulundu, durum, gecersizTS, err := musteriSorguFn(ctx, mc.FTPHesapID)
	if err == nil {
		if !bulundu {
			return http.StatusForbidden, "hesap bulunamadı"
		}
		musteriCacheYaz(mc.FTPHesapID, durum, gecersizTS, now)
		return musteriKarar(mc, durum, gecersizTS)
	}
	if v, ok := musteriCacheOku(mc.FTPHesapID, now); ok {
		return musteriKarar(mc, v.durum, v.gecersizTS)
	}
	log.Printf("musteri oturum kontrolu (fhid=%d) DB hatasi + onbellek yok: %v — istek REDDEDILDI (fail-closed)", mc.FTPHesapID, err)
	return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin"
}
