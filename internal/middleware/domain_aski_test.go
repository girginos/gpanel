package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// domainAskidaKod'un bounded fail-CLOSED davranışını kanıtlar (sqlmock'suz).
func TestDomainAskidaFailClosed(t *testing.T) {
	scopeDB = &sql.DB{}
	defer func() { scopeDB = nil }()
	base := time.Unix(3_000_000, 0)
	nowFn = func() time.Time { return base }
	defer func() { nowFn = time.Now }()
	defer func() { domainAskiSorguFn = domainAskiSorgu }()

	askiOk := func(askida bool) func(context.Context, int64) (bool, bool, error) {
		return func(context.Context, int64) (bool, bool, error) { return true, askida, nil }
	}
	yok := func(context.Context, int64) (bool, bool, error) { return false, false, nil }
	patlat := func(context.Context, int64) (bool, bool, error) { return false, false, sql.ErrConnDone }

	// 1) Sağlıklı DB, askıda → 403 + önbelleğe yazılır.
	domainAskiSorguFn = askiOk(true)
	if kod, _ := domainAskidaKod(context.Background(), 50); kod != http.StatusForbidden {
		t.Fatalf("askidaki domain 403 olmali, kod=%d", kod)
	}
	// 2) DB hatası + taze önbellek(askıda) → yine 403.
	domainAskiSorguFn = patlat
	if kod, _ := domainAskidaKod(context.Background(), 50); kod != http.StatusForbidden {
		t.Fatalf("DB hatasinda onbellekteki aski korunmali, kod=%d", kod)
	}
	// 3) Sağlıklı DB, askıda değil → geç + önbelleğe.
	domainAskiSorguFn = askiOk(false)
	if kod, _ := domainAskidaKod(context.Background(), 60); kod != 0 {
		t.Fatalf("askida-olmayan domain gecmeli, kod=%d", kod)
	}
	// 4) DB hatası + taze önbellek(aktif) → geç.
	domainAskiSorguFn = patlat
	if kod, _ := domainAskidaKod(context.Background(), 60); kod != 0 {
		t.Fatalf("blip'te taze-aktif domain gecmeli, kod=%d", kod)
	}
	// 5) DB hatası + önbellek YOK → fail-CLOSED 503.
	domainAskiSorguFn = patlat
	if kod, _ := domainAskidaKod(context.Background(), 999); kod != http.StatusServiceUnavailable {
		t.Fatalf("onbelleksiz DB hatasi 503 olmali, kod=%d", kod)
	}
	// 6) Domain yok (ErrNoRows sınıfı) + DB sağlıklı → geç (eski davranış korunur).
	domainAskiSorguFn = yok
	if kod, _ := domainAskidaKod(context.Background(), 12345); kod != 0 {
		t.Fatalf("domain-yok gecmeli (eski davranis), kod=%d", kod)
	}
}
