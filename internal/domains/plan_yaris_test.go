package domains

// plan_yaris_test.go — VM doğrulaması 2026-10-13 (ajan C) bulguları:
//   - PUT /plan'dan hemen sonra silinen hesabın vhost'u arka plan uygulamasıyla yeniden yazılıp
//     yetim kalıyordu → planSonrasiUygula hesap kilidi altında ve silinmişse hiçbir şey yazmaz;
//   - bayi düşürme kapısı bayat boyut_kb'ye bakıyordu → canlı kota sayacı büyükse o esas alınır.

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/kilit"
	"girginospanel/internal/sahtedb"
)

func TestPlanSonrasiUygula_SilinmisHesabaYazmaz_SilmeyleSeri(t *testing.T) {
	eK, eW := planSonrasiKaynak, planSonrasiWAF
	t.Cleanup(func() { planSonrasiKaynak, planSonrasiWAF = eK, eW })
	var cagri int32
	planSonrasiKaynak = func(context.Context, *sql.DB, int64) error { atomic.AddInt32(&cagri, 1); return nil }
	planSonrasiWAF = func(*sql.DB, int64) error { atomic.AddInt32(&cagri, 1); return nil }

	// Silinmiş hesap: hiçbir uygulama çağrılmaz.
	_, db := sahtedb.Yeni()
	planSonrasiUygula(db, 42, "yok.test")
	if n := atomic.LoadInt32(&cagri); n != 0 {
		t.Fatalf("silinmiş hesaba uygulama yapıldı: %d", n)
	}

	// Var olan hesap, ama silme kilidi tutuluyor: uygulama kilit bırakılana dek bekler.
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici FROM domains WHERE id=?", []any{"c_plan_yaris"})
	sd.Satirlar("SELECT 1 FROM domains WHERE id=?", []any{int64(1)})
	hk := kilit.Hesap("c_plan_yaris")
	hk.Lock()
	bitti := make(chan struct{})
	go func() { planSonrasiUygula(db, 42, "var.test"); close(bitti) }()
	select {
	case <-bitti:
		hk.Unlock()
		t.Fatal("uygulama hesap kilidini beklemedi (silmeyle yarışır)")
	case <-time.After(150 * time.Millisecond):
	}
	hk.Unlock()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("kilit bırakılınca uygulama bitmedi")
	}
	if n := atomic.LoadInt32(&cagri); n != 2 {
		t.Fatalf("var olan hesapta kaynak + WAF uygulanmalı (2), olan: %d", n)
	}
}

func TestPlanKullanimSigar_CanliKotaSayaciEsas(t *testing.T) {
	eC := canliDiskMB
	t.Cleanup(func() { canliDiskMB = eC })
	sd, db := sahtedb.Yeni()
	sd.Satirlar("FROM domains d LEFT JOIN hosting_limit_ust", []any{int64(20), int64(0)}) // yeni plan 20 MB
	sd.Satirlar("SELECT COALESCE(boyut_kb,0) FROM domains", []any{int64(24)})             // bayat: 24 KB
	sd.Satirlar("SELECT sistem_kullanici FROM domains WHERE id=?", []any{"c_canli"})
	h := &Handlers{DB: db}

	canliDiskMB = func(string) int64 { return 40 } // XFS: 40 MB
	err := h.planKullanimSigar(context.Background(), 7, 3)
	if err == nil || !strings.Contains(err.Error(), "(40 MB)") {
		t.Fatalf("canlı kullanım (40 MB) yeni kotayı (20 MB) aşıyor: ret beklenirdi, olan %v", err)
	}
	canliDiskMB = func(string) int64 { return 5 }
	if err := h.planKullanimSigar(context.Background(), 7, 3); err != nil {
		t.Fatalf("canlı 5 MB < 20 MB: geçmeliydi: %v", err)
	}
}
