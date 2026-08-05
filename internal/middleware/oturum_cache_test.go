package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"girginospanel/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

// SINIRLI fail-open davranışını kanıtlar (sqlmock'suz — oturumSorguFn stub'lanır).
func reseller(uid int64) *auth.Claims {
	c := &auth.Claims{UserID: uid, Role: "reseller"}
	c.IssuedAt = jwt.NewNumericDate(time.Unix(1_000_000, 0))
	return c
}

func TestOturumFailClosed(t *testing.T) {
	scopeDB = &sql.DB{} // yalnız nil-kontrolünü geçmek için; sorgu stub'lanıyor
	defer func() { scopeDB = nil }()

	base := time.Unix(2_000_000, 0)
	nowFn = func() time.Time { return base }
	defer func() { nowFn = time.Now }()
	defer func() { oturumSorguFn = oturumDBSorgu }()

	ok := func(durum string) func(context.Context, int64) (bool, string, int64, error) {
		return func(context.Context, int64) (bool, string, int64, error) { return true, durum, 0, nil }
	}
	patlat := func(context.Context, int64) (bool, string, int64, error) {
		return false, "", 0, sql.ErrConnDone
	}

	// 1) Sağlıklı DB, askıda hesap → 403 + önbelleğe 'suspended'.
	oturumSorguFn = ok("suspended")
	if code, _ := oturumGecerliImpl(context.Background(), reseller(7)); code != http.StatusForbidden {
		t.Fatalf("askidaki hesap reddedilmeli, kod=%d", code)
	}
	// 2) DB hatası + taze önbellek(suspended) → yine 403 (güvenlik korunur).
	oturumSorguFn = patlat
	if code, _ := oturumGecerliImpl(context.Background(), reseller(7)); code != http.StatusForbidden {
		t.Fatalf("DB hatasinda onbellekteki askı korunmali, kod=%d", code)
	}

	// 3) Sağlıklı DB, aktif hesap → geç + önbelleğe 'active'.
	oturumSorguFn = ok("active")
	if code, _ := oturumGecerliImpl(context.Background(), reseller(9)); code != 0 {
		t.Fatalf("aktif hesap gecmeli, kod=%d", code)
	}
	// 4) DB hatası + taze önbellek(active) → geç (kullanılabilirlik).
	oturumSorguFn = patlat
	if code, _ := oturumGecerliImpl(context.Background(), reseller(9)); code != 0 {
		t.Fatalf("DB blip'inde taze-aktif hesap gecmeli, kod=%d", code)
	}

	// 5) DB hatası + önbellek YOK → fail-CLOSED 503.
	oturumSorguFn = patlat
	if code, _ := oturumGecerliImpl(context.Background(), reseller(99)); code != http.StatusServiceUnavailable {
		t.Fatalf("onbelleksiz DB hatasi FAIL-CLOSED (503) olmali, kod=%d", code)
	}

	// 6) Önbellek bayatlarsa (TTL aşımı) yine fail-CLOSED.
	nowFn = func() time.Time { return base.Add(oturumCacheTTL + time.Second) }
	oturumSorguFn = patlat
	if code, _ := oturumGecerliImpl(context.Background(), reseller(9)); code != http.StatusServiceUnavailable {
		t.Fatalf("bayat onbellek fail-CLOSED olmali, kod=%d", code)
	}
}
