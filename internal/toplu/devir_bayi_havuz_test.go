package toplu

// devir_bayi_havuz_test.go — devirde bayi kaynak havuzu eşitlemesi: yalnız SAHİP DEĞİŞİNCE
// tetiklenir (eski bayinin dilimi/üyeliği/proje atamasının temizlenmesi). Gerçek sunucu
// dosyalarına (systemd, xfs_quota) hiçbir test dokunmaz: kanca paket başlatmasında kayıt
// tutan sahteyle değiştirilir.

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

var bhDevirKayit struct {
	mu   sync.Mutex
	cagr []int64
}

func init() {
	bayiHavuzDevir = func(_ *sql.DB, domainID int64) {
		bhDevirKayit.mu.Lock()
		defer bhDevirKayit.mu.Unlock()
		bhDevirKayit.cagr = append(bhDevirKayit.cagr, domainID)
	}
}

func bhDevirSifirla(t *testing.T) {
	t.Helper()
	bhDevirKayit.mu.Lock()
	bhDevirKayit.cagr = nil
	bhDevirKayit.mu.Unlock()
}

func bhDevirCagrilar() []int64 {
	bhDevirKayit.mu.Lock()
	defer bhDevirKayit.mu.Unlock()
	return append([]int64(nil), bhDevirKayit.cagr...)
}

func TestDevir_BayiHavuzuYalnizSahipDegisinceUzlasir(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db) // domain dt_did bayi 7'nin; bayi 8 hedef
	h := &Handlers{DB: db}

	// Sahip değişti (7 → 8): havuz eşitlemesi tam bir kez, doğru domain için.
	bhDevirSifirla(t)
	hedef := int64(8)
	if _, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root"); err != nil {
		t.Fatalf("devret: %v", err)
	}
	if c := bhDevirCagrilar(); len(c) != 1 || c[0] != dt_did {
		t.Fatalf("sahip değişince havuz eşitlenmedi: %v", c)
	}

	// Aynı bayiye "devir" (8 → 8): havuzlara dokunulmaz.
	bhDevirSifirla(t)
	if _, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root"); err != nil {
		t.Fatalf("devret(aynı sahip): %v", err)
	}
	if c := bhDevirCagrilar(); len(c) != 0 {
		t.Fatalf("sahip değişmedi ama havuz eşitlendi: %v", c)
	}

	// Bayiden doğrudan müşteriye (yeni bayi yok): eski bayinin havuzundan ÇIKIŞ da hemen
	// eşitlenir (30 dakikalık tura bırakılmaz).
	bhDevirSifirla(t)
	dt_exec(t, db, `INSERT INTO users (id, role, status) VALUES (30,'customer','active')`)
	musteri := int64(30)
	if _, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, nil, &musteri, 1, "root"); err != nil {
		t.Fatalf("devret(müşteriye): %v", err)
	}
	if c := bhDevirCagrilar(); len(c) != 1 || c[0] != dt_did {
		t.Fatalf("bayiden müşteriye devirde eski havuz eşitlenmedi: %v", c)
	}
}
