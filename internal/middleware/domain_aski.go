package middleware

// Müşteri (domain) askı kontrolü için oturum_cache.go'daki ile AYNI sınıf
// bounded fail-closed davranışı. Bağımsız doğrulama, MusteriScopeParam'daki
// domain-askı kontrolünün geçici DB hatasında fail-OPEN olduğunu (askıdaki
// müşteri kesinti penceresinde kendi domainine erişebiliyor) ve bunun
// oturumGecerli'nin fail-CLOSED felsefesiyle tutarsız olduğunu tespit etti.
// Burada aynı retry + kısa-ömürlü önbellek + fail-CLOSED deseni uygulanır.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
)

type domainAskiDurum struct {
	askida bool
	zaman  time.Time
}

var (
	domainAskiCache   = map[int64]domainAskiDurum{}
	domainAskiCacheMu sync.RWMutex
)

func domainAskiCacheYaz(id int64, askida bool, now time.Time) {
	domainAskiCacheMu.Lock()
	domainAskiCache[id] = domainAskiDurum{askida: askida, zaman: now}
	domainAskiCacheMu.Unlock()
}

func domainAskiCacheOku(id int64, now time.Time) (domainAskiDurum, bool) {
	domainAskiCacheMu.RLock()
	v, ok := domainAskiCache[id]
	domainAskiCacheMu.RUnlock()
	if !ok || now.Sub(v.zaman) > oturumCacheTTL {
		return domainAskiDurum{}, false
	}
	return v, true
}

// domainAskiSorguFn: DB-okuma adımı (testte stub edilir).
var domainAskiSorguFn = domainAskiSorgu

// domainAskiSorgu: domains.askida okur; retry dahil. (bulundu, askida, err).
// ErrNoRows → (false,false,nil): domain yok, geçici hata DEĞİL.
func domainAskiSorgu(ctx context.Context, id int64) (bool, bool, error) {
	const sorgu = `SELECT COALESCE(askida,0) FROM domains WHERE id=?`
	var son error
	for deneme := 0; deneme <= oturumRetryAdet; deneme++ {
		if deneme > 0 {
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case <-time.After(oturumRetryBekle):
			}
		}
		var askida int
		err := scopeDB.QueryRowContext(ctx, sorgu, id).Scan(&askida)
		if err == nil {
			return true, askida == 1, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, nil
		}
		son = err
	}
	return false, false, son
}

// domainAskidaKod: müşteri token'ı için domain-askı kararı.
//
//	(0,"")   → geç
//	(403,..) → askıda
//	(503,..) → doğrulanamadı (fail-CLOSED, önbellek yok)
//
// scopeDB nil ise kontrol atlanır (erken açılış).
func domainAskidaKod(ctx context.Context, domainID int64) (int, string) {
	if scopeDB == nil {
		return 0, ""
	}
	now := nowFn()
	bulundu, askida, err := domainAskiSorguFn(ctx, domainID)
	if err == nil {
		// Domain yoksa (bulundu=false) eski davranış: geçir (sahiplik zaten
		// eşleşti; domain silinmişse erişilecek bir şey de yok).
		if !bulundu {
			return 0, ""
		}
		domainAskiCacheYaz(domainID, askida, now)
		if askida {
			return http.StatusForbidden, "hesap askıya alınmış"
		}
		return 0, ""
	}
	// Geçici DB hatası → son bilinen durum taze mi?
	if v, ok := domainAskiCacheOku(domainID, now); ok {
		if v.askida {
			return http.StatusForbidden, "hesap askıya alınmış"
		}
		return 0, ""
	}
	log.Printf("domain aski kontrolu (domain=%d) DB hatasi + onbellek yok: %v — istek REDDEDILDI (fail-closed)", domainID, err)
	return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin"
}
